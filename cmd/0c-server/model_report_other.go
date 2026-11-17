//go:build !linux

package main

// processTreeRSS 在非 Linux 平台不讀取行程記憶體；resident_bytes 仍照 residency 記帳回報。
func processTreeRSS() (self, children int64) { return 0, 0 }
