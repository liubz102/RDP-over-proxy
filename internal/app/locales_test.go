package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/api"
	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/session"
)

// The Go side sends codes and message keys; the frontend translates them.
// This package links every package that declares codes, so it is where the
// whole list can be checked against both catalogs.
func TestEveryCodeIsTranslated(t *testing.T) {
	var want []string
	for _, code := range append(errcode.All(), api.CodeValidation) {
		want = append(want, "errors."+code)
	}
	for _, code := range api.NoticeCodes {
		want = append(want, "notices."+code)
	}
	for _, msg := range append(slices.Clone(session.Messages), api.Messages...) {
		want = append(want, "log."+msg)
	}
	// Engine and store codes come from their packages' declarations; make
	// sure those were linked rather than checking a short list.
	for _, code := range []string{"proxy.auth", "store.notFound", "probe.noAnswer", "net.refused"} {
		if !slices.Contains(want, "errors."+code) {
			t.Fatalf("%s was not declared; the list is incomplete: %q", code, want)
		}
	}
	for _, lang := range []string{"en", "zh-CN"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var catalog map[string]any
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatalf("%s.json: %v", lang, err)
		}
		have := map[string]bool{}
		flatten("", catalog, have)
		for _, key := range want {
			if !have[key] {
				t.Errorf("%s.json has no %q", lang, key)
			}
		}
	}
}

// flatten collects the dotted paths of the strings in a catalog.
func flatten(prefix string, v any, out map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			flatten(prefix+k+".", inner, out)
		}
	case string:
		out[prefix[:len(prefix)-1]] = true
	}
}
