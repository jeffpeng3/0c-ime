package jaimport

import (
	"strings"

	"github.com/0c-biggo/0c-ime/core/reading"
)

// ExpandConjugations 由辭書形產生常見活用形，讓讀音索引不必只依賴來源詞典
// 已列出的每一個活用。產生的詞條成本比原形高 generatedCostAdd，並標記
// rule:conjugation，供編譯報告與除錯辨識。
//
// 來源詞典（Mozc、SudachiDict）通常已收錄活用形；重複的詞條由 Merge 合併。
// 這個函式只處理能從辭書形與活用型可靠推導的形，不猜測不規則變化。
func ExpandConjugations(e Entry) []Entry {
	if e.Conjugation == ConjNone || e.Conjugation == ConjUnknown {
		return nil
	}
	if len(e.Reading) == 0 || len(e.Surface) == 0 {
		return nil
	}
	var out []Entry
	addStem := func(stemSurface string, stemReading reading.Sequence, suffixSurface string, suffixUnits ...reading.Unit) {
		r := append(reading.Sequence(nil), stemReading...)
		r = append(r, suffixUnits...)
		origins := append([]string(nil), e.Origins...)
		origins = append(origins, conjRulePrefix)
		out = append(out, MakeEntry(stemSurface+suffixSurface, r, e.Cost+generatedCostAdd, e.POS, origins, ConjNone))
	}

	switch e.Conjugation {
	case ConjGodan:
		last, stemSurface, stemReading := splitLastKana(e.Surface, e.Reading)
		row, ok := godanRows[last]
		if !ok {
			return nil
		}
		// 未然形＋ない、連用形＋ます、仮定形＋ば、意志形＋う、命令形。
		addStem(stemSurface, stemReading, row.a+"ない", readingUnits(row.a+"ない")...)
		addStem(stemSurface, stemReading, row.i+"ます", readingUnits(row.i+"ます")...)
		addStem(stemSurface, stemReading, row.e+"ば", readingUnits(row.e+"ば")...)
		addStem(stemSurface, stemReading, row.o+"う", readingUnits(row.o+"う")...)
		addStem(stemSurface, stemReading, row.e, readingUnits(row.e)...)
		if te, ok := godanTe(last); ok {
			// 行く是唯一常見的促音便例外：行って／行った，不是行いて。
			if e.Surface == "行く" || e.Surface == "逝く" || e.Surface == "往く" {
				te = "って"
			}
			addStem(stemSurface, stemReading, te, readingUnits(te)...)
			// 過去形與て形共用音便：書いて→書いた、読んで→読んだ。
			ta := strings.TrimSuffix(te, "て") + "た"
			addStem(stemSurface, stemReading, ta, readingUnits(ta)...)
		}
	case ConjIchidan:
		_, stemSurface, stemReading := splitLastKana(e.Surface, e.Reading) // 去る
		if !strings.HasSuffix(e.Surface, "る") || len(e.Reading) < 1 || e.Reading[len(e.Reading)-1] != "る" {
			return nil
		}
		addStem(stemSurface, stemReading, "ない", readingUnits("ない")...)
		addStem(stemSurface, stemReading, "ます", readingUnits("ます")...)
		addStem(stemSurface, stemReading, "た", readingUnits("た")...)
		addStem(stemSurface, stemReading, "て", readingUnits("て")...)
		addStem(stemSurface, stemReading, "れば", readingUnits("れば")...)
		addStem(stemSurface, stemReading, "よう", readingUnits("よう")...)
		addStem(stemSurface, stemReading, "ろ", readingUnits("ろ")...)
		addStem(stemSurface, stemReading, "られる", readingUnits("られる")...)
		addStem(stemSurface, stemReading, "させる", readingUnits("させる")...)
	case ConjSuru:
		stemSurface, ok := strings.CutSuffix(e.Surface, "する")
		if !ok || len(e.Reading) < 2 {
			return nil
		}
		stemReading := e.Reading[:len(e.Reading)-2]
		addStem(stemSurface, stemReading, "しない", readingUnits("しない")...)
		addStem(stemSurface, stemReading, "します", readingUnits("します")...)
		addStem(stemSurface, stemReading, "した", readingUnits("した")...)
		addStem(stemSurface, stemReading, "して", readingUnits("して")...)
		addStem(stemSurface, stemReading, "すれば", readingUnits("すれば")...)
		addStem(stemSurface, stemReading, "しよう", readingUnits("しよう")...)
		addStem(stemSurface, stemReading, "しろ", readingUnits("しろ")...)
	case ConjKuru:
		if !strings.HasSuffix(e.Surface, "来る") || len(e.Reading) < 2 {
			return nil
		}
		stemSurface := strings.TrimSuffix(e.Surface, "来る")
		stemReading := e.Reading[:len(e.Reading)-2]
		addStem(stemSurface, stemReading, "来ない", readingUnits("こない")...)
		addStem(stemSurface, stemReading, "来ます", readingUnits("きます")...)
		addStem(stemSurface, stemReading, "来た", readingUnits("きた")...)
		addStem(stemSurface, stemReading, "来て", readingUnits("きて")...)
		addStem(stemSurface, stemReading, "来れば", readingUnits("くれば")...)
		addStem(stemSurface, stemReading, "来よう", readingUnits("こよう")...)
		addStem(stemSurface, stemReading, "来い", readingUnits("こい")...)
	case ConjIAdjective:
		if !strings.HasSuffix(e.Surface, "い") || len(e.Reading) < 1 || e.Reading[len(e.Reading)-1] != "い" {
			return nil
		}
		stemSurface := strings.TrimSuffix(e.Surface, "い")
		stemReading := e.Reading[:len(e.Reading)-1]
		for _, f := range []struct{ surface, kana string }{
			{"く", "く"}, {"くない", "くない"}, {"かった", "かった"},
			{"くなかった", "くなかった"}, {"ければ", "ければ"}, {"くて", "くて"},
			{"さ", "さ"}, {"そう", "そう"},
		} {
			addStem(stemSurface, stemReading, f.surface, readingUnits(f.kana)...)
		}
	case ConjNaAdjective:
		for _, f := range []struct{ surface, kana string }{
			{"だ", "だ"}, {"な", "な"}, {"に", "に"}, {"で", "で"},
			{"じゃない", "じゃない"}, {"だった", "だった"},
		} {
			addStem(e.Surface, e.Reading, f.surface, readingUnits(f.kana)...)
		}
	}
	return out
}

type godanRow struct{ a, i, e, o string }

var godanRows = map[string]godanRow{
	"う": {"わ", "い", "え", "お"},
	"く": {"か", "き", "け", "こ"},
	"ぐ": {"が", "ぎ", "げ", "ご"},
	"す": {"さ", "し", "せ", "そ"},
	"つ": {"た", "ち", "て", "と"},
	"ぬ": {"な", "に", "ね", "の"},
	"ぶ": {"ば", "び", "べ", "ぼ"},
	"む": {"ま", "み", "め", "も"},
	"る": {"ら", "り", "れ", "ろ"},
}

func godanTe(last string) (string, bool) {
	switch last {
	case "う", "つ", "る":
		return "って", true
	case "む", "ぶ", "ぬ":
		return "んで", true
	case "く":
		return "いて", true
	case "ぐ":
		return "いで", true
	case "す":
		return "して", true
	}
	return "", false
}

// splitLastKana 把詞面與讀音各去掉最後一個かな。詞面結尾不是かな時回傳空值。
func splitLastKana(surface string, seq reading.Sequence) (string, string, reading.Sequence) {
	rs := []rune(surface)
	if len(rs) == 0 || len(seq) == 0 {
		return "", "", nil
	}
	last := string(rs[len(rs)-1])
	if !isKana(rs[len(rs)-1]) {
		return "", "", nil
	}
	return last, string(rs[:len(rs)-1]), append(reading.Sequence(nil), seq[:len(seq)-1]...)
}

func readingUnits(s string) reading.Sequence {
	seq, _ := NormalizeReading(s)
	return seq
}
