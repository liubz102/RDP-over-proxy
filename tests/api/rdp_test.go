package api_test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
	"unicode/utf16"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// rdpBytes encodes lines as mstsc saves an .rdp file: UTF-16 with a
// byte-order mark.
func rdpBytes(lines string) []byte {
	b := binary.LittleEndian.AppendUint16(nil, 0xFEFF)
	for _, u := range utf16.Encode([]rune(lines)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func TestParseRDP(t *testing.T) {
	h := newHarness(t)
	existing := h.profile(t, "Office", model.DirectProxyID, "192.0.2.20:3390", "")

	file := rdpBytes("screen mode id:i:1\r\ndesktopwidth:i:1600\r\ndesktopheight:i:900\r\n" +
		"full address:s:192.0.2.20:3390\r\nusername:s:alice\r\ndomain:s:EXAMPLE\r\n" +
		"redirectclipboard:i:1\r\nauthentication level:i:2\r\n" +
		"gatewayhostname:s:gw.example.com\r\ngatewayusagemethod:i:1\r\ngatewayprofileusagemethod:i:1\r\n")
	v, err := h.profiles.ParseRDP(`C:\Users\alice\Documents\Office PC.RDP`, file)
	if err != nil {
		t.Fatal(err)
	}
	want := model.DefaultProfile()
	want.Name = "Office PC"
	want.Target = model.Target{Host: "192.0.2.20", Port: 3390}
	want.Username = `EXAMPLE\alice`
	want.Display = model.Display{Mode: model.DisplayWindow, Width: 1600, Height: 900}
	if v.Profile != want {
		t.Errorf("Profile =\n %+v\nwant\n %+v", v.Profile, want)
	}
	if !v.ViaGateway || v.Gateway != "gw.example.com" {
		t.Errorf("gateway: %v %q", v.ViaGateway, v.Gateway)
	}
	if !slices.Equal(v.Existing, []string{existing.Name}) {
		t.Errorf("Existing = %q, want the profile with the same computer", v.Existing)
	}
	if n := len(h.profiles.List()); n != 1 {
		t.Fatalf("ParseRDP stored something: %d profiles", n)
	}

	// Another port is another computer.
	v, err = h.profiles.ParseRDP("pc.rdp", []byte("full address:s:192.0.2.20\n"))
	if err != nil || v.Profile.Name != "pc" || v.ViaGateway || len(v.Existing) != 0 {
		t.Fatalf("ParseRDP = %+v, %v", v, err)
	}
	// Host names ignore case.
	named := h.profiles.Draft()
	named.Name, named.ProxyID = "Named", model.DirectProxyID
	named.Target = model.Target{Host: "pc.example.com", Port: model.DefaultRDPPort}
	if _, err := h.profiles.Create(named, ""); err != nil {
		t.Fatal(err)
	}
	v, err = h.profiles.ParseRDP("upper.rdp", []byte("full address:s:PC.Example.com\n"))
	if err != nil || !slices.Equal(v.Existing, []string{"Named"}) {
		t.Fatalf("the same name in capitals: %+v, %v", v, err)
	}

	// A file that names no computer, and one too large to be an .rdp file.
	if _, err := h.profiles.ParseRDP("notes.rdp", []byte("just some text\n")); errcode.Of(err) != "rdp.noAddress" {
		t.Errorf("no address: %v", err)
	}
	big := bytes.Repeat([]byte("x"), rdpfile.MaxSize+1)
	if _, err := h.profiles.ParseRDP("big.rdp", big); errcode.Of(err) != "rdp.tooLarge" {
		t.Errorf("too large: %v", err)
	}
}
