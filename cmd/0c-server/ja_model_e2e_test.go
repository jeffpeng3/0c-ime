//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/bridge/ipc"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// TestJAModelPackageEndToEnd is the real-model acceptance gate. It needs the
// native build, the installed Japanese model package, a Japanese data pack and
// the ONNX Runtime shared library. It is skipped unless all paths are set:
//
//	P230F_JA_E2E=1
//	P230F_JA_DATA=/path/to/data
//	P230F_JA_PACKAGE=/path/to/models/install
//	P230F_JA_LOCK=/path/to/models/pairing.lock.json
//	P230F_JA_ORT_LIB=/path/to/libonnxruntime.so
//	P230F_JA_INPUTS=kyouha,shinji,koushou,kikan
func TestJAModelPackageEndToEnd(t *testing.T) {
	if os.Getenv("P230F_JA_E2E") != "1" {
		t.Skip("set P230F_JA_E2E=1 and the P230F_JA_* paths to run the real-model gate")
	}
	data := os.Getenv("P230F_JA_DATA")
	pkg := os.Getenv("P230F_JA_PACKAGE")
	lock := os.Getenv("P230F_JA_LOCK")
	lib := os.Getenv("P230F_JA_ORT_LIB")
	if data == "" || pkg == "" || lock == "" || lib == "" {
		t.Fatal("P230F_JA_DATA, P230F_JA_PACKAGE, P230F_JA_LOCK and P230F_JA_ORT_LIB are required")
	}
	inputs := strings.Split(os.Getenv("P230F_JA_INPUTS"), ",")
	if len(inputs) < 3 || inputs[0] == "" {
		inputs = []string{"kyouha", "shinji", "koushou", "kikan"}
	}
	binary := t.TempDir() + "/0c-server"
	if out, err := exec.Command("go", "build", "-tags", "onnxruntime", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}

	stat := runJAServer(t, binary, data, "", lock, "", "none")
	model := runJAServer(t, binary, data, pkg, lock, lib, "all")
	before, _ := stat.candidates(t, inputs)
	after, commits := model.candidates(t, inputs)

	health := model.health(t)
	for key, want := range map[string]string{
		"ja_reranker_state": "ready", "ja_reranker_quality": "trained",
		"ja_reranker_placeholder": "false", "ja_punctuation_state": "ready",
		"ja_punctuation_quality": "trained", "ja_state": "neural",
	} {
		if got := health[key]; got != want {
			t.Errorf("health %s = %q, want %q", key, got, want)
		}
	}
	changed := 0
	for i := range inputs {
		t.Logf("%s: before=%v after=%v", inputs[i], before[i], after[i])
		if !reflect.DeepEqual(before[i], after[i]) {
			changed++
		}
	}
	if changed < 3 {
		t.Fatalf("reranker changed only %d candidate orders", changed)
	}
	punctuated := false
	for _, commit := range commits {
		if strings.ContainsAny(commit, "。、？") {
			punctuated = true
		}
	}
	if !punctuated {
		t.Fatalf("no Japanese punctuation at commit: %v", commits)
	}
}

type jaServerRun struct {
	t     *testing.T
	cmd   *exec.Cmd
	pid   int
	dir   string
	sock  string
	label string
}

func runJAServer(t *testing.T, binary, data, pkg, lock, lib, mode string) *jaServerRun {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "0c.sock")
	args := []string{"-data", data, "-user", filepath.Join(dir, "user.json"), "-socket", sock,
		"-language", "ja", "-model-mode", mode, "-primary-language", "ja"}
	if pkg != "" {
		args = append(args, "-model-package-root", pkg, "-model-package-lock", lock,
			"-neural-backend", "native", "-neural-ort-lib", lib)
	}
	cmd := exec.Command(binary, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	run := &jaServerRun{t: t, cmd: cmd, pid: cmd.Process.Pid, dir: dir, sock: sock, label: mode}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})
	waitDial(t, sock)
	return run
}

func (r *jaServerRun) client() *ipc.Client {
	r.t.Helper()
	c, err := ipc.Dial("unix", r.sock)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	return c
}

func (r *jaServerRun) health(t *testing.T) map[string]string {
	t.Helper()
	c := r.client()
	deadline := time.Now().Add(20 * time.Second)
	for {
		info, err := c.Health()
		if err != nil {
			t.Fatal(err)
		}
		if info["ja_reranker_state"] != "loading" || time.Now().After(deadline) {
			return info
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *jaServerRun) candidates(t *testing.T, inputs []string) ([][]string, []string) {
	t.Helper()
	c := r.client()
	var orders [][]string
	var commits []string
	for _, reading := range inputs {
		if err := c.Open(); err != nil {
			t.Fatal(err)
		}
		var snap proto.Snapshot
		var err error
		for _, k := range reading {
			if snap, err = c.Handle(proto.Request{Type: proto.Key, Key: string(k)}); err != nil {
				t.Fatal(err)
			}
		}
		if snap, err = c.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert}); err != nil {
			t.Fatal(err)
		}
		if snap, err = c.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert}); err != nil {
			t.Fatal(err)
		}
		var order []string
		if snap.Candidates != nil {
			for _, item := range snap.Candidates.Items {
				order = append(order, item.Text)
			}
		}
		orders = append(orders, order)
		if snap, err = c.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter}); err != nil {
			t.Fatal(err)
		}
		commits = append(commits, snap.Commit)
	}
	if r.pid > 0 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", r.pid)); err == nil {
			var rss, hwm string
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "VmRSS:") {
					rss = strings.Join(strings.Fields(line)[1:], " ")
				}
				if strings.HasPrefix(line, "VmHWM:") {
					hwm = strings.Join(strings.Fields(line)[1:], " ")
				}
			}
			r.t.Logf("%s process memory: VmRSS=%s VmHWM=%s", r.label, rss, hwm)
		}
	}
	return orders, commits
}
