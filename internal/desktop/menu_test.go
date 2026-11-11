package desktop

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func running(lang string) State {
	return State{Version: "2.0", Language: lang, Port: 8080, StudentURL: "http://192.168.1.5:8080/", Running: true}
}

func TestMenuInUkrainian(t *testing.T) {
	want := []Item{
		{Title: "WebQuiz 2.0 — http://192.168.1.5:8080/", Action: ActionInfo, Disabled: true},
		{Separator: true},
		{Title: "Відкрити панель адміністратора", Action: ActionOpenAdmin, URL: "http://127.0.0.1:8080/admin/"},
		{Title: "Відкрити живу статистику", Action: ActionOpenLiveStats, URL: "http://127.0.0.1:8080/live-stats/"},
		{Title: "Відкрити менеджер файлів", Action: ActionOpenFiles, URL: "http://127.0.0.1:8080/files/"},
		{Separator: true},
		{Title: "Вийти", Action: ActionQuit},
	}
	if got := Menu(running("uk")); !reflect.DeepEqual(got, want) {
		t.Errorf("menu:\n%s\nwant:\n%s", dump(got), dump(want))
	}
}

func TestMenuInEnglish(t *testing.T) {
	want := []Item{
		{Title: "WebQuiz 2.0 — http://192.168.1.5:8080/", Action: ActionInfo, Disabled: true},
		{Separator: true},
		{Title: "Open admin panel", Action: ActionOpenAdmin, URL: "http://127.0.0.1:8080/admin/"},
		{Title: "Open live stats", Action: ActionOpenLiveStats, URL: "http://127.0.0.1:8080/live-stats/"},
		{Title: "Open file manager", Action: ActionOpenFiles, URL: "http://127.0.0.1:8080/files/"},
		{Separator: true},
		{Title: "Quit", Action: ActionQuit},
	}
	if got := Menu(running("en")); !reflect.DeepEqual(got, want) {
		t.Errorf("menu:\n%s\nwant:\n%s", dump(got), dump(want))
	}
}

// The pages open on the port of the config, at 127.0.0.1: the address the
// program opens the admin page at when it starts.
func TestMenuOpensPagesOnTheConfigPort(t *testing.T) {
	st := running("en")
	st.Port = 9090
	urls := map[Action]string{}
	for _, item := range Menu(st) {
		if item.URL != "" {
			urls[item.Action] = item.URL
		}
	}
	want := map[Action]string{
		ActionOpenAdmin:     "http://127.0.0.1:9090/admin/",
		ActionOpenLiveStats: "http://127.0.0.1:9090/live-stats/",
		ActionOpenFiles:     "http://127.0.0.1:9090/files/",
	}
	if !reflect.DeepEqual(urls, want) {
		t.Errorf("urls %v, want %v", urls, want)
	}
}

func TestMenuOfAnUnknownLanguageIsUkrainian(t *testing.T) {
	if got, want := Menu(running("fr")), Menu(running("uk")); !reflect.DeepEqual(got, want) {
		t.Errorf("menu:\n%s\nwant:\n%s", dump(got), dump(want))
	}
}

// When the server did not start, the first line shows the error instead of
// the address, and the items that open pages cannot be picked. Quit can.
func TestMenuShowsTheStartError(t *testing.T) {
	st := State{Version: "2.0", Language: "uk", Port: 8080, Problem: "something broke"}
	items := Menu(st)
	if items[0].Title != "WebQuiz 2.0 — Помилка: something broke" || !items[0].Disabled {
		t.Errorf("first line: %+v", items[0])
	}
	for _, item := range items {
		switch item.Action {
		case ActionOpenAdmin, ActionOpenLiveStats, ActionOpenFiles:
			if !item.Disabled {
				t.Errorf("%q can be picked while the server does not run", item.Title)
			}
		case ActionQuit:
			if item.Disabled {
				t.Error("Quit is disabled")
			}
		}
	}
	st.Language = "en"
	if got := Menu(st)[0].Title; got != "WebQuiz 2.0 — Error: something broke" {
		t.Errorf("English first line %q", got)
	}
}

// A broken webquiz.yaml: the server runs with the default settings, so the
// pages open, and the first line shows the problem.
func TestMenuShowsAProblemWhileTheServerRuns(t *testing.T) {
	st := running("en")
	st.Problem = "Error loading config from webquiz.yaml: bad"
	items := Menu(st)
	if items[0].Title != "WebQuiz 2.0 — Error: Error loading config from webquiz.yaml: bad" {
		t.Errorf("first line %q", items[0].Title)
	}
	for _, item := range items {
		if item.Action != ActionInfo && item.Disabled {
			t.Errorf("%q is disabled while the server runs", item.Title)
		}
	}
}

// A long error is cut in the menu (the message box and the log have all of it).
func TestMenuCutsALongError(t *testing.T) {
	st := State{Version: "2.0", Language: "en", Port: 8080, Problem: strings.Repeat("x", 300)}
	title := Menu(st)[0].Title
	problem := strings.TrimPrefix(title, "WebQuiz 2.0 — Error: ")
	if utf8.RuneCountInString(problem) != maxProblemRunes || !strings.HasSuffix(problem, "…") {
		t.Errorf("first line %q (%d runes in the error)", title, utf8.RuneCountInString(problem))
	}
	st.Problem = strings.Repeat("я", maxProblemRunes)
	if title := Menu(st)[0].Title; title != "WebQuiz 2.0 — Error: "+st.Problem {
		t.Errorf("an error of exactly %d runes was cut: %q", maxProblemRunes, title)
	}
}

// When the program keeps its data in ~/Documents/WebQuiz instead of next to
// WebQuiz.app, a second line says where.
func TestMenuShowsTheDataFolder(t *testing.T) {
	st := running("uk")
	st.DataFolder = "/Users/olga/Documents/WebQuiz"
	items := Menu(st)
	want := Item{Title: "Папка з даними: /Users/olga/Documents/WebQuiz", Action: ActionInfo, Disabled: true}
	if len(items) != len(Menu(running("uk")))+1 || !reflect.DeepEqual(items[1], want) {
		t.Errorf("menu:\n%s", dump(items))
	}
	st.Language = "en"
	if got := Menu(st)[1].Title; got != "Data folder: /Users/olga/Documents/WebQuiz" {
		t.Errorf("English folder line %q", got)
	}
}

func TestTooltip(t *testing.T) {
	if got := Tooltip(running("uk")); got != "WebQuiz 2.0 — http://192.168.1.5:8080/" {
		t.Errorf("tooltip %q", got)
	}
	st := State{Version: "2.0", Language: "en", Problem: strings.Repeat("x", 300)}
	if got := Tooltip(st); utf8.RuneCountInString(got) > maxTooltipRunes || !strings.HasPrefix(got, "WebQuiz 2.0 — Error: xxx") {
		t.Errorf("tooltip %q is longer than %d runes", got, maxTooltipRunes)
	}
}

// The message box (Windows) has the whole error and names the log file.
func TestMessageText(t *testing.T) {
	st := State{Version: "2.0", Language: "uk", Problem: strings.Repeat("x", 300)}
	want := "Помилка: " + strings.Repeat("x", 300) + "\n\nПодробиці є в журналі: /data/logs/0002.log"
	if got := MessageText(st, "/data/logs/0002.log"); got != want {
		t.Errorf("message %q", got)
	}
	st.Language = "en"
	if got := MessageText(st, ""); got != "Error: "+strings.Repeat("x", 300) {
		t.Errorf("message without a log file %q", got)
	}
}

// A port that another program holds gets a plain explanation; other errors
// are shown as they are.
func TestProblemText(t *testing.T) {
	taken, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := taken.Addr().(*net.TCPAddr).Port
	_, busy := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if busy == nil {
		t.Fatal("the second listen on the same port worked")
	}
	if got, want := ProblemText(busy, port, "uk"), fmt.Sprintf("порт %d зайнятий іншою програмою. Можливо, WebQuiz уже запущено", port); got != want {
		t.Errorf("uk: %q, want %q", got, want)
	}
	if got, want := ProblemText(fmt.Errorf("listen: %w", busy), port, "en"), fmt.Sprintf("port %d is used by another program. Maybe WebQuiz is already running", port); got != want {
		t.Errorf("en: %q, want %q", got, want)
	}
	if got := ProblemText(errors.New("create the log file in logs: permission denied"), 8080, "en"); got != "create the log file in logs: permission denied" {
		t.Errorf("other error: %q", got)
	}
}

// The student address is the first line of the admin page's "URL for other
// devices": server.url_format with the first network address.
func TestStudentURL(t *testing.T) {
	cases := []struct {
		format    string
		port      int
		addresses []string
		want      string
	}{
		{"http://{IP}:{PORT}/", 8080, []string{"192.168.1.5", "10.0.0.7"}, "http://192.168.1.5:8080/"},
		{"http://{IP}:{PORT}/", 80, []string{"192.168.1.5"}, "http://192.168.1.5/"},
		{"http://{IP}/webquiz/", 8080, []string{"10.0.0.7"}, "http://10.0.0.7/webquiz/"},
		// No network address: the computer's own address, so the line still says something true.
		{"http://{IP}:{PORT}/", 8080, nil, "http://127.0.0.1:8080/"},
	}
	for _, c := range cases {
		if got := StudentURL(c.format, c.port, c.addresses); got != c.want {
			t.Errorf("StudentURL(%q, %d, %v) = %q, want %q", c.format, c.port, c.addresses, got, c.want)
		}
	}
}

func dump(items []Item) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "  %+v\n", item)
	}
	return b.String()
}
