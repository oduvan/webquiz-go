package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Daemon mode, as in cli.py. Go cannot fork, so -d starts a second copy of
// the program (without -d) in its own session, with no terminal. The first
// copy writes webquiz.pid in the base folder, checks after one second that
// the new copy is still running and ends. --stop sends SIGTERM to the
// process in webquiz.pid; --status reads it.

// pidFilePath is get_pid_file_path: webquiz.pid in the base folder (the
// program's working folder, see useBaseFolder).
func pidFilePath() string {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "webquiz.pid")
}

// readPID reads the process number from the PID file.
func readPID() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// isDaemonRunning is is_daemon_running: it checks that the process in the
// PID file is alive. A PID file of a process that is gone is removed.
func isDaemonRunning() bool {
	pid, err := readPID()
	if err == nil && processAlive(pid) {
		return true
	}
	if _, statErr := os.Stat(pidFilePath()); statErr == nil {
		_ = os.Remove(pidFilePath())
	}
	return false
}

// removeOwnPIDFile is called when the server ends: a daemon removes its PID
// file (a program that is not a daemon finds none, or one of another
// process, and leaves it).
func removeOwnPIDFile() {
	if pid, err := readPID(); err == nil && pid == os.Getpid() {
		_ = os.Remove(pidFilePath())
	}
}

// startDaemon is start_daemon. args are the options for the new copy.
func startDaemon(stdout io.Writer, args []string) int {
	if !daemonSupported {
		fmt.Fprintln(stdout, "❌ Daemon mode is not supported on this system")
		return 1
	}
	if isDaemonRunning() {
		fmt.Fprintln(stdout, "❌ Daemon is already running")
		return 1
	}
	fmt.Fprintln(stdout, "🚀 Starting webquiz daemon...")

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stdout, "❌ Fork failed: %v\n", err)
		return 1
	}
	cmd := exec.Command(exe, args...)
	detach(cmd) // a new session; stdin, stdout and stderr stay empty (/dev/null)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stdout, "❌ Fork failed: %v\n", err)
		return 1
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		fmt.Fprintf(stdout, "❌ Fork failed: %v\n", err)
		return 1
	}

	// Wait a moment to check that the new copy started (it ends at once when,
	// for example, the port is taken).
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(time.Second):
	}
	if isDaemonRunning() {
		fmt.Fprintf(stdout, "✅ Daemon started successfully (PID: %d)\n", pid)
		fmt.Fprintln(stdout, "🌐 Server running at http://0.0.0.0:8080 (accessible on network)")
		fmt.Fprintln(stdout, "📄 Logs: server.log")
		fmt.Fprintf(stdout, "⏹️  Stop with: kill %d\n", pid)
		return 0
	}
	fmt.Fprintln(stdout, "❌ Failed to start daemon")
	return 1
}

// stopDaemon is stop_daemon.
func stopDaemon(stdout io.Writer) int {
	if !isDaemonRunning() {
		fmt.Fprintln(stdout, "❌ No daemon is running")
		return 1
	}
	pid, err := readPID()
	if err != nil {
		fmt.Fprintf(stdout, "❌ Error stopping daemon: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "⏹️  Stopping daemon (PID: %d)...\n", pid)
	if err := terminate(pid); err != nil {
		fmt.Fprintf(stdout, "❌ Error stopping daemon: %v\n", err)
		return 1
	}
	// Wait for the process to stop (up to 10 seconds).
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
		if !isDaemonRunning() {
			fmt.Fprintln(stdout, "✅ Daemon stopped successfully")
			return 0
		}
	}
	fmt.Fprintln(stdout, "⚠️  Force killing daemon...")
	if err := kill(pid); err != nil {
		fmt.Fprintf(stdout, "❌ Error stopping daemon: %v\n", err)
		return 1
	}
	_ = os.Remove(pidFilePath())
	fmt.Fprintln(stdout, "✅ Daemon force stopped")
	return 0
}

// daemonStatus is the --status branch of main in cli.py. Its address is the
// fixed text of Python, not the configured port.
func daemonStatus(stdout io.Writer) int {
	if !isDaemonRunning() {
		fmt.Fprintln(stdout, "❌ Daemon is not running")
		return 1
	}
	pid, _ := readPID()
	fmt.Fprintf(stdout, "✅ Daemon is running (PID: %d)\n", pid)
	fmt.Fprintln(stdout, "🌐 Server: http://0.0.0.0:8080 (accessible on network)")
	return 0
}
