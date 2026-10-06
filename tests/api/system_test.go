package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/internal/sysproxy"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// configureFunc answers the automatic configuration.
type configureFunc func(ctx context.Context, url string, autoDetect bool, script string) ([]sysproxy.Entry, error)

// fakeSystem stands in for Windows' proxy settings and their automatic
// configuration.
type fakeSystem struct {
	mu        sync.Mutex
	settings  sysproxy.Settings
	err       error
	configure configureFunc
	asked     []string // the URLs the configuration was asked about
}

func (s *fakeSystem) Settings() (sysproxy.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.err
}

func (s *fakeSystem) Configure(ctx context.Context, url string, autoDetect bool, script string) ([]sysproxy.Entry, error) {
	s.mu.Lock()
	configure := s.configure
	s.asked = append(s.asked, url)
	s.mu.Unlock()
	if configure == nil {
		return nil, sysproxy.ErrNothingDetected
	}
	return configure(ctx, url, autoDetect, script)
}

func (s *fakeSystem) set(settings sysproxy.Settings, err error) {
	s.mu.Lock()
	s.settings, s.err = settings, err
	s.mu.Unlock()
}

func (s *fakeSystem) setConfigure(f configureFunc) {
	s.mu.Lock()
	s.configure = f
	s.mu.Unlock()
}

func (s *fakeSystem) askedURLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// names is a configuration that names entries.
func names(entries ...sysproxy.Entry) configureFunc {
	return func(context.Context, string, bool, string) ([]sysproxy.Entry, error) { return entries, nil }
}

// The entry that follows the system is listed with the direct one, before
// the user's own, and like it can be chosen but not changed.
func TestTheSystemEntryIsBuiltIn(t *testing.T) {
	h := newHarness(t)
	own := h.proxy(t, "Office")
	p := h.profile(t, "PC", own.ID, "192.0.2.20:3389", "")
	if _, err := h.profiles.SetProxy(p.ID, model.SystemProxyID); err != nil {
		t.Fatalf("SetProxy(system): %v", err)
	}
	list := h.proxies.List()
	if len(list) != 3 || list[0].Proxy.ID != model.DirectProxyID || list[1].Proxy.ID != model.SystemProxyID ||
		!list[1].BuiltIn || list[1].UsedBy != 1 || list[2].Proxy.ID != own.ID {
		t.Fatalf("List() = %+v", list)
	}
	if v := h.profiles.List(); v[0].ProxyMissing {
		t.Fatal("a profile that follows the system is shown without its proxy")
	}
	if _, err := h.proxies.ShareLink(model.SystemProxyID); !errors.Is(err, store.ErrBuiltIn) {
		t.Fatalf("ShareLink(system) = %v", err)
	}
	if err := h.proxies.Delete(model.SystemProxyID, nil); !errors.Is(err, store.ErrBuiltIn) {
		t.Fatalf("Delete(system) = %v", err)
	}
}

// A session that follows the system goes the way Windows' setting says for
// its target, and its log says which way.
func TestSessionsFollowTheSystemProxy(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	through := func(kind string, port int) model.Proxy {
		return model.Proxy{Schema: model.ProxySchema, ID: model.SystemProxyID, Name: model.SystemProxyID,
			Kind: kind, Server: "127.0.0.1", Port: port}
	}
	manual := through(model.KindHTTP, 10809)
	const script = "http://127.0.0.1:10810/pac"
	scriptFailed := errcode.Wrap(sysproxy.CodeScriptFailed, errors.New("the setup script failed (WinHTTP error 12166)"))
	for _, tc := range []struct {
		name      string
		settings  sysproxy.Settings
		err       error
		configure configureFunc
		route     model.Proxy // the proxy the route is taken for; zero when the session fails first
		fails     string      // the failure's code
		log       []string    // the lines from following the system
	}{
		{name: "manual proxy", settings: sysproxy.Settings{Proxy: "127.0.0.1:10809", Bypass: "<local>"},
			route: manual, log: []string{MsgSystemManual}},
		// The target is this computer (the fake server): directly, the
		// tunnel would connect to itself.
		{name: "no proxy set", settings: sysproxy.Settings{}, fails: "session.loopbackDirect", log: []string{MsgSystemNone}},
		{name: "an exception", settings: sysproxy.Settings{Proxy: "127.0.0.1:10809", Bypass: "127.*"},
			fails: "session.loopbackDirect", log: []string{MsgSystemBypass}},
		{name: "settings unreadable", err: errors.New("access denied"), fails: "sysproxy.unreadable"},
		{name: "a setup script", settings: sysproxy.Settings{Script: script, Proxy: "127.0.0.1:1"},
			configure: names(sysproxy.Entry{Scheme: "http", Host: "127.0.0.1", Port: 10809}),
			route:     manual, log: []string{MsgSystemConfig}},
		{name: "a setup script naming SOCKS5", settings: sysproxy.Settings{Script: script},
			configure: names(sysproxy.Entry{Scheme: "socks", Host: "5 127.0.0.1", Port: 10808}, sysproxy.Entry{Direct: true}),
			route:     through(model.KindSocks, 10808), log: []string{MsgSystemConfig}},
		{name: "a script that fails", settings: sysproxy.Settings{Script: script, Proxy: "127.0.0.1:10809"},
			configure: func(context.Context, string, bool, string) ([]sysproxy.Entry, error) { return nil, scriptFailed },
			route:     manual, log: []string{MsgSystemConfigFailed, MsgSystemManual}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.system.set(tc.settings, tc.err)
			h.system.setConfigure(tc.configure)
			p := h.profile(t, "PC", model.SystemProxyID, srv.Addr, "")
			if _, err := h.sessions.Connect(p.ID, ""); err != nil {
				t.Fatal(err)
			}
			if tc.fails != "" {
				end := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
				if end.Failure == nil || end.Failure.Code != tc.fails || end.FailedStep != string(session.StepRoute) {
					t.Fatalf("final state %+v, failure %+v; want %s in the route step", end, end.Failure, tc.fails)
				}
				if h.routes.acquired.Load() != 0 {
					t.Fatalf("a route was taken for %+v", h.routes.last())
				}
			} else {
				proc := <-h.launched
				if got := h.routes.last(); got != tc.route {
					t.Fatalf("the route was taken for %+v, want %+v", got, tc.route)
				}
				h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
				proc.Close()
				h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
			}
			var got []string
			for _, l := range h.sessions.Log(p.ID) {
				if !strings.HasPrefix(l.Msg, "session.system") {
					continue
				}
				got = append(got, l.Msg)
				switch l.Msg {
				case MsgSystemManual, MsgSystemConfig:
					kind := map[string]string{model.KindHTTP: "HTTP", model.KindSocks: "SOCKS5"}[tc.route.Kind]
					if l.Args["server"] != net.JoinHostPort(tc.route.Server, strconv.Itoa(tc.route.Port)) || l.Args["kind"] != kind {
						t.Errorf("%s args %v", l.Msg, l.Args)
					}
				case MsgSystemConfigFailed:
					// A code, which the log view translates.
					if l.Args["code"] != sysproxy.CodeScriptFailed || l.Args["error"] != scriptFailed.Error() {
						t.Errorf("%s args %v", l.Msg, l.Args)
					}
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.log, ",") {
				t.Fatalf("log lines %q, want %q", got, tc.log)
			}
		})
	}
}

// Running the automatic configuration may take as long as the network
// does: disconnecting does not wait for it.
func TestDisconnectWhileFollowingTheSystem(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	asked := make(chan struct{})
	h.system.set(sysproxy.Settings{AutoDetect: true, Proxy: "127.0.0.1:10809"}, nil)
	h.system.setConfigure(func(ctx context.Context, _ string, _ bool, _ string) ([]sysproxy.Entry, error) {
		close(asked)
		<-ctx.Done()
		return nil, errors.New("the operation was cancelled (WinHTTP error 12017)")
	})
	p := h.profile(t, "PC", model.SystemProxyID, srv.Addr, "")
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	<-asked
	if err := h.sessions.Disconnect(p.ID, false); err != nil {
		t.Fatal(err)
	}
	end := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if end.Outcome != string(session.OutcomeCancelled) || end.Failure != nil || h.routes.acquired.Load() != 0 {
		t.Fatalf("final state %+v; want cancelled before taking a route", end)
	}
}

// The route check and the latency test follow the system too: the check for
// the profile's target, the test for the test URL.
func TestCheckAndLatencyFollowTheSystemProxy(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer web.Close()
	h := newHarness(t)
	st := h.settings.Get()
	st.TestURL = web.URL + "/generate_204"
	if err := h.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	webHost, _, _ := net.SplitHostPort(strings.TrimPrefix(web.URL, "http://"))
	h.system.set(sysproxy.Settings{Script: "http://127.0.0.1:10810/pac"}, nil)
	h.system.setConfigure(names(sysproxy.Entry{Scheme: "http", Host: "gw.example.com", Port: 3128}))
	p := h.profile(t, "PC", model.SystemProxyID, srv.Addr, "")

	if _, err := h.sessions.CheckRoute(context.Background(), p.ID); err != nil {
		t.Fatalf("CheckRoute: %v", err)
	}
	if got := h.routes.last(); got.Kind != model.KindHTTP || got.Server != "gw.example.com" || got.Port != 3128 {
		t.Fatalf("the check's route: %+v", got)
	}
	if _, err := h.proxies.Latency(context.Background(), model.SystemProxyID); err != nil {
		t.Fatalf("Latency(system): %v", err)
	}
	if asked := h.system.askedURLs(); len(asked) != 2 || asked[0] != "https://"+srv.Addr+"/" || !strings.Contains(asked[1], webHost) {
		t.Fatalf("the configuration was asked about %q", asked)
	}
	// The server the setting names is masked in the log file, as the
	// user's own proxy servers are.
	if text := h.log.Redactor().Text("via gw.example.com:3128"); strings.Contains(text, "gw.example.com") {
		t.Fatalf("not masked: %q", text)
	}
}

// Names that say nothing about anyone are not masked: masking them would
// garble every line that has the word.
func TestCommonProxyNamesAreNotMasked(t *testing.T) {
	h := newHarness(t)
	far := h.profile(t, "Far", model.SystemProxyID, "192.0.2.20:3389", "")
	for _, setting := range []string{"localhost:8888", "proxy:8080", "127.0.0.1:10809"} {
		h.system.set(sysproxy.Settings{Proxy: setting}, nil)
		if _, err := h.sessions.SystemRoute(context.Background(), far.ID); err != nil {
			t.Fatalf("SystemRoute through %s: %v", setting, err)
		}
	}
	const line = "RDP-over-proxy: proxy.config on localhost via 127.0.0.1"
	if text := h.log.Redactor().Text(line); text != line {
		t.Fatalf("masked: %q", text)
	}
}

// The check dialog shows the way Windows' setting takes the connection,
// then checks that way without asking Windows again.
func TestSystemRouteAndCheckRouteVia(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	h.system.set(sysproxy.Settings{Proxy: "socks=127.0.0.1:10808", Bypass: "*.corp.example.com"}, nil)
	far := h.profile(t, "Far", model.SystemProxyID, "192.0.2.20:3389", "")
	ctx := context.Background()
	via, err := h.sessions.SystemRoute(ctx, far.ID)
	if err != nil || via != (RouteView{Kind: model.KindSocks, Server: "127.0.0.1", Port: 10808, By: sysproxy.ByManual}) {
		t.Fatalf("SystemRoute = %+v, %v", via, err)
	}
	near := h.profiles.Draft()
	near.Name, near.ProxyID, near.Target = "Near", model.SystemProxyID, model.Target{Host: "pc.corp.example.com", Port: 3389}
	nv, err := h.profiles.Create(near, "")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := h.sessions.SystemRoute(ctx, nv.Profile.ID); err != nil || v != (RouteView{Kind: model.KindDirect, By: sysproxy.ByBypass}) {
		t.Fatalf("SystemRoute of an exception = %+v, %v", v, err)
	}
	if _, err := h.sessions.SystemRoute(ctx, "nosuchprofile"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SystemRoute of a missing profile = %v", err)
	}

	// Why the automatic configuration gave no answer comes with its code.
	failed := errcode.Wrap(sysproxy.CodeScriptUnavailable, errors.New("the setup script could not be downloaded (WinHTTP error 12167)"))
	h.system.set(sysproxy.Settings{Script: "http://127.0.0.1:10810/pac"}, nil)
	h.system.setConfigure(func(context.Context, string, bool, string) ([]sysproxy.Entry, error) { return nil, failed })
	v, err := h.sessions.SystemRoute(ctx, far.ID)
	if err != nil || v.Kind != model.KindDirect || v.ConfigCode != sysproxy.CodeScriptUnavailable || v.ConfigError != failed.Error() {
		t.Fatalf("SystemRoute with a failing script = %+v, %v", v, err)
	}

	// Checking the way shown takes the route it names, whatever Windows
	// says now.
	pc := h.profile(t, "PC", model.SystemProxyID, srv.Addr, "")
	asked := len(h.system.askedURLs())
	if _, err := h.sessions.CheckRouteVia(ctx, pc.ID, via); err != nil {
		t.Fatalf("CheckRouteVia: %v", err)
	}
	if got := h.routes.last(); got.Kind != model.KindSocks || got.Server != "127.0.0.1" || got.Port != 10808 || got.ID != model.SystemProxyID {
		t.Fatalf("the check's route: %+v", got)
	}
	if len(h.system.askedURLs()) != asked {
		t.Fatal("CheckRouteVia asked Windows again")
	}
	// Directly, a target on this computer would be the tunnel itself.
	if _, err := h.sessions.CheckRouteVia(ctx, pc.ID, RouteView{Kind: model.KindDirect, By: sysproxy.ByNone}); !errors.Is(err, session.ErrLoopbackDirect) {
		t.Fatalf("CheckRouteVia directly to this computer = %v", err)
	}
	for _, bad := range []RouteView{{Kind: model.KindSystem}, {Kind: model.KindVMess, Server: "192.0.2.1", Port: 443},
		{Kind: model.KindHTTP, Server: "", Port: 3128}, {Kind: model.KindSocks, Server: "192.0.2.1", Port: 0}} {
		if _, err := h.sessions.CheckRouteVia(ctx, pc.ID, bad); err == nil {
			t.Fatalf("CheckRouteVia(%+v) passed", bad)
		}
	}
}

// The page is told when the setting changes, and only then.
func TestSystemProxyView(t *testing.T) {
	h := newHarness(t)
	if v := h.proxies.SystemProxy(); v.Manual != nil || v.Error != "" || v.Settings != (sysproxy.Settings{}) {
		t.Fatalf("SystemProxy() = %+v", v)
	}
	h.system.set(sysproxy.Settings{Proxy: "127.0.0.1:10809", Bypass: "<local>"}, nil)
	h.systemChanged()
	h.systemChanged() // something else under the Internet settings changed
	sent := h.events.named(EventSystemProxyChanged)
	if len(sent) != 1 {
		t.Fatalf("%d events, want one", len(sent))
	}
	v := sent[0].(SystemProxyView)
	if v.Manual == nil || *v.Manual != (sysproxy.Server{Kind: model.KindHTTP, Host: "127.0.0.1", Port: 10809}) {
		t.Fatalf("event %+v", v)
	}
	if got := h.proxies.SystemProxy(); got.Manual == nil || got.Settings != v.Settings {
		t.Fatalf("SystemProxy() = %+v", got)
	}
	h.system.set(sysproxy.Settings{}, errors.New("access denied"))
	h.systemChanged()
	if sent := h.events.named(EventSystemProxyChanged); len(sent) != 2 || sent[1].(SystemProxyView).Error == "" {
		t.Fatalf("events %+v", sent)
	}

	// Without Windows (a test's Core), the entry cannot be followed.
	bare := NewProxyService(NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)}))
	if v := bare.SystemProxy(); v.Error == "" {
		t.Fatalf("SystemProxy() without Windows = %+v", v)
	}
}

// A change made while the watch starts is not lost: the setting is read
// once the watch is on, so changing it back is a change too.
func TestSystemProxyChangedWhileTheWatchStarts(t *testing.T) {
	data, problems := store.OpenData(t.TempDir(), sealer{})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	system := &fakeSystem{}
	ev := &events{changed: make(chan struct{}, 1)}
	var changed func()
	c := NewCore(Deps{Data: data, Log: logging.New(nil, logging.LevelInfo, 10), Emit: ev.emit, SystemProxy: system,
		WatchSystemProxy: func(f func()) (func(), error) {
			changed = f
			system.set(sysproxy.Settings{Proxy: "127.0.0.1:10809"}, nil) // the user sets a proxy meanwhile
			return func() {}, nil
		}})
	c.Start(nil)
	t.Cleanup(c.Quit)
	system.set(sysproxy.Settings{}, nil) // and turns it off again
	changed()
	if sent := ev.named(EventSystemProxyChanged); len(sent) != 1 || sent[0].(SystemProxyView).Settings != (sysproxy.Settings{}) {
		t.Fatalf("events %+v; want the proxy turned off", sent)
	}
}
