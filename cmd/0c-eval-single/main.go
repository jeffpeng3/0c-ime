// 0c-eval-single 跑 P2-60 的單語言評測：凍結的注音 golden 題庫與日文
// 詞典歧義題庫。它不改產品排序、不訓練模型，只重現固定題庫上的指標。
//
// 子命令：
//
//	build-zh  從 testdata/golden/zh 重新產生注音題庫
//	build-ja  從 ja-lexicon-public-core.pack 取樣日文題庫
//	zh        跑注音題庫
//	ja        跑日文題庫（設定 OC_JADIC_REFERENCE_PACK）
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/internal/datadir"
	"github.com/0c-biggo/0c-ime/internal/eval"
)

const (
	defaultZhCorpus    = "testdata/eval/zhuyin-golden.json"
	defaultZhManifest  = "testdata/eval/zhuyin-golden.manifest.json"
	defaultZhData      = "testdata/golden/zh/data"
	defaultJACorpus    = "testdata/eval/ja-lexicon-ambiguous.json"
	defaultJAManifest  = "testdata/eval/ja-lexicon-ambiguous.manifest.json"
	defaultConnections = "data/ja/connection.tsv"
	defaultBaseline    = "testdata/eval/baseline.json"
	referencePackEnv   = "OC_JADIC_REFERENCE_PACK"
	referencePackName  = "ja-lexicon-public-core.pack"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "build-zh":
		buildZh(os.Args[2:])
	case "build-ja":
		buildJA(os.Args[2:])
	case "zh":
		runZh(os.Args[2:])
	case "ja":
		runJA(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `用法：0c-eval-single <子命令> [旗標]

  build-zh  從 testdata/golden/zh 產生注音題庫與 manifest
  build-ja  從參考 pack 產生日文題庫與 manifest
  zh        跑注音題庫
  ja        跑日文題庫

日文題庫需要 %s 指向 %s。
`, referencePackEnv, referencePackName)
	os.Exit(2)
}

func buildZh(args []string) {
	fs := flag.NewFlagSet("build-zh", flag.ExitOnError)
	golden := fs.String("golden", "testdata/golden/zh", "注音 golden 目錄")
	corpusPath := fs.String("corpus", defaultZhCorpus, "輸出的題庫")
	manifestPath := fs.String("manifest", defaultZhManifest, "輸出的 manifest")
	fs.Parse(args)

	corpus, err := eval.BuildZhGoldenCorpus(*golden)
	check(err)
	raw, err := marshal(corpus)
	check(err)
	check(os.MkdirAll(filepath.Dir(*corpusPath), 0755))
	check(os.WriteFile(*corpusPath, raw, 0644))
	manifest := eval.CorpusManifest{
		Schema:         eval.SingleBaselineSchema,
		Corpus:         corpus.Name,
		Path:           filepath.Base(*corpusPath),
		SHA256:         shaBytes(raw),
		Count:          corpus.Count,
		CasesSHA256:    corpus.SourcePin.CasesSHA256,
		ExpectedSHA256: corpus.SourcePin.ExpectedSHA256,
	}
	check(writeJSON(*manifestPath, manifest))
	fmt.Printf("wrote %s (%d items, sha256 %s)\n", *corpusPath, corpus.Count, manifest.SHA256[:12])
}

func buildJA(args []string) {
	fs := flag.NewFlagSet("build-ja", flag.ExitOnError)
	pack := fs.String("pack", os.Getenv(referencePackEnv), "參考 pack（預設讀 "+referencePackEnv+"）")
	packManifest := fs.String("pack-manifest", "", "pack 的 manifest.json（預設放在 pack 旁）")
	corpusPath := fs.String("corpus", defaultJACorpus, "輸出的題庫")
	manifestPath := fs.String("manifest", defaultJAManifest, "輸出的 manifest")
	opts := eval.DefaultJALexBuildOptions()
	fs.IntVar(&opts.Seed, "seed", opts.Seed, "取樣種子")
	fs.IntVar(&opts.SampleSize, "sample", opts.SampleSize, "取樣讀音數")
	fs.IntVar(&opts.HoldoutSize, "holdout", opts.HoldoutSize, "保留切片數")
	fs.IntVar(&opts.CandidateCap, "candidate-cap", opts.CandidateCap, "每個讀音保留的候選數上限")
	fs.IntVar(&opts.ReadingMin, "reading-min", opts.ReadingMin, "讀音長度下限（かな單位）")
	fs.IntVar(&opts.ReadingMax, "reading-max", opts.ReadingMax, "讀音長度上限（かな單位）")
	fs.Parse(args)

	if *pack == "" {
		fail("build-ja: 請設定 " + referencePackEnv + " 或 -pack")
	}
	if *packManifest == "" {
		candidate := filepath.Join(filepath.Dir(*pack), "manifest.json")
		if _, err := os.Stat(candidate); err == nil {
			*packManifest = candidate
		}
	}
	corpus, err := eval.BuildJALexCorpus(*pack, *packManifest, opts)
	check(err)
	raw, err := marshal(corpus)
	check(err)
	check(os.MkdirAll(filepath.Dir(*corpusPath), 0755))
	check(os.WriteFile(*corpusPath, raw, 0644))
	manifest := eval.CorpusManifest{
		Schema:     eval.SingleBaselineSchema,
		Corpus:     corpus.Name,
		Path:       filepath.Base(*corpusPath),
		SHA256:     shaBytes(raw),
		Count:      corpus.Count,
		Holdout:    corpus.HoldoutSize,
		Seed:       corpus.Seed,
		SampleSize: corpus.SampleSize,
		Pack:       &corpus.Pack,
	}
	check(writeJSON(*manifestPath, manifest))
	fmt.Printf("wrote %s (%d items, %d holdout, seed %d, pack sha256 %s)\n",
		*corpusPath, corpus.Count, corpus.HoldoutSize, corpus.Seed, corpus.Pack.SHA256[:12])
}

func runZh(args []string) {
	fs := flag.NewFlagSet("zh", flag.ExitOnError)
	corpusPath := fs.String("corpus", defaultZhCorpus, "注音題庫")
	data := fs.String("data", defaultZhData, "固定注音資料目錄")
	baselinePath := fs.String("baseline", defaultBaseline, "baseline 檔")
	out := fs.String("out", "", "完整 JSON 報告")
	writeBaseline := fs.Bool("write-baseline", false, "把量到的數字寫進 baseline（故意執行的更新）")
	gate := fs.Bool("gate", false, "與 baseline 比較；注音退步即失敗")
	fs.Parse(args)

	corpus, corpusSHA, err := eval.LoadZhGoldenCorpus(*corpusPath)
	check(err)
	eng, err := datadir.Open(*data)
	check(err)
	info := eval.ZhEngineInfo{DataDir: *data}
	info.LexiconSHA256, err = fileSHA(filepath.Join(*data, "lexicon.tsv"))
	check(err)
	info.LMSHA256, err = fileSHA(filepath.Join(*data, "lm.tsv"))
	check(err)

	report := eval.RunZhGolden(eng, corpus, corpusSHA, info)
	printZh(report)
	if *out != "" {
		check(writeJSON(*out, report))
	}
	if *writeBaseline {
		base := loadOrNewBaseline(*baselinePath)
		base.Zhuyin = &eval.ZhGoldenBaseline{
			CorpusSHA256:      corpusSHA,
			Engine:            info,
			SegmentTop1Rate:   report.Metrics.Segments.Top1Rate,
			SegmentMRR:        report.Metrics.Segments.MRR,
			Sentences:         report.Metrics.Sentences,
			SentenceExactRate: report.Metrics.SentenceExactRate,
			Gate: eval.SingleGate{
				Gated:                true,
				MaxTop1Drop:          0,
				MaxMRRDrop:           0,
				MaxSentenceExactDrop: 0,
				Note:                 "zhuyin must not regress at all",
			},
		}
		check(writeJSON(*baselinePath, base))
		fmt.Printf("baseline: wrote zhuyin section to %s\n", *baselinePath)
	}
	if *gate {
		base, err := eval.LoadSingleBaseline(*baselinePath)
		check(err)
		problems := eval.ZhGoldenProblems(report, base.Zhuyin)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "gate:", p)
			}
			os.Exit(1)
		}
		fmt.Println("gate: 注音沒有退步")
	}
}

func runJA(args []string) {
	fs := flag.NewFlagSet("ja", flag.ExitOnError)
	corpusPath := fs.String("corpus", defaultJACorpus, "日文題庫")
	pack := fs.String("pack", os.Getenv(referencePackEnv), "參考 pack（預設讀 "+referencePackEnv+"）")
	connections := fs.String("connections", defaultConnections, "固定的日文連接成本")
	topN := fs.Int("top-n", 8, "統計轉換保留的候選數")
	baselinePath := fs.String("baseline", defaultBaseline, "baseline 檔")
	out := fs.String("out", "", "完整 JSON 報告")
	writeBaseline := fs.Bool("write-baseline", false, "把量到的數字寫進 baseline（故意執行的更新）")
	gate := fs.Bool("gate", false, "與 baseline 比較；日文只警告，不因指標變動失敗")
	fs.Parse(args)

	corpus, corpusSHA, err := eval.LoadJALexCorpus(*corpusPath)
	check(err)
	var report eval.JALexReport
	if *pack == "" {
		fmt.Fprintf(os.Stderr, "ja: 未設定 %s；只跑 dictionary 基準，ranker 標記為 skipped\n", referencePackEnv)
		report, err = eval.RunJALex(nil, corpus, corpusSHA, *topN)
		check(err)
	} else {
		dec, packSHA, connSHA, err := eval.LoadJALexDecoder(*pack, *connections)
		check(err)
		if packSHA != corpus.Pack.SHA256 {
			fail(fmt.Sprintf("ja: pack sha256 %s does not match corpus pin %s", packSHA, corpus.Pack.SHA256))
		}
		report, err = eval.RunJALex(dec, corpus, corpusSHA, *topN)
		check(err)
		report.ConnectionsSHA256 = connSHA
	}
	printJA(report)
	if *out != "" {
		check(writeJSON(*out, report))
	}
	if *writeBaseline {
		split, ok := splitByName(report, "holdout")
		if !ok {
			fail("ja: holdout split is missing")
		}
		if split.RankerSkipped {
			fail("ja: refusing to write a baseline without the ranker arm; set " + referencePackEnv)
		}
		base := loadOrNewBaseline(*baselinePath)
		base.Japanese = &eval.JALexBaseline{
			CorpusSHA256:       corpusSHA,
			PackSHA256:         report.PackSHA256,
			ConnectionsSHA256:  report.ConnectionsSHA256,
			Split:              "holdout",
			N:                  split.N,
			DictionaryTop1Rate: split.Dictionary.Top1Rate,
			DictionaryMRR:      split.Dictionary.MRR,
			RankerTop1Rate:     split.Ranker.Top1Rate,
			RankerMRR:          split.Ranker.MRR,
			Gate: eval.SingleGate{
				Gated: false,
				Note:  "baseline number, not a target: references are dictionary preference, not human annotation",
			},
		}
		check(writeJSON(*baselinePath, base))
		fmt.Printf("baseline: wrote japanese section to %s\n", *baselinePath)
	}
	if *gate {
		base, err := eval.LoadSingleBaseline(*baselinePath)
		check(err)
		problems := eval.JALexProblems(report, base.Japanese)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "gate:", p)
			}
			os.Exit(1)
		}
		fmt.Println("gate: 日文題庫與來源雜湊一致（指標變動僅供參考）：" + eval.JALexDelta(report, base.Japanese))
	}
}

func printZh(rep eval.ZhGoldenReport) {
	m := rep.Metrics
	fmt.Printf("corpus %s  sha256 %s\n", rep.Corpus, rep.CorpusSHA256[:12])
	fmt.Printf("engine data=%s lexicon=%s lm=%s\n", rep.Engine.DataDir, rep.Engine.LexiconSHA256[:12], rep.Engine.LMSHA256[:12])
	fmt.Printf("segments  n=%d  top1=%d %.4f  mrr=%.4f  misses=%d\n",
		m.Segments.N, m.Segments.Top1, m.Segments.Top1Rate, m.Segments.MRR, m.Segments.Misses)
	fmt.Printf("sentences n=%d  exact=%d %.4f\n", m.Sentences, m.SentencesExact, m.SentenceExactRate)
}

func printJA(rep eval.JALexReport) {
	fmt.Printf("corpus %s  sha256 %s  pack %s\n", rep.Corpus, rep.CorpusSHA256[:12], rep.PackSHA256[:12])
	fmt.Printf("%-8s %5s %10s %8s %10s %8s %10s\n", "split", "n", "dict_top1", "dict_mrr", "rank_top1", "rank_mrr", "outcome")
	for _, s := range rep.Splits {
		outcome := "skipped"
		if !s.RankerSkipped {
			outcome = ""
			for name, n := range s.RankerOutcomes {
				outcome += fmt.Sprintf("%s=%d ", name, n)
			}
		}
		fmt.Printf("%-8s %5d %9.4f %8.4f %9.4f %8.4f %10s\n",
			s.Split, s.N, s.Dictionary.Top1Rate, s.Dictionary.MRR, s.Ranker.Top1Rate, s.Ranker.MRR, outcome)
	}
}

func splitByName(rep eval.JALexReport, name string) (eval.JALexSplitReport, bool) {
	for _, s := range rep.Splits {
		if s.Split == name {
			return s, true
		}
	}
	return eval.JALexSplitReport{}, false
}

func loadOrNewBaseline(path string) *eval.SingleBaseline {
	if _, err := os.Stat(path); err == nil {
		base, err := eval.LoadSingleBaseline(path)
		check(err)
		return base
	}
	return &eval.SingleBaseline{Schema: eval.SingleBaselineSchema}
}

func marshal(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func writeJSON(path string, v any) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func fileSHA(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return shaBytes(raw), nil
}

func shaBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "0c-eval-single:", msg)
	os.Exit(1)
}
