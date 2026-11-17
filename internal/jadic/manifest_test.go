package jadic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleManifest(p *Pack) *Manifest {
	return &Manifest{
		Schema:        ManifestSchema,
		Lexicon:       "public-core",
		Target:        "public-core-pack",
		PackFormat:    PackFormat{Name: "0c-jadic", Version: FormatVersion},
		ReadingScript: "hiragana",
		Counts: ManifestCounts{
			Entries:   int64(p.EntryCount()),
			Surfaces:  int64(p.SurfaceCount()),
			POS:       int64(len(p.POSList())),
			ConjTypes: int64(len(p.ConjTypes())),
			ConjForms: int64(len(p.ConjForms())),
		},
		Outputs: ManifestOutputs{Pack: ManifestPack{
			Path:          "ja-lexicon-public-core.pack",
			Bytes:         p.Bytes(),
			SHA256:        p.FileSHA256(),
			PayloadSHA256: p.PayloadSHA256(),
		}},
		FileSHA256: "deadbeef",
	}
}

func TestManifestVerify(t *testing.T) {
	p, err := Parse(samplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := sampleManifest(p).Verify(p); err != nil {
		t.Fatal(err)
	}

	// katakana pack 可接受，其他 script 不行。
	m := sampleManifest(p)
	m.ReadingScript = "katakana"
	if err := m.Verify(p); err != nil {
		t.Fatalf("katakana 應接受：%v", err)
	}
}

func TestManifestVerifyRejectsMismatch(t *testing.T) {
	p, err := Parse(samplePack(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{"schema", func(m *Manifest) { m.Schema = "0c-lexicon-manifest/2" }, "schema"},
		{"format_name", func(m *Manifest) { m.PackFormat.Name = "0c-jadic-next" }, "pack_format.name"},
		{"format_version", func(m *Manifest) { m.PackFormat.Version = 2 }, "pack_format.version"},
		{"reading_script", func(m *Manifest) { m.ReadingScript = "romaji" }, "reading_script"},
		{"entries", func(m *Manifest) { m.Counts.Entries++ }, "counts.entries"},
		{"surfaces", func(m *Manifest) { m.Counts.Surfaces++ }, "counts.surfaces"},
		{"pos", func(m *Manifest) { m.Counts.POS++ }, "counts.pos"},
		{"conj_types", func(m *Manifest) { m.Counts.ConjTypes++ }, "counts.conj_types"},
		{"conj_forms", func(m *Manifest) { m.Counts.ConjForms++ }, "counts.conj_forms"},
		{"bytes", func(m *Manifest) { m.Outputs.Pack.Bytes++ }, "outputs.pack.bytes"},
		{"file_sha", func(m *Manifest) { m.Outputs.Pack.SHA256 = strings.Repeat("0", 64) }, "整檔 SHA-256"},
		{"payload_sha", func(m *Manifest) { m.Outputs.Pack.PayloadSHA256 = strings.Repeat("0", 64) }, "payload SHA-256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleManifest(p)
			tc.edit(m)
			err := m.Verify(p)
			if err == nil {
				t.Fatal("預期驗證失敗")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("錯誤 %q 未包含 %q", err, tc.want)
			}
		})
	}
}

func TestLoadManifestRecordsFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	body := []byte(`{
	  "schema": "0c-lexicon-manifest/1",
	  "lexicon": "public-core",
	  "target": "public-core-pack",
	  "pack_format": {"name": "0c-jadic", "version": 1},
	  "reading_script": "hiragana",
	  "counts": {"entries": 5, "surfaces": 4, "pos": 2, "conj_types": 2, "conj_forms": 2},
	  "outputs": {"pack": {"path": "x", "bytes": 1, "sha256": "aa", "payload_sha256": "bb"}}
	}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.FileSHA256) != 64 || m.Lexicon != "public-core" || m.Counts.Entries != 5 {
		t.Fatalf("manifest = %+v", m)
	}
	if _, err := LoadManifest(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("缺檔應失敗")
	}
}
