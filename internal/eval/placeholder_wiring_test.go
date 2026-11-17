package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/lattice"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/score"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/internal/neural"
)

// TestPlaceholderModelWiringOnly 只證明「特徵式仲裁可以接上模型」與
// 「placeholder 會被標成 untrained」，不產生任何品質數字。
//
// 真實的 int8 檔由 ValidateArtifact 核對大小、雜湊與 manifest 標記；推論端
// 用固定中性分數的替身，避免把未訓練模型的分數寫進 benchmark 或 baseline。
// 設定 OC_PLACEHOLDER_MODELS 指向 reference/models 後才執行。
func TestPlaceholderModelWiringOnly(t *testing.T) {
	root := os.Getenv("OC_PLACEHOLDER_MODELS")
	if root == "" {
		t.Skip("set OC_PLACEHOLDER_MODELS to the placeholder reference/models directory")
	}
	manifest := filepath.Join(root, "reranker-public-core", "manifest.json")
	artifact, err := neural.ValidateArtifact(manifest, root, "int8")
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Manifest.Placeholder || artifact.Manifest.Quality != "untrained" || artifact.Manifest.ProductionEligible {
		t.Fatalf("placeholder metadata not surfaced: %+v", artifact.Manifest)
	}
	if artifact.Bytes < 50<<20 {
		t.Fatalf("unexpected placeholder size %d", artifact.Bytes)
	}

	ranker := score.NewRanker(score.RankerConfig{
		Models:  []score.Scorer{neutralPlaceholderScorer{}},
		Timeout: time.Second,
	})
	seq := reading.Sequence{"あ"}
	candidates := []lattice.Candidate{
		{Segment: segment.Segment{Span: segment.Span{Start: 0, End: 1}, Reading: seq, Surface: "亜", Language: lang.Tag(lang.Ja)}, Source: lattice.SourceDictionary},
		{Segment: segment.Segment{Span: segment.Span{Start: 0, End: 1}, Reading: seq, Surface: "唖", Language: lang.Tag(lang.Ja)}, Source: lattice.SourceDictionary},
	}
	ranking, err := ranker.RankResult(context.Background(), score.RankRequest{
		Privacy:         score.PublicPrivacy(),
		PrimaryLanguage: lang.Ja,
		Candidates:      candidates,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ranking.Outcome != score.OutcomeModel {
		t.Fatalf("outcome = %v, want model", ranking.Outcome)
	}
	t.Logf("wiring only: placeholder=%v quality=%s sha256=%s; scores are neutral stubs and are not used for any quality claim",
		artifact.Manifest.Placeholder, artifact.Manifest.Quality, artifact.SHA256[:12])
}

// neutralPlaceholderScorer 是推論替身：只回傳中性分數，證明呼叫路徑。
type neutralPlaceholderScorer struct{}

func (neutralPlaceholderScorer) Languages() []lang.ID { return []lang.ID{lang.Ja} }

func (neutralPlaceholderScorer) Score(_ context.Context, req score.Request) ([]float64, error) {
	return make([]float64, len(req.Candidates)), nil
}
