// Package app wires the Wails application together: the main window, the tray
// icon, single-instance handling and what closing the window does, and the
// parts behind the services: data, log, proxy engine and sessions.
package app

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/liubz102/RDP-over-proxy/internal/api"
	"github.com/liubz102/RDP-over-proxy/internal/engine"
	"github.com/liubz102/RDP-over-proxy/internal/i18n"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
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

// Log file limits: app.log plus two older files of at most 2 MiB each.
const (
	logMaxBytes = 2 << 20
	logKeep     = 2
	// logRecent is how many lines the app's own log view can show.
	logRecent = 1000
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
	// Read only: until the single-instance check below, another instance may
	// own these files.
	early := settings.Peek()

	if !preflight(uiLanguage(early)) {
		return nil
	}

	// Lines stay in memory until this is known to be the only instance; then
	// they go to the log file as well. Paths in error messages name the
	// Windows account, so the profile folder is masked in the file.
	logger := logging.New(nil, early.LogLevel, logRecent)
	if home, err := os.UserHomeDir(); err == nil {
		logger.Redactor().AddPath(home, "%USERPROFILE%")
	}
	logger.Infof("%s %s starting", productName, opts.Version)

	// Creating the application settles which instance this is. A second one
	// brings the first one's window forward and exits inside
	// application.New, before it has touched the log file, the data or
	// Credential Manager, which the first instance's sessions rely on.
	sh := &shell{settings: settings, logger: logger}
	var stop func()
	sh.app = application.New(application.Options{
		Name:        productName,
		Description: description,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(opts.Assets),
		},
		Windows: application.WindowsOptions{
			// Keep the WebView2 profile with our other machine-local data
			// instead of next to the exe.
			WebviewUserDataPath: filepath.Join(dirs.Local, "WebView2"),
		},
		Logger:         slog.New(logging.SlogHandler(logger, logging.SourceUI)),
		SingleInstance: singleInstance(sh.showWindow),
		OnShutdown:     func() { stop() },
		PostShutdown:   func() { logger.Close() },
	})

	// This is the only instance from here on.
	current, settingsErr := settings.Load()
	logger.SetLevel(current.LogLevel)
	// Without a log file the app still runs; the in-memory log remains.
	logFile, logErr := logging.OpenFile(filepath.Join(dirs.Local, "logs"), logMaxBytes, logKeep)
	logger.Attach(logFile)

	data, problems := store.OpenData(dirs.Config, sealer())
	eng, err := engine.Start(engine.Options{
		Log: func(level, msg string) {
			logger.Log(logging.Line{Level: level, Source: logging.SourceEngine, Msg: msg})
		},
		Verbose: logger.Debug,
	})
	if err != nil {
		logger.Errorf("start the proxy engine: %v", err)
		logger.Close()
		reportStartupError(uiLanguage(current), err)
		return err
	}

	core := api.NewCore(api.Deps{
		Data:     data,
		Settings: settings,
		Routes:   eng,
		Vault:    vault(),
		Servers:  servers(),
		Launch:   launchMstsc,
		Gateway:  checkGateway,
		Log:      logger,
	})
	core.Start(problems)
	if settingsErr != nil {
		core.Notify(logging.LevelWarn, api.NoticeSettingsRecovered, nil, settingsErr)
	}
	if logErr != nil {
		core.Notify(logging.LevelWarn, api.NoticeLogUnavailable, nil, logErr)
	}
	sh.core = core

	// Sessions end before the engine they use, and both before the log.
	var once sync.Once
	stop = func() {
		once.Do(func() {
			core.Quit()
			if err := eng.Close(); err != nil {
				logger.Warnf("stop the proxy engine: %v", err)
			}
			logger.Infof("%s stopped", productName)
		})
	}

	withErrors := application.ServiceOptions{MarshalError: api.MarshalError}
	for _, svc := range []application.Service{
		application.NewServiceWithOptions(api.NewSettingsService(settings,
			api.AppInfo{Name: productName, Version: opts.Version, Repo: repoURL},
			i18n.Detect, sh.settingsChanged), withErrors),
		application.NewServiceWithOptions(api.NewProfileService(core), withErrors),
		application.NewServiceWithOptions(api.NewProxyService(core), withErrors),
		application.NewServiceWithOptions(api.NewSessionService(core), withErrors),
		application.NewServiceWithOptions(api.NewAppService(core, sh.quitLater), withErrors),
	} {
		sh.app.RegisterService(svc)
	}

	sh.createWindow(current)
	sh.createTray(opts.Icon, current)
	err = sh.app.Run()
	stop() // in case Run returned without shutting down
	if err != nil {
		logger.Errorf("the app stopped with an error: %v", err)
		logger.Close()
		reportStartupError(uiLanguage(settings.Get()), err)
		return err
	}
	logger.Close()
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
	core     *api.Core
	logger   *logging.Logger
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
		// While a remote desktop uses a tunnel, closing the window must not
		// quit: that would cut the session.
		if s.settings.Get().CloseBehavior == model.CloseToTray || s.core.Running() > 0 {
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
	s.quitItem = s.menu.Add(i18n.T(lang, "tray.quit")).OnClick(func(*application.Context) { s.quitFromTray() })

	s.tray = s.app.SystemTray.New()
	s.tray.SetIcon(icon)
	s.tray.SetTooltip(productName)
	s.tray.SetMenu(s.menu)
	// A left click opens the window. In Wails v3 beta the right-click menu may
	// not open on Windows (wailsapp/wails#6161); the window has every function.
	s.tray.OnClick(s.showWindow)
}

// settingsChanged applies saved settings to the Go side: the tray labels
// follow the language, the log its level.
func (s *shell) settingsChanged(v model.Settings) {
	s.logger.SetLevel(v.LogLevel)
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

// quitFromTray quits at once when no remote desktop is connected. Otherwise
// quitting would end them, so the window comes forward and asks first; it
// quits through AppService.Quit. Choosing Quit again while the question is
// open quits, in case the page cannot show it.
func (s *shell) quitFromTray() {
	if s.core.Running() == 0 {
		s.quit()
		return
	}
	s.showWindow()
	if !s.core.AskToQuit() {
		s.quit()
	}
}

// quitLater quits without holding up the service call that asked for it.
// The shutdown waits for the calls in flight (the server build's HTTP
// server) and runs on the main thread, so it must not run inside one.
func (s *shell) quitLater() { go s.quit() }

func (s *shell) quit() {
	s.quitting.Store(true)
	s.app.Quit()
}
