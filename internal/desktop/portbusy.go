//go:build !windows

package desktop

import (
	"errors"
	"syscall"
)

// isPortBusy tells whether err says that another program holds the port.
func isPortBusy(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
