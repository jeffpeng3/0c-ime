//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

func waitJaHealth(t *testing.T, client *ipc.Client, what string, ok func(map[string]string) bool) map[string]string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last map[string]string
	for time.Now().Before(deadline) {
		info, err := client.Health()
		if err != nil {
			t.Fatal(err)
		}
		last = info
		if ok(info) {
			return info
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("等不到 %s：%v", what, last)
	return nil
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

// 真正的 0c-server：OpenSession hook 把日文 profile 與共享 reranker 掛上，
// 按鍵走日文 composer、変換走 T12 draft + T30 rerank + T14 mock worker。
func TestJaServerSessionUsesSharedNeuralReranker(t *testing.T) {
	python, script, pkg := jaMockPaths(t)
	client := startJaTestServer(t, jaServerData(t),
		"-ja-neural-package", pkg,
		"-ja-neural-python", python,
		"-ja-neural-script", script,
		"-ja-neural-server", "mock",
		"-ja-neural-threads", "2",
	)
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

	// 第一次変換觸發 worker 背景啟動；Cancel 回かな，等就緒再做一次。
	if _, err := client.Handle(proto.Request{Type: proto.Key, Key: proto.KeyConvert}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Handle(proto.Request{Type: proto.Key, Key: proto.KeyEscape}); err != nil {
		t.Fatal(err)
	}
	info := waitJaHealth(t, client, "日文 worker 就緒", func(info map[string]string) bool {
		return info["ja_neural_state"] == "ready"
	})
	if info["ja_neural_state"] != "ready" {
		t.Fatalf("health = %v", info)
	}

	commit := ipcJaConvert(t, client)
	if commit.Commit == "" || commit.Converting || commit.Language != "ja" {
		t.Fatalf("送出 = %#v", commit)
	}
	info = waitJaHealth(t, client, "神經重排真的執行", func(info map[string]string) bool {
		n, _ := strconv.Atoi(info["ja_rerank_verified"])
		return n > 0
	})
	if calls, _ := strconv.Atoi(info["ja_rerank_calls"]); calls == 0 {
		t.Fatalf("rerank calls = 0：%v", info)
	}
}

// 模型檔缺失時 hook 仍建立日文 session，但 reranker 不執行，輸入照統計轉換。
func TestJaServerSessionFallsBackWhenModelMissing(t *testing.T) {
	pkg := t.TempDir()
	manifest := `{"schema":"ja-zenz-verify-v1","backend":"llama.cpp","backend_commit":"test",` +
		`"context_policy":"confirmed_left_128","model":{"file":"model.gguf","sha256":"` +
		"0000000000000000000000000000000000000000000000000000000000000000" + `","bytes":5}}`
	if err := os.WriteFile(filepath.Join(pkg, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	client := startJaTestServer(t, jaServerData(t), "-ja-neural-package", pkg)
	if err := client.Open(); err != nil {
		t.Fatal(err)
	}
	info := waitJaHealth(t, client, "模型缺失狀態", func(info map[string]string) bool {
		return info["ja_neural_state"] == "fallback"
	})
	if info["ja_neural_reason"] != "model_missing" {
		t.Fatalf("health = %v", info)
	}
	snap := ipcJaKeys(t, client, "kyouha")
	if snap.Language != "ja" {
		t.Fatalf("session 不是日文：%q", snap.Language)
	}
	commit := ipcJaConvert(t, client)
	if commit.Commit != "今日は" {
		t.Fatalf("統計轉換 = %q", commit.Commit)
	}
	info, err := client.Health()
	if err != nil {
		t.Fatal(err)
	}
	if info["ja_rerank_calls"] != "0" || info["ja_rerank_verified"] != "0" {
		t.Fatalf("模型缺失仍呼叫 rerank：%v", info)
	}
}
