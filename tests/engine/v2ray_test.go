package engine_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/engine"
	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
	"github.com/liubz102/RDP-over-proxy/tests/testutil/xraytest"
)

const userID = "b831381d-6324-4d53-ad4f-8cda48b30811"

// v2rayCases are servers of every V2Ray-family kind, over each transport,
// with and without TLS or REALITY. Each one is a real Xray server that the
// engine reaches with the outbound it generates.
func v2rayCases() []xraytest.Options {
	key16 := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	return []xraytest.Options{
		{Protocol: model.KindVMess, Secret: userID},
		{Protocol: model.KindVMess, Secret: "a custom id", Options: model.ProxyOptions{Cipher: "chacha20-poly1305"}},
		{Protocol: model.KindVMess, Secret: userID, Options: model.ProxyOptions{Network: "ws", Path: "/ws?ed=2048",
			Host: "cdn.example.com", Security: "tls"}},
		{Protocol: model.KindVMess, Secret: userID, Options: model.ProxyOptions{HeaderType: "http",
			Host: "a.example.com,b.example.com", Path: "/a,/b"}},
		{Protocol: model.KindVMess, Secret: userID, Options: model.ProxyOptions{Network: "kcp"}},
		{Protocol: model.KindVMess, Secret: userID, Options: model.ProxyOptions{Network: "kcp",
			HeaderType: "wechat-video", Seed: "kcp seed"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Flow: "xtls-rprx-vision",
			Security: "reality", Fingerprint: "chrome", ShortID: "6ba85179e30d4fc2"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Flow: "xtls-rprx-vision", Security: "tls"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Network: "xhttp", Path: "/x",
			Security: "tls", ALPN: "h2"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Network: "xhttp", Path: "/x",
			Mode: "packet-up", Security: "reality", ShortID: "ab"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Network: "grpc", ServiceName: "svc",
			Mode: "multi", Security: "tls"}},
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Network: "httpupgrade", Path: "/up"}},
		{Protocol: model.KindTrojan, Secret: "trojan pass", Options: model.ProxyOptions{Security: "tls"}},
		{Protocol: model.KindTrojan, Secret: "trojan pass", Options: model.ProxyOptions{Network: "ws", Path: "/t"}},
		{Protocol: model.KindTrojan, Secret: "trojan pass", Options: model.ProxyOptions{Network: "grpc",
			ServiceName: "svc", Security: "reality"}},
		{Protocol: model.KindShadowsocks, Secret: "ss pass", Options: model.ProxyOptions{Cipher: "aes-256-gcm"}},
		{Protocol: model.KindShadowsocks, Secret: "ss pass", Options: model.ProxyOptions{Cipher: "chacha20-ietf-poly1305"}},
		{Protocol: model.KindShadowsocks, Secret: key16, Options: model.ProxyOptions{Cipher: "2022-blake3-aes-128-gcm"}},
		{Protocol: model.KindHysteria2, Secret: "hy2 auth"},
		{Protocol: model.KindHysteria2, Secret: "user:pass", Options: model.ProxyOptions{ObfsPassword: "salamander pw"}},
	}
}

func describe(o xraytest.Options) string {
	p := model.Proxy{Kind: o.Protocol, Options: o.Options}.Normalize()
	s := o.Protocol
	if model.HasTransport(o.Protocol) {
		s += " " + p.Options.Network + "/" + p.Options.HeaderType + " " + p.Options.Security
	}
	return s + " " + p.Options.Cipher + p.Options.Flow
}

func TestRoutesThroughTheV2RayFamily(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybridEx})
	e := startEngine(t, engine.Options{})
	for i, o := range v2rayCases() {
		px := xraytest.Start(t, o)
		p := px.Model("v"+strconv.Itoa(i), o)
		if err := p.Validate(); err != nil {
			t.Fatalf("%s: the proxy is not valid: %v", describe(o), err)
		}
		if err := e.Check(p); err != nil {
			t.Fatalf("%s: Check: %v", describe(o), err)
		}
		res, err := probe.Check(t.Context(), acquire(t, e, p), srv.Addr)
		if err != nil {
			ob, _ := engine.Outbound(p)
			t.Fatalf("%s: route check: %v\noutbound: %s", describe(o), err, ob)
		}
		if res.Confirm.Selected != probe.ProtocolHybridEx {
			t.Fatalf("%s: confirm %+v", describe(o), res.Confirm)
		}
		if req := <-srv.Requests(); req.Protocols != probe.Requested {
			t.Fatalf("%s: the target received %+v", describe(o), req)
		}
		t.Logf("%s: %v", describe(o), res.Elapsed)
	}
}

func TestLatencyThroughTheV2RayFamily(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(web.Close)
	e := startEngine(t, engine.Options{})
	for i, o := range []xraytest.Options{
		{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Flow: "xtls-rprx-vision", Security: "reality"}},
		{Protocol: model.KindHysteria2, Secret: "hy2 auth"},
	} {
		px := xraytest.Start(t, o)
		took, err := probe.Latency(t.Context(), acquire(t, e, px.Model("lat"+strconv.Itoa(i), o)), web.URL+"/generate_204")
		if err != nil {
			t.Fatalf("%s: Latency: %v", describe(o), err)
		}
		t.Logf("%s: %v", describe(o), took)
	}
}

func TestV2RayFailuresCarryAReason(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	e := startEngine(t, engine.Options{})

	vless := xraytest.Options{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Security: "tls"}}
	vlessProxy := xraytest.Start(t, vless)
	wrongPin := vlessProxy.Model("pin", vless)
	wrongPin.Options.PinnedCerts = strings.Repeat("ab", 32)
	noPin := vlessProxy.Model("nopin", vless)
	noPin.Options.PinnedCerts = ""

	reality := xraytest.Options{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Security: "reality"}}
	realityProxy := xraytest.Start(t, reality)
	wrongKey := realityProxy.Model("key", reality)
	wrongKey.Options.PublicKey = "SbVKOEMjK0sIlbwg4akyBg5mL5KZwwB-ed4eEE7YnRc"

	trojan := xraytest.Options{Protocol: model.KindTrojan, Secret: "right", Options: model.ProxyOptions{Security: "tls"}}
	trojanProxy := xraytest.Start(t, trojan)
	wrongPassword := trojanProxy.Model("trojan", trojan)
	wrongPassword.Secret = "wrong"

	vmess := xraytest.Options{Protocol: model.KindVMess, Secret: userID}
	vmessProxy := xraytest.Start(t, vmess)
	wrongID := vmessProxy.Model("vmess", vmess)
	wrongID.Secret = "00000000-0000-4000-8000-000000000000"

	hy2 := xraytest.Options{Protocol: model.KindHysteria2, Secret: "right"}
	hy2Proxy := xraytest.Start(t, hy2)
	wrongAuth := hy2Proxy.Model("hy2", hy2)
	wrongAuth.Secret = "wrong"

	cases := []struct {
		name  string
		proxy model.Proxy
		code  string
	}{
		{"a certificate other than the pinned one", wrongPin, engine.CodeProxyTLS},
		{"a self-signed certificate without a pin", noPin, engine.CodeProxyTLS},
		{"a REALITY server with another key", wrongKey, engine.CodeProxyTLS},
		{"a wrong Hysteria2 password", wrongAuth, engine.CodeProxyAuth},
		// VMess and Trojan servers say nothing to a client they do not know,
		// so as not to give themselves away: the connection just ends.
		{"a wrong Trojan password", wrongPassword, errcode.Of(probe.ErrNoAnswer)},
		{"a wrong VMess user ID", wrongID, errcode.Of(probe.ErrNoAnswer)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := probe.Check(t.Context(), acquire(t, e, c.proxy), srv.Addr)
			if err == nil {
				t.Fatal("the route check passed")
			}
			if got := errcode.Of(err); got != c.code {
				t.Errorf("code %q, want %q (%v)", got, c.code, err)
			}
		})
	}
}

func TestCustomOutbound(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	o := xraytest.Options{Protocol: model.KindVLESS, Secret: userID, Options: model.ProxyOptions{Network: "ws",
		Path: "/ws", Security: "tls"}}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})
	// What a user pastes: a complete outbound, with a tag of its own and
	// numbers written as numbers.
	custom := `{"tag": "my-proxy", "protocol": "vless", "settings": {"vnext": [{"address": "127.0.0.1", "port": ` +
		strconv.Itoa(px.Port) + `, "users": [{"id": "` + userID + `", "encryption": "none"}]}]}, "streamSettings": ` +
		`{"network": "ws", "wsSettings": {"path": "/ws"}, "security": "tls", "tlsSettings": {"serverName": "` +
		xraytest.ServerName + `", "pinnedPeerCertSha256": "` + px.CertHash + `"}}}`
	p := model.Proxy{ID: "custom", Name: "Custom", Kind: model.KindXray, Outbound: custom}.Normalize()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if network, security := p.Transport(); network != "ws" || security != "tls" {
		t.Fatalf("Transport() = %q, %q", network, security)
	}
	if err := e.Check(p); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if _, err := probe.Check(t.Context(), acquire(t, e, p), srv.Addr); err != nil {
		t.Fatalf("route check through the custom outbound: %v", err)
	}
	ob, err := engine.Outbound(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ob, "my-proxy") {
		t.Fatalf("the user's tag was kept: %s", ob)
	}
}

func TestCheckReportsWhatXrayRejects(t *testing.T) {
	e := startEngine(t, engine.Options{})
	// An ML-KEM-768 key of the right length that is no key: its numbers are
	// out of range. Only building the outbound's handler finds that.
	badKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 1184))
	node := func(kind, secret string, o model.ProxyOptions) model.Proxy {
		return model.Proxy{ID: "check", Name: "Check", Kind: kind, Server: "192.0.2.1", Port: 443, Secret: secret, Options: o}
	}
	custom := func(outbound string) model.Proxy {
		return model.Proxy{ID: "check", Name: "Check", Kind: model.KindXray, Outbound: outbound}
	}
	cases := []struct {
		name  string
		proxy model.Proxy
	}{
		{"an unknown fingerprint", node(model.KindTrojan, "pw", model.ProxyOptions{Security: "tls", Fingerprint: "netscape"})},
		{"broken VLESS Encryption", node(model.KindVLESS, userID,
			model.ProxyOptions{Encryption: "mlkem768x25519plus.native.0rtt.abc"})},
		{"a VLESS Encryption key that is no key", node(model.KindVLESS, userID,
			model.ProxyOptions{Encryption: "mlkem768x25519plus.native.1rtt." + badKey})},
		{"a custom outbound Xray does not know", custom(`{"protocol": "carrier-pigeon"}`)},
		{"a custom outbound with a broken transport", custom(`{"protocol": "freedom", "streamSettings": {"network": "quic"}}`)},
		{"XHTTP extra settings Xray rejects", node(model.KindVLESS, userID,
			model.ProxyOptions{Network: "xhttp", Extra: `{"headers": {"Host": "x"}}`})},
	}
	for _, c := range cases {
		err := e.Check(c.proxy)
		if !errors.Is(err, engine.ErrConfig) {
			t.Errorf("%s: Check = %v, want ErrConfig", c.name, err)
			continue
		}
		t.Logf("%s: %v", c.name, err)
	}
	if err := e.Check(model.DirectProxy()); err != nil {
		t.Errorf("Check(direct) = %v", err)
	}
	if got := e.Outbounds(); len(got) != 0 {
		t.Errorf("checking left outbounds %q", got)
	}
}

// What validation refuses never reaches Xray, even in a proxy that was
// stored before (or by hand): Xray would take this fragment length and crash
// on it when connecting.
func TestInvalidProxiesNeverReachXray(t *testing.T) {
	e := startEngine(t, engine.Options{})
	p := model.Proxy{ID: "frag", Name: "Fragments", Kind: model.KindVLESS, Server: "192.0.2.1", Port: 443, Secret: userID,
		Options: model.ProxyOptions{FinalMask: `{"tcp":[{"type":"fragment","settings":{"packets":"1-3","length":"-5--5"}}]}`}}
	if _, _, err := e.Acquire(p); !errors.Is(err, engine.ErrConfig) {
		t.Fatalf("Acquire = %v, want ErrConfig", err)
	}
	if err := e.Check(p); !errors.Is(err, engine.ErrConfig) {
		t.Fatalf("Check = %v, want ErrConfig", err)
	}
	if got := e.Outbounds(); len(got) != 0 {
		t.Fatalf("outbounds %q", got)
	}
}

// The outbounds the engine writes for the transports that need translating
// into Xray's current settings: mKCP's disguise and seed became masks, and
// Hysteria2 needs QUIC's ALPN and a keep-alive.
func TestOutboundTranslations(t *testing.T) {
	kcp := model.Proxy{ID: "kcp", Name: "mKCP", Kind: model.KindVMess, Server: "192.0.2.1", Port: 443, Secret: userID,
		Options: model.ProxyOptions{Network: "kcp", HeaderType: "dns", Host: "d.example.com", Seed: "s"}}
	var ob struct {
		StreamSettings struct {
			Network   string `json:"network"`
			FinalMask struct {
				UDP []struct {
					Type     string            `json:"type"`
					Settings map[string]string `json:"settings"`
				} `json:"udp"`
				QuicParams map[string]int `json:"quicParams"`
			} `json:"finalmask"`
			TLS struct {
				ALPN []string `json:"alpn"`
			} `json:"tlsSettings"`
		} `json:"streamSettings"`
	}
	text, err := engine.Outbound(kcp)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(text), &ob); err != nil {
		t.Fatal(err)
	}
	masks := ob.StreamSettings.FinalMask.UDP
	if len(masks) != 2 || masks[0].Type != "header-dns" || masks[0].Settings["domain"] != "d.example.com" ||
		masks[1].Type != "mkcp-aes128gcm" || masks[1].Settings["password"] != "s" {
		t.Fatalf("mKCP masks: %s", text)
	}

	hy2 := model.Proxy{ID: "hy2", Name: "Hysteria2", Kind: model.KindHysteria2, Server: "192.0.2.1", Port: 443, Secret: "pw"}
	if text, err = engine.Outbound(hy2); err != nil {
		t.Fatal(err)
	}
	ob.StreamSettings.FinalMask.UDP = nil
	if err := json.Unmarshal([]byte(text), &ob); err != nil {
		t.Fatal(err)
	}
	s := ob.StreamSettings
	if s.Network != "hysteria" || len(s.TLS.ALPN) != 1 || s.TLS.ALPN[0] != "h3" ||
		s.FinalMask.QuicParams["keepAlivePeriod"] == 0 || len(s.FinalMask.UDP) != 0 {
		t.Fatalf("Hysteria2: %s", text)
	}
}
