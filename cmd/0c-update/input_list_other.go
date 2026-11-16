//go:build !windows

package main

// listInputMethod：Linux 與 macOS 在安裝流程裡各自把輸入法加入清單（setupInputMethod、activateInputSource）。
func listInputMethod(string) {}

// removeInputMethods：解除安裝時 Linux 與 macOS 各自移除輸入來源。
func removeInputMethods() {}

// removeInputMethodJa：回復舊版時 Linux 與 macOS 沒有獨立的 ja-JP profile 項目要移除。
func removeInputMethodJa() {}
