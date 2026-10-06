//go:build windows

package sysproxy

import (
	"context"
	"errors"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// System is the real Windows: the current user's Internet settings and
// WinHTTP's automatic proxy configuration.
type System struct{}

// errAutodetectionFailed is ERROR_WINHTTP_AUTODETECTION_FAILED.
const errAutodetectionFailed = windows.Errno(12180)

// The codes of WinHTTP's errors about the automatic configuration.
var configCodes = map[windows.Errno]string{
	12166:                  CodeScriptFailed,      // ERROR_WINHTTP_BAD_AUTO_PROXY_SCRIPT
	12167:                  CodeScriptUnavailable, // ERROR_WINHTTP_UNABLE_TO_DOWNLOAD_SCRIPT
	12178:                  CodeConfigService,     // ERROR_WINHTTP_AUTO_PROXY_SERVICE_ERROR
	errAutodetectionFailed: CodeNotDetected,
}

// Settings implements Windows.
func (System) Settings() (Settings, error) {
	c, err := winx.IEProxyConfig()
	if err != nil {
		return Settings{}, err
	}
	return Settings{AutoDetect: c.AutoDetect, Script: c.Script, Proxy: c.Proxy, Bypass: c.Bypass}, nil
}

// Configure implements Windows.
func (System) Configure(ctx context.Context, url string, autoDetect bool, script string) ([]Entry, error) {
	list, err := winx.ProxyForURL(ctx, url, autoDetect, script)
	if err != nil {
		var errno windows.Errno
		switch {
		case errors.Is(err, errAutodetectionFailed) && script == "":
			return nil, ErrNothingDetected
		case errors.As(err, &errno) && configCodes[errno] != "":
			return nil, errcode.Wrap(configCodes[errno], err)
		}
		return nil, err
	}
	entries := make([]Entry, len(list))
	for i, e := range list {
		entries[i] = Entry{Direct: e.Direct, Scheme: e.Scheme, Host: e.Host, Port: e.Port}
	}
	return entries, nil
}

// Watch calls changed whenever the current user's Internet settings
// change, until stop is called: they are in the registry, which tells.
func Watch(changed func()) (stop func(), err error) {
	return winx.WatchKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, changed)
}
