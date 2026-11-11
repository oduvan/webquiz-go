package desktop

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// The recorder keeps the lines that are logged before the log file exists
// (config loading, the data folder) and writes them to the log file later.
func TestRecorder(t *testing.T) {
	var rec Recorder
	logger := slog.New(&rec)
	logger.Info("Loaded configuration from: /data/webquiz.yaml")
	logger.With("logger", "x").Warn("Daemon mode is not supported")
	logger.Error("Error loading config from /data/webquiz.yaml: bad")
	logger.Error("a second error")

	want := []Line{
		{slog.LevelInfo, "Loaded configuration from: /data/webquiz.yaml"},
		{slog.LevelWarn, "Daemon mode is not supported"},
		{slog.LevelError, "Error loading config from /data/webquiz.yaml: bad"},
		{slog.LevelError, "a second error"},
	}
	if got := rec.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("lines %v, want %v", got, want)
	}
	if got := rec.FirstError(); got != "Error loading config from /data/webquiz.yaml: bad" {
		t.Errorf("FirstError %q", got)
	}

	var out bytes.Buffer
	rec.WriteTo(slog.New(slog.NewTextHandler(&out, nil)))
	text := out.String()
	for _, line := range []string{
		`level=INFO msg="Loaded configuration from: /data/webquiz.yaml"`,
		`level=WARN msg="Daemon mode is not supported"`,
		`level=ERROR msg="Error loading config from /data/webquiz.yaml: bad"`,
	} {
		if !strings.Contains(text, line) {
			t.Errorf("written log lacks %s:\n%s", line, text)
		}
	}
}

func TestRecorderWithoutErrors(t *testing.T) {
	var rec Recorder
	slog.New(&rec).Warn("only a warning")
	if got := rec.FirstError(); got != "" {
		t.Errorf("FirstError %q, want none", got)
	}
	if !rec.Enabled(context.Background(), slog.LevelInfo) || rec.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("the recorder keeps INFO and above")
	}
}

// The tray library writes its errors with Go's log package; LogWriter puts
// each line into the log file as a warning.
func TestLogWriter(t *testing.T) {
	var out bytes.Buffer
	std := log.New(LogWriter(slog.New(slog.NewTextHandler(&out, nil))), "", 0)
	std.Printf("systray error: failed to connect to DBus: %v\n", fmt.Errorf("no bus"))
	std.Print("first\nsecond")
	text := out.String()
	for _, line := range []string{
		`level=WARN msg="systray error: failed to connect to DBus: no bus"`,
		`level=WARN msg=first`,
		`level=WARN msg=second`,
	} {
		if !strings.Contains(text, line) {
			t.Errorf("log lacks %s:\n%s", line, text)
		}
	}
	if strings.Count(text, "\n") != 3 {
		t.Errorf("want 3 lines:\n%s", text)
	}
}

// UseLog sends Go's standard log (without its own time stamp) to a logger
// and gives a function that puts the old output back.
func TestUseLog(t *testing.T) {
	oldOut, oldFlags := log.Writer(), log.Flags()
	var out bytes.Buffer
	restore := UseLog(slog.New(slog.NewTextHandler(&out, nil)))
	log.Printf("systray error: %s", "x")
	if log.Flags() != 0 {
		t.Errorf("flags %d, want 0 (the log file has its own time)", log.Flags())
	}
	restore()
	if log.Writer() != oldOut || log.Flags() != oldFlags {
		t.Error("restore did not put the old output and flags back")
	}
	if !strings.Contains(out.String(), `level=WARN msg="systray error: x"`) {
		t.Errorf("log: %s", out.String())
	}
}
