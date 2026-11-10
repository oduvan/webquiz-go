package server_test

import (
	"io"
	"log/slog"
)

// discardLogger is a logger for servers made with server.New directly.
func discardLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}
