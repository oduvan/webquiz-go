package desktop

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/oduvan/webquiz-go/internal/i18n"
	"github.com/oduvan/webquiz-go/internal/netinfo"
)

// Action is what a menu item does when the person picks it.
type Action int

const (
	// ActionInfo is a line that only informs: it is disabled and does nothing.
	ActionInfo Action = iota
	ActionOpenAdmin
	ActionOpenLiveStats
	ActionOpenFiles
	ActionQuit
)

// Item is one line of the tray menu.
type Item struct {
	Title     string
	Action    Action
	URL       string // the page that an "open" item opens
	Disabled  bool
	Separator bool // a line between groups of items; the other fields are empty
}

// State is what the menu shows.
type State struct {
	Version    string // the program version, like "2.0"
	Language   string // the config's language: "uk" or "en"
	Port       int    // server.port: the pages open at http://127.0.0.1:<Port>/
	StudentURL string // the address students open (see StudentURL)
	Running    bool   // the server serves; only then the pages can be opened
	Problem    string // an error to show in the first line instead of the address
	DataFolder string // the data folder when it is not next to the program (see DataFolder); "" otherwise
}

const (
	// maxProblemRunes is how much of an error the menu shows. The log file
	// and the message box on Windows have all of it.
	maxProblemRunes = 100
	// maxTooltipRunes keeps the tooltip short: Windows has room for 127
	// characters.
	maxTooltipRunes = 100
)

// Menu is the tray menu for a state: a line with the version and the student
// address (or the error), the data folder when it is not next to the
// program, the three admin pages, and Quit.
func Menu(st State) []Item {
	texts := i18n.Tray(st.Language)
	items := []Item{{Title: infoLine(st, maxProblemRunes), Action: ActionInfo, Disabled: true}}
	if st.DataFolder != "" {
		items = append(items, Item{Title: fill(texts["tray_data_folder"], "folder", st.DataFolder), Action: ActionInfo, Disabled: true})
	}
	local := "http://127.0.0.1:" + strconv.Itoa(st.Port)
	items = append(items,
		Item{Separator: true},
		Item{Title: texts["tray_open_admin"], Action: ActionOpenAdmin, URL: local + "/admin/", Disabled: !st.Running},
		Item{Title: texts["tray_open_live_stats"], Action: ActionOpenLiveStats, URL: local + "/live-stats/", Disabled: !st.Running},
		Item{Title: texts["tray_open_files"], Action: ActionOpenFiles, URL: local + "/files/", Disabled: !st.Running},
		Item{Separator: true},
		Item{Title: texts["tray_quit"], Action: ActionQuit},
	)
	return items
}

// Tooltip is the text shown when the pointer rests on the icon: the first
// line of the menu, kept short.
func Tooltip(st State) string {
	return cut(infoLine(st, maxProblemRunes), maxTooltipRunes)
}

// MessageText is the text of the message box that the Windows desktop
// version shows when there is a problem: the whole error and the log file
// ("" when no log file could be written).
func MessageText(st State, logFile string) string {
	texts := i18n.Tray(st.Language)
	text := fill(texts["tray_error"], "error", st.Problem)
	if logFile != "" {
		text += "\n\n" + fill(texts["tray_see_log"], "log", logFile)
	}
	return text
}

// ProblemText is the text of a start error for the menu. A port that
// another program holds (most often a WebQuiz that already runs) gets a
// plain explanation; other errors are shown as they are.
func ProblemText(err error, port int, language string) string {
	if isPortBusy(err) {
		return fill(i18n.Tray(language)["tray_port_busy"], "port", strconv.Itoa(port))
	}
	return err.Error()
}

// StudentURL is the address students open: server.url_format with the
// first network address of this computer, the first address that the admin
// page shows as "URL for other devices". With no network address it uses
// 127.0.0.1, which works on this computer only.
func StudentURL(urlFormat string, port int, addresses []string) string {
	ip := "127.0.0.1"
	if len(addresses) > 0 {
		ip = addresses[0]
	}
	return netinfo.FormatURL(urlFormat, ip, port)
}

// infoLine is "WebQuiz <version> — <student address>", or the error in place
// of the address, cut to maxProblem runes.
func infoLine(st State, maxProblem int) string {
	shown := st.StudentURL
	if st.Problem != "" {
		shown = fill(i18n.Tray(st.Language)["tray_error"], "error", cut(st.Problem, maxProblem))
	}
	return fmt.Sprintf("WebQuiz %s — %s", st.Version, shown)
}

// fill puts value in place of {name} in text.
func fill(text, name, value string) string {
	return strings.ReplaceAll(text, "{"+name+"}", value)
}

// cut shortens text to at most max runes; a cut text ends with "…".
func cut(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}
