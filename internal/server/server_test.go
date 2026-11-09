package server_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/quiz"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Python's log format: "%(asctime)s - %(name)s - %(levelname)s - %(message)s".
var logLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3} - [a-z.]+ - (INFO|WARNING|ERROR|DEBUG|CRITICAL) - `)

// The log goes to logs_dir/NNNN.log (generate_log_path: the first free
// number), in Python's format.
func TestLogFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if got := filepath.Base(ts.LogFile()); got != "0001.log" {
		t.Errorf("log file %s, want 0001.log", got)
	}
	if filepath.Dir(ts.LogFile()) != ts.LogsDir {
		t.Errorf("log file %s is not in %s", ts.LogFile(), ts.LogsDir)
	}
	ts.GetJSON(t, "/api/verify-user/123456?x=1", testutil.WithHeader("User-Agent", "probe/1.0"))

	log := ts.ReadLog(t)
	for _, want := range []string{
		"Server state reset for new quiz",
		"Loaded 1 questions from " + filepath.Join(ts.QuizzesDir, "test_quiz.yaml") + ", show_right_answer: True, randomize_questions: False, show_answers_on_completion: False, show_final_list: True, show_explanation: False, explanations: 0, time_limit: none 0s",
		"Switched to quiz: test_quiz.yaml",
		"Using single quiz file as default: test_quiz.yaml",
		"SSH tunnel not configured, skipping initialization",
		` - webquiz.access - INFO - 127.0.0.1 [`,
		`] "GET /api/verify-user/123456?x=1 HTTP/1.1" 501 `,
		` "-" "probe/1.0"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if !logLine.MatchString(line) {
			t.Errorf("not in Python's log format: %q", line)
		}
	}
}

// A second server in the same logs folder takes the next number.
func TestLogFileNumbersContinue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Paths = config.PathsConfig{
		QuizzesDir: filepath.Join(dir, "quizzes"),
		LogsDir:    filepath.Join(dir, "logs"),
		CSVDir:     filepath.Join(dir, "data"),
		StaticDir:  filepath.Join(dir, "static"),
	}
	if err := os.MkdirAll(cfg.Paths.LogsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001.log", "0002.log", "0004.log"} {
		if err := os.WriteFile(filepath.Join(cfg.Paths.LogsDir, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	srv, err := server.New(cfg, server.Options{Logger: discardLogger(&out)})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if got := filepath.Base(srv.LogFile()); got != "0003.log" {
		t.Errorf("log file %s, want 0003.log", got)
	}
	if old, _ := os.ReadFile(filepath.Join(cfg.Paths.LogsDir, "0002.log")); string(old) != "old\n" {
		t.Errorf("an old log file was changed: %q", old)
	}
}

// Start runs the background loops until Close; Close can be called twice,
// and the server writes nothing to its log file after Close.
func TestStartAndClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Paths = config.PathsConfig{
		QuizzesDir: filepath.Join(dir, "quizzes"),
		LogsDir:    filepath.Join(dir, "logs"),
		CSVDir:     filepath.Join(dir, "data"),
		StaticDir:  filepath.Join(dir, "static"),
	}
	var out bytes.Buffer
	srv, err := server.New(cfg, server.Options{
		Logger:        discardLogger(&out),
		CheckInterval: 10 * time.Millisecond,
		Now:           func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv.Start(ctx)
	srv.Start(ctx) // a second Start does nothing
	time.Sleep(30 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		srv.Close()
		srv.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	cancel()

	before, err := os.ReadFile(srv.LogFile())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	after, _ := os.ReadFile(srv.LogFile())
	if !bytes.Equal(before, after) {
		t.Errorf("the log grew after Close:\n%s", after[len(before):])
	}
}

// The server connects the quiz rules to the Starlark checker when it is
// loaded (once, before any server exists).
func TestCheckerHooksAreSet(t *testing.T) {
	if quiz.RunChecker == nil || quiz.ValidateChecker == nil {
		t.Fatal("quiz.RunChecker and quiz.ValidateChecker must be set by the server package")
	}
	if correct, message := quiz.RunChecker("assert user_answer == 'x', 'wrong'", "y"); correct || message != "wrong" {
		t.Errorf("RunChecker: %v %q", correct, message)
	}
	if _, line, ok := quiz.ValidateChecker("if x\n  pass"); ok || line != 1 {
		t.Errorf("ValidateChecker: ok=%v line=%d", ok, line)
	}
	if _, _, ok := quiz.ValidateChecker("assert int(user_answer) > 2"); !ok {
		t.Error("ValidateChecker rejected good code")
	}
}
