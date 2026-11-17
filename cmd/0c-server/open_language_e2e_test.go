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

// startOpenLanguageServer 啟動真正的 0c-server，回傳 socket；language 是啟動
// 預設（等同舊外殼沒有送 open.language 時的行為）。
func startOpenLanguageServer(t *testing.T, data, language string) string {
	t.Helper()
	binary := buildServer(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "0c.sock")
	cmd := exec.Command(binary, "-data", data, "-user", filepath.Join(dir, "user.json"),
		"-socket", sock, "-language", language)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})
	waitDial(t, sock)
	return sock
}

func dialOpenLanguage(t *testing.T, sock string) *ipc.Client {
	t.Helper()
	client, err := ipc.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func keyOpenLanguage(t *testing.T, client *ipc.Client, key string) proto.Snapshot {
	t.Helper()
	snap, err := client.Handle(proto.Request{Type: proto.Key, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// 同一個 server 可同時服務日文與注音 session：open.language 只影響那一條連線，
// 互不汙染；日文第一個按鍵是かな，注音照舊是注音。舊外殼不送 language 時
// 仍從啟動預設開始。
func TestOpenLanguageKeepsSessionsIndependent(t *testing.T) {
	sock := startOpenLanguageServer(t, jaServerData(t), "zh")
	jaClient := dialOpenLanguage(t, sock)
	zhClient := dialOpenLanguage(t, sock)
	legacyClient := dialOpenLanguage(t, sock)

	if err := jaClient.OpenLanguage("ja"); err != nil {
		t.Fatal(err)
	}
	if err := zhClient.OpenLanguage("zh"); err != nil {
		t.Fatal(err)
	}
	if err := legacyClient.Open(); err != nil {
		t.Fatal(err)
	}

	if got := jaClient.Info()["start_mode"]; got != proto.ModeJapanese {
		t.Fatalf("ja start_mode = %q", got)
	}
	if got := zhClient.Info()["start_mode"]; got != "zhuyin" {
		t.Fatalf("zh start_mode = %q", got)
	}
	if got := legacyClient.Info()["start_mode"]; got != "zhuyin" {
		t.Fatalf("legacy start_mode = %q", got)
	}
	for _, cap := range []string{proto.CapJapanese, proto.CapConvertKeys, proto.CapClauseDisplay} {
		if !jaClient.Capability(cap) {
			t.Errorf("日文 session 缺少能力 %s：%v", cap, jaClient.Info())
		}
	}

	// 交錯按鍵：兩邊各自保留自己的組字，日文是かな、注音是注音。
	ja := keyOpenLanguage(t, jaClient, "a")
	zh := keyOpenLanguage(t, zhClient, "a")
	legacy := keyOpenLanguage(t, legacyClient, "a")
	if ja.Language != "ja" || ja.Text() != "あ" {
		t.Fatalf("ja 第一個按鍵：language=%q text=%q", ja.Language, ja.Text())
	}
	if zh.Language != "" || zh.Text() != "ㄇ" {
		t.Fatalf("zh 第一個按鍵：language=%q text=%q", zh.Language, zh.Text())
	}
	if legacy.Language != "" || legacy.Text() != "ㄇ" {
		t.Fatalf("legacy 第一個按鍵：language=%q text=%q", legacy.Language, legacy.Text())
	}

	ja = keyOpenLanguage(t, jaClient, "i")
	zh = keyOpenLanguage(t, zhClient, "i")
	if ja.Language != "ja" || ja.Text() != "あい" {
		t.Fatalf("ja 第二個按鍵被其他 session 影響：language=%q text=%q", ja.Language, ja.Text())
	}
	if zh.Text() != "ㄇㄛ" {
		t.Fatalf("zh 第二個按鍵被日文 session 影響：text=%q", zh.Text())
	}
}

// 舊外殼不送 language 時，-language ja 的行為不變：直接開日文 session。
func TestOpenWithoutLanguageKeepsJapaneseStartupDefault(t *testing.T) {
	sock := startOpenLanguageServer(t, jaServerData(t), "ja")
	client := dialOpenLanguage(t, sock)
	if err := client.Open(); err != nil {
		t.Fatal(err)
	}
	if got := client.Info()["start_mode"]; got != proto.ModeJapanese {
		t.Fatalf("start_mode = %q", got)
	}
	snap := keyOpenLanguage(t, client, "a")
	if snap.Language != "ja" || snap.Text() != "あ" {
		t.Fatalf("legacy ja 第一個按鍵：language=%q text=%q", snap.Language, snap.Text())
	}
}

// 不認得的 language 照啟動預設，不報錯、不改其他 session。
func TestOpenUnknownLanguageUsesStartupDefault(t *testing.T) {
	sock := startOpenLanguageServer(t, jaServerData(t), "zh")
	client := dialOpenLanguage(t, sock)
	if err := client.OpenLanguage("fr"); err != nil {
		t.Fatal(err)
	}
	if got := client.Info()["start_mode"]; got != "zhuyin" {
		t.Fatalf("start_mode = %q", got)
	}
	snap := keyOpenLanguage(t, client, "a")
	if snap.Language != "" || snap.Text() != "ㄇ" {
		t.Fatalf("language=%q text=%q", snap.Language, snap.Text())
	}
}

// 要求日文但沒有日文資料包：open 仍成功，回退注音，不 crash、不回報日文能力。
func TestOpenJapaneseWithoutDataFallsBackToZhuyin(t *testing.T) {
	data, _ := filepath.Abs("../../testdata/toy")
	sock := startOpenLanguageServer(t, data, "zh")
	client := dialOpenLanguage(t, sock)
	if err := client.OpenLanguage("ja"); err != nil {
		t.Fatalf("open 不應失敗：%v", err)
	}
	if got := client.Info()["start_mode"]; got != "zhuyin" {
		t.Fatalf("start_mode = %q", got)
	}
	if client.Capability(proto.CapJapanese) {
		t.Fatalf("沒有日文資料仍回報 japanese：%v", client.Info())
	}
	snap := keyOpenLanguage(t, client, "a")
	if snap.Language != "" || snap.Text() != "ㄇ" {
		t.Fatalf("language=%q text=%q", snap.Language, snap.Text())
	}
}
