//go:build !windows

package atomicfile

import "os"

// replaceFile 在非 Windows 系統直接改名；POSIX 的 rename 會原子覆蓋既有檔案。
func replaceFile(tmp, path string) error {
	return os.Rename(tmp, path)
}
