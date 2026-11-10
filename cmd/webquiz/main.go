// Command webquiz runs the WebQuiz server.
//
// This is the first, small version of the command: the start steps of the
// Python binary (binary_entry.py) and the flags --config and --port of
// cli.py, plus --version. Task 11 of the Go migration adds the other flags
// of cli.py and the daemon mode.
//
// Start steps: the base folder is WEBQUIZ_BINARY_DIR when it is set, else
// the folder of the program; the program sets WEBQUIZ_BINARY_DIR to it and
// changes into it, so webquiz.yaml, quizzes/, data/, logs/ and static/ live
// next to the program. WEBQUIZ_IS_BINARY is set to 1 when it is not set;
// with 1 the admin page opens in a browser after the start (tests set 0).
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
	"syscall"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("webquiz", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "Path to YAML configuration file")
	port := flags.Int("port", 0, "Port to listen on (default: 8080)")
	showVersion := flags.Bool("version", false, "Show the program's version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "webquiz %s\n", version.Version)
		return 0
	}

	if err := useBaseFolder(); err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}

	var overrides config.Overrides
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			overrides.Port = port
		}
	})
	// Before the server makes its log file, only warnings and errors are
	// shown (on the console), as Python's logging did before its setup.
	console := slog.New(&messageHandler{out: stderr, level: slog.LevelWarn})
	cfg, err := config.LoadWithOverrides(*configPath, overrides, console)
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}
	return runServer(cfg, stdout)
}

// useBaseFolder does the start steps of binary_entry.py (see the package
// comment).
func useBaseFolder() error {
	dir := os.Getenv("WEBQUIZ_BINARY_DIR")
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir = filepath.Dir(exe)
		if err := os.Setenv("WEBQUIZ_BINARY_DIR", dir); err != nil {
			return err
		}
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(cfg, server.Options{})
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}
	defer srv.Close()

	listener, err := listen(cfg.Server.Host, cfg.Server.Port)
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error: %v\n", err)
		return 1
	}
	httpServer := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 30 * time.Second}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	srv.Start(ctx)
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
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stdout, "❌ Error: %v\n", err)
			return 1
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	srv.Close()
	fmt.Fprintln(stdout, "✅ Server stopped")
	return 0
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
