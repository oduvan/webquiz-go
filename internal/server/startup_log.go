package server

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/version"
)

// logStartupEnvironment is log_startup_environment: the facts about the
// program, the machine and the config that the log starts with, for
// troubleshooting. New calls it right after the log file is made. The lines
// are those of the Python version. The Python facts are replaced by the
// Go ones:
//
//	Python version / executable -> Go version / executable
//	platform.platform()         -> GOOS/GOARCH
//	aiohttp, PyYAML versions    -> versions of the web socket and YAML
//	                               libraries the program was built with
//
// The master key is never written, only whether it is set.
func (s *Server) logStartupEnvironment() {
	cfg := s.cfg
	logf := func(format string, args ...any) { s.log.Info(fmt.Sprintf(format, args...)) }

	logf("%s", strings.Repeat("=", 60))
	logf("WebQuiz Server Starting - Environment Information")
	logf("%s", strings.Repeat("=", 60))

	logf("WebQuiz version: %s", version.Version)

	logf("Go version: %s", runtime.Version())
	executable, err := os.Executable()
	if err != nil {
		executable = "unknown"
	}
	logf("Go executable: %s", executable)

	logf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	logf("OS: %s %s", runtime.GOOS, osRelease())
	logf("Machine: %s", runtime.GOARCH)
	logf("Processor: %d logical CPUs", runtime.NumCPU())

	// Python logged the aiohttp and PyYAML versions; the Go program's
	// counterparts are its web socket and YAML libraries.
	logf("websocket version: %s", moduleVersion("github.com/coder/websocket"))
	logf("yaml version: %s", moduleVersion("gopkg.in/yaml.v3"))

	workDir, _ := os.Getwd()
	logf("Working directory: %s", workDir)
	configFile := cfg.ConfigPath
	if configFile == "" {
		configFile = "None (using defaults)"
	}
	logf("Config file: %s", configFile)

	binaryDir := os.Getenv("WEBQUIZ_BINARY_DIR")
	logf("Running as binary: %s", pyBool(os.Getenv("WEBQUIZ_IS_BINARY") == "1"))
	if binaryDir != "" {
		logf("Binary directory: %s", binaryDir)
	}

	logf("%s", strings.Repeat("-", 40))
	logf("Server Configuration:")
	logf("  Host: %s", cfg.Server.Host)
	logf("  Port: %d", cfg.Server.Port)

	logf("Path Configuration:")
	logf("  Quizzes directory: %s", cfg.Paths.QuizzesDir)
	logf("  Logs directory: %s", cfg.Paths.LogsDir)
	logf("  CSV directory: %s", cfg.Paths.CSVDir)
	logf("  Static directory: %s", cfg.Paths.StaticDir)

	logf("Admin Configuration:")
	logf("  Master key set: %s", pyBool(cfg.Admin.MasterKey != ""))
	logf("  Trusted IPs: %s", pyList(cfg.Admin.TrustedIPs))

	logf("Registration Configuration:")
	logf("  Approval required: %s", pyBool(cfg.Registration.Approve))
	label := cfg.Registration.UsernameLabel
	if label == "" {
		label = s.translations["username_label"]
	}
	logf("  Username label: %s", label)
	logf("  Custom fields: %s", pyList(cfg.Registration.Fields))

	logf("Extra answers-with-users CSV (per-answer + user info): %s", pyBool(cfg.ExtraAnswersWithUsersCSV))

	if cfg.Tunnel.Server != "" {
		logf("Tunnel Configuration:")
		logf("  Server: %s", cfg.Tunnel.Server)
		logf("  Public key path: %s", cfg.Tunnel.PublicKey)
		logf("  Private key path: %s", cfg.Tunnel.PrivateKey)
		if cfg.Tunnel.SocketName != "" {
			logf("  Socket name: %s", cfg.Tunnel.SocketName)
		}
	}

	if interfaces := s.interfaces(cfg.Server.IncludeIPv6); len(interfaces) > 0 {
		logf("Network interfaces: %s", strings.Join(interfaces, ", "))
	} else {
		logf("Network interfaces: No non-loopback interfaces found")
	}

	if host, err := os.Hostname(); err == nil {
		logf("Hostname: %s", host)
	}

	logf("%s", strings.Repeat("=", 60))
}

// pyList writes a list of texts like Python's str(list): ['a', 'b'].
func pyList(items []string) string {
	list := make([]any, len(items))
	for i, item := range items {
		list[i] = item
	}
	return pyval.Repr(list)
}

// moduleVersion is the version of a library the program was built with.
func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	return "unknown"
}

// osRelease is the release of the operating system, as far as it can be
// found without extra tools (Python: platform.release()).
func osRelease() string {
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "unknown"
}
