package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/loopback"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Folder names in the data folder.
const (
	ProxiesDir  = "proxies"
	ProfilesDir = "profiles"
)

// Sealer protects secrets in files. The app uses DPAPI (package secret);
// tests use a stand-in.
type Sealer interface {
	Seal(plain string) (string, error)
	Open(sealed string) (string, error)
}

// Errors from Data.
var (
	ErrNotFound = errcode.New("store.notFound", "it no longer exists")
	// ErrBuiltIn: the built-in entries (direct, and following the system
	// proxy) cannot be changed.
	ErrBuiltIn = errcode.New("store.builtIn", "the entry is built in and cannot be changed")
	// ErrWrite labels a failure to write or delete a file.
	ErrWrite = errcode.New("store.writeFailed", "the file could not be saved")

	errInUse = errcode.New("proxy.inUse", "the proxy is still in use")
)

// InUseError is returned when deleting a proxy that profiles still use and
// that were not named to move to the direct entry (DeleteProxy).
type InUseError struct {
	// Profiles are the IDs of those profiles.
	Profiles []string
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("the proxy is used by %d connection(s)", len(e.Profiles))
}

// Unwrap gives the error its code, "proxy.inUse".
func (e *InUseError) Unwrap() error { return errInUse }

// Problem codes reported when loading.
const (
	// ProblemUnreadable: the file was not valid JSON. It was renamed to
	// <name>.corrupt and left out.
	ProblemUnreadable = "store.unreadable"
	// ProblemNewer: the file was written by a newer version of the app. It
	// was left untouched and left out.
	ProblemNewer = "store.newerSchema"
	// ProblemSecretLost: the proxy's secrets could not be decrypted (the
	// file came from another Windows user or computer). It was loaded
	// without them.
	ProblemSecretLost = "store.secretLost"
	// ProblemLoopbackMoved: the profile's loopback address was missing,
	// malformed or already another profile's, so it was given a new one.
	ProblemLoopbackMoved = "store.loopbackMoved"
	// ProblemInvalid: the entry has values that cannot be used as they are.
	// It is loaded so the user can correct it.
	ProblemInvalid = "store.invalid"
	// ProblemReadFailed: the file or folder could not be read at all (access
	// denied, or held open by another program). It was left out, unchanged.
	ProblemReadFailed = "store.readFailed"
)

// Problem is something wrong with one file, found while loading.
type Problem struct {
	// File is the file's path relative to the data folder.
	File string
	Code string
	Err  error
}

// Data keeps the proxies and connection profiles: one JSON file per entry
// under proxies\ and profiles\, and the current values in memory, where
// secrets are in the clear. Its methods may be called from any goroutine.
type Data struct {
	dir    string
	sealer Sealer

	mu       sync.Mutex
	proxies  map[string]model.Proxy
	profiles map[string]model.Profile
	// lost are the proxies whose sealed values could not be opened when
	// they were loaded (SecretsLost).
	lost map[string]bool
}

// OpenData loads every proxy and profile in dataDir. Loading never
// fails as a whole: whatever cannot be used as it is comes back as a
// problem, and the rest loads.
func OpenData(dataDir string, sealer Sealer) (*Data, []Problem) {
	d := &Data{
		dir:      dataDir,
		sealer:   sealer,
		proxies:  map[string]model.Proxy{},
		profiles: map[string]model.Profile{},
		lost:     map[string]bool{},
	}
	problems := d.loadEach(ProxiesDir, func(id, rel string, data []byte) []Problem {
		p, probs := d.decodeProxy(id, rel, data)
		if p != nil {
			d.proxies[id] = *p
		}
		return probs
	})
	var loaded []model.Profile
	problems = append(problems, d.loadEach(ProfilesDir, func(id, rel string, data []byte) []Problem {
		p, prob := d.decodeProfile(id, rel, data)
		if p != nil {
			loaded = append(loaded, *p)
		}
		if prob != nil {
			return []Problem{*prob}
		}
		return nil
	})...)
	problems = append(problems, d.placeProfiles(loaded)...)
	return d, problems
}

// loadEach calls f with every <id>.json in the folder, in ID order, and
// collects the problems. It also removes the temporary files a crash in the
// middle of WriteJSONAtomic can leave behind.
func (d *Data) loadEach(folder string, f func(id, rel string, data []byte) []Problem) []Problem {
	dir := filepath.Join(d.dir, folder)
	entries, err := os.ReadDir(dir) // sorted by name
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Problem{{File: folder, Code: ProblemReadFailed, Err: err}}
	}
	var problems []Problem
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".tmp") && strings.Contains(name, ".json.") {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		id, ok := strings.CutSuffix(name, ".json")
		if _, builtIn := model.BuiltInProxy(id); !ok || !model.ValidID(id) || builtIn {
			continue // not one of ours
		}
		rel := filepath.Join(folder, name)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			problems = append(problems, Problem{File: rel, Code: ProblemReadFailed, Err: err})
			continue
		}
		problems = append(problems, f(id, rel, data)...)
	}
	return problems
}

// decode checks the file's format number and decodes it on top of v. A
// problem means the file is left out.
func (d *Data) decode(rel string, data []byte, schema int, v any) *Problem {
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return d.setAside(rel, err)
	}
	if header.Schema > schema {
		return &Problem{File: rel, Code: ProblemNewer,
			Err: fmt.Errorf("format %d is newer than this version of the app reads (%d)", header.Schema, schema)}
	}
	if err := json.Unmarshal(data, v); err != nil {
		return d.setAside(rel, err)
	}
	return nil
}

// setAside renames an unreadable file to <name>.corrupt: it is kept for the
// user but no longer loaded.
func (d *Data) setAside(rel string, err error) *Problem {
	path := filepath.Join(d.dir, rel)
	if moveErr := os.Rename(path, path+".corrupt"); moveErr != nil {
		err = errors.Join(err, moveErr)
	}
	return &Problem{File: rel, Code: ProblemUnreadable, Err: err}
}

// proxyFile is a proxy as its file holds it: Secret, Options and Outbound
// are sealed text.
type proxyFile struct {
	model.Proxy
	// Options shadows Proxy.Options: the options as sealed JSON text.
	Options string `json:"options"`
}

func (d *Data) decodeProxy(id, rel string, data []byte) (*model.Proxy, []Problem) {
	f := proxyFile{Proxy: model.Proxy{Schema: model.ProxySchema}}
	if prob := d.decode(rel, data, model.ProxySchema, &f); prob != nil {
		return nil, []Problem{*prob}
	}
	p := f.Proxy
	p.ID = id // the file name is the ID
	var problems []Problem
	secret, errSecret := d.sealer.Open(p.Secret)
	outbound, errOutbound := d.sealer.Open(p.Outbound)
	options, errOptions := d.sealer.Open(f.Options)
	if errOptions == nil && options != "" {
		errOptions = json.Unmarshal([]byte(options), &p.Options)
	}
	p.Secret, p.Outbound = secret, outbound
	if err := errors.Join(errSecret, errOutbound, errOptions); err != nil {
		// None of what was sealed is used: what a failed opening returns
		// is no secret, and the rest belongs with it.
		p.Secret, p.Outbound, p.Options = "", "", model.ProxyOptions{}
		problems = append(problems, Problem{File: rel, Code: ProblemSecretLost, Err: err})
		d.lost[id] = true
	}
	p = p.Normalize()
	if model.BuiltInKind(p.Kind) {
		return nil, nil // only the built-in entries are direct or follow the system
	}
	if err := p.Validate(); err != nil {
		problems = append(problems, Problem{File: rel, Code: ProblemInvalid, Err: err})
	}
	return &p, problems
}

func (d *Data) decodeProfile(id, rel string, data []byte) (*model.Profile, *Problem) {
	// On top of the defaults: a field added after the file was written keeps
	// its default instead of the zero value.
	p := model.DefaultProfile()
	if prob := d.decode(rel, data, model.ProfileSchema, &p); prob != nil {
		return nil, prob
	}
	p.ID = id
	p = p.Normalize()
	return &p, nil
}

// placeProfiles adds the loaded profiles, making sure each has a loopback
// address of its own. In a conflict the profile with the lower ID keeps the
// address; any other gets a new one, which is saved at once so it stays.
func (d *Data) placeProfiles(loaded []model.Profile) []Problem {
	var problems []Problem
	taken := map[netip.Addr]bool{}
	var homeless []model.Profile
	for _, p := range loaded { // in ID order
		a, err := netip.ParseAddr(p.Loopback)
		if err != nil || !loopback.ValidString(p.Loopback) || taken[a] {
			homeless = append(homeless, p)
			continue
		}
		taken[a] = true
		d.profiles[p.ID] = p
	}
	for _, p := range homeless {
		rel := filepath.Join(ProfilesDir, p.ID+".json")
		a, err := loopback.Assign(p.ID, func(a netip.Addr) bool { return taken[a] })
		if err != nil {
			problems = append(problems, Problem{File: rel, Code: ProblemInvalid, Err: err})
			continue
		}
		taken[a] = true
		old := p.Loopback
		p.Loopback = a.String()
		d.profiles[p.ID] = p
		err = fmt.Errorf("loopback address %q replaced by %s", old, p.Loopback)
		if werr := d.writeProfile(p); werr != nil {
			err = errors.Join(err, werr)
		}
		problems = append(problems, Problem{File: rel, Code: ProblemLoopbackMoved, Err: err})
	}
	for _, id := range slices.Sorted(maps.Keys(d.profiles)) {
		if err := d.profiles[id].Validate(); err != nil {
			problems = append(problems, Problem{File: filepath.Join(ProfilesDir, id+".json"), Code: ProblemInvalid, Err: err})
		}
	}
	return problems
}

// Proxies returns the stored proxies, secrets included, by name. The
// built-in direct entry is not among them.
func (d *Data) Proxies() []model.Proxy {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]model.Proxy, 0, len(d.proxies))
	for _, p := range d.proxies {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b model.Proxy) int {
		return cmp.Or(compareText(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// SecretsLost reports whether the proxy's sealed values (its secret, the
// V2Ray family's settings, a custom outbound) could not be opened when it
// was loaded, because they came from another Windows user or computer. What
// is left are defaults, which are no way to reach the server: the user's
// ID would go out without the TLS it was meant to travel in. It stays so
// until the proxy is saved again.
func (d *Data) SecretsLost(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lost[id]
}

// Proxy returns the proxy with the given ID; model.DirectProxyID and
// model.SystemProxyID give the built-in entries.
func (d *Data) Proxy(id string) (model.Proxy, bool) {
	if p, ok := model.BuiltInProxy(id); ok {
		return p, true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.proxies[id]
	return p, ok
}

// Profiles returns the profiles by group, then name.
func (d *Data) Profiles() []model.Profile {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]model.Profile, 0, len(d.profiles))
	for _, p := range d.profiles {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b model.Profile) int {
		return cmp.Or(compareText(a.Group, b.Group), compareText(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// Profile returns the profile with the given ID.
func (d *Data) Profile(id string) (model.Profile, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.profiles[id]
	return p, ok
}

// CreateProxy stores a new proxy and returns it with its new ID.
func (d *Data) CreateProxy(p model.Proxy) (model.Proxy, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p.ID = newID(func(id string) bool { _, ok := d.proxies[id]; return ok })
	return d.saveProxy(p)
}

// UpdateProxy replaces a stored proxy. Sessions already using it keep the
// settings they started with (see package engine).
func (d *Data) UpdateProxy(p model.Proxy) (model.Proxy, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, builtIn := model.BuiltInProxy(p.ID); builtIn {
		return model.Proxy{}, ErrBuiltIn
	}
	if _, ok := d.proxies[p.ID]; !ok {
		return model.Proxy{}, ErrNotFound
	}
	return d.saveProxy(p)
}

func (d *Data) saveProxy(p model.Proxy) (model.Proxy, error) {
	p = p.Normalize()
	if model.BuiltInKind(p.Kind) {
		return model.Proxy{}, model.FieldErrors{{Field: "kind", Code: model.CodeUnsupported}}
	}
	if err := p.Validate(); err != nil {
		return model.Proxy{}, err
	}
	onDisk := proxyFile{Proxy: p}
	var err error
	if onDisk.Secret, err = d.sealer.Seal(p.Secret); err != nil {
		return model.Proxy{}, err
	}
	if onDisk.Outbound, err = d.sealer.Seal(p.Outbound); err != nil {
		return model.Proxy{}, err
	}
	if p.Options != (model.ProxyOptions{}) {
		options, err := json.Marshal(p.Options)
		if err != nil {
			return model.Proxy{}, err
		}
		if onDisk.Options, err = d.sealer.Seal(string(options)); err != nil {
			return model.Proxy{}, err
		}
	}
	if err := writeFile(filepath.Join(d.dir, ProxiesDir, p.ID+".json"), onDisk); err != nil {
		return model.Proxy{}, err
	}
	d.proxies[p.ID] = p
	delete(d.lost, p.ID)
	return p, nil
}

// DeleteProxy removes a proxy. The profiles that use it switch to the
// built-in direct entry, but only those named in moveToDirect, the ones the
// user agreed to: any other user leaves everything as it was, and the error
// is an *InUseError naming them. It returns the IDs of the profiles that
// switched, sorted, which may be some of them when saving one fails (the
// proxy then stays, so no profile is left without its proxy).
func (d *Data) DeleteProxy(id string, moveToDirect []string) (moved []string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, builtIn := model.BuiltInProxy(id); builtIn {
		return nil, ErrBuiltIn
	}
	if _, ok := d.proxies[id]; !ok {
		return nil, ErrNotFound
	}
	var users, unasked []string
	for _, p := range d.profiles {
		if p.ProxyID == id {
			users = append(users, p.ID)
			if !slices.Contains(moveToDirect, p.ID) {
				unasked = append(unasked, p.ID)
			}
		}
	}
	if len(unasked) > 0 {
		slices.Sort(unasked)
		return nil, &InUseError{Profiles: unasked}
	}
	slices.Sort(users)
	for _, pid := range users {
		// Written as it is: a problem elsewhere in the profile (a file
		// edited by hand) is no reason to keep the proxy.
		p := d.profiles[pid]
		p.ProxyID = model.DirectProxyID
		if err := d.writeProfile(p); err != nil {
			return moved, err
		}
		d.profiles[pid] = p
		moved = append(moved, pid)
	}
	if err := removeFile(filepath.Join(d.dir, ProxiesDir, id+".json")); err != nil {
		return moved, err
	}
	delete(d.proxies, id)
	delete(d.lost, id)
	return moved, nil
}

// CreateProfile stores a new profile and returns it with its new ID and
// loopback address. Any ID or address in p is ignored.
func (d *Data) CreateProfile(p model.Profile) (model.Profile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p.ID = newID(func(id string) bool { _, ok := d.profiles[id]; return ok })
	taken := map[netip.Addr]bool{}
	for _, other := range d.profiles {
		if a, err := netip.ParseAddr(other.Loopback); err == nil {
			taken[a] = true
		}
	}
	a, err := loopback.Assign(p.ID, func(a netip.Addr) bool { return taken[a] })
	if err != nil {
		return model.Profile{}, err
	}
	p.Loopback = a.String()
	return d.saveProfile(p)
}

// UpdateProfile replaces a stored profile and returns it as stored, along
// with the version it replaced. The loopback address never changes: it is
// the profile's identity for mstsc's saved passwords and certificate trust.
// The proxy stays too: SetProfileProxy changes it, so an editor that knows
// nothing of it cannot set it back.
func (d *Data) UpdateProfile(p model.Profile) (stored, previous model.Profile, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	previous, ok := d.profiles[p.ID]
	if !ok {
		return model.Profile{}, model.Profile{}, ErrNotFound
	}
	p.Loopback, p.ProxyID = previous.Loopback, previous.ProxyID
	stored, err = d.saveProfile(p)
	return stored, previous, err
}

// SetProfileProxy points a profile at another proxy, or at the built-in
// direct entry, and returns the profile as stored. Only the proxy changes,
// so a problem elsewhere in the profile (a file edited by hand) does not
// stand in the way.
func (d *Data) SetProfileProxy(id, proxyID string) (model.Profile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.profiles[id]
	if !ok || !d.hasProxy(proxyID) {
		return model.Profile{}, ErrNotFound
	}
	p.ProxyID = proxyID
	if err := d.writeProfile(p); err != nil {
		return model.Profile{}, err
	}
	d.profiles[id] = p
	return p, nil
}

func (d *Data) saveProfile(p model.Profile) (model.Profile, error) {
	p = p.Normalize()
	err := p.Validate()
	var fields model.FieldErrors
	if err != nil && !errors.As(err, &fields) {
		return model.Profile{}, err
	}
	// A proxy has to exist when it is chosen. A profile keeps the one it has
	// even when that is gone (its file could not be loaded); the list says so.
	old, existed := d.profiles[p.ID]
	chosen := !existed || old.ProxyID != p.ProxyID
	if chosen && model.ValidID(p.ProxyID) && !d.hasProxy(p.ProxyID) {
		fields = append(fields, model.FieldError{Field: "proxyId", Code: model.CodeInvalid})
	}
	if len(fields) > 0 {
		return model.Profile{}, fields
	}
	if err := d.writeProfile(p); err != nil {
		return model.Profile{}, err
	}
	d.profiles[p.ID] = p
	return p, nil
}

// hasProxy reports whether id is a stored proxy or a built-in entry. The
// caller holds d.mu.
func (d *Data) hasProxy(id string) bool {
	_, ok := d.proxies[id]
	_, builtIn := model.BuiltInProxy(id)
	return ok || builtIn
}

func (d *Data) writeProfile(p model.Profile) error {
	return writeFile(filepath.Join(d.dir, ProfilesDir, p.ID+".json"), p)
}

// DeleteProfile removes a profile and returns what it was.
func (d *Data) DeleteProfile(id string) (model.Profile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.profiles[id]
	if !ok {
		return model.Profile{}, ErrNotFound
	}
	if err := removeFile(filepath.Join(d.dir, ProfilesDir, id+".json")); err != nil {
		return model.Profile{}, err
	}
	delete(d.profiles, id)
	return p, nil
}

func writeFile(path string, v any) error {
	if err := WriteJSONAtomic(path, v); err != nil {
		return fmt.Errorf("%w: %w", ErrWrite, err)
	}
	return nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrWrite, err)
	}
	return nil
}

// newID returns a new random ID that exists does not report as taken.
func newID(exists func(string) bool) string {
	for {
		if id := model.NewID(); !exists(id) {
			if _, builtIn := model.BuiltInProxy(id); !builtIn {
				return id
			}
		}
	}
}

// compareText orders names for people: without regard to case first.
func compareText(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}
