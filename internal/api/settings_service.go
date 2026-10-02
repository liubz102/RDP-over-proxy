// Package api is the layer the frontend talks to. Wails turns the exported
// methods of these services into TypeScript functions (frontend/bindings).
// Services stay thin: they call into the internal packages, broadcast changes
// as events and leave decisions to the packages that own them.
package api

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// EventSettingsChanged carries the new model.Settings every time they are saved,
// so every open view (and a second window, should one appear) stays in step.
const EventSettingsChanged = "settings:changed"

func init() {
	application.RegisterEvent[model.Settings](EventSettingsChanged)
}

// AppInfo describes the running build for the About section.
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Repo    string `json:"repo"`
}

// SettingsService reads and saves the user's preferences.
type SettingsService struct {
	store    *store.SettingsStore
	info     AppInfo
	detect   func() string
	onChange func(model.Settings)
}

// NewSettingsService returns the service. detect suggests a UI language from
// the system; onChange lets the shell react to saved settings (tray labels).
func NewSettingsService(st *store.SettingsStore, info AppInfo, detect func() string, onChange func(model.Settings)) *SettingsService {
	return &SettingsService{store: st, info: info, detect: detect, onChange: onChange}
}

// Get returns the settings in effect. An empty Language means the user has not
// picked one yet; the UI then shows the language picker first.
func (s *SettingsService) Get() model.Settings {
	return s.store.Get()
}

// Save validates and stores new settings, then tells the shell and every
// window about them. It returns the settings as stored (defaults filled in).
func (s *SettingsService) Save(in model.Settings) (model.Settings, error) {
	if err := s.store.Save(in); err != nil {
		return model.Settings{}, err
	}
	out := s.store.Get()
	if s.onChange != nil {
		s.onChange(out)
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(EventSettingsChanged, out)
	}
	return out, nil
}

// SystemLanguage suggests the UI language that matches the Windows display
// language: "zh-CN" for any Chinese variant, otherwise "en".
func (s *SettingsService) SystemLanguage() string {
	return s.detect()
}

// AppInfo returns the product name, version and repository URL.
func (s *SettingsService) AppInfo() AppInfo {
	return s.info
}
