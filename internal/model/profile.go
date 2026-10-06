package model

import (
	"slices"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/loopback"
)

// ProfileSchema is the data-format number written into each profile file.
const ProfileSchema = 1

// Display modes. Each maps to mstsc command-line switches (see mstsc.Args).
// Everything else about the remote session (clipboard, drives, audio,
// performance) comes from the user's Default.rdp, because mstsc has no
// switches for it.
const (
	// DisplayDefault passes no display switch, so mstsc uses the screen
	// settings saved in Default.rdp.
	DisplayDefault = "default"
	// DisplayFullscreen is /f, optionally with /multimon or /span.
	DisplayFullscreen = "fullscreen"
	// DisplayWindow is /w and /h.
	DisplayWindow = "window"
)

// DisplayModes lists the display modes in the order the UI offers them.
var DisplayModes = []string{DisplayDefault, DisplayFullscreen, DisplayWindow}

// The window size range mstsc accepts for /w and /h.
const (
	MinDesktopSize = 200
	MaxDesktopSize = 8192
)

// Display is how the remote desktop is shown.
type Display struct {
	Mode string `json:"mode"`
	// Width and Height apply to DisplayWindow. They are kept while another
	// mode is selected, so switching back restores them.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Multimon and Span apply to DisplayFullscreen and exclude each other.
	// Multimon gives the remote session one monitor per local monitor; Span
	// stretches a single remote desktop across all of them.
	Multimon bool `json:"multimon"`
	Span     bool `json:"span"`
}

// DefaultDisplay follows Default.rdp; the window size is only a starting
// value for when the user picks DisplayWindow.
func DefaultDisplay() Display {
	return Display{Mode: DisplayDefault, Width: 1280, Height: 800}
}

// Profile is one remote computer the user connects to (profiles\<id>.json).
type Profile struct {
	Schema int    `json:"schema"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	// Group sorts profiles into sections in the list; empty means none.
	Group  string `json:"group"`
	Target Target `json:"target"`
	// ProxyID is the proxy the tunnel goes through, DirectProxyID or
	// SystemProxyID.
	ProxyID string `json:"proxyId"`
	// Loopback is the profile's own tunnel entrance, 127.a.b.c (see package
	// loopback). It is assigned once when the profile is created.
	Loopback string `json:"loopback"`
	// Username pre-fills the Remote Desktop sign-in: DOMAIN\user or
	// user@domain.
	Username string `json:"username"`
	// RememberPassword keeps a password entered in this app in Windows
	// Credential Manager. When it is false, a password entered for one
	// connection is deleted again when that session ends.
	RememberPassword bool    `json:"rememberPassword"`
	Display          Display `json:"display"`
	// Admin connects to the server's administrative session (mstsc /admin).
	Admin bool `json:"admin"`
}

// DefaultProfile is the starting point for a new profile. ID, Name, Target,
// ProxyID and Loopback still have to be filled in.
func DefaultProfile() Profile {
	return Profile{
		Schema:           ProfileSchema,
		Target:           Target{Port: DefaultRDPPort},
		RememberPassword: true,
		Display:          DefaultDisplay(),
	}
}

// Normalize trims the text fields, removes brackets around an IPv6 target,
// and fills in a missing port and an unknown display mode with defaults.
func (p Profile) Normalize() Profile {
	p.Schema = ProfileSchema
	p.Name = strings.TrimSpace(p.Name)
	p.Group = strings.TrimSpace(p.Group)
	p.Target.Host = normalizeHost(p.Target.Host)
	if p.Target.Port == 0 {
		p.Target.Port = DefaultRDPPort
	}
	p.ProxyID = strings.TrimSpace(p.ProxyID)
	p.Loopback = strings.TrimSpace(p.Loopback)
	p.Username = strings.TrimSpace(p.Username)
	if !slices.Contains(DisplayModes, p.Display.Mode) {
		p.Display.Mode = DisplayDefault
	}
	return p
}

// Validate reports every field that cannot be saved as is, as FieldErrors.
// Whether ProxyID names an existing proxy, and whether Loopback is unique,
// are for the store to check.
func (p Profile) Validate() error {
	var e FieldErrors
	if !ValidID(p.ID) {
		e.add("id", CodeInvalid)
	}
	e.text("name", p.Name, true, MaxNameLen)
	e.text("group", p.Group, false, MaxNameLen)
	e.host("target.host", p.Target.Host, true)
	e.port("target.port", p.Target.Port, true)
	switch {
	case p.ProxyID == "":
		e.add("proxyId", CodeRequired)
	case !ValidID(p.ProxyID):
		e.add("proxyId", CodeInvalid)
	}
	if !loopback.ValidString(p.Loopback) {
		e.add("loopback", CodeInvalid)
	}
	e.text("username", p.Username, false, MaxUsernameLen)
	p.Display.validate(&e)
	return e.err()
}

func (d Display) validate(e *FieldErrors) {
	switch d.Mode {
	case DisplayDefault:
	case DisplayFullscreen:
		if d.Multimon && d.Span {
			e.add("display.span", CodeConflict)
		}
	case DisplayWindow:
		if d.Width < MinDesktopSize || d.Width > MaxDesktopSize {
			e.add("display.width", CodeOutOfRange)
		}
		if d.Height < MinDesktopSize || d.Height > MaxDesktopSize {
			e.add("display.height", CodeOutOfRange)
		}
	default:
		e.add("display.mode", CodeUnsupported)
	}
}
