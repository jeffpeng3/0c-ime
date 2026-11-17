//go:build windows

package filelock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 另一個 handle 沒有分享寫入權時，開啟鎖檔會暫時得到 ERROR_SHARING_VIOLATION／
// ERROR_ACCESS_DENIED。Wait 必須在原本的兩秒上限內重試，而不是立刻失敗。
func TestWaitRetriesWindowsTransientOpenError(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "update")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "test-write.lock"
	path := filepath.Join(dir, name)
	// 記錄一個不存在的 pid：對方放手後，這把鎖可視為前一次執行留下的。
	if err := os.WriteFile(path, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		windows.CloseHandle(h)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	unlock, err := Wait(ctx, base, name)
	if err != nil {
		t.Fatalf("暫時的 Windows 開檔錯誤應重試：%v", err)
	}
	unlock()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("解鎖後鎖檔仍在：%v", err)
	}
}
