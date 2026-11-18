// 0c-japack 把 0c-compile 的日文資料包轉成引擎實際載入的 TSV 布局。
//
// 用法：
//
//	0c-japack -pack build/ja -out build/ja
//
// 輸出 lexicon/words.tsv 與 connection.tsv，不改動 lexicon.bin、lm.bin、manifest.json
// 等編譯產物。輸出目錄可以和資料包相同。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/0c-biggo/0c-ime/internal/compile"
)

func main() {
	pack := flag.String("pack", "", "0c-compile -lang ja 產生的資料包目錄")
	out := flag.String("out", "", "輸出目錄；空白時寫回 -pack")
	flag.Parse()
	if *pack == "" {
		fmt.Fprintln(os.Stderr, "0c-japack: 需要 -pack")
		os.Exit(2)
	}
	if *out == "" {
		*out = *pack
	}
	if err := compile.WriteJARuntime(*pack, *out); err != nil {
		fmt.Fprintln(os.Stderr, "0c-japack:", err)
		os.Exit(1)
	}
}
