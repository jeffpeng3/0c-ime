// 0c-eval-ja measures Japanese kana-kanji conversion quality, bunsetsu
// boundaries, top-5 recall, latency, and timeout rate.
//
// The rule file is frozen before a run.  Report generation is local and
// deterministic; -gate only compares the deterministic quality metrics.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/0c-biggo/0c-ime/internal/eval"
)

func main() {
	data := flag.String("data", "data/ja", "日文資料目錄（含 lexicon/words.tsv 與 connection.tsv）")
	rulesPath := flag.String("rules", "testdata/ja/rules.json", "凍結的評測規則")
	baselinePath := flag.String("baseline", "testdata/ja/baseline.json", "baseline 數字")
	t03 := flag.String("t03-fixture", "scripts/spike/ja-zenz/fixtures/anthy_sample200.json", "T03 corpus.1 fixture，用來驗證題庫來源")
	out := flag.String("out", "", "把完整 JSON 報告寫到檔案")
	writeBaseline := flag.String("write-baseline", "", "把本次品質數字寫成 baseline JSON")
	gate := flag.Bool("gate", false, "與 baseline 比較，品質退步時回傳非零")
	determinism := flag.Bool("determinism", false, "連跑兩次並比較所有非延遲結果")
	quiet := flag.Bool("quiet", false, "不印摘要")
	flag.Parse()

	rules, rulesHash, err := eval.LoadJARules(*rulesPath)
	check(err)
	corpora, err := eval.LoadJACorpusSet(rules, *rulesPath)
	check(err)
	check(eval.CheckJACorpusProvenance(corpora, rules, *t03))

	dec, engine, err := eval.LoadJADecoder(*data)
	check(err)
	opts := eval.JAOptions{
		TopN:      rules.TopN,
		Timeout:   time.Duration(rules.TimeoutMS) * time.Millisecond,
		TimeoutMS: rules.TimeoutMS,
	}
	report := eval.RunJA(dec, engine, rules, rulesHash, corpora, opts)

	if *determinism {
		second := eval.RunJA(dec, engine, rules, rulesHash, corpora, opts)
		firstSig, err := eval.DeterminismSignature(report)
		check(err)
		secondSig, err := eval.DeterminismSignature(second)
		check(err)
		if firstSig != secondSig {
			fail("determinism check failed: two runs differ on non-latency output")
		}
	}

	if !*quiet {
		printSummary(report)
	}
	if *out != "" {
		writeJSON(*out, report)
	}
	if *writeBaseline != "" {
		base := eval.JABaseline{
			Schema:      eval.JASchema,
			RulesSHA256: rulesHash,
			Engine:      engine,
			MeasuredOn:  time.Now().Format("2006-01-02"),
			Corpora:     eval.BaselineFor(report),
		}
		writeJSON(*writeBaseline, base)
	}
	if *gate {
		base, err := eval.LoadJABaseline(*baselinePath)
		check(err)
		problems := eval.JARegression(report, base, rulesHash, rules.Gate)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "gate:", p)
			}
			os.Exit(1)
		}
		if !*quiet {
			fmt.Println("gate: 沒有品質退步")
		}
	}
}

func printSummary(r eval.JAReport) {
	fmt.Printf("engine %s  data %s\n", r.Engine.Name, r.Engine.Data)
	fmt.Printf("rules  %s  %s\n", r.Rules.Version, r.Rules.SHA256[:12])
	fmt.Printf("%-14s %5s %10s %10s %12s %8s %9s %8s %8s\n",
		"corpus", "n", "top1", "top5", "boundary", "F1", "P50ms", "P95ms", "timeout")
	for _, c := range r.Corpora {
		m := c.Metrics
		fmt.Printf("%-14s %5d %4d %5.1f%% %4d %5.1f%% %4d %5.1f%% %8.3f %8.3f %8.3f %7.1f%%\n",
			c.Name, m.N,
			m.Top1, m.Top1Rate*100,
			m.Top5, m.Top5Rate*100,
			m.BoundaryExact, m.BoundaryExactRate*100,
			m.BoundaryF1,
			m.LatencyP50MS, m.LatencyP95MS,
			m.TimeoutRate*100)
	}
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	check(err)
	check(os.WriteFile(path, append(b, '\n'), 0644))
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "0c-eval-ja:", msg)
	os.Exit(1)
}
