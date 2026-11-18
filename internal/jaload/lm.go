// Package jaload 載入日文執行期資料檔。core 只解碼位元組，檔案系統留給宿主。
package jaload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	jalm "github.com/0c-biggo/0c-ime/core/ja/lm"
)

// LoadLM 載入 lm.bin。檔案不存在時回傳 (nil, nil)，讓沒有語言模型的執行環境
// 維持原本行為；檔案存在但格式錯誤時回報錯誤，不默默忽略損壞的模型。
func LoadLM(path string) (*jalm.Model, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := jalm.Load(f)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", path, err)
	}
	return m, nil
}
