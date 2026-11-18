// 0c-jaeval runs a committed mixed Chinese/Japanese/English corpus through
// whatever local data is present and compares the per-case output with a
// checksummed snapshot.
//
// The harness never touches the network and never loads an ONNX model.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/internal/jaeval"
)

func main() {
	corpusPath := flag.String("corpus", "testdata/jaeval/corpus.json", "committed mixed-input corpus")
	snapshotPath := flag.String("snapshot", "testdata/jaeval/snapshot.json", "expected per-case snapshot")
	data := flag.String("data", "data/ja", "Japanese data directory (lexicon/words.tsv and connection.tsv)")
	zhData := flag.String("zh-data", "testdata/golden/zh/data", "Chinese data directory (lexicon.tsv and lm.tsv)")
	maxCandidates := flag.Int("candidates", 5, "maximum candidates kept per case")
	update := flag.Bool("update", false, "write the current output to the snapshot")
	report := flag.Bool("report", false, "print aggregate statistics after the table")
	flag.Parse()

	corpus, corpusSHA, err := jaeval.LoadCorpus(*corpusPath)
	check(err)
	engine, info, err := jaeval.Load(jaeval.Options{
		JapaneseDir:   *data,
		ChineseDir:    *zhData,
		MaxCandidates: *maxCandidates,
	})
	check(err)
	results, err := engine.Convert(corpus)
	check(err)
	got := jaeval.BuildSnapshot(corpus.Name, corpusSHA, results)

	printData(info)
	printTable(results)

	if *update {
		check(jaeval.WriteSnapshot(*snapshotPath, got))
		fmt.Printf("snapshot: wrote %s\nsnapshot sha256: %s\n", *snapshotPath, got.SHA256)
	} else {
		want, err := jaeval.LoadSnapshot(*snapshotPath)
		if err != nil {
			fail(err.Error())
		}
		diffs := jaeval.CompareSnapshots(want, got)
		if len(diffs) > 0 {
			fmt.Fprintf(os.Stderr, "snapshot mismatch: %d change(s)\n", len(diffs))
			fmt.Fprint(os.Stderr, jaeval.FormatDiffs(diffs))
			fmt.Fprintf(os.Stderr, "\nsnapshot sha256: want %s got %s\n", want.SHA256, got.SHA256)
			fmt.Fprintln(os.Stderr, "If the change is intentional, rerun with -update.")
			os.Exit(1)
		}
		fmt.Printf("snapshot: OK (%s)\n", got.SHA256)
	}

	if *report {
		fmt.Println()
		fmt.Print(jaeval.FormatReport(jaeval.BuildReport(results)))
	}
}

func printData(info jaeval.DataInfo) {
	ja := "fallback (kana only)"
	if info.JapaneseLoaded {
		ja = fmt.Sprintf("loaded %s/%s", short(info.JapaneseLexiconSHA256), short(info.JapaneseConnectionSHA256))
	}
	zh := "fallback (literal zhuyin)"
	if info.ChineseLoaded {
		zh = fmt.Sprintf("loaded %s/%s", short(info.ChineseLexiconSHA256), short(info.ChineseLMSHA256))
	}
	fmt.Printf("data: ja=%s  zh=%s  models=none\n", ja, zh)
}

func printTable(results []jaeval.CaseResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCATEGORY\tINPUT\tOUTPUT\tEXPECT\tRANK\tCANDS")
	for _, r := range results {
		rank := "-"
		if r.Rank > 0 {
			rank = fmt.Sprint(r.Rank)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			r.ID, r.Category, display(r.Input), display(r.Output), display(r.Expect), rank, len(r.Candidates))
	}
	w.Flush()
}

func display(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) > 48 {
		runes := []rune(s)
		return string(runes[:45]) + "..."
	}
	return s
}

func short(s string) string {
	if len(s) <= 12 {
		if s == "" {
			return "none"
		}
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
	fmt.Fprintln(os.Stderr, "0c-jaeval:", msg)
	os.Exit(1)
}
