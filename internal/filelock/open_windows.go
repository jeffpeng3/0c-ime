//go:build windows

package filelock

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// transientLockError 回報 err 是否只是 Windows 的暫時檔案狀態：另一個 handle 尚未關閉
// （share mode 衝突）、檔案刪除進行中，或掃毒軟體短暫掃描新檔，都會讓 CreateFile／
// DeleteFile 回 ERROR_SHARING_VIOLATION 或 ERROR_ACCESS_DENIED。這些不是鎖語意上的
// 衝突，Wait 會在原本的兩秒上限內重試。
func transientLockError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// readLockFile 讀取鎖檔的持有者 pid。Windows 的 os.OpenFile 不分享刪除權，讀取期間
// 會讓持有者的 os.Remove 失敗、鎖檔留在原地；這裡改成允許 FILE_SHARE_DELETE。
func readLockFile(name string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	f := os.NewFile(uintptr(h), name)
	defer f.Close()
	return io.ReadAll(f)
}
