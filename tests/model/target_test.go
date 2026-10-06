package model_test

import (
	"testing"

	. "github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in   string
		want Target
	}{
		{"example.com", Target{Host: "example.com", Port: 3389}},
		{"  example.com  ", Target{Host: "example.com", Port: 3389}},
		{"example.com:3390", Target{Host: "example.com", Port: 3390}},
		{"192.0.2.10", Target{Host: "192.0.2.10", Port: 3389}},
		{"192.0.2.10:65535", Target{Host: "192.0.2.10", Port: 65535}},
		{"[2001:db8::1]", Target{Host: "2001:db8::1", Port: 3389}},
		{"[2001:db8::1]:3390", Target{Host: "2001:db8::1", Port: 3390}},
		{"2001:db8::1", Target{Host: "2001:db8::1", Port: 3389}}, // bare IPv6: every colon belongs to the address
		{"rdp-host_1", Target{Host: "rdp-host_1", Port: 3389}},
		{"example.com.:1", Target{Host: "example.com.", Port: 1}},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in, DefaultRDPPort)
		if err != nil {
			t.Errorf("ParseTarget(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTarget(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseTargetUsesTheGivenDefaultPort(t *testing.T) {
	got, err := ParseTarget("example.com", 3390)
	if err != nil || got.Port != 3390 {
		t.Fatalf("ParseTarget = %+v, %v; want port 3390", got, err)
	}
}

func TestParseTargetRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"example.com:",
		"example.com:0",
		"example.com:65536",
		"example.com:+80",
		"example.com:80x",
		":3389",
		"[2001:db8::1",
		"[2001:db8::1]x",
		"[2001:db8::1]:",
		"[example.com]:3389", // brackets are only for IPv6
		"[192.0.2.1]",
		"192.0.2.256",
		"exa mple.com",
		"0.0.0.0",
		"[::]:3389",
	} {
		if got, err := ParseTarget(in, DefaultRDPPort); err == nil {
			t.Errorf("ParseTarget(%q) = %+v, want an error", in, got)
		}
	}
}

func TestValidHost(t *testing.T) {
	valid := []string{
		"example.com", "EXAMPLE.COM", "rdp.example.com.", "server1", "my_host", "xn--fsq.example",
		"192.0.2.1", "2001:db8::1", "::ffff:192.0.2.1", "127.0.0.1", "::1",
	}
	for _, h := range valid {
		if !ValidHost(h) {
			t.Errorf("ValidHost(%q) = false, want true", h)
		}
	}
	invalid := []string{
		"", ".", "..", "a..b", "-example.com", "example-.com", "exa mple.com", "例子.com",
		"192.0.2.256", "1.2.3", "123", "0.0.0.0", "::", "224.0.0.1", "ff02::1", "fe80::1%eth0",
		"[2001:db8::1]", "example.com:3389",
	}
	for _, h := range invalid {
		if ValidHost(h) {
			t.Errorf("ValidHost(%q) = true, want false", h)
		}
	}
}

func TestValidHostLengthLimits(t *testing.T) {
	label63 := "a123456789012345678901234567890123456789012345678901234567890bc"
	if len(label63) != 63 || !ValidHost(label63+".example") {
		t.Fatalf("a 63-character label should be valid")
	}
	if ValidHost(label63 + "d.example") {
		t.Fatal("a 64-character label should be invalid")
	}
	long := label63 + "." + label63 + "." + label63 + "." + label63[:61] // 253 characters
	if len(long) != 253 || !ValidHost(long) {
		t.Fatalf("a 253-character name should be valid (len %d)", len(long))
	}
	if ValidHost(long + "x") {
		t.Fatal("a 254-character name should be invalid")
	}
}

func TestTargetString(t *testing.T) {
	cases := map[Target]string{
		{Host: "example.com", Port: 3389}: "example.com:3389",
		{Host: "192.0.2.1", Port: 3390}:   "192.0.2.1:3390",
		{Host: "2001:db8::1", Port: 3389}: "[2001:db8::1]:3389",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", in, got, want)
		}
	}
}

func TestTargetIsLoopback(t *testing.T) {
	yes := []string{"localhost", "LOCALHOST.", "rdp.localhost", "127.0.0.1", "127.12.34.56", "::1", "::ffff:127.0.0.1"}
	for _, h := range yes {
		if !(Target{Host: h, Port: 3389}).IsLoopback() {
			t.Errorf("%q should be loopback", h)
		}
	}
	no := []string{"example.com", "192.0.2.1", "2001:db8::1", "localhost.example.com", "128.0.0.1"}
	for _, h := range no {
		if (Target{Host: h, Port: 3389}).IsLoopback() {
			t.Errorf("%q should not be loopback", h)
		}
	}
}
