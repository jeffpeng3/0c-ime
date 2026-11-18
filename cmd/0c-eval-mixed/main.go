// 0c-eval-mixed 跑 P2-61 的混合語言 benchmark。它只重現固定題庫上的觀察值：
// 不改產品排序、不訓練、不載入任何模型（參考資料夾的 int8 檔是 placeholder）。
//
// 子命令：
//
//	build  從 testdata/mixed 的題庫重建 manifest（記錄 pack SHA-256 與 fixtures）
//	run    用真正的 profile-free session 跑題庫並印出指標表
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0c-biggo/0c-ime/internal/eval"
)

const (
	defaultCorpus      = "testdata/mixed"
	defaultManifest    = "testdata/mixed/bench.manifest.json"
	defaultData        = "testdata/golden/zh/data"
	defaultConnections = "data/ja/connection.tsv"
	jaCorpusPath       = "testdata/eval/ja-lexicon-ambiguous.json"
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
	fmt.Fprintf(os.Stderr, `用法：0c-eval-mixed <子命令> [旗標]

  build  重建 testdata/mixed/bench.manifest.json（固定種子、fixtures 與 pack 雜湊）
  run    跑混合語言 benchmark

run 的日文漢字轉換需要 %s 指向 ja-lexicon-public-core.pack；沒有設定時
只會組かな，報告會標記 pack=none。任何載入 placeholder 模型的執行都只能
標成 wiring-only，不是品質數字。
`, referencePackEnv)
	os.Exit(2)
}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	corpus := fs.String("corpus", defaultCorpus, "題庫目錄")
	manifest := fs.String("manifest", defaultManifest, "輸出的 manifest")
	data := fs.String("data", defaultData, "注音 fixture 資料目錄")
	pack := fs.String("pack", os.Getenv(referencePackEnv), "參考 pack；空白時沿用日文題庫釘選的 SHA-256")
	seed := fs.Int("seed", eval.MixedBenchSeed, "固定種子")
	fs.Parse(args)

	dataPin, err := pinData(*data)
	check(err)
	packPin, err := pinPack(*pack)
	check(err)
	m, err := eval.BuildMixedBenchManifest(*corpus, *seed, packPin, dataPin, eval.MixedModelPin{
		Used: false,
		Note: eval.MixedBenchModelNote,
	})
	check(err)
	raw, err := marshal(m)
	check(err)
	check(os.MkdirAll(filepath.Dir(*manifest), 0755))
	check(os.WriteFile(*manifest, raw, 0644))
	fmt.Printf("wrote %s (%d fixtures, seed %d, pack %s)\n",
		*manifest, len(m.Fixtures), m.Seed, short(packPin.SHA256))
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	corpus := fs.String("corpus", defaultCorpus, "題庫目錄")
	manifestPath := fs.String("manifest", defaultManifest, "manifest")
	data := fs.String("data", defaultData, "注音 fixture 資料目錄")
	pack := fs.String("pack", os.Getenv(referencePackEnv), "參考 pack（預設讀 "+referencePackEnv+"）")
	connections := fs.String("connections", defaultConnections, "日文連接成本")
	out := fs.String("out", "", "完整 JSON 報告")
	twice := fs.Bool("twice", false, "連跑兩次並比對可重現性")
	fs.Parse(args)

	entries, err := eval.LoadMixedCorpusDir(*corpus)
	check(err)
	manifest, err := eval.LoadMixedBenchManifest(*manifestPath)
	check(err)
	runner, engineInfo, err := eval.LoadMixedSessionRunner(*data, *pack, *connections)
	check(err)
	report, err := eval.RunMixedBench(entries, manifest, fileSHA(*manifestPath), engineInfo, runner)
	check(err)
	if problems := eval.MixedBenchProblems(entries, manifest, fileSHA(*manifestPath), report); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "manifest:", p)
		}
		os.Exit(1)
	}
	if *twice {
		second, err := eval.RunMixedBench(entries, manifest, fileSHA(*manifestPath), engineInfo, runner)
		check(err)
		a, err := eval.MixedBenchDeterminismSignature(report)
		check(err)
		b, err := eval.MixedBenchDeterminismSignature(second)
		check(err)
		if a != b {
			fail("同一份題庫與 runner 連跑兩次結果不同")
		}
		fmt.Println("determinism: two runs are byte-identical")
	}
	fmt.Print(eval.FormatMixedBenchTable(report))
	if *out != "" {
		raw, err := marshal(report)
		check(err)
		check(os.WriteFile(*out, raw, 0644))
		fmt.Printf("report: %s\n", *out)
	}
}

func pinData(dir string) (*eval.MixedDataPin, error) {
	return &eval.MixedDataPin{
		Dir:           dir,
		LexiconSHA256: fileSHA(filepath.Join(dir, "lexicon.tsv")),
		LMSHA256:      fileSHA(filepath.Join(dir, "lm.tsv")),
	}, nil
}

func pinPack(path string) (*eval.MixedPackPin, error) {
	// 沒有參考 pack 時仍留下日文題庫已凍結的 pack identity，manifest 才能
	// 分辨「換了 pack」與「程式退步」。
	corpus, _, err := eval.LoadJALexCorpus(jaCorpusPath)
	if err != nil {
		return nil, err
	}
	pin := &eval.MixedPackPin{Path: corpus.Pack.Path, SHA256: corpus.Pack.SHA256}
	if path == "" {
		return pin, nil
	}
	sha := fileSHA(path)
	if sha == "" {
		return nil, fmt.Errorf("讀不到 pack %s", path)
	}
	pin.Path = filepath.Base(path)
	pin.SHA256 = sha
	if manifest := filepath.Join(filepath.Dir(path), "manifest.json"); exists(manifest) {
		pin.ManifestPath = filepath.Base(manifest)
		pin.ManifestSHA256 = fileSHA(manifest)
	}
	return pin, nil
}

func fileSHA(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func marshal(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "0c-eval-mixed:", msg)
	os.Exit(1)
}
