package model_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/model"

	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

const (
	vmessOutbound = `{"protocol":"vmess","settings":{"vnext":[{"address":"proxy.example.com","port":443,` +
		`"users":[{"id":"00000000-0000-4000-8000-000000000000"}]}]}}`
	userID = "b831381d-6324-4d53-ad4f-8cda48b30811"
	// realityKey is a REALITY public key: 32 bytes in unpadded URL-safe base64.
	realityKey = "SbVKOEMjK0sIlbwg4akyBg5mL5KZwwB-ed4eEE7YnRc"
)

// v2ray returns a normalized proxy of a V2Ray-family kind.
func v2ray(kind, secret string, o ProxyOptions) Proxy {
	return Proxy{ID: "a1", Name: "Node", Kind: kind, Server: "proxy.example.com", Port: 443, Secret: secret, Options: o}.Normalize()
}

func TestProxyValidateAcceptsEachKind(t *testing.T) {
	cases := []Proxy{
		DirectProxy(),
		SystemProxy(),
		{ID: "a1", Name: "Local SOCKS", Kind: KindSocks, Server: "127.0.0.1", Port: 10808},
		{ID: "a2", Name: "SOCKS with auth", Kind: KindSocks, Server: "proxy.example.com", Port: 1080,
			Username: "alice", Secret: "s3cret"},
		{ID: "a3", Name: "User without password", Kind: KindSocks, Server: "192.0.2.5", Port: 1080, Username: "alice"},
		{ID: "a4", Name: "HTTP", Kind: KindHTTP, Server: "proxy.example.com", Port: 8080},
		v2ray(KindVMess, userID, ProxyOptions{}),
		v2ray(KindVMess, "a custom id", ProxyOptions{Network: "ws", Path: "/ray?ed=2048", Host: "cdn.example.com", Security: "tls"}),
		v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-vision", Security: "reality", SNI: "www.example.com",
			Fingerprint: "chrome", PublicKey: realityKey, ShortID: "6ba85179e30d4fc2"}),
		v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp", Path: "/x", Mode: "stream-one", Extra: `{"xmux":{}}`,
			Security: "tls", ALPN: "h2, http/1.1", PinnedCerts: strings.Repeat("ab:", 31) + "ab"}),
		v2ray(KindVLESS, userID, ProxyOptions{Network: "grpc", ServiceName: "svc", Mode: "multi", Security: "reality",
			PublicKey: realityKey}),
		v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-vision", Network: "ws",
			Encryption: "mlkem768x25519plus.xorpub.1rtt.100-111-1111.75-0-111." + realityKey}),
		// The masks a server needs, and none at all (a current mKCP server
		// without obfuscation).
		v2ray(KindVMess, userID, ProxyOptions{Network: "kcp", FinalMask: `{"udp":[{"type":"header-wechat"},` +
			`{"type":"mkcp-aes128gcm","settings":{"password":"seed"}}],"quicParams":{"congestion":"bbr"}}`}),
		v2ray(KindVMess, userID, ProxyOptions{Network: "kcp", FinalMask: `{"udp":[]}`}),
		v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp", Extra: `{"xmux":{"maxConcurrency":"16-32",` +
			`"hKeepAlivePeriod":-1},"xPaddingBytes":"100-1000","downloadSettings":{"network":"xhttp",` +
			`"finalmask":{"udp":[{"type":"salamander","settings":{"password":"long enough"}}]}}}`}),
		v2ray(KindHysteria2, "pw", ProxyOptions{ObfsPassword: "four"}),
		v2ray(KindTrojan, "pass word", ProxyOptions{Security: "tls", SNI: "proxy.example.com"}),
		v2ray(KindTrojan, "pw", ProxyOptions{Network: "kcp", HeaderType: "wechat-video", Seed: "seed"}),
		v2ray(KindTrojan, "pw", ProxyOptions{Network: "tcp", HeaderType: "http", Host: "a.example.com, b.example.com",
			Path: "/, /index.html"}),
		v2ray(KindShadowsocks, "pw", ProxyOptions{Cipher: "chacha20-ietf-poly1305"}),
		v2ray(KindShadowsocks, base64.StdEncoding.EncodeToString(make([]byte, 16)), ProxyOptions{Cipher: "2022-blake3-aes-128-gcm"}),
		v2ray(KindShadowsocks, base64.StdEncoding.EncodeToString(make([]byte, 32))+":"+base64.StdEncoding.EncodeToString(make([]byte, 32)),
			ProxyOptions{Cipher: "2022-blake3-aes-256-gcm"}),
		v2ray(KindHysteria2, "user:pass", ProxyOptions{SNI: "proxy.example.com", ObfsPassword: "obfs"}),
		{ID: "a7", Name: "Custom", Kind: KindXray, Outbound: `{"protocol":"freedom"}`},
		// A custom outbound may use any mask, sensibly sized.
		{ID: "a9", Name: "Fragments", Kind: KindXray, Outbound: `{"protocol":"freedom","streamSettings":` +
			`{"finalmask":{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200"}}]}}}`},
		{ID: "a8", Name: "Custom with server", Kind: KindXray, Server: "proxy.example.com", Port: 443,
			Outbound: vmessOutbound},
	}
	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("%s %s: Validate: %v", p.Kind, p.Options.Network, err)
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
		{"vless without a user ID", v2ray(KindVLESS, "", ProxyOptions{}),
			FieldErrors{{Field: "secret", Code: CodeRequired}}},
		{"vmess with a broken UUID", v2ray(KindVMess, "b831381d-6324-4d53-ad4f-8cda48b3081", ProxyOptions{}),
			FieldErrors{{Field: "secret", Code: CodeInvalid}}},
		{"trojan without server", Proxy{ID: "a1", Name: "x", Kind: KindTrojan, Secret: "pw"}.Normalize(),
			FieldErrors{{Field: "server", Code: CodeRequired}, {Field: "port", Code: CodeRequired}}},
		{"hysteria2 with a bad port", Proxy{ID: "a1", Name: "x", Kind: KindHysteria2, Server: "192.0.2.1", Port: -1,
			Secret: "pw"}.Normalize(),
			FieldErrors{{Field: "port", Code: CodeOutOfRange}}},
		{"shadowsocks without a method", v2ray(KindShadowsocks, "pw", ProxyOptions{}),
			FieldErrors{{Field: "options.cipher", Code: CodeRequired}}},
		{"shadowsocks with an unknown method", v2ray(KindShadowsocks, "pw", ProxyOptions{Cipher: "rc4-md5"}),
			FieldErrors{{Field: "options.cipher", Code: CodeUnsupported}}},
		{"shadowsocks 2022 with a short key", v2ray(KindShadowsocks, base64.StdEncoding.EncodeToString(make([]byte, 16)),
			ProxyOptions{Cipher: "2022-blake3-aes-256-gcm"}),
			FieldErrors{{Field: "secret", Code: CodeInvalid}}},
		{"shadowsocks 2022 with a plain password", v2ray(KindShadowsocks, "password",
			ProxyOptions{Cipher: "2022-blake3-aes-128-gcm"}),
			FieldErrors{{Field: "secret", Code: CodeInvalid}}},
		{"vmess with an unknown cipher", v2ray(KindVMess, userID, ProxyOptions{Cipher: "rc4"}),
			FieldErrors{{Field: "options.cipher", Code: CodeUnsupported}}},
		{"a transport Xray no longer has", v2ray(KindVMess, userID, ProxyOptions{Network: "h2"}),
			FieldErrors{{Field: "options.network", Code: CodeUnsupported}}},
		{"quic", v2ray(KindVMess, userID, ProxyOptions{Network: "quic"}),
			FieldErrors{{Field: "options.network", Code: CodeUnsupported}}},
		{"an unknown security", v2ray(KindTrojan, "pw", ProxyOptions{Security: "xtls"}),
			FieldErrors{{Field: "options.security", Code: CodeUnsupported}}},
		{"vision without TLS", v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-vision"}),
			FieldErrors{{Field: "options.flow", Code: CodeConflict}}},
		{"vision over WebSocket", v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-vision", Network: "ws", Security: "tls"}),
			FieldErrors{{Field: "options.flow", Code: CodeConflict}}},
		{"an unknown flow", v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-direct", Security: "tls"}),
			FieldErrors{{Field: "options.flow", Code: CodeUnsupported}}},
		{"an unknown VLESS encryption", v2ray(KindVLESS, userID, ProxyOptions{Encryption: "aes-128-gcm"}),
			FieldErrors{{Field: "options.encryption", Code: CodeUnsupported}}},
		// Xray crashes reading these.
		{"VLESS Encryption without a key", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.native.0rtt.abc"}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"VLESS Encryption with padding after the key", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.native.0rtt." + realityKey + ".100-111"}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"VLESS Encryption with an unknown mode", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.plain.0rtt." + realityKey}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"VLESS Encryption with a key of the wrong size", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.native.1rtt." + realityKey + "AAAA"}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"REALITY without a public key", v2ray(KindVLESS, userID, ProxyOptions{Security: "reality", SNI: "www.example.com"}),
			FieldErrors{{Field: "options.publicKey", Code: CodeRequired}}},
		{"REALITY with bad keys", v2ray(KindVLESS, userID, ProxyOptions{Security: "reality", PublicKey: "abc",
			ShortID: "123", SpiderX: "x", MLDSA65Verify: realityKey}),
			FieldErrors{{Field: "options.publicKey", Code: CodeInvalid}, {Field: "options.shortId", Code: CodeInvalid},
				{Field: "options.spiderX", Code: CodeInvalid}, {Field: "options.mldsa65Verify", Code: CodeInvalid}}},
		{"REALITY with a too long short ID", v2ray(KindVLESS, userID, ProxyOptions{Security: "reality", PublicKey: realityKey,
			ShortID: "0123456789abcdef01"}),
			FieldErrors{{Field: "options.shortId", Code: CodeInvalid}}},
		{"REALITY over WebSocket", v2ray(KindVLESS, userID, ProxyOptions{Network: "ws", Security: "reality", PublicKey: realityKey}),
			FieldErrors{{Field: "options.security", Code: CodeConflict}}},
		{"bad TLS settings", v2ray(KindTrojan, "pw", ProxyOptions{Security: "tls", SNI: "bad name", ALPN: "h2,http/ 1.1",
			Fingerprint: "chrome 120", PinnedCerts: "abcd", VerifyNames: "ok.example.com,not ok"}),
			FieldErrors{{Field: "options.sni", Code: CodeInvalid}, {Field: "options.alpn", Code: CodeInvalid},
				{Field: "options.pinnedCerts", Code: CodeInvalid}, {Field: "options.verifyNames", Code: CodeInvalid},
				{Field: "options.fingerprint", Code: CodeInvalid}}},
		{"paths that are not paths", v2ray(KindVMess, userID, ProxyOptions{Network: "ws", Path: "ray", Host: "a\r\nb"}),
			FieldErrors{{Field: "options.host", Code: CodeInvalid}, {Field: "options.path", Code: CodeInvalid}}},
		{"an HTTP disguise with a bad path", v2ray(KindVMess, userID, ProxyOptions{HeaderType: "http", Path: "/a,b"}),
			FieldErrors{{Field: "options.path", Code: CodeInvalid}}},
		{"unknown disguises", v2ray(KindVMess, userID, ProxyOptions{Network: "kcp", HeaderType: "http"}),
			FieldErrors{{Field: "options.headerType", Code: CodeUnsupported}}},
		{"unknown modes", v2ray(KindVLESS, userID, ProxyOptions{Network: "grpc", Mode: "guna"}),
			FieldErrors{{Field: "options.mode", Code: CodeUnsupported}}},
		{"XHTTP extra that is not an object", v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp", Extra: "[1]"}),
			FieldErrors{{Field: "options.extra", Code: CodeInvalid}}},
		// Xray takes these, then crashes when it connects.
		{"a negative fragment length", v2ray(KindVLESS, userID, ProxyOptions{FinalMask: `{"tcp":[{"type":"fragment",` +
			`"settings":{"packets":"1-3","length":"-5--5"}}]}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeInvalid}}},
		{"fragments from a link", v2ray(KindVLESS, userID, ProxyOptions{FinalMask: `{"tcp":[{"type":"fragment",` +
			`"settings":{"packets":"tlshello","length":"100-200"}}]}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeUnsupported}}},
		{"noise from a link", v2ray(KindVMess, userID, ProxyOptions{Network: "kcp", FinalMask: `{"udp":[{"type":"noise",` +
			`"settings":{"noise":[{"rand":"10-20"}]}}]}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeUnsupported}}},
		{"a short Salamander password in the masks", v2ray(KindVMess, userID, ProxyOptions{Network: "kcp",
			FinalMask: `{"udp":[{"type":"salamander","settings":{"password":"abc"}}]}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeInvalid}}},
		{"negative QUIC parameters", v2ray(KindVMess, userID, ProxyOptions{Network: "kcp",
			FinalMask: `{"quicParams":{"brutalUp":"-5mbps","maxIncomingStreams":-8}}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeInvalid}}},
		{"unknown mask settings", v2ray(KindVMess, userID, ProxyOptions{FinalMask: `{"udp":[],"other":1}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeUnsupported}}},
		{"negative XHTTP sizes", v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp", Extra: `{"xPaddingBytes":"-5--5"}`}),
			FieldErrors{{Field: "options.extra", Code: CodeInvalid}}},
		{"a negative XHTTP number", v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp", Extra: `{"scMaxBufferedPosts":-1}`}),
			FieldErrors{{Field: "options.extra", Code: CodeInvalid}}},
		{"fragments on XHTTP's download path", v2ray(KindVLESS, userID, ProxyOptions{Network: "xhttp",
			Extra: `{"downloadSettings":{"finalmask":{"tcp":[{"type":"fragment"}]}}}`}),
			FieldErrors{{Field: "options.extra", Code: CodeUnsupported}}},
		{"a short Salamander password", v2ray(KindHysteria2, "pw", ProxyOptions{ObfsPassword: "abc"}),
			FieldErrors{{Field: "options.obfsPassword", Code: CodeInvalid}}},
		{"VLESS Encryption with malformed padding", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.native.1rtt.abc." + realityKey}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"VLESS Encryption with too little padding first", v2ray(KindVLESS, userID,
			ProxyOptions{Encryption: "mlkem768x25519plus.native.1rtt.99-35-35." + realityKey}),
			FieldErrors{{Field: "options.encryption", Code: CodeInvalid}}},
		{"vision inside the HTTP disguise", v2ray(KindVLESS, userID, ProxyOptions{Flow: "xtls-rprx-vision",
			HeaderType: "http", Security: "tls"}),
			FieldErrors{{Field: "options.flow", Code: CodeConflict}}},
		{"masks next to the mKCP disguise", v2ray(KindVMess, userID, ProxyOptions{Network: "kcp", Seed: "s",
			FinalMask: `{"udp":[]}`}),
			FieldErrors{{Field: "options.finalMask", Code: CodeConflict}}},
		{"masks that are not JSON", v2ray(KindVMess, userID, ProxyOptions{FinalMask: "udp"}),
			FieldErrors{{Field: "options.finalMask", Code: CodeInvalid}}},
		{"hysteria2 with bad certificate settings", v2ray(KindHysteria2, "pw", ProxyOptions{PinnedCerts: "zz"}),
			FieldErrors{{Field: "options.pinnedCerts", Code: CodeInvalid}}},
		{"xray with an array", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `[{"protocol":"vmess"}]`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray without protocol", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"settings":{}}`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with broken JSON", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"protocol":"vmess"`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with null", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `null`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray without an outbound", Proxy{ID: "a1", Name: "x", Kind: KindXray},
			FieldErrors{{Field: "outbound", Code: CodeRequired}}},
		{"xray with a negative fragment length", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"protocol":"freedom",` +
			`"streamSettings":{"finalmask":{"tcp":[{"type":"fragment","settings":{"length":"-5--5"}}]}}}`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with negative XHTTP sizes", Proxy{ID: "a1", Name: "x", Kind: KindXray, Outbound: `{"protocol":"vless",` +
			`"streamSettings":{"network":"xhttp","xhttpSettings":{"extra":{"uplinkChunkSize":-1}}}}`},
			FieldErrors{{Field: "outbound", Code: CodeInvalid}}},
		{"xray with a huge outbound", Proxy{ID: "a1", Name: "x", Kind: KindXray,
			Outbound: `{"protocol":"freedom","pad":"` + strings.Repeat("x", MaxOutboundLen) + `"}`},
			FieldErrors{{Field: "outbound", Code: CodeTooLong}}},
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

func TestProxyNormalizeKeepsWhatTheKindUses(t *testing.T) {
	all := ProxyOptions{Cipher: "aes-128-gcm", Flow: "xtls-rprx-vision", Encryption: "none", ObfsPassword: "obfs",
		Network: "ws", HeaderType: "http", Host: "h.example.com", Path: "/p", ServiceName: "svc", Authority: "a",
		Mode: "multi", Seed: "seed", Extra: "{}", FinalMask: "{}", Security: "tls", SNI: "s.example.com", ALPN: "h2",
		Fingerprint: "firefox", PinnedCerts: "aa", VerifyNames: "v.example.com", ECH: "ech", PublicKey: "pbk",
		ShortID: "ab", SpiderX: "/s", MLDSA65Verify: "pqv"}
	base := Proxy{Name: "n", Server: "proxy.example.com", Port: 443, Username: "alice", Secret: " secret ",
		Outbound: `{"protocol":"freedom"}`, Options: all}

	cases := []struct {
		kind string
		want Proxy
	}{
		{KindSocks, Proxy{Username: "alice", Secret: " secret "}},
		{KindXray, Proxy{Outbound: `{"protocol":"freedom"}`}},
		{KindShadowsocks, Proxy{Secret: " secret ", Options: ProxyOptions{Cipher: "aes-128-gcm"}}},
		{KindHysteria2, Proxy{Secret: " secret ", Options: ProxyOptions{ObfsPassword: "obfs", SNI: "s.example.com",
			ALPN: "h2", PinnedCerts: "aa", VerifyNames: "v.example.com"}}},
		{KindVMess, Proxy{Secret: "secret", Options: ProxyOptions{Cipher: "aes-128-gcm", Network: "ws",
			Host: "h.example.com", Path: "/p", FinalMask: "{}", Security: "tls", SNI: "s.example.com", ALPN: "h2",
			Fingerprint: "firefox", PinnedCerts: "aa", VerifyNames: "v.example.com", ECH: "ech"}}},
		{KindVLESS, Proxy{Secret: "secret", Options: ProxyOptions{Flow: "xtls-rprx-vision", Encryption: "none",
			Network: "ws", Host: "h.example.com", Path: "/p", FinalMask: "{}", Security: "tls", SNI: "s.example.com",
			ALPN: "h2", Fingerprint: "firefox", PinnedCerts: "aa", VerifyNames: "v.example.com", ECH: "ech"}}},
	}
	for _, c := range cases {
		p := base
		p.Kind = c.kind
		got := p.Normalize()
		want := c.want
		want.Schema, want.Kind, want.Name, want.Server, want.Port = ProxySchema, c.kind, "n", "proxy.example.com", 443
		if got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.kind, got, want)
		}
	}
}

func TestProxyOptionsDefaultsAndSpellings(t *testing.T) {
	cases := []struct {
		name string
		kind string
		in   ProxyOptions
		want ProxyOptions
	}{
		{"vmess defaults", KindVMess, ProxyOptions{},
			ProxyOptions{Cipher: "auto", Network: "tcp", HeaderType: "none", Security: "none"}},
		{"vless defaults", KindVLESS, ProxyOptions{Flow: "None"},
			ProxyOptions{Encryption: "none", Network: "tcp", HeaderType: "none", Security: "none"}},
		{"network aliases", KindTrojan, ProxyOptions{Network: "RAW", Security: "TLS"},
			ProxyOptions{Network: "tcp", HeaderType: "none", Security: "tls"}},
		{"splithttp is XHTTP", KindVMess, ProxyOptions{Network: "splithttp", Path: " /x "},
			ProxyOptions{Cipher: "auto", Network: "xhttp", Path: "/x", Mode: "auto", Security: "none"}},
		{"mkcp and its disguise", KindVMess, ProxyOptions{Network: "mkcp", HeaderType: "wechat", Seed: " s "},
			ProxyOptions{Cipher: "auto", Network: "kcp", HeaderType: "wechat-video", Seed: " s ", Security: "none"}},
		{"mKCP's DNS disguise keeps its domain", KindVMess, ProxyOptions{Network: "kcp", HeaderType: "dns", Host: "d.example.com"},
			ProxyOptions{Cipher: "auto", Network: "kcp", HeaderType: "dns", Host: "d.example.com", Security: "none"}},
		{"gRPC's default mode", KindVMess, ProxyOptions{Network: "grpc"},
			ProxyOptions{Cipher: "auto", Network: "grpc", Mode: "gun", Security: "none"}},
		{"Trojan is TLS unless told otherwise", KindTrojan, ProxyOptions{},
			ProxyOptions{Network: "tcp", HeaderType: "none", Security: "tls"}},
		{"Trojan without TLS", KindTrojan, ProxyOptions{Security: "none"},
			ProxyOptions{Network: "tcp", HeaderType: "none", Security: "none"}},
		{"lists and certificate hashes", KindTrojan, ProxyOptions{Security: "tls", ALPN: " h2 ,, http/1.1 ",
			PinnedCerts: "AB:CD, EF ", VerifyNames: "a.example.com , b.example.com"},
			ProxyOptions{Network: "tcp", HeaderType: "none", Security: "tls", ALPN: "h2,http/1.1", PinnedCerts: "abcd,ef",
				VerifyNames: "a.example.com,b.example.com"}},
		{"REALITY keys in any base64", KindVLESS, ProxyOptions{Security: "reality", PublicKey: "ab+/cd==", ShortID: "AB",
			MLDSA65Verify: "x/y="},
			ProxyOptions{Encryption: "none", Network: "tcp", HeaderType: "none", Security: "reality", PublicKey: "ab-_cd",
				ShortID: "ab", MLDSA65Verify: "x_y"}},
		{"shadowsocks method aliases", KindShadowsocks, ProxyOptions{Cipher: "CHACHA20-IETF-POLY1305"},
			ProxyOptions{Cipher: "chacha20-poly1305"}},
		{"plain is none", KindShadowsocks, ProxyOptions{Cipher: "plain"}, ProxyOptions{Cipher: "none"}},
	}
	for _, c := range cases {
		got := Proxy{Kind: c.kind, Options: c.in}.Normalize().Options
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestValidUserID(t *testing.T) {
	for _, ok := range []string{userID, "b831381d63244d53ad4f8cda48b30811", "B831381D-6324-4D53-AD4F-8CDA48B30811",
		"x", strings.Repeat("x", 30)} {
		if !ValidUserID(ok) {
			t.Errorf("ValidUserID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", 31), strings.Repeat("x", 32), userID + "0",
		"g831381d-6324-4d53-ad4f-8cda48b30811", "b831381d--6324-4d53-ad4f-8cda48b3081"} {
		if ValidUserID(bad) {
			t.Errorf("ValidUserID(%q) = true", bad)
		}
	}
}

func TestKindsExcludeDirect(t *testing.T) {
	if slices.Contains(Kinds, KindDirect) || slices.Contains(Kinds, KindSystem) {
		t.Fatal("users cannot create direct proxies or ones that follow the system; the built-in entries cover them")
	}
	if !ValidID(DirectProxyID) || !ValidID(SystemProxyID) {
		t.Fatal("the built-in entries' IDs must be valid IDs so profiles can reference them")
	}
	for _, id := range []string{DirectProxyID, SystemProxyID} {
		p, ok := BuiltInProxy(id)
		if !ok || p.ID != id || !BuiltInKind(p.Kind) {
			t.Errorf("BuiltInProxy(%s) = %+v, %v", id, p, ok)
		}
	}
	if _, ok := BuiltInProxy("a1"); ok || BuiltInKind(KindSocks) {
		t.Error("a stored proxy is taken for a built-in entry")
	}
	for _, k := range Kinds {
		if IsV2Ray(k) == (k == KindSocks || k == KindHTTP || k == KindXray) {
			t.Errorf("IsV2Ray(%s) = %v", k, IsV2Ray(k))
		}
	}
}
