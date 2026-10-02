package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRedactor(t *testing.T) {
	var r Redactor
	r.Add("pc.example.com", `EXAMPLE\alice`, "", "ab")
	r.Add("proxy.example.net", "PC.EXAMPLE.COM") // the set only grows; case does not matter
	r.AddPath(`C:\Users\Alice Example\`, "%USERPROFILE%")
	cases := map[string]string{
		`open C:\users\alice example\Documents\Default.rdp: denied`: `open %USERPROFILE%\Documents\Default.rdp: denied`,
		"dial tcp 192.0.2.10:1080: refused":                         "dial tcp <ip>:1080: refused",
		"lookup PC.Example.com: no such host":                       "lookup <redacted>: no such host",
		`user EXAMPLE\alice signed in`:                              "user <redacted> signed in",
		"tunnel on 127.12.34.56:13389 to 0.0.0.0":                   "tunnel on 127.12.34.56:13389 to 0.0.0.0",
		"[2001:db8::7]:3389 and [::1]:3389":                         "[<ip>]:3389 and [::1]:3389",
		"at 21:04:05 version 1.260327.0 ab":                         "at 21:04:05 version 1.260327.0 ab",
		"via proxy.example.net (proxy.example.net:80)":              "via <redacted> (<redacted>:80)",
	}
	for in, want := range cases {
		if got := r.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRing(t *testing.T) {
	r := NewRing(3)
	if len(r.Lines()) != 0 {
		t.Fatal("a new ring has lines")
	}
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		r.Add(Line{Msg: m})
	}
	var got []string
	for _, l := range r.Lines() {
		got = append(got, l.Msg)
	}
	if !slices.Equal(got, []string{"c", "d", "e"}) {
		t.Fatalf("ring = %q", got)
	}
	r.Clear()
	r.Add(Line{Msg: "f"})
	if l := r.Lines(); len(l) != 1 || l[0].Msg != "f" {
		t.Fatalf("after Clear: %+v", l)
	}
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\r\n"), "\r\n")
}

func newTestLogger(t *testing.T, level string) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := OpenFile(dir, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	l := New(f, level, 100)
	clock := time.Date(2026, 10, 2, 21, 4, 5, 0, time.Local)
	l.now = func() time.Time { return clock }
	t.Cleanup(func() { l.Close() })
	return l, f.Path()
}

func TestLoggerWritesRedactedLinesAtItsLevel(t *testing.T) {
	l, path := newTestLogger(t, LevelInfo)
	l.Redactor().Add("pc.example.com")
	l.Log(Line{Level: LevelDebug, Source: SourceEngine, Msg: "chatter"})
	l.Log(Line{Level: LevelWarn, Source: SourceSession, Profile: "1a2b", Msg: "session.upstreamFailing",
		Args: map[string]any{"code": "net.refused", "error": "dial tcp pc.example.com:3389: refused"}})
	l.Close()
	lines := readLog(t, path)
	want := `2026-10-02 21:04:05.000 WARN  session[1a2b] session.upstreamFailing code=net.refused error="dial tcp <redacted>:3389: refused"`
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("log file:\n%s\nwant\n%s", strings.Join(lines, "\n"), want)
	}
	// The ring keeps the same lines unmasked, for the user's own screen.
	recent := l.Recent()
	if len(recent) != 1 || !strings.Contains(recent[0].Args["error"].(string), "pc.example.com") {
		t.Fatalf("ring: %+v", recent)
	}
}

func TestLoggerCollapsesRepeats(t *testing.T) {
	l, path := newTestLogger(t, LevelInfo)
	for range 5 {
		l.Log(Line{Level: LevelError, Source: SourceEngine, Msg: "failed to process outbound traffic"})
	}
	l.Log(Line{Level: LevelInfo, Source: SourceApp, Msg: "something else"})
	l.Log(Line{Level: LevelInfo, Source: SourceApp, Msg: "something else"})
	l.Close() // writes the pending note
	lines := readLog(t, path)
	if len(lines) != 4 ||
		!strings.HasSuffix(lines[0], "engine failed to process outbound traffic") ||
		!strings.HasSuffix(lines[1], "engine (the line before repeated 4 more times)") ||
		!strings.HasSuffix(lines[2], "app something else") ||
		!strings.HasSuffix(lines[3], "app (the line before repeated 1 more times)") {
		t.Fatalf("log file:\n%s", strings.Join(lines, "\n"))
	}
}

func TestLevelChanges(t *testing.T) {
	l, path := newTestLogger(t, LevelError)
	l.Infof("hidden")
	l.SetLevel(LevelDebug)
	if !l.Debug() {
		t.Fatal("Debug() is false at debug level")
	}
	l.Log(Line{Level: LevelDebug, Source: SourceEngine, Msg: "shown"})
	l.SetLevel("nonsense") // means info
	l.Log(Line{Level: LevelDebug, Source: SourceEngine, Msg: "hidden again"})
	l.Close()
	if lines := readLog(t, path); len(lines) != 1 || !strings.HasSuffix(lines[0], "shown") {
		t.Fatalf("log file: %q", lines)
	}
}

func TestFileRotates(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenFile(dir, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 40) // 42 bytes with CRLF: two fit in 100
	for range 7 {
		f.WriteLine(line)
	}
	f.Close()
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"app.1.log", "app.2.log", "app.log"}) {
		t.Fatalf("files %q", names)
	}
	for _, n := range names {
		if st, _ := os.Stat(filepath.Join(dir, n)); st.Size() > 100 {
			t.Fatalf("%s is %d bytes, over the limit", n, st.Size())
		}
	}
	// Reopening appends to the current file.
	f, err = OpenFile(dir, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteLine("y")
	f.Close()
	if lines := readLog(t, filepath.Join(dir, "app.log")); lines[len(lines)-1] != "y" {
		t.Fatalf("app.log after reopening: %q", lines)
	}
}

func TestSlogHandler(t *testing.T) {
	// Even at debug level the runtime's own diagnostics stay out: some carry
	// the arguments of service calls, passwords included.
	l, path := newTestLogger(t, LevelDebug)
	log := slog.New(SlogHandler(l, SourceUI)).With("window", "main")
	log.Info("Handling request", "file", "index.html")
	log.Debug("Binding call complete:", "method", "Connect", "args", `["1a2b","Hunter2!"]`, "result", "{}")
	log.Debug("Runtime call:", "method", "Call.Call", "args", `{"args":["1a2b","Hunter2!"]}`)
	log.Error("Binding call failed: Bound method returned an error: the proxy is used by 1 connection(s)")
	log.Warn("window state", "state", "minimised")
	log.Error("Binding call failed: failed to call binding: could not parse argument #0", "args", `["Hunter2!"]`)
	l.Close()
	lines := readLog(t, path)
	if len(lines) != 2 || !strings.HasSuffix(lines[0], `WARN  ui window state state=minimised window=main`) ||
		!strings.HasSuffix(lines[1], `ERROR ui Binding call failed: failed to call binding: could not parse argument #0 args=<omitted> window=main`) {
		t.Fatalf("log file: %q", lines)
	}
	for _, line := range l.Recent() {
		if strings.Contains(fmt.Sprint(line), "Hunter2!") {
			t.Fatalf("a password reached the ring: %+v", line)
		}
	}
}

func TestAttachWritesWhatCameBefore(t *testing.T) {
	l := New(nil, LevelInfo, 10)
	clock := time.Date(2026, 10, 2, 21, 4, 5, 0, time.Local)
	l.now = func() time.Time { return clock }
	l.Infof("before the file")
	dir := t.TempDir()
	f, err := OpenFile(dir, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	l.Attach(f)
	l.Infof("after")
	l.Close()
	lines := readLog(t, f.Path())
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "app before the file") || !strings.HasSuffix(lines[1], "app after") {
		t.Fatalf("log file: %q", lines)
	}
}
