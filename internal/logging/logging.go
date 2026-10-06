// Package logging records what the app does: a log file in the logs folder
// next to the app, for troubleshooting, and in-memory rings of recent lines
// for the UI.
//
// The file is meant to be attachable to a bug report, so personal data is
// masked before it is written (see Redactor). The rings feed the user's own
// screen and keep the details.
package logging

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Levels, from the most to the least detailed. They match the values of
// model.Settings.LogLevel.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

var levelRank = map[string]int{LevelDebug: 0, LevelInfo: 1, LevelWarn: 2, LevelError: 3}

// Sources of log lines.
const (
	SourceApp     = "app"
	SourceEngine  = "engine"
	SourceSession = "session"
	SourceUI      = "ui" // the Wails runtime
)

// Line is one log line. Msg is a stable key (session lines, which the UI
// translates) or plain English text.
type Line struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Source  string         `json:"source"`
	Profile string         `json:"profile,omitempty"` // the profile ID of a session line
	Msg     string         `json:"msg"`
	Args    map[string]any `json:"args,omitempty"`
	// Seq numbers the lines of a log in the order they were kept, so the UI
	// can merge a log it read with the lines it was sent. The app's log
	// (Logger) and the session logs, across all profiles, count apart.
	Seq uint64 `json:"seq,omitempty"`
}

// Logger keeps the lines at or above its level: in the file, masked, and in
// a ring for the UI. Consecutive identical lines are written to the file
// once; when a different line follows, a note says how often the previous
// one repeated. Its methods may be called from any goroutine.
type Logger struct {
	file   *File // nil: no file
	redact *Redactor
	recent *Ring

	mu       sync.Mutex
	level    int
	seq      uint64 // the last kept line's Seq
	last     *Line  // the last line written to the file
	repeats  int    // how many times it has repeated since
	watchers []*watcher
}

// New returns a logger writing to file (which may be nil) at the given
// level, keeping the last ringSize lines in memory.
func New(file *File, level string, ringSize int) *Logger {
	l := &Logger{file: file, redact: &Redactor{}, recent: NewRing(ringSize)}
	l.SetLevel(level)
	return l
}

// Attach starts writing to file, beginning with the lines kept so far. It is
// for a log file that can only be opened once the app knows it is the only
// instance. A logger that already has a file keeps it.
func (l *Logger) Attach(file *File) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil || file == nil {
		return
	}
	l.file = file
	for _, line := range l.recent.Lines() {
		l.file.WriteLine(l.format(line))
	}
}

// SetLevel changes the lowest level written to the file. An unknown level
// means info.
func (l *Logger) SetLevel(level string) {
	rank, ok := levelRank[level]
	if !ok {
		rank = levelRank[LevelInfo]
	}
	l.mu.Lock()
	l.level = rank
	l.mu.Unlock()
}

// Debug reports whether debug lines are written.
func (l *Logger) Debug() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level == levelRank[LevelDebug]
}

// Redactor is the logger's redactor; the app tells it what to mask.
func (l *Logger) Redactor() *Redactor { return l.redact }

// Recent returns the lines in the ring, oldest first.
func (l *Logger) Recent() []Line { return l.recent.Lines() }

// Log records a line. A zero Time is set to now. A line kept gets the next
// Seq of the app's log.
func (l *Logger) Log(line Line) {
	if line.Time.IsZero() {
		line.Time = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if levelRank[line.Level] < l.level {
		return
	}
	l.seq++
	line.Seq = l.seq
	l.recent.Add(line)
	for _, w := range l.watchers {
		w.post(line)
	}
	if l.file == nil {
		return
	}
	if l.last != nil && sameLine(*l.last, line) {
		l.repeats++
		return
	}
	l.flushRepeats(line.Time)
	l.last = &line
	l.file.WriteLine(l.format(line))
}

// flushRepeats writes how often the last line repeated, if it did. l.mu
// must be held.
func (l *Logger) flushRepeats(at time.Time) {
	if l.repeats == 0 {
		return
	}
	note := *l.last
	note.Time, note.Args = at, nil
	note.Msg = fmt.Sprintf("(the line before repeated %d more times)", l.repeats)
	l.file.WriteLine(l.format(note))
	l.repeats = 0
}

// Close writes a pending repeat note and closes the file. Lines logged
// afterwards only go to the ring.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	l.flushRepeats(time.Now())
	err := l.file.Close()
	l.file = nil
	return err
}

// Watch calls f with each line the logger keeps from now on, in the order
// they were kept. The calls come one at a time from a goroutine of the
// watch's own, so Log never waits for f, and f may log. stop ends the
// calls: no line kept after it returns is passed on, and lines still
// waiting are dropped; a call already under way may finish after it.
func (l *Logger) Watch(f func(Line)) (stop func()) {
	w := &watcher{signal: make(chan struct{}, 1), done: make(chan struct{})}
	l.mu.Lock()
	l.watchers = append(l.watchers, w)
	l.mu.Unlock()
	go w.run(f)
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.watchers = slices.DeleteFunc(l.watchers, func(x *watcher) bool { return x == w })
			l.mu.Unlock()
			close(w.done)
		})
	}
}

// watcher is an unbounded queue of lines for one Watch. Posting never
// blocks.
type watcher struct {
	mu     sync.Mutex
	queue  []Line
	signal chan struct{}
	done   chan struct{}
}

func (w *watcher) post(line Line) {
	w.mu.Lock()
	w.queue = append(w.queue, line)
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default: // a wake-up is already pending
	}
}

func (w *watcher) run(f func(Line)) {
	for {
		select {
		case <-w.done:
			return
		case <-w.signal:
		}
		w.mu.Lock()
		q := w.queue
		w.queue = nil
		w.mu.Unlock()
		for _, line := range q {
			select {
			case <-w.done:
				return
			default:
			}
			f(line)
		}
	}
}

// Convenience methods for the app's own lines.

func (l *Logger) Infof(format string, a ...any) { l.app(LevelInfo, format, a...) }
func (l *Logger) Warnf(format string, a ...any) { l.app(LevelWarn, format, a...) }
func (l *Logger) Errorf(format string, a ...any) {
	l.app(LevelError, format, a...)
}

func (l *Logger) app(level, format string, a ...any) {
	l.Log(Line{Level: level, Source: SourceApp, Msg: fmt.Sprintf(format, a...)})
}

func sameLine(a, b Line) bool {
	if a.Level != b.Level || a.Source != b.Source || a.Profile != b.Profile || a.Msg != b.Msg || len(a.Args) != len(b.Args) {
		return false
	}
	for k, v := range a.Args {
		if fmt.Sprint(b.Args[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// format renders a line for the file, masking personal data:
//
//	2026-10-02 21:04:05.123 WARN  session[1a2b3c4d5e6f7a8b] session.upstreamFailing code=proxy.auth error="..."
func (l *Logger) format(line Line) string {
	var b strings.Builder
	b.WriteString(line.Time.Format("2006-01-02 15:04:05.000"))
	fmt.Fprintf(&b, " %-5s ", strings.ToUpper(line.Level))
	b.WriteString(line.Source)
	if line.Profile != "" {
		b.WriteString("[" + line.Profile + "]")
	}
	b.WriteByte(' ')
	b.WriteString(l.redact.Text(line.Msg))
	for _, k := range slices.Sorted(maps.Keys(line.Args)) {
		v := l.redact.Text(fmt.Sprint(line.Args[k]))
		if v == "" || strings.ContainsAny(v, " \t\"=") {
			v = strconv.Quote(v)
		}
		b.WriteString(" " + k + "=" + v)
	}
	return strings.ReplaceAll(b.String(), "\n", `\n`)
}

// Ring keeps the most recent lines, up to a fixed number.
type Ring struct {
	mu    sync.Mutex
	lines []Line
	next  int // where the next line goes once the ring is full
	full  bool
}

// NewRing returns a ring that keeps size lines.
func NewRing(size int) *Ring { return &Ring{lines: make([]Line, 0, max(size, 1))} }

// Add appends a line, dropping the oldest when the ring is full.
func (r *Ring) Add(l Line) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		r.lines = append(r.lines, l)
		r.full = len(r.lines) == cap(r.lines)
		return
	}
	r.lines[r.next] = l
	r.next = (r.next + 1) % len(r.lines)
}

// Lines returns the kept lines, oldest first.
func (r *Ring) Lines() []Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Line, 0, len(r.lines))
	out = append(out, r.lines[r.next:]...)
	return append(out, r.lines[:r.next]...)
}

// Clear empties the ring.
func (r *Ring) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines, r.next, r.full = r.lines[:0], 0, false
}
