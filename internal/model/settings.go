// Package model holds the data the application stores and edits: proxies,
// connection profiles and settings. It is pure logic with no OS calls, so
// everything here can be unit-tested anywhere.
package model

import (
	"fmt"
	"slices"
)

// SettingsSchema is the data-format number written into settings.json. It is a
// file-format marker for migrations, not the application version.
const SettingsSchema = 1

// Supported UI languages.
const (
	LangZhCN = "zh-CN"
	LangEn   = "en"
)

// Languages lists the UI languages in the order the language picker shows them.
var Languages = []string{LangZhCN, LangEn}

// Themes.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// What closing the main window does. While a remote desktop session is using a
// tunnel the window always hides to the tray, whatever this says, because
// quitting would cut the session.
const (
	CloseToTray = "tray"
	CloseQuit   = "quit"
)

// DefaultLocalPort is the port the per-connection loopback tunnels listen on.
const DefaultLocalPort = 13389

// DefaultTestURL is fetched through a proxy to measure its latency.
const DefaultTestURL = "https://www.bing.com"

// Log levels.
const (
	LogDebug = "debug"
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

// Settings are the user's application preferences (settings.json).
type Settings struct {
	Schema int `json:"schema"`
	// Language is empty until the user picks one on first launch; the UI then
	// shows the language picker instead of the main window.
	Language                string `json:"language"`
	Theme                   string `json:"theme"`
	CloseBehavior           string `json:"closeBehavior"`
	LocalPort               int    `json:"localPort"`
	CheckRouteBeforeConnect bool   `json:"checkRouteBeforeConnect"`
	TestURL                 string `json:"testUrl"`
	LogLevel                string `json:"logLevel"`
}

// DefaultSettings returns the settings used before the user changes anything.
func DefaultSettings() Settings {
	return Settings{
		Schema:                  SettingsSchema,
		Language:                "",
		Theme:                   ThemeSystem,
		CloseBehavior:           CloseToTray,
		LocalPort:               DefaultLocalPort,
		CheckRouteBeforeConnect: true,
		TestURL:                 DefaultTestURL,
		LogLevel:                LogInfo,
	}
}

// Normalize fills fields that are missing or unknown (for example a file
// written by an older build) with their defaults. Language is left alone: an
// unknown value is reported by Validate rather than silently replaced.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	s.Schema = SettingsSchema
	if !slices.Contains([]string{ThemeSystem, ThemeLight, ThemeDark}, s.Theme) {
		s.Theme = d.Theme
	}
	if !slices.Contains([]string{CloseToTray, CloseQuit}, s.CloseBehavior) {
		s.CloseBehavior = d.CloseBehavior
	}
	if s.LocalPort == 0 {
		s.LocalPort = d.LocalPort
	}
	if s.TestURL == "" {
		s.TestURL = d.TestURL
	}
	if !slices.Contains([]string{LogDebug, LogInfo, LogWarn, LogError}, s.LogLevel) {
		s.LogLevel = d.LogLevel
	}
	return s
}

// Repair is Normalize plus a fix-up for values Validate would reject. It is
// used when reading settings.json, which may have been hand-edited or written
// by a different build: an unknown language is cleared (so the language picker
// shows again) and an out-of-range port goes back to the default.
func (s Settings) Repair() Settings {
	s = s.Normalize()
	if s.Language != "" && !slices.Contains(Languages, s.Language) {
		s.Language = ""
	}
	if s.LocalPort < 1 || s.LocalPort > 65535 {
		s.LocalPort = DefaultLocalPort
	}
	return s
}

// Validate reports the first field that cannot be saved as is.
func (s Settings) Validate() error {
	if s.Language != "" && !slices.Contains(Languages, s.Language) {
		return fmt.Errorf("unsupported language %q", s.Language)
	}
	if s.LocalPort < 1 || s.LocalPort > 65535 {
		return fmt.Errorf("local port %d is outside 1-65535", s.LocalPort)
	}
	return nil
}
