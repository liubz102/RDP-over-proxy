package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// ErrSettingsRecovered is returned by Load when settings.json could not be
// read. The unreadable file is kept next to it as settings.json.corrupt and
// the defaults are used instead, so the application still starts.
var ErrSettingsRecovered = errors.New("settings.json was unreadable and has been reset")

// SettingsStore loads and saves settings.json and keeps the current value in
// memory for cheap reads.
type SettingsStore struct {
	path string

	mu      sync.Mutex
	current model.Settings
}

// NewSettingsStore returns a store for settings.json inside configDir.
// Call Load before Get.
func NewSettingsStore(configDir string) *SettingsStore {
	return &SettingsStore{
		path:    filepath.Join(configDir, "settings.json"),
		current: model.DefaultSettings(),
	}
}

// Path is the location of settings.json.
func (s *SettingsStore) Path() string { return s.path }

// Load reads settings.json. A missing file yields the defaults. An unreadable
// file is moved aside and the defaults are used; Load then returns the
// defaults together with an error wrapping ErrSettingsRecovered.
func (s *SettingsStore) Load() (model.Settings, error) {
	// Decode on top of the defaults: a field missing from the file (one added
	// after the file was written) keeps its default instead of the zero value.
	loaded := model.DefaultSettings()
	found, err := ReadJSON(s.path, &loaded)
	var result model.Settings
	switch {
	case err != nil:
		result = model.DefaultSettings()
		if moveErr := os.Rename(s.path, s.path+".corrupt"); moveErr != nil {
			err = errors.Join(err, moveErr)
		}
		err = fmt.Errorf("%w: %w", ErrSettingsRecovered, err)
	case !found:
		result = model.DefaultSettings()
	default:
		result = loaded.Repair()
	}
	s.mu.Lock()
	s.current = result
	s.mu.Unlock()
	return result, err
}

// Get returns the settings currently in effect.
func (s *SettingsStore) Get() model.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Save validates v, writes it to disk and makes it current.
func (s *SettingsStore) Save(v model.Settings) error {
	v = v.Normalize()
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := WriteJSONAtomic(s.path, v); err != nil {
		return err
	}
	s.current = v
	return nil
}
