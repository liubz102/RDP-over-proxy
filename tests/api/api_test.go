package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/internal/secret"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// These tests run the services against real files in a temporary folder and
// stand-ins for everything Windows keeps: Credential Manager, mstsc's
// registry memory and mstsc itself, which is never started.

type cred struct {
	user, password string
	oneTime        bool
}

// fakeVault stands in for Credential Manager.
type fakeVault struct {
	mu    sync.Mutex
	ours  map[string]cred
	mstsc map[string]string // server -> user, as if mstsc remembered a password
}

func newFakeVault() *fakeVault {
	return &fakeVault{ours: map[string]cred{}, mstsc: map[string]string{}}
}

func (v *fakeVault) Save(server, user, password string, oneTime bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ours[server] = cred{user, password, oneTime}
	return nil
}

func (v *fakeVault) Lookup(server string) (secret.Saved, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ours := v.ours[server]
	mu, byMstsc := v.mstsc[server]
	s := secret.Saved{Ours: ours, OneTime: c.oneTime, ByMstsc: byMstsc, User: c.user}
	if !ours {
		s.User = mu
	}
	return s, nil
}

func (v *fakeVault) Password(server string) (string, string, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.ours[server]
	return c.user, c.password, ok, nil
}

func (v *fakeVault) Delete(server string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.ours, server)
	delete(v.mstsc, server)
	return nil
}

func (v *fakeVault) DeleteOneTime(server string) error    { return v.deleteOurs(server, true) }
func (v *fakeVault) DeleteRemembered(server string) error { return v.deleteOurs(server, false) }

func (v *fakeVault) deleteOurs(server string, oneTime bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if c, ok := v.ours[server]; ok && c.oneTime == oneTime {
		delete(v.ours, server)
	}
	return nil
}

func (v *fakeVault) get(server string) (cred, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	c, ok := v.ours[server]
	return c, ok
}

// fakeServers stands in for mstsc's registry memory.
type fakeServers struct {
	mu        sync.Mutex
	hints     map[string]string
	forgotten []string
}

func (s *fakeServers) SetUsernameHint(server, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hints[server] = user
	return nil
}

// Forget removes the bare address and the address with any port, as
// mstsc.Servers does.
func (s *fakeServers) Forget(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.hints {
		if name == server || strings.HasPrefix(name, server+":") {
			delete(s.hints, name)
		}
	}
	s.forgotten = append(s.forgotten, server)
	return nil
}

// fakeProcess stands in for mstsc: it exits when asked to close.
type fakeProcess struct {
	pid  int
	args []string
	exit chan int
	once sync.Once
}

func (p *fakeProcess) PID() int              { return p.pid }
func (p *fakeProcess) Wait() (int, error)    { return <-p.exit, nil }
func (p *fakeProcess) Close() (bool, error)  { p.once.Do(func() { p.exit <- 0 }); return true, nil }
func (p *fakeProcess) Kill() error           { p.once.Do(func() { p.exit <- 1 }); return nil }
func (p *fakeProcess) Focus() error          { return nil }
func (p *fakeProcess) ShowName(string) error { return nil }

// anyRoute reaches every target directly, whatever the proxy: it stands in
// for the Xray engine.
type anyRoute struct {
	acquired, released atomic.Int32
	mu                 sync.Mutex
	asked              []model.Proxy // the proxies routes were taken for
}

func (r *anyRoute) Acquire(p model.Proxy) (route.Dialer, func(), error) {
	r.acquired.Add(1)
	r.mu.Lock()
	r.asked = append(r.asked, p)
	r.mu.Unlock()
	return route.Direct(), func() { r.released.Add(1) }, nil
}

// last is the proxy the latest route was taken for.
func (r *anyRoute) last() model.Proxy {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.asked) == 0 {
		return model.Proxy{}
	}
	return r.asked[len(r.asked)-1]
}

// sealer stands in for DPAPI.
type sealer struct{}

func (sealer) Seal(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return "sealed:" + base64.StdEncoding.EncodeToString([]byte(s)), nil
}

func (sealer) Open(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "sealed:"))
	return string(b), err
}

type event struct {
	name string
	data any
}

// events records what the services send to the frontend.
type events struct {
	mu      sync.Mutex
	all     []event
	changed chan struct{}
}

func (e *events) emit(name string, data any) {
	e.mu.Lock()
	e.all = append(e.all, event{name, data})
	e.mu.Unlock()
	select {
	case e.changed <- struct{}{}:
	default:
	}
}

func (e *events) named(name string) []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []any
	for _, ev := range e.all {
		if ev.name == name {
			out = append(out, ev.data)
		}
	}
	return out
}

// session waits until the profile's latest session state satisfies ok.
func (e *events) session(id string, ok func(SessionView) bool) SessionView {
	for {
		views := e.named(EventSessionsChanged)
		for i := len(views) - 1; i >= 0; i-- {
			if v := views[i].(SessionView); v.ProfileID == id {
				if ok(v) {
					return v
				}
				break
			}
		}
		<-e.changed
	}
}

type harness struct {
	core     *Core
	profiles *ProfileService
	proxies  *ProxyService
	sessions *SessionService
	app      *AppService
	data     *store.Data
	settings *store.SettingsStore
	vault    *fakeVault
	servers  *fakeServers
	routes   *anyRoute
	events   *events
	launched chan *fakeProcess
	// defaults stands in for Default.rdp and the RD Gateway policy, and
	// defaultsErr for failing to read them.
	defaults    mstsc.Defaults
	defaultsErr error
	quits       atomic.Int32 // AppService.Quit's calls of the app's quit
	log         *logging.Logger
	// check stands in for Xray's check of a proxy's settings; nil accepts
	// everything.
	check func(model.Proxy) error
	// system stands in for Windows' proxy settings; systemChanged is how
	// the app is told they changed.
	system        *fakeSystem
	systemChanged func()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h, problems := newHarnessIn(t, t.TempDir())
	if len(problems) != 0 {
		t.Fatalf("OpenData: %v", problems)
	}
	return h
}

// newHarnessIn starts the services on the data in dir, as the app does,
// and returns the problems found while loading.
func newHarnessIn(t *testing.T, dir string) (*harness, []store.Problem) {
	t.Helper()
	data, problems := store.OpenData(dir, sealer{})
	settings := store.NewSettingsStore(dir)
	st := model.DefaultSettings()
	st.Language = model.LangEn
	// Tunnels must never use the real port 13389. Sessions take the port
	// from the settings, which cannot say "0"; FreePort is the registered
	// exception for that.
	st.LocalPort = testutil.FreePort(t)
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	h := &harness{
		data:     data,
		settings: settings,
		vault:    newFakeVault(),
		servers:  &fakeServers{hints: map[string]string{}},
		routes:   &anyRoute{},
		events:   &events{changed: make(chan struct{}, 1)},
		launched: make(chan *fakeProcess, 4),
		log:      logging.New(nil, logging.LevelInfo, 100),
		system:   &fakeSystem{},
	}
	var pids atomic.Int32
	h.core = NewCore(Deps{
		Data:     data,
		Settings: settings,
		Routes:   h.routes,
		CheckProxy: func(p model.Proxy) error {
			if h.check != nil {
				return h.check(p)
			}
			return nil
		},
		Vault:   h.vault,
		Servers: h.servers,
		Launch: func(args []string) (session.Process, error) {
			p := &fakeProcess{pid: 5000 + int(pids.Add(1)), args: args, exit: make(chan int, 1)}
			h.launched <- p
			return p, nil
		},
		Defaults:    func() (mstsc.Defaults, error) { return h.defaults, h.defaultsErr },
		SystemProxy: h.system,
		WatchSystemProxy: func(changed func()) (func(), error) {
			h.systemChanged = changed
			return func() {}, nil
		},
		Log:  h.log,
		Emit: h.events.emit,
	})
	t.Cleanup(h.core.Quit)
	h.profiles = NewProfileService(h.core)
	h.proxies = NewProxyService(h.core)
	h.sessions = NewSessionService(h.core)
	h.app = NewAppService(h.core, func() { h.quits.Add(1) })
	h.core.Start(problems)
	return h, problems
}

// proxy stores a SOCKS proxy; anyRoute ignores where it points.
func (h *harness) proxy(t *testing.T, name string) model.Proxy {
	t.Helper()
	v, err := h.proxies.Create(model.Proxy{Name: name, Kind: model.KindSocks, Server: "192.0.2.10", Port: 1080,
		Username: "proxyuser", Secret: "proxy secret"})
	if err != nil {
		t.Fatal(err)
	}
	return v.Proxy
}

// profile stores a profile whose target is addr ("host:port").
func (h *harness) profile(t *testing.T, name, proxyID, addr, password string) model.Profile {
	t.Helper()
	ap := netip.MustParseAddrPort(addr)
	p := h.profiles.Draft()
	p.Name, p.ProxyID, p.Username = name, proxyID, `EXAMPLE\alice`
	p.Target = model.Target{Host: ap.Addr().String(), Port: int(ap.Port())}
	v, err := h.profiles.Create(p, password)
	if err != nil {
		t.Fatal(err)
	}
	return v.Profile
}

func TestProfilePasswords(t *testing.T) {
	h := newHarness(t)
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, "192.0.2.20:3389", "first")
	server := p.Loopback
	if c, ok := h.vault.get(server); !ok || c.password != "first" || c.oneTime || c.user != `EXAMPLE\alice` {
		t.Fatalf("after Create: %+v %v", c, ok)
	}
	if v := h.profiles.List(); len(v) != 1 || !v[0].PasswordSaved || v[0].PasswordByMstsc {
		t.Fatalf("List = %+v", v)
	}

	// A new user name takes the saved password along.
	p.Username = "alice@example.com"
	if _, err := h.profiles.Update(p, ""); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.vault.get(server); c.user != "alice@example.com" || c.password != "first" {
		t.Fatalf("after renaming: %+v", c)
	}

	// A new password replaces the old one.
	if _, err := h.profiles.Update(p, "second"); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.vault.get(server); c.password != "second" {
		t.Fatalf("after a new password: %+v", c)
	}

	// Not remembering any more deletes the app's password, not mstsc's.
	h.vault.mstsc[server] = "alice"
	p.RememberPassword = false
	if v, err := h.profiles.Update(p, "ignored"); err != nil || v.PasswordSaved || !v.PasswordByMstsc {
		t.Fatalf("after turning remember off: %+v, %v", v, err)
	}

	// Another computer: every saved password goes.
	p.RememberPassword = true
	if _, err := h.profiles.Update(p, "third"); err != nil {
		t.Fatal(err)
	}
	p.Target.Port = 3390
	v, err := h.profiles.Update(p, "")
	if err != nil || v.PasswordSaved || v.PasswordByMstsc || v.Profile.Loopback != server {
		t.Fatalf("after moving the target: %+v, %v", v, err)
	}

	// ForgetPassword removes both kinds.
	h.vault.mstsc[server] = "alice"
	if _, err := h.profiles.Update(p, "fourth"); err != nil {
		t.Fatal(err)
	}
	if err := h.profiles.ForgetPassword(p.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.vault.Lookup(server); s.Any() {
		t.Fatalf("after ForgetPassword: %+v", s)
	}

	// Deleting the profile forgets everything about its address.
	withPort := net.JoinHostPort(server, strconv.Itoa(h.settings.Get().LocalPort))
	h.servers.hints[server], h.servers.hints[withPort], h.servers.hints[server+":23389"] = "alice", "alice", "alice"
	if err := h.profiles.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.servers.forgotten, server) || len(h.servers.hints) != 0 {
		t.Fatalf("mstsc's memory of the address was not removed: forgot %q, left %q", h.servers.forgotten, h.servers.hints)
	}
	if len(h.profiles.List()) != 0 {
		t.Fatal("the profile is still listed")
	}
	if n := len(h.events.named(EventDataChanged)); n < 7 {
		t.Fatalf("%d data:changed events, want one per change", n)
	}
}

func TestOneTimePasswordNeverReplacesARememberedOne(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	p := h.profile(t, "PC", h.proxy(t, "Office").ID, srv.Addr, "")
	p.RememberPassword = false
	if _, err := h.profiles.Update(p, ""); err != nil {
		t.Fatal(err)
	}
	// The user saved a password to remember after the session decided to
	// use a one-time one (say, during a long route check): it is there when
	// the session prepares the sign-in.
	if err := h.vault.Save(p.Loopback, p.Username, "remembered", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Connect(p.ID, "one time"); err != nil {
		t.Fatal(err)
	}
	proc := <-h.launched
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
	proc.Close()
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	// The one-time password stepped aside, and its session's end did not
	// delete the remembered one.
	if got, ok := h.vault.get(p.Loopback); !ok || got.password != "remembered" || got.oneTime {
		t.Fatalf("the vault holds %+v %v; want the remembered password", got, ok)
	}
}

func TestPasswordValidation(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("x", secret.MaxPasswordLen+1)
	p := h.profiles.Draft()
	p.Name, p.ProxyID, p.Target = "PC", model.DirectProxyID, model.Target{Host: "pc.example.com", Port: 3389}
	_, err := h.profiles.Create(p, long)
	// What the UI receives (the error in the rejected call's cause).
	var v ErrorView
	if jerr := json.Unmarshal(MarshalError(err), &v); jerr != nil ||
		v.Code != CodeValidation || len(v.Fields) != 1 || v.Fields[0].Field != "password" {
		t.Fatalf("Create with a long password: %+v (%v)", v, jerr)
	}
	if len(h.profiles.List()) != 0 {
		t.Fatal("the profile was created anyway")
	}
}

func TestProxies(t *testing.T) {
	h := newHarness(t)
	px := h.proxy(t, "Office")
	// The two built-in entries come first: direct, and following the system.
	list := h.proxies.List()
	if len(list) != 3 || !list[0].BuiltIn || !list[1].BuiltIn || list[2].Proxy.Secret != "" || !list[2].HasSecret {
		t.Fatalf("List = %+v", list)
	}
	got, err := h.proxies.Get(px.ID)
	if err != nil || got.Secret != "" || got.Username != "proxyuser" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	// Editing without touching the password keeps it.
	got.Name = "Office 2"
	if _, err := h.proxies.Update(got, true); err != nil {
		t.Fatal(err)
	}
	if stored, _ := h.data.Proxy(px.ID); stored.Secret != "proxy secret" || stored.Name != "Office 2" {
		t.Fatalf("stored %+v", stored)
	}

	// Deleting a proxy in use moves the users the caller names to direct;
	// any other user stops it, and the error names them.
	one := h.profile(t, "PC one", px.ID, "192.0.2.20:3389", "")
	two := h.profile(t, "PC two", px.ID, "192.0.2.21:3389", "")
	if view := errorJSON(t, h.proxies.Delete(px.ID, nil)); view.Code != "proxy.inUse" || view.Args["profiles"] != "PC one, PC two" {
		t.Fatalf("Delete of a used proxy naming none: %+v", view)
	}
	if view := errorJSON(t, h.proxies.Delete(px.ID, []string{one.ID})); view.Code != "proxy.inUse" || view.Args["profiles"] != "PC two" {
		t.Fatalf("Delete of a used proxy naming one of two: %+v", view)
	}
	changes := len(h.events.named(EventDataChanged))
	if err := h.proxies.Delete(px.ID, []string{one.ID, two.ID}); err != nil {
		t.Fatalf("Delete naming every user: %v", err)
	}
	for _, v := range h.profiles.List() {
		if v.Profile.ProxyID != model.DirectProxyID || v.ProxyMissing {
			t.Fatalf("after Delete: %+v", v)
		}
	}
	if list := h.proxies.List(); len(list) != 2 || list[0].UsedBy != 2 {
		t.Fatalf("after Delete, proxies = %+v", list)
	}
	if len(h.events.named(EventDataChanged)) != changes+1 {
		t.Fatal("Delete did not tell the frontend")
	}
	if err := h.proxies.Delete(model.DirectProxyID, nil); errcode.Of(err) != "store.builtIn" {
		t.Fatalf("deleting the direct entry: %v", err)
	}
}

func TestSetProxy(t *testing.T) {
	h := newHarness(t)
	office, home := h.proxy(t, "Office"), h.proxy(t, "Home")
	p := h.profile(t, "PC", office.ID, "192.0.2.20:3389", "")
	v, err := h.profiles.SetProxy(p.ID, home.ID)
	if err != nil || v.Profile.ProxyID != home.ID || v.ProxyMissing {
		t.Fatalf("SetProxy = %+v, %v", v, err)
	}
	// The editor does not change it back.
	p.Name = "Renamed"
	if v, err := h.profiles.Update(p, ""); err != nil || v.Profile.ProxyID != home.ID {
		t.Fatalf("Update after SetProxy = %+v, %v", v.Profile, err)
	}
	if _, err := h.profiles.SetProxy(p.ID, "nosuchproxy"); errcode.Of(err) != "store.notFound" {
		t.Fatalf("SetProxy to a missing proxy = %v", err)
	}
	if v, err := h.profiles.SetProxy(p.ID, model.DirectProxyID); err != nil || v.Profile.ProxyID != model.DirectProxyID {
		t.Fatalf("SetProxy to direct = %+v, %v", v, err)
	}
}

// While a profile is connected, neither it nor its proxy changes: its
// session would go on with what it started with.
func TestConnectedSettingsAreLocked(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	px, other := h.proxy(t, "Office"), h.proxy(t, "Home")
	p := h.profile(t, "PC", px.ID, srv.Addr, "secret")
	idle := h.profile(t, "PC idle", px.ID, "192.0.2.21:3389", "")
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	proc := <-h.launched
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })

	edit := p
	edit.Name = "Renamed"
	if _, err := h.profiles.Update(edit, ""); errcode.Of(err) != "session.running" {
		t.Fatalf("Update while connected = %v", err)
	}
	if _, err := h.profiles.SetProxy(p.ID, other.ID); errcode.Of(err) != "session.running" {
		t.Fatalf("SetProxy while connected = %v", err)
	}
	if err := h.profiles.ForgetPassword(p.ID); errcode.Of(err) != "session.running" {
		t.Fatalf("ForgetPassword while connected = %v", err)
	}
	proxy, _ := h.proxies.Get(px.ID)
	proxy.Name = "Office 2"
	_, err := h.proxies.Update(proxy, true)
	if view := errorJSON(t, err); view.Code != "proxy.connected" || view.Args["profiles"] != "PC" {
		t.Fatalf("proxy Update while a user is connected: %+v", view)
	}
	err = h.proxies.Delete(px.ID, []string{p.ID, idle.ID})
	if view := errorJSON(t, err); view.Code != "proxy.connected" || view.Args["profiles"] != "PC" {
		t.Fatalf("proxy Delete while a user is connected: %+v", view)
	}
	if got, _ := h.data.Profile(p.ID); got != p {
		t.Fatalf("the connected profile changed: %+v", got)
	}
	if got, _ := h.data.Proxy(px.ID); got.Name != "Office" {
		t.Fatalf("its proxy changed: %+v", got)
	}
	if c, ok := h.vault.get(p.Loopback); !ok || c.password != "secret" {
		t.Fatal("the password went")
	}
	// A profile that is not connected changes as before.
	if _, err := h.profiles.SetProxy(idle.ID, other.ID); err != nil {
		t.Fatalf("SetProxy of a profile that is not connected: %v", err)
	}

	proc.Close()
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if _, err := h.profiles.Update(edit, ""); err != nil {
		t.Fatalf("Update after the session ended: %v", err)
	}
	if _, err := h.proxies.Update(proxy, true); err != nil {
		t.Fatalf("proxy Update after the session ended: %v", err)
	}
	if err := h.proxies.Delete(px.ID, []string{p.ID}); err != nil {
		t.Fatalf("proxy Delete after the session ended: %v", err)
	}
	if err := h.profiles.ForgetPassword(p.ID); err != nil {
		t.Fatalf("ForgetPassword after the session ended: %v", err)
	}
}

// errorJSON is what the UI receives of err: the error in the rejected
// call's cause.
func errorJSON(t *testing.T, err error) ErrorView {
	t.Helper()
	var v ErrorView
	if jerr := json.Unmarshal(MarshalError(err), &v); jerr != nil {
		t.Fatalf("MarshalError(%v): %v", err, jerr)
	}
	return v
}

func TestLatency(t *testing.T) {
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
	px := h.proxy(t, "Office")
	if _, err := h.proxies.Latency(t.Context(), px.ID); err != nil {
		t.Fatalf("Latency: %v", err)
	}
	if h.routes.acquired.Load() != 1 || h.routes.released.Load() != 1 {
		t.Fatal("the route was not given back")
	}
}

func TestSessionWithOneTimePassword(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybridEx})
	h := newHarness(t)
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, srv.Addr, "")
	p.RememberPassword = false
	if _, err := h.profiles.Update(p, ""); err != nil {
		t.Fatal(err)
	}

	res, err := h.sessions.Connect(p.ID, "one time")
	if err != nil || res.Focused {
		t.Fatalf("Connect = %+v, %v", res, err)
	}
	proc := <-h.launched
	running := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
	if running.Check == nil || running.Check.Protocol != "HYBRID_EX" || running.Addr == "" {
		t.Fatalf("running: %+v", running)
	}
	if c, ok := h.vault.get(p.Loopback); !ok || !c.oneTime || c.password != "one time" {
		t.Fatalf("while running, the vault holds %+v %v", c, ok)
	}
	// mstsc keeps its memory under the address without the port.
	if h.servers.hints[p.Loopback] != `EXAMPLE\alice` || len(h.servers.hints) != 1 {
		t.Fatalf("user name hints = %q", h.servers.hints)
	}
	if err := h.profiles.Delete(p.ID); errcode.Of(err) != "session.running" {
		t.Fatalf("deleting a connected profile = %v", err)
	}
	if res, err := h.sessions.Connect(p.ID, ""); err != nil || !res.Focused {
		t.Fatalf("connecting again = %+v, %v; want focused", res, err)
	}

	if err := h.sessions.Disconnect(p.ID, false); err != nil {
		t.Fatal(err)
	}
	end := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if end.Outcome != "closed" || proc.args[0] != "/v:"+running.Addr {
		t.Fatalf("ended: %+v, mstsc args %q", end, proc.args)
	}
	if _, ok := h.vault.get(p.Loopback); ok {
		t.Fatal("the one-time password outlived the session")
	}
	if err := h.sessions.Disconnect(p.ID, false); errcode.Of(err) != "session.none" {
		t.Fatalf("disconnecting an ended session = %v", err)
	}

	lines := h.sessions.Log(p.ID)
	if len(lines) == 0 || lines[0].Msg != session.MsgStarting || lines[0].Args["target"] != srv.Addr {
		t.Fatalf("log starts with %+v", lines)
	}
	if len(h.events.named(EventSessionLog)) != len(lines) {
		t.Fatal("not every log line was sent to the frontend")
	}
	for i := 1; i < len(lines); i++ {
		if lines[i].Seq <= lines[i-1].Seq {
			t.Fatalf("log line %d has Seq %d after %d", i, lines[i].Seq, lines[i-1].Seq)
		}
	}
	if states := h.sessions.States(); len(states) != 1 || states[0].Phase != "ended" {
		t.Fatalf("States = %+v", states)
	}
}

func TestConnectWithPasswordToRemember(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, srv.Addr, "")
	if _, err := h.sessions.Connect(p.ID, "keep me"); err != nil {
		t.Fatal(err)
	}
	proc := <-h.launched
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
	proc.Close()
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if c, ok := h.vault.get(p.Loopback); !ok || c.oneTime || c.password != "keep me" {
		t.Fatalf("the vault holds %+v %v; want the password remembered", c, ok)
	}
}

func TestQuitAsksWhileConnected(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	p := h.profile(t, "PC", h.proxy(t, "Office").ID, srv.Addr, "")
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	proc := <-h.launched
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })

	if v := h.app.Quit(false); v.Connected != 1 || h.quits.Load() != 0 {
		t.Fatalf("unconfirmed Quit while connected = %+v, quit called %d times", v, h.quits.Load())
	}
	if !h.core.AskToQuit() {
		t.Fatal("AskToQuit did not ask")
	}
	if asks := h.events.named(EventQuitRequested); len(asks) != 1 || asks[0].(QuitView).Connected != 1 {
		t.Fatalf("AskToQuit sent %+v", asks)
	}
	if h.core.AskToQuit() {
		t.Fatal("asking again before an answer asked again; want the caller to quit")
	}
	h.app.KeepRunning()
	if !h.core.AskToQuit() || len(h.events.named(EventQuitRequested)) != 2 {
		t.Fatal("after KeepRunning, AskToQuit did not ask again")
	}
	if v := h.app.Quit(true); v.Connected != 0 || h.quits.Load() != 1 {
		t.Fatalf("confirmed Quit = %+v, quit called %d times", v, h.quits.Load())
	}

	proc.Close()
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if v := h.app.Quit(false); v.Connected != 0 || h.quits.Load() != 2 {
		t.Fatalf("Quit with nothing connected = %+v, quit called %d times", v, h.quits.Load())
	}
}

func TestGatewayPreflight(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, srv.Addr, "")

	h.defaults.Gateway = mstsc.Gateway{Verdict: rdpfile.GatewayUsed, Server: "gw.example.com"}
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	end := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if end.Failure == nil || end.Failure.Code != "gateway.used" || end.FailedStep != "preflight" ||
		end.Failure.Args["server"] != "gw.example.com" {
		t.Fatalf("ended: %+v failure %+v", end, end.Failure)
	}
	// The log line carries what the translated message needs.
	var failed *logging.Line
	for _, l := range h.sessions.Log(p.ID) {
		if l.Msg == session.MsgStepFailed {
			failed = &l
		}
	}
	if failed == nil || failed.Args["code"] != "gateway.used" ||
		failed.Args["errorArgs"].(map[string]string)["server"] != "gw.example.com" {
		t.Fatalf("step failed line: %+v", failed)
	}

	h.defaults.Gateway = mstsc.Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "gw.example.com"}
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	proc := <-h.launched
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
	proc.Close()
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	var keys []string
	for _, l := range h.sessions.Log(p.ID) {
		keys = append(keys, l.Msg)
	}
	if !slices.Contains(keys, MsgGatewayMaybe) || keys[0] != session.MsgStarting {
		t.Fatalf("log of the second session: %q", keys)
	}
}

func TestCheckRoute(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolSSL})
	quiet := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerNothing})
	h := newHarness(t)
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, srv.Addr, "")
	v, err := h.sessions.CheckRoute(t.Context(), p.ID)
	if err != nil || v.Protocol != "SSL" {
		t.Fatalf("CheckRoute = %+v, %v", v, err)
	}

	// Cancelling the call stops a check that would wait forever.
	q := h.profile(t, "Quiet", px.ID, quiet.Addr, "")
	ctx, cancel := context.WithCancel(t.Context())
	go func() { <-quiet.Requests(); cancel() }()
	if _, err := h.sessions.CheckRoute(ctx, q.ID); errcode.Of(err) != errcode.Cancelled {
		t.Fatalf("cancelled CheckRoute = %v", err)
	}

	// Direct to this computer would connect the tunnel to itself.
	d := h.profile(t, "Self", model.DirectProxyID, srv.Addr, "")
	if _, err := h.sessions.CheckRoute(t.Context(), d.ID); errcode.Of(err) != "session.loopbackDirect" {
		t.Fatalf("CheckRoute direct to loopback = %v", err)
	}
	if h.routes.acquired.Load() != h.routes.released.Load() {
		t.Fatal("a route was not given back")
	}
}

func TestStartReportsProblemsAndRemovesLeftovers(t *testing.T) {
	h := newHarness(t)
	p := h.profile(t, "PC", model.DirectProxyID, "192.0.2.20:3389", "")
	h.vault.ours[p.Loopback] = cred{user: "alice", password: "left over", oneTime: true}
	h.core.Start([]store.Problem{{File: `proxies\abcd.json`, Code: store.ProblemSecretLost, Err: errors.New("cannot decrypt")}})
	if _, ok := h.vault.get(p.Loopback); ok {
		t.Fatal("a one-time password from before was not removed")
	}
	notices := h.app.Notices()
	if len(notices) != 1 || notices[0].Code != store.ProblemSecretLost || notices[0].Args["file"] != `proxies\abcd.json` {
		t.Fatalf("Notices = %+v", notices)
	}
	if got := h.events.named(EventNotice); len(got) != 1 {
		t.Fatalf("notice events: %v", got)
	}
	h.app.Dismiss(notices[0].ID)
	if len(h.app.Notices()) != 0 {
		t.Fatal("Dismiss left the notice")
	}
}

func TestMarshalError(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{model.FieldErrors{{Field: "name", Code: model.CodeRequired}}, CodeValidation},
		{store.ErrNotFound, "store.notFound"},
		{errors.New("something odd"), errcode.Unknown},
		{&net.OpError{Op: "dial", Err: errors.New("x")}, errcode.Unknown},
	}
	for _, c := range cases {
		var v ErrorView
		if err := json.Unmarshal(MarshalError(c.err), &v); err != nil {
			t.Fatal(err)
		}
		if v.Code != c.code || v.Message != c.err.Error() {
			t.Errorf("MarshalError(%v) = %+v, want code %q", c.err, v, c.code)
		}
	}
}
