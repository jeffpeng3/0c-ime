// 0c-golden 比對注音行為 baseline（T01）。預設只比對；-update 依目前行為重寫 expected。
// 用法：go run ./cmd/0c-golden [-dir testdata/golden/zh] [-update]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/0c-biggo/0c-ime/internal/golden"
)

func main() {
	dir := flag.String("dir", "testdata/golden/zh", "baseline 目錄（cases.json、data/、expected/）")
	update := flag.Bool("update", false, "以目前行為重寫 expected，並刪除沒有案例的舊檔")
	list := flag.Bool("list", false, "列出案例名稱後結束")
	flag.Parse()

	suite, err := golden.Load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "0c-golden：", err)
		os.Exit(2)
	}
	if *list {
		for _, c := range suite.Cases() {
			fmt.Println(c.Name)
		}
		return
	}
	if *update {
		written, removed, err := suite.Update()
		if err != nil {
			fmt.Fprintln(os.Stderr, "0c-golden：", err)
			os.Exit(1)
		}
		fmt.Printf("已更新 %d 個 baseline", len(written))
		if len(removed) > 0 {
			fmt.Printf("，刪除 %d 個沒有案例的舊檔：%v", len(removed), removed)
		}
		fmt.Println()
		return
	}
	if err := suite.Compare(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "\n注音行為與 baseline 不符。確認是預期的改變後執行：go run ./cmd/0c-golden -update")
		os.Exit(1)
	}
	fmt.Printf("注音 baseline 一致（%d 個案例）\n", len(suite.Cases()))
}
