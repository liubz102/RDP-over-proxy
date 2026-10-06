package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/sharelink"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

const (
	userID   = "b831381d-6324-4d53-ad4f-8cda48b30811"
	vlessURL = "vless://" + userID + "@proxy.example.com:443?type=ws&path=%2Fsecret-path&host=cdn.example.com" +
		"&security=tls&sni=cdn.example.com&allowInsecure=1#Tokyo"
)

// errorView is what the frontend gets for err.
func errorView(t *testing.T, err error) ErrorView {
	t.Helper()
	var v ErrorView
	if jerr := json.Unmarshal(MarshalError(err), &v); jerr != nil {
		t.Fatal(jerr)
	}
	return v
}

func TestShareLinks(t *testing.T) {
	h := newHarness(t)
	link, err := h.proxies.ParseLink(vlessURL)
	if err != nil {
		t.Fatal(err)
	}
	p := link.Proxy
	if p.Kind != model.KindVLESS || p.Name != "Tokyo" || p.Secret != userID || p.Options.Path != "/secret-path" ||
		len(link.Notes) != 1 || link.Notes[0].Code != sharelink.NoteInsecure {
		t.Fatalf("ParseLink = %+v", link)
	}
	if _, err := h.proxies.ParseLink("tuic://x@proxy.example.com:443"); errorView(t, err).Args["what"] != "tuic://" {
		t.Fatalf("an unsupported link: %+v", errorView(t, err))
	}
	if link, _ := h.proxies.ParseLink("socks://127.0.0.1:10808"); link.Notes == nil {
		t.Fatal("no notes should be an empty list, not null")
	}

	// Saved, the proxy lists its transport but none of its settings.
	v, err := h.proxies.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	list := h.proxies.List()
	got := list[len(list)-1]
	if got.Network != "ws" || got.Security != "tls" || !got.HasSecret || got.Proxy.Options != (model.ProxyOptions{}) {
		t.Fatalf("listed as %+v", got)
	}
	// The editor gets the settings, but not the user ID.
	edit, err := h.proxies.Get(v.Proxy.ID)
	if err != nil || edit.Secret != "" || edit.Options.Path != "/secret-path" {
		t.Fatalf("Get = %+v, %v", edit, err)
	}

	// The link it gives back carries the user ID again.
	out, err := h.proxies.ShareLink(v.Proxy.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.proxies.ParseLink(out)
	if err != nil || again.Proxy.Secret != userID || again.Proxy.Options != p.Options {
		t.Fatalf("ShareLink gave %s: %+v, %v", out, again, err)
	}
	if _, err := h.proxies.ShareLink(model.DirectProxyID); !errors.Is(err, store.ErrBuiltIn) {
		t.Fatalf("ShareLink(direct) = %v", err)
	}
	custom, err := h.proxies.Create(model.Proxy{Name: "Custom", Kind: model.KindXray, Outbound: `{"protocol":"freedom"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.proxies.ShareLink(custom.Proxy.ID); errcode.Of(err) != "link.notShareable" {
		t.Fatalf("ShareLink(custom) = %v", err)
	}
}

func TestXrayChecksBeforeSaving(t *testing.T) {
	h := newHarness(t)
	rejected := errcode.Wrap("proxy.config", errors.New(`unknown "fingerprint": netscape`))
	var checked []model.Proxy
	h.check = func(p model.Proxy) error {
		checked = append(checked, p)
		if p.Options.Fingerprint == "netscape" {
			return rejected
		}
		return nil
	}
	p := model.Proxy{Name: "Node", Kind: model.KindTrojan, Server: "proxy.example.com", Port: 443, Secret: "pw",
		Options: model.ProxyOptions{Security: "tls", Fingerprint: "netscape"}}
	if _, err := h.proxies.Create(p); errorView(t, err).Code != "proxy.config" {
		t.Fatalf("Create = %v", err)
	}
	if len(h.proxies.List()) != 2 { // the built-in entries
		t.Fatal("a proxy Xray rejected was saved")
	}
	// Xray only sees settings that pass validation, normalized.
	bad := p
	bad.Port = 0
	if _, err := h.proxies.Create(bad); errorView(t, err).Code != CodeValidation || len(checked) != 1 {
		t.Fatalf("Create with a missing port = %v, checked %d", err, len(checked))
	}
	if checked[0].Options.Network != model.NetworkTCP {
		t.Fatalf("Xray checked settings that were not normalized: %+v", checked[0].Options)
	}

	p.Options.Fingerprint = "chrome"
	v, err := h.proxies.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if p, err = h.proxies.Get(v.Proxy.ID); err != nil {
		t.Fatal(err)
	}
	p.Options.Fingerprint = "netscape"
	if _, err := h.proxies.Update(p, true); errorView(t, err).Code != "proxy.config" {
		t.Fatalf("Update = %v", err)
	}
	if stored, _ := h.data.Proxy(p.ID); stored.Options.Fingerprint != "chrome" {
		t.Fatalf("the rejected settings were stored: %+v", stored.Options)
	}
}

func TestStoredSecretsStayWithTheirKind(t *testing.T) {
	h := newHarness(t)
	v, err := h.proxies.Create(model.Proxy{Name: "Node", Kind: model.KindVMess, Server: "proxy.example.com", Port: 443,
		Secret: userID})
	if err != nil {
		t.Fatal(err)
	}
	p := v.Proxy
	p.Name = "Renamed"
	if _, err := h.proxies.Update(p, true); err != nil {
		t.Fatalf("keeping the user ID: %v", err)
	}
	if stored, _ := h.data.Proxy(p.ID); stored.Secret != userID {
		t.Fatalf("the user ID was lost: %+v", stored)
	}
	// A VMess user ID is no Trojan password.
	p.Kind = model.KindTrojan
	view := errorView(t, func() error { _, err := h.proxies.Update(p, true); return err }())
	if view.Code != CodeValidation || len(view.Fields) != 1 || view.Fields[0] != (model.FieldError{Field: "secret", Code: model.CodeRequired}) {
		t.Fatalf("changing the kind and keeping the secret: %+v", view)
	}

	// A SOCKS5 account is an HTTP account all the same.
	px := h.proxy(t, "Account")
	edited, err := h.proxies.Get(px.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited.Kind = model.KindHTTP
	if _, err := h.proxies.Update(edited, true); err != nil {
		t.Fatalf("SOCKS5 to HTTP: %v", err)
	}
	if stored, _ := h.data.Proxy(px.ID); stored.Secret != "proxy secret" || stored.Username != "proxyuser" {
		t.Fatalf("the account was lost: %+v", stored)
	}
}

func TestProxiesWhoseSecretsAreLost(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, store.ProxiesDir, "0123456789abcdef.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	// Sealed by another Windows user: the test sealer cannot open it.
	text := `{"schema":1,"name":"Node","kind":"vless","server":"proxy.example.com","port":443,` +
		`"secret":"dpapi:AQAAAB","options":"dpapi:AQAAAN"}`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	h, problems := newHarnessIn(t, dir)
	if len(problems) == 0 || problems[0].Code != store.ProblemSecretLost {
		t.Fatalf("problems = %v", problems)
	}
	list := h.proxies.List()
	if v := list[len(list)-1]; !v.SecretsLost || v.HasSecret {
		t.Fatalf("listed as %+v", v)
	}
	pc := h.profile(t, "PC", "0123456789abcdef", "192.0.2.20:3389", "")
	if _, err := h.sessions.Connect(pc.ID, ""); errcode.Of(err) != "proxy.secretsLost" {
		t.Fatalf("Connect = %v", err)
	}
	if _, err := h.proxies.Latency(t.Context(), "0123456789abcdef"); errcode.Of(err) != "proxy.secretsLost" {
		t.Fatalf("Latency = %v", err)
	}
	if h.routes.acquired.Load() != 0 {
		t.Fatal("a proxy without its secrets was used")
	}
	// Entered again, it is whole.
	p, err := h.proxies.Get("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	p.Secret = userID
	p.Options.Security = model.SecurityTLS
	if _, err := h.proxies.Update(p, false); err != nil {
		t.Fatal(err)
	}
	if list := h.proxies.List(); list[len(list)-1].SecretsLost {
		t.Fatal("still lost after saving")
	}
}

func TestDraftLatency(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(web.Close)
	h := newHarness(t)
	st := h.settings.Get()
	st.TestURL = web.URL + "/generate_204"
	if err := h.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	var tested []model.Proxy
	h.check = func(p model.Proxy) error { tested = append(tested, p); return nil }

	draft := model.Proxy{Name: "New", Kind: model.KindVLESS, Server: "proxy.example.com", Port: 443, Secret: userID}
	if _, err := h.proxies.DraftLatency(t.Context(), draft, false); err != nil {
		t.Fatalf("DraftLatency: %v", err)
	}
	if h.routes.acquired.Load() != 1 || h.routes.released.Load() != 1 || len(tested) != 1 {
		t.Fatal("the draft's route was not checked, or not given back")
	}
	if len(h.proxies.List()) != 2 { // the built-in entries
		t.Fatal("a draft was saved")
	}
	// Problems show as they would when saving.
	draft.Secret = ""
	if _, err := h.proxies.DraftLatency(t.Context(), draft, false); errorView(t, err).Code != CodeValidation {
		t.Fatalf("a draft without a user ID: %v", err)
	}
	// Editing a saved proxy, the stored secret stands in for the empty field.
	v, err := h.proxies.Create(model.Proxy{Name: "Saved", Kind: model.KindVLESS, Server: "proxy.example.com",
		Port: 443, Secret: userID})
	if err != nil {
		t.Fatal(err)
	}
	edited := v.Proxy
	edited.Port = 8443
	if _, err := h.proxies.DraftLatency(t.Context(), edited, true); err != nil {
		t.Fatalf("DraftLatency keeping the secret: %v", err)
	}
	if last := tested[len(tested)-1]; last.Secret != userID || last.Port != 8443 {
		t.Fatalf("tested %+v", last)
	}
}

func TestProxySettingsAreMaskedInTheLogFile(t *testing.T) {
	h := newHarness(t)
	link, err := h.proxies.ParseLink(vlessURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.proxies.Create(link.Proxy); err != nil {
		t.Fatal(err)
	}
	custom := `{"protocol":"vless","settings":{"vnext":[{"address":"custom.example.com","port":443,` +
		`"users":[{"id":"0b0d4ab5-5bd1-4c04-a7f5-3e3a5d0e44a1"}]}]},"streamSettings":{"network":"xhttp",` +
		`"xhttpSettings":{"headers":{"X-Token":"token-in-a-header"}}}}`
	if _, err := h.proxies.Create(model.Proxy{Name: "Mine", Kind: model.KindXray, Outbound: custom}); err != nil {
		t.Fatal(err)
	}
	// Masks hold passwords; common words and this computer say nothing.
	if _, err := h.proxies.Create(model.Proxy{Name: "kcp node", Kind: model.KindVMess, Server: "127.0.0.1", Port: 3000,
		Secret: userID, Options: model.ProxyOptions{Network: "kcp",
			FinalMask: `{"udp":[{"type":"mkcp-aes128gcm","settings":{"password":"mask-password"}}]}`}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.proxies.Create(model.Proxy{Name: "grpc", Kind: model.KindTrojan, Server: "proxy.example.com", Port: 443,
		Secret: "pw-long", Options: model.ProxyOptions{Network: "grpc", ServiceName: "grpc"}}); err != nil {
		t.Fatal(err)
	}
	line := "dial cdn.example.com/secret-path as " + userID + " via custom.example.com, id " +
		"0b0d4ab5-5bd1-4c04-a7f5-3e3a5d0e44a1, token-in-a-header, mask-password over tcp; gRPC transport via 127.0.0.1"
	masked := h.log.Redactor().Text(line)
	for _, secret := range []string{"cdn.example.com", "secret-path", userID, "custom.example.com", "0b0d4ab5",
		"token-in-a-header", "mask-password"} {
		if strings.Contains(masked, secret) {
			t.Errorf("%q is in the log: %s", secret, masked)
		}
	}
	for _, kept := range []string{" over tcp", "gRPC transport", "127.0.0.1"} {
		if !strings.Contains(masked, kept) {
			t.Errorf("%q was masked: %s", kept, masked)
		}
	}
}
