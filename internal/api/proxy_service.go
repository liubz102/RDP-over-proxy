package api

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// ProxyService manages the proxies.
type ProxyService struct{ c *Core }

// NewProxyService returns the service.
func NewProxyService(c *Core) *ProxyService { return &ProxyService{c: c} }

// List returns the built-in direct entry first, then every proxy by name.
// Passwords are left out.
func (s *ProxyService) List() []ProxyView { return s.c.dataView().Proxies }

// Get returns one proxy for editing, without its password (see HasSecret
// in List).
func (s *ProxyService) Get(id string) (model.Proxy, error) {
	p, ok := s.c.d.Data.Proxy(id)
	if !ok {
		return model.Proxy{}, store.ErrNotFound
	}
	p.Secret = ""
	return p, nil
}

// Create stores a new proxy.
func (s *ProxyService) Create(p model.Proxy) (ProxyView, error) {
	stored, err := s.c.d.Data.CreateProxy(p)
	if err != nil {
		return ProxyView{}, err
	}
	s.c.dataChanged()
	return proxyView(stored, 0), nil
}

// Update stores an edited proxy. With keepSecret the stored password stays
// and p.Secret is ignored, so the form does not need to know it. Sessions
// already using the proxy keep the settings they started with.
func (s *ProxyService) Update(p model.Proxy, keepSecret bool) (ProxyView, error) {
	if keepSecret {
		old, ok := s.c.d.Data.Proxy(p.ID)
		if !ok {
			return ProxyView{}, store.ErrNotFound
		}
		p.Secret = old.Secret
	}
	stored, err := s.c.d.Data.UpdateProxy(p)
	if err != nil {
		return ProxyView{}, err
	}
	s.c.dataChanged()
	return proxyView(stored, s.usedBy(stored.ID)), nil
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

// Latency fetches the test URL (Settings.TestURL) through the proxy and
// reports how long it took. It waits as long as the route takes; the
// frontend cancels the call to stop it.
func (s *ProxyService) Latency(ctx context.Context, id string) (LatencyResult, error) {
	p, ok := s.c.d.Data.Proxy(id)
	if !ok {
		return LatencyResult{}, store.ErrNotFound
	}
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

func (s *ProxyService) usedBy(id string) int {
	n := 0
	for _, p := range s.c.d.Data.Profiles() {
		if p.ProxyID == id {
			n++
		}
	}
	return n
}
