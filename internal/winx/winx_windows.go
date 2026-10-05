//go:build windows

// Package winx wraps the few Win32 calls the application makes outside Wails.
package winx

import (
	"errors"
	"strings"

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
	return WebView2Version() != ""
}

// WebView2Version is the installed WebView2 Runtime's version, or "" when
// there is none.
func WebView2Version() string {
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
			return pv
		}
	}
	return ""
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

// Confirm asks in a modal message box with OK and Cancel, and reports
// whether the user chose OK. Like ShowError, it is for the time before the
// application window exists.
func Confirm(title, text string) bool {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	m, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	choice, _ := windows.MessageBox(0, m, t, windows.MB_OKCANCEL|windows.MB_ICONINFORMATION|windows.MB_SETFOREGROUND)
	return choice == idOK
}

// idOK is what MessageBox returns for the OK button.
const idOK = 1

// ErrorText is Windows' own text for the Windows error in err, in lang (a UI
// language: "zh-CN" or "en"), such as "拒绝访问。" for access denied. Go's
// err.Error() asks Windows for English text; a message shown in Chinese
// should give the reason in Chinese too. Without a Windows error inside, or
// without that language's text on this system, it is err.Error().
func ErrorText(err error, lang string) string {
	var errno windows.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	langID := uint32(0x0409) // en-US
	if lang == "zh-CN" {
		langID = 0x0804
	}
	buf := make([]uint16, 512)
	n, ferr := windows.FormatMessage(windows.FORMAT_MESSAGE_FROM_SYSTEM|windows.FORMAT_MESSAGE_IGNORE_INSERTS,
		0, uint32(errno), langID, buf, nil)
	if ferr != nil || n == 0 {
		return err.Error()
	}
	return strings.TrimSpace(windows.UTF16ToString(buf[:n]))
}
