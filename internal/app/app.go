// Package app wires the Wails application together: the main window, the tray
// icon, single-instance handling and what closing the window does.
package app

import (
	"io/fs"
	"path/filepath"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/liubz102/RDP-over-proxy/internal/api"
	"github.com/liubz102/RDP-over-proxy/internal/i18n"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

const (
	productName = "RDP over Proxy"
	description = "Windows Remote Desktop (mstsc) through SOCKS5, HTTP and V2Ray-family proxies"
	// uniqueID identifies the single running instance.
	uniqueID = "io.github.liubz102.rdp-over-proxy"
	repoURL  = "https://github.com/liubz102/RDP-over-proxy"
)

// Options are the inputs main provides.
type Options struct {
	Version string
	// Assets is the built frontend (frontend/dist, embedded by main).
	Assets fs.FS
	// Icon is a PNG used for the tray icon.
	Icon []byte
}

// Run starts the application and blocks until it quits.
func Run(opts Options) error {
	dirs, err := store.DefaultDirs()
	if err != nil {
		reportStartupError(i18n.Detect(), err)
		return err
	}
	dirs = buildDirs(dirs)
	settings := store.NewSettingsStore(dirs.Config)
	current, loadErr := settings.Load()

	if !preflight(uiLanguage(current)) {
		return nil
	}

	sh := &shell{settings: settings}
	settingsSvc := api.NewSettingsService(settings,
		api.AppInfo{Name: productName, Version: opts.Version, Repo: repoURL},
		i18n.Detect, sh.settingsChanged)

	sh.app = application.New(application.Options{
		Name:        productName,
		Description: description,
		Services: []application.Service{
			application.NewService(settingsSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
		Windows: application.WindowsOptions{
			// Keep the WebView2 profile with our other machine-local data
			// instead of next to the exe.
			WebviewUserDataPath: filepath.Join(dirs.Local, "WebView2"),
		},
		SingleInstance: singleInstance(sh.showWindow),
	})
	if loadErr != nil {
		sh.app.Logger.Warn("settings.json could not be read; defaults are in use", "error", loadErr)
	}

	sh.createWindow(current)
	sh.createTray(opts.Icon, current)
	if err := sh.app.Run(); err != nil {
		reportStartupError(uiLanguage(settings.Get()), err)
		return err
	}
	return nil
}

// uiLanguage is the language for strings the Go side shows itself. Before the
// user has picked one, it follows the Windows display language.
func uiLanguage(s model.Settings) string {
	if s.Language != "" {
		return s.Language
	}
	return i18n.Detect()
}

// shell owns the native parts of the UI.
type shell struct {
	app      *application.App
	settings *store.SettingsStore
	window   *application.WebviewWindow

	tray     *application.SystemTray
	menu     *application.Menu
	showItem *application.MenuItem
	quitItem *application.MenuItem

	// quitting is set once the user asked to quit, so the close hook lets the
	// window go instead of hiding it.
	quitting atomic.Bool
}

func (s *shell) createWindow(current model.Settings) {
	s.window = s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     productName,
		Width:     1040,
		Height:    680,
		MinWidth:  900,
		MinHeight: 600,
		URL:       "/",
		// Painted before the page loads; matching the theme avoids a white
		// flash in dark mode.
		BackgroundColour: windowBackground(current.Theme),
	})
	s.window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if s.quitting.Load() {
			return
		}
		if s.settings.Get().CloseBehavior == model.CloseToTray {
			s.window.Hide()
			e.Cancel()
		}
		// Otherwise the window closes; it is the only one, so the app quits.
	})
}

func (s *shell) createTray(icon []byte, current model.Settings) {
	lang := uiLanguage(current)
	s.menu = s.app.NewMenu()
	s.showItem = s.menu.Add(i18n.T(lang, "tray.show")).OnClick(func(*application.Context) { s.showWindow() })
	s.menu.AddSeparator()
	s.quitItem = s.menu.Add(i18n.T(lang, "tray.quit")).OnClick(func(*application.Context) { s.quit() })

	s.tray = s.app.SystemTray.New()
	s.tray.SetIcon(icon)
	s.tray.SetTooltip(productName)
	s.tray.SetMenu(s.menu)
	// A left click opens the window. In Wails v3 beta the right-click menu may
	// not open on Windows (wailsapp/wails#6161); the window has every function.
	s.tray.OnClick(s.showWindow)
}

// settingsChanged re-labels the tray menu when the language changes.
func (s *shell) settingsChanged(v model.Settings) {
	if s.menu == nil {
		return
	}
	lang := uiLanguage(v)
	s.showItem.SetLabel(i18n.T(lang, "tray.show"))
	s.quitItem.SetLabel(i18n.T(lang, "tray.quit"))
	// The Windows tray builds its popup menu from the Go menu in SetMenu, so
	// it has to be handed the menu again to pick up the new labels.
	s.tray.SetMenu(s.menu)
}

func (s *shell) showWindow() {
	if s.window == nil {
		return
	}
	if s.window.IsMinimised() {
		s.window.Restore()
	}
	s.window.Show()
	s.window.Focus()
}

func (s *shell) quit() {
	s.quitting.Store(true)
	s.app.Quit()
}
