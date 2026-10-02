package logging

import (
	"cmp"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Placeholders that replace personal data in the log file.
const (
	maskName = "<redacted>"
	maskIP   = "<ip>"
)

// Redactor masks personal data in log text:
//   - folders, such as the user's profile folder, whose path contains the
//     Windows account name (AddPath);
//   - the names the app knows about: host names, servers, user names,
//     connection and proxy names, proxy passwords (Add);
//   - every IP address except loopback and unspecified ones, which say
//     nothing about the user and help with tunnel problems.
//
// Its methods may be called from any goroutine.
type Redactor struct {
	mu    sync.RWMutex
	seen  map[string]bool
	known []string // longest first, so a name is masked before its parts
	paths []pathMask
}

type pathMask struct{ path, placeholder string }

// Add masks values from now on. The set only grows: a session that is still
// running may use a host name the data no longer has, and masking a name
// too many is harmless. Values shorter than three characters are skipped;
// masking "a" would garble everything.
func (r *Redactor) Add(values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	changed := false
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < 3 || r.seen[strings.ToLower(v)] {
			continue
		}
		r.seen[strings.ToLower(v)] = true
		r.known = append(r.known, v)
		changed = true
	}
	if changed {
		slices.SortStableFunc(r.known, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	}
}

// AddPath replaces path, wherever it appears and whatever its case, with
// placeholder, such as the user's profile folder with "%USERPROFILE%".
func (r *Redactor) AddPath(path, placeholder string) {
	path = strings.TrimRight(path, `\/`)
	if path == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, pathMask{path, placeholder})
	slices.SortStableFunc(r.paths, func(a, b pathMask) int { return cmp.Compare(len(b.path), len(a.path)) })
}

var (
	ipv4 = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	// Candidates only; each is checked with netip, so times such as
	// "21:04:05" are left alone.
	ipv6 = regexp.MustCompile(`(?i)[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}(?:%[0-9a-z]+)?`)
)

// Text returns s with personal data masked.
func (r *Redactor) Text(s string) string {
	if s == "" {
		return s
	}
	r.mu.RLock()
	paths, known := r.paths, r.known
	r.mu.RUnlock()
	for _, p := range paths {
		s = replaceFold(s, p.path, p.placeholder)
	}
	for _, k := range known {
		s = replaceFold(s, k, maskName)
	}
	s = ipv4.ReplaceAllStringFunc(s, maskAddr)
	return ipv6.ReplaceAllStringFunc(s, maskAddr)
}

// maskAddr masks m if it is an IP address that could identify someone.
func maskAddr(m string) string {
	a, err := netip.ParseAddr(m)
	if err != nil || a.Unmap().IsLoopback() || a.IsUnspecified() {
		return m
	}
	return maskIP
}

// replaceFold replaces every occurrence of old in s, ignoring case: host
// names and Windows paths are case-insensitive.
func replaceFold(s, old, repl string) string {
	lower, lowerOld := strings.ToLower(s), strings.ToLower(old)
	if len(lower) != len(s) || len(lowerOld) != len(old) {
		return strings.ReplaceAll(s, old, repl) // a case change altered the length
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, lowerOld)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(repl)
		s, lower = s[i+len(old):], lower[i+len(old):]
	}
}
