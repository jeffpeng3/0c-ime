package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/jaimport"
)

// 日文資料包的執行期布局。0c-compile 產出的是 lexicon.bin／lm.bin／lexicon.tsv；
// 0c-server 的日文載入路徑讀 lexicon/words.tsv 與 connection.tsv。打包時由
// WriteJARuntime 把同一個編譯結果轉成引擎實際載入的兩個 TSV，不改編譯格式。
const (
	JARuntimeWords       = "lexicon/words.tsv"
	JARuntimeConnections = "connection.tsv"
)

// WriteJARuntime 從已編譯的 lexicon.bin 匯出 0c-server 載入的執行期 TSV 布局。
// 詞條使用 jaimport.TSV，與 lexicon.tsv 相同；連接成本由解碼後的矩陣重新輸出，
// 不另外建立格式。輸出可以和 packDir 相同。
func WriteJARuntime(packDir, outDir string) error {
	data, err := os.ReadFile(filepath.Join(packDir, "lexicon.bin"))
	if err != nil {
		return err
	}
	lex, err := jaimport.DecodeLexicon(data)
	if err != nil {
		return fmt.Errorf("解碼 lexicon.bin：%w", err)
	}
	// 執行期的 core/ja/lexicon.LoadConnections 固定使用 1.0 作為未指定詞性對的
	// 保底值；資料包若改用別的保底值，TSV 布局無法表達，應直接拒絕而不是默默改變成本。
	if fallback := lex.Matrix().Fallback(); fallback != jaimport.DefaultConnectionCost {
		return fmt.Errorf("連接成本保底值 %v 無法寫進執行期 TSV（只支援 %v）", fallback, jaimport.DefaultConnectionCost)
	}
	if err := os.MkdirAll(filepath.Join(outDir, "lexicon"), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(outDir, JARuntimeWords), jaimport.TSV(lex.Entries())); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(outDir, JARuntimeConnections), jaRuntimeConnections(lex))
}

func jaRuntimeConnections(lex *jaimport.Lexicon) []byte {
	var b strings.Builder
	b.WriteString("# 左詞性\t右詞性\t成本\n")
	for _, c := range lex.Matrix().Connections() {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", c.Left, c.Right, strconv.FormatFloat(c.Cost, 'f', -1, 64))
	}
	return []byte(b.String())
}
