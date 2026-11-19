package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"unicode"

	"github.com/0c-biggo/0c-ime/core/ja/decode"
	"github.com/0c-biggo/0c-ime/core/ja/input"
	"github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	"github.com/0c-biggo/0c-ime/core/reading"
)

// runConvert 用產品引擎載入執行期資料目錄，印出每個讀音的前 n 名候選。
// 這是匯入後的煙霧測試：資料目錄必須有 lexicon/words.tsv 與 connection.tsv。
func runConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	data := fs.String("data", "", "執行期資料目錄（含 lexicon/words.tsv 與 connection.tsv）")
	top := fs.Int("top", 5, "每個讀音印出的候選數")
	romaji := fs.Bool("romaji", false, "把參數當羅馬字按鍵，先用正式組字器轉成かな")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *data == "" {
		return errors.New("convert 需要 -data")
	}
	if *top <= 0 {
		return fmt.Errorf("convert 的 -top 必須大於 0，收到 %d", *top)
	}
	if fs.NArg() == 0 {
		return errors.New("convert 需要至少一個讀音（平假名，或加 -romaji 的羅馬字）")
	}
	dec, err := loadRuntimeDecoder(*data)
	if err != nil {
		return err
	}
	for _, arg := range fs.Args() {
		seq, err := inputSequence(arg, *romaji)
		if err != nil {
			return fmt.Errorf("%q：%w", arg, err)
		}
		results, err := dec.NBest(seq, lang.Context{}, *top)
		if err != nil {
			return fmt.Errorf("%q：%w", arg, err)
		}
		fmt.Printf("讀音 %s：%d 個候選\n", arg, len(results))
		for i, res := range results {
			fmt.Printf("  [%d] %s  分數 %.3f\n", i, decode.ResultSurface(res.Clauses), res.Score)
			for _, c := range res.Clauses {
				fmt.Printf("      %s\t%s\n", reading.Key(c.Reading), c.Surface)
			}
		}
	}
	return nil
}

func loadRuntimeDecoder(dir string) (*decode.Decoder, error) {
	words := filepath.Join(dir, "lexicon", "words.tsv")
	conns := filepath.Join(dir, "connection.tsv")
	wf, err := os.Open(words)
	if err != nil {
		return nil, err
	}
	defer wf.Close()
	lex, err := lexicon.Load(wf)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", words, err)
	}
	cf, err := os.Open(conns)
	if err != nil {
		return nil, err
	}
	defer cf.Close()
	conn, err := lexicon.LoadConnections(cf)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", conns, err)
	}
	var c lexicon.Connection = conn
	if binPath := filepath.Join(dir, "connection.bin"); fileExists(binPath) {
		bf, err := os.Open(binPath)
		if err != nil {
			return nil, err
		}
		ctx, err := lexicon.LoadContextMatrix(bf)
		bf.Close()
		if err != nil {
			return nil, fmt.Errorf("%s：%w", binPath, err)
		}
		c = &lexicon.Connections{POS: conn, Context: ctx}
	}
	return &decode.Decoder{Lex: lex, Conn: c}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// inputSequence 依 -romaji 決定是否先走正式羅馬字組字器。
func inputSequence(raw string, romaji bool) (reading.Sequence, error) {
	if !romaji {
		return kanaSequence(raw)
	}
	c := input.NewRomajiComposer()
	for _, r := range raw {
		if _, ok := c.Insert(string(r)); !ok {
			return nil, fmt.Errorf("羅馬字按鍵 %q 無法組成かな", r)
		}
	}
	c.Flush()
	if pending := c.Pending(); pending != "" {
		return nil, fmt.Errorf("羅馬字輸入停在未完成的按鍵 %q", pending)
	}
	units := c.Units()
	if len(units) == 0 {
		return nil, fmt.Errorf("羅馬字輸入沒有完整的かな")
	}
	return units, nil
}

// kanaSequence 只接受平假名與長音符；範圍外字元直接報錯，不悄悄轉換。
func kanaSequence(s string) (reading.Sequence, error) {
	seq := make(reading.Sequence, 0, len([]rune(s)))
	for _, r := range s {
		if !unicode.Is(unicode.Hiragana, r) && r != 'ー' && r != 'ゝ' && r != 'ゞ' {
			return nil, fmt.Errorf("讀音含非平假名字元 %q", r)
		}
		seq = append(seq, reading.Unit(string(r)))
	}
	return seq, nil
}
