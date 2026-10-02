// Package store keeps the application's data on disk: one JSON file per
// entity, written atomically so a crash or power loss never leaves a
// half-written file behind.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// AppDirName is the folder name used under %APPDATA% and %LOCALAPPDATA%.
const AppDirName = "RDP-over-proxy"

// Dirs are the two places the application keeps files.
type Dirs struct {
	// Config holds what the user would want to back up: settings, proxies and
	// connection profiles (%APPDATA%\RDP-over-proxy).
	Config string
	// Local holds machine-specific data: logs and the WebView2 profile
	// (%LOCALAPPDATA%\RDP-over-proxy).
	Local string
}

// EnvHome, when set, puts all data under that folder instead of %APPDATA% and
// %LOCALAPPDATA%. Tests and development runs use it so they never touch the
// user's real settings (or use up the first-run language picker).
const EnvHome = "RDP_OVER_PROXY_HOME"

// DefaultDirs resolves Dirs for the current Windows user, or under EnvHome
// when that is set.
func DefaultDirs() (Dirs, error) {
	if home := os.Getenv(EnvHome); home != "" {
		return Dirs{Config: filepath.Join(home, "config"), Local: filepath.Join(home, "local")}, nil
	}
	roaming, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locate %%APPDATA%%: %w", err)
	}
	local, err := os.UserCacheDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("locate %%LOCALAPPDATA%%: %w", err)
	}
	return Dirs{
		Config: filepath.Join(roaming, AppDirName),
		Local:  filepath.Join(local, AppDirName),
	}, nil
}

// ReadJSON decodes the file at path into v. found is false, with a nil error,
// when the file does not exist.
func ReadJSON(path string, v any) (found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return true, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	return true, nil
}

// WriteJSONAtomic replaces the file at path with v encoded as indented JSON.
// The data goes to a temporary file in the same folder, is flushed to disk,
// and is then renamed over the old file, so readers see either the old or the
// new content and never a mix.
func WriteJSONAtomic(path string, v any) (err error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
