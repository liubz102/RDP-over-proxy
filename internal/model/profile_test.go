package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// validProfile is a complete profile that passes Validate.
func validProfile() Profile {
	p := DefaultProfile()
	p.ID = "0123456789abcdef"
	p.Name = "Office PC"
	p.Target = Target{Host: "rdp.example.com", Port: 3389}
	p.ProxyID = "fedcba9876543210"
	p.Loopback = "127.10.20.30"
	return p
}

// fieldErrors returns the FieldErrors inside err, failing the test if err is
// something else.
func fieldErrors(t *testing.T, err error) FieldErrors {
	t.Helper()
	var fe FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("error %v (%T) is not FieldErrors", err, err)
	}
	return fe
}

func TestValidProfileValidates(t *testing.T) {
	if err := validProfile().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	p := validProfile()
	p.ProxyID = DirectProxyID
	if err := p.Validate(); err != nil {
		t.Fatalf("the built-in direct proxy should be accepted: %v", err)
	}
}

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()
	if p.Target.Port != DefaultRDPPort || p.Display.Mode != DisplayDefault || !p.RememberPassword {
		t.Fatalf("DefaultProfile() = %+v", p)
	}
	// The window size is pre-filled so choosing "window" starts valid.
	p.Display.Mode = DisplayWindow
	var e FieldErrors
	p.Display.validate(&e)
	if len(e) != 0 {
		t.Fatalf("default window size is invalid: %v", e)
	}
}

func TestProfileValidateReportsEveryProblem(t *testing.T) {
	p := Profile{
		ID:       "Not-An-ID",
		Name:     "",
		Group:    strings.Repeat("g", MaxNameLen+1),
		Target:   Target{Host: "bad host", Port: 70000},
		ProxyID:  "",
		Loopback: "127.0.0.1",
		Username: strings.Repeat("u", MaxUsernameLen+1),
		Display:  Display{Mode: "stretched"},
	}
	fe := fieldErrors(t, p.Validate())
	want := FieldErrors{
		{"id", CodeInvalid},
		{"name", CodeRequired},
		{"group", CodeTooLong},
		{"target.host", CodeInvalid},
		{"target.port", CodeOutOfRange},
		{"proxyId", CodeRequired},
		{"loopback", CodeInvalid},
		{"username", CodeTooLong},
		{"display.mode", CodeUnsupported},
	}
	if !slices.Equal(fe, want) {
		t.Fatalf("Validate() =\n %v\nwant\n %v", fe, want)
	}
}

func TestProfileValidateFields(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Profile)
		field  string
		code   string
	}{
		{"missing host", func(p *Profile) { p.Target.Host = "" }, "target.host", CodeRequired},
		{"missing port", func(p *Profile) { p.Target.Port = 0 }, "target.port", CodeRequired},
		{"proxy id with capitals", func(p *Profile) { p.ProxyID = "ABC" }, "proxyId", CodeInvalid},
		{"empty loopback", func(p *Profile) { p.Loopback = "" }, "loopback", CodeInvalid},
		{"loopback outside 127/8", func(p *Profile) { p.Loopback = "192.0.2.1" }, "loopback", CodeInvalid},
		{"loopback with 255", func(p *Profile) { p.Loopback = "127.1.1.255" }, "loopback", CodeInvalid},
		{"long name", func(p *Profile) { p.Name = strings.Repeat("名", MaxNameLen+1) }, "name", CodeTooLong},
		{"window too narrow", func(p *Profile) {
			p.Display = Display{Mode: DisplayWindow, Width: MinDesktopSize - 1, Height: 600}
		}, "display.width", CodeOutOfRange},
		{"window too tall", func(p *Profile) {
			p.Display = Display{Mode: DisplayWindow, Width: 800, Height: MaxDesktopSize + 1}
		}, "display.height", CodeOutOfRange},
		{"multimon and span", func(p *Profile) {
			p.Display = Display{Mode: DisplayFullscreen, Multimon: true, Span: true}
		}, "display.span", CodeConflict},
	}
	for _, c := range cases {
		p := validProfile()
		c.change(&p)
		err := p.Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil", c.name)
			continue
		}
		if fe := fieldErrors(t, err); !fe.Has(c.field, c.code) {
			t.Errorf("%s: Validate() = %v, want %s %s", c.name, fe, c.field, c.code)
		}
	}
}

func TestProfileValidateIgnoresSettingsOfOtherDisplayModes(t *testing.T) {
	p := validProfile()
	// Window size and monitor options are kept for when the user switches
	// back; they are only checked for the mode that uses them.
	p.Display = Display{Mode: DisplayDefault, Width: 1, Height: 1, Multimon: true, Span: true}
	if err := p.Validate(); err != nil {
		t.Fatalf("default mode: %v", err)
	}
	p.Display = Display{Mode: DisplayFullscreen, Width: 1, Height: 1, Span: true}
	if err := p.Validate(); err != nil {
		t.Fatalf("full screen mode: %v", err)
	}
	p.Display = Display{Mode: DisplayWindow, Width: 1024, Height: 768, Multimon: true, Span: true}
	if err := p.Validate(); err != nil {
		t.Fatalf("window mode: %v", err)
	}
}

func TestProfileNormalize(t *testing.T) {
	p := Profile{
		Name:     "  Office  ",
		Group:    " Work ",
		Target:   Target{Host: " [2001:db8::1] "},
		ProxyID:  " fedcba9876543210 ",
		Loopback: " 127.10.20.30 ",
		Username: ` EXAMPLE\alice `,
		Display:  Display{Mode: "stretched", Width: 1024, Height: 768},
	}.Normalize()
	want := Profile{
		Schema:   ProfileSchema,
		Name:     "Office",
		Group:    "Work",
		Target:   Target{Host: "2001:db8::1", Port: DefaultRDPPort},
		ProxyID:  "fedcba9876543210",
		Loopback: "127.10.20.30",
		Username: `EXAMPLE\alice`,
		Display:  Display{Mode: DisplayDefault, Width: 1024, Height: 768},
	}
	if p != want {
		t.Fatalf("Normalize() =\n %+v\nwant\n %+v", p, want)
	}
}

func TestProfileJSONFieldNames(t *testing.T) {
	data, err := json.Marshal(validProfile())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "id", "name", "group", "target", "proxyId", "loopback",
		"username", "rememberPassword", "display", "admin"} {
		if _, ok := m[key]; !ok {
			t.Errorf("profile JSON has no %q: %s", key, data)
		}
	}
	if len(m) != 11 {
		t.Errorf("profile JSON has %d keys, want 11: %s", len(m), data)
	}
}
