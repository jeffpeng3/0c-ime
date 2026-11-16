//go:build windows

package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// TSF 的 CLSID 與兩個語言 profile，與 platforms/windows/tsf/src/guids.rs 相同。
const (
	tsfCLSID       = "{138C35D0-7F08-4CEF-BA90-C34C16301528}"
	tsfProfileZh   = "{EF62F743-9855-4C94-A658-1F0F98B7B840}"
	tsfProfileJa   = "{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"
	tsfLanguageZh  = "0x0404"
	tsfLanguageJa  = "0x0411"
	tsfProfileRoot = `SOFTWARE\Microsoft\CTF\TIP\` + tsfCLSID + `\LanguageProfile`
)

// profileRegistration 是 status -json 的單一 profile 狀態。
type profileRegistration struct {
	Language   string `json:"language"`
	LanguageID string `json:"language_id"`
	Profile    string `json:"profile"`
	Registered bool   `json:"registered"`
}

// profileRegistered 檢查 TSF 的 profile 機碼。msctf 可能把語言 ID 寫成 0x0404 或 0x00000404，
// 兩種都看；機碼存在即代表這個 profile 已註冊（DLL 取消註冊時會一起刪除）。
func profileRegistered(langid, profile string) bool {
	for _, lang := range []string{langid, "0x0000" + strings.TrimPrefix(langid, "0x")} {
		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			fmt.Sprintf(`%s\%s\%s`, tsfProfileRoot, lang, profile),
			registry.QUERY_VALUE|registry.WOW64_64KEY,
		)
		if err == nil {
			key.Close()
			return true
		}
	}
	return false
}

// tsfProfilesStatus 是 0c-update status -json 的 profiles 欄位（T28）：兩個 profile 各自是否已註冊。
// 輸入法選單與設定程式可以據此判斷缺少哪一個語言，不必再自行拼登錄路徑。
func tsfProfilesStatus() any {
	return map[string]profileRegistration{
		"zh-TW": {
			Language:   "zh",
			LanguageID: tsfLanguageZh,
			Profile:    tsfProfileZh,
			Registered: profileRegistered(tsfLanguageZh, tsfProfileZh),
		},
		"ja-JP": {
			Language:   "ja",
			LanguageID: tsfLanguageJa,
			Profile:    tsfProfileJa,
			Registered: profileRegistered(tsfLanguageJa, tsfProfileJa),
		},
	}
}
