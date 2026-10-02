package mstsc

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestArgs(t *testing.T) {
	addr := netip.MustParseAddrPort("127.10.20.30:13389")
	cases := []struct {
		name    string
		display model.Display
		admin   bool
		want    []string
	}{
		{"default display", model.DefaultDisplay(), false,
			[]string{"/v:127.10.20.30:13389"}},
		{"full screen", model.Display{Mode: model.DisplayFullscreen}, false,
			[]string{"/v:127.10.20.30:13389", "/f"}},
		{"all monitors", model.Display{Mode: model.DisplayFullscreen, Multimon: true}, false,
			[]string{"/v:127.10.20.30:13389", "/f", "/multimon"}},
		{"span", model.Display{Mode: model.DisplayFullscreen, Span: true}, false,
			[]string{"/v:127.10.20.30:13389", "/f", "/span"}},
		{"window", model.Display{Mode: model.DisplayWindow, Width: 1600, Height: 900}, false,
			[]string{"/v:127.10.20.30:13389", "/w:1600", "/h:900"}},
		{"admin", model.DefaultDisplay(), true,
			[]string{"/v:127.10.20.30:13389", "/admin"}},
		// Options of other modes are kept in the profile but never passed on.
		{"window ignores monitor options", model.Display{Mode: model.DisplayWindow, Width: 800, Height: 600,
			Multimon: true, Span: true}, true,
			[]string{"/v:127.10.20.30:13389", "/w:800", "/h:600", "/admin"}},
		{"default ignores the window size", model.Display{Mode: model.DisplayDefault, Width: 800, Height: 600,
			Multimon: true}, false,
			[]string{"/v:127.10.20.30:13389"}},
	}
	for _, c := range cases {
		p := model.DefaultProfile()
		p.Display = c.display
		p.Admin = c.admin
		if got := Args(addr, p); !slices.Equal(got, c.want) {
			t.Errorf("%s: Args = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestArgsUsesTheTunnelNotTheTarget(t *testing.T) {
	p := model.DefaultProfile()
	p.Target = model.Target{Host: "rdp.example.com", Port: 3390}
	got := Args(netip.MustParseAddrPort("127.1.2.3:23389"), p)
	if got[0] != "/v:127.1.2.3:23389" {
		t.Fatalf("Args = %q; mstsc must connect to the tunnel entrance", got)
	}
}
