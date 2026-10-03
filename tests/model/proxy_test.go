package model_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/model"

	"slices"
	"strings"
	"testing"
)

const vmessOutbound = `{"protocol":"vmess","settings":{"vnext":[{"address":"proxy.example.com","port":443,` +
	`"users":[{"id":"00000000-0000-4000-8000-000000000000"}]}]}}`

func TestProxyValidateAcceptsEachKind(t *testing.T) {
	cases := []Proxy{
		DirectProxy(),
		{ID: "a1", Name: "Local SOCKS", Kind: KindSocks, Server: "127.0.0.1", Port: 10808},
		{ID: "a2", Name: "SOCKS with auth", Kind: KindSocks, Server: "proxy.example.com", Port: 1080,
			Username: "alice", Secret: "s3cret"},
		{ID: "a3", Name: "User without password", Kind: KindSocks, Server: "192.0.2.5", Port: 1080, Username: "alice"},
		{ID: "a4", Name: "HTTP", Kind: KindHTTP, Server: "proxy.example.com", Port: 8080},
		{ID: "a5", Name: "VMess", Kind: KindVMess, Server: "proxy.example.com", Port: 443, Outbound: vmessOutbound},
		{ID: "a6", Name: "Shadowsocks", Kind: KindShadowsocks, Server: "192.0.2.6", Port: 8388,
			Outbound: `{"protocol":"shadowsocks"}`},
		{ID: "a7", Name: "Custom", Kind: KindXray, Outbound: `{"protocol":"freedom"}`},
		{ID: "a8", Name: "Custom with server", Kind: KindXray, Server: "proxy.example.com", Port: 443,
			Outbound: vmessOutbound},
	}
	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", p.Name, err)
		}
	}
}

func TestProxyValidateRejects(t *testing.T) {
	cases := []struct {
		name  string
		proxy Proxy
		want  FieldErrors
	}{
		{"no kind", Proxy{ID: "a1"}, FieldErrors{{Field: "kind", Code: CodeRequired}}},
		{"unknown kind", Proxy{ID: "a1", Kind: "tor"}, FieldErrors{{Field: "kind", Code: CodeUnsupported}}},
		{"bad id", Proxy{ID: "A 1", Kind: KindDirect}, FieldErrors{{Field: "id", Code: CodeInvalid}}},
		{"socks without anything", Proxy{ID: "a1", Kind: KindSocks},
			FieldErrors{{Field: "name", Code: CodeRequired}, {Field: "server", Code: CodeRequired}, {Field: "port", Code: CodeRequired}}},
		{"socks with a bad server and port", Proxy{ID: "a1", Name: "x", Kind: KindSocks, Server: "bad host", Port: 99999},
			FieldErrors{{Field: "server", Code: CodeInvalid}, {Field: "port", Code: CodeOutOfRange}}},
		{"password without user", Proxy{ID: "a1", Name: "x", Kind: KindHTTP, Server: "192.0.2.1", Port: 8080, Secret: "pw"},
			FieldErrors{{Field: "username", Code: CodeRequired}}},
		{"over-long credentials", Proxy{ID: "a1", Name: "x", Kind: KindSocks, Server: "192.0.2.1", Port: 1080,
			Username: strings.Repeat("u", 256), Secret: strings.Repeat("p", 256)},
			FieldErrors{{Field: "username", Code: CodeTooLong}, {Field: "secret", Code: CodeTooLong}}},
		{"vless without outbound", Proxy{ID: "a1", Name: "x", Kind: KindVLESS, Server: "192.0.2.1", Port: 443},
			FieldErrors{{Field: "outbound", Code: CodeRequired}}},
		{"trojan without server", Proxy{ID: "a1", Name: "x", Kind: KindTrojan, Outbound: `{"protocol":"trojan"}`},
			FieldErrors{{Field: "server", Code: CodeRequired}, {Field: "port", Code: CodeRequired}}},
		{"xray with an array", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `[{"protocol":"vmess"}]`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray without protocol", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"settings":{}}`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with broken JSON", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"protocol":"vmess"`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with null", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `null`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with a huge outbound", Proxy{ID: "a1", Name: "x", Kind: KindXray,
			Outbound: `{"protocol":"freedom","pad":"` + strings.Repeat("x", MaxOutboundLen) + `"}`},
			FieldErrors{{Field: "outbound", Code: CodeTooLong}}},
		{"hysteria2 with a bad port", Proxy{ID: "a1", Name: "x", Kind: KindHysteria2, Server: "192.0.2.1", Port: -1,
			Outbound: `{"protocol":"hysteria"}`},
			FieldErrors{{Field: "port", Code: CodeOutOfRange}}},
	}
	for _, c := range cases {
		err := c.proxy.Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil, want %v", c.name, c.want)
			continue
		}
		if got := fieldErrors(t, err); !slices.Equal(got, c.want) {
			t.Errorf("%s: Validate() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProxyNormalizeKeepsTheSecret(t *testing.T) {
	p := Proxy{Name: " Home ", Server: " [2001:db8::2] ", Username: " alice ", Secret: " pass word ",
		Outbound: "\n{\"protocol\":\"vmess\"}\n"}.Normalize()
	want := Proxy{Schema: ProxySchema, Name: "Home", Server: "2001:db8::2", Username: "alice",
		Secret: " pass word ", Outbound: `{"protocol":"vmess"}`}
	if p != want {
		t.Fatalf("Normalize() = %+v, want %+v", p, want)
	}
}

func TestKindsExcludeDirect(t *testing.T) {
	if slices.Contains(Kinds, KindDirect) {
		t.Fatal("users cannot create direct proxies; the built-in entry covers it")
	}
	if !ValidID(DirectProxyID) {
		t.Fatal("DirectProxyID must be a valid ID so profiles can reference it")
	}
}
