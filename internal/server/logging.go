package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The logger names of the Python version, written in each log line. Give a
// logger another name with logger.With("logger", name).
const (
	loggerServer = "webquiz.server"
	loggerAccess = "webquiz.access"
	loggerTunnel = "webquiz.tunnel"
	loggerKey    = "logger" // the attribute that holds the name
)

// lockedWriter writes whole log lines one at a time. After close it drops
// everything, so a goroutine that logs late cannot write to a closed file.
type lockedWriter struct {
	mu     sync.Mutex
	w      io.Writer
	closer io.Closer // closed by close(); may be nil
	closed bool
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return len(p), nil
	}
	return l.w.Write(p)
}

func (l *lockedWriter) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// pyLogHandler writes log records like Python's logging.basicConfig with
// format "%(asctime)s - %(name)s - %(levelname)s - %(message)s":
//
//	2026-11-09 19:26:56,334 - webquiz.server - INFO - Switched to quiz: math.yaml
//
// Attributes other than the logger name are added as " key=value".
type pyLogHandler struct {
	out   *lockedWriter
	level slog.Level
	name  string
	attrs string // the formatted attributes from WithAttrs
	group string // the group prefix from WithGroup
}

func newPyLogHandler(out *lockedWriter, name string) *pyLogHandler {
	return &pyLogHandler{out: out, level: slog.LevelInfo, name: name}
}

func (h *pyLogHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *pyLogHandler) Handle(_ context.Context, r slog.Record) error {
	var line strings.Builder
	line.WriteString(r.Time.Format("2006-01-02 15:04:05,000"))
	line.WriteString(" - ")
	name, attrs := h.name, h.attrs
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == loggerKey && h.group == "" {
			name = a.Value.String()
		} else {
			attrs += " " + h.group + a.Key + "=" + a.Value.String()
		}
		return true
	})
	line.WriteString(name)
	line.WriteString(" - ")
	line.WriteString(pyLevelName(r.Level))
	line.WriteString(" - ")
	line.WriteString(r.Message)
	line.WriteString(attrs)
	line.WriteString("\n")
	_, err := h.out.Write([]byte(line.String()))
	return err
}

func (h *pyLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	for _, a := range attrs {
		if a.Key == loggerKey && h.group == "" {
			next.name = a.Value.String()
			continue
		}
		next.attrs += " " + h.group + a.Key + "=" + a.Value.String()
	}
	return &next
}

func (h *pyLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.group += name + "."
	return &next
}

// pyLevelName is Python's name of a log level.
func pyLevelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError+4:
		return "CRITICAL"
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARNING"
	case level >= slog.LevelInfo:
		return "INFO"
	}
	return "DEBUG"
}

// fanoutHandler sends every record to several handlers: the log file and
// the console (or the test log).
type fanoutHandler []slog.Handler

func (f fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (f fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(fanoutHandler, len(f))
	for i, h := range f {
		next[i] = h.WithAttrs(attrs)
	}
	return next
}

func (f fanoutHandler) WithGroup(name string) slog.Handler {
	next := make(fanoutHandler, len(f))
	for i, h := range f {
		next[i] = h.WithGroup(name)
	}
	return next
}

// generateLogPath is generate_log_path: logs_dir/0001.log, or the first
// number whose file does not exist yet. It makes the folder.
func generateLogPath(logsDir string) (string, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return "", err
	}
	for suffix := 1; ; suffix++ {
		path := filepath.Join(logsDir, fmt.Sprintf("%04d.log", suffix))
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
}

// openLog makes the new log file (initialize_log_file) and the logger that
// writes to it and to extra (nil: the console, stderr, in the same format,
// like Python's StreamHandler).
func openLog(logsDir string, extra *slog.Logger) (path string, out *lockedWriter, logger *slog.Logger, err error) {
	var file *os.File
	for {
		path, err = generateLogPath(logsDir)
		if err != nil {
			return "", nil, nil, err
		}
		// O_EXCL: when another server took this number a moment ago, try the next one.
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return "", nil, nil, err
		}
	}
	out = &lockedWriter{w: file, closer: file}
	handlers := fanoutHandler{newPyLogHandler(out, loggerServer)}
	if extra != nil {
		handlers = append(handlers, extra.Handler())
	} else {
		handlers = append(handlers, newPyLogHandler(&lockedWriter{w: os.Stderr}, loggerServer))
	}
	return path, out, slog.New(handlers), nil
}
