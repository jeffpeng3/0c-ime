package personal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0c-biggo/0c-ime/core/reading"
)

func units(rs ...string) reading.Sequence {
	out := make(reading.Sequence, len(rs))
	for i, r := range rs {
		out[i] = reading.Unit(r)
	}
	return out
}

// writeStoreFile 在測試裡模擬宿主存檔：先寫暫存檔、落盤，再改名。
// core 不碰檔案系統，正式存檔由宿主用同樣的協定實作。
func writeStoreFile(dir, name string, st *Store) error {
	var buf bytes.Buffer
	if err := st.Save(&buf); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".personal-ja-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

func TestKeyUsesReadingAndNormalizedLeftContext(t *testing.T) {
	seq := units("き", "ょ", "う")
	if got, want := Key(seq, ""), "き ょ う|"+ContextHash(""); got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
	if Key(seq, "今日は") == Key(seq, "空気が") {
		t.Fatal("不同前文不該共用鍵值")
	}
	if Key(seq, " 今日　は ") != Key(seq, "今日は") {
		t.Fatal("空白差異不該拆成不同前文")
	}
	if Key(seq, "ABC") != Key(seq, "abc") {
		t.Fatal("英文大小寫應正規化")
	}
	long := strings.Repeat("あ", 100)
	if got, want := NormalizeLeft(long), strings.Repeat("あ", maxContextRunes); got != want {
		t.Fatalf("長前文 = %q，應只留尾巴 %q", got, want)
	}
}

func TestRecordLookupAndJSONContract(t *testing.T) {
	st := NewStore()
	seq := units("き", "ょ", "う")
	if !st.Record(Selection{Reading: seq, Surface: "今日", Segments: []string{"今日"}, Left: "", At: 1760000000}) {
		t.Fatal("Record = false")
	}
	e, ok := st.Lookup(seq, "")
	if !ok || e.Surface != "今日" || e.Count != 1 || e.LastUsed != 1760000000 || e.Source != SourceSelection {
		t.Fatalf("Lookup = %+v, %v", e, ok)
	}
	if !st.Record(Selection{Reading: seq, Surface: "今日", Left: ""}) {
		t.Fatal("第二次 Record 應回 true")
	}
	if e, _ := st.Lookup(seq, ""); e.Count != 2 {
		t.Fatalf("次數 = %d", e.Count)
	}
}

func TestJSONContractShape(t *testing.T) {
	st := NewStore()
	st.Record(Selection{
		Reading:         units("き", "ょ", "う", "は"),
		Surface:         "今日は",
		Segments:        []string{"今日", "は"},
		SegmentReadings: []reading.Sequence{units("き", "ょ", "う"), units("は")},
		Left:            "",
		At:              1760000000,
	})
	var buf strings.Builder
	if err := st.Save(&buf); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Format   int              `json:"format"`
		Language string           `json:"language"`
		Entries  []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != 1 || doc.Language != "ja" || len(doc.Entries) != 1 {
		t.Fatalf("文件 = %+v", doc)
	}
	e := doc.Entries[0]
	for _, k := range []string{"key", "reading", "surface", "segments", "count", "last_used", "source"} {
		if _, ok := e[k]; !ok {
			t.Errorf("缺少 %q：%v", k, e)
		}
	}
	if key, _ := e["key"].(string); !strings.Contains(key, "|") || !strings.HasPrefix(key, "き ょ う は|") {
		t.Fatalf("key = %q", key)
	}
	rd, _ := e["reading"].([]any)
	if len(rd) != 2 || rd[0] != "きょう" || rd[1] != "は" {
		t.Fatalf("reading 應與 segments 逐段對應：%#v", e["reading"])
	}
	segs, _ := e["segments"].([]any)
	if len(segs) != 2 || segs[0] != "今日" || segs[1] != "は" {
		t.Fatalf("segments = %#v", e["segments"])
	}
}

func TestSaveLoadRoundTripIsStable(t *testing.T) {
	dir := t.TempDir()
	st := NewStore()
	st.RecordAll([]Selection{
		{Reading: units("き", "ょ", "う"), Surface: "今日", Segments: []string{"今日"}, At: 3},
		{Reading: units("く", "う", "き"), Surface: "空気", Segments: []string{"空気"}, Left: "今日", At: 4},
	})
	if err := writeStoreFile(dir, FileName, st); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(os.DirFS(dir), FileName)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 2 {
		t.Fatalf("條目數 = %d", got.Len())
	}
	if e, ok := got.Lookup(units("き", "ょ", "う"), ""); !ok || e.Surface != "今日" {
		t.Fatalf("今日 = %+v, %v", e, ok)
	}
	if e, ok := got.Lookup(units("く", "う", "き"), "今日"); !ok || e.Surface != "空気" {
		t.Fatalf("空気 = %+v, %v", e, ok)
	}
	if err := writeStoreFile(dir, FileName, got); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("存檔不穩定：\n%s\n%s", first, second)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".personal-ja-*")); len(leftovers) != 0 {
		t.Fatalf("改名後不該留下暫存檔：%v", leftovers)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	st, err := LoadFile(os.DirFS(t.TempDir()), FileName)
	if err != nil || st.Len() != 0 {
		t.Fatalf("LoadFile = %v, %v", st, err)
	}
}

func TestLoadCorruptFileReportsAndCanRecover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	corrupt := `{"format":1,"entries":[`
	if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadFile(os.DirFS(dir), FileName)
	if st == nil || st.Len() != 0 {
		t.Fatalf("損壞檔應從空白開始：%v", st)
	}
	var corruptErr *CorruptError
	if !errors.As(err, &corruptErr) || corruptErr.Name != FileName {
		t.Fatalf("err = %v，want *CorruptError", err)
	}
	// 宿主收到 CorruptError 後把原檔改名保留，下一次讀取視為不存在。
	saved := path + ".corrupt-20261117T000000Z"
	if err := os.Rename(path, saved); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(saved); err != nil || string(b) != corrupt {
		t.Fatalf("隔離檔 = %q, %v", b, err)
	}
	if st, err := LoadFile(os.DirFS(dir), FileName); err != nil || st.Len() != 0 {
		t.Fatalf("隔離後 = %v, %v", st, err)
	}
	// 下一次存檔要能建立乾淨的檔案。
	st.Record(Selection{Reading: units("あ"), Surface: "亜"})
	if err := writeStoreFile(dir, FileName, st); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFile(os.DirFS(dir), FileName); err != nil || got.Len() != 1 {
		t.Fatalf("重建後 = %v, %v", got, err)
	}
}

func TestLoadNewerFormatAndLanguageRejected(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		body string
		as   func(error) bool
	}{
		{"newer", `{"format":99,"language":"ja","entries":[]}`, func(err error) bool {
			var e *NewerFormatError
			return errors.As(err, &e)
		}},
		{"other-language", `{"format":1,"language":"zh","entries":[]}`, func(err error) bool {
			var e *LanguageError
			return errors.As(err, &e)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFile(os.DirFS(dir), tc.name+".json"); err == nil || !tc.as(err) {
				t.Fatalf("err = %v", err)
			}
			if b, err := os.ReadFile(path); err != nil || string(b) != tc.body {
				t.Fatalf("不得改寫原檔：%q, %v", b, err)
			}
		})
	}
}

func TestLoadSkipsInvalidEntries(t *testing.T) {
	okKey := Key(units("あ"), "")
	derivedKey := Key(units("お"), "")
	body := `{"format":1,"language":"ja","entries":[
		{"key":"` + okKey + `","reading":["あ"],"surface":"亜","count":2},
		{"key":"","reading":["い"],"surface":"以","count":1},
		{"key":"う|","reading":["う"],"surface":"","count":1},
		{"key":"え|","reading":["え"],"surface":"江","count":0},
		{"key":"` + derivedKey + `","reading":[],"surface":"尾","count":1}
	]}`
	st, err := Load(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if st.Len() != 2 {
		t.Fatalf("條目數 = %d，want 2", st.Len())
	}
	if e, ok := st.Lookup(units("お"), ""); !ok || e.Surface != "尾" || len(e.Units()) != 1 {
		t.Fatalf("缺 reading 的條目應由鍵值推回：%+v, %v", e, ok)
	}
}

func TestUndoRestoresWholeTransaction(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("き", "ょ", "う"), Surface: "今日"})
	if n := st.RecordAll([]Selection{
		{Reading: units("く", "う", "き"), Surface: "空気"},
		{Reading: units("が"), Surface: "が"},
	}); n != 2 {
		t.Fatalf("RecordAll = %d", n)
	}
	if st.Len() != 3 {
		t.Fatalf("條目數 = %d", st.Len())
	}
	if !st.Undo() {
		t.Fatal("Undo = false")
	}
	if st.Len() != 1 {
		t.Fatalf("反悔後應只剩第一次：%d", st.Len())
	}
	if _, ok := st.Lookup(units("く", "う", "き"), ""); ok {
		t.Fatal("反悔應一次移除整筆交易")
	}
	if e, ok := st.Lookup(units("き", "ょ", "う"), ""); !ok || e.Count != 1 {
		t.Fatalf("較早的學習不該被反悔：%+v, %v", e, ok)
	}
	if !st.Undo() || st.Len() != 0 || st.Undo() {
		t.Fatalf("反悔堆疊 = len %d", st.Len())
	}
}

func TestForgetMatchesSurfaceAndSegments(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("き", "ょ", "う"), Surface: "今日", Segments: []string{"今日"}})
	st.Record(Selection{Reading: units("く", "う", "き", "が"), Surface: "空気が", Segments: []string{"空気", "が"}})
	if n := st.Forget("空気"); n != 1 {
		t.Fatalf("Forget = %d", n)
	}
	if st.Len() != 1 {
		t.Fatalf("剩 %d 筆", st.Len())
	}
	if _, ok := st.Lookup(units("く", "う", "き", "が"), ""); ok {
		t.Fatal("包含該詞段的條目應被忘記")
	}
	if !st.Undo() || st.Len() != 2 {
		t.Fatal("忘記應可反悔")
	}
	if n := st.ForgetAll([]string{"今日", "が"}); n != 2 || st.Len() != 0 {
		t.Fatalf("ForgetAll = %d, 剩 %d", n, st.Len())
	}
}

func TestResetClearsLearningButKeepsPaused(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("あ"), Surface: "亜"})
	st.SetPaused(true)
	st.Reset()
	if st.Len() != 0 || !st.Paused() {
		t.Fatalf("重置後 len=%d paused=%v", st.Len(), st.Paused())
	}
	if st.Record(Selection{Reading: units("い"), Surface: "以"}) {
		t.Fatal("暫停時不該學習")
	}
	st.SetPaused(false)
	if !st.Record(Selection{Reading: units("い"), Surface: "以"}) {
		t.Fatal("恢復後應可學習")
	}
}

func TestPausedStillAppliesExistingRanking(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("あ"), Surface: "亜"})
	st.SetPaused(true)
	if e, ok := st.Lookup(units("あ"), ""); !ok || e.Count != 1 {
		t.Fatalf("暫停學習不該清掉既有排序：%+v, %v", e, ok)
	}
	if st.Record(Selection{Reading: units("あ"), Surface: "亜"}) {
		t.Fatal("暫停時不該增加次數")
	}
	if e, _ := st.Lookup(units("あ"), ""); e.Count != 1 {
		t.Fatalf("次數 = %d", e.Count)
	}
}

func TestConcurrentRecordOnOneStore(t *testing.T) {
	st := NewStore()
	const workers, each = 8, 50
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				seq := units("あ", string(rune('あ'+w)), string(rune('あ'+i)))
				st.Record(Selection{Reading: seq, Surface: "語" + string(rune('あ'+w)) + string(rune('あ'+i))})
			}
		}(w)
	}
	wg.Wait()
	if st.Len() != workers*each {
		t.Fatalf("條目數 = %d, want %d", st.Len(), workers*each)
	}
}

func TestConcurrentWritersLeaveCompleteFile(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 8, 100
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := NewStore()
			for i := 0; i < each; i++ {
				// 兩個かな欄位各取安全範圍，讓 100 筆鍵值都不同又都是合法單位。
				a := string(rune('あ' + i/50))
				b := string(rune('あ' + i%50))
				st.Record(Selection{Reading: units("あ", a, b), Surface: "語" + a + b})
			}
			if err := writeStoreFile(dir, FileName, st); err != nil {
				t.Errorf("存檔：%v", err)
			}
		}()
	}
	wg.Wait()
	got, err := LoadFile(os.DirFS(dir), FileName)
	if err != nil {
		t.Fatalf("最後一份檔應完整可讀：%v", err)
	}
	if got.Len() != each {
		t.Fatalf("條目數 = %d, want %d（每個寫入者都有 %d 筆）", got.Len(), each, each)
	}
}

func TestConcurrentRecordAndSave(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("あ"), Surface: "亜"})
	const readers, writers, each = 4, 4, 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				seq := units("あ", string(rune('い'+w)), string(rune('あ'+i%40)))
				st.Record(Selection{Reading: seq, Surface: "語" + string(rune('あ'+i%40))})
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				var buf bytes.Buffer
				if err := st.Save(&buf); err != nil {
					t.Errorf("Save: %v", err)
					return
				}
				var doc struct {
					Format   int     `json:"format"`
					Language string  `json:"language"`
					Entries  []Entry `json:"entries"`
				}
				if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
					t.Errorf("同時存檔讀到半份 JSON：%v", err)
					return
				}
				if doc.Format != Format || doc.Language != Language {
					t.Errorf("文件 = %+v", doc)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestZeroStoreIsUsable(t *testing.T) {
	var st Store
	if st.Len() != 0 {
		t.Fatalf("零值 Len = %d", st.Len())
	}
	if !st.Record(Selection{Reading: units("あ"), Surface: "亜"}) {
		t.Fatal("零值 Store 應可學習")
	}
	if e, ok := st.Lookup(units("あ"), ""); !ok || e.Surface != "亜" {
		t.Fatalf("零值 Lookup = %+v, %v", e, ok)
	}
	var buf strings.Builder
	if err := st.Save(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"format":1`) || !strings.Contains(buf.String(), `"language":"ja"`) {
		t.Fatalf("存檔 = %s", buf.String())
	}
}

func TestLookupKeyAndLen(t *testing.T) {
	st := NewStore()
	st.Record(Selection{Reading: units("あ"), Surface: "亜"})
	key := Key(units("あ"), "")
	if e, ok := st.LookupKey(key); !ok || e.Surface != "亜" {
		t.Fatalf("LookupKey = %+v, %v", e, ok)
	}
	if _, ok := st.LookupKey("ない|"); ok {
		t.Fatal("不存在的鍵不該回傳條目")
	}
}
