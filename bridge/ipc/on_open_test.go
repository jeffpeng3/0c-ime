package ipc

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// OpenSession 的 hook 在回覆 open 之前執行，宿主可以替新 session 註冊語言
// profile；回呼拿到的就是回覆裡的 session id。
func TestOnOpenSessionHookRunsBeforeReply(t *testing.T) {
	srv := &Server{Engine: toyEngine(t)}
	var got uint64
	srv.OnOpenSession = func(id uint64) { got = id }
	sessions := map[uint64]bool{}
	reply := srv.do(Envelope{Op: OpOpen, Version: proto.Version}, sessions, new(bool))
	if !reply.OK || reply.Session == 0 {
		t.Fatalf("reply = %+v", reply)
	}
	if got != reply.Session {
		t.Fatalf("hook id = %d, reply session = %d", got, reply.Session)
	}
}
