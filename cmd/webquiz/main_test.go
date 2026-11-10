package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
	"github.com/oduvan/webquiz-go/internal/version"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.RemoveBinary()
	os.Exit(code)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// syncBuffer collects the program's output from two pipes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startProgram runs the program and waits until it answers on port.
func startProgram(t *testing.T, binary string, port int, env []string, args ...string) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(cleanEnv(), env...)
	cmd.Dir = t.TempDir() // the program must not depend on where it was started from
	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			conn.Close()
			return cmd, output
		}
		if time.Now().After(deadline) {
			t.Fatalf("the program did not answer on port %d:\n%s", port, output)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// cleanEnv is the test's environment without the WEBQUIZ_ variables.
func cleanEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "WEBQUIZ_") {
			env = append(env, entry)
		}
	}
	return env
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func stopProgram(t *testing.T, cmd *exec.Cmd, output *syncBuffer) {
	t.Helper()
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
	case <-time.After(10 * time.Second):
		t.Fatalf("the program did not stop on SIGTERM:\n%s", output)
	}
	if !strings.Contains(output.String(), "✅ Server stopped") {
		t.Errorf("no stop message:\n%s", output)
	}
}

// Like the e2e fixture: WEBQUIZ_BINARY_DIR names the folder, --config and
// --port are given. The program serves the quiz page and the admin page.
func TestProgramServesQuizAndAdminPages(t *testing.T) {
	binary := testutil.BuildBinary(t)
	dir := t.TempDir()
	config := "server:\n  port: 1\nadmin:\n  master_key: test123\n  trusted_ips: []\n"
	if err := os.WriteFile(filepath.Join(dir, "webquiz.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cmd, output := startProgram(t, binary, port,
		[]string{"WEBQUIZ_BINARY_DIR=" + dir, "WEBQUIZ_IS_BINARY=0"},
		"--config", filepath.Join(dir, "webquiz.yaml"), "--port", strconv.Itoa(port))

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	if status, body := get(t, base+"/"); status != 200 || !strings.Contains(body, `"state":"quiz"`) || !strings.Contains(body, "Тестовий Quiz") {
		t.Errorf("GET /: %d\n%s", status, body)
	}
	if status, body := get(t, base+"/admin/"); status != 200 || !strings.Contains(body, `src="/assets/`) {
		t.Errorf("GET /admin/: %d\n%s", status, body)
	}
	// Relative folders of the default config are inside WEBQUIZ_BINARY_DIR.
	for _, path := range []string{"quizzes/default.yaml", "static/index.html", "logs/0001.log", "quizzes/imgs", "quizzes/attach"} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	log, _ := os.ReadFile(filepath.Join(dir, "logs", "0001.log"))
	if !strings.Contains(string(log), "Switched to quiz: default.yaml") {
		t.Errorf("log file:\n%s", log)
	}
	if strings.Contains(output.String(), "Opening admin interface") {
		t.Errorf("WEBQUIZ_IS_BINARY=0 must not open a browser:\n%s", output)
	}
	stopProgram(t, cmd, output)
}

// Without WEBQUIZ_BINARY_DIR the program's own folder is the base folder:
// a missing webquiz.yaml is made there from the example config.
func TestProgramUsesItsOwnFolder(t *testing.T) {
	built := testutil.BuildBinary(t)
	dir := t.TempDir()
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, filepath.Base(built))
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cmd, output := startProgram(t, binary, port, []string{"WEBQUIZ_IS_BINARY=0"}, "--port", strconv.Itoa(port))
	config, err := os.ReadFile(filepath.Join(dir, "webquiz.yaml"))
	if err != nil || !strings.Contains(string(config), "# WebQuiz Server Configuration") {
		t.Errorf("webquiz.yaml next to the program: %v\n%s", err, config)
	}
	if _, err := os.Stat(filepath.Join(dir, "static", "index.html")); err != nil {
		t.Errorf("static/index.html next to the program: %v", err)
	}
	if !strings.Contains(output.String(), "🔧 Admin panel: http://0.0.0.0:"+strconv.Itoa(port)+"/admin") {
		t.Errorf("start messages:\n%s", output)
	}
	stopProgram(t, cmd, output)
}

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code %d: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "webquiz "+version.Version+"\n" {
		t.Errorf("--version printed %q", got)
	}
	if code := run([]string{"--no-such-flag"}, &stdout, &stderr); code != 2 {
		t.Errorf("an unknown flag: exit code %d, want 2", code)
	}
}

// A port that is taken stops the program with an error, as in Python.
func TestProgramFailsWhenThePortIsTaken(t *testing.T) {
	binary := testutil.BuildBinary(t)
	taken, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	cmd := exec.Command(binary, "--port", strconv.Itoa(port))
	cmd.Env = append(cleanEnv(), "WEBQUIZ_BINARY_DIR="+dir, "WEBQUIZ_IS_BINARY=0")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Errorf("exit %v, want code 1:\n%s", err, output)
	}
	if !strings.Contains(string(output), "❌ Error:") {
		t.Errorf("output:\n%s", output)
	}
}
