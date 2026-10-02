//go:build !windows

package i18n

import "github.com/liubz102/RDP-over-proxy/internal/model"

// Detect returns English on platforms other than Windows, where the
// application does not run; it exists so pure packages still build there.
func Detect() string { return model.LangEn }
