//go:build server || !windows

package app

import (
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// The server build (-tags server) serves the UI over HTTP so it can be checked
// in a browser. Wails does not support single-instance mode there, and no
// native window is shown, so these hooks do nothing.

// buildDirs keeps the browser preview's data apart from the real app's, unless
// RDP_OVER_PROXY_HOME already points somewhere else.
func buildDirs(d store.Dirs) store.Dirs {
	if os.Getenv(store.EnvHome) != "" {
		return d
	}
	return store.Dirs{Config: d.Config + "-preview", Local: d.Local + "-preview"}
}

func singleInstance(func()) *application.SingleInstanceOptions { return nil }

func preflight(string) bool { return true }

func reportStartupError(_ string, err error) { log.Printf("startup failed: %v", err) }

func windowBackground(theme string) application.RGBA {
	if theme == model.ThemeDark {
		return application.NewRGB(41, 41, 41)
	}
	return application.NewRGB(250, 250, 250)
}
