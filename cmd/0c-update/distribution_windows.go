//go:build windows

package main

import (
	"github.com/0c-biggo/0c-ime/internal/enginedir"
	"github.com/0c-biggo/0c-ime/internal/installs"
)

// 系統套件安裝（#181）只有 Linux；Windows 一律是官網安裝。
func packageManagedExe(string) bool         { return false }
func distributionVersion() string           { return "" }
func delegateToPersonal(args []string) bool { return false }

// installList 是 status 的 installs：使用中的版本目錄，設定程式的「模型」頁據此
// 用與引擎相同的 0c-server 與 -data 執行 -model-report（P2-50）。
func installList() []installs.Install {
	if in, ok := windowsInstallAt(basePath(), enginedir.Executable(),
		enginedir.SettingsExecutable(), updaterName()); ok {
		return []installs.Install{in}
	}
	return nil
}
