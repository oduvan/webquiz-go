package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/i18n"
)

// startupLogOf calls logStartupEnvironment for a bare server with cfg, as
// the Python tests called log_startup_environment(config) with a handler on
// the logger.
func startupLogOf(t *testing.T, cfg *config.Config) string {
	t.Helper()
	var out bytes.Buffer
	s := &Server{
		cfg:          cfg,
		translations: i18n.Get(cfg.Language),
		interfaces:   func(bool) []string { return nil },
		log:          slog.New(newPyLogHandler(&lockedWriter{w: &out}, loggerServer)),
	}
	s.logStartupEnvironment()
	return out.String()
}

// py: tests/test_startup_logging.py::test_log_startup_environment_function_directly
func TestLogStartupEnvironmentFunctionDirectly(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9999
	cfg.Paths.QuizzesDir = "/test/quizzes"
	cfg.Admin.MasterKey = "secret_key_123"

	log := startupLogOf(t, cfg)
	for _, want := range []string{
		"WebQuiz Server Starting", "WebQuiz version:", "Go version:", // "Python version:" in Python
		"Host: 127.0.0.1", "Port: 9999", "Quizzes directory: /test/quizzes",
		"Master key set: True", "Network interfaces: No non-loopback interfaces found",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in:\n%s", want, log)
		}
	}
	// The master key value is never written, only True/False.
	if strings.Contains(log, "secret_key_123") {
		t.Errorf("the master key is in the log:\n%s", log)
	}
}

// py: tests/test_startup_logging.py::test_log_startup_environment_with_tunnel_config
func TestLogStartupEnvironmentWithTunnelConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Tunnel = config.TunnelConfig{
		Server: "tunnel.example.com", PublicKey: "/path/to/public.key",
		PrivateKey: "/path/to/private.key", SocketName: "test_socket",
	}
	log := startupLogOf(t, cfg)
	for _, want := range []string{
		"Tunnel Configuration:", "Server: tunnel.example.com",
		"Public key path: /path/to/public.key", "Socket name: test_socket",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in:\n%s", want, log)
		}
	}
	// Without a tunnel server the section is left out.
	if strings.Contains(startupLogOf(t, config.Default()), "Tunnel Configuration:") {
		t.Error("a Tunnel Configuration section without a tunnel server")
	}
}

// Lists are written like Python's str(list).
func TestStartupLogListsLikePython(t *testing.T) {
	cfg := config.Default()
	cfg.Admin.TrustedIPs = []string{"127.0.0.1", "10.0.0.2"}
	cfg.Registration.Fields = []string{"Група", "it's"}
	log := startupLogOf(t, cfg)
	for _, want := range []string{"Trusted IPs: ['127.0.0.1', '10.0.0.2']", `Custom fields: ['Група', "it's"]`} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in:\n%s", want, log)
		}
	}
}
