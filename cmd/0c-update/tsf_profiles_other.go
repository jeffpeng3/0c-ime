//go:build !windows

package main

// tsfProfilesStatus：TSF profile 只在 Windows 註冊，其他平台不輸出 profiles。
func tsfProfilesStatus() any { return nil }
