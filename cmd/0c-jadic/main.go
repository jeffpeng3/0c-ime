// 0c-jadic 檢查與匯入 0c-jadic v1 日文詞典 pack。
//
// 用法：
//
//	0c-jadic inspect -pack ja-lexicon-public-core.pack [-manifest manifest.json] [-surface 明日]
//	0c-jadic import  -pack ja-lexicon-public-core.pack -out build/ja-jadic [-connections data/ja/connection.tsv]
//	0c-jadic convert -data build/ja-jadic kyouha ashita
//
// 匯入只寫入指定的輸出目錄，不修改 repo 內 checked-in 的 data/ja。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/jadic"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "inspect":
		err = runInspect(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "convert":
		err = runConvert(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("未知子命令 %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "0c-jadic:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `0c-jadic 檢查與匯入 0c-jadic v1 日文詞典 pack

  inspect -pack <pack> [-manifest <manifest.json>] [-surface <詞面>] [-pos <n>]
      驗證 pack 與 manifest，印出數量、成本範圍、POS 與 surface 讀音。

  import -pack <pack> [-manifest <manifest.json>] -out <目錄>
         [-origin <來源 ID>] [-cost-scale 0.001]
         [-connections <data/ja/connection.tsv>] [-force]
      把 pack 寫成產品可編譯的 lexicon/words.tsv 與 jadic-import.json。
      預設輸出 build/ja-jadic；不寫入 checked-in data/。

  convert -data <執行期目錄> [-top 5] <讀音>...
      用 core/ja 引擎載入 lexicon/words.tsv 與 connection.tsv，印出候選文節。
      匯入後的煙霧測試；讀音只接受平假名與長音符。
`)
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	packPath := fs.String("pack", "", "pack 檔案")
	manifestPath := fs.String("manifest", "", "manifest.json；空白時用 pack 旁邊的 manifest.json")
	surface := fs.String("surface", "", "只顯示這個 surface 的讀音")
	topPOS := fs.Int("pos", 10, "POS 統計列出的數量")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *packPath == "" {
		return errors.New("inspect 需要 -pack")
	}
	p, err := jadic.Load(*packPath)
	if err != nil {
		return err
	}
	mp, m, err := loadOptionalManifest(*manifestPath, *packPath)
	if err != nil {
		return err
	}
	if err := jadic.Describe(os.Stdout, p, jadic.DescribeOptions{
		PackPath:     *packPath,
		ManifestPath: mp,
		Manifest:     m,
		TopPOS:       *topPOS,
	}); err != nil {
		return err
	}
	if *surface != "" {
		if err := jadic.DescribeSurface(os.Stdout, p, *surface); err != nil {
			return err
		}
	}
	return nil
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	packPath := fs.String("pack", "", "pack 檔案")
	manifestPath := fs.String("manifest", "", "manifest.json；空白時用 pack 旁邊的 manifest.json")
	out := fs.String("out", filepath.Join("build", "ja-jadic"), "輸出目錄（產品資料目錄）")
	origin := fs.String("origin", jadic.DefaultOrigin, "寫進 words.tsv 的來源 ID")
	costScale := fs.Float64("cost-scale", jadic.DefaultCostScale, "pack 成本換算係數")
	connections := fs.String("connections", "", "選填：複製成輸出目錄的 connection.tsv，pack 本身不含連接成本")
	maxUnits := fs.Int("max-reading-units", 64, "讀音單位上限；超過視為錯誤")
	force := fs.Bool("force", false, "覆寫既有輸出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *packPath == "" {
		return errors.New("import 需要 -pack")
	}
	if isCheckedInData(*out) {
		return fmt.Errorf("輸出目錄 %q 指向 checked-in data/；請改用 build/ 或 repo 外的目錄", *out)
	}
	p, err := jadic.Load(*packPath)
	if err != nil {
		return err
	}
	mp, m, err := loadRequiredManifest(*manifestPath, *packPath)
	if err != nil {
		return err
	}
	res, err := jadic.Import(p, m, jadic.ImportOptions{
		OutDir:          *out,
		PackPath:        *packPath,
		ManifestPath:    mp,
		ConnectionsPath: *connections,
		Origin:          *origin,
		CostScale:       *costScale,
		MaxReadingUnits: *maxUnits,
		Force:           *force,
	})
	if err != nil {
		return err
	}
	fmt.Printf("已匯入 %d 筆詞條、%d 個 surface\n", res.Stats.Entries, res.Stats.Surfaces)
	fmt.Printf("詞條：%s\n回執：%s\n", res.WordsPath, res.ReceiptPath)
	fmt.Printf("成本換算：%s；負成本保留 %d 筆\n", jadic.CostRule, res.Stats.NegativeCosts)
	if len(res.Receipt.Import.Needs) > 0 {
		fmt.Printf("注意：%s\n", strings.Join(res.Receipt.Import.Needs, "；"))
	}
	return nil
}

// isCheckedInData 阻止把匯入結果寫進 repo 的 data/。輸出若在 repo 外則不在此限。
func isCheckedInData(out string) bool {
	clean := filepath.Clean(out)
	if filepath.IsAbs(clean) {
		return false
	}
	return clean == "data" || strings.HasPrefix(clean, "data"+string(os.PathSeparator))
}

func loadOptionalManifest(manifestPath, packPath string) (string, *jadic.Manifest, error) {
	path := manifestPath
	if path == "" {
		path = siblingManifest(packPath)
		if _, err := os.Stat(path); err != nil {
			return "", nil, nil
		}
	}
	m, err := jadic.LoadManifest(path)
	if err != nil {
		return "", nil, err
	}
	return path, m, nil
}

func loadRequiredManifest(manifestPath, packPath string) (string, *jadic.Manifest, error) {
	path := manifestPath
	if path == "" {
		path = siblingManifest(packPath)
	}
	m, err := jadic.LoadManifest(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("找不到 manifest.json（%s）；import 需要 manifest 配對", path)
		}
		return "", nil, err
	}
	return path, m, nil
}

func siblingManifest(packPath string) string {
	return filepath.Join(filepath.Dir(packPath), "manifest.json")
}
