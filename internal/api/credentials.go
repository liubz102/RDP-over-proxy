package api

import (
	"sync"

	"github.com/liubz102/RDP-over-proxy/internal/secret"
)

// credStore is the app's only way to the vault. A session ending, a session
// starting and a profile being edited can all touch the same server's
// password at once, and several rules need a look and a change to happen
// together, so every operation holds one lock.
type credStore struct {
	mu sync.Mutex
	v  Vault
}

func (s *credStore) lookup(server string) (secret.Saved, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.Lookup(server)
}

// remember stores the password the user chose to keep.
func (s *credStore) remember(server, user, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.Save(server, user, password, false)
}

// saveOneTime stores a password for one session. A password the app
// remembers for the server is never replaced by it: the user saved that one
// last, and a one-time password is deleted when its session ends. saved
// reports whether the one-time password was stored (and must be deleted).
func (s *credStore) saveOneTime(server, user, password string) (saved bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.v.Lookup(server)
	if err != nil {
		return false, err
	}
	if cur.Ours && !cur.OneTime {
		return false, nil
	}
	if err := s.v.Save(server, user, password, true); err != nil {
		return false, err
	}
	return true, nil
}

// rename moves the app's remembered password to a new user name.
func (s *credStore) rename(server, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.v.Lookup(server)
	if err != nil || !cur.Ours || cur.OneTime {
		return err
	}
	_, password, found, err := s.v.Password(server)
	if err != nil || !found {
		return err
	}
	return s.v.Save(server, user, password, false)
}

// deleteAll removes every password saved for the server, mstsc's included.
func (s *credStore) deleteAll(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.Delete(server)
}

func (s *credStore) deleteOneTime(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.DeleteOneTime(server)
}

func (s *credStore) deleteRemembered(server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.DeleteRemembered(server)
}
