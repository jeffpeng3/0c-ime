package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0c-biggo/0c-ime/core/engine"
	"github.com/0c-biggo/0c-ime/core/english"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/lang"
	jlang "github.com/0c-biggo/0c-ime/core/lang/ja"
	"github.com/0c-biggo/0c-ime/core/layout"
	"github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/proto"
	"github.com/0c-biggo/0c-ime/core/segment"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
	"github.com/0c-biggo/0c-ime/internal/datadir"
	"github.com/0c-biggo/0c-ime/internal/jadiclex"
)

// MixedSessionRunner 用真正的 profile-free session 跑混合語言題庫（P2-61）。
//
// 每個 case 一個新的 engine／session，學習狀態不跨題累積；同一個 case 內
// 只在同一個 session 依序送按鍵，不呼叫 SetLanguage，也不切換模式。片段
// 語言完全由引擎推斷，runner 只負責：
//
//  1. 把題庫的 reading／keys 轉成實際按鍵；
//  2. 在 input 的 composition 邊界讀取畫面、需要時 変換、送出；
//  3. 依讀音單位把引擎片段對回題庫的 input 索引。
//
// 句子中間換語言時，目前產品會以一次送出（Enter）結束前一段組字；題庫用
// composition 名稱表達這件事。同一個 composition 內可以混合注音與 Latin，
// 也可以讓長詞跨過多個 input 片段合併；runner 會回報 merged 範圍，讓評分把
// 被覆蓋的 input 視為同一個片段，而不是把後段當成缺段。
type MixedSessionRunner struct {
	cfg MixedSessionConfig
}

// MixedSessionConfig 是建立 runner 的固定輸入。資料來源必須由呼叫端載入，
// runner 不讀取使用者個人資料、時鐘或環境變數。
type MixedSessionConfig struct {
	Lex         lexicon.Source
	LM          lm.System
	Japanese    jlang.Source
	Connections *jalexicon.Matrix
	English     *english.Dictionary // nil 使用 session 內建的英文原型詞表
}

// NewMixedSessionRunner 建立 runner。lex／lm 為必填；Japanese 可以為 nil
// （羅馬字仍可組成かな，但沒有漢字候選，報告會標記為沒有 pack）。
func NewMixedSessionRunner(cfg MixedSessionConfig) (*MixedSessionRunner, error) {
	if cfg.Lex == nil || cfg.LM == nil {
		return nil, fmt.Errorf("mixed session runner: lexicon 與 lm 為必填")
	}
	return &MixedSessionRunner{cfg: cfg}, nil
}

// Predict 實作 MixedRunner。
func (r *MixedSessionRunner) Predict(cs MixedCase) (MixedPrediction, error) {
	comps := splitMixedCompositions(cs.Input)
	eng := engine.New(r.cfg.Lex, r.cfg.LM)
	// 產品外殼在 0.2.0 起一律開啟 Preview02；Windows 的單一 Mixed profile
	// 也無條件開啟中英混打。這兩個開關是產品預設，不是評測調參。
	eng.MixedInput = true
	eng.Preview02 = true
	if r.cfg.English != nil {
		eng.English = r.cfg.English
	}
	id := eng.OpenSession()
	defer eng.CloseSession(id)
	sess := eng.Session(id)
	// 即使沒有參考 pack 也開啟日文 runtime：羅馬字仍可組成かな，只是沒有
	// 漢字候選。沒有這一步，空詞典的執行會把羅馬字當成英文原文。
	if !sess.EnableJapanese(r.cfg.Japanese, r.cfg.Connections) {
		return MixedPrediction{}, fmt.Errorf("session 不接受日文 profile")
	}

	var pred MixedPrediction
	unitStart := make([]int, len(cs.Input))
	unitEnd := make([]int, len(cs.Input))
	units := 0
	for i, in := range cs.Input {
		unitStart[i] = units
		n, err := mixedInputUnits(in)
		if err != nil {
			return MixedPrediction{}, err
		}
		units += n
		unitEnd[i] = units
	}

	type compResult struct {
		first, last int
		prefix      []string
		text        string
		segs        []segment.Segment
		unordered   int
	}
	results := make([]compResult, 0, len(comps))
	for _, comp := range comps {
		res := compResult{first: comp.first, last: comp.last}
		res.prefix = make([]string, comp.last-comp.first+1)
		for i := comp.first; i <= comp.last; i++ {
			keys, err := MixedInputKeys(cs.Input[i])
			if err != nil {
				return MixedPrediction{}, err
			}
			var direct strings.Builder
			for _, k := range keys {
				composing := sess.Composing()
				snap, err := eng.Do(id, proto.Request{Type: proto.Key, Key: k})
				if err != nil {
					return MixedPrediction{}, fmt.Errorf("%s: 按鍵 %q: %w", cs.ID, k, err)
				}
				if snap.Handled || !isPrintableKey(k) {
					continue
				}
				// 沒有被輸入法接收的可見字元由應用程式直接插入。只有組字
				// 開始前的空白是正常位置；其他位置記成結構觀察。
				if composing {
					res.unordered++
				}
				direct.WriteString(k)
			}
			res.prefix[i-comp.first] = direct.String()
		}

		// 日文片段先 変換，讓輸出是統計轉換的首選，而不是かな。
		snap, err := eng.Do(id, proto.Request{Type: proto.Display})
		if err != nil {
			return MixedPrediction{}, err
		}
		if sess.Language() == lang.Ja && !snap.Converting && mixedHasKana(sess.Segments()) {
			if _, err := eng.Do(id, proto.Request{Type: proto.Key, Key: proto.KeyConvert}); err != nil {
				return MixedPrediction{}, err
			}
		}
		// 把尚未定案的中英按鍵／字面文字變成真正的片段；日文的路徑本來就
		// 已含字面片段，Cursor 對日文沒有作用。
		snap, err = eng.Do(id, proto.Request{Type: proto.Cursor, To: 1 << 30})
		if err != nil {
			return MixedPrediction{}, err
		}
		res.text = snap.Text()
		res.segs = sess.Segments()
		commit, err := eng.Do(id, proto.Request{Type: proto.Key, Key: proto.KeyEnter})
		if err != nil {
			return MixedPrediction{}, err
		}
		if commit.Commit == "" && res.text != "" {
			return MixedPrediction{}, fmt.Errorf("%s: 送出是空的，畫面卻有 %q", cs.ID, res.text)
		}
		// 以實際送出的文字為準；正常情況與畫面相同。
		if commit.Commit != "" {
			res.text = commit.Commit
		}
		pred.UnorderedDirectKeys += res.unordered
		results = append(results, res)
	}

	for _, res := range results {
		// composition 開頭的應用程式直接輸入（通常是英文前的空白）排在
		// 這次送出之前。
		prefix := res.prefix[0]
		for _, p := range res.prefix[1:] {
			if p != "" {
				pred.UnorderedDirectKeys++
				prefix += p
			}
		}
		pred.Text += prefix + res.text

		spans := mixedSegmentSpans(res.segs, unitStart[res.first])
		si := 0
		for i := res.first; i <= res.last; i++ {
			lo, hi := unitStart[i], unitEnd[i]
			for si < len(spans) && spans[si].start < lo {
				si++
			}
			var assigned []segment.Segment
			for si < len(spans) && spans[si].start < hi {
				sp := spans[si]
				assigned = append(assigned, sp.seg)
				if i < res.last && sp.end > hi {
					pred.MergedSegments++
					last := i
					for last+1 <= res.last && unitStart[last+1] < sp.end {
						last++
					}
					pred.Merged = append(pred.Merged, MixedMergedSpan{First: i, Last: last})
				}
				si++
			}
			surface := res.prefix[i-res.first] + joinedSegmentSurfaces(assigned)
			if surface == "" {
				continue
			}
			langID, variant, ok := mixedPredictionLanguage(assigned, cs.Input[i])
			if !ok {
				// 語言不一致的預測仍要留著，讓 invalid_segments 看見；不能
				// 悄悄用題庫的 language_hint 補上。
				langID, variant = lang.Und, ""
			}
			pred.Segments = append(pred.Segments, MixedPredictedSegment{
				InputIndex: i,
				Surface:    surface,
				Language:   langID,
				Variant:    variant,
			})
		}
	}
	return pred, nil
}

// mixedComposition 是同一組字內連續的 input 片段。
type mixedComposition struct {
	first, last int
}

// mixedHasKana 回報組字中是否有かな。只有字面英文時不送 Convert，避免把
// 原文変換成不相干的候選。
func mixedHasKana(segs []segment.Segment) bool {
	for _, sg := range segs {
		if sg.Script == segment.ScriptKana {
			return true
		}
	}
	return false
}

func splitMixedCompositions(input []MixedInputSegment) []mixedComposition {
	var out []mixedComposition
	for i := 0; i < len(input); {
		name := input[i].Composition
		if name == "" {
			out = append(out, mixedComposition{first: i, last: i})
			i++
			continue
		}
		j := i
		for j+1 < len(input) && input[j+1].Composition == name {
			j++
		}
		out = append(out, mixedComposition{first: i, last: j})
		i = j + 1
	}
	return out
}

// MixedInputKeys 把一個題庫片段轉成實際送出的按鍵。
//
// 題庫有 keys 時照用；注音片段由 reading 經標準排列推出（與注音 golden
// 相同），Latin／數字／符號片段直接用 text。かな沒有通用按鍵，必須在題庫
// 明確記錄羅馬字 keys。
func MixedInputKeys(in MixedInputSegment) ([]string, error) {
	if in.Keys != "" {
		return splitKeyRunes(in.Keys), nil
	}
	switch in.Script {
	case segment.ScriptZhuyin:
		if len(in.Reading) == 0 {
			return nil, fmt.Errorf("注音片段 %q 沒有 reading", in.Text)
		}
		var keys []string
		for _, u := range in.Reading {
			for _, k := range layout.Standard.Keys(zhuyin.Syllable(u)) {
				keys = append(keys, string(k))
			}
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("注音片段 %q 推不出按鍵", in.Text)
		}
		return keys, nil
	case segment.ScriptKana:
		return nil, fmt.Errorf("かな片段 %q 沒有 keys（羅馬字按鍵）", in.Text)
	default:
		return splitKeyRunes(in.Text), nil
	}
}

func splitKeyRunes(s string) []string {
	out := make([]string, 0, len([]rune(s)))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func isPrintableKey(k string) bool {
	r := []rune(k)
	return len(r) == 1 && unicode.IsPrint(r[0])
}

// mixedInputUnits 是對齊用的讀音單位數。有 reading 時就是 reading 的長度；
// 否則 Latin／數字／符號以 rune 數計，與 literal 片段的長度一致。
func mixedInputUnits(in MixedInputSegment) (int, error) {
	if len(in.Reading) > 0 {
		return len(in.Reading), nil
	}
	if in.Script == segment.ScriptZhuyin {
		return 0, fmt.Errorf("注音片段 %q 沒有 reading", in.Text)
	}
	return utf8.RuneCountInString(in.Text), nil
}

// mixedSegmentSpans 把引擎片段換成 composition 內的讀音單位區間。引擎片段
// 有 reading 時用它，沒有 reading 的字面片段以詞面 rune 數計。
func mixedSegmentSpans(segs []segment.Segment, base int) []mixedSegmentSpan {
	out := make([]mixedSegmentSpan, 0, len(segs))
	at := base
	for _, sg := range segs {
		n := len(sg.Reading)
		if n == 0 {
			n = utf8.RuneCountInString(sg.Surface)
		}
		out = append(out, mixedSegmentSpan{seg: sg, start: at, end: at + n})
		at += n
	}
	return out
}

type mixedSegmentSpan struct {
	seg        segment.Segment
	start, end int
}

func joinedSegmentSurfaces(segs []segment.Segment) string {
	var b strings.Builder
	for _, sg := range segs {
		b.WriteString(sg.Surface)
	}
	return b.String()
}

// mixedPredictionLanguage 從引擎片段取語言與變體。數字與符號片段沒有語言
// 標籤（literalLanguage 只標字母），這裡依題庫的 script 補成英文原文；
// 這是評測的標籤慣例，不是引擎的語言判斷。
func mixedPredictionLanguage(segs []segment.Segment, in MixedInputSegment) (lang.ID, lang.OutputVariant, bool) {
	if len(segs) == 0 {
		id := fallbackLiteralLanguage(in.Script)
		return id, lang.DefaultVariant(id), id != lang.Und
	}
	// 數字／符號的字面片段沒有語言標籤；先找第一個有標籤的片段，再要求
	// 其餘有標籤的片段一致。不能把零值當成另一種語言。
	id := lang.Und
	variant := lang.OutputVariant("")
	for _, sg := range segs {
		if sid := sg.Language.Language; sid != lang.Und {
			if id == lang.Und {
				id = sid
			} else if sid != id {
				return lang.Und, "", false
			}
		}
		if sg.Variant != "" {
			if variant == "" {
				variant = sg.Variant
			} else if sg.Variant != variant {
				return lang.Und, "", false
			}
		}
	}
	if id == lang.Und {
		id = fallbackLiteralLanguage(in.Script)
	}
	if variant == "" {
		variant = lang.DefaultVariant(id)
	}
	if id == lang.Und || variant == "" || variant.Language() != id {
		return lang.Und, "", false
	}
	return id, variant, true
}

func fallbackLiteralLanguage(script segment.Script) lang.ID {
	switch script {
	case segment.ScriptZhuyin, segment.ScriptHan:
		return lang.Zh
	case segment.ScriptKana:
		return lang.Ja
	case segment.ScriptLatin, segment.ScriptDigit, segment.ScriptSymbol, segment.ScriptMixed:
		return lang.En
	}
	return lang.Und
}

// MixedPackInfo 是日文參考 pack 的識別；沒有載入時為 nil。
type MixedPackInfo struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ManifestPath   string `json:"manifest_path,omitempty"`
	ManifestSHA256 string `json:"manifest_sha256,omitempty"`
	Entries        int    `json:"entries"`
}

// MixedEngineInfo 記錄一次混合 benchmark 實際使用的資料。models 永遠是
// "none"：參考資料夾的 int8 ONNX 檔是 placeholder，載入它們的執行只能標成
// wiring-only，不能產生品質數字。
type MixedEngineInfo struct {
	DataDir            string         `json:"data_dir"`
	LexiconSHA256      string         `json:"lexicon_sha256"`
	LMSHA256           string         `json:"lm_sha256"`
	JapanesePack       *MixedPackInfo `json:"japanese_pack,omitempty"`
	JapaneseConnSHA256 string         `json:"japanese_connection_sha256,omitempty"`
	English            string         `json:"english"`
	Models             string         `json:"models"`
	WiringOnly         bool           `json:"wiring_only"`
}

// LoadMixedSessionRunner 從 repo 的資料目錄與（可選的）參考 pack 建立 runner。
// packPath 為空時只組かな，不載入任何日文詞典；報告會留下 pack=nil。
func LoadMixedSessionRunner(dataDir, packPath, connectionPath string) (*MixedSessionRunner, MixedEngineInfo, error) {
	info := MixedEngineInfo{
		DataDir: dataDir,
		English: "prototype",
		Models:  "none: placeholder int8 ONNX models are wiring-only and are never loaded",
	}
	base, err := datadir.Open(dataDir)
	if err != nil {
		return nil, info, err
	}
	cfg := MixedSessionConfig{Lex: base.Model.Lex, LM: base.Model.LM, English: base.English}
	if base.English != nil {
		info.English = "host english pack"
	}
	info.LexiconSHA256, err = optionalFileSHA(filepath.Join(dataDir, "lexicon.tsv"))
	if err != nil {
		return nil, info, err
	}
	info.LMSHA256, err = optionalFileSHA(filepath.Join(dataDir, "lm.tsv"))
	if err != nil {
		return nil, info, err
	}
	if packPath != "" {
		src, err := jadiclex.Load(packPath)
		if err != nil {
			return nil, info, err
		}
		packSHA, err := optionalFileSHA(packPath)
		if err != nil {
			return nil, info, err
		}
		pack := &MixedPackInfo{Path: filepath.Base(packPath), SHA256: packSHA, Entries: src.Entries()}
		if manifestPath := filepath.Join(filepath.Dir(packPath), "manifest.json"); fileExists(manifestPath) {
			pack.ManifestPath = filepath.Base(manifestPath)
			pack.ManifestSHA256, err = optionalFileSHA(manifestPath)
			if err != nil {
				return nil, info, err
			}
		}
		cfg.Japanese = src
		info.JapanesePack = pack
		if connectionPath != "" {
			f, err := os.Open(connectionPath)
			if err != nil {
				return nil, info, err
			}
			conn, err := jalexicon.LoadConnections(f)
			f.Close()
			if err != nil {
				return nil, info, err
			}
			cfg.Connections = conn
			info.JapaneseConnSHA256, err = optionalFileSHA(connectionPath)
			if err != nil {
				return nil, info, err
			}
		}
	}
	r, err := NewMixedSessionRunner(cfg)
	return r, info, err
}

func optionalFileSHA(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
