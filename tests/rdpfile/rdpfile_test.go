package rdpfile_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/rdpfile"

	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func readTestdata(t *testing.T, name string) *File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Parse(data)
}

func utf16Bytes(s string, order binary.AppendByteOrder, bom bool) []byte {
	var b []byte
	if bom {
		b = order.AppendUint16(b, 0xFEFF)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = order.AppendUint16(b, u)
	}
	return b
}

func TestParseMstscSavedFile(t *testing.T) {
	f := readTestdata(t, "mstsc-saved.rdp")
	draft, err := f.Draft(" Office PC ")
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	want := model.DefaultProfile()
	want.Name = "Office PC"
	want.Target = model.Target{Host: "rdp.example.com", Port: 3390}
	want.Username = `EXAMPLE\alice`
	want.Display = model.Display{Mode: model.DisplayWindow, Width: 1600, Height: 900}
	if draft != want {
		t.Fatalf("Draft =\n %+v\nwant\n %+v", draft, want)
	}
	if g := f.Gateway(); g != (Gateway{Usage: UsageNoneBypassLocal, AdminDefaults: true, ProfileStated: true}) || g.Verdict() != GatewayNotUsed {
		t.Fatalf("Gateway = %+v (verdict %d), want the defaults mstsc writes", g, g.Verdict())
	}
	if v, ok := f.String("winposstr"); !ok || v != "0,1,262,57,1886,1003" {
		t.Errorf("String(winposstr) = %q, %v", v, ok)
	}
	if v, ok := f.String("alternate shell"); !ok || v != "" {
		t.Errorf("an empty string property should be present: %q, %v", v, ok)
	}
}

func TestParseGatewayFile(t *testing.T) {
	f := readTestdata(t, "gateway.rdp")
	draft, err := f.Draft("Server")
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if draft.Target != (model.Target{Host: "192.0.2.20", Port: 3389}) {
		t.Errorf("Target = %+v", draft.Target)
	}
	if draft.Username != `EXAMPLE\bob` {
		t.Errorf("Username = %q, want the domain joined on", draft.Username)
	}
	if draft.Display != (model.Display{Mode: model.DisplayFullscreen, Width: 1280, Height: 800, Multimon: true}) {
		t.Errorf("Display = %+v", draft.Display)
	}
	if !draft.Admin {
		t.Error("Admin = false, want true")
	}
	g := f.Gateway()
	if g != (Gateway{Host: "gateway.example.com", Usage: UsageAlways, ProfileStated: true}) || g.Verdict() != GatewayUsed {
		t.Errorf("Gateway = %+v (verdict %d)", g, g.Verdict())
	}
}

func TestParseEncodings(t *testing.T) {
	const text = "full address:s:例子.example:3390\r\nusername:s:用户\r\n"
	cases := map[string][]byte{
		"UTF-16LE with BOM":    utf16Bytes(text, binary.LittleEndian, true),
		"UTF-16BE with BOM":    utf16Bytes(text, binary.BigEndian, true),
		"UTF-16LE without BOM": utf16Bytes(text, binary.LittleEndian, false),
		"UTF-16BE without BOM": utf16Bytes(text, binary.BigEndian, false),
		"UTF-8 with BOM":       append([]byte{0xEF, 0xBB, 0xBF}, text...),
		"UTF-8 without BOM":    []byte(text),
	}
	for name, data := range cases {
		f := Parse(data)
		if addr, _ := f.String("full address"); addr != "例子.example:3390" {
			t.Errorf("%s: full address = %q", name, addr)
		}
		if u := f.Username(); u != "用户" {
			t.Errorf("%s: username = %q", name, u)
		}
	}
}

func TestParseInvalidBytesDoNotSpoilTheFile(t *testing.T) {
	// A file saved in a legacy code page: the user name is not valid UTF-8,
	// but the address still reads correctly.
	data := []byte("full address:s:rdp.example.com\nusername:s:\xd3\xc3\xbb\xa7\n")
	f := Parse(data)
	if tgt, err := f.Target(); err != nil || tgt.Host != "rdp.example.com" {
		t.Fatalf("Target = %+v, %v", tgt, err)
	}
	if u := f.Username(); !utf8.ValidString(u) || !strings.Contains(u, "\uFFFD") {
		t.Errorf("username = %q, want valid UTF-8 with replacement characters", u)
	}
	// An odd trailing byte in UTF-16 is dropped rather than misread.
	odd := append(utf16Bytes("full address:s:192.0.2.1\r\n", binary.LittleEndian, true), 'x')
	if tgt, err := Parse(odd).Target(); err != nil || tgt.Host != "192.0.2.1" {
		t.Fatalf("odd-length UTF-16: Target = %+v, %v", tgt, err)
	}
}

func TestParseLines(t *testing.T) {
	data := []byte("" +
		"Full Address:S:first.example.com\n" + // names and types ignore case
		"no colon here\n" +
		"only:one colon\n" +
		":s:no name\n" +
		"bad type:x:value\n" +
		"\n" +
		"   desktopwidth:i: 1024   \r\n" +
		"desktopheight:i:not a number\n" +
		"full address:s:second.example.com:3391\n" + // the last occurrence wins
		"password 51:b:01000000D08C9DDF\n")
	f := Parse(data)
	if v, _ := f.String("FULL ADDRESS"); v != "second.example.com:3391" {
		t.Errorf("full address = %q", v)
	}
	if n, ok := f.Int("desktopwidth"); !ok || n != 1024 {
		t.Errorf("desktopwidth = %d, %v", n, ok)
	}
	if _, ok := f.Int("desktopheight"); ok {
		t.Error("a malformed integer should read as absent")
	}
	if _, ok := f.String("desktopwidth"); ok {
		t.Error("an integer property should not read as a string")
	}
	if _, ok := f.Int("full address"); ok {
		t.Error("a string property should not read as an integer")
	}
	if _, ok := f.String("bad type"); ok {
		t.Error("a line with an unknown type should be skipped")
	}
	// Malformed lines are skipped, not read as properties.
	for _, name := range []string{"no colon here", "only", ""} {
		if _, ok := f.String(name); ok {
			t.Errorf("the malformed line for %q was read as a property", name)
		}
	}
}

func TestTarget(t *testing.T) {
	cases := []struct {
		lines string
		want  model.Target
	}{
		{"full address:s:rdp.example.com", model.Target{Host: "rdp.example.com", Port: 3389}},
		{"full address:s:rdp.example.com\nserver port:i:3390", model.Target{Host: "rdp.example.com", Port: 3390}},
		{"full address:s:rdp.example.com:3391\nserver port:i:3390", model.Target{Host: "rdp.example.com", Port: 3391}},
		{"full address:s:rdp.example.com\nserver port:i:0", model.Target{Host: "rdp.example.com", Port: 3389}},
		{"full address:s:[2001:db8::5]:3390", model.Target{Host: "2001:db8::5", Port: 3390}},
		{"full address:s:2001:db8::5", model.Target{Host: "2001:db8::5", Port: 3389}},
		{"full address:s:\nalternate full address:s:alt.example.com", model.Target{Host: "alt.example.com", Port: 3389}},
		{"full address:s:main.example.com\nalternate full address:s:alt.example.com", model.Target{Host: "main.example.com", Port: 3389}},
	}
	for _, c := range cases {
		got, err := Parse([]byte(c.lines)).Target()
		if err != nil {
			t.Errorf("%q: %v", c.lines, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: Target = %+v, want %+v", c.lines, got, c.want)
		}
	}
}

func TestTargetErrors(t *testing.T) {
	if _, err := Parse([]byte("username:s:alice")).Target(); !errors.Is(err, ErrNoAddress) {
		t.Errorf("no address: err = %v, want ErrNoAddress", err)
	}
	if _, err := Parse([]byte("full address:s:bad host name")).Target(); err == nil {
		t.Error("an invalid address should be an error")
	}
	if _, err := Parse(nil).Draft("x"); !errors.Is(err, ErrNoAddress) {
		t.Errorf("Draft of an empty file: err = %v, want ErrNoAddress", err)
	}
}

func TestUsername(t *testing.T) {
	cases := map[string]string{
		"username:s:alice":                          "alice",
		"username:s:alice\ndomain:s:EXAMPLE":        `EXAMPLE\alice`,
		"username:s:OTHER\\alice\ndomain:s:EXAMPLE": `OTHER\alice`,
		"username:s:alice@example.com\ndomain:s:X":  "alice@example.com",
		"domain:s:EXAMPLE":                          "",
		"":                                          "",
	}
	for lines, want := range cases {
		if got := Parse([]byte(lines)).Username(); got != want {
			t.Errorf("%q: Username = %q, want %q", lines, got, want)
		}
	}
}

func TestDisplay(t *testing.T) {
	def := model.DefaultDisplay()
	cases := []struct {
		lines string
		want  model.Display
	}{
		{"", def},
		{"screen mode id:i:1\ndesktopwidth:i:1024\ndesktopheight:i:768",
			model.Display{Mode: model.DisplayWindow, Width: 1024, Height: 768}},
		{"screen mode id:i:1\ndesktopwidth:i:100\ndesktopheight:i:768", def}, // too small for /w
		{"screen mode id:i:1", def},
		{"screen mode id:i:2", model.Display{Mode: model.DisplayFullscreen, Width: 1280, Height: 800}},
		{"screen mode id:i:2\nspan monitors:i:1",
			model.Display{Mode: model.DisplayFullscreen, Width: 1280, Height: 800, Span: true}},
		{"screen mode id:i:2\nuse multimon:i:1\nspan monitors:i:1", // the two exclude each other
			model.Display{Mode: model.DisplayFullscreen, Width: 1280, Height: 800, Multimon: true}},
		{"screen mode id:i:3", def},
	}
	for _, c := range cases {
		if got := Parse([]byte(c.lines)).Display(); got != c.want {
			t.Errorf("%q: Display = %+v, want %+v", c.lines, got, c.want)
		}
	}
}

func TestAdmin(t *testing.T) {
	cases := map[string]bool{
		"administrative session:i:1": true,
		"connect to console:i:1":     true,
		"administrative session:i:0": false,
		"":                           false,
	}
	for lines, want := range cases {
		if got := Parse([]byte(lines)).Admin(); got != want {
			t.Errorf("%q: Admin = %v, want %v", lines, got, want)
		}
	}
}

func TestGatewayVerdict(t *testing.T) {
	cases := []struct {
		lines string
		want  GatewayVerdict
	}{
		{"", GatewayNotUsed},
		// "Use these RD Gateway server settings": the file's own usage decides.
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:0\ngatewayhostname:s:gw.example.com", GatewayNotUsed},
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:4", GatewayNotUsed},
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:1\ngatewayhostname:s:gw.example.com", GatewayUsed},
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:2\ngatewayhostname:s:gw.example.com", GatewayMaybeUsed},
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:3", GatewayMaybeUsed},
		{"gatewayprofileusagemethod:i:1\ngatewayusagemethod:i:9", GatewayMaybeUsed},
		// "Automatically detect": the usage left over from the greyed-out
		// settings does not count, not even "always" (as in a real Default.rdp).
		{"gatewayusagemethod:i:1\ngatewayprofileusagemethod:i:0\ngatewayhostname:s:gw.example.com", GatewayNotUsed},
		{"gatewayprofileusagemethod:i:0\ngatewayusagemethod:i:2", GatewayNotUsed},
		// Not stated (a hand-written file): "always" may or may not count.
		{"gatewayusagemethod:i:1\ngatewayhostname:s:gw.example.com", GatewayMaybeUsed},
		{"gatewayusagemethod:i:0", GatewayNotUsed},
		{"gatewayusagemethod:i:2", GatewayMaybeUsed},
	}
	for _, c := range cases {
		if got := Parse([]byte(c.lines)).Gateway().Verdict(); got != c.want {
			t.Errorf("%q: Verdict = %d, want %d", c.lines, got, c.want)
		}
	}
	if !Parse([]byte("gatewayprofileusagemethod:i:0")).Gateway().AdminDefaults {
		t.Error("profile usage 0 defers to the administrator's settings")
	}
	if Parse([]byte("gatewayprofileusagemethod:i:1")).Gateway().AdminDefaults {
		t.Error("profile usage 1 uses the file's own settings")
	}
}

func TestSignInSettings(t *testing.T) {
	cases := []struct {
		lines  string
		auth   ServerAuth
		prompt bool
	}{
		{"", ServerAuthUnspecified, false},
		{"authentication level:i:0", ServerAuthConnect, false},
		{"authentication level:i:1", ServerAuthRefuse, false},
		{"authentication level:i:2\nprompt for credentials:i:0", ServerAuthWarn, false},
		{"authentication level:i:3", ServerAuthUnspecified, false},
		{"authentication level:i:7\nprompt for credentials:i:1", ServerAuthUnspecified, true}, // not a value mstsc defines
		{"authentication level:s:1", ServerAuthUnspecified, false},                            // not an integer
	}
	for _, c := range cases {
		f := Parse([]byte(c.lines))
		if got := f.ServerAuth(); got != c.auth {
			t.Errorf("%q: ServerAuth = %d, want %d", c.lines, got, c.auth)
		}
		if got := f.AlwaysPrompt(); got != c.prompt {
			t.Errorf("%q: AlwaysPrompt = %v, want %v", c.lines, got, c.prompt)
		}
	}
	// What mstsc writes into a file it saves.
	if f := readTestdata(t, "mstsc-saved.rdp"); f.ServerAuth() != ServerAuthWarn || f.AlwaysPrompt() {
		t.Errorf("mstsc-saved.rdp: ServerAuth = %d, AlwaysPrompt = %v", f.ServerAuth(), f.AlwaysPrompt())
	}
}
