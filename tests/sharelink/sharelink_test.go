package sharelink_test

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/sharelink"
)

const (
	uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	pbk  = "SbVKOEMjK0sIlbwg4akyBg5mL5KZwwB-ed4eEE7YnRc"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// proxy is what a link should come out as, before Normalize fills in the
// defaults.
func proxy(kind, name, server string, port int, secret string, o model.ProxyOptions) model.Proxy {
	return model.Proxy{Kind: kind, Name: name, Server: server, Port: port, Secret: secret, Options: o}.Normalize()
}

func TestParse(t *testing.T) {
	cases := []struct {
		name  string
		link  string
		want  model.Proxy
		notes []sharelink.Note
	}{
		{"VLESS with REALITY", "vless://" + uuid + "@proxy.example.com:443?encryption=none&flow=xtls-rprx-vision" +
			"&security=reality&sni=www.example.com&fp=chrome&pbk=" + pbk + "&sid=6ba85179e30d4fc2&spx=%2F&type=tcp" +
			"&headerType=none&allowInsecure=1#Tokyo%20VLESS",
			proxy(model.KindVLESS, "Tokyo VLESS", "proxy.example.com", 443, uuid, model.ProxyOptions{
				Flow: "xtls-rprx-vision", Security: "reality", SNI: "www.example.com", Fingerprint: "chrome",
				PublicKey: pbk, ShortID: "6ba85179e30d4fc2"}), nil},
		{"VLESS over WebSocket and TLS that asks to skip verification",
			"vless://" + uuid + "@[2001:db8::1]:8443?type=ws&host=cdn.example.com&path=%2Fray%3Fed%3D2048" +
				"&security=tls&sni=cdn.example.com&alpn=h2%2Chttp%2F1.1&allowInsecure=1#WS",
			proxy(model.KindVLESS, "WS", "2001:db8::1", 8443, uuid, model.ProxyOptions{Network: "ws",
				Host: "cdn.example.com", Path: "/ray?ed=2048", Security: "tls", SNI: "cdn.example.com", ALPN: "h2,http/1.1"}),
			[]sharelink.Note{{Code: sharelink.NoteInsecure}}},
		{"VLESS over XHTTP with extra settings and masks",
			"vless://" + uuid + "@proxy.example.com:443?type=xhttp&path=%2Fx&mode=stream-up" +
				"&extra=%7B%22xmux%22%3A%7B%22maxConcurrency%22%3A%2216-32%22%7D%7D&fm=%7B%22udp%22%3A%5B%5D%7D" +
				"&security=tls&pcs=AB%3ACD&vcn=a.example.com&ech=AEX%2B#X",
			proxy(model.KindVLESS, "X", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "xhttp", Path: "/x",
				Mode: "stream-up", Extra: `{"xmux":{"maxConcurrency":"16-32"}}`, FinalMask: `{"udp":[]}`,
				Security: "tls", PinnedCerts: "abcd", VerifyNames: "a.example.com", ECH: "AEX+"}), nil},
		{"VLESS over gRPC", "vless://" + uuid + "@proxy.example.com:443?type=grpc&serviceName=svc&authority=a.example.com" +
			"&mode=multi&security=reality&pbk=" + pbk + "&pqv=QUJD",
			proxy(model.KindVLESS, "proxy.example.com", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "grpc",
				ServiceName: "svc", Authority: "a.example.com", Mode: "multi", Security: "reality", PublicKey: pbk,
				MLDSA65Verify: "QUJD"}), nil},
		{"VLESS with VLESS Encryption", "vless://" + uuid + "@proxy.example.com:443?encryption=mlkem768x25519plus.native.0rtt.abc&type=tcp",
			proxy(model.KindVLESS, "proxy.example.com", "proxy.example.com", 443, uuid,
				model.ProxyOptions{Encryption: "mlkem768x25519plus.native.0rtt.abc"}), nil},
		{"Trojan is TLS by default", "trojan://p%40ss%3Aw%2Frd@proxy.example.com:443?sni=proxy.example.com#Trojan",
			proxy(model.KindTrojan, "Trojan", "proxy.example.com", 443, "p@ss:w/rd",
				model.ProxyOptions{Security: "tls", SNI: "proxy.example.com"}), nil},
		{"Trojan with an unescaped @ in the password and the old peer parameter",
			"trojan://p@ss@proxy.example.com?peer=sni.example.com&type=ws&path=/t",
			proxy(model.KindTrojan, "proxy.example.com", "proxy.example.com", 443, "p@ss",
				model.ProxyOptions{Network: "ws", Path: "/t", Security: "tls", SNI: "sni.example.com"}), nil},
		{"Trojan without TLS, over mKCP", "trojan://pw@192.0.2.1:3000?security=none&type=kcp&headerType=wechat-video&seed=s%20s",
			proxy(model.KindTrojan, "192.0.2.1", "192.0.2.1", 3000, "pw",
				model.ProxyOptions{Network: "kcp", HeaderType: "wechat-video", Seed: "s s", Security: "none"}), nil},
		{"v2rayN's VMess over WebSocket and TLS", "vmess://" + b64(`{"v":"2","ps":"VMess 节点","add":"proxy.example.com",`+
			`"port":"443","id":"`+uuid+`","aid":"0","scy":"auto","net":"ws","type":"none","host":"cdn.example.com",`+
			`"path":"/ray","tls":"tls","sni":"cdn.example.com","alpn":"","fp":"chrome"}`),
			proxy(model.KindVMess, "VMess 节点", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "ws",
				Host: "cdn.example.com", Path: "/ray", Security: "tls", SNI: "cdn.example.com", Fingerprint: "chrome"}), nil},
		{"v2rayN's VMess with numbers and an alterId", "vmess://" + strings.TrimRight(b64(`{"add":"192.0.2.2","port":10086,`+
			`"id":"`+uuid+`","aid":64,"net":"tcp","type":"http","host":"a.example.com,b.example.com","path":"/,/x",`+
			`"tls":"","test":"","remark":"x"}`), "="),
			proxy(model.KindVMess, "192.0.2.2", "192.0.2.2", 10086, uuid, model.ProxyOptions{HeaderType: "http",
				Host: "a.example.com,b.example.com", Path: "/,/x"}),
			[]sharelink.Note{{Code: sharelink.NoteAlterID, Args: map[string]string{"value": "64"}},
				{Code: sharelink.NoteIgnored, Args: map[string]string{"params": "remark"}}}},
		{"v2rayN's VMess over mKCP", "vmess://" + b64(`{"v":"2","ps":"kcp","add":"proxy.example.com","port":"3000",`+
			`"id":"`+uuid+`","aid":"0","scy":"aes-128-gcm","net":"kcp","type":"wechat-video","path":"seed"}`),
			proxy(model.KindVMess, "kcp", "proxy.example.com", 3000, uuid, model.ProxyOptions{Cipher: "aes-128-gcm",
				Network: "kcp", HeaderType: "wechat-video", Seed: "seed"}), nil},
		{"v2rayN's VMess with another name for mKCP", "vmess://" + b64(`{"v":"2","ps":"mkcp","add":"proxy.example.com",`+
			`"port":"3000","id":"`+uuid+`","net":"mkcp","type":"srtp","path":"the seed"}`),
			proxy(model.KindVMess, "mkcp", "proxy.example.com", 3000, uuid, model.ProxyOptions{Network: "kcp",
				HeaderType: "srtp", Seed: "the seed"}), nil},
		{"v2rayN's VMess over gRPC", "vmess://" + b64(`{"v":"2","ps":"grpc","add":"proxy.example.com","port":"443",`+
			`"id":"`+uuid+`","net":"grpc","type":"multi","host":"a.example.com","path":"svc","tls":"tls"}`),
			proxy(model.KindVMess, "grpc", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "grpc",
				Mode: "multi", Authority: "a.example.com", ServiceName: "svc", Security: "tls"}), nil},
		{"v2rayN's first VMess format", "vmess://" + b64(`{"ps":"v1","add":"proxy.example.com","port":"80",`+
			`"id":"`+uuid+`","net":"ws","host":"h.example.com;/path"}`),
			proxy(model.KindVMess, "v1", "proxy.example.com", 80, uuid, model.ProxyOptions{Network: "ws",
				Host: "h.example.com", Path: "/path"}), nil},
		{"VMess in the standard format", "vmess://" + uuid + "@proxy.example.com:443?encryption=chacha20-poly1305&type=httpupgrade" +
			"&host=h.example.com&path=%2Fu&security=tls#Std",
			proxy(model.KindVMess, "Std", "proxy.example.com", 443, uuid, model.ProxyOptions{Cipher: "chacha20-poly1305",
				Network: "httpupgrade", Host: "h.example.com", Path: "/u", Security: "tls"}), nil},
		{"Shadowsocks, SIP002", "ss://" + strings.TrimRight(base64.URLEncoding.EncodeToString([]byte("aes-256-gcm:pass:word")), "=") +
			"@192.0.2.3:8388/?outline=1#SS",
			proxy(model.KindShadowsocks, "SS", "192.0.2.3", 8388, "pass:word", model.ProxyOptions{Cipher: "aes-256-gcm"}),
			[]sharelink.Note{{Code: sharelink.NoteIgnored, Args: map[string]string{"params": "outline"}}}},
		{"Shadowsocks, the old form", "ss://" + b64("chacha20-ietf-poly1305:p@ss@proxy.example.com:8388") + "#Old",
			proxy(model.KindShadowsocks, "Old", "proxy.example.com", 8388, "p@ss",
				model.ProxyOptions{Cipher: "chacha20-poly1305"}), nil},
		{"Shadowsocks 2022", "ss://2022-blake3-aes-128-gcm:AAAAAAAAAAAAAAAAAAAAAA%3D%3D@proxy.example.com:443#2022",
			proxy(model.KindShadowsocks, "2022", "proxy.example.com", 443, "AAAAAAAAAAAAAAAAAAAAAA==",
				model.ProxyOptions{Cipher: "2022-blake3-aes-128-gcm"}), nil},
		{"Shadowsocks 2022 with its separator escaped too",
			"ss://2022-blake3-aes-128-gcm%3AAAAAAAAAAAAAAAAAAAAAAA%3D%3D@proxy.example.com:443",
			proxy(model.KindShadowsocks, "proxy.example.com", "proxy.example.com", 443, "AAAAAAAAAAAAAAAAAAAAAA==",
				model.ProxyOptions{Cipher: "2022-blake3-aes-128-gcm"}), nil},
		{"Hysteria2", "hysteria2://user:pa%20ss@proxy.example.com:8443/?sni=sni.example.com&obfs=salamander" +
			"&obfs-password=ob&pinSHA256=AB:CD&insecure=1&upmbps=50#Hy2",
			proxy(model.KindHysteria2, "Hy2", "proxy.example.com", 8443, "user:pa ss", model.ProxyOptions{
				SNI: "sni.example.com", ObfsPassword: "ob", PinnedCerts: "abcd"}),
			[]sharelink.Note{{Code: sharelink.NoteInsecure},
				{Code: sharelink.NoteIgnored, Args: map[string]string{"params": "upmbps"}}}},
		{"Hysteria2 with port hopping", "hy2://auth@[2001:db8::5]:443,20000-30000?sni=a.example.com",
			proxy(model.KindHysteria2, "2001:db8::5", "2001:db8::5", 443, "auth", model.ProxyOptions{SNI: "a.example.com"}),
			[]sharelink.Note{{Code: sharelink.NotePorts, Args: map[string]string{"ports": "443,20000-30000", "port": "443"}}}},
		{"Hysteria2 on the default port", "hysteria2://auth@proxy.example.com/",
			proxy(model.KindHysteria2, "proxy.example.com", "proxy.example.com", 443, "auth", model.ProxyOptions{}), nil},
		{"v2rayN's SOCKS", "socks://" + b64("alice:s3cret") + "@127.0.0.1:10808#Local",
			model.Proxy{Kind: model.KindSocks, Name: "Local", Server: "127.0.0.1", Port: 10808, Username: "alice",
				Secret: "s3cret"}.Normalize(), nil},
		{"SOCKS5 with a plain account", "socks5://alice:p%40ss@[::1]:1080",
			model.Proxy{Kind: model.KindSocks, Name: "::1", Server: "::1", Port: 1080, Username: "alice",
				Secret: "p@ss"}.Normalize(), nil},
		{"HTTP without an account", "http://proxy.example.com:8080",
			model.Proxy{Kind: model.KindHTTP, Name: "proxy.example.com", Server: "proxy.example.com",
				Port: 8080}.Normalize(), nil},
		{"unknown parameters", "vless://" + uuid + "@proxy.example.com:443?type=kcp&mtu=1350&tti=20&zz=1&security=none",
			proxy(model.KindVLESS, "proxy.example.com", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "kcp"}),
			[]sharelink.Note{{Code: sharelink.NoteIgnored, Args: map[string]string{"params": "mtu, tti, zz"}}}},
	}
	for _, c := range cases {
		got, notes, err := sharelink.Parse(c.link)
		if err != nil {
			t.Errorf("%s: Parse: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
		if !slices.EqualFunc(notes, c.notes, func(a, b sharelink.Note) bool {
			return a.Code == b.Code && len(a.Args) == len(b.Args) && func() bool {
				for k, v := range a.Args {
					if b.Args[k] != v {
						return false
					}
				}
				return true
			}()
		}) {
			t.Errorf("%s: notes %+v, want %+v", c.name, notes, c.notes)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct {
		link string
		code string
		args map[string]string
	}{
		{"", "link.invalid", nil},
		{"hello", "link.invalid", nil},
		{"vless://" + uuid + "@proxy.example.com?type=tcp", "link.invalid", nil}, // no port
		{"vmess://bm90IGpzb24", "link.invalid", nil},
		{"vmess://" + b64(`{"add":"proxy.example.com","port":"x","id":"`+uuid+`"}`), "link.invalid", nil},
		{"ss://bm8tc2VydmVy", "link.invalid", nil},
		{"tuic://" + uuid + ":pw@proxy.example.com:443", "link.unsupported", map[string]string{"what": "tuic://"}},
		{"https://proxy.example.com:443", "link.unsupported", map[string]string{"what": "https://"}},
		{"vless://" + uuid + "@proxy.example.com:443?type=quic", "link.transportRemoved", map[string]string{"network": "quic"}},
		{"vmess://" + b64(`{"add":"proxy.example.com","port":"443","id":"`+uuid+`","net":"h2"}`), "link.transportRemoved",
			map[string]string{"network": "h2"}},
		{"trojan://pw@proxy.example.com:443?type=http", "link.transportRemoved", map[string]string{"network": "http"}},
		{"vless://" + uuid + "@proxy.example.com:443?security=xtls", "link.unsupported", map[string]string{"what": "security=xtls"}},
		{"ss://" + b64("aes-256-gcm:pw") + "@proxy.example.com:8388/?plugin=obfs-local%3Bobfs%3Dhttp", "link.unsupported",
			map[string]string{"what": "plugin=obfs-local"}},
		{"hysteria2://pw@proxy.example.com:443?obfs=gecko", "link.unsupported", map[string]string{"what": "obfs=gecko"}},
	}
	for _, c := range cases {
		_, _, err := sharelink.Parse(c.link)
		if got := errcode.Of(err); got != c.code {
			t.Errorf("Parse(%q) = %v (%s), want %s", c.link, err, got, c.code)
			continue
		}
		if args := errcode.Args(err); len(args) != len(c.args) || args["what"] != c.args["what"] || args["network"] != c.args["network"] {
			t.Errorf("Parse(%q): args %v, want %v", c.link, args, c.args)
		}
	}
	if _, _, err := sharelink.Parse("hello"); !errors.Is(err, sharelink.ErrInvalid) {
		t.Errorf("errors.Is(ErrInvalid) = false for %v", err)
	}
}

// Every proxy the parser reads is valid, and writing it as a link and
// reading that again gives the same proxy.
func TestRoundTrip(t *testing.T) {
	ss2022 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	proxies := []model.Proxy{
		proxy(model.KindVLESS, "REALITY 节点", "proxy.example.com", 443, uuid, model.ProxyOptions{Flow: "xtls-rprx-vision",
			Security: "reality", SNI: "www.example.com", Fingerprint: "chrome", PublicKey: pbk, ShortID: "6ba8", SpiderX: "/a?b=c"}),
		proxy(model.KindVLESS, "xhttp", "2001:db8::1", 443, uuid, model.ProxyOptions{Network: "xhttp", Host: "h.example.com",
			Path: "/x?ed=2048&a=b#c", Mode: "packet-up", Extra: `{"a":"b&c=d"}`, Security: "tls", ALPN: "h2,http/1.1",
			PinnedCerts: strings.Repeat("ab", 32), VerifyNames: "a.example.com", ECH: "AEX+/=="}),
		proxy(model.KindVLESS, "kcp", "proxy.example.com", 3000, uuid, model.ProxyOptions{Network: "kcp", HeaderType: "dns",
			Host: "d.example.com", Seed: "s+e/e=d"}),
		proxy(model.KindVMess, "ws", "proxy.example.com", 443, uuid, model.ProxyOptions{Cipher: "zero", Network: "ws",
			Host: "h.example.com", Path: "/ws?ed=2048", Security: "tls", SNI: "h.example.com", Fingerprint: "firefox"}),
		proxy(model.KindVMess, "grpc", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "grpc",
			ServiceName: "svc", Authority: "a.example.com", Mode: "multi"}),
		proxy(model.KindVMess, "kcp", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "kcp",
			HeaderType: "srtp", Seed: "seed"}),
		proxy(model.KindVMess, "tcp http", "proxy.example.com", 80, uuid, model.ProxyOptions{HeaderType: "http",
			Host: "a.example.com,b.example.com", Path: "/,/b"}),
		proxy(model.KindVMess, "xhttp", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "xhttp", Path: "/x"}),
		proxy(model.KindVMess, "REALITY", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "grpc",
			Security: "reality", PublicKey: pbk}),
		proxy(model.KindVMess, "pinned", "proxy.example.com", 443, uuid, model.ProxyOptions{Security: "tls",
			PinnedCerts: strings.Repeat("cd", 32)}),
		proxy(model.KindTrojan, "trojan", "proxy.example.com", 443, "p@ss:w/rd #1", model.ProxyOptions{Security: "tls",
			SNI: "sni.example.com"}),
		proxy(model.KindTrojan, "plain trojan", "192.0.2.1", 8080, "pw", model.ProxyOptions{}),
		proxy(model.KindShadowsocks, "ss", "proxy.example.com", 8388, "p@ss:w/rd", model.ProxyOptions{Cipher: "aes-128-gcm"}),
		proxy(model.KindShadowsocks, "ss2022", "proxy.example.com", 8388, ss2022+":"+ss2022,
			model.ProxyOptions{Cipher: "2022-blake3-chacha20-poly1305"}),
		proxy(model.KindHysteria2, "hy2", "proxy.example.com", 443, "user:pass word", model.ProxyOptions{SNI: "sni.example.com",
			ALPN: "h3", ObfsPassword: "o&b=f", PinnedCerts: strings.Repeat("ef", 32), VerifyNames: "a.example.com,b.example.com"}),
		model.Proxy{Kind: model.KindSocks, Name: "socks", Server: "127.0.0.1", Port: 10808, Username: "alice",
			Secret: "s3c:ret"}.Normalize(),
		model.Proxy{Kind: model.KindHTTP, Name: "http", Server: "proxy.example.com", Port: 8080}.Normalize(),
	}
	for _, p := range proxies {
		p.ID = "0123456789abcdef"
		if err := p.Validate(); err != nil {
			t.Errorf("%s %s: not valid: %v", p.Kind, p.Name, err)
		}
		link, err := sharelink.Format(p)
		if err != nil {
			t.Errorf("%s %s: Format: %v", p.Kind, p.Name, err)
			continue
		}
		got, notes, err := sharelink.Parse(link)
		if err != nil {
			t.Errorf("%s %s: Parse(%q): %v", p.Kind, p.Name, link, err)
			continue
		}
		got.ID = p.ID
		if got != p || len(notes) != 0 {
			t.Errorf("%s %s: %s\n got %+v (notes %v)\nwant %+v", p.Kind, p.Name, link, got, notes, p)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		proxy model.Proxy
		want  string
	}{
		{proxy(model.KindVLESS, "Tokyo 1", "proxy.example.com", 443, uuid, model.ProxyOptions{Flow: "xtls-rprx-vision",
			Security: "reality", SNI: "www.example.com", PublicKey: pbk, ShortID: "ab"}),
			"vless://" + uuid + "@proxy.example.com:443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality" +
				"&sni=www.example.com&pbk=" + pbk + "&sid=ab#Tokyo%201"},
		{proxy(model.KindVMess, "v", "proxy.example.com", 443, uuid, model.ProxyOptions{Network: "ws", Path: "/ws"}),
			"vmess://" + b64(`{"v":"2","ps":"v","add":"proxy.example.com","port":"443","id":"`+uuid+`","aid":"0",`+
				`"scy":"auto","net":"ws","type":"","host":"","path":"/ws","tls":"","sni":"","alpn":"","fp":""}`)},
		{proxy(model.KindShadowsocks, "", "192.0.2.1", 8388, "pw", model.ProxyOptions{Cipher: "aes-256-gcm"}),
			"ss://" + strings.TrimRight(base64.URLEncoding.EncodeToString([]byte("aes-256-gcm:pw")), "=") + "@192.0.2.1:8388"},
		{proxy(model.KindHysteria2, "h", "2001:db8::1", 443, "pw", model.ProxyOptions{ObfsPassword: "o"}),
			"hysteria2://pw@[2001:db8::1]:443/?obfs=salamander&obfs-password=o#h"},
	}
	for _, c := range cases {
		if got, err := sharelink.Format(c.proxy); err != nil || got != c.want {
			t.Errorf("Format(%s):\n got %s (%v)\nwant %s", c.proxy.Kind, got, err, c.want)
		}
	}
	if _, err := sharelink.Format(model.Proxy{Kind: model.KindXray, Outbound: `{"protocol":"freedom"}`}); !errors.Is(err, sharelink.ErrNotShareable) {
		t.Errorf("Format(xray) = %v", err)
	}
}
