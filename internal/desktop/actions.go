package desktop

import (
	"fmt"
	"log/slog"
)

// Tray is the icon in the system tray (the menu bar on macOS) with its menu.
// NewSystray (build tag desktop) is the real one; tests use a fake.
type Tray interface {
	// Run shows the icon and returns after Quit. Call it on the main thread
	// (macOS needs that). Run calls the functions of events: see Events.
	Run(events Events)
	// SetMenu shows the menu and the tooltip. The first call adds the
	// items; later calls give the same items with new titles or a new
	// disabled state.
	SetMenu(tooltip string, items []Item)
	// Quit removes the icon and makes Run return.
	Quit()
}

// Events are what the tray tells the program.
type Events struct {
	// Ready runs in a new goroutine once the icon can take a menu.
	Ready func()
	// Pick runs when the person picks a menu item.
	Pick func(Item)
	// Exit runs when the system ends the program (log out, shut down) and
	// may also run after Quit. It must finish the work that has to be done
	// before the program ends: the program may end right after it.
	Exit func()
}

// Actions does what the menu items say.
type Actions struct {
	Open func(url string) error // opens a page in the browser
	Stop func()                 // stops the server the way Ctrl+C does: the waiting answers are written
	Quit func()                 // removes the icon (Tray.Quit)
	Log  *slog.Logger           // the log file; nil for none
}

// Pick does what item says.
func (a Actions) Pick(item Item) {
	switch item.Action {
	case ActionOpenAdmin, ActionOpenLiveStats, ActionOpenFiles:
		a.info("Opening " + item.URL)
		if err := a.Open(item.URL); err != nil {
			a.warn(fmt.Sprintf("Could not open the browser: %v", err))
		}
	case ActionQuit:
		a.info("Quit from the tray menu")
		a.Stop()
		a.Quit()
	}
}

func (a Actions) info(message string) {
	if a.Log != nil {
		a.Log.Info(message)
	}
}

func (a Actions) warn(message string) {
	if a.Log != nil {
		a.Log.Warn(message)
	}
}
