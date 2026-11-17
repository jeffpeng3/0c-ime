//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0c-biggo/0c-ime/bridge/ipc"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// jaServerData 複製最小的注音資料包，再放入日文詞典，讓 0c-server 可以
// 同時載入注音與日文。
func jaServerData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"lexicon.tsv", "lm.tsv"} {
		b, err := os.ReadFile(filepath.Join("../../testdata/toy", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "ja", "lexicon"), 0o755); err != nil {
		t.Fatal(err)
	}
	words := "# 詞面\t讀音\t成本\t詞性\n今日\tきょう\t1.0\tnoun\n京\tきょう\t3.0\tnoun\nは\tは\t1.0\tparticle\n"
	if err := os.WriteFile(filepath.Join(dir, "ja", "lexicon", "words.tsv"), []byte(words), 0o644); err != nil {
		t.Fatal(err)
	}
	conns := "# 左詞性\t右詞性\t成本\n<s>\tnoun\t0.2\nnoun\tparticle\t0.2\nparticle\t</s>\t0.2\nnoun\t</s>\t0.2\n"
	if err := os.WriteFile(filepath.Join(dir, "ja", "connection.tsv"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func startJaTestServer(t *testing.T, data string, extra ...string) *ipc.Client {
	t.Helper()
	binary := buildServer(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "0c.sock")
	args := []string{"-data", data, "-user", filepath.Join(dir, "user.json"), "-socket", sock, "-language", "ja"}
	args = append(args, extra...)
	cmd := exec.Command(binary, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})
	waitDial(t, sock)
	client, err := ipc.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func ipcJaKeys(t *testing.T, client *ipc.Client, keys string) proto.Snapshot {
	t.Helper()
	var snap proto.Snapshot
	for _, k := range keys {
		var err error
		snap, err = client.Handle(proto.Request{Type: proto.Key, Key: string(k)})
		if err != nil {
			t.Fatal(err)
		}
	}
	return snap
}

func ipcJaConvert(t *testing.T, client *ipc.Client) proto.Snapshot {
	t.Helper()
	snap, err := client.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Converting {
		t.Fatalf("沒有進入変換：%#v", snap)
	}
	snap, err = client.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEnter})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// 真正的 0c-server：日文 session 只走統計轉換，health 明確回報
// statistical，不再有 neural/rerank 狀態，也沒有模型或 worker 可啟動。
func TestJaServerSessionUsesStatisticalConversion(t *testing.T) {
	client := startJaTestServer(t, jaServerData(t))
	if err := client.Open(); err != nil {
		t.Fatal(err)
	}
	snap := ipcJaKeys(t, client, "kyouha")
	if snap.Language != "ja" || snap.InputMethod != "romaji" {
		t.Fatalf("session 不是日文：language=%q method=%q", snap.Language, snap.InputMethod)
	}
	if snap.Text() != "きょうは" {
		t.Fatalf("組字 = %q", snap.Text())
	}
	commit := ipcJaConvert(t, client)
	if commit.Commit != "今日は" || commit.Converting || commit.Language != "ja" {
		t.Fatalf("送出 = %#v", commit)
	}

	info, err := client.Health()
	if err != nil {
		t.Fatal(err)
	}
	if info["ja_state"] != "statistical" {
		t.Fatalf("health = %v", info)
	}
	for _, key := range []string{"ja_neural_state", "ja_neural_reason", "ja_rerank_calls", "ja_rerank_verified"} {
		if _, ok := info[key]; ok {
			t.Fatalf("正式路徑仍有 %q：%v", key, info)
		}
	}
}

// 日文資料缺失時 -language ja 不會讓 session 壞掉；session 留在注音，
// health 回報 not_installed。
func TestJaServerWithoutDataStaysZhuyin(t *testing.T) {
	data := t.TempDir()
	for _, name := range []string{"lexicon.tsv", "lm.tsv"} {
		b, err := os.ReadFile(filepath.Join("../../testdata/toy", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	client := startJaTestServer(t, data)
	if err := client.Open(); err != nil {
		t.Fatal(err)
	}
	snap := ipcJaKeys(t, client, "a")
	if snap.Language != "" && snap.Language != "zh" {
		t.Fatalf("沒有日文資料仍切到 %q", snap.Language)
	}
	info, err := client.Health()
	if err != nil {
		t.Fatal(err)
	}
	if info["ja_state"] != "not_installed" {
		t.Fatalf("health = %v", info)
	}
}
