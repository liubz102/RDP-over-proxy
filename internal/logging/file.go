package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// File is a log file that rotates by size: app.log is the current one, and
// when it would grow past the limit it becomes app.1.log (app.1.log becomes
// app.2.log, and so on, up to the number kept). The size limit bounds the
// disk the log can take; it has nothing to do with timing.
type File struct {
	dir      string
	maxBytes int64
	keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
	err  error // the last write error, reported once
}

// File name parts.
const (
	logBase = "app"
	logExt  = ".log"
)

// OpenFile opens dir\app.log for appending, creating the folder if needed.
// keep is how many older files to keep besides the current one.
func OpenFile(dir string, maxBytes int64, keep int) (*File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l := &File{dir: dir, maxBytes: maxBytes, keep: keep}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

// Path is the current log file.
func (l *File) Path() string { return l.name(0) }

func (l *File) name(i int) string {
	if i == 0 {
		return filepath.Join(l.dir, logBase+logExt)
	}
	return filepath.Join(l.dir, fmt.Sprintf("%s.%d%s", logBase, i, logExt))
}

func (l *File) open() error {
	f, err := os.OpenFile(l.name(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, st.Size()
	return nil
}

// WriteLine appends one line. Failures are not returned: logging must never
// stop the app. The first failure after a success is written to stderr.
func (l *File) WriteLine(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	data := []byte(s + "\r\n")
	if l.size > 0 && l.size+int64(len(data)) > l.maxBytes {
		l.rotate()
	}
	if l.f == nil {
		return
	}
	n, err := l.f.Write(data)
	l.size += int64(n)
	if err != nil && l.err == nil {
		fmt.Fprintln(os.Stderr, "log file:", err)
	}
	l.err = err
}

// rotate shifts the files by one and starts a new app.log. l.mu is held.
func (l *File) rotate() {
	l.f.Close()
	l.f = nil
	_ = os.Remove(l.name(l.keep))
	for i := l.keep - 1; i >= 0; i-- {
		_ = os.Rename(l.name(i), l.name(i+1))
	}
	if l.keep == 0 {
		_ = os.Remove(l.name(0))
	}
	if err := l.open(); err != nil {
		fmt.Fprintln(os.Stderr, "log file:", err)
	}
}

// Close closes the file.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
