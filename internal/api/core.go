package api

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/liubz102/RDP-over-proxy/internal/diag"
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
)

// Events the services send to the frontend.
const (
	// EventDataChanged carries a DataView whenever a profile or proxy
	// changes, and when a session ends (mstsc may have remembered a
	// password).
	EventDataChanged = "data:changed"
	// EventSessionsChanged carries a SessionView each time a session's
	// state changes.
	EventSessionsChanged = "sessions:changed"
	// EventSessionLog carries each line of a session's log.
	EventSessionLog = "session:log"
	// EventNotice carries a Notice: something the user should know about
	// that no button press of theirs caused.
	EventNotice = "app:notice"
	// EventQuitRequested carries a QuitView: the user asked to quit from the
	// tray while remote desktops are connected. The window asks them to
	// confirm and then calls AppService.Quit.
	EventQuitRequested = "app:quitRequested"
	// EventDiagChanged carries the environment report again once a part of
	// it that takes long (Credential Guard) is known (DiagService.Report).
	EventDiagChanged = "diag:changed"
)

func init() {
	application.RegisterEvent[DataView](EventDataChanged)
	application.RegisterEvent[SessionView](EventSessionsChanged)
	application.RegisterEvent[logging.Line](EventSessionLog)
	application.RegisterEvent[Notice](EventNotice)
	application.RegisterEvent[QuitView](EventQuitRequested)
	application.RegisterEvent[[]diag.Item](EventDiagChanged)
}

// Session log keys the services add to the reducer's (session.Msg*).
const (
	// MsgGatewayMaybe: Default.rdp or policy may send the connection through
	// an RD Gateway, which cannot reach the tunnel.
	MsgGatewayMaybe = "session.gatewayMaybe"
	// MsgDefaultsUnknown: Default.rdp or the RD Gateway policy could not be
	// read.
	MsgDefaultsUnknown = "session.defaultsUnknown"
	// MsgServerAuthRefuse: Default.rdp says not to connect when the
	// computer's identity cannot be verified, which through the tunnel it
	// cannot by name. MsgServerAuthRefusePolicy: Group Policy says so.
	MsgServerAuthRefuse       = "session.serverAuthRefuse"
	MsgServerAuthRefusePolicy = "session.serverAuthRefusePolicy"
	// MsgAlwaysPrompt: Default.rdp says to ask for the password every time,
	// so the saved one is not used.
	MsgAlwaysPrompt = "session.alwaysPrompt"
	// MsgHintFailed: the user name hint for mstsc could not be written.
	MsgHintFailed = "session.hintFailed"
)

// Messages lists the session log keys the services add.
var Messages = []string{MsgGatewayMaybe, MsgDefaultsUnknown, MsgServerAuthRefuse, MsgServerAuthRefusePolicy,
	MsgAlwaysPrompt, MsgHintFailed}

// Notice codes, besides the problems found while loading (store.Problem*).
const (
	NoticeSettingsRecovered = "settings.recovered"
	NoticeLogUnavailable    = "log.unavailable"
	NoticeSaveFailed        = "credential.saveFailed"
	NoticeDeleteFailed      = "credential.deleteFailed"
	NoticeForgetFailed      = "mstsc.forgetFailed"
)

// NoticeCodes lists every notice code, for checking that the UI translates
// each one.
var NoticeCodes = []string{
	store.ProblemUnreadable, store.ProblemNewer, store.ProblemSecretLost, store.ProblemLoopbackMoved,
	store.ProblemInvalid, store.ProblemReadFailed, NoticeSettingsRecovered, NoticeLogUnavailable,
	NoticeSaveFailed, NoticeDeleteFailed, NoticeForgetFailed,
}

// ErrGatewayUsed: mstsc would send the connection to an RD Gateway.
var ErrGatewayUsed = errcode.New("gateway.used", "Remote Desktop is set to connect through an RD Gateway, which cannot reach this app's tunnel")

// Vault is where Remote Desktop passwords are kept (secret.Vault).
type Vault interface {
	Save(server, user, password string, oneTime bool) error
	Lookup(server string) (secret.Saved, error)
	Password(server string) (user, password string, found bool, err error)
	Delete(server string) error
	DeleteOneTime(server string) error
	DeleteRemembered(server string) error
}

// Servers is what mstsc remembers per computer (mstsc.Servers). Forget
// removes the address with and without any port.
type Servers interface {
	SetUsernameHint(server, user string) error
	Forget(server string) error
}

// Deps are the parts of the app the services use. The app passes the real
// ones; tests pass stand-ins.
type Deps struct {
	Data     *store.Data
	Settings *store.SettingsStore
	// Routes reaches targets: the Xray engine.
	Routes route.Provider
	// CheckProxy reports whether Xray accepts a proxy's settings
	// (engine.Check), before they are saved. Optional.
	CheckProxy func(model.Proxy) error
	Vault      Vault
	Servers    Servers
	Launch     func(args []string) (session.Process, error)
	// Defaults reads what Default.rdp and Group Policy decide for every
	// connection (mstsc.ReadDefaults). Optional.
	Defaults func() (mstsc.Defaults, error)
	// Diagnose gathers the facts of the environment report (diag.Gather):
	// all but whether Credential Guard runs, which CredentialGuard asks
	// (winx.CredentialGuardRunning). Optional.
	Diagnose        func() diag.Facts
	CredentialGuard func() (bool, error)
	// EditDefaults opens Remote Desktop Connection on Default.rdp
	// (mstsc.EditDefaults), and OpenLogs the log folder. Optional.
	EditDefaults func() error
	OpenLogs     func() error
	Log          *logging.Logger
	// Emit sends an event to the frontend. Optional: by default it goes to
	// the running Wails application.
	Emit func(name string, data any)
}

// Core is what the services share: the data, the sessions, the notices.
type Core struct {
	d       Deps
	manager *session.Manager
	creds   *credStore

	// lifecycle keeps "is it connected? then change or delete it" and "does
	// it exist? then connect it" from interleaving. While a profile is
	// connected, neither it nor its proxy changes: the session goes on with
	// what it started with, and the UI would show settings it does not use.
	lifecycle sync.Mutex

	mu       sync.Mutex
	logs     map[string]*logging.Ring // each profile's latest session log
	logSeq   uint64                   // the last session line's Seq
	notices  []Notice
	noticeID int

	// quitAsked: the window was asked to confirm quitting and has not
	// answered yet (AskToQuit, AppService.KeepRunning).
	quitAsked atomic.Bool

	// guard is what is known about Credential Guard (a diag.Running), asked
	// once (credentialGuard).
	guardOnce sync.Once
	guard     atomic.Int32
}

// sessionLogSize is how many lines of a session's log the UI can show.
const sessionLogSize = 500

// NewCore returns the services' shared core.
func NewCore(d Deps) *Core {
	c := &Core{d: d, creds: &credStore{v: d.Vault}, logs: map[string]*logging.Ring{}}
	if c.d.Emit == nil {
		c.d.Emit = emitToApp
	}
	c.manager = session.NewManager(session.Options{
		Routes:      d.Routes,
		Launch:      d.Launch,
		Preflight:   c.preflight,
		Credentials: c.credentials,
		Changed:     c.sessionChanged,
		Log:         c.sessionLog,
	})
	return c
}

func emitToApp(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

// Start prepares the core once the data has loaded: problems found while
// loading become notices, and one-time passwords a crash may have left in
// Credential Manager are removed (no session runs yet, so none is in use).
func (c *Core) Start(problems []store.Problem) {
	for _, p := range problems {
		c.Notify(logging.LevelWarn, p.Code, map[string]string{"file": p.File}, p.Err)
	}
	for _, p := range c.d.Data.Profiles() {
		if err := c.creds.deleteOneTime(c.server(p)); err != nil {
			c.d.Log.Warnf("remove a leftover one-time password of profile %s: %v", p.ID, err)
		}
	}
	c.refreshRedactor()
}

// Running counts the sessions that have not ended.
func (c *Core) Running() int { return c.manager.Running() }

// AskToQuit asks the window to confirm quitting while sessions run (see
// EventQuitRequested). It returns false, without asking, when the window
// was asked before and has not answered: the page may be unable to (not
// loaded, or broken), and the user asking again is taken as the answer.
func (c *Core) AskToQuit() bool {
	if c.quitAsked.Swap(true) {
		return false
	}
	c.d.Emit(EventQuitRequested, QuitView{Connected: c.Running()})
	return true
}

// Quit ends every session and waits until each has given back everything
// it held. Call it before closing the engine.
func (c *Core) Quit() { c.manager.Quit() }

// server is the name mstsc looks up a profile's saved password under:
// TERMSRV/<loopback address>, without the port. Tools that pre-store mstsc
// passwords (Connect-Mstsc, KeePassRDP) strip the port the same way.
func (c *Core) server(p model.Profile) string {
	return p.Loopback
}

// Notice is something the user should know about that no button press of
// theirs caused, such as a file that could not be loaded. It stays until
// dismissed.
type Notice struct {
	ID    int    `json:"id"`
	Level string `json:"level"`
	// Code is translated ("notices.<code>"), with Args filled in.
	Code    string            `json:"code"`
	Args    map[string]string `json:"args,omitempty"`
	Message string            `json:"message,omitempty"`
}

// Notify adds a notice, logs it and tells the frontend.
func (c *Core) Notify(level, code string, args map[string]string, err error) {
	n := Notice{Level: level, Code: code, Args: args}
	if err != nil {
		n.Message = err.Error()
	}
	c.mu.Lock()
	c.noticeID++
	n.ID = c.noticeID
	c.notices = append(c.notices, n)
	c.mu.Unlock()
	lineArgs := map[string]any{}
	for k, v := range args {
		lineArgs[k] = v
	}
	if err != nil {
		lineArgs["error"] = err.Error()
	}
	c.d.Log.Log(logging.Line{Level: level, Source: logging.SourceApp, Msg: "notice " + code, Args: lineArgs})
	c.d.Emit(EventNotice, n)
}

func (c *Core) dataView() DataView {
	v := DataView{Profiles: []ProfileView{}, Proxies: []ProxyView{}}
	used := map[string]int{}
	proxies := c.d.Data.Proxies()
	exists := map[string]bool{model.DirectProxyID: true}
	for _, p := range proxies {
		exists[p.ID] = true
	}
	for _, p := range c.d.Data.Profiles() {
		used[p.ProxyID]++
		v.Profiles = append(v.Profiles, c.profileView(p, exists[p.ProxyID]))
	}
	v.Proxies = append(v.Proxies, ProxyView{Proxy: model.DirectProxy(), BuiltIn: true, UsedBy: used[model.DirectProxyID]})
	for _, p := range proxies {
		v.Proxies = append(v.Proxies, c.proxyView(p, used[p.ID]))
	}
	return v
}

func (c *Core) profileView(p model.Profile, proxyExists bool) ProfileView {
	v := ProfileView{Profile: p, ProxyMissing: !proxyExists}
	saved, err := c.creds.lookup(c.server(p))
	if err != nil {
		c.d.Log.Warnf("look up the saved password of profile %s: %v", p.ID, err)
	}
	v.PasswordSaved = saved.Ours && !saved.OneTime
	v.PasswordByMstsc = saved.ByMstsc
	return v
}

// proxyView leaves out the secret, and the options and Xray outbound, which
// hold credentials too; ProxyService.Get returns them for editing.
func (c *Core) proxyView(p model.Proxy, usedBy int) ProxyView {
	v := ProxyView{Proxy: p, HasSecret: p.Secret != "", UsedBy: usedBy, SecretsLost: c.d.Data.SecretsLost(p.ID)}
	v.Network, v.Security = p.Transport()
	v.Proxy.Secret, v.Proxy.Outbound, v.Proxy.Options = "", "", model.ProxyOptions{}
	return v
}

// dataChanged tells the frontend about the new data, and the redactor
// about the names to mask.
func (c *Core) dataChanged() {
	c.refreshRedactor()
	c.d.Emit(EventDataChanged, c.dataView())
}

// refreshRedactor masks, in the log file, every name the data holds that
// could say something about the user: hosts, servers, user names, the names
// of connections, groups and proxies, and proxy passwords and the like
// should one ever appear in a message. Names only ever join the set.
func (c *Core) refreshRedactor() {
	var known []string
	for _, p := range c.d.Data.Profiles() {
		known = append(known, p.Target.Host, p.Username, p.Name, p.Group)
	}
	for _, p := range c.d.Data.Proxies() {
		known = append(known, proxyNames(p)...)
	}
	c.d.Log.Redactor().Add(slices.DeleteFunc(known, func(s string) bool {
		s = strings.ToLower(strings.TrimSpace(s))
		a, err := netip.ParseAddr(s)
		return commonWords[s] || err == nil && a.IsLoopback()
	})...)
}

// commonWords are values that say nothing about anyone, such as a gRPC
// service called "grpc" or a path "/ws". Masking them would garble every
// line that mentions the word, such as Xray's notes about gRPC. Loopback
// addresses (a local SOCKS port) are left alone for the same reason.
var commonWords = map[string]bool{}

func init() {
	for _, w := range []string{
		"/", "/ws", "/ray", "/vmess", "/vless", "/trojan", "/grpc", "/xhttp", "/path", "/proxy",
		"tcp", "raw", "udp", "ws", "websocket", "grpc", "gun", "multi", "xhttp", "splithttp", "httpupgrade",
		"kcp", "mkcp", "quic", "http", "https", "h2", "h3", "tls", "reality", "none", "auto",
		"vmess", "vless", "trojan", "shadowsocks", "hysteria", "hysteria2", "socks", "socks5", "freedom",
		"proxy", "direct", "chrome", "firefox", "safari", "localhost",
	} {
		commonWords[w] = true
	}
}

// proxyNames are what a proxy holds that could identify the user or open
// the server: its names and server, its credentials, and the server names,
// paths and keys of its transport, its masks and XHTTP's extra settings.
func proxyNames(p model.Proxy) []string {
	o := p.Options
	names := []string{p.Server, p.Username, p.Name, p.Secret, o.ObfsPassword, o.Seed, o.SNI, o.ServiceName,
		o.Authority, o.PublicKey, o.ShortID}
	for _, list := range []string{o.Host, o.Path, o.VerifyNames} {
		names = append(names, strings.Split(list, ",")...)
	}
	for _, text := range []string{o.FinalMask, o.Extra, p.Outbound} {
		names = append(names, jsonNames(text)...)
	}
	return names
}

// secretKeys are the members of Xray settings, in lower case, whose values
// say where the server is or how to get in.
var secretKeys = map[string]bool{
	"address": true, "id": true, "password": true, "user": true, "pass": true, "auth": true, "email": true,
	"servername": true, "servernames": true, "host": true, "path": true, "servicename": true,
	"authority": true, "publickey": true, "shortid": true, "seed": true, "secretkey": true,
	"privatekey": true, "presharedkey": true, "domain": true,
}

// jsonNames collects those values from Xray settings in JSON (a custom
// outbound, masks, XHTTP's extra settings), and every header value: a
// header may carry a token.
func jsonNames(text string) []string {
	if text == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		return nil
	}
	var names []string
	var walk func(key string, v any, all bool)
	walk = func(key string, v any, all bool) {
		switch v := v.(type) {
		case map[string]any:
			for k, inner := range v {
				k = strings.ToLower(k)
				walk(k, inner, all || k == "headers")
			}
		case []any:
			for _, inner := range v {
				walk(key, inner, all)
			}
		case string:
			if all || secretKeys[key] {
				names = append(names, v)
			}
		}
	}
	walk("", v, false)
	return names
}

// sessionChanged receives every state of every session.
func (c *Core) sessionChanged(profileID string, s session.State) {
	c.d.Emit(EventSessionsChanged, sessionView(profileID, s))
	if s.Step == session.StepDone {
		c.dataChanged() // mstsc may have remembered a password meanwhile
	}
}

// sessionLog receives every line of every session's log.
func (c *Core) sessionLog(profileID string, l session.Log) {
	args := l.Args
	if l.Msg == session.MsgStarting {
		args = c.startingArgs(profileID)
	}
	c.addSessionLine(profileID, logging.Line{Level: string(l.Level), Msg: l.Msg, Args: args}, l.Msg == session.MsgStarting)
}

// startingArgs describe the session in its first log line.
func (c *Core) startingArgs(profileID string) map[string]any {
	p, ok := c.d.Data.Profile(profileID)
	if !ok {
		return nil
	}
	args := map[string]any{"target": p.Target.String(), "proxyId": p.ProxyID}
	if px, ok := c.d.Data.Proxy(p.ProxyID); ok {
		args["proxyKind"] = px.Kind
	}
	return args
}

func (c *Core) addSessionLine(profileID string, line logging.Line, fresh bool) {
	line.Time = time.Now()
	line.Source = logging.SourceSession
	line.Profile = profileID
	c.mu.Lock()
	c.logSeq++
	line.Seq = c.logSeq
	ring := c.logs[profileID]
	if ring == nil {
		ring = logging.NewRing(sessionLogSize)
		c.logs[profileID] = ring
	} else if fresh {
		ring.Clear()
	}
	c.mu.Unlock()
	c.d.Log.Log(line)
	ring.Add(line)
	c.d.Emit(EventSessionLog, line)
}

// preflight runs before a session acquires anything. mstsc takes much of
// what it does from Default.rdp and Group Policy: an RD Gateway it would
// always use stops the session, as the gateway cannot reach the tunnel;
// settings that may get in the way are noted in the session log.
func (c *Core) preflight(req session.Request) error {
	if c.d.Defaults == nil {
		return nil
	}
	id := req.Profile.ID
	d, err := c.d.Defaults()
	if err != nil {
		// Unreadable settings should not stop the connection; mstsc will
		// show what it does.
		c.warnSession(id, MsgDefaultsUnknown, map[string]any{"error": err.Error(), "code": errcode.Of(err)})
		return nil
	}
	g := d.Gateway
	c.d.Log.Redactor().Add(g.Server)
	source := "defaultRdp"
	if g.ByPolicy {
		source = "policy"
	}
	switch g.Verdict {
	case rdpfile.GatewayUsed:
		return errcode.WithArgs(ErrGatewayUsed, map[string]string{"server": g.Server, "source": source})
	case rdpfile.GatewayMaybeUsed:
		c.warnSession(id, MsgGatewayMaybe, map[string]any{"server": g.Server, "source": source})
	}
	if d.ServerAuth == rdpfile.ServerAuthRefuse {
		msg := MsgServerAuthRefuse
		if d.ServerAuthByPolicy {
			msg = MsgServerAuthRefusePolicy
		}
		c.warnSession(id, msg, nil)
	}
	if d.AlwaysPrompt && (req.Password != "" || c.passwordSaved(req.Profile)) {
		c.warnSession(id, MsgAlwaysPrompt, nil)
	}
	return nil
}

// passwordSaved reports whether the app or mstsc keeps a password for the
// profile.
func (c *Core) passwordSaved(p model.Profile) bool {
	saved, err := c.creds.lookup(c.server(p))
	return err == nil && saved.Any()
}

// warnSession adds a warning to the profile's session log.
func (c *Core) warnSession(profileID, msg string, args map[string]any) {
	c.addSessionLine(profileID, logging.Line{Level: logging.LevelWarn, Msg: msg, Args: args}, false)
}

// credentials gives a session what mstsc signs in with.
func (c *Core) credentials(req session.Request) session.Credentials {
	return &sessionCredentials{
		core:      c,
		profileID: req.Profile.ID,
		server:    c.server(req.Profile),
		hint:      req.Profile.Loopback,
		user:      req.Profile.Username,
		password:  req.Password,
	}
}

// sessionCredentials writes the user name hint, and the one-time password
// if there is one, before mstsc starts.
type sessionCredentials struct {
	core      *Core
	profileID string
	server    string
	// hint is the name mstsc keeps its memory of the entrance under (user
	// name hint, certificate trust): the address without the port, as seen
	// on a real machine. Servers.Forget also removes the address with any
	// port, which earlier builds wrote as well.
	hint     string
	user     string
	password string
}

func (s *sessionCredentials) Prepare() (oneTime bool, err error) {
	if s.user != "" {
		// Only a convenience: mstsc asks for the user name if it is missing.
		if err := s.core.d.Servers.SetUsernameHint(s.hint, s.user); err != nil {
			s.core.addSessionLine(s.profileID, logging.Line{Level: logging.LevelWarn, Msg: MsgHintFailed,
				Args: map[string]any{"error": err.Error(), "code": errcode.Of(err)}}, false)
		}
	}
	if s.password == "" {
		return false, nil
	}
	oneTime, err = s.core.creds.saveOneTime(s.server, s.user, s.password)
	s.password = ""
	return oneTime, err
}

func (s *sessionCredentials) Delete() error {
	return s.core.creds.deleteOneTime(s.server)
}

func checkView(r probe.Result) *CheckView {
	v := &CheckView{ElapsedMs: r.Elapsed.Milliseconds(), Protocol: r.Confirm.Selected.String()}
	if r.Confirm.Failure != 0 {
		v.Protocol, v.NegotiationFailure = "", r.Confirm.Failure.String()
	}
	return v
}
