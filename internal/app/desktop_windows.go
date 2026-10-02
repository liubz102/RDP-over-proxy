//go:build windows && !server

package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/liubz102/RDP-over-proxy/internal/i18n"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// buildDirs: the desktop build uses the real data folders.
func buildDirs(d store.Dirs) store.Dirs { return d }

// singleInstance keeps one copy of the app running; launching it again brings
// the existing window forward.
func singleInstance(show func()) *application.SingleInstanceOptions {
	return &application.SingleInstanceOptions{
		UniqueID: uniqueID,
		OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			show()
		},
	}
}

// preflight checks what Wails needs before it can show a window. Without the
// WebView2 Runtime Wails would exit silently, so explain instead.
func preflight(lang string) bool {
	if winx.WebView2Installed() {
		return true
	}
	winx.ShowError(i18n.T(lang, "webview2.missing.title"), i18n.T(lang, "webview2.missing.body"))
	return false
}

// reportStartupError shows a fatal error in a message box: the release build
// has no console, so a log line alone would never be seen.
func reportStartupError(lang string, err error) {
	winx.ShowError(i18n.T(lang, "startup.failed.title"), err.Error())
}

func windowBackground(theme string) application.RGBA {
	dark := theme == model.ThemeDark || (theme == model.ThemeSystem && winx.SystemPrefersDark())
	if dark {
		return application.NewRGB(41, 41, 41)
	}
	return application.NewRGB(250, 250, 250)
}
