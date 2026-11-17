package session

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/proto"
)

// P2-41：Windows 外殼在游標移動後會為新的片段重開候選。引擎這邊必須回傳
// 游標所在片段的範圍；詞庫長詞 match 是一項候選，即使它蓋過多個輸入片段。
func TestCandidateListFollowsTheActiveSegment(t *testing.T) {
	s := cursorSession()
	for _, syl := range []string{"ㄨㄛˇ", "ㄈㄥ", "ㄐㄧㄥˇ"} {
		typeSyl(s, syl)
	}
	if segs := s.Segments(); len(segs) != 2 || segs[0].Surface != "我" || segs[1].Surface != "風景" {
		t.Fatalf("fixture 片段 = %#v", segs)
	}

	// 游標移到「我」：候選重新錨定在第一個片段。
	s.Handle(proto.Request{Type: proto.Cursor, To: 0})
	if got := s.ActiveSegment(); got != 0 {
		t.Fatalf("游標在開頭時 ActiveSegment = %d", got)
	}
	first := s.Handle(proto.Request{Type: proto.Candidates})
	cursorRange(t, first, 0, 1)

	// 游標移到長詞「風景」：同一張清單換成第二個片段，長詞仍是一項。
	s.Handle(proto.Request{Type: proto.Cursor, To: 1})
	if got := s.ActiveSegment(); got != 1 {
		t.Fatalf("游標在長詞時 ActiveSegment = %d", got)
	}
	second := s.Handle(proto.Request{Type: proto.Candidates})
	cursorRange(t, second, 1, 2)
	longID, seen := 0, 0
	for _, item := range second.Candidates.Items {
		if item.Text == "風景" {
			longID, seen = item.ID, seen+1
		}
	}
	if seen != 1 {
		t.Fatalf("長詞 match 只能是一項候選：%+v", second.Candidates.Items)
	}

	// 選字鎖定目前片段，不影響前面的片段。
	if !s.selectCandidate(longID) {
		t.Fatal("選取長詞候選失敗")
	}
	if !s.SegmentLocked(1) || s.SegmentLocked(0) {
		t.Fatalf("選字後鎖定 = %v/%v", s.SegmentLocked(0), s.SegmentLocked(1))
	}
}
