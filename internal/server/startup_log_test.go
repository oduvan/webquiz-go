package server_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// startupLog starts a server and returns the text of its log file (the file
// is written before New returns, so no waiting is needed; Python slept 0.5 s).
func startupLog(t *testing.T) (string, *testutil.TestServer) {
	t.Helper()
	ts := testutil.StartServer(t, testutil.Options{})
	return ts.ReadLog(t), ts
}

func requireLines(t *testing.T, log string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(log, want) {
			t.Errorf("the startup log has no %q:\n%s", want, log)
		}
	}
}

// py: tests/test_startup_logging.py::test_startup_logging_creates_log_file
func TestStartupLoggingCreatesLogFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	files, err := filepath.Glob(filepath.Join(ts.LogsDir, "*.log"))
	if err != nil || len(files) == 0 {
		t.Errorf("no log file in %s: %v", ts.LogsDir, err)
	}
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_environment_info
func TestStartupLoggingContainsEnvironmentInfo(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	// "Python version:" is "Go version:" here.
	requireLines(t, log, "WebQuiz Server Starting - Environment Information", "WebQuiz version:", "Go version:", "Platform:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_python_info
func TestStartupLoggingContainsGoInfo(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	// Python logged its version and executable; the Go program logs its own.
	requireLines(t, log, "Go version: go", "Go executable:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_os_info
func TestStartupLoggingContainsOSInfo(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	requireLines(t, log, "Platform:", "OS:", "Machine:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_server_config
func TestStartupLoggingContainsServerConfig(t *testing.T) {
	t.Parallel()
	log, ts := startupLog(t)
	requireLines(t, log, "Server Configuration:", "Host:", "Port:", "  Port: "+strconv.Itoa(ts.Port))
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_path_config
func TestStartupLoggingContainsPathConfig(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	requireLines(t, log, "Path Configuration:", "Quizzes directory:", "Logs directory:", "CSV directory:", "Static directory:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_admin_config
func TestStartupLoggingContainsAdminConfig(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	requireLines(t, log, "Admin Configuration:", "Master key set: True", "Trusted IPs: []")
	// The key itself ("test123") is never written.
	if strings.Contains(log, "test123") {
		t.Errorf("the master key is in the log:\n%s", log)
	}
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_registration_config
func TestStartupLoggingContainsRegistrationConfig(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	requireLines(t, log, "Registration Configuration:", "Approval required:", "Username label:", "Custom fields:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_dependency_versions
func TestStartupLoggingContainsDependencyVersions(t *testing.T) {
	t.Parallel()
	log, _ := startupLog(t)
	// Python: "aiohttp version:". The Go program's web server is the standard
	// library (its version is the Go version); the web socket library is the
	// closest counterpart of a package that needs a version line.
	requireLines(t, log, "websocket version:", "yaml version:")
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_working_directory
func TestStartupLoggingContainsWorkingDirectory(t *testing.T) {
	t.Parallel()
	log, ts := startupLog(t)
	requireLines(t, log, "Working directory:", "Config file: "+ts.ConfigPath)
}

// py: tests/test_startup_logging.py::test_startup_logging_contains_binary_mode_info
func TestStartupLoggingContainsBinaryModeInfo(t *testing.T) {
	t.Parallel()
	if os.Getenv("WEBQUIZ_IS_BINARY") == "1" {
		t.Skip("WEBQUIZ_IS_BINARY is 1 in this environment")
	}
	log, _ := startupLog(t)
	requireLines(t, log, "Running as binary: False")
}
