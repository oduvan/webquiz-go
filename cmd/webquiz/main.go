// Command webquiz runs the WebQuiz server.
//
// It has the options of cli.py (flag accepts both -d and --daemon), the
// start steps of binary_entry.py and the daemon mode (see daemon.go).
//
// Start steps: the base folder is WEBQUIZ_BINARY_DIR when it is set, else
// the folder of the program; the program sets WEBQUIZ_BINARY_DIR to it and
// changes into it, so webquiz.yaml, quizzes/, data/, logs/ and static/ live
// next to the program. WEBQUIZ_IS_BINARY is set to 1 when it is not set;
// with 1 the admin page opens in a browser after the start (tests set 0).
//
// The program comes in two versions from the same code (new in 2.0). The
// console version (no build tag, main_console.go) is this program as it
// always was. The desktop version ("go build -tags desktop",
// main_desktop.go) runs the same server with an icon in the system tray
// instead of the console: see desktop.go and internal/desktop.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/version"
)

// run is the console version: the command line of cli.py.
func run(args []string, stdout, stderr io.Writer) int {
	opts, code, done := parseArgs(args, stdout, stderr)
	if done {
		return code
	}

	if err := useBaseFolder(); err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}

	// Before the server makes its log file, only warnings and errors are
	// shown (on the console), as Python's logging did before its setup.
	console := slog.New(&messageHandler{out: stderr, level: slog.LevelWarn})
	cfg, err := config.LoadWithOverrides(opts.configPath, opts.overrides, console)
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}

	// The order of cli.py: stop, status, daemon, then the server in front.
	switch {
	case opts.stop:
		return stopDaemon(stdout)
	case opts.status:
		return daemonStatus(stdout)
	case opts.daemon:
		return startDaemon(stdout, opts.daemonArgs)
	}
	return runServer(cfg, stdout)
}

// options are the parsed command line.
type options struct {
	daemon, stop, status bool
	configPath           string
	overrides            config.Overrides // only the options that were given override the config file
	daemonArgs           []string         // the options for the detached copy of -d
}

// parseArgs reads the command line like argparse in cli.py. It writes the
// help, the version and option errors itself; then done is true and code is
// the exit code.
func parseArgs(args []string, stdout, stderr io.Writer) (opts options, code int, done bool) {
	flags := flag.NewFlagSet("webquiz", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // the messages are written below, like argparse does
	flags.Usage = func() {}
	var (
		daemon     = flags.Bool("daemon", false, "")
		stop       = flags.Bool("stop", false, "")
		status     = flags.Bool("status", false, "")
		configPath = flags.String("config", "", "")
		port       = flags.Int("port", 0, "")
		quizzesDir = flags.String("quizzes-dir", "", "")
		masterKey  = flags.String("master-key", "", "")
		logsDir    = flags.String("logs-dir", "", "")
		csvDir     = flags.String("csv-dir", "", "")
		staticDir  = flags.String("static", "", "")
		urlFormat  = flags.String("url-format", "", "")
		showVer    = flags.Bool("version", false, "")
	)
	flags.BoolVar(daemon, "d", false, "")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, helpText)
		return opts, 0, true
	}
	if err == nil && flags.NArg() > 0 {
		err = fmt.Errorf("unrecognized arguments: %s", strings.Join(flags.Args(), " "))
	}
	if err != nil {
		fmt.Fprint(stderr, usageLine)
		fmt.Fprintf(stderr, "webquiz: error: %v\n", err)
		return opts, 2, true
	}
	if *showVer {
		fmt.Fprintf(stdout, "webquiz %s\n", version.Version)
		return opts, 0, true
	}

	opts = options{daemon: *daemon, stop: *stop, status: *status, configPath: *configPath, daemonArgs: daemonArgs(flags)}
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			opts.overrides.Port = port
		case "quizzes-dir":
			opts.overrides.QuizzesDir = quizzesDir
		case "master-key":
			opts.overrides.MasterKey = masterKey
		case "logs-dir":
			opts.overrides.LogsDir = logsDir
		case "csv-dir":
			opts.overrides.CSVDir = csvDir
		case "static":
			opts.overrides.StaticDir = staticDir
		case "url-format":
			opts.overrides.URLFormat = urlFormat
		}
	})
	return opts, 0, false
}

// daemonArgs are the options for the detached copy of the program: every
// option that was given, except -d/--daemon.
func daemonArgs(flags *flag.FlagSet) []string {
	var args []string
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "d" || f.Name == "daemon" {
			return
		}
		args = append(args, "--"+f.Name+"="+f.Value.String())
	})
	return args
}

// useBaseFolder does the start steps of binary_entry.py (see the package
// comment).
func useBaseFolder() error {
	dir := os.Getenv("WEBQUIZ_BINARY_DIR")
	if dir == "" {
		exe, err := programFile()
		if err != nil {
			return err
		}
		dir = filepath.Dir(exe)
	}
	return useFolder(dir)
}

// programFile is the path of the running program, with symbolic links
// resolved.
func programFile() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// useFolder makes dir the base folder: WEBQUIZ_BINARY_DIR names it, the
// program works in it, and WEBQUIZ_IS_BINARY is 1 unless it is set.
func useFolder(dir string) error {
	if err := os.Setenv("WEBQUIZ_BINARY_DIR", dir); err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	if _, ok := os.LookupEnv("WEBQUIZ_IS_BINARY"); !ok {
		return os.Setenv("WEBQUIZ_IS_BINARY", "1")
	}
	return nil
}

// runServer is run_server of cli.py: it starts the server and serves until
// SIGINT or SIGTERM.
func runServer(cfg *config.Config, stdout io.Writer) int {
	address := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	fmt.Fprintln(stdout, "🚀 Starting WebQuiz Testing System...")
	fmt.Fprintf(stdout, "📁 Quizzes directory: %s\n", cfg.Paths.QuizzesDir)
	fmt.Fprintf(stdout, "📝 Logs directory: %s\n", cfg.Paths.LogsDir)
	fmt.Fprintf(stdout, "📊 CSV directory: %s\n", cfg.Paths.CSVDir)
	fmt.Fprintf(stdout, "📂 Static directory: %s\n", cfg.Paths.StaticDir)
	if cfg.Admin.MasterKey != "" {
		fmt.Fprintln(stdout, "🔑 Master key: Set")
	} else {
		fmt.Fprintln(stdout, "🔑 Master key: Not set (admin disabled)")
	}
	fmt.Fprintf(stdout, "🌐 Server will be available at: %s (accessible on network)\n", address)
	if cfg.Admin.MasterKey != "" {
		fmt.Fprintf(stdout, "🔧 Admin panel: %s/admin\n", address)
	}
	fmt.Fprintln(stdout, "⏹️  Press Ctrl+C to stop")

	defer removeOwnPIDFile()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	running, err := newServing(cfg, server.Options{})
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}
	defer running.stop()

	if err := running.serve(ctx); err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "✅ Server started successfully")

	if os.Getenv("WEBQUIZ_IS_BINARY") == "1" {
		adminURL := fmt.Sprintf("http://127.0.0.1:%d/admin/", cfg.Server.Port)
		fmt.Fprintf(stdout, "🌐 Opening admin interface: %s\n", adminURL)
		if err := openBrowser(adminURL); err != nil {
			fmt.Fprintf(stdout, "⚠️  Could not open browser: %v\n", err)
		}
	}

	select {
	case <-ctx.Done():
	case err := <-running.served:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stdout, "❌ Error: %v\n", err)
			return 1
		}
	}
	running.stop()
	fmt.Fprintln(stdout, "✅ Server stopped")
	return 0
}

// serving is the running server of the program: the WebQuiz server and the
// HTTP server in front of it. The console and the desktop version start it
// and stop it the same way.
type serving struct {
	cfg      *config.Config
	srv      *server.Server
	http     *http.Server
	served   chan error // gets the error that ends the HTTP server
	stopOnce sync.Once
}

// newServing makes the server: its log file, the quiz. Nothing listens yet.
func newServing(cfg *config.Config, opts server.Options) (*serving, error) {
	srv, err := server.New(cfg, opts)
	if err != nil {
		return nil, err
	}
	return &serving{
		cfg:    cfg,
		srv:    srv,
		http:   &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 30 * time.Second},
		served: make(chan error, 1),
	}, nil
}

// serve opens the port of the config, serves it in a goroutine and starts
// the background work of the server (the CSV flush and the time limit
// watcher), which ends with ctx.
func (s *serving) serve(ctx context.Context) error {
	listener, err := listen(s.cfg.Server.Host, s.cfg.Server.Port)
	if err != nil {
		return err
	}
	go func() { s.served <- s.http.Serve(listener) }()
	s.srv.Start(ctx)
	return nil
}

// stop is what Ctrl+C does: the HTTP server stops taking requests and lets
// the running ones finish (up to 5 seconds), then the server writes the
// answers that wait for the CSV flush and closes its log file. A second
// call waits for the first one and does nothing.
func (s *serving) stop() {
	s.stopOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		s.srv.Close()
	})
}

// listen opens the port like aiohttp's TCPSite: an IPv4 address (the default
// 0.0.0.0) listens on IPv4 only.
func listen(host string, port int) (net.Listener, error) {
	network := "tcp"
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		network = "tcp4"
	}
	return net.Listen(network, net.JoinHostPort(host, strconv.Itoa(port)))
}

// openBrowser opens url in the default browser, like Python's webbrowser.open.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// messageHandler writes only the message of each record, like Python's
// logging before basicConfig (its "last resort" handler).
type messageHandler struct {
	out   io.Writer
	level slog.Level
}

func (h *messageHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *messageHandler) Handle(_ context.Context, r slog.Record) error {
	_, err := fmt.Fprintln(h.out, r.Message)
	return err
}

func (h *messageHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *messageHandler) WithGroup(string) slog.Handler { return h }
