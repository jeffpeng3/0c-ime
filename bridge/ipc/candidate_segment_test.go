package ipc

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// P2-41：Windows 外殼在游標移動後送 candidates（Length 0）。這一條路真的經過
// bridge 時，候選範圍必須換成游標所在片段；詞庫長詞 match 是一項候選。
func TestCandidateOrderingAcrossBridgeFollowsTheCursorSegment(t *testing.T) {
	srv := &Server{Engine: toyEngine(t)}
	sessions := map[uint64]bool{}
	open := srv.do(Envelope{Op: OpOpen, Version: proto.Version}, sessions, new(bool))
	if !open.OK || open.Session == 0 {
		t.Fatalf("open = %+v", open)
	}
	handle := func(req proto.Request) *proto.Snapshot {
		t.Helper()
		reply := srv.do(Envelope{Op: OpHandle, Session: open.Session, Req: &req}, sessions, new(bool))
		if !reply.OK || reply.Snapshot == nil {
			t.Fatalf("handle %+v = %+v", req, reply)
		}
		return reply.Snapshot
	}
	for _, syl := range []string{"ㄨㄛˇ", "ㄈㄥ", "ㄐㄧㄥˇ"} {
		for _, key := range layout.Standard.Keys(zhuyin.Syllable(syl)) {
			handle(proto.Request{Type: proto.Key, Key: string(key)})
		}
	}

	if snap := handle(proto.Request{Type: proto.Cursor, To: 0}); snap.Text() != "我風景" {
		t.Fatalf("組字 = %q", snap.Text())
	}
	first := handle(proto.Request{Type: proto.Candidates})
	if first.Candidates == nil || first.Candidates.Start != 0 || first.Candidates.Length != 1 {
		t.Fatalf("游標在第一個片段：%+v", first.Candidates)
	}

	handle(proto.Request{Type: proto.Cursor, To: 1})
	long := handle(proto.Request{Type: proto.Candidates})
	if long.Candidates == nil || long.Candidates.Start != 1 || long.Candidates.Length != 2 {
		t.Fatalf("游標在長詞片段：%+v", long.Candidates)
	}
	if len(long.Candidates.Items) == 0 {
		t.Fatalf("沒有長詞候選：%+v", long.Candidates)
	}
	found := false
	for _, item := range long.Candidates.Items {
		if item.Text == "風景" {
			found = true
		}
	}
	if !found {
		t.Fatalf("長詞候選 = %+v", long.Candidates.Items)
	}
}
