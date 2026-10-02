package store

import (
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
	if err := d.DeleteProxy(model.DirectProxyID); !errors.Is(err, ErrBuiltIn) {
		t.Fatalf("deleting the direct entry = %v", err)
	}
	if _, err := d.UpdateProxy(socks("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("updating a proxy that does not exist = %v", err)
	}
	if len(d.Proxies()) != 0 {
		t.Fatal("a rejected proxy was stored")
	}
}

func TestProxyInUseCannotBeDeleted(t *testing.T) {
	d, _ := open(t, t.TempDir())
	px, _ := d.CreateProxy(socks("Office"))
	pr, err := d.CreateProfile(profile("PC", px.ID))
	if err != nil {
		t.Fatal(err)
	}
	err = d.DeleteProxy(px.ID)
	var inUse *InUseError
	if !errors.As(err, &inUse) || !slices.Equal(inUse.Profiles, []string{pr.ID}) || errcode.Of(err) != "proxy.inUse" {
		t.Fatalf("DeleteProxy of a used proxy = %v", err)
	}
	if _, err := d.DeleteProfile(pr.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteProxy(px.ID); err != nil {
		t.Fatalf("DeleteProxy after its last user went: %v", err)
	}
	if _, ok := d.Proxy(px.ID); ok {
		t.Fatal("the proxy is still there")
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
