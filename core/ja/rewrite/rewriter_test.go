package rewrite_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/ja/rewrite"
	"github.com/0c-biggo/0c-ime/core/lang"
)

func TestAppendKeepsPrimaryFirst(t *testing.T) {
	primary := []lang.Candidate{
		{Surface: "今日", Score: 100, Source: lang.SourceConvert},
		{Surface: "きょう", Score: 20, Source: lang.SourceConvert},
	}
	extra := []lang.Candidate{
		{Surface: "キョウ", Score: 999, Annotation: rewrite.AnnotationKatakana},
		{Surface: "今日", Score: 999, Annotation: "duplicate"},
		{Surface: "ｷｮｳ", Score: 999, Annotation: rewrite.AnnotationHalfwidthKana},
	}

	got := rewrite.Append(primary, extra)
	if len(got) != 4 {
		t.Fatalf("got %d candidates, want 4: %#v", len(got), got)
	}
	if got[0].Surface != "今日" || got[0].Score != 100 {
		t.Errorf("primary[0] = %#v", got[0])
	}
	if got[1].Surface != "きょう" || got[1].Score != 20 {
		t.Errorf("primary[1] = %#v", got[1])
	}
	if got[2].Surface != "キョウ" || got[2].Score >= 20 {
		t.Errorf("extra[0] = %#v, want score below 20", got[2])
	}
	if got[3].Surface != "ｷｮｳ" || got[3].Score >= got[2].Score {
		t.Errorf("extra[1] = %#v, want score below previous", got[3])
	}
	if primary[0].Score != 100 || primary[1].Score != 20 {
		t.Error("Append modified primary candidates")
	}
}

func TestAppendWithoutPrimary(t *testing.T) {
	got := rewrite.Append(nil, []lang.Candidate{
		{Surface: "ア", Score: 0},
		{Surface: "", Score: 0},
		{Surface: "イ", Score: 0},
	})
	if len(got) != 2 || got[0].Surface != "ア" || got[1].Surface != "イ" {
		t.Fatalf("got %#v", got)
	}
	if !(got[0].Score > got[1].Score) {
		t.Errorf("scores %v, %v should decrease", got[0].Score, got[1].Score)
	}
}

func TestCandidatesOrderAndSource(t *testing.T) {
	r := rewrite.New()
	cands := r.Candidates("abc123", units("えーびーしー"))
	got := surfaces(cands)
	if len(got) < 3 {
		t.Fatalf("got too few candidates: %#v", got)
	}
	if got[0] != "えーびーしー" || got[1] != "エービーシー" || got[2] != "ｴｰﾋﾞｰｼｰ" {
		t.Errorf("kana candidates not first: %#v", got)
	}
	lastKana := 2
	for i, c := range cands {
		if i <= lastKana {
			if c.Source != rewrite.SourceRewrite {
				t.Errorf("candidate %q source = %q", c.Surface, c.Source)
			}
			continue
		}
		if c.Annotation == rewrite.AnnotationFullwidthLatin {
			if i != lastKana+1 {
				t.Errorf("fullwidth latin at %d, want %d", i, lastKana+1)
			}
			break
		}
	}
}

func TestCandidatesReadsReadingAndCopies(t *testing.T) {
	seq := units("きょう")
	cands := rewrite.New().Candidates("今日", seq)
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	seq[0] = "あ"
	for _, c := range cands {
		if c.Reading[0] != "き" {
			t.Fatalf("candidate %q reading shares input slice: %#v", c.Surface, c.Reading)
		}
	}
}

func TestMaxCandidates(t *testing.T) {
	r := &rewrite.Rewriter{MaxCandidates: 2}
	cands := r.Candidates("abc123", units("えーびーしー"))
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2: %#v", len(cands), surfaces(cands))
	}

	r.MaxCandidates = 0
	cands = r.Candidates("abc123", units("えーびーしー"))
	if len(cands) <= 2 {
		t.Fatalf("default limit returned %d candidates, want more than 2", len(cands))
	}
}

func TestNilSymbols(t *testing.T) {
	var r rewrite.Rewriter
	cands := r.Candidates("記号", units("きごう"))
	if len(cands) != 3 {
		t.Fatalf("got %d candidates, want only kana: %#v", len(cands), surfaces(cands))
	}
}
