//go:build windows

package mstsc_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
)

// testHistory uses a registry key of the test's own under HKEY_CURRENT_USER,
// never mstsc's, and a Default.rdp in the test's temporary folder. The key is
// removed afterwards.
func testHistory(t *testing.T) (*mstsc.History, string) {
	t.Helper()
	parent := `Software\RDP-over-proxy-test`
	key := parent + `\` + model.NewID()
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent) // fails while other tests use it
	})
	file := filepath.Join(t.TempDir(), "Default.rdp")
	return &mstsc.History{Key: key, RDPFile: func() (string, error) { return file, nil }}, file
}

// setValues writes the list's values by name; an empty value removes it.
func setValues(t *testing.T, key string, values map[string]string) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	for name, v := range values {
		if v == "" {
			_ = k.DeleteValue(name)
			continue
		}
		if err := k.SetStringValue(name, v); err != nil {
			t.Fatal(err)
		}
	}
}

// setList replaces the list with list, as MRU0 onwards.
func setList(t *testing.T, key string, list []string) {
	t.Helper()
	values := map[string]string{}
	for name := range readValues(t, key) {
		values[name] = ""
	}
	for i, v := range list {
		values["MRU"+strconv.Itoa(i)] = v
	}
	setValues(t, key, values)
}

// readValues returns every value of the key.
func readValues(t *testing.T, key string) map[string]string {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return map[string]string{}
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, name := range names {
		v, _, err := k.GetStringValue(name)
		if err != nil {
			t.Fatal(err)
		}
		values[name] = v
	}
	return values
}

// wantList checks that the key holds exactly list, as MRU0 onwards.
func wantList(t *testing.T, key string, list []string) {
	t.Helper()
	want := map[string]string{}
	for i, v := range list {
		want["MRU"+strconv.Itoa(i)] = v
	}
	if got := readValues(t, key); !mapsEqual(got, want) {
		t.Fatalf("list = %q, want %q", got, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

var appRange = model.HistoryMatch(model.HistoryApp)

func TestHistoryRemovesEntrances(t *testing.T) {
	h, _ := testHistory(t)
	setList(t, h.Key, []string{"127.104.1.1:13389", "pc.example.com:3390", "127.20.161.131:13389",
		"127.0.0.1:3390", "192.0.2.1", "127.92.99.171"})
	setValues(t, h.Key, map[string]string{"Other": "kept as it is"})
	f, err := h.Forget(appRange)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if !slices.Equal(f.Dropped, []string{"127.104.1.1:13389", "127.20.161.131:13389", "127.92.99.171"}) ||
		f.Restored != 0 || f.AddressCleared {
		t.Fatalf("Forget = %+v", f)
	}
	got := readValues(t, h.Key)
	want := map[string]string{"MRU0": "pc.example.com:3390", "MRU1": "127.0.0.1:3390", "MRU2": "192.0.2.1",
		"Other": "kept as it is"}
	if !mapsEqual(got, want) {
		t.Fatalf("values = %q, want %q", got, want)
	}

	// All of 127.0.0.0/8.
	if _, err := h.Forget(model.HistoryMatch(model.HistoryLoopback)); err != nil {
		t.Fatal(err)
	}
	if got := readValues(t, h.Key); got["MRU0"] != "pc.example.com:3390" || got["MRU1"] != "192.0.2.1" || len(got) != 3 {
		t.Fatalf("values = %q", got)
	}
}

// With nothing to remove the list is not written: a gap mstsc left stays.
func TestHistoryLeavesACleanListAlone(t *testing.T) {
	h, _ := testHistory(t)
	setValues(t, h.Key, map[string]string{"MRU0": "pc.example.com", "MRU2": "192.0.2.1"})
	f, err := h.Forget(appRange)
	if err != nil || len(f.Dropped) != 0 {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	if got := readValues(t, h.Key); !mapsEqual(got, map[string]string{"MRU0": "pc.example.com", "MRU2": "192.0.2.1"}) {
		t.Fatalf("values = %q", got)
	}

	// With something to remove, the gap closes.
	setValues(t, h.Key, map[string]string{"MRU1": "127.1.2.3"})
	if _, err := h.Forget(appRange); err != nil {
		t.Fatal(err)
	}
	wantList(t, h.Key, []string{"pc.example.com", "192.0.2.1"})
}

// mstsc connects to an entrance with a full list: the entrance goes first
// and the tenth computer goes. Forget gives it back.
func TestHistoryPutsBackWhatAnEntrancePushedOut(t *testing.T) {
	h, _ := testHistory(t)
	full := ten(10)
	setList(t, h.Key, full)
	if _, err := h.Forget(appRange); err != nil { // learns the list
		t.Fatal(err)
	}
	setList(t, h.Key, append([]string{"127.1.2.3:13389"}, full[:9]...))
	f, err := h.Forget(appRange)
	if err != nil || f.Restored != 1 {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	wantList(t, h.Key, full)
}

func TestHistoryWithoutAList(t *testing.T) {
	h, _ := testHistory(t)
	f, err := h.Forget(appRange)
	if err != nil || len(f.Dropped) != 0 || f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, h.Key, registry.QUERY_VALUE); err == nil {
		t.Fatal("Forget created the key")
	}
}

// A value that is not a string is not mstsc's doing: the list stays as it is.
func TestHistoryLeavesAStrangeListAlone(t *testing.T) {
	h, _ := testHistory(t)
	setList(t, h.Key, []string{"127.1.2.3", "pc.example.com"})
	k, err := registry.OpenKey(registry.CURRENT_USER, h.Key, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	err = k.SetDWordValue("MRU2", 7)
	k.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Forget(appRange); err == nil {
		t.Fatal("Forget accepted a number in the list")
	}
	k, err = registry.OpenKey(registry.CURRENT_USER, h.Key, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if v, _, err := k.GetStringValue("MRU0"); err != nil || v != "127.1.2.3" {
		t.Fatalf("MRU0 = %q, %v", v, err)
	}
}

func TestHistoryEmptiesDefaultRDPsAddress(t *testing.T) {
	h, file := testHistory(t)
	text := "screen mode id:i:2\r\nfull address:s:127.104.198.139:13389\r\nusername:s:alice\r\n"
	if err := os.WriteFile(file, rdp(text), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	f, err := h.Forget(appRange)
	if err != nil || !f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := rdp("screen mode id:i:2\r\nfull address:s:\r\nusername:s:alice\r\n"); !bytes.Equal(got, want) {
		t.Fatalf("Default.rdp = %q, want %q", got, want)
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Fatalf("attributes = %#x, %v; want it still hidden", attrs, err)
	}

	// Another computer's address stays.
	other := rdp("full address:s:pc.example.com\r\n")
	if err := windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_NORMAL); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, other, 0o644); err != nil {
		t.Fatal(err)
	}
	if f, err := h.Forget(appRange); err != nil || f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, other) {
		t.Fatalf("Default.rdp = %q", got)
	}
}

// While someone else writes Default.rdp, Forget leaves it alone and says so.
func TestHistoryWaitsForDefaultRDPsWriter(t *testing.T) {
	h, file := testHistory(t)
	text := rdp("full address:s:127.1.2.3\r\n")
	if err := os.WriteFile(file, text, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Forget(appRange)
	if err == nil {
		windows.CloseHandle(writer)
		t.Fatal("Forget wrote Default.rdp while it was being written")
	}
	if bytes.Contains([]byte(err.Error()), []byte(filepath.Dir(file))) {
		t.Errorf("the error names the folder: %v", err)
	}
	windows.CloseHandle(writer)
	if got, _ := os.ReadFile(file); !bytes.Equal(got, text) {
		t.Fatalf("Default.rdp = %q", got)
	}

	// Once the writer is done, the next call empties it.
	if f, err := h.Forget(appRange); err != nil || !f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
}

func TestHistoryWithoutDefaultRDP(t *testing.T) {
	h, _ := testHistory(t)
	if f, err := h.Forget(appRange); err != nil || f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
	h.RDPFile = func() (string, error) { return filepath.Join(t.TempDir(), "missing", "Default.rdp"), nil }
	if f, err := h.Forget(appRange); err != nil || f.AddressCleared {
		t.Fatalf("Forget = %+v, %v", f, err)
	}
}
