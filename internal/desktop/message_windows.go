//go:build desktop

package desktop

import (
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

// ShowError shows text in a Windows message box with an error sign. It
// returns when the person closes the box, so call it in its own goroutine.
func ShowError(text string) {
	// The box belongs to the thread that makes it: keep this goroutine on
	// one thread until it closes.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	title, _ := windows.UTF16PtrFromString("WebQuiz")
	body, err := windows.UTF16PtrFromString(strings.ReplaceAll(text, "\x00", ""))
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SETFOREGROUND)
}
