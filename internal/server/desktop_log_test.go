package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The desktop version writes its own lines into the server's log file, with
// its own logger name.
func TestLoggerWritesToTheLogFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	ts.Server.Logger().With("logger", "webquiz.desktop").Warn("Daemon mode is not supported")
	log := ts.ReadLog(t)
	if !strings.Contains(log, " - webquiz.desktop - WARNING - Daemon mode is not supported\n") {
		t.Errorf("the log lacks the desktop line:\n%s", log)
	}
}

// When the server cannot start, the desktop version writes why into a new
// log file: the next free number, the server's format, nothing on the
// console, nothing after Close.
func TestOpenLogFile(t *testing.T) {
	t.Parallel()
	logs := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "0001.log"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := server.OpenLogFile(logs)
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != filepath.Join(logs, "0002.log") {
		t.Errorf("path %s, want 0002.log", file.Path)
	}
	file.Logger.With("logger", "webquiz.desktop").Error("Could not start: port 8080 is taken")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	file.Logger.Error("after Close")
	data, err := os.ReadFile(file.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !logLine.MatchString(text) || !strings.HasSuffix(text, " - webquiz.desktop - ERROR - Could not start: port 8080 is taken\n") || strings.Count(text, "\n") != 1 {
		t.Errorf("log file:\n%s", text)
	}
}

func TestOpenLogFileInAFolderThatCannotBeMade(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := server.OpenLogFile(filepath.Join(blocker, "logs")); err == nil {
		t.Error("OpenLogFile made a log file inside a file")
	}
}
