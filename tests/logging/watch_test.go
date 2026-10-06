package logging_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/logging"

	"fmt"
	"slices"
	"sync"
	"testing"
)

// collector receives what a watch passes on.
type collector struct {
	mu    sync.Mutex
	lines []Line
	got   chan struct{}
}

func newCollector() *collector { return &collector{got: make(chan struct{}, 1)} }

func (c *collector) add(l Line) {
	c.mu.Lock()
	c.lines = append(c.lines, l)
	c.mu.Unlock()
	select {
	case c.got <- struct{}{}:
	default:
	}
}

// until waits until a line with the message has arrived, and returns the
// lines so far.
func (c *collector) until(msg string) []Line {
	for {
		c.mu.Lock()
		lines := slices.Clone(c.lines)
		c.mu.Unlock()
		if slices.ContainsFunc(lines, func(l Line) bool { return l.Msg == msg }) {
			return lines
		}
		<-c.got
	}
}

// Lines logged from many goroutines come out in the order they were kept,
// numbered one after another, as the ring has them.
func TestWatchPassesLinesOnInOrder(t *testing.T) {
	l := New(nil, LevelInfo, 1000)
	c := newCollector()
	stop := l.Watch(c.add)
	defer stop()

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 50 {
				l.Infof("goroutine %d line %d", g, i)
				l.Log(Line{Level: LevelDebug, Source: SourceEngine, Msg: "below the level"})
			}
		})
	}
	wg.Wait()
	l.Infof("last")

	got := c.until("last")
	if len(got) != 401 {
		t.Fatalf("passed on %d lines, want 401", len(got))
	}
	for i, line := range got {
		if line.Seq != uint64(i+1) || line.Level == LevelDebug {
			t.Fatalf("line %d is %+v; want Seq %d, nothing below the level", i, line, i+1)
		}
	}
	if recent := l.Recent(); !slices.EqualFunc(recent, got, func(a, b Line) bool { return a.Seq == b.Seq && a.Msg == b.Msg }) {
		t.Fatal("the ring and the watch disagree")
	}
}

// The watch's function may log: it runs apart from Log.
func TestWatchMayLog(t *testing.T) {
	l := New(nil, LevelInfo, 100)
	c := newCollector()
	stop := l.Watch(func(line Line) {
		c.add(line)
		if line.Msg == "ping" {
			l.Infof("pong")
		}
	})
	defer stop()
	l.Infof("ping")
	if got := c.until("pong"); len(got) != 2 || got[1].Seq != got[0].Seq+1 {
		t.Fatalf("lines: %+v", got)
	}
}

func TestWatchStops(t *testing.T) {
	l := New(nil, LevelInfo, 100)
	first, second := newCollector(), newCollector()
	stop := l.Watch(first.add)
	l.Infof("before")
	first.until("before")
	stop()
	stop() // twice is harmless

	defer l.Watch(second.add)()
	l.Infof("after")
	second.until("after")
	// A line kept after stop has returned never reaches the stopped watch.
	first.mu.Lock()
	defer first.mu.Unlock()
	if len(first.lines) != 1 {
		t.Fatalf("the stopped watch got %s", fmt.Sprint(first.lines))
	}
}
