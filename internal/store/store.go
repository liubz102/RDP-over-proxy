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

// Dirs are the two folders the application keeps files in. Both are in the
// folder the exe is in, never in the user profile: whoever opens that folder
// sees everything the app keeps, and it all goes where the app goes.
type Dirs struct {
	// Data holds the settings, the proxies, the connection profiles and the
	// WebView2 profile (<exe folder>\data).
	Data string
	// Logs holds the log files (<exe folder>\logs).
	Logs string
}

// The folders' names in the exe's folder.
const (
	DataDirName = "data"
	LogsDirName = "logs"
)

// DirsIn returns the Dirs in base: base\data and base\logs.
func DirsIn(base string) Dirs {
	return Dirs{Data: filepath.Join(base, DataDirName), Logs: filepath.Join(base, LogsDirName)}
}

// EnvHome, when set, is used in place of the exe's folder. Tests and
// development runs use it so they never touch the data of a copy that is in
// use (or use up the first-run language picker).
const EnvHome = "RDP_OVER_PROXY_HOME"

// DefaultDirs returns the Dirs in the folder of the running exe, or in
// EnvHome when that is set.
func DefaultDirs() (Dirs, error) {
	if home := os.Getenv(EnvHome); home != "" {
		return DirsIn(home), nil
	}
	dir, err := ExeDir()
	if err != nil {
		return Dirs{}, err
	}
	return DirsIn(dir), nil
}

// ExeDir is the folder the running exe is in. Started through a symbolic
// link, it is the folder of the exe the link points to.
func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the program file: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe), nil
}

// FolderError is a folder the app cannot use: it could not be created, or
// files cannot be written in it.
type FolderError struct {
	Dir string
	// Err is the system's reason, such as "Access is denied."
	Err error
}

func (e *FolderError) Error() string { return fmt.Sprintf("%s: %v", e.Dir, e.Err) }

func (e *FolderError) Unwrap() error { return e.Err }

// Prepare creates both folders if they are missing and makes sure files can
// be written in them, by writing a file and removing it again: a folder can
// exist and still refuse writes (one in Program Files, on a read-only share).
// It returns a *FolderError for the first folder that cannot be used.
func (d Dirs) Prepare() error {
	for _, dir := range []string{d.Data, d.Logs} {
		if err := writable(dir); err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) {
				err = pe.Err // the folder is named on its own
			}
			return &FolderError{Dir: dir, Err: err}
		}
	}
	return nil
}

func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// A name of its own each time: another copy of the app may be starting
	// in the same folder.
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}
	return errors.Join(f.Close(), os.Remove(f.Name()))
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
