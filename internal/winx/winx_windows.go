//go:build windows

// Package winx wraps the few Win32 calls the application makes outside Wails.
package winx

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// webView2ClientKey is the EdgeUpdate client key of the Evergreen WebView2
// Runtime, as documented by Microsoft for detecting an installed runtime.
const webView2ClientKey = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// WebView2Installed reports whether the WebView2 Runtime is installed for the
// machine (64- or 32-bit registry view) or for the current user. Without it
// Wails exits without showing anything, so the app checks first and explains.
func WebView2Installed() bool {
	candidates := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\` + webView2ClientKey},
		{registry.LOCAL_MACHINE, `SOFTWARE\` + webView2ClientKey},
		{registry.CURRENT_USER, `Software\` + webView2ClientKey},
	}
	for _, c := range candidates {
		k, err := registry.OpenKey(c.root, c.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		pv, _, err := k.GetStringValue("pv")
		_ = k.Close()
		if err == nil && pv != "" && pv != "0.0.0.0" {
			return true
		}
	}
	return false
}

// SystemPrefersDark reports whether Windows is set to dark mode for apps. It
// reads the registry directly so it works before the Wails application runs.
func SystemPrefersDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	light, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && light == 0
}

// ShowError shows a modal error message box. It is for failures that happen
// before the application window exists, or when it never will.
func ShowError(title, text string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	m, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
