//go:build windows

package app

import (
	"github.com/liubz102/RDP-over-proxy/internal/api"
	"github.com/liubz102/RDP-over-proxy/internal/diag"
	"github.com/liubz102/RDP-over-proxy/internal/engine"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/secret"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// The parts of Windows the app keeps its data in and starts mstsc with.
// Both the desktop build and the browser-preview build use them.

func sealer() store.Sealer { return secret.DPAPI{} }

func vault() api.Vault { return secret.Vault{} }

func servers() api.Servers { return mstsc.Servers{Key: mstsc.ServersKey} }

func launchMstsc(args []string) (session.Process, error) {
	p, err := mstsc.Launch(args)
	if err != nil {
		return nil, err // not a nil *mstsc.Process inside a non-nil interface
	}
	return p, nil
}

func readDefaults() (mstsc.Defaults, error) { return mstsc.ReadDefaults() }

func editDefaults() error { return mstsc.EditDefaults() }

// diagnose gathers the facts of the environment report.
func diagnose(version, logDir string) func() diag.Facts {
	return func() diag.Facts {
		return diag.Gather(diag.Options{App: version, Xray: engine.XrayVersion(), Logs: logDir})
	}
}

func credentialGuard() (bool, error) { return winx.CredentialGuardRunning() }

func openFolder(path string) func() error {
	return func() error { return winx.OpenFolder(path) }
}
