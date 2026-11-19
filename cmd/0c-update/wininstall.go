package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/0c-biggo/0c-ime/internal/enginedir"
	"github.com/0c-biggo/0c-ime/internal/installs"
)

// windowsInstallAt 找出 Windows 上使用中的安裝。安裝器把每個版本放在
// %LOCALAPPDATA%\Zuin\versions\<版本>-<雜湊>\，引擎、資料與模型都在那裡；穩定副本
// （0c-update、0c-settings 等）則在 %LOCALAPPDATA%\Zuin\bin\。
//
// 設定程式讀 status 的 installs 啟動 -model-report，所以這裡回傳的路徑必須和引擎實際
// 啟動時用的相同：先看 engine.json（核心更新後運作中的版本），再看收據記錄的版本目錄，
// 最後才是 bin 穩定副本搭配舊的 Zuin\data 布局。資料目錄以 manifest.json 判斷完整性；
// 指向不存在的版本目錄時往後退，不把 base\data 這種不存在的位置交給引擎。
func windowsInstallAt(base, serverName, settingsName, updaterName string) (installs.Install, bool) {
	var candidates []string
	if dir, ok := enginedir.Dir(base); ok {
		candidates = append(candidates, dir)
	}
	if dir := windowsReceiptDir(base); dir != "" {
		candidates = append(candidates, dir)
	}
	seen := make(map[string]bool, len(candidates))
	for _, dir := range candidates {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if in, ok := windowsInstallIn(base, dir, serverName, settingsName, updaterName); ok {
			return in, true
		}
	}
	// 舊版布局：程式直接在 Zuin\bin，字典在 Zuin\data。
	if in, ok := windowsInstallPaths(base, filepath.Join(base, "bin", serverName), filepath.Join(base, "data"),
		serverName, settingsName, updaterName); ok {
		return in, true
	}
	// 更舊的布局：程式與字典都在 Zuin\。
	return windowsInstallPaths(base, filepath.Join(base, serverName), filepath.Join(base, "data"),
		serverName, settingsName, updaterName)
}

func windowsInstallIn(base, dir, serverName, settingsName, updaterName string) (installs.Install, bool) {
	return windowsInstallPaths(base, filepath.Join(dir, serverName), filepath.Join(dir, "data"),
		serverName, settingsName, updaterName)
}

func windowsInstallPaths(base, server, data, serverName, settingsName, updaterName string) (installs.Install, bool) {
	if !regularFile(server) || !regularFile(filepath.Join(data, "manifest.json")) {
		return installs.Install{}, false
	}
	in := installs.Install{Kind: installs.KindPersonal, Server: server, Data: data,
		Active: true, Updates: installs.UpdatesSelf}
	bin := filepath.Join(base, "bin")
	if regularFile(filepath.Join(bin, updaterName)) {
		in.Updater = filepath.Join(bin, updaterName)
	}
	if regularFile(filepath.Join(bin, settingsName)) {
		in.Settings = filepath.Join(bin, settingsName)
	}
	return in, true
}

// windowsReceiptDir 讀安裝收據記錄的版本目錄；停用或格式不對時回空字串。
func windowsReceiptDir(base string) string {
	b, err := os.ReadFile(filepath.Join(base, "install-receipt.json"))
	if err != nil {
		return ""
	}
	var r struct {
		Directory string `json:"directory"`
		Active    *bool  `json:"active"`
	}
	if json.Unmarshal(b, &r) != nil || r.Active != nil && !*r.Active || !windowsVersionDir(r.Directory) {
		return ""
	}
	return filepath.Join(base, filepath.FromSlash(r.Directory))
}

// windowsVersionDir 只接受 versions/<名稱>：相對、正斜線、沒有磁碟代號與 ..。
func windowsVersionDir(dir string) bool {
	rest, ok := strings.CutPrefix(dir, "versions/")
	return ok && rest != "" && !strings.ContainsAny(rest, `/\:`) && rest != "." && rest != ".."
}
