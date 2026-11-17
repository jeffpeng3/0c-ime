package ipc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/proto"
)

// OpenSession 的 hook 在回覆 open 之前執行，宿主可以替新 session 註冊語言
// profile；回呼拿到的就是回覆裡的 session id 與 open 請求的語言。
func TestOnOpenSessionHookRunsBeforeReply(t *testing.T) {
	srv := &Server{Engine: toyEngine(t)}
	var got uint64
	var gotLanguage string
	srv.OnOpenSession = func(id uint64, language string) string {
		got, gotLanguage = id, language
		return language
	}
	sessions := map[uint64]bool{}
	reply := srv.do(Envelope{Op: OpOpen, Version: proto.Version, Language: "ja"}, sessions, new(bool))
	if !reply.OK || reply.Session == 0 {
		t.Fatalf("reply = %+v", reply)
	}
	if got != reply.Session {
		t.Fatalf("hook id = %d, reply session = %d", got, reply.Session)
	}
	if gotLanguage != "ja" {
		t.Fatalf("hook language = %q", gotLanguage)
	}
}

// 日文 session 要在 open 回覆說 start_mode=japanese，外殼才會把按鍵交給日文；
// 其他語言照 engine 的啟動模式，不覆蓋。
func TestOpenReplyStartModeFollowsAppliedLanguage(t *testing.T) {
	srv := &Server{Engine: toyEngine(t), Info: map[string]string{"start_mode": "zhuyin"}}
	srv.OnOpenSession = func(_ uint64, language string) string {
		if language == string(lang.Ja) {
			return string(lang.Ja)
		}
		return language
	}
	sessions := map[uint64]bool{}

	ja := srv.do(Envelope{Op: OpOpen, Version: proto.Version, Language: "ja"}, sessions, new(bool))
	if ja.Info["start_mode"] != proto.ModeJapanese {
		t.Fatalf("ja start_mode = %q", ja.Info["start_mode"])
	}
	zh := srv.do(Envelope{Op: OpOpen, Version: proto.Version, Language: "zh"}, sessions, new(bool))
	if zh.Info["start_mode"] != "zhuyin" {
		t.Fatalf("zh start_mode = %q", zh.Info["start_mode"])
	}
	legacy := srv.do(Envelope{Op: OpOpen, Version: proto.Version}, sessions, new(bool))
	if legacy.Info["start_mode"] != "zhuyin" {
		t.Fatalf("legacy start_mode = %q", legacy.Info["start_mode"])
	}
}

// 舊外殼不送 language 時，Envelope 重新編碼不應多出這個鍵。
func TestEnvelopeLanguageIsOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Envelope{Op: OpOpen, Version: proto.Version})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"language"`) {
		t.Fatalf("零值 language 必須省略：%s", b)
	}
}
