package desktop

import (
	"errors"
	"syscall"
)

// wsaeAddrInUse is WSAEADDRINUSE, the Windows error for a port that another
// program holds (Go's syscall.EADDRINUSE is a different number on Windows).
const wsaeAddrInUse syscall.Errno = 10048

// isPortBusy tells whether err says that another program holds the port.
func isPortBusy(err error) bool {
	return errors.Is(err, wsaeAddrInUse) || errors.Is(err, syscall.EADDRINUSE)
}
