// Package jaeval runs a deterministic mixed Chinese/Japanese/English conversion
// corpus against whatever runtime data is present on disk. It is intentionally
// independent of the session UI: each input string is split into script runs,
// converted in order, and combined into a small candidate list.
package jaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	zhdecode "github.com/0c-biggo/0c-ime/core/decode"
	"github.com/0c-biggo/0c-ime/core/input"
	jadecode "github.com/0c-biggo/0c-ime/core/ja/decode"
	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
	"github.com/0c-biggo/0c-ime/core/ja/mixed"
	"github.com/0c-biggo/0c-ime/core/lang"
	zhlexicon "github.com/0c-biggo/0c-ime/core/lexicon"
	"github.com/0c-biggo/0c-ime/core/lm"
	"github.com/0c-biggo/0c-ime/core/personal"
	"github.com/0c-biggo/0c-ime/core/reading"
	"github.com/0c-biggo/0c-ime/core/zhuyin"
)

// Options control data loading. Empty directories are allowed: conversion then
// falls back to kana/literal output instead of failing.
type Options struct {
	JapaneseDir   string
	ChineseDir    string
	MaxCandidates int
}

// DataInfo records which files were actually used. Hashes are informational and
// are not part of the snapshot hash, so a snapshot can be compared after moving
// the repository or switching between real data and the fallback.
type DataInfo struct {
	JapaneseDir              string `json:"japanese_dir,omitempty"`
	JapaneseLexiconSHA256    string `json:"japanese_lexicon_sha256,omitempty"`
	JapaneseConnectionSHA256 string `json:"japanese_connection_sha256,omitempty"`
	JapaneseLoaded           bool   `json:"japanese_loaded"`
	ChineseDir               string `json:"chinese_dir,omitempty"`
	ChineseLexiconSHA256     string `json:"chinese_lexicon_sha256,omitempty"`
	ChineseLMSHA256          string `json:"chinese_lm_sha256,omitempty"`
	ChineseLoaded            bool   `json:"chinese_loaded"`
}

// Engine performs deterministic conversion for one corpus run.
type Engine struct {
	ja      *jadecode.Decoder
	zh      *zhdecode.Decoder
	maxCand int
}

// Load builds an engine from the supplied data directories. Missing files are
// not an error: the statistical backend simply stays disabled for that language.
func Load(opts Options) (*Engine, DataInfo, error) {
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 5
	}
	e := &Engine{maxCand: opts.MaxCandidates}
	var info DataInfo

	if opts.JapaneseDir != "" {
		info.JapaneseDir = opts.JapaneseDir
		dec, lexiconPath, connPath, err := loadJapanese(opts.JapaneseDir)
		if err != nil {
			return nil, info, err
		}
		if lexiconPath != "" {
			info.JapaneseLexiconSHA256, _ = fileSHA(lexiconPath)
		}
		if connPath != "" {
			info.JapaneseConnectionSHA256, _ = fileSHA(connPath)
		}
		if dec != nil {
			e.ja = dec
			info.JapaneseLoaded = true
		}
	}

	if opts.ChineseDir != "" {
		info.ChineseDir = opts.ChineseDir
		lexPath := filepath.Join(opts.ChineseDir, "lexicon.tsv")
		lmPath := filepath.Join(opts.ChineseDir, "lm.tsv")
		if fileExists(lexPath) && fileExists(lmPath) {
			dec, err := loadChinese(lexPath, lmPath)
			if err != nil {
				return nil, info, err
			}
			e.zh = dec
			info.ChineseLoaded = true
			info.ChineseLexiconSHA256, _ = fileSHA(lexPath)
			info.ChineseLMSHA256, _ = fileSHA(lmPath)
		}
	}
	return e, info, nil
}

// loadJapanese accepts a data directory (data/ja) or a lexicon TSV directly.
// The connection table is optional; the lexicon is required for the decoder.
func loadJapanese(path string) (*jadecode.Decoder, string, string, error) {
	var lexiconPath, connPath string
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		lexiconPath = path
		connPath = filepath.Join(filepath.Dir(path), "connection.tsv")
	} else {
		lexiconPath = filepath.Join(path, "lexicon", "words.tsv")
		if !fileExists(lexiconPath) {
			lexiconPath = filepath.Join(path, "words.tsv")
		}
		connPath = filepath.Join(path, "connection.tsv")
	}
	if !fileExists(lexiconPath) {
		return nil, "", "", nil
	}

	f, err := os.Open(lexiconPath)
	if err != nil {
		return nil, lexiconPath, connPath, err
	}
	lex, err := jalexicon.Load(f)
	f.Close()
	if err != nil {
		return nil, lexiconPath, connPath, fmt.Errorf("%s: %w", lexiconPath, err)
	}
	var conn *jalexicon.Matrix
	if fileExists(connPath) {
		cf, err := os.Open(connPath)
		if err != nil {
			return nil, lexiconPath, connPath, err
		}
		conn, err = jalexicon.LoadConnections(cf)
		cf.Close()
		if err != nil {
			return nil, lexiconPath, connPath, fmt.Errorf("%s: %w", connPath, err)
		}
	}
	return &jadecode.Decoder{Lex: lex, Conn: connectionOrNil(conn), PreferHiragana: true}, lexiconPath, connPath, nil
}

func connectionOrNil(conn *jalexicon.Matrix) jalexicon.Connection {
	if conn == nil {
		return nil
	}
	return conn
}

func loadChinese(lexPath, lmPath string) (*zhdecode.Decoder, error) {
	lf, err := os.Open(lexPath)
	if err != nil {
		return nil, err
	}
	lex, err := zhlexicon.LoadWithOptions(lf, zhlexicon.LoadOptions{Language: "zh"})
	lf.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", lexPath, err)
	}
	mf, err := os.Open(lmPath)
	if err != nil {
		return nil, err
	}
	sys, err := lm.Load(mf)
	mf.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", lmPath, err)
	}
	return &zhdecode.Decoder{Lex: lex, Score: personal.NewModel(lex, sys)}, nil
}

// CaseResult is one converted corpus case.
type CaseResult struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Input      string   `json:"input"`
	Expect     string   `json:"expect"`
	Output     string   `json:"output"`
	Candidates []string `json:"candidates"`
	Rank       int      `json:"rank"`
}

// Convert runs every case in file order.
func (e *Engine) Convert(c *Corpus) ([]CaseResult, error) {
	out := make([]CaseResult, 0, len(c.Cases))
	for _, cs := range c.Cases {
		cands := e.ConvertString(cs.Input)
		if len(cands) == 0 {
			cands = []string{cs.Input}
		}
		rank := 0
		for i, cand := range cands {
			if cand == cs.Expect {
				rank = i + 1
				break
			}
		}
		out = append(out, CaseResult{
			ID:         cs.ID,
			Category:   cs.Category,
			Input:      cs.Input,
			Expect:     cs.Expect,
			Output:     cands[0],
			Candidates: cands,
			Rank:       rank,
		})
	}
	return out, nil
}

// ConvertString splits input into script runs and returns combined candidates,
// best first. The first candidate is the displayed conversion.
func (e *Engine) ConvertString(input string) []string {
	parts := splitParts(input)
	groups := make([][]string, 0, len(parts))
	for _, p := range parts {
		switch p.kind {
		case partZhuyin:
			groups = append(groups, e.convertZhuyin(p.text))
		case partKana:
			groups = append(groups, e.convertJapaneseUnits(runeUnits(p.text)))
		case partLatin:
			groups = append(groups, e.convertLatin(p.text))
		default:
			groups = append(groups, []string{p.text})
		}
	}
	cands := combine(groups, e.maxCand)
	if len(cands) == 0 {
		return []string{input}
	}
	return cands
}

const (
	partLiteral = iota
	partZhuyin
	partKana
	partLatin
)

type inputPart struct {
	kind int
	text string
}

func splitParts(s string) []inputPart {
	var out []inputPart
	start := 0
	kind := partLiteral
	flush := func(end int) {
		if end > start {
			out = append(out, inputPart{kind: kind, text: s[start:end]})
		}
	}
	for i, r := range s {
		k := runeKind(r)
		if i == 0 {
			kind = k
			continue
		}
		if k != kind {
			flush(i)
			start = i
			kind = k
		}
	}
	if len(s) > 0 {
		flush(len(s))
	}
	return out
}

func runeKind(r rune) int {
	switch {
	case zhuyin.ClassOf(r) != zhuyin.None:
		return partZhuyin
	case unicode.In(r, unicode.Hiragana, unicode.Katakana) || r == 'ー' || r == 'ゝ' || r == 'ゞ':
		return partKana
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return partLatin
	case r >= 'Ａ' && r <= 'Ｚ', r >= 'ａ' && r <= 'ｚ', r >= '０' && r <= '９':
		return partLatin
	}
	return partLiteral
}

func (e *Engine) convertZhuyin(text string) []string {
	if e.zh == nil {
		return []string{text}
	}
	syls, ok := splitZhuyin(text)
	if !ok || len(syls) == 0 {
		return []string{text}
	}
	lat := input.Exact{}.Expand(syls)
	hyps, _ := e.zh.NBest(lat, nil, false, e.maxCand)
	out := make([]string, 0, len(hyps))
	for _, h := range hyps {
		if h.Text != "" {
			out = append(out, h.Text)
		}
	}
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

// splitZhuyin parses concatenated Bopomofo syllables. Spaces are only allowed
// between syllables; a new initial or a class that repeats always starts a new
// syllable.
func splitZhuyin(s string) ([]zhuyin.Syllable, bool) {
	var out []zhuyin.Syllable
	var cur strings.Builder
	last := zhuyin.None
	flush := func() bool {
		if cur.Len() == 0 {
			return true
		}
		syl := zhuyin.Syllable(cur.String())
		if !syl.Valid() {
			return false
		}
		out = append(out, syl)
		cur.Reset()
		last = zhuyin.None
		return true
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !flush() {
				return nil, false
			}
			continue
		}
		cl := zhuyin.ClassOf(r)
		if cl == zhuyin.None {
			return nil, false
		}
		if cur.Len() > 0 && cl != zhuyin.Tone && cl <= last {
			if !flush() {
				return nil, false
			}
		}
		cur.WriteRune(r)
		last = cl
		if cl == zhuyin.Tone {
			if !flush() {
				return nil, false
			}
		}
	}
	if !flush() {
		return nil, false
	}
	return out, true
}

func (e *Engine) convertLatin(text string) []string {
	units := composeLatin(text)
	if len(units) == 0 {
		return []string{text}
	}
	return e.convertJapaneseUnits(units)
}

// composeLatin uses the product's mixed romaji composer so English words stay
// literal while romaji becomes kana. Kana is handled by convertJapaneseUnits.
func composeLatin(text string) reading.Sequence {
	c := mixed.NewComposer(lang.InputMethod{ID: "romaji"}, mixed.DefaultConfig())
	for _, r := range text {
		c.Insert(string(r))
	}
	c.Flush()
	return c.Units()
}

func (e *Engine) convertJapaneseUnits(units reading.Sequence) []string {
	if len(units) == 0 {
		return nil
	}
	if e.ja == nil {
		return []string{unitsText(units)}
	}
	var groups [][]string
	for i := 0; i < len(units); {
		literal := literalUnit(units[i])
		j := i + 1
		for j < len(units) && literalUnit(units[j]) == literal {
			j++
		}
		run := units[i:j]
		if literal {
			groups = append(groups, []string{unitsText(run)})
		} else {
			groups = append(groups, e.convertJapaneseRun(run))
		}
		i = j
	}
	return combine(groups, e.maxCand)
}

func (e *Engine) convertJapaneseRun(run reading.Sequence) []string {
	results, err := e.ja.NBest(run, lang.Context{}, e.maxCand)
	if err != nil {
		return []string{unitsText(run)}
	}
	out := make([]string, 0, len(results))
	for _, res := range results {
		if s := jadecode.ResultSurface(res.Clauses); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{unitsText(run)}
	}
	return out
}

func literalUnit(u reading.Unit) bool {
	s := string(u)
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= unicode.MaxASCII || mixed.IsFullWidthAlnum(r) {
			continue
		}
		return false
	}
	return true
}

func runeUnits(s string) reading.Sequence {
	out := make(reading.Sequence, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, reading.Unit(string(r)))
	}
	return out
}

func unitsText(units reading.Sequence) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(string(u))
	}
	return b.String()
}

// combine builds deterministic candidate strings from per-run candidate lists.
// The first group varies slowest, so the all-top combination is always first.
func combine(groups [][]string, limit int) []string {
	if len(groups) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	out := []string{""}
	for _, g := range groups {
		if len(g) == 0 {
			g = []string{""}
		}
		next := make([]string, 0, min(limit, len(out)*len(g)))
		for _, prefix := range out {
			for _, s := range g {
				next = append(next, prefix+s)
				if len(next) >= limit {
					break
				}
			}
			if len(next) >= limit {
				break
			}
		}
		out = next
	}
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fileSHA(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// sortedKeys is a small helper used by reports and tests.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
