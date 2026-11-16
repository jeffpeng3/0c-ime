//go:build !windows

package filelock

import "os"

// transientLockError 只在 Windows 上有暫時的檔案狀態；其他平台的錯誤照原樣回傳。
func transientLockError(error) bool { return false }

// readLockFile 讀取鎖檔的持有者 pid。
func readLockFile(name string) ([]byte, error) { return os.ReadFile(name) }
