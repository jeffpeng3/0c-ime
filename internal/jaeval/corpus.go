package jaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// CorpusSchema is the version of the mixed Japanese evaluation corpus.
const CorpusSchema = 1

// Categories are the dimensions every committed case must declare. They exist
// so the report can separate pure romaji from mixed zhuyin+romaji and from
// literal English/number noise.
var Categories = map[string]bool{
	"ja-word":         true,
	"ja-okurigana":    true,
	"ja-particle":     true,
	"ja-sentence":     true,
	"katakana":        true,
	"mixed-zh-ja":     true,
	"ja-english":      true,
	"numbers-symbols": true,
	"punctuation":     true,
}

// Corpus is the committed set of mixed input strings. The file order is the
// stable iteration order used by the table, snapshot and report.
type Corpus struct {
	Schema      int    `json:"schema"`
	Name        string `json:"name"`
	License     string `json:"license"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
	Count       int    `json:"count"`
	Cases       []Case `json:"cases"`
}

// Case is one input string and the surface a human expects the single Mixed
// profile to choose first. Input may contain romaji, kana, zhuyin, English,
// digits and punctuation; whitespace is preserved literally.
type Case struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Input    string `json:"input"`
	Expect   string `json:"expect"`
	Note     string `json:"note,omitempty"`
}

// LoadCorpus reads and validates a corpus file, returning its SHA-256 as well.
func LoadCorpus(path string) (*Corpus, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var c Corpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return &c, hex.EncodeToString(sum[:]), nil
}

// Validate checks the corpus without reading any dictionary data.
func (c *Corpus) Validate() error {
	if c.Schema != CorpusSchema {
		return fmt.Errorf("schema %d, want %d", c.Schema, CorpusSchema)
	}
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.License) == "" || strings.TrimSpace(c.Source) == "" {
		return fmt.Errorf("name, license and source are required")
	}
	if len(c.Cases) == 0 {
		return fmt.Errorf("no cases")
	}
	if c.Count != 0 && c.Count != len(c.Cases) {
		return fmt.Errorf("count %d does not match %d cases", c.Count, len(c.Cases))
	}
	seen := map[string]bool{}
	for i, cs := range c.Cases {
		where := fmt.Sprintf("case %d", i+1)
		if cs.ID == "" {
			return fmt.Errorf("%s: id is required", where)
		}
		if seen[cs.ID] {
			return fmt.Errorf("duplicate case id %q", cs.ID)
		}
		seen[cs.ID] = true
		if !Categories[cs.Category] {
			return fmt.Errorf("case %q: unknown category %q", cs.ID, cs.Category)
		}
		if strings.TrimSpace(cs.Input) == "" {
			return fmt.Errorf("case %q: input is empty", cs.ID)
		}
		if cs.Expect == "" {
			return fmt.Errorf("case %q: expect is empty", cs.ID)
		}
	}
	return nil
}

// CategoryNames returns the declared categories in sorted order, for reports.
func CategoryNames() []string {
	names := make([]string, 0, len(Categories))
	for name := range Categories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
