//go:build desktop

package desktop

import (
	"log"
	"runtime"
	"sync"

	"fyne.io/systray"
)

// Systray is the real tray icon, made with fyne.io/systray. Only the
// desktop version (build tag desktop) has it, so the console version does
// not link the library.
//
// No click handlers are set, so a left or a right click opens the menu on
// every system: Windows and macOS show it, and on Linux the icon tells the
// panel that it is only a menu (ItemIsMenu).
type Systray struct {
	mu      sync.Mutex
	pick    func(Item)
	items   []*systray.MenuItem // the menu items in order; nil for a separator
	current []Item              // what each item shows now
}

// NewSystray makes the tray icon. Call its Run on the main thread.
func NewSystray() *Systray {
	return &Systray{}
}

// Run shows the icon and returns after Quit (see Tray).
func (t *Systray) Run(events Events) {
	t.mu.Lock()
	t.pick = events.Pick
	t.mu.Unlock()
	defer func() {
		// On Linux without a tray (no D-Bus session) the library fails
		// while it ends, after Exit has run: it closes a connection it never
		// made. The program has stopped by then; only note it in the log.
		if value := recover(); value != nil {
			log.Printf("The tray stopped with an error: %v", value)
		}
	}()
	systray.Run(func() {
		// SetTemplateIcon uses the template on macOS and the normal icon
		// elsewhere.
		systray.SetTemplateIcon(TemplateIcon(), Icon(runtime.GOOS))
		if runtime.GOOS == "linux" {
			systray.SetTitle("WebQuiz") // the name of the icon; on macOS a title would show as text in the menu bar
		}
		events.Ready()
	}, events.Exit)
}

// SetMenu shows the menu and the tooltip (see Tray).
func (t *Systray) SetMenu(tooltip string, items []Item) {
	t.mu.Lock()
	defer t.mu.Unlock()
	systray.SetTooltip(tooltip)
	if t.items == nil {
		for i, item := range items {
			if item.Separator {
				systray.AddSeparator()
				t.items = append(t.items, nil)
				continue
			}
			menuItem := systray.AddMenuItem(item.Title, "")
			if item.Disabled {
				menuItem.Disable()
			}
			t.items = append(t.items, menuItem)
			go t.listen(menuItem, i)
		}
		t.current = items
		return
	}
	if len(items) != len(t.current) {
		log.Printf("The tray menu cannot change its items: %d items, then %d", len(t.current), len(items))
		return
	}
	for i, item := range items {
		menuItem := t.items[i]
		if menuItem == nil {
			continue
		}
		if item.Title != t.current[i].Title {
			menuItem.SetTitle(item.Title)
		}
		if item.Disabled != t.current[i].Disabled {
			if item.Disabled {
				menuItem.Disable()
			} else {
				menuItem.Enable()
			}
		}
	}
	t.current = items
}

// listen passes the clicks on one menu item to Events.Pick.
func (t *Systray) listen(menuItem *systray.MenuItem, index int) {
	for range menuItem.ClickedCh {
		t.mu.Lock()
		item, pick := t.current[index], t.pick
		t.mu.Unlock()
		if pick != nil {
			pick(item)
		}
	}
}

// Quit removes the icon and makes Run return (see Tray).
func (t *Systray) Quit() {
	systray.Quit()
}
