//go:build windows

package atomicfile

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows 沒有在 syscall 套件定義這兩個錯誤，但改名遇到它們都是暫時狀態：
// 目標檔被別的 handle 開著（sharing/lock violation）。
const (
	errSharingViolation syscall.Errno = 32
	errLockViolation    syscall.Errno = 33
)

const (
	replaceAttempts   = 20
	replaceFirstDelay = time.Millisecond
	replaceMaxDelay   = 50 * time.Millisecond
)

// replaceFile 用 MoveFileEx(MOVEFILE_REPLACE_EXISTING) 覆蓋既有檔案。
//
// os.Rename 在 Windows 就是這個呼叫，但不會重試；目標檔被防毒、索引服務或另一個
// writer 短暫開著時會回 Access is denied。這裡只對暫時的錯誤重試，其他錯誤
// （權限、路徑不存在、目標是目錄）立即回報。總等待上限約 0.7 秒。
func replaceFile(tmp, path string) error {
	delay := replaceFirstDelay
	var err error
	for attempt := 0; attempt < replaceAttempts; attempt++ {
		if err = os.Rename(tmp, path); err == nil || !transientRenameError(err) {
			return err
		}
		if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
			return err // 目的地是目錄：重試也不會成功
		}
		if attempt+1 < replaceAttempts {
			time.Sleep(delay)
			if delay < replaceMaxDelay {
				delay *= 2
			}
		}
	}
	return err
}

func transientRenameError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errSharingViolation) ||
		errors.Is(err, errLockViolation)
}
