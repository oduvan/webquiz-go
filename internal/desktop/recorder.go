package desktop

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"sync"
)

// Line is one log line that the Recorder kept.
type Line struct {
	Level   slog.Level
	Message string
}

// Recorder is a slog.Handler that keeps log lines (INFO and above) in
// memory. The desktop version has no console, so the lines that come before
// the log file exists (config loading, the data folder) are kept and written
// to the log file once it is open. The zero value is ready to use.
type Recorder struct {
	mu    sync.Mutex
	lines []Line
}

func (r *Recorder) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (r *Recorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, Line{Level: record.Level, Message: record.Message})
	return nil
}

// WithAttrs and WithGroup keep only the messages: attributes are dropped.
func (r *Recorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *Recorder) WithGroup(string) slog.Handler { return r }

// Lines are the kept lines, oldest first.
func (r *Recorder) Lines() []Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Line(nil), r.lines...)
}

// FirstError is the message of the first line at ERROR level or above, or
// "" when there is none.
func (r *Recorder) FirstError() string {
	for _, line := range r.Lines() {
		if line.Level >= slog.LevelError {
			return line.Message
		}
	}
	return ""
}

// WriteTo logs the kept lines to logger, with their levels.
func (r *Recorder) WriteTo(logger *slog.Logger) {
	for _, line := range r.Lines() {
		logger.Log(context.Background(), line.Level, line.Message)
	}
}

// LogWriter is an io.Writer for Go's standard log package: each line written
// to it becomes a warning of logger. The tray library reports its errors with
// the standard log, which would go to stderr, and the desktop version has no
// stderr.
func LogWriter(logger *slog.Logger) io.Writer {
	return logWriter{logger}
}

type logWriter struct{ logger *slog.Logger }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range bytes.Split(bytes.TrimRight(p, "\n"), []byte("\n")) {
		w.logger.Warn(string(line))
	}
	return len(p), nil
}

// UseLog sends Go's standard log to logger (see LogWriter), without the
// standard log's own time stamp, since the log file has one. It returns a
// function that puts the old output and flags back.
func UseLog(logger *slog.Logger) (restore func()) {
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(LogWriter(logger))
	log.SetFlags(0)
	return func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}
}
