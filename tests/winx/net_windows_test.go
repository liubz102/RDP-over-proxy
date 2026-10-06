package winx_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/winx"

	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test's own sockets show up with its process ID, IPv4 and IPv6.
func TestListeners(t *testing.T) {
	var want []netip.AddrPort
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		want = append(want, ln.Addr().(*net.TCPAddr).AddrPort())
	}
	got, err := Listeners()
	if err != nil {
		t.Fatalf("Listeners: %v", err)
	}
	for _, w := range want {
		found := false
		for _, l := range got {
			if l.Addr == w {
				found = true
				if l.PID != uint32(os.Getpid()) {
					t.Errorf("%v belongs to process %d, want %d", w, l.PID, os.Getpid())
				}
			}
		}
		if !found {
			t.Errorf("%v is not among the %d listeners", w, len(got))
		}
	}
}

func TestProcesses(t *testing.T) {
	got, err := Processes()
	if err != nil {
		t.Fatalf("Processes: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	byPID := map[uint32]Process{}
	for _, p := range got {
		byPID[p.PID] = p
	}
	p, ok := byPID[uint32(os.Getpid())]
	if !ok {
		t.Fatalf("this process is not among the %d processes", len(got))
	}
	if !strings.EqualFold(p.Exe, filepath.Base(exe)) || p.ParentPID != uint32(os.Getppid()) {
		t.Fatalf("this process is listed as %+v; want %s started by %d", p, filepath.Base(exe), os.Getppid())
	}
	if p.Created.IsZero() || p.Created.After(time.Now()) {
		t.Fatalf("this process started at %v", p.Created)
	}
	// The process that started this one did so before it.
	if parent, ok := byPID[p.ParentPID]; ok && !parent.Created.IsZero() && parent.Created.After(p.Created) {
		t.Fatalf("the parent started at %v, after this process (%v)", parent.Created, p.Created)
	}
}
