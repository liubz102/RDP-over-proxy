//go:build windows

package mstsc

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// History is mstsc's memory of recent computers: the list of its Computer box
// (HistoryKey), which it writes as it connects, and the address Default.rdp
// opens with, which it writes as it exits. Neither is written by a session
// once its mstsc has exited, which is when the app calls Forget.
type History struct {
	// Key is the list's registry key under HKEY_CURRENT_USER. The app uses
	// HistoryKey; tests use one of their own.
	Key string
	// RDPFile finds Default.rdp. The app uses DefaultRDPPath; tests use a
	// file of their own. Nil leaves Default.rdp alone.
	RDPFile func() (string, error)

	mu sync.Mutex
	// known is the list as Forget last left it, for putting back what
	// dropped entries pushed out (see Prune).
	known []string
}

// Forgotten is what Forget changed.
type Forgotten struct {
	// Dropped are the entries removed from the list.
	Dropped []string
	// Restored counts the entries put back at the end of the list.
	Restored int
	// AddressCleared: Default.rdp no longer opens with an address.
	AddressCleared bool
}

// Forget removes every address drop matches from the list (see Prune) and
// empties Default.rdp's address if drop matches it. A missing list or file
// is not an error. An error never names Default.rdp's path, which goes into
// the log file and can name a file server or a company.
//
// Other mstsc processes may write the list meanwhile: Forget writes, reads
// the list again and goes on until what it reads is clean. Default.rdp is
// read, changed and written through one handle that lets nobody else write,
// so a save of mstsc's is either wholly before or wholly after, or fails
// while Forget holds the file (a few milliseconds).
func (h *History) Forget(drop func(entry string) bool) (Forgotten, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var f Forgotten
	listErr := h.pruneList(drop, &f)
	fileErr := h.clearFile(drop, &f)
	return f, errors.Join(listErr, fileErr)
}

func (h *History) pruneList(drop func(entry string) bool, f *Forgotten) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, h.Key, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Remote Desktop's list of computers: %w", err)
	}
	defer k.Close()
	known := h.known
	for {
		names, list, err := readList(k)
		if err != nil {
			return err
		}
		kept, dropped, restored := Prune(list, known, drop)
		if slices.Equal(kept, list) {
			h.known = kept
			return nil
		}
		if err := writeList(k, names, list, kept); err != nil {
			return err
		}
		f.Dropped = append(f.Dropped, dropped...)
		f.Restored += restored
	}
}

// readList returns the list's value names, MRU<n> by n, and their values.
// A value that is not a string fails it: the list is left as it is.
func readList(k registry.Key) (names, list []string, err error) {
	all, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, nil, fmt.Errorf("read Remote Desktop's list of computers: %w", err)
	}
	slots := map[string]int{}
	for _, name := range all {
		if n, ok := slot(name); ok {
			slots[name] = n
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int { return slots[a] - slots[b] })
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s of Remote Desktop's list of computers: %w", name, err)
		}
		list = append(list, v)
	}
	return names, list, nil
}

// slot reads n from a value name MRU<n>.
func slot(name string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.ToUpper(name), "MRU")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n >= 0 && strconv.Itoa(n) == rest
}

// writeList turns the list (names, list) into kept, as MRU0 onwards. Only
// the values that differ are written; the names left over are removed.
func writeList(k registry.Key, names, list, kept []string) error {
	for i, v := range kept {
		name := "MRU" + strconv.Itoa(i)
		if i < len(names) && strings.EqualFold(names[i], name) && list[i] == v {
			continue
		}
		if err := k.SetStringValue(name, v); err != nil {
			return fmt.Errorf("write %s of Remote Desktop's list of computers: %w", name, err)
		}
	}
	for _, name := range names {
		if n, _ := slot(name); n < len(kept) {
			continue
		}
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("remove %s of Remote Desktop's list of computers: %w", name, err)
		}
	}
	return nil
}

func (h *History) clearFile(drop func(entry string) bool, f *Forgotten) error {
	if h.RDPFile == nil {
		return nil
	}
	path, err := h.RDPFile()
	if err != nil {
		return fmt.Errorf("find Default.rdp: %w", err)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("find Default.rdp: %w", err)
	}
	// Opening the file as it is keeps its attributes (mstsc hides it) and
	// its permissions.
	handle, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Default.rdp: %w", err)
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return rdpError("read", err)
	}
	out, changed := ClearAddress(data, drop)
	if !changed {
		return nil
	}
	if _, err := file.WriteAt(out, 0); err != nil {
		return rdpError("write", err)
	}
	if err := file.Truncate(int64(len(out))); err != nil {
		return rdpError("write", err)
	}
	f.AddressCleared = true
	return nil
}

// rdpError leaves Default.rdp's path out of err.
func rdpError(op string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Errorf("%s Default.rdp: %w", op, err)
}
