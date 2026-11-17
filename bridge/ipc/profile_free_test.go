package ipc

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// P2-40：引擎 API profile-free。open 只有協定版本，沒有語言參數；同一個 session
// 直接接受注音與拉丁字母。Envelope 沒有 language 欄位，所以這裡的 open 就是外殼
// 實際送出的形狀，沒有相容 shim。
func TestOpenAndDriveWithoutLanguage(t *testing.T) {
	e := toyEngine(t)
	e.MixedInput = true // Mixed profile：英文直接打，不切模式
	srv := &Server{Engine: e}
	sessions := map[uint64]bool{}
	reply := srv.do(Envelope{Op: OpOpen, Version: proto.Version}, sessions, new(bool))
	if !reply.OK || reply.Session == 0 {
		t.Fatalf("open = %+v", reply)
	}
	// 「hello」是英文、「t␣」是注音 ㄔ 的一聲：同一個 session、沒有語言切換。
	for _, k := range "hellot " {
		r := srv.do(Envelope{Op: OpHandle, Session: reply.Session, Req: &proto.Request{Type: proto.Key, Key: string(k)}}, sessions, new(bool))
		if !r.OK {
			t.Fatalf("key %q: %s", k, r.Error)
		}
	}
	r := srv.do(Envelope{Op: OpHandle, Session: reply.Session, Req: &proto.Request{Type: proto.Commit}}, sessions, new(bool))
	if r.Snapshot == nil || r.Snapshot.Commit != "hello吃" {
		t.Fatalf("commit = %+v", r.Snapshot)
	}
}
