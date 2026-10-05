package i18n_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/i18n"

	"regexp"
	"slices"
	"strings"
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

// Every language of a string has the same placeholders, so Both fills in
// each of them.
func TestPlaceholdersMatch(t *testing.T) {
	placeholder := regexp.MustCompile(`\{[a-z]+\}`)
	for _, key := range Keys() {
		want := placeholder.FindAllString(T(model.LangEn, key), -1)
		slices.Sort(want)
		for _, lang := range model.Languages {
			got := placeholder.FindAllString(T(lang, key), -1)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s %s has placeholders %q, English %q", lang, key, got, want)
			}
		}
	}
}

func TestOrder(t *testing.T) {
	if got := Order("en-GB"); !slices.Equal(got, []string{model.LangEn, model.LangZhCN}) {
		t.Errorf("Order(en-GB) = %q", got)
	}
	if got := Order("zh-TW"); !slices.Equal(got, []string{model.LangZhCN, model.LangEn}) {
		t.Errorf("Order(zh-TW) = %q", got)
	}
}

func TestBoth(t *testing.T) {
	if got := Both("zh-Hans-CN", "folders.unusable.title", " / "); got != "无法保存数据 / Can't save data" {
		t.Errorf("Chinese first: %q", got)
	}
	got := Both("en-US", "folders.unusable.body", "\n\n", "{dir}", `D:\Tools\RDP-over-proxy\data`, "{reason}", "Access is denied.")
	en := strings.Index(got, "RDP over Proxy keeps")
	zh := strings.Index(got, "RDP over Proxy 把数据")
	if en != 0 || zh < 0 {
		t.Errorf("English first, then Chinese: %q", got)
	}
	if strings.Count(got, `D:\Tools\RDP-over-proxy\data`) != 2 || strings.Count(got, "Access is denied.") != 2 ||
		strings.Contains(got, "{") {
		t.Errorf("placeholders not filled in in both: %q", got)
	}
}
