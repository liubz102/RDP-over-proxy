package i18n

import (
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestCatalogsHaveTheSameKeys(t *testing.T) {
	for _, lang := range model.Languages {
		if _, ok := catalogs[lang]; !ok {
			t.Fatalf("no catalog for supported language %s", lang)
		}
	}
	for lang, cat := range catalogs {
		for other, otherCat := range catalogs {
			for key := range cat {
				if _, ok := otherCat[key]; !ok {
					t.Errorf("key %q exists in %s but not in %s", key, lang, other)
				}
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh-CN":      model.LangZhCN,
		"zh-Hans-CN": model.LangZhCN,
		"zh-TW":      model.LangZhCN,
		"ZH":         model.LangZhCN,
		"en-US":      model.LangEn,
		"ja-JP":      model.LangEn,
		"":           model.LangEn,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTFallsBack(t *testing.T) {
	if got := T("de", "tray.quit"); got != "Quit" {
		t.Errorf("unknown language should fall back to English, got %q", got)
	}
	if got := T(model.LangZhCN, "no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key should come back unchanged, got %q", got)
	}
}
