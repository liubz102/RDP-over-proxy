package api

import (
	"unicode/utf16"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/secret"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// ProfileService manages the connection profiles and their saved passwords.
type ProfileService struct{ c *Core }

// NewProfileService returns the service.
func NewProfileService(c *Core) *ProfileService { return &ProfileService{c: c} }

// List returns every profile, by group and name.
func (s *ProfileService) List() []ProfileView { return s.c.dataView().Profiles }

// Draft returns the starting values for a new profile. ID, Name, Target and
// ProxyID are for the user to fill in.
func (s *ProfileService) Draft() model.Profile { return model.DefaultProfile() }

// Create stores a new profile. When password is set and the profile
// remembers passwords, it is saved in Windows Credential Manager. A
// password that cannot be saved does not undo the profile; a notice says
// so.
func (s *ProfileService) Create(p model.Profile, password string) (ProfileView, error) {
	if err := checkPassword(password); err != nil {
		return ProfileView{}, err
	}
	stored, err := s.c.d.Data.CreateProfile(p)
	if err != nil {
		return ProfileView{}, err
	}
	if password != "" && stored.RememberPassword {
		s.savePassword(stored, password)
	}
	s.c.dataChanged()
	return s.c.profileView(stored, true), nil
}

// Update stores the edited profile. Its ID and loopback address stay.
//
//   - Changing the target computer (host or port) deletes the saved
//     passwords: they belong to the old computer, and mstsc would offer them
//     to the new one.
//   - A new password replaces the saved one; an empty password keeps it.
//   - Turning "remember password" off deletes the password the app saved.
//   - A new user name moves the app's saved password to that name.
func (s *ProfileService) Update(p model.Profile, password string) (ProfileView, error) {
	if err := checkPassword(password); err != nil {
		return ProfileView{}, err
	}
	stored, previous, err := s.c.d.Data.UpdateProfile(p)
	if err != nil {
		return ProfileView{}, err
	}
	server := s.c.server(stored)
	switch {
	case stored.Target != previous.Target:
		s.cleanup(NoticeDeleteFailed, stored, s.c.creds.deleteAll(server))
	case !stored.RememberPassword:
		s.cleanup(NoticeDeleteFailed, stored, s.c.creds.deleteRemembered(server))
	}
	switch {
	case password != "" && stored.RememberPassword:
		s.savePassword(stored, password)
	case stored.RememberPassword && stored.Username != previous.Username && stored.Target == previous.Target:
		if err := s.c.creds.rename(server, stored.Username); err != nil {
			s.c.Notify(logging.LevelError, NoticeSaveFailed, map[string]string{"profile": stored.Name}, err)
		}
	}
	s.c.dataChanged()
	_, proxyOK := s.c.d.Data.Proxy(stored.ProxyID)
	return s.c.profileView(stored, proxyOK), nil
}

// Delete removes a profile, its saved passwords and what mstsc remembers
// about its address. A connected profile cannot be deleted.
func (s *ProfileService) Delete(id string) error {
	s.c.lifecycle.Lock()
	if s.c.manager.Active(id) {
		s.c.lifecycle.Unlock()
		return ErrSessionRunning
	}
	p, err := s.c.d.Data.DeleteProfile(id)
	s.c.lifecycle.Unlock()
	if err != nil {
		return err
	}
	s.cleanup(NoticeDeleteFailed, p, s.c.creds.deleteAll(s.c.server(p)))
	s.cleanup(NoticeForgetFailed, p, s.c.d.Servers.Forget(p.Loopback))
	s.c.mu.Lock()
	delete(s.c.logs, id)
	s.c.mu.Unlock()
	s.c.dataChanged()
	return nil
}

// ForgetPassword deletes every saved password of the profile: the app's
// and the one mstsc remembered.
func (s *ProfileService) ForgetPassword(id string) error {
	p, ok := s.c.d.Data.Profile(id)
	if !ok {
		return store.ErrNotFound
	}
	if err := s.c.creds.deleteAll(s.c.server(p)); err != nil {
		return err
	}
	s.c.dataChanged()
	return nil
}

func (s *ProfileService) savePassword(p model.Profile, password string) {
	if err := s.c.creds.remember(s.c.server(p), p.Username, password); err != nil {
		s.c.Notify(logging.LevelError, NoticeSaveFailed, map[string]string{"profile": p.Name}, err)
	}
}

// cleanup reports a failed clean-up as a notice; the change itself stands.
func (s *ProfileService) cleanup(code string, p model.Profile, err error) {
	if err != nil {
		s.c.Notify(logging.LevelWarn, code, map[string]string{"profile": p.Name}, err)
	}
}

// checkPassword rejects a password Credential Manager cannot hold.
func checkPassword(password string) error {
	if len(utf16.Encode([]rune(password))) > secret.MaxPasswordLen {
		return model.FieldErrors{{Field: "password", Code: model.CodeTooLong}}
	}
	return nil
}
