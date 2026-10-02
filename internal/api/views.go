package api

import (
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/session"
)

// ProfileView is a profile as the UI lists it.
type ProfileView struct {
	Profile model.Profile `json:"profile"`
	// PasswordSaved: the app remembers a password for this connection.
	PasswordSaved bool `json:"passwordSaved"`
	// PasswordByMstsc: mstsc itself remembers one ("Remember me" in its
	// sign-in prompt).
	PasswordByMstsc bool `json:"passwordByMstsc"`
	// ProxyMissing: the profile's proxy no longer exists.
	ProxyMissing bool `json:"proxyMissing"`
}

// ProxyView is a proxy as the UI lists it. Secret and Outbound, which both
// hold credentials, are always empty; ProxyService.Get returns the outbound.
type ProxyView struct {
	Proxy model.Proxy `json:"proxy"`
	// HasSecret: a password is stored.
	HasSecret bool `json:"hasSecret"`
	// BuiltIn: the direct entry, which cannot be edited or deleted.
	BuiltIn bool `json:"builtIn"`
	// UsedBy counts the profiles that use the proxy.
	UsedBy int `json:"usedBy"`
}

// DataView is every profile and proxy, sent with EventDataChanged.
type DataView struct {
	Profiles []ProfileView `json:"profiles"`
	Proxies  []ProxyView   `json:"proxies"`
}

// CheckView is the result of a route check.
type CheckView struct {
	ElapsedMs int64 `json:"elapsedMs"`
	// Protocol is the security protocol the server chose, such as
	// "HYBRID_EX"; empty when NegotiationFailure is set.
	Protocol string `json:"protocol"`
	// NegotiationFailure is why the server refused every protocol offered,
	// such as "SSL_REQUIRED_BY_SERVER". The route works all the same.
	NegotiationFailure string `json:"negotiationFailure,omitempty"`
}

// SessionView is a session's state as the UI shows it.
type SessionView struct {
	ProfileID string `json:"profileId"`
	// Phase is preparing, checking, launching, running, ending or ended.
	Phase string `json:"phase"`
	Step  string `json:"step"`
	// Outcome is set once the phase is ended: closed, cancelled or failed.
	Outcome string `json:"outcome"`
	// Failure and FailedStep say why a failed session ended.
	Failure    *ErrorView `json:"failure"`
	FailedStep string     `json:"failedStep"`
	// Addr is the tunnel entrance mstsc connects to.
	Addr     string `json:"addr"`
	PID      int    `json:"pid"`
	ExitCode int    `json:"exitCode"`
	// Conns counts mstsc's open connections through the tunnel.
	Conns int `json:"conns"`
	// Upstream is unknown, ok or failing; UpstreamError is the latest reason
	// a connection could not reach the target.
	Upstream      string     `json:"upstream"`
	UpstreamError *ErrorView `json:"upstreamError"`
	// TunnelError is set when the entrance stopped accepting connections.
	TunnelError *ErrorView `json:"tunnelError"`
	Check       *CheckView `json:"check"`
}

// ConnectResult is what Connect did.
type ConnectResult struct {
	// Focused: the connection was already running, so its window was
	// brought to the front instead.
	Focused bool `json:"focused"`
}

// LatencyResult is a proxy latency test's result.
type LatencyResult struct {
	Ms int64 `json:"ms"`
}

func sessionView(profileID string, s session.State) SessionView {
	v := SessionView{
		ProfileID: profileID,
		Phase:     string(s.Phase()),
		Step:      string(s.Step),
		Outcome:   string(s.Outcome),
		Addr:      s.Addr,
		PID:       s.PID,
		ExitCode:  s.ExitCode,
		Conns:     s.Conns,
		Upstream:  string(s.Upstream),
	}
	if s.Failure != nil {
		v.Failure = errorView(s.Failure.Err)
		v.FailedStep = string(s.Failure.Step)
	}
	if s.UpstreamError != "" {
		v.UpstreamError = &ErrorView{Code: s.UpstreamCode, Message: s.UpstreamError}
	}
	if s.TunnelError != "" {
		v.TunnelError = &ErrorView{Code: s.TunnelCode, Message: s.TunnelError}
	}
	if s.Check != nil {
		v.Check = checkView(*s.Check)
	}
	return v
}
