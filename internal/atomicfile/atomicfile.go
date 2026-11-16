// Package atomicfile 以「同目錄暫存檔、寫完 Sync、再改名」取代整個檔案：讀的人只會讀到舊檔或完整的新檔，
// 斷電後也不會留下長度對了、內容卻是零的檔案。同一路徑的寫入在程式內序列化；Windows 的
// 改名以 MoveFileEx(MOVEFILE_REPLACE_EXISTING) 覆蓋既有檔案，遇到防毒或索引服務暫時開著
// 目標檔時會重試（見 replaceFile）。暫存檔的權限是 0600。
package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Write 原子地把 data 寫到 path；失敗時刪掉暫存檔，原本的檔案不動。
func Write(path string, data []byte) error {
	unlock := lockPath(path)
	defer unlock()

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil { // 改名之前一定要先關閉，Windows 才不會鎖住暫存檔
		return err
	}
	if err = replaceFile(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// pathLock 是單一路徑的寫入鎖；refs 讓沒有等待者的鎖可以從 map 移除。
type pathLock struct {
	mu   sync.Mutex
	refs int
}

var (
	pathLocksMu sync.Mutex
	pathLocks   = map[string]*pathLock{}
)

// lockPath 取得 path 的寫入鎖。多個 writer 同時存同一份檔案時，改名一次只會有
// 一個在進行，避免 Windows 的 replace-existing 互撞。
func lockPath(path string) func() {
	key := filepath.Clean(path)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	if runtime.GOOS == "windows" { // Windows 路徑不分大小寫
		key = strings.ToLower(key)
	}

	pathLocksMu.Lock()
	l := pathLocks[key]
	if l == nil {
		l = &pathLock{}
		pathLocks[key] = l
	}
	l.refs++
	pathLocksMu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		pathLocksMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(pathLocks, key)
		}
		pathLocksMu.Unlock()
	}
}

// WriteJSON 以兩格縮排、結尾換行的 JSON 原子寫入。
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, append(b, '\n'))
}
