package i18n_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/i18n"

	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestEveryLanguageHasEveryString(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("no strings at all")
	}
	for _, lang := range model.Languages {
		for _, key := range keys {
			if s, ok := Lookup(lang, key); !ok || s == "" {
				t.Errorf("%s has no %q", lang, key)
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
