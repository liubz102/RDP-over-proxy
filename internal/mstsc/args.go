// Package mstsc starts Windows Remote Desktop Connection (mstsc.exe).
//
// The app always connects with "mstsc /v:<address>" and never through an
// .rdp file: since the April 2026 security update, opening an .rdp file
// shows a warning dialog every time and turns every redirection off, while
// /v: is unaffected. The price is that only what the command line offers can
// differ per connection; clipboard, drives, audio and performance settings
// come from the user's Default.rdp for every connection alike.
package mstsc

import (
	"net/netip"
	"strconv"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Args returns the mstsc command-line arguments that connect to addr (the
// profile's tunnel entrance, 127.a.b.c:port) with the profile's display and
// session options.
func Args(addr netip.AddrPort, p model.Profile) []string {
	args := []string{"/v:" + addr.String()}
	switch p.Display.Mode {
	case model.DisplayFullscreen:
		args = append(args, "/f")
		switch {
		case p.Display.Multimon:
			args = append(args, "/multimon")
		case p.Display.Span:
			args = append(args, "/span")
		}
	case model.DisplayWindow:
		args = append(args, "/w:"+strconv.Itoa(p.Display.Width), "/h:"+strconv.Itoa(p.Display.Height))
	}
	if p.Admin {
		args = append(args, "/admin")
	}
	return args
}
