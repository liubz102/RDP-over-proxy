// Package i18n holds the few strings the Go side shows itself: the tray menu
// and native message boxes. Everything else is translated in the frontend
// (frontend/src/locales).
package i18n

import (
	"maps"
	"slices"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

var catalogs = map[string]map[string]string{
	model.LangZhCN: {
		"tray.show":              "显示主窗口",
		"tray.quit":              "退出",
		"webview2.missing.title": "缺少 WebView2 运行时",
		"webview2.missing.body": "RDP over Proxy 需要 Microsoft Edge WebView2 运行时来显示界面，但这台电脑上没有找到它。\n\n" +
			"请从微软官网下载安装后再运行：\nhttps://developer.microsoft.com/microsoft-edge/webview2/",
		"startup.failed.title": "RDP over Proxy 无法启动",
	},
	model.LangEn: {
		"tray.show":              "Show window",
		"tray.quit":              "Quit",
		"webview2.missing.title": "WebView2 Runtime is missing",
		"webview2.missing.body": "RDP over Proxy needs the Microsoft Edge WebView2 Runtime to show its window, and it isn't installed on this PC.\n\n" +
			"Install it from Microsoft, then start the app again:\nhttps://developer.microsoft.com/microsoft-edge/webview2/",
		"startup.failed.title": "RDP over Proxy couldn't start",
	},
}

// Normalize maps a language tag such as "zh-Hans-CN" or "en-US" to one of the
// supported UI languages. Any Chinese variant becomes simplified Chinese;
// everything else becomes English.
func Normalize(tag string) string {
	if strings.HasPrefix(strings.ToLower(tag), "zh") {
		return model.LangZhCN
	}
	return model.LangEn
}

// T returns the string for key in lang, falling back to English and then to
// the key itself.
func T(lang, key string) string {
	if s, ok := catalogs[Normalize(lang)][key]; ok {
		return s
	}
	if s, ok := catalogs[model.LangEn][key]; ok {
		return s
	}
	return key
}

// Lookup returns the string for key in lang (a supported language, not a
// tag), without falling back. It is for checking that every language has
// every string; the app itself uses T.
func Lookup(lang, key string) (string, bool) {
	s, ok := catalogs[lang][key]
	return s, ok
}

// Keys lists every key of every language, sorted, for checking that each
// language has them all.
func Keys() []string {
	set := map[string]bool{}
	for _, cat := range catalogs {
		for key := range cat {
			set[key] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}
