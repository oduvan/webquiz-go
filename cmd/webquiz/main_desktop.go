//go:build desktop

package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/oduvan/webquiz-go/internal/desktop"
	"github.com/oduvan/webquiz-go/internal/netinfo"
)

func init() {
	// macOS runs the tray on the main thread. Locking the thread in init
	// makes main run on the main thread, so the tray can be run from main.
	runtime.LockOSThread()
}

// The desktop version: the server with an icon in the system tray (see
// desktop.go). There is no console: nothing goes to stdout or stderr except
// what --help and --version are asked for.
func main() {
	// Go's standard log (the tray library writes there) goes to the log
	// file once it is open; until then, nowhere.
	log.SetOutput(io.Discard)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runDesktop(ctx, os.Args[1:], desktopDeps{
		tray:       desktop.NewSystray(),
		openURL:    openBrowser,
		showError:  desktop.ShowError,
		interfaces: netinfo.Interfaces,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		refresh:    30 * time.Second,
	})
	stop()
	os.Exit(code)
}
