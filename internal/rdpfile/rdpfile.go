// Package rdpfile reads Remote Desktop (.rdp) files. It never writes them:
// the app starts mstsc with /v: instead (see docs/ARCHITECTURE.md). Reading
// is for importing connections and for checking whether the user's
// Default.rdp would send connections to an RD Gateway instead of the tunnel.
//
// An .rdp file is a list of "name:type:value" lines, where type is s
// (string), i (integer) or b (binary, hex). mstsc saves them as UTF-16.
package rdpfile

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Property names this package reads.
const (
	propFullAddress      = "full address"
	propAltFullAddress   = "alternate full address"
	propServerPort       = "server port"
	propUsername         = "username"
	propDomain           = "domain"
	propScreenMode       = "screen mode id"
	propDesktopWidth     = "desktopwidth"
	propDesktopHeight    = "desktopheight"
	propUseMultimon      = "use multimon"
	propSpanMonitors     = "span monitors"
	propAdminSession     = "administrative session"
	propConnectToConsole = "connect to console"
	propGatewayHost      = "gatewayhostname"
	propGatewayUsage     = "gatewayusagemethod"
	propGatewayProfile   = "gatewayprofileusagemethod"
	propServerAuth       = "authentication level"
	propPromptCreds      = "prompt for credentials"
)

// "screen mode id" values.
const (
	screenWindowed   = 1
	screenFullscreen = 2
)

// ErrNoAddress is returned by Target when the file names no computer.
var ErrNoAddress = errcode.New("rdp.noAddress", "the .rdp file has no \"full address\"")

// MaxSize is the largest file taken for an .rdp file. mstsc writes a few
// kilobytes; anything near this size is some other file chosen by mistake.
const MaxSize = 1 << 20

// ErrTooLarge: the file is larger than MaxSize.
var ErrTooLarge = errcode.New("rdp.tooLarge", "the file is too large to be an .rdp file")

// File is a parsed .rdp file.
type File struct {
	props map[string]property
}

type property struct {
	typ   byte // 's', 'i' or 'b'
	value string
}

// Parse decodes data and reads its properties. data may be UTF-16 (little or
// big endian, as mstsc saves it) or UTF-8, with or without a byte-order mark;
// bytes that are not valid in the detected encoding become U+FFFD instead of
// failing the whole file. Lines that are not "name:type:value" are skipped.
// Names are compared without regard to case, and when a name appears more
// than once the last line wins, as each line sets the property.
func Parse(data []byte) *File {
	f := &File{props: map[string]property{}}
	for line := range strings.Lines(decode(data)) {
		name, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		typ, value, ok := strings.Cut(rest, ":")
		name = strings.ToLower(strings.TrimSpace(name))
		typ = strings.ToLower(typ)
		if !ok || name == "" || (typ != "s" && typ != "i" && typ != "b") {
			continue
		}
		f.props[name] = property{typ: typ[0], value: strings.TrimSpace(value)}
	}
	return f
}

// decode turns the raw file into text.
func decode(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], binary.LittleEndian)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], binary.BigEndian)
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		data = data[3:]
	// Without a byte-order mark, UTF-16 shows as a zero byte next to the
	// first character, which in an .rdp file is always an ASCII letter of a
	// property name. UTF-8 text never contains zero bytes.
	case len(data) >= 2 && data[0] != 0 && data[1] == 0:
		return decodeUTF16(data, binary.LittleEndian)
	case len(data) >= 2 && data[0] == 0 && data[1] != 0:
		return decodeUTF16(data, binary.BigEndian)
	}
	return strings.ToValidUTF8(string(data), "�")
}

func decodeUTF16(b []byte, order binary.ByteOrder) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = order.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units))
}

// String returns the value of a string property, and whether the file has
// it.
func (f *File) String(name string) (string, bool) {
	p, ok := f.props[strings.ToLower(name)]
	if !ok || p.typ != 's' {
		return "", false
	}
	return p.value, true
}

// Int returns the value of an integer property, and whether the file has a
// well-formed one.
func (f *File) Int(name string) (int, bool) {
	p, ok := f.props[strings.ToLower(name)]
	if !ok || p.typ != 'i' {
		return 0, false
	}
	n, err := strconv.Atoi(p.value)
	return n, err == nil
}

// flag reports whether an integer property is present and set to 1.
func (f *File) flag(name string) bool {
	n, ok := f.Int(name)
	return ok && n == 1
}

// Target returns the computer the file connects to. The address comes from
// "full address", or "alternate full address" when that is empty; a port in
// the address wins over "server port", which wins over 3389.
func (f *File) Target() (model.Target, error) {
	addr, _ := f.String(propFullAddress)
	if addr == "" {
		addr, _ = f.String(propAltFullAddress)
	}
	if addr == "" {
		return model.Target{}, ErrNoAddress
	}
	port := model.DefaultRDPPort
	if p, ok := f.Int(propServerPort); ok && p >= 1 && p <= 65535 {
		port = p
	}
	return model.ParseTarget(addr, port)
}

// Username returns the user name to sign in with. A separate "domain"
// property is joined to it as DOMAIN\user unless the name already carries a
// domain.
func (f *File) Username() string {
	user, _ := f.String(propUsername)
	domain, _ := f.String(propDomain)
	if user != "" && domain != "" && !strings.ContainsAny(user, `\@`) {
		return domain + `\` + user
	}
	return user
}

// Display returns the screen settings in the terms of model.Display. A
// windowed file without a usable size, or a file without "screen mode id",
// maps to model.DisplayDefault (follow Default.rdp).
func (f *File) Display() model.Display {
	d := model.DefaultDisplay()
	mode, _ := f.Int(propScreenMode)
	switch mode {
	case screenFullscreen:
		d.Mode = model.DisplayFullscreen
		d.Multimon = f.flag(propUseMultimon)
		d.Span = !d.Multimon && f.flag(propSpanMonitors)
	case screenWindowed:
		w, okW := f.Int(propDesktopWidth)
		h, okH := f.Int(propDesktopHeight)
		if okW && okH && sizeInRange(w) && sizeInRange(h) {
			d.Mode = model.DisplayWindow
			d.Width, d.Height = w, h
		}
	}
	return d
}

func sizeInRange(n int) bool { return n >= model.MinDesktopSize && n <= model.MaxDesktopSize }

// Admin reports whether the file connects to the administrative session.
// "connect to console" is the name older clients used.
func (f *File) Admin() bool {
	return f.flag(propAdminSession) || f.flag(propConnectToConsole)
}

// Draft turns the file into a new profile named name. ID, ProxyID and
// Loopback are left for the caller to fill in.
func (f *File) Draft(name string) (model.Profile, error) {
	t, err := f.Target()
	if err != nil {
		return model.Profile{}, err
	}
	p := model.DefaultProfile()
	p.Name = strings.TrimSpace(name)
	p.Target = t
	p.Username = f.Username()
	p.Display = f.Display()
	p.Admin = f.Admin()
	return p, nil
}
