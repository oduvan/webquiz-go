package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/desktop"
	"github.com/oduvan/webquiz-go/internal/i18n"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/version"
)

// The desktop version (new in 2.0): the server of runServer with an icon in
// the system tray (the menu bar on macOS) instead of the console. With
// "-tags desktop", main_desktop.go runs runDesktop with the real tray. This
// file has no build tag, so the tests run it with a fake tray; the console
// version never calls it.

// desktopDeps are the parts of the computer that the desktop version uses.
// main_desktop.go gives the real ones; the tests give fakes.
type desktopDeps struct {
	tray       desktop.Tray
	openURL    func(url string) error          // opens a page in the browser
	showError  func(text string)               // a message box on Windows; nothing on the other systems
	interfaces func(includeIPv6 bool) []string // this computer's network addresses (netinfo.Interfaces)
	stdout     io.Writer                       // only --help, --version, --stop and --status write here
	stderr     io.Writer                       // only option errors are written here
	refresh    time.Duration                   // how often the student address is looked up again; 0: never
}

// runDesktop runs the desktop version until Quit in the tray menu, Ctrl+C,
// SIGTERM (ctx ends) or the end of the session. It writes nothing to the
// console: a Windows program without a console has no stdout. Problems go
// to the log file, the first line of the menu and, on Windows, a message box.
func runDesktop(ctx context.Context, args []string, deps desktopDeps) int {
	opts, code, done := parseArgs(dropProcessArgument(args), deps.stdout, deps.stderr)
	if done {
		return code
	}
	app := &desktopApp{deps: deps, notes: &desktop.Recorder{}, restoreLog: func() {}}
	app.note = slog.New(app.notes)
	app.state = desktop.State{Version: version.Version, Language: i18n.Default, Port: config.Default().Server.Port}
	defer func() { app.restoreLog() }()
	if code, done := app.start(ctx, opts); done {
		return code
	}
	return app.runTray(ctx)
}

// dropProcessArgument removes the -psn_... argument that macOS gives an app
// in some cases (it names the process); it is not an option of the program.
func dropProcessArgument(args []string) []string {
	var kept []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-psn_") {
			kept = append(kept, arg)
		}
	}
	return kept
}

// desktopApp is one run of the desktop version.
type desktopApp struct {
	deps    desktopDeps
	notes   *desktop.Recorder // log lines kept until a log file exists
	note    *slog.Logger      // writes to notes
	log     *slog.Logger      // the log file, as webquiz.desktop; nil while there is none
	logFile string            // the path of that log file
	cfg     *config.Config
	running *serving // nil when the server did not start

	restoreLog func() // puts Go's standard log back (see desktop.UseLog)

	mu    sync.Mutex
	state desktop.State // what the menu shows; guarded by mu once the tray runs
}

// start does the start steps of the console version: the data folder, the
// config, the server. A step that fails keeps its error in the state for
// the menu, and the tray still shows. done is true for --stop and --status,
// which end the program as in the console version.
func (a *desktopApp) start(ctx context.Context, opts options) (code int, done bool) {
	if opts.daemon {
		a.note.Warn("Daemon mode (-d) is not supported in the desktop version: it runs as usual, with the tray icon")
	}
	folder, err := useDesktopFolder()
	if folder.Fallback {
		a.state.DataFolder = folder.Path
		a.note.Warn(fmt.Sprintf("Data folder: %s, because %s", folder.Path, folder.Reason))
	}
	if err != nil {
		a.fail(err)
		return 0, false
	}
	cfg, err := config.LoadWithOverrides(opts.configPath, opts.overrides, a.note)
	if err != nil {
		a.fail(err)
		return 0, false
	}
	a.cfg = cfg
	a.state.Language, a.state.Port = cfg.Language, cfg.Server.Port
	switch {
	case opts.stop:
		return stopDaemon(a.deps.stdout), true
	case opts.status:
		return daemonStatus(a.deps.stdout), true
	}
	// A broken webquiz.yaml: as in the console version the server runs with
	// the default settings, and the menu shows what is wrong.
	a.state.Problem = a.notes.FirstError()

	running, err := newServing(cfg, server.Options{Logger: slog.New(slog.DiscardHandler), NetworkInterfaces: a.deps.interfaces})
	if err != nil {
		a.fail(err)
		return 0, false
	}
	a.useLog(running.srv.Logger(), running.srv.LogFile())
	if err := running.serve(ctx); err != nil {
		a.fail(err)
		running.stop()
		return 0, false
	}
	a.running = running
	a.state.Running = true
	a.state.StudentURL = a.studentURL()
	a.log.Info("Desktop version with a tray icon. Students open " + a.state.StudentURL)
	go a.logServeError(running)

	if os.Getenv("WEBQUIZ_IS_BINARY") == "1" {
		adminURL := fmt.Sprintf("http://127.0.0.1:%d/admin/", cfg.Server.Port)
		a.log.Info("Opening admin interface: " + adminURL)
		if err := a.deps.openURL(adminURL); err != nil {
			a.log.Warn(fmt.Sprintf("Could not open the browser: %v", err))
		}
	}
	return 0, false
}

// useLog starts writing to a log file: the kept lines first, then the
// desktop version's own lines and Go's standard log (the tray library
// writes its errors there).
func (a *desktopApp) useLog(logger *slog.Logger, path string) {
	a.log = logger.With("logger", "webquiz.desktop")
	a.logFile = path
	if abs, err := filepath.Abs(path); err == nil {
		a.logFile = abs // the message box names the file in full
	}
	a.notes.WriteTo(a.log)
	a.restoreLog()
	a.restoreLog = desktop.UseLog(a.log)
}

// fail keeps a start error for the menu and writes it to a log file: the
// server's, or a new one when the server has none.
func (a *desktopApp) fail(err error) {
	a.state.Running = false
	a.state.Problem = desktop.ProblemText(err, a.state.Port, a.state.Language)
	if a.log == nil {
		logsDir := config.Default().Paths.LogsDir
		if a.cfg != nil {
			logsDir = a.cfg.Paths.LogsDir
		}
		file, openErr := server.OpenLogFile(logsDir)
		if openErr != nil {
			return // no log file can be made; the menu still shows the error
		}
		defer file.Close()
		a.useLog(file.Logger, file.Path)
	}
	a.log.Error("WebQuiz could not start: " + err.Error())
}

// studentURL is the address students open, from this computer's network
// addresses now.
func (a *desktopApp) studentURL() string {
	server := a.cfg.Server
	return desktop.StudentURL(server.URLFormat, server.Port, a.deps.interfaces(server.IncludeIPv6))
}

// logServeError writes to the log why the HTTP server ended, when it ended
// by itself (the console version prints it and ends).
func (a *desktopApp) logServeError(running *serving) {
	if err := <-running.served; !errors.Is(err, http.ErrServerClosed) {
		a.log.Error(fmt.Sprintf("The HTTP server stopped: %v", err))
	}
}

// runTray shows the tray icon until Quit in the menu, a signal (ctx) or the
// end of the session. Each way stops the server as Ctrl+C does in the
// console version. The exit code is 1 when the server did not start.
func (a *desktopApp) runTray(ctx context.Context) int {
	tray := a.deps.tray
	stopped := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			if a.running != nil {
				a.running.stop()
			}
			close(stopped)
		})
	}
	actions := desktop.Actions{Open: a.deps.openURL, Stop: stop, Quit: tray.Quit, Log: a.log}

	go func() {
		select {
		case <-ctx.Done(): // Ctrl+C or SIGTERM
			stop()
			tray.Quit()
		case <-stopped:
		}
	}()

	tray.Run(desktop.Events{
		Ready: func() {
			a.showMenu()
			a.mu.Lock()
			st := a.state
			a.mu.Unlock()
			if st.Problem != "" {
				go a.deps.showError(desktop.MessageText(st, a.logFile))
			}
			go a.refreshAddress(stopped)
		},
		Pick: actions.Pick,
		Exit: stop,
	})
	stop() // Run can end without Quit: then the server still has to stop
	if a.running == nil {
		return 1
	}
	return 0
}

// showMenu shows the menu of the current state.
func (a *desktopApp) showMenu() {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	a.deps.tray.SetMenu(desktop.Tooltip(st), desktop.Menu(st))
}

// refreshAddress looks up the student address again from time to time, so
// the menu follows when the computer joins another network.
func (a *desktopApp) refreshAddress(stopped <-chan struct{}) {
	if a.running == nil || a.deps.refresh <= 0 {
		return
	}
	ticker := time.NewTicker(a.deps.refresh)
	defer ticker.Stop()
	for {
		select {
		case <-stopped:
			return
		case <-ticker.C:
		}
		url := a.studentURL()
		a.mu.Lock()
		changed := url != a.state.StudentURL
		a.state.StudentURL = url
		a.mu.Unlock()
		select {
		case <-stopped:
			return
		default:
		}
		if changed {
			a.log.Info("Students open " + url)
			a.showMenu()
		}
	}
}

// useDesktopFolder is useBaseFolder of the desktop version: the folder is
// chosen by desktop.DataFolder (for WebQuiz.app on macOS, the folder that
// holds the app, or ~/Documents/WebQuiz).
func useDesktopFolder() (desktop.Folder, error) {
	facts := desktop.FolderFacts{Env: os.Getenv("WEBQUIZ_BINARY_DIR"), OS: runtime.GOOS, Writable: desktop.CanWrite}
	if facts.Env == "" {
		exe, err := programFile()
		if err != nil {
			return desktop.Folder{}, err
		}
		facts.Executable = exe
		facts.Home, _ = os.UserHomeDir()
	}
	folder := desktop.DataFolder(facts)
	if folder.Fallback {
		if err := os.MkdirAll(folder.Path, 0o755); err != nil {
			return folder, err
		}
	}
	return folder, useFolder(folder.Path)
}
