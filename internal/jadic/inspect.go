package jadic

import (
	"fmt"
	"io"
	"sort"
)

// DescribeOptions 是 inspect 報告的輸入。Manifest 可為 nil。
type DescribeOptions struct {
	PackPath     string
	ManifestPath string
	Manifest     *Manifest
	TopPOS       int
}

// Describe 把 pack 摘要寫成人類可讀報告。報告不含時間與絕對路徑以外的環境資訊，
// 同一份 pack 得到相同輸出。
func Describe(w io.Writer, p *Pack, opt DescribeOptions) error {
	if opt.TopPOS == 0 {
		opt.TopPOS = 10
	}
	h := p.Header()
	if _, err := fmt.Fprintf(w, "pack: %s\n", opt.PackPath); err != nil {
		return err
	}
	fmt.Fprintf(w, "bytes: %d\n", p.Bytes())
	fmt.Fprintf(w, "sha256: %s\n", p.FileSHA256())
	fmt.Fprintf(w, "payload_sha256: %s\n", p.PayloadSHA256())
	fmt.Fprintf(w, "format: 0c-jadic v%d；header_size=%d；flags=%#x\n", h.FormatVersion, h.HeaderSize, h.Flags)
	fmt.Fprintf(w, "counts: entries=%d surfaces=%d pos=%d conj_types=%d conj_forms=%d\n",
		h.EntryCount, h.SurfaceCount, h.POSCount, h.ConjTypeCount, h.ConjFormCount)
	fmt.Fprintf(w, "sections: surface=%d@%d reading=%d@%d entries=%d@%d index=%d@%d pos=%d@%d conj=%d@%d\n",
		h.SurfaceBlobLength, h.SurfaceBlobOffset,
		h.ReadingBlobLength, h.ReadingBlobOffset,
		h.EntriesLength, h.EntriesOffset,
		h.IndexLength, h.IndexOffset,
		h.POSLength, h.POSOffset,
		h.ConjLength, h.ConjOffset)

	st := p.Stats()
	fmt.Fprintf(w, "cost: min=%d max=%d negative=%d（越小越優先）\n", st.CostMin, st.CostMax, st.NegativeCosts)
	fmt.Fprintf(w, "split: C=%d A=%d B=%d\n", st.SplitCounts[0], st.SplitCounts[1], st.SplitCounts[2])
	fmt.Fprintf(w, "max entries per surface: %d；max reading units: %d\n", st.MaxReadings, st.MaxReadingUnit)

	posCounts := map[string]int{}
	conjTypes := map[string]int{}
	if err := p.ForEach(func(e Entry) error {
		posCounts[e.POS]++
		conjTypes[e.ConjType]++
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(w, "POS（前 %d 名，共 %d 種）：\n", opt.TopPOS, len(posCounts))
	for _, name := range topKeys(posCounts, opt.TopPOS) {
		fmt.Fprintf(w, "  %8d  %s\n", posCounts[name], name)
	}
	if len(conjTypes) > 1 {
		known := 0
		for name := range conjTypes {
			if name != "*" {
				known++
			}
		}
		fmt.Fprintf(w, "conjugation types: %d（不含 *）\n", known)
	}

	if opt.Manifest == nil {
		fmt.Fprintf(w, "manifest: 未提供（只驗證 pack 內部結構與 trailer）\n")
		return nil
	}
	fmt.Fprintf(w, "manifest: %s\n", opt.ManifestPath)
	fmt.Fprintf(w, "  sha256: %s\n", opt.Manifest.FileSHA256)
	fmt.Fprintf(w, "  schema: %s；lexicon: %s；target: %s\n",
		opt.Manifest.Schema, opt.Manifest.Lexicon, opt.Manifest.Target)
	fmt.Fprintf(w, "  pack_format: %s v%d；reading_script: %s\n",
		opt.Manifest.PackFormat.Name, opt.Manifest.PackFormat.Version, opt.Manifest.ReadingScript)
	if err := opt.Manifest.Verify(p); err != nil {
		fmt.Fprintf(w, "  verification: 失敗：%v\n", err)
		return nil
	}
	fmt.Fprintf(w, "  verification: 通過（schema、數量、整檔與 payload SHA-256 相符）\n")
	return nil
}

// DescribeSurface 印出一個 surface 的所有讀音與詞性。
func DescribeSurface(w io.Writer, p *Pack, surface string) error {
	entries := p.Lookup(surface)
	if len(entries) == 0 {
		_, err := fmt.Fprintf(w, "surface %q: 不在 pack 內\n", surface)
		return err
	}
	if _, err := fmt.Fprintf(w, "surface %q: %d 筆\n", surface, len(entries)); err != nil {
		return err
	}
	for i, e := range entries {
		if _, err := fmt.Fprintf(w, "  [%d] reading=%s cost=%d pos=%s conj=%s/%s flags=%#x split=%d\n",
			i, e.Reading, e.Cost, e.POS, e.ConjType, e.ConjForm, e.Flags, e.Split); err != nil {
			return err
		}
	}
	return nil
}

func topKeys(counts map[string]int, n int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}
