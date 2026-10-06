package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liubz102/RDP-over-proxy/internal/localproxy"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
)

func TestLocalProxies(t *testing.T) {
	facts := localproxy.Facts{
		Processes: []localproxy.Process{
			{PID: 100, ParentPID: 50, Exe: "v2rayN.exe", Created: time.Unix(1000, 0)},
			{PID: 200, ParentPID: 100, Exe: "xray.exe", Created: time.Unix(1001, 0)},
		},
		Listeners: []localproxy.Listener{{Addr: netip.MustParseAddrPort("127.0.0.1:10808"), PID: 200}},
	}
	var gatherErr error
	s := NewProxyService(NewCore(Deps{
		LocalProxies: func() (localproxy.Facts, error) { return facts, gatherErr },
		Log:          logging.New(nil, logging.LevelInfo, 10),
	}))
	got, err := s.LocalProxies()
	want := []localproxy.Candidate{{Name: "v2rayN", Hosts: []string{"127.0.0.1"}, Port: 10808, Source: localproxy.SourceProgram}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LocalProxies = %+v, %v; want %+v", got, err, want)
	}
	gatherErr = errors.New("the socket table could not be read")
	if _, err := s.LocalProxies(); !errors.Is(err, gatherErr) {
		t.Fatalf("LocalProxies when reading fails = %v", err)
	}

	// Without a way to look (a test's Core), there are none: an empty list,
	// which the page reads as such, not null.
	none := NewProxyService(NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)}))
	list, err := none.LocalProxies()
	if data, _ := json.Marshal(list); err != nil || string(data) != "[]" {
		t.Fatalf("LocalProxies without Deps = %s, %v", data, err)
	}
}

func TestProbeLocal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				g := make([]byte, 4)
				if _, err := io.ReadFull(c, g); err == nil {
					_, _ = c.Write([]byte{5, 2})
				}
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	s := NewProxyService(NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)}))
	port := ln.Addr().(*net.TCPAddr).Port
	got, err := s.ProbeLocal(context.Background(), []string{"127.0.0.1"}, port)
	if err != nil || got != (localproxy.Result{Socks5: true, Password: true, Host: "127.0.0.1"}) {
		t.Fatalf("ProbeLocal = %+v, %v", got, err)
	}
	// The page can make the app knock on no other computer's door.
	_, err = s.ProbeLocal(context.Background(), []string{"192.0.2.1"}, port)
	if v := errorJSON(t, err); v.Code != "localproxy.notLocal" {
		t.Fatalf("ProbeLocal of another computer = %+v", v)
	}
}

// The app's log lines reach the page as they are kept, but the Wails
// runtime's: sending an event can make Wails log, which would send another.
func TestAppLogLinesAreSent(t *testing.T) {
	h := newHarness(t)
	h.log.Log(logging.Line{Level: logging.LevelWarn, Source: logging.SourceUI, Msg: "from Wails"})
	h.log.Log(logging.Line{Level: logging.LevelInfo, Source: logging.SourceEngine, Msg: "from Xray"})
	for {
		sent := h.events.named(EventAppLog)
		if i := slices.IndexFunc(sent, func(v any) bool { return v.(logging.Line).Msg == "from Xray" }); i >= 0 {
			// The lines go in order, so the Wails line was passed over before it.
			if slices.ContainsFunc(sent, func(v any) bool { return v.(logging.Line).Msg == "from Wails" }) {
				t.Fatal("a Wails line was sent")
			}
			if line := sent[i].(logging.Line); line.Seq == 0 {
				t.Fatalf("the line has no Seq: %+v", line)
			}
			break
		}
		<-h.events.changed
	}
	// Both are in the log the page reads.
	read := h.app.Log()
	if !slices.ContainsFunc(read, func(l logging.Line) bool { return l.Msg == "from Wails" }) {
		t.Fatalf("Log() = %+v", read)
	}
}

// A probe the page could not cancel (in the browser preview, a request the
// server gave up on) ends with the next look, or when the app quits.
func TestProbesEndWithTheNextLook(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	greeted := make(chan struct{}, 2)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				g := make([]byte, 4)
				if _, err := io.ReadFull(c, g); err == nil {
					greeted <- struct{}{}
				}
				_, _ = io.Copy(io.Discard, c) // never answers
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	core := NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)})
	s := NewProxyService(core)

	for _, end := range []struct {
		name string
		do   func()
	}{
		{"a new look", func() { _, _ = s.LocalProxies() }},
		{"quitting", core.Quit},
	} {
		done := make(chan error, 1)
		go func() {
			_, err := s.ProbeLocal(context.Background(), []string{"127.0.0.1"}, port)
			done <- err
		}()
		<-greeted
		end.do()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("after %s the probe returned %v, want context.Canceled", end.name, err)
		}
	}
}
