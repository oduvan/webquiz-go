package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The real desktop program on a machine without a desktop (no D-Bus
// session, so no tray). It still serves; it writes nothing to stdout and
// stderr; the tray library's error goes to the log file; SIGTERM stops it
// the way Ctrl+C stops the console version, so the waiting answer is
// written.
func TestDesktopProgramWithoutATray(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("this test runs on Linux: the program is built without cgo there, and it gets SIGTERM")
	}
	binary := testutil.BuildDesktopBinary(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "quizzes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "quizzes", "math.yaml"), mathQuiz)
	port := freePort(t)
	cmd, output := startProgram(t, binary, port, []string{
		"WEBQUIZ_BINARY_DIR=" + dir,
		"WEBQUIZ_IS_BINARY=0",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(dir, "no-bus"),
	}, "--port", strconv.Itoa(port))

	run := &desktopRun{dir: dir, port: port}
	userID := run.answer(t)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the program ended with %v:\n%s", err, output)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the program did not stop on SIGTERM:\n%s", output)
	}
	if output.String() != "" {
		t.Errorf("the desktop program wrote to stdout or stderr:\n%s", output)
	}
	if !strings.Contains(run.answers(t), userID+",2 + 2?,4,4,True,") {
		t.Errorf("the answer is not in the CSV:\n%s", run.answers(t))
	}
	_, log := run.log(t)
	for _, want := range []string{
		" - webquiz.desktop - INFO - Desktop version with a tray icon. Students open http://",
		" - webquiz.desktop - WARNING - systray error: failed to connect to DBus",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
}

// --version of the desktop program: the admin panel's version check runs
// the program file with it.
func TestDesktopProgramVersion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the desktop program is built here only on Linux")
	}
	out, err := exec.Command(testutil.BuildDesktopBinary(t), "--version").Output()
	if err != nil || !strings.HasPrefix(string(out), "webquiz ") {
		t.Errorf("--version: %v %q", err, out)
	}
}
