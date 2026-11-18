package eval

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/segment"
)

const (
	mixedBenchDir          = "../../testdata/mixed"
	mixedBenchManifestPath = "../../testdata/mixed/bench.manifest.json"
	mixedZhDataDir         = "../../testdata/golden/zh/data"
	mixedConnectionsPath   = "../../data/ja/connection.tsv"
	mixedReferencePackEnv  = "OC_JADIC_REFERENCE_PACK"
)

func loadMixedBenchManifest(t *testing.T) *MixedBenchManifest {
	t.Helper()
	m, err := LoadMixedBenchManifest(mixedBenchManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// pinnedPack 回傳日文單語言題庫已凍結的 pack identity。混合 benchmark 的
// manifest 不需要參考 pack 實際存在也能記錄同一個 SHA-256。
func pinnedPack(t *testing.T) *MixedPackPin {
	t.Helper()
	corpus, _, err := LoadJALexCorpus(jaLexCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	return &MixedPackPin{Path: corpus.Pack.Path, SHA256: corpus.Pack.SHA256}
}

func pinnedData(t *testing.T) *MixedDataPin {
	t.Helper()
	return &MixedDataPin{
		Dir:           "testdata/golden/zh/data",
		LexiconSHA256: testFileSHA(t, filepath.Join(mixedZhDataDir, "lexicon.tsv")),
		LMSHA256:      testFileSHA(t, filepath.Join(mixedZhDataDir, "lm.tsv")),
	}
}

func testFileSHA(t *testing.T, path string) string {
	t.Helper()
	sha, err := optionalFileSHA(path)
	if err != nil || sha == "" {
		t.Fatalf("%s: %v", path, err)
	}
	return sha
}

func TestMixedBenchManifestPinned(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := loadMixedBenchManifest(t)
	if manifest.Seed != MixedBenchSeed {
		t.Fatalf("seed = %d，want %d", manifest.Seed, MixedBenchSeed)
	}
	if len(manifest.Fixtures) != len(entries) || len(entries) != 3 {
		t.Fatalf("fixtures = %d，題庫 = %d，want 3", len(manifest.Fixtures), len(entries))
	}
	pins := map[string]MixedFixturePin{}
	for _, f := range manifest.Fixtures {
		pins[f.File] = f
	}
	for _, entry := range entries {
		file := filepath.Base(entry.Path)
		pin, ok := pins[file]
		if !ok {
			t.Fatalf("%s 沒有 manifest pin", file)
		}
		if pin.SHA256 != entry.SHA256 {
			t.Errorf("%s: pin %s != %s", file, pin.SHA256, entry.SHA256)
		}
		if pin.Name != entry.Corpus.Name || pin.Scenario != entry.Corpus.Scenario || pin.Count != len(entry.Corpus.Cases) {
			t.Errorf("%s: pin %+v 與題庫不符", file, pin)
		}
	}
	if manifest.Pack == nil || manifest.Pack.SHA256 != pinnedPack(t).SHA256 {
		t.Fatalf("manifest pack = %+v，want 日文題庫的 pack pin", manifest.Pack)
	}
	data := pinnedData(t)
	if manifest.Data == nil || manifest.Data.LexiconSHA256 != data.LexiconSHA256 || manifest.Data.LMSHA256 != data.LMSHA256 {
		t.Fatalf("manifest data = %+v，want %+v", manifest.Data, data)
	}
	if manifest.Models.Used || manifest.Models.Note == "" {
		t.Fatalf("models pin = %+v，want used=false 且有 wiring-only 說明", manifest.Models)
	}
	if !strings.Contains(manifest.Quality, "not a release gate") {
		t.Fatalf("quality = %q", manifest.Quality)
	}
}

// 題庫或 pack 換了就要故意重跑 build；manifest 必須能被同一組輸入逐位元重建。
func TestMixedBenchManifestRebuildIsFrozen(t *testing.T) {
	first, err := BuildMixedBenchManifest(mixedBenchDir, MixedBenchSeed, pinnedPack(t), pinnedData(t), MixedModelPin{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildMixedBenchManifest(mixedBenchDir, MixedBenchSeed, pinnedPack(t), pinnedData(t), MixedModelPin{})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON := encodeJSON(t, first)
	secondJSON := encodeJSON(t, second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("同一份題庫與 seed 重建 manifest 得到不同結果")
	}
	committed, err := os.ReadFile(mixedBenchManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, committed) {
		t.Fatalf("%s 已過期；重跑 `go run ./cmd/0c-eval-mixed build`", mixedBenchManifestPath)
	}
}

func TestMixedSeedOrderStable(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		first := MixedSeedOrder(MixedBenchSeed, entry.Corpus.Cases)
		second := MixedSeedOrder(MixedBenchSeed, entry.Corpus.Cases)
		ids := func(cases []MixedCase) string {
			var b strings.Builder
			for _, c := range cases {
				b.WriteString(c.ID)
				b.WriteByte('\n')
			}
			return b.String()
		}
		if ids(first) != ids(second) {
			t.Fatalf("%s: 同一 seed 的順序不同", entry.Corpus.Name)
		}
		if len(first) != len(entry.Corpus.Cases) {
			t.Fatalf("%s: 排序後案例數改變", entry.Corpus.Name)
		}
		seen := map[string]bool{}
		for _, c := range first {
			if seen[c.ID] {
				t.Fatalf("%s: 排序後 %q 重複", entry.Corpus.Name, c.ID)
			}
			seen[c.ID] = true
		}
	}
}

func TestMixedLoaderSkipsManifest(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Path, ".manifest.json") {
			t.Fatalf("manifest 被當成題庫載入：%s", entry.Path)
		}
	}
}

func TestMixedInputKeys(t *testing.T) {
	zh := MixedInputSegment{Script: segment.ScriptZhuyin, Text: "ㄏㄠˇ", Reading: reading.Sequence{"ㄏㄠˇ"}}
	keys, err := MixedInputKeys(zh)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 || strings.Join(keys, "") == "ㄏㄠˇ" {
		t.Fatalf("注音按鍵 = %#v，want 標準排列按鍵", keys)
	}
	kana := MixedInputSegment{Script: segment.ScriptKana, Text: "すし", Reading: reading.Sequence{"す", "し"}}
	if _, err := MixedInputKeys(kana); err == nil {
		t.Fatal("かな沒有 keys 應回傳錯誤")
	}
	kana.Keys = "susi"
	keys, err = MixedInputKeys(kana)
	if err != nil || strings.Join(keys, "") != "susi" {
		t.Fatalf("かな keys = %#v, %v", keys, err)
	}
	latin := MixedInputSegment{Script: segment.ScriptLatin, Text: "API"}
	keys, err = MixedInputKeys(latin)
	if err != nil || strings.Join(keys, "") != "API" {
		t.Fatalf("Latin keys = %#v, %v", keys, err)
	}
}

func TestMixedSessionRunnerNoPack(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := loadMixedBenchManifest(t)
	runner, info, err := LoadMixedSessionRunner(mixedZhDataDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.JapanesePack != nil {
		t.Fatal("沒有 pack 時不應記錄 JapanesePack")
	}
	report, err := RunMixedBench(entries, manifest, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	assertSmokeHealthy(t, report)
	if report.Totals.Smoke.MergedSegments != 2 {
		t.Errorf("merged = %d，want 2（每個長詞合併案例一次）", report.Totals.Smoke.MergedSegments)
	}
	if report.Totals.Smoke.MissingSegments != 2 {
		t.Errorf("missing = %d，want 2", report.Totals.Smoke.MissingSegments)
	}
	if report.Totals.Smoke.InvalidSegments != 0 || report.Totals.Smoke.UnorderedDirectKeys != 0 {
		t.Errorf("沒有 pack 的執行不應有結構錯誤：%+v", report.Totals.Smoke)
	}
	if report.WiringOnly {
		t.Error("沒有模型參與時 wiring_only 必須是 false")
	}
	if len(report.Notes) == 0 || !strings.Contains(strings.Join(report.Notes, " "), "pack 未載入") {
		t.Errorf("notes 沒有標記 kana-only：%v", report.Notes)
	}
	problems := MixedBenchProblems(entries, manifest, testFileSHA(t, mixedBenchManifestPath), report)
	if len(problems) > 0 {
		t.Fatalf("manifest problems: %v", problems)
	}

	// 同一份題庫與 runner 連跑兩次必須逐位元相同。
	second, err := RunMixedBench(entries, manifest, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	firstSig, err := MixedBenchDeterminismSignature(report)
	if err != nil {
		t.Fatal(err)
	}
	secondSig, err := MixedBenchDeterminismSignature(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstSig != secondSig {
		t.Fatal("兩次 no-pack 執行結果不同")
	}
	t.Log("\n" + FormatMixedBenchTable(report))
}

// 參考 pack 是 repo 外的真實詞典；設定 OC_JADIC_REFERENCE_PACK 才執行。
// 這些數字是 lexicon+統計轉換的 baseline，沒有模型參與。
func TestMixedSessionRunnerWithReferencePack(t *testing.T) {
	pack := os.Getenv(mixedReferencePackEnv)
	if pack == "" {
		t.Skipf("未設定 %s，跳過真實 pack 的混合執行", mixedReferencePackEnv)
	}
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := loadMixedBenchManifest(t)
	runner, info, err := LoadMixedSessionRunner(mixedZhDataDir, pack, mixedConnectionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.JapanesePack == nil || info.JapanesePack.SHA256 != manifest.Pack.SHA256 {
		t.Fatalf("pack = %+v，manifest = %+v", info.JapanesePack, manifest.Pack)
	}
	report, err := RunMixedBench(entries, manifest, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	assertSmokeHealthy(t, report)
	if report.Totals.Smoke.MergedSegments != 2 || report.Totals.Smoke.MissingSegments != 2 {
		t.Errorf("長詞合併觀察 = merged %d missing %d，want 2/2",
			report.Totals.Smoke.MergedSegments, report.Totals.Smoke.MissingSegments)
	}
	if problems := MixedBenchProblems(entries, manifest, testFileSHA(t, mixedBenchManifestPath), report); len(problems) > 0 {
		t.Fatalf("manifest problems: %v", problems)
	}
	if report.WiringOnly || report.Models.Used {
		t.Error("參考 pack 是真實詞典，但沒有模型參與；不得標成 wiring-only")
	}

	second, err := RunMixedBench(entries, manifest, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	firstSig, err := MixedBenchDeterminismSignature(report)
	if err != nil {
		t.Fatal(err)
	}
	secondSig, err := MixedBenchDeterminismSignature(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstSig != secondSig {
		t.Fatal("兩次 pack 執行結果不同")
	}
	t.Log("\n" + FormatMixedBenchTable(report))
}

func TestMixedSessionRunnerMergedSegments(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	var merged MixedCase
	for _, entry := range entries {
		for _, cs := range entry.Corpus.Cases {
			if cs.ID == "b-noisy-long-match" {
				merged = cs
			}
		}
	}
	if merged.ID == "" {
		t.Fatal("缺少 b-noisy-long-match")
	}
	runner, _, err := LoadMixedSessionRunner(mixedZhDataDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	pred, err := runner.Predict(merged)
	if err != nil {
		t.Fatal(err)
	}
	if pred.MergedSegments != 1 {
		t.Fatalf("merged = %d，want 1", pred.MergedSegments)
	}
	if len(pred.Segments) != 1 || pred.Segments[0].InputIndex != 0 || pred.Segments[0].Surface != "麻婆豆腐" {
		t.Fatalf("segments = %+v，want 只有合併後的麻婆豆腐", pred.Segments)
	}
	if pred.Text != "麻婆豆腐" {
		t.Fatalf("text = %q", pred.Text)
	}
}

func TestMixedBenchWiringOnlyLabel(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := loadMixedBenchManifest(t)
	clone := *manifest
	clone.Models = MixedModelPin{Used: true, Note: "placeholder model wiring-only"}
	runner, info, err := LoadMixedSessionRunner(mixedZhDataDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunMixedBench(entries, &clone, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !report.WiringOnly {
		t.Fatal("models.used=true 時報告必須是 wiring-only")
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "wiring-only") {
		t.Fatalf("notes 沒有 wiring-only：%v", report.Notes)
	}
	if !strings.Contains(FormatMixedBenchTable(report), "wiring_only=true") {
		t.Fatal("表格沒有標記 wiring_only=true")
	}
	// 把報告的 wiring_only 改掉必須被 manifest 檢查抓到。
	report.WiringOnly = false
	if problems := MixedBenchProblems(entries, &clone, testFileSHA(t, mixedBenchManifestPath), report); len(problems) == 0 {
		t.Fatal("wiring_only 與 models.used 不一致時應回報問題")
	}
}

func TestMixedBenchProblems(t *testing.T) {
	entries, err := LoadMixedCorpusDir(mixedBenchDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := loadMixedBenchManifest(t)
	runner, info, err := LoadMixedSessionRunner(mixedZhDataDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunMixedBench(entries, manifest, testFileSHA(t, mixedBenchManifestPath), info, runner)
	if err != nil {
		t.Fatal(err)
	}
	if problems := MixedBenchProblems(entries, manifest, testFileSHA(t, mixedBenchManifestPath), report); len(problems) > 0 {
		t.Fatalf("乾淨的 manifest 不應有問題：%v", problems)
	}

	bad := *manifest
	bad.Fixtures = append([]MixedFixturePin(nil), manifest.Fixtures...)
	bad.Fixtures[0].SHA256 = strings.Repeat("0", 64)
	if problems := MixedBenchProblems(entries, &bad, testFileSHA(t, mixedBenchManifestPath), report); len(problems) == 0 {
		t.Fatal("fixture hash 不符應回報問題")
	}

	badOrder := *manifest
	badOrder.CaseOrderSHA256 = strings.Repeat("2", 64)
	if problems := MixedBenchProblems(entries, &badOrder, testFileSHA(t, mixedBenchManifestPath), report); len(problems) == 0 {
		t.Fatal("case_order_sha256 不符應回報問題")
	}

	withPack := report
	withPack.Engine.JapanesePack = &MixedPackInfo{SHA256: strings.Repeat("1", 64)}
	if problems := MixedBenchProblems(entries, manifest, testFileSHA(t, mixedBenchManifestPath), withPack); len(problems) == 0 {
		t.Fatal("pack hash 不符應回報問題")
	}
}

func assertSmokeHealthy(t *testing.T, rep MixedBenchReport) {
	t.Helper()
	if rep.Totals.Cases != 13 {
		t.Fatalf("cases = %d，want 13", rep.Totals.Cases)
	}
	if rep.Totals.Smoke.FailedRuns != 0 || rep.Totals.Smoke.Panics != 0 {
		t.Fatalf("smoke 有失敗：%+v", rep.Totals.Smoke)
	}
	if rep.Totals.Smoke.Runs != rep.Totals.Cases {
		t.Fatalf("runs = %d，cases = %d", rep.Totals.Smoke.Runs, rep.Totals.Cases)
	}
	for _, c := range rep.Corpora {
		for _, cs := range c.Cases {
			if cs.Error != "" {
				t.Errorf("%s: %s", cs.ID, cs.Error)
			}
			if !cs.Smoke.Run || !cs.Smoke.FinalTextNonEmpty {
				t.Errorf("%s: smoke = %+v", cs.ID, cs.Smoke)
			}
		}
	}
	// clean 與雜訊 tag 都要有案例，否則 manifest 的固定種子題庫失去分組意義。
	tags := map[string]int{}
	for _, tag := range rep.ByTag {
		tags[tag.Tag] = tag.Cases
	}
	for _, want := range []string{MixedTidyTag, "noisy:no-space", "noisy:mixed-case", "noisy:digits", "noisy:long-match"} {
		if tags[want] == 0 {
			t.Errorf("缺少 tag %q 的案例", want)
		}
	}
}
