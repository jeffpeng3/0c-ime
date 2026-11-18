package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	jalexicon "github.com/0c-biggo/0c-ime/core/ja/lexicon"
)

// 任何 lexicon.bin 的開啟或解碼失敗都必須退回 TSV；成功載入時不可被 TSV 蓋掉。
func TestLoadJaDecoderFallsBackOnBadBin(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(t *testing.T, path string, valid []byte)
		wantBin  bool
		wantWarn int
	}{
		{
			name: "missing file",
			mutate: func(t *testing.T, path string, _ []byte) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "empty file",
			mutate: func(t *testing.T, path string, _ []byte) {
				t.Helper()
				writeFile(t, path, nil)
			},
			wantWarn: 1,
		},
		{
			name: "truncated header",
			mutate: func(t *testing.T, path string, valid []byte) {
				t.Helper()
				writeFile(t, path, valid[:4])
			},
			wantWarn: 1,
		},
		{
			name: "truncated body",
			mutate: func(t *testing.T, path string, valid []byte) {
				t.Helper()
				writeFile(t, path, valid[:len(valid)/2])
			},
			wantWarn: 1,
		},
		{
			name: "bad magic",
			mutate: func(t *testing.T, path string, valid []byte) {
				t.Helper()
				bad := append([]byte(nil), valid...)
				bad[0] ^= 0xff
				writeFile(t, path, bad)
			},
			wantWarn: 1,
		},
		{
			name:    "valid file",
			mutate:  func(*testing.T, string, []byte) {},
			wantBin: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldWarn := jaLoadWarnf
			var warnings []string
			jaLoadWarnf = func(format string, args ...any) {
				warnings = append(warnings, fmt.Sprintf(format, args...))
			}
			t.Cleanup(func() { jaLoadWarnf = oldWarn })

			dir := t.TempDir()
			writeJaPack(t, dir)
			path := filepath.Join(dir, "lexicon.bin")
			valid, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, path, valid)

			dec, lex, conn, err := loadJaDecoder(dir)
			if err != nil {
				t.Fatalf("loadJaDecoder: %v", err)
			}
			if dec == nil || conn == nil {
				t.Fatal("decoder 或連接矩陣是 nil")
			}
			if tc.wantBin {
				if _, ok := lex.(*jalexicon.Bin); !ok {
					t.Fatalf("lexicon = %T, want *lexicon.Bin", lex)
				}
			} else if _, ok := lex.(*jalexicon.Mem); !ok {
				t.Fatalf("lexicon = %T, want *lexicon.Mem", lex)
			}
			if len(warnings) != tc.wantWarn {
				t.Fatalf("警告行數 = %d %q, want %d", len(warnings), warnings, tc.wantWarn)
			}
			if got := convert(t, dec); got != "今日は" {
				t.Fatalf("轉換 = %q, want 今日は", got)
			}
		})
	}
}

// 二進位檔成功載入時，即使 TSV 內容不同也必須使用二進位結果。
func TestLoadJaDecoderKeepsSuccessfulBinWhenTSVDiffers(t *testing.T) {
	dir := t.TempDir()
	writeJaPack(t, dir)
	tsv := "違\tちが\t1\tnoun\n"
	writeFile(t, filepath.Join(dir, "lexicon", "words.tsv"), []byte(tsv))

	_, lex, _, err := loadJaDecoder(dir)
	if err != nil {
		t.Fatal(err)
	}
	bin, ok := lex.(*jalexicon.Bin)
	if !ok {
		t.Fatalf("lexicon = %T, want *lexicon.Bin", lex)
	}
	if len(bin.Lookup("今日")) == 0 {
		t.Fatal("成功載入的 lexicon.bin 被 TSV 蓋掉")
	}
	if bin.Has("違") {
		t.Fatal("使用了 TSV 的資料")
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
