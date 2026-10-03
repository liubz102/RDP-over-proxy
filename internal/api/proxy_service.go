package api

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/sharelink"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// ProxyService manages the proxies.
type ProxyService struct{ c *Core }

// NewProxyService returns the service.
func NewProxyService(c *Core) *ProxyService { return &ProxyService{c: c} }

// List returns the built-in direct entry first, then every proxy by name.
// Passwords are left out.
func (s *ProxyService) List() []ProxyView { return s.c.dataView().Proxies }

// draftID stands in for the ID of a proxy that is not saved yet.
const draftID = "draft"

// Get returns one proxy for editing, without its secret (see HasSecret in
// List). Its options and custom outbound come along: the editor shows them.
func (s *ProxyService) Get(id string) (model.Proxy, error) {
	p, ok := s.c.d.Data.Proxy(id)
	if !ok {
		return model.Proxy{}, store.ErrNotFound
	}
	p.Secret = ""
	return p, nil
}

// Create stores a new proxy, once Xray has accepted its settings.
func (s *ProxyService) Create(p model.Proxy) (ProxyView, error) {
	if err := s.check(p); err != nil {
		return ProxyView{}, err
	}
	stored, err := s.c.d.Data.CreateProxy(p)
	if err != nil {
		return ProxyView{}, err
	}
	s.c.dataChanged()
	return s.c.proxyView(stored, 0), nil
}

// Update stores an edited proxy, once Xray has accepted its settings. With
// keepSecret the stored secret stays and p.Secret is ignored, so the form
// does not need to know it; a proxy that changes its kind keeps none.
// Sessions already using the proxy keep the settings they started with.
func (s *ProxyService) Update(p model.Proxy, keepSecret bool) (ProxyView, error) {
	if keepSecret {
		var err error
		if p, err = s.withStoredSecret(p); err != nil {
			return ProxyView{}, err
		}
	}
	if err := s.check(p); err != nil {
		return ProxyView{}, err
	}
	stored, err := s.c.d.Data.UpdateProxy(p)
	if err != nil {
		return ProxyView{}, err
	}
	s.c.dataChanged()
	return s.c.proxyView(stored, s.usedBy(stored.ID)), nil
}

// Delete removes a proxy. One that connections still use is not removed;
// the error's args name them ("profiles").
func (s *ProxyService) Delete(id string) error {
	err := s.c.d.Data.DeleteProxy(id)
	var inUse *store.InUseError
	if errors.As(err, &inUse) {
		var names []string
		for _, pid := range inUse.Profiles {
			if p, ok := s.c.d.Data.Profile(pid); ok {
				names = append(names, p.Name)
			}
		}
		slices.Sort(names)
		return errcode.WithArgs(err, map[string]string{"profiles": strings.Join(names, ", ")})
	}
	if err != nil {
		return err
	}
	s.c.dataChanged()
	return nil
}

// ParseLink reads a share link into a proxy for the editor to fill in;
// nothing is saved. The notes say what the link asks for that the proxy does
// without.
func (s *ProxyService) ParseLink(link string) (LinkView, error) {
	p, notes, err := sharelink.Parse(link)
	if err != nil {
		return LinkView{}, err
	}
	if notes == nil {
		notes = []sharelink.Note{}
	}
	return LinkView{Proxy: p, Notes: notes}, nil
}

// ShareLink writes a stored proxy as a share link, credentials included, for
// the user to copy.
func (s *ProxyService) ShareLink(id string) (string, error) {
	if id == model.DirectProxyID {
		return "", store.ErrBuiltIn
	}
	p, ok := s.c.d.Data.Proxy(id)
	if !ok {
		return "", store.ErrNotFound
	}
	return sharelink.Format(p)
}

// Latency fetches the test URL (Settings.TestURL) through the proxy and
// reports how long it took. It waits as long as the route takes; the
// frontend cancels the call to stop it.
func (s *ProxyService) Latency(ctx context.Context, id string) (LatencyResult, error) {
	p, ok := s.c.d.Data.Proxy(id)
	if !ok {
		return LatencyResult{}, store.ErrNotFound
	}
	if s.c.d.Data.SecretsLost(id) {
		return LatencyResult{}, ErrSecretsLost
	}
	return s.latency(ctx, p)
}

// DraftLatency is Latency through settings that are not saved, such as a
// link just pasted into the editor. With keepSecret the stored proxy's
// secret is used (p.ID names it), as Update would. The settings are checked
// as Create and Update check them.
func (s *ProxyService) DraftLatency(ctx context.Context, p model.Proxy, keepSecret bool) (LatencyResult, error) {
	if keepSecret {
		var err error
		if p, err = s.withStoredSecret(p); err != nil {
			return LatencyResult{}, err
		}
	}
	if p.ID == "" {
		p.ID = draftID
	}
	p = p.Normalize()
	if err := s.check(p); err != nil {
		return LatencyResult{}, err
	}
	return s.latency(ctx, p)
}

func (s *ProxyService) latency(ctx context.Context, p model.Proxy) (LatencyResult, error) {
	d, release, err := s.c.d.Routes.Acquire(p)
	if err != nil {
		return LatencyResult{}, err
	}
	defer release()
	elapsed, err := probe.Latency(ctx, d, s.c.d.Settings.Get().TestURL)
	if err != nil {
		return LatencyResult{}, err
	}
	return LatencyResult{Ms: elapsed.Milliseconds()}, nil
}

// withStoredSecret gives p the stored proxy's secret. A secret belongs to
// its kind: a VMess user ID is no Trojan password. SOCKS5 and HTTP accounts
// are alike, so their passwords go along.
func (s *ProxyService) withStoredSecret(p model.Proxy) (model.Proxy, error) {
	old, ok := s.c.d.Data.Proxy(p.ID)
	if !ok {
		return p, store.ErrNotFound
	}
	p.Secret = ""
	if sameSecret(old.Kind, p.Kind) {
		p.Secret = old.Secret
	}
	return p, nil
}

// sameSecret reports whether a secret of one kind means the same to the
// other.
func sameSecret(a, b string) bool {
	account := func(k string) bool { return k == model.KindSocks || k == model.KindHTTP }
	return a == b || account(a) && account(b)
}

// check validates p, then asks Xray whether it accepts the settings
// (Deps.CheckProxy), so the editor shows the problems of each field first
// and what Xray says about the rest.
func (s *ProxyService) check(p model.Proxy) error {
	p = p.Normalize()
	if p.ID == "" {
		p.ID = draftID // Create gives it one later
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if s.c.d.CheckProxy != nil && p.Kind != model.KindDirect {
		return s.c.d.CheckProxy(p)
	}
	return nil
}

func (s *ProxyService) usedBy(id string) int {
	n := 0
	for _, p := range s.c.d.Data.Profiles() {
		if p.ProxyID == id {
			n++
		}
	}
	return n
}
