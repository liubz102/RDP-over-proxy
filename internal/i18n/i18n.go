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
		"startup.failed.title":  "RDP over Proxy 无法启动",
		"folders.elevate.title": "需要管理员权限",
		"folders.elevate.body": "RDP over Proxy 把设置、代理、连接和日志都保存在程序所在的文件夹里：\n{dir}\n\n" +
			"在这里写入需要管理员权限。点「确定」后 Windows 会请求管理员权限，只用来建立 data 和 logs 两个文件夹，" +
			"并允许你的账户写入它们。程序本身仍以普通权限运行，以后启动也不会再请求。\n\n" +
			"不想这样的话，点「取消」，把程序文件夹移到不需要管理员权限的地方（不在 Program Files 里）再启动。",
		"folders.declined.body": "没有得到管理员权限，RDP over Proxy 无法在这里保存数据：\n{dir}\n\n" +
			"请把程序文件夹移到不需要管理员权限的地方再启动；或者重新启动，在 Windows 询问时选「是」。",
		"folders.unusable.title": "无法保存数据",
		"folders.unusable.body": "RDP over Proxy 把数据保存在程序所在的文件夹里，但没能在这里写入：\n{dir}\n原因：{reason}\n\n" +
			"请把程序文件夹移到可以写入的地方（不在只读的盘或网络共享上）再启动。",
	},
	model.LangEn: {
		"tray.show":              "Show window",
		"tray.quit":              "Quit",
		"webview2.missing.title": "WebView2 Runtime is missing",
		"webview2.missing.body": "RDP over Proxy needs the Microsoft Edge WebView2 Runtime to show its window, and it isn't installed on this PC.\n\n" +
			"Install it from Microsoft, then start the app again:\nhttps://developer.microsoft.com/microsoft-edge/webview2/",
		"startup.failed.title":  "RDP over Proxy couldn't start",
		"folders.elevate.title": "Administrator rights needed",
		"folders.elevate.body": "RDP over Proxy keeps its settings, proxies, connections and logs in the folder it runs from:\n{dir}\n\n" +
			"Writing there needs administrator rights. When you choose OK, Windows asks for them. They are used only to create " +
			"the data and logs folders and let your account write to them; the app itself keeps running without them and won't ask again.\n\n" +
			"If you'd rather not, choose Cancel and move the app's folder somewhere that doesn't need administrator rights " +
			"(not under Program Files), then start it again.",
		"folders.declined.body": "Without administrator rights, RDP over Proxy can't save its data here:\n{dir}\n\n" +
			"Move the app's folder somewhere that doesn't need administrator rights, or start the app again and choose Yes when Windows asks.",
		"folders.unusable.title": "Can't save data",
		"folders.unusable.body": "RDP over Proxy keeps its data in the folder it runs from, but it can't write here:\n{dir}\nReason: {reason}\n\n" +
			"Move the app's folder somewhere you can write to (not a read-only drive or a network share), then start it again.",
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

// Both is the string for key in every UI language, joined by sep, the
// language of first (a tag) first, with the placeholders replaced: replace
// lists them in pairs, such as "{dir}", `D:\Tools`. It is for messages shown
// before the user's choice of language can be known.
func Both(first, key, sep string, replace ...string) string {
	r := strings.NewReplacer(replace...)
	langs := Order(first)
	parts := make([]string, len(langs))
	for i, lang := range langs {
		parts[i] = r.Replace(T(lang, key))
	}
	return strings.Join(parts, sep)
}

// Order lists the UI languages with the language of first (a tag) first.
func Order(first string) []string {
	first = Normalize(first)
	langs := []string{first}
	for _, lang := range model.Languages {
		if lang != first {
			langs = append(langs, lang)
		}
	}
	return langs
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
