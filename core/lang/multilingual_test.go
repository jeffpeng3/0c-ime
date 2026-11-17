package lang_test

import (
	"testing"

	"github.com/0c-biggo/0c-ime/core/lang"
)

func TestOutputVariantLanguageAndDefault(t *testing.T) {
	tests := []struct {
		variant lang.OutputVariant
		want    lang.ID
		valid   bool
	}{
		{lang.VariantZhHant, lang.Zh, true},
		{lang.VariantZhHans, lang.Zh, true},
		{lang.VariantJaShinjitai, lang.Ja, true},
		{lang.VariantJaKyujitai, lang.Ja, true},
		{lang.VariantEnglish, lang.En, true},
		{lang.VariantJa, lang.Ja, true},
		{lang.VariantEn, lang.En, true},
		{"", lang.Und, false},
		{"zh", lang.Und, false},
	}
	for _, tt := range tests {
		if got := tt.variant.Language(); got != tt.want {
			t.Errorf("%q.Language() = %q, want %q", tt.variant, got, tt.want)
		}
		if got := tt.variant.Valid(); got != tt.valid {
			t.Errorf("%q.Valid() = %v, want %v", tt.variant, got, tt.valid)
		}
	}
}

func TestDefaultVariant(t *testing.T) {
	tests := []struct {
		id   lang.ID
		want lang.OutputVariant
	}{
		{lang.Zh, lang.VariantZhHant},
		{lang.Ja, lang.VariantJaShinjitai},
		{lang.En, lang.VariantEnglish},
		{lang.Und, ""},
		{"ko", ""},
	}
	for _, tt := range tests {
		if got := lang.DefaultVariant(tt.id); got != tt.want {
			t.Errorf("DefaultVariant(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestLanguageTag(t *testing.T) {
	zero := lang.LanguageTag{}
	if !zero.IsZero() || zero.Valid() {
		t.Fatalf("zero tag: IsZero=%v Valid=%v, want true false", zero.IsZero(), zero.Valid())
	}
	if got := zero.String(); got != "und" {
		t.Errorf("zero.String() = %q, want und", got)
	}

	tag := lang.Tag(lang.Ja)
	if tag.IsZero() || !tag.Valid() || tag.Language != lang.Ja || tag.Confidence != 0 {
		t.Fatalf("Tag(Ja) = %#v", tag)
	}

	tag = tag.WithConfidence(1.5)
	if tag.Confidence != 1 {
		t.Errorf("confidence > 1 clamped to %v, want 1", tag.Confidence)
	}
	tag = tag.WithConfidence(-1)
	if tag.Confidence != 0 {
		t.Errorf("confidence < 0 clamped to %v, want 0", tag.Confidence)
	}

	if bad := (lang.LanguageTag{Language: " "}).Valid(); bad {
		t.Error("空白語言 ID 應為無效")
	}
	if got := (lang.LanguageTag{Language: lang.En, Confidence: 0.75}).String(); got != "en" {
		t.Errorf("String() = %q, want en", got)
	}
}
