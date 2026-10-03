package store_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/store"

	"encoding/base64"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/loopback"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// fakeSealer stands in for DPAPI: "sealed:" plus base64, so the plain text
// never appears in a file.
type fakeSealer struct{}

func (fakeSealer) Seal(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return "sealed:" + base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func (fakeSealer) Open(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	text, ok := strings.CutPrefix(s, "sealed:")
	if !ok {
		return "", errors.New("not sealed by this user")
	}
	b, err := base64.StdEncoding.DecodeString(text)
	return string(b), err
}

func open(t *testing.T, dir string) (*Data, []Problem) {
	t.Helper()
	return OpenData(dir, fakeSealer{})
}

func socks(name string) model.Proxy {
	return model.Proxy{Name: name, Kind: model.KindSocks, Server: "192.0.2.10", Port: 1080, Username: "alice", Secret: "s3cret pass"}
}

func profile(name, proxyID string) model.Profile {
	p := model.DefaultProfile()
	p.Name = name
	p.Target = model.Target{Host: "pc.example.com", Port: 3389}
	p.ProxyID = proxyID
	p.Username = `EXAMPLE\alice`
	return p
}

func TestEmptyFolder(t *testing.T) {
	d, problems := open(t, t.TempDir())
	if len(problems) != 0 || len(d.Proxies()) != 0 || len(d.Profiles()) != 0 {
		t.Fatalf("problems %v, proxies %v, profiles %v", problems, d.Proxies(), d.Profiles())
	}
	if p, ok := d.Proxy(model.DirectProxyID); !ok || p.Kind != model.KindDirect {
		t.Fatalf("the direct entry is missing: %+v", p)
	}
}

func TestProxyRoundTripSealsSecrets(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	created, err := d.CreateProxy(socks(" Office "))
	if err != nil {
		t.Fatalf("CreateProxy: %v", err)
	}
	if !model.ValidID(created.ID) || created.Name != "Office" || created.Secret != "s3cret pass" {
		t.Fatalf("created %+v", created)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProxiesDir, created.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") || !strings.Contains(string(raw), `"secret": "sealed:`) {
		t.Fatalf("the file shows the secret:\n%s", raw)
	}

	again, problems := open(t, dir)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	got, ok := again.Proxy(created.ID)
	if !ok || got != created {
		t.Fatalf("reloaded %+v, want %+v", got, created)
	}
}

func TestV2RayOptionsAreSealed(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	created, err := d.CreateProxy(model.Proxy{Name: "Node", Kind: model.KindVLESS, Server: "proxy.example.com", Port: 443,
		Secret:  "b831381d-6324-4d53-ad4f-8cda48b30811",
		Options: model.ProxyOptions{Network: "ws", Path: "/secret-path", Security: "tls", SNI: "cdn.example.com"}})
	if err != nil {
		t.Fatalf("CreateProxy: %v", err)
	}
	if created.Options.Encryption != "none" || created.Options.Path != "/secret-path" {
		t.Fatalf("created %+v", created.Options)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProxiesDir, created.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"secret-path", "cdn.example.com", "b831381d"} {
		if strings.Contains(string(raw), plain) {
			t.Fatalf("the file shows %q:\n%s", plain, raw)
		}
	}
	if !strings.Contains(string(raw), `"options": "sealed:`) {
		t.Fatalf("the options are not sealed:\n%s", raw)
	}
	again, problems := open(t, dir)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if got, _ := again.Proxy(created.ID); got != created {
		t.Fatalf("reloaded %+v, want %+v", got, created)
	}

	// SOCKS has no options and writes none.
	s, err := d.CreateProxy(socks("Local"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, ProxiesDir, s.ID+".json"))
	if !strings.Contains(string(raw), `"options": ""`) {
		t.Fatalf("SOCKS options:\n%s", raw)
	}
}

func TestSecretsOfAnotherUserAreLost(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ProxiesDir, "0123456789abcdef.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	// Sealed by someone else (DPAPI of another user or computer): fakeSealer
	// can open neither.
	text := `{"schema":1,"name":"Node","kind":"trojan","server":"proxy.example.com","port":443,` +
		`"secret":"dpapi:AQAAAB","options":"dpapi:AQAAAN"}`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	d, problems := open(t, dir)
	if len(problems) != 2 || problems[0].Code != ProblemSecretLost || problems[1].Code != ProblemInvalid {
		t.Fatalf("problems = %v", problems)
	}
	p, ok := d.Proxy("0123456789abcdef")
	if !ok || p.Secret != "" || p.Options.Security != model.SecurityTLS {
		t.Fatalf("loaded %+v", p)
	}
	if !d.SecretsLost(p.ID) {
		t.Fatal("the proxy is not marked as having lost its secrets")
	}
	// Saved again with what the user entered, it is whole.
	p.Secret = "pw"
	if _, err := d.UpdateProxy(p); err != nil {
		t.Fatal(err)
	}
	if d.SecretsLost(p.ID) {
		t.Fatal("still marked after saving")
	}
}

func TestProxyValidationAndBuiltIn(t *testing.T) {
	d, _ := open(t, t.TempDir())
	bad := socks("")
	bad.Server = "not a host"
	_, err := d.CreateProxy(bad)
	var fields model.FieldErrors
	if !errors.As(err, &fields) || !fields.Has("name", model.CodeRequired) || !fields.Has("server", model.CodeInvalid) {
		t.Fatalf("CreateProxy(bad) = %v", err)
	}
	direct := model.DirectProxy()
	direct.Name = "Mine"
	if _, err := d.CreateProxy(direct); !errors.As(err, &fields) || !fields.Has("kind", model.CodeUnsupported) {
		t.Fatalf("creating a direct proxy = %v", err)
	}
	if _, err := d.UpdateProxy(model.DirectProxy()); !errors.Is(err, ErrBuiltIn) {
		t.Fatalf("updating the direct entry = %v", err)
	}
	if _, err := d.DeleteProxy(model.DirectProxyID, nil); !errors.Is(err, ErrBuiltIn) {
		t.Fatalf("deleting the direct entry = %v", err)
	}
	if _, err := d.UpdateProxy(socks("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a proxy that does not exist = %v", err)
	}
	if len(d.Proxies()) != 0 {
		t.Fatal("a rejected proxy was stored")
	}
}

func TestDeletingAProxyMovesItsUsersToDirect(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	px, _ := d.CreateProxy(socks("Office"))
	other, _ := d.CreateProxy(socks("Home"))
	one, _ := d.CreateProfile(profile("PC one", px.ID))
	two, _ := d.CreateProfile(profile("PC two", px.ID))
	elsewhere, err := d.CreateProfile(profile("PC three", other.ID))
	if err != nil {
		t.Fatal(err)
	}

	// A user the caller did not name keeps everything as it was.
	moved, err := d.DeleteProxy(px.ID, []string{one.ID})
	var inUse *InUseError
	if !errors.As(err, &inUse) || !slices.Equal(inUse.Profiles, []string{two.ID}) || errcode.Of(err) != "proxy.inUse" || moved != nil {
		t.Fatalf("DeleteProxy naming one of two users = %v, %v", moved, err)
	}
	if p, _ := d.Profile(one.ID); p.ProxyID != px.ID {
		t.Fatalf("a refused delete moved %+v", p)
	}
	if _, ok := d.Proxy(px.ID); !ok {
		t.Fatal("a refused delete removed the proxy")
	}

	moved, err = d.DeleteProxy(px.ID, []string{two.ID, one.ID, elsewhere.ID})
	if want := slices.Sorted(slices.Values([]string{one.ID, two.ID})); err != nil || !slices.Equal(moved, want) {
		t.Fatalf("DeleteProxy = %v, %v; want %v moved", moved, err, want)
	}
	for _, reopened := range []bool{false, true} {
		if reopened {
			d, _ = open(t, dir)
		}
		if _, ok := d.Proxy(px.ID); ok {
			t.Fatal("the proxy is still there")
		}
		for _, id := range []string{one.ID, two.ID} {
			if p, _ := d.Profile(id); p.ProxyID != model.DirectProxyID {
				t.Fatalf("reopened %v: user %+v did not move to direct", reopened, p)
			}
		}
		if p, _ := d.Profile(elsewhere.ID); p.ProxyID != other.ID {
			t.Fatalf("reopened %v: a profile of another proxy moved: %+v", reopened, p)
		}
	}
	if _, err := d.DeleteProxy(px.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting it again = %v", err)
	}
	if moved, err := d.DeleteProxy(other.ID, []string{elsewhere.ID}); err != nil || len(moved) != 1 {
		t.Fatalf("DeleteProxy of the other = %v, %v", moved, err)
	}
}

func TestProfileProxyChangesOnlyBySetProfileProxy(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	office, _ := d.CreateProxy(socks("Office"))
	home, _ := d.CreateProxy(socks("Home"))
	p, err := d.CreateProfile(profile("PC", office.ID))
	if err != nil {
		t.Fatal(err)
	}

	// An editor that sends another proxy does not change it.
	edit := p
	edit.Name, edit.ProxyID = "Renamed", home.ID
	if stored, _, err := d.UpdateProfile(edit); err != nil || stored.ProxyID != office.ID || stored.Name != "Renamed" {
		t.Fatalf("UpdateProfile = %+v, %v; want the proxy kept", stored, err)
	}

	for _, id := range []string{home.ID, model.DirectProxyID} {
		stored, err := d.SetProfileProxy(p.ID, id)
		if err != nil || stored.ProxyID != id || stored.Name != "Renamed" {
			t.Fatalf("SetProfileProxy(%s) = %+v, %v", id, stored, err)
		}
		again, _ := open(t, dir)
		if got, _ := again.Profile(p.ID); got != stored {
			t.Fatalf("reloaded %+v, want %+v", got, stored)
		}
	}
	if _, err := d.SetProfileProxy(p.ID, "nosuchproxy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetProfileProxy to a missing proxy = %v", err)
	}
	if _, err := d.SetProfileProxy("nosuchprofile", model.DirectProxyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetProfileProxy of a missing profile = %v", err)
	}
}

func TestProfileKeepsAProxyThatIsGone(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	px, _ := d.CreateProxy(socks("Office"))
	p, err := d.CreateProfile(profile("PC", px.ID))
	if err != nil {
		t.Fatal(err)
	}
	// The proxy's file goes, as if it could not be loaded.
	if err := os.Remove(filepath.Join(dir, ProxiesDir, px.ID+".json")); err != nil {
		t.Fatal(err)
	}
	d, _ = open(t, dir)
	p.Name = "Renamed"
	if stored, _, err := d.UpdateProfile(p); err != nil || stored.ProxyID != px.ID {
		t.Fatalf("editing a profile whose proxy is gone = %+v, %v", stored, err)
	}
	// Choosing it is another matter.
	q := profile("PC two", px.ID)
	var fields model.FieldErrors
	if _, err := d.CreateProfile(q); !errors.As(err, &fields) || !fields.Has("proxyId", model.CodeInvalid) {
		t.Fatalf("creating a profile with a missing proxy = %v", err)
	}
}

func TestProfileGetsALoopbackAddressOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	in := profile("PC", model.DirectProxyID)
	in.ID, in.Loopback = "chosenbycaller", "127.0.0.1" // both ignored
	p, err := d.CreateProfile(in)
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if p.ID == "chosenbycaller" || p.Loopback != loopback.Derive(p.ID).String() {
		t.Fatalf("created %+v; want a new ID and the address it derives to", p)
	}

	// Editing never moves the address.
	edit := p
	edit.Name = "Renamed"
	edit.Loopback = "127.9.9.9"
	stored, previous, err := d.UpdateProfile(edit)
	if err != nil || stored.Loopback != p.Loopback || stored.Name != "Renamed" || previous != p {
		t.Fatalf("UpdateProfile = %+v (previous %+v), %v", stored, previous, err)
	}
	again, problems := open(t, dir)
	if got, _ := again.Profile(p.ID); len(problems) != 0 || got != stored {
		t.Fatalf("reloaded %+v (problems %v), want %+v", got, problems, stored)
	}
}

func TestProfileValidation(t *testing.T) {
	d, _ := open(t, t.TempDir())
	bad := profile("", "nosuchproxy")
	bad.Target.Host = ""
	_, err := d.CreateProfile(bad)
	var fields model.FieldErrors
	if !errors.As(err, &fields) || !fields.Has("name", model.CodeRequired) ||
		!fields.Has("target.host", model.CodeRequired) || !fields.Has("proxyId", model.CodeInvalid) {
		t.Fatalf("CreateProfile(bad) = %v", err)
	}
	if len(d.Profiles()) != 0 {
		t.Fatal("a rejected profile was stored")
	}
	if _, _, err := d.UpdateProfile(profile("x", model.DirectProxyID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a profile that does not exist = %v", err)
	}
	if _, err := d.DeleteProfile("nosuchprofile"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a profile that does not exist = %v", err)
	}
}

func TestSorting(t *testing.T) {
	d, _ := open(t, t.TempDir())
	for _, n := range []string{"b", "A", "c"} {
		if _, err := d.CreateProxy(socks(n)); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, p := range d.Proxies() {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"A", "b", "c"}) {
		t.Fatalf("proxies by name: %q", names)
	}
	for _, gn := range [][2]string{{"work", "z"}, {"", "y"}, {"Home", "x"}, {"work", "a"}} {
		p := profile(gn[1], model.DirectProxyID)
		p.Group = gn[0]
		if _, err := d.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	names = nil
	for _, p := range d.Profiles() {
		names = append(names, p.Group+"/"+p.Name)
	}
	if !slices.Equal(names, []string{"/y", "Home/x", "work/a", "work/z"}) {
		t.Fatalf("profiles by group and name: %q", names)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func problemCodes(problems []Problem) map[string]string {
	out := map[string]string{}
	for _, p := range problems {
		out[filepath.ToSlash(p.File)] = p.Code
	}
	return out
}

func TestLoadingDamagedAndForeignFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "proxies/aaaa.json", "{broken")
	write(t, dir, "proxies/bbbb.json", `{"schema":99,"name":"From the future"}`)
	write(t, dir, "proxies/cccc.json", `{"schema":1,"name":"Copied","kind":"socks","server":"192.0.2.1","port":1080,"username":"u","secret":"dpapi:from another computer"}`)
	write(t, dir, "proxies/dddd.json", `{"schema":1,"id":"other","name":"Fine","kind":"http","server":"192.0.2.2","port":8080}`)
	write(t, dir, "proxies/direct.json", `{"schema":1,"kind":"direct"}`)
	write(t, dir, "proxies/Not An ID.json", `{}`)
	write(t, dir, "proxies/eeee.json.123.tmp", `{}`)
	write(t, dir, "profiles/ffff.json", `{"schema":1,"name":"Old","target":{"host":"pc.example.com"},"proxyId":"direct","loopback":"127.1.1.1"}`)

	d, problems := open(t, dir)
	want := map[string]string{
		"proxies/aaaa.json": ProblemUnreadable,
		"proxies/bbbb.json": ProblemNewer,
		"proxies/cccc.json": ProblemSecretLost,
	}
	if got := problemCodes(problems); !mapsEqual(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "proxies/aaaa.json.corrupt")); err != nil {
		t.Fatal("the unreadable file was not kept as .corrupt")
	}
	if _, err := os.Stat(filepath.Join(dir, "proxies/bbbb.json")); err != nil {
		t.Fatal("a file from a newer version must be left alone")
	}
	if _, err := os.Stat(filepath.Join(dir, "proxies/eeee.json.123.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a leftover temporary file was not removed")
	}
	ids := []string{}
	for _, p := range d.Proxies() {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []string{"cccc", "dddd"}) {
		t.Fatalf("loaded proxies %q", ids)
	}
	if p, _ := d.Proxy("cccc"); p.Secret != "" || p.Username != "u" {
		t.Fatalf("a proxy whose secret is lost: %+v", p)
	}
	if p, _ := d.Proxy("dddd"); p.ID != "dddd" {
		t.Fatalf("the file name is the ID, got %q", p.ID)
	}
	// Fields missing from an old file take their defaults.
	pr, ok := d.Profile("ffff")
	if !ok || pr.Target.Port != model.DefaultRDPPort || !pr.RememberPassword || pr.Display != model.DefaultDisplay() {
		t.Fatalf("old profile loaded as %+v", pr)
	}
}

func TestLoopbackConflictsAreRepaired(t *testing.T) {
	dir := t.TempDir()
	body := func(loop string) string {
		return `{"schema":1,"name":"PC","target":{"host":"pc.example.com","port":3389},"proxyId":"direct","loopback":"` + loop + `"}`
	}
	write(t, dir, "profiles/aaaa.json", body("127.5.5.5"))
	write(t, dir, "profiles/bbbb.json", body("127.5.5.5")) // a copy of aaaa
	write(t, dir, "profiles/cccc.json", body("10.0.0.1"))  // edited by hand

	d, problems := open(t, dir)
	want := map[string]string{"profiles/bbbb.json": ProblemLoopbackMoved, "profiles/cccc.json": ProblemLoopbackMoved}
	if got := problemCodes(problems); !mapsEqual(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, p := range d.Profiles() {
		if !loopback.ValidString(p.Loopback) || seen[p.Loopback] {
			t.Fatalf("profile %s has %q", p.ID, p.Loopback)
		}
		seen[p.Loopback] = true
	}
	if a, _ := d.Profile("aaaa"); a.Loopback != "127.5.5.5" {
		t.Fatalf("the lower ID should keep its address, got %s", a.Loopback)
	}
	// The repair was saved: the next start has nothing to fix.
	again, problems := open(t, dir)
	if len(problems) != 0 {
		t.Fatalf("problems after the repair: %v", problems)
	}
	if b1, _ := d.Profile("bbbb"); func() string { b2, _ := again.Profile("bbbb"); return b2.Loopback }() != b1.Loopback {
		t.Fatal("the repaired address did not stay")
	}
}

func TestNewProfilesAvoidTakenAddresses(t *testing.T) {
	dir := t.TempDir()
	d, _ := open(t, dir)
	first, _ := d.CreateProfile(profile("one", model.DirectProxyID))
	// CreateProfile picks the IDs, so a collision cannot be staged here
	// (package loopback tests the search itself); every address handed out
	// must still be unique and well-formed.
	addrs := map[string]bool{first.Loopback: true}
	for range 20 {
		p, err := d.CreateProfile(profile("more", model.DirectProxyID))
		if err != nil {
			t.Fatal(err)
		}
		if addrs[p.Loopback] {
			t.Fatalf("address %s handed out twice", p.Loopback)
		}
		addrs[p.Loopback] = true
		if _, err := netip.ParseAddr(p.Loopback); err != nil {
			t.Fatal(err)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
