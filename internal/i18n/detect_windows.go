//go:build windows

package i18n

import (
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"golang.org/x/sys/windows"
)

// Detect returns the UI language that matches the user's Windows display
// language. It is only a starting suggestion for the first-run picker.
func Detect() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return model.LangEn
	}
	return Normalize(langs[0])
}
