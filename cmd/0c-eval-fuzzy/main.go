// 0c-eval-fuzzy 跑 P2-62 的模糊輸入評測：錯鍵、漏鍵、重複鍵與注音近音。
//
// 子命令：
//
//	build  從凍結的注音 golden 與日文單語言題庫重建模糊輸入題庫與 manifest
//	run    用真正的產品 session 打題庫，印出 raw 與回退統計兩個角色的 top-1/top-5
//
// verify 不是必要步驟；run 會先比對 manifest 與題庫雜湊，不會默默跑別份資料。
// 日文 top-1/top-5 需要 OC_JADIC_REFERENCE_PACK；沒有 pack 時日文案例仍留在
// 分母，報告把 no_decoder 記成降級原因，不把它們丟掉。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/eval"
)

const (
	defaultCorpus      = "testdata/fuzzy/fuzzy-input.json"
	defaultManifest    = "testdata/fuzzy/fuzzy-input.manifest.json"
	defaultZhCorpus    = "testdata/eval/zhuyin-golden.json"
	defaultJACorpus    = "testdata/eval/ja-lexicon-ambiguous.json"
	defaultData        = "testdata/golden/zh/data"
	defaultConnections = "data/ja/connection.tsv"
	referencePackEnv   = "OC_JADIC_REFERENCE_PACK"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "build":
		build(os.Args[2:])
	case "run":
		run(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `用法：0c-eval-fuzzy <子命令> [旗標]

  build  重建 testdata/fuzzy/fuzzy-input.json 與 manifest
  run    跑模糊輸入題庫並印出 raw／fallback 指標

run 的日文漢字候選需要 %s 指向 ja-lexicon-public-core.pack。
沒有設定時日文案例仍在分母，但候選指標會標成 no_decoder，不得引用為品質數字。
`, referencePackEnv)
	os.Exit(2)
}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	zh := fs.String("zh", defaultZhCorpus, "注音 golden 題庫")
	ja := fs.String("ja", defaultJACorpus, "日文單語言題庫")
	out := fs.String("out", defaultCorpus, "輸出的模糊輸入題庫")
	manifest := fs.String("manifest", defaultManifest, "輸出的 manifest")
	seed := fs.Int("seed", eval.FuzzySeed, "固定種子")
	jaSample := fs.Int("ja-sample", eval.FuzzyJASample, "日文保留切片取樣數")
	fs.Parse(args)

	corpus, err := eval.BuildFuzzyCorpus(*zh, *ja, eval.FuzzyBuildOptions{Seed: *seed, JASample: *jaSample})
	check(err)
	raw, err := marshal(corpus)
	check(err)
	check(os.MkdirAll(filepath.Dir(*out), 0755))
	check(os.WriteFile(*out, raw, 0644))
	m := eval.FuzzyManifestFor(corpus, filepath.Base(*out), raw)
	check(writeJSON(*manifest, m))
	fmt.Printf("wrote %s (%d cases, seed %d, sha256 %s)\n", *out, corpus.Count, corpus.Seed, m.SHA256[:12])
	fmt.Printf("wrote %s (kinds %v languages %v)\n", *manifest, m.Kinds, m.Languages)
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	corpusPath := fs.String("corpus", defaultCorpus, "模糊輸入題庫")
	manifestPath := fs.String("manifest", defaultManifest, "題庫 manifest")
	data := fs.String("data", defaultData, "注音資料目錄")
	pack := fs.String("pack", os.Getenv(referencePackEnv), "日文參考 pack（預設讀 "+referencePackEnv+"）")
	connections := fs.String("connections", defaultConnections, "日文連接成本")
	languages := fs.String("languages", "zh,ja", "要跑的語言（逗號分隔）")
	kinds := fs.String("kinds", strings.Join(eval.FuzzyKinds(), ","), "要跑的擾動種類（逗號分隔）")
	limit := fs.Int("limit", 0, "只跑前 N 題（0 表示全部；測試用）")
	top := fs.Int("top", 8, "日文 N-best 候選數")
	out := fs.String("out", "", "完整 JSON 報告")
	twice := fs.Bool("twice", false, "連跑兩次並比對可重現性")
	fs.Parse(args)

	corpus, corpusSHA, err := eval.LoadFuzzyCorpus(*corpusPath)
	check(err)
	manifest, err := eval.LoadFuzzyManifest(*manifestPath)
	check(err)
	if problems := eval.FuzzyCorpusProblems(corpus, corpusSHA, manifest); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "manifest:", p)
		}
		os.Exit(1)
	}
	filtered := filterCorpus(corpus, splitList(*languages), splitList(*kinds), *limit)
	env, info, err := eval.LoadFuzzyEnv(*data, *pack, *connections)
	check(err)
	if info.JapanesePack == nil {
		fmt.Fprintln(os.Stderr, "warning: 沒有日文參考 pack；日文候選指標不可用，但日文案例仍留在分母（no_decoder）")
	}
	report, err := eval.RunFuzzyCorpus(env, filtered, corpusSHA, eval.FuzzyRunOptions{TopN: *top})
	check(err)
	if *twice {
		second, err := eval.RunFuzzyCorpus(env, filtered, corpusSHA, eval.FuzzyRunOptions{TopN: *top})
		check(err)
		a, err := eval.FuzzyDeterminismSignature(report)
		check(err)
		b, err := eval.FuzzyDeterminismSignature(second)
		check(err)
		if a != b {
			fail("同一份題庫與 runner 連跑兩次結果不同")
		}
		fmt.Println("determinism: two runs are byte-identical")
	}
	printReport(report)
	if *out != "" {
		check(writeJSON(*out, report))
		fmt.Printf("report: %s\n", *out)
	}
}

func filterCorpus(c *eval.FuzzyCorpus, languages, kinds map[string]bool, limit int) *eval.FuzzyCorpus {
	out := *c
	out.Items = nil
	for _, item := range c.Items {
		if len(languages) > 0 && !languages[item.Language] {
			continue
		}
		if len(kinds) > 0 && !kinds[item.Kind] {
			continue
		}
		out.Items = append(out.Items, item)
	}
	out.Count = len(out.Items)
	if limit > 0 && limit < out.Count {
		out.Items = out.Items[:limit]
		out.Count = limit
	}
	return &out
}

func splitList(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out[part] = true
		}
	}
	return out
}

func printReport(rep eval.FuzzyReport) {
	fmt.Printf("題庫 %s（seed %d，%d 題）；注音資料 %s；日文 pack %s；模型 none\n",
		rep.Corpus, rep.Seed, len(rep.Cases), rep.Engine.DataDir, packName(rep.Engine.JapanesePack))
	fmt.Printf("注音 fuzzy=%s；回退角色：%s\n", rep.Config.Fuzzy, rep.Config.Ranker)
	fmt.Printf("回退請求：%d（model %d、fallback %d、timeout %d、privacy %d）\n",
		rep.Ranker.Requests, rep.Ranker.Model, rep.Ranker.Fallback, rep.Ranker.Timeout, rep.Ranker.Privacy)
	fmt.Printf("\n%-6s %-12s %5s | %-25s | %-25s | %s\n", "語言", "擾動", "n", "raw top1/top5/文字", "fallback top1/top5/文字", "降級")
	for _, g := range rep.Groups {
		lang := g.Language
		kind := g.Kind
		if lang == "" {
			lang = "(all)"
		}
		if kind == "" {
			kind = "(合計)"
		}
		fmt.Printf("%-6s %-12s %5d | %7.1f%% %7.1f%% %7.1f%% | %7.1f%% %7.1f%% %7.1f%% | %d\n",
			lang, kind, g.N,
			100*g.Raw.Top1Rate, 100*g.Raw.Top5Rate, 100*g.Raw.IntendedRate,
			100*g.Fallback.Top1Rate, 100*g.Fallback.Top5Rate, 100*g.Fallback.IntendedRate,
			g.Degraded.Cases)
	}
	for _, g := range rep.Groups {
		if g.Kind != "" || g.Degraded.Cases == 0 {
			continue
		}
		fmt.Printf("%s 降級原因（仍在分母）：typing %d、no_reading %d、no_decoder %d、no_candidates %d、no_commit %d、unhandled %d\n",
			g.Language, g.Degraded.TypingErrors, g.Degraded.NoReading, g.Degraded.NoDecoder,
			g.Degraded.NoCandidates, g.Degraded.NoCommit, g.Degraded.Unhandled)
	}
}

func packName(p *eval.FuzzyPackInfo) string {
	if p == nil {
		return "none"
	}
	if p.SHA256 == "" {
		return p.Path
	}
	return p.Path + "@" + p.SHA256[:12]
}

func marshal(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func writeJSON(path string, v any) error {
	raw, err := marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "0c-eval-fuzzy:", msg)
	os.Exit(1)
}
