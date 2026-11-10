package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// runBriefly is run_webquiz_cli_briefly of test_cli_directory_creation.py:
// it starts the real program, waits until it answers, stops it and returns
// the base folder. Python ran in the current folder; the Go program uses
// WEBQUIZ_BINARY_DIR (or its own folder) as its base, so the test sets it.
// Relative paths in the test config are inside that folder in both.
func runBriefly(t *testing.T, dir string, args ...string) {
	t.Helper()
	port := freePort(t)
	hasPort := false
	for _, a := range args {
		if a == "--port" {
			hasPort = true
		}
	}
	if !hasPort {
		args = append(args, "--port", strconv.Itoa(port))
	} else {
		for i, a := range args {
			if a == "--port" {
				port, _ = strconv.Atoi(args[i+1])
			}
		}
	}
	cmd, output := startProgram(t, testutil.BuildBinary(t), port,
		[]string{"WEBQUIZ_BINARY_DIR=" + dir, "WEBQUIZ_IS_BINARY=0"}, args...)
	stopProgram(t, cmd, output)
}

func requireDirs(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, name)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("%s was not created as a folder: %v", name, err)
		}
	}
}

func requireFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, name)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not created: %v", name, err)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_creates_all_files
func TestCLICreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	runBriefly(t, dir)
	requireDirs(t, dir, "quizzes", "logs", "data", "static", "quizzes/imgs")
	requireFiles(t, dir, "quizzes/default.yaml", "static/index.html")
	logs, _ := filepath.Glob(filepath.Join(dir, "logs", "*.log"))
	if len(logs) == 0 {
		t.Error("No log files were created by CLI command")
	}
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_with_custom_directories
func TestCLIWithCustomDirectories(t *testing.T) {
	dir := t.TempDir()
	quizzes, logs, data, static := filepath.Join(dir, "my_quizzes"), filepath.Join(dir, "my_logs"),
		filepath.Join(dir, "my_data"), filepath.Join(dir, "my_static")
	runBriefly(t, dir, "--quizzes-dir", quizzes, "--logs-dir", logs, "--csv-dir", data, "--static", static)
	requireDirs(t, dir, quizzes, logs, data, static)
	requireFiles(t, dir, filepath.Join(quizzes, "default.yaml"))
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_handles_existing_directories
func TestCLIHandlesExistingDirectories(t *testing.T) {
	dir := t.TempDir()
	quizzes := filepath.Join(dir, "quizzes")
	if err := os.MkdirAll(quizzes, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(quizzes, "custom.yaml")
	writeFile(t, custom, "title: Custom Quiz\nquestions: []")
	runBriefly(t, dir)
	content, err := os.ReadFile(custom)
	if err != nil || !strings.Contains(string(content), "Custom Quiz") {
		t.Errorf("custom.yaml: %v\n%s", err, content)
	}
	requireDirs(t, dir, "logs", "data", "static")
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_with_config_file
func TestCLIWithConfigFile(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "test_config.yaml")
	writeFile(t, config, `
server:
  host: "127.0.0.1"
  port: 9090

paths:
  quizzes_dir: "custom_quizzes"
  logs_dir: "custom_logs"
  csv_dir: "custom_data"
  static_dir: "custom_static"

admin:
  master_key: "test123"
`)
	runBriefly(t, dir, "--config", config)
	requireDirs(t, dir, "custom_quizzes", "custom_logs", "custom_data", "custom_static")
	requireFiles(t, dir, "custom_quizzes/default.yaml")
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_config_file_with_cli_overrides
func TestCLIConfigFileWithCLIOverrides(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "test_config.yaml")
	writeFile(t, config, `
paths:
  quizzes_dir: "config_quizzes"
  logs_dir: "config_logs"
  csv_dir: "config_data"
  static_dir: "config_static"
`)
	quizzes, logs := filepath.Join(dir, "cli_override_quizzes"), filepath.Join(dir, "cli_override_logs")
	runBriefly(t, dir, "--config", config, "--quizzes-dir", quizzes, "--logs-dir", logs)

	requireDirs(t, dir, quizzes, logs, "config_data", "config_static")
	for _, overridden := range []string{"config_quizzes", "config_logs"} {
		if _, err := os.Stat(filepath.Join(dir, overridden)); err == nil {
			t.Errorf("%s should not exist (overridden by the command line)", overridden)
		}
	}
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_with_master_key
func TestCLIWithMasterKey(t *testing.T) {
	dir := t.TempDir()
	runBriefly(t, dir, "--master-key", "secret123")
	requireDirs(t, dir, "quizzes", "logs", "data", "static")
	if content, err := os.ReadFile(filepath.Join(dir, "webquiz.yaml")); err == nil {
		if !strings.Contains(string(content), "admin:") && !strings.Contains(string(content), "master_key:") {
			t.Errorf("webquiz.yaml has no admin section:\n%s", content)
		}
	}
	// Python only looked at the folders; the startup log also says that a key is set.
	log, _ := os.ReadFile(filepath.Join(dir, "logs", "0001.log"))
	if !strings.Contains(string(log), "Master key set: True") {
		t.Errorf("--master-key was not applied:\n%s", log)
	}
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_creates_webquiz_yaml_config
func TestCLICreatesWebquizYAMLConfig(t *testing.T) {
	dir := t.TempDir()
	runBriefly(t, dir)
	content, err := os.ReadFile(filepath.Join(dir, "webquiz.yaml"))
	if err != nil {
		t.Fatalf("webquiz.yaml configuration file was not created: %v", err)
	}
	for _, section := range []string{"server:", "paths:", "admin:"} {
		if !strings.Contains(string(content), section) {
			t.Errorf("Config file missing %s section", section)
		}
	}
}

// py: tests/test_cli_directory_creation.py::test_webquiz_cli_port_override
func TestCLIPortOverride(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "test_config.yaml")
	writeFile(t, config, "\nserver:\n  port: 9099\n")
	port := freePort(t)
	runBriefly(t, dir, "--config", config, "--port", strconv.Itoa(port))
	logs, _ := filepath.Glob(filepath.Join(dir, "logs", "*.log"))
	if len(logs) == 0 {
		t.Fatal("No log file was created")
	}
	content, _ := os.ReadFile(logs[0])
	if !strings.Contains(string(content), "  Port: "+strconv.Itoa(port)) {
		t.Errorf("CLI --port was not applied:\n%s", content)
	}
	if strings.Contains(string(content), "Port: 9099") {
		t.Errorf("Config port was used instead of the CLI override:\n%s", content)
	}
}

// Not a Python test: -h prints the help (exit 0) and an unknown option or an
// extra word is an error (exit 2), as with argparse.
func TestHelpAndOptionErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, flag := range []string{"-h", "--help"} {
		stdout.Reset()
		if code := run([]string{flag}, &stdout, &stderr); code != 0 {
			t.Errorf("%s: exit code %d", flag, code)
		}
		for _, want := range []string{"--daemon", "--stop", "--status", "--config", "--port", "--quizzes-dir", "--master-key",
			"--logs-dir", "--csv-dir", "--static", "--url-format", "--version", "Examples:", "webquiz --status"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s: no %q in the help", flag, want)
			}
		}
	}
	for _, args := range [][]string{{"--no-such-option"}, {"extra"}, {"--port", "abc"}, {"--port"}} {
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit code %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "webquiz: error:") {
			t.Errorf("%v: stderr %q", args, stderr.String())
		}
	}
}

// --stop and --status with no daemon: a message and exit code 1.
func TestStopAndStatusWithoutDaemon(t *testing.T) {
	binary := testutil.BuildBinary(t)
	dir := t.TempDir()
	for flag, want := range map[string]string{"--stop": "❌ No daemon is running", "--status": "❌ Daemon is not running"} {
		cmd := exec.Command(binary, flag)
		cmd.Env = append(cleanEnv(), "WEBQUIZ_BINARY_DIR="+dir, "WEBQUIZ_IS_BINARY=0")
		output, err := cmd.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), want) {
			t.Errorf("%s: %v\n%s", flag, err, output)
		}
	}
	// A PID file of a process that is gone is removed.
	writeFile(t, filepath.Join(dir, "webquiz.pid"), "999999999")
	cmd := exec.Command(binary, "--status")
	cmd.Env = append(cleanEnv(), "WEBQUIZ_BINARY_DIR="+dir, "WEBQUIZ_IS_BINARY=0")
	_, _ = cmd.CombinedOutput()
	if _, err := os.Stat(filepath.Join(dir, "webquiz.pid")); err == nil {
		t.Error("the stale PID file was not removed")
	}
}

// The whole daemon cycle: -d starts a detached copy and writes webquiz.pid;
// --status finds it; a second -d is refused; --stop ends it.
func TestDaemonStartStatusStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no daemon mode on Windows")
	}
	binary := testutil.BuildBinary(t)
	dir := t.TempDir()
	port := freePort(t)
	env := append(cleanEnv(), "WEBQUIZ_BINARY_DIR="+dir, "WEBQUIZ_IS_BINARY=0")
	runCLI := func(args ...string) (string, int) {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		cmd.Dir = t.TempDir()
		output, err := cmd.CombinedOutput()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(output), code
	}
	t.Cleanup(func() { runCLI("--stop") })

	out, code := runCLI("-d", "--port", strconv.Itoa(port))
	if code != 0 || !strings.Contains(out, "✅ Daemon started successfully (PID: ") {
		t.Fatalf("-d: exit %d\n%s", code, out)
	}
	pidText, err := os.ReadFile(filepath.Join(dir, "webquiz.pid"))
	if err != nil {
		t.Fatalf("no PID file in the base folder: %v", err)
	}
	if !strings.Contains(out, "(PID: "+strings.TrimSpace(string(pidText))+")") {
		t.Errorf("the PID file says %s:\n%s", pidText, out)
	}
	if status, _ := get(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/"); status != 200 {
		t.Errorf("the daemon does not serve: %d", status)
	}

	if out, code = runCLI("--status"); code != 0 || !strings.Contains(out, "✅ Daemon is running (PID: "+strings.TrimSpace(string(pidText))+")") {
		t.Errorf("--status: exit %d\n%s", code, out)
	}
	if out, code = runCLI("-d", "--port", strconv.Itoa(port)); code != 1 || !strings.Contains(out, "❌ Daemon is already running") {
		t.Errorf("second -d: exit %d\n%s", code, out)
	}
	if out, code = runCLI("--stop"); code != 0 || !strings.Contains(out, "✅ Daemon stopped successfully") {
		t.Errorf("--stop: exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "webquiz.pid")); err == nil {
		t.Error("the PID file is still there after --stop")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := httpGet("http://127.0.0.1:" + strconv.Itoa(port) + "/"); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon still serves after --stop")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// -d with a port that is taken: the new copy ends at once, and the first
// copy says so.
func TestDaemonFailsWhenThePortIsTaken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no daemon mode on Windows")
	}
	binary := testutil.BuildBinary(t)
	taken := listenTaken(t)
	dir := t.TempDir()
	cmd := exec.Command(binary, "-d", "--port", strconv.Itoa(taken))
	cmd.Env = append(cleanEnv(), "WEBQUIZ_BINARY_DIR="+dir, "WEBQUIZ_IS_BINARY=0")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "❌ Failed to start daemon") {
		t.Errorf("exit %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(dir, "webquiz.pid")); err == nil {
		t.Error("the PID file was left behind")
	}
}
