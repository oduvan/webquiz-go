package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/desktop"
	"github.com/oduvan/webquiz-go/internal/version"
)

// The desktop version runs here with a fake tray and a fake browser: the
// real tray needs a desktop, which a test machine does not have.

// fakeTray is a tray that a test drives. Run blocks until Quit, like the
// real one.
type fakeTray struct {
	mu      sync.Mutex
	events  desktop.Events
	tooltip string
	items   []desktop.Item
	quits   int

	menus    chan []desktop.Item // every menu the program shows
	ran      chan struct{}       // closed when Run starts
	quit     chan struct{}       // closed by the first Quit
	quitOnce sync.Once
}

func newFakeTray() *fakeTray {
	return &fakeTray{menus: make(chan []desktop.Item, 64), ran: make(chan struct{}), quit: make(chan struct{})}
}

func (f *fakeTray) Run(events desktop.Events) {
	f.mu.Lock()
	f.events = events
	f.mu.Unlock()
	close(f.ran)
	go events.Ready()
	<-f.quit
}

func (f *fakeTray) SetMenu(tooltip string, items []desktop.Item) {
	f.mu.Lock()
	f.tooltip, f.items = tooltip, items
	f.mu.Unlock()
	f.menus <- items
}

func (f *fakeTray) Quit() {
	f.mu.Lock()
	f.quits++
	f.mu.Unlock()
	f.quitOnce.Do(func() { close(f.quit) })
}

// menu waits for the next menu the program shows.
func (f *fakeTray) menu(t *testing.T) []desktop.Item {
	t.Helper()
	select {
	case items := <-f.menus:
		return items
	case <-time.After(15 * time.Second):
		t.Fatal("the program showed no menu")
		return nil
	}
}

// pick picks the menu item with this action, as the person would.
func (f *fakeTray) pick(t *testing.T, action desktop.Action) {
	t.Helper()
	f.mu.Lock()
	items, pick := f.items, f.events.Pick
	f.mu.Unlock()
	for _, item := range items {
		if item.Action == action && !item.Separator {
			if item.Disabled {
				t.Fatalf("%q is disabled", item.Title)
			}
			pick(item)
			return
		}
	}
	t.Fatalf("the menu has no item with action %d:\n%v", action, items)
}

func (f *fakeTray) quitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quits
}

// texts collects texts from several goroutines (opened pages, message boxes).
type texts struct {
	mu   sync.Mutex
	list []string
	seen chan string
}

func newTexts() *texts { return &texts{seen: make(chan string, 64)} }

func (l *texts) add(text string) {
	l.mu.Lock()
	l.list = append(l.list, text)
	l.mu.Unlock()
	l.seen <- text
}

func (l *texts) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.list...)
}

// wait waits for the next text.
func (l *texts) wait(t *testing.T) string {
	t.Helper()
	select {
	case text := <-l.seen:
		return text
	case <-time.After(15 * time.Second):
		t.Fatal("nothing came")
		return ""
	}
}

// addresses is a fake list of network addresses that a test can change.
type addresses struct {
	mu   sync.Mutex
	list []string
}

func (a *addresses) set(list ...string) {
	a.mu.Lock()
	a.list = list
	a.mu.Unlock()
}

func (a *addresses) get(bool) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.list...)
}

// desktopRun is the desktop version running in this test.
type desktopRun struct {
	tray      *fakeTray
	opened    *texts // the pages the program opened in the browser
	messages  *texts // the message boxes it showed
	addresses *addresses
	stdout    bytes.Buffer
	stderr    bytes.Buffer
	done      chan int // the exit code
	cancel    context.CancelFunc
	dir       string
	port      int
}

// mathQuiz is a quiz with one question; its answers go to data/math_0001.csv.
const mathQuiz = `title: Math
questions:
  - question: "2 + 2?"
    options: ["3", "4"]
    correct_answer: 1
`

// newDesktopRun prepares the data folder dir (WEBQUIZ_BINARY_DIR) with
// mathQuiz. The environment and the working folder go back after the test.
func newDesktopRun(t *testing.T) *desktopRun {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "quizzes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "quizzes", "math.yaml"), mathQuiz)
	t.Setenv("WEBQUIZ_BINARY_DIR", dir)
	t.Setenv("WEBQUIZ_IS_BINARY", "0")
	t.Setenv("WEBQUIZ_MASTER_KEY", "")
	t.Setenv("WEBQUIZ_TIME_LIMIT_GRACE_SECONDS", "")
	t.Setenv("WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL", "")
	t.Chdir(t.TempDir()) // the program changes into dir; this puts the old folder back
	run := &desktopRun{
		tray:      newFakeTray(),
		opened:    newTexts(),
		messages:  newTexts(),
		addresses: &addresses{list: []string{"10.1.2.3"}},
		done:      make(chan int, 1),
		dir:       dir,
		port:      freePort(t),
	}
	return run
}

// start runs the desktop version with args (and --port) in a goroutine.
func (r *desktopRun) start(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	deps := desktopDeps{
		tray:       r.tray,
		openURL:    func(url string) error { r.opened.add(url); return nil },
		showError:  r.messages.add,
		interfaces: r.addresses.get,
		stdout:     &r.stdout,
		stderr:     &r.stderr,
		refresh:    20 * time.Millisecond,
	}
	args = append(args, "--port", strconv.Itoa(r.port))
	go func() { r.done <- runDesktop(ctx, args, deps) }()
	t.Cleanup(func() {
		cancel()
		r.tray.Quit()
		select {
		case <-r.done:
		case <-time.After(15 * time.Second):
			t.Error("the desktop version did not end")
		}
	})
}

// exitCode waits for the program to end.
func (r *desktopRun) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case code := <-r.done:
		r.done <- code // keep it for the cleanup
		return code
	case <-time.After(15 * time.Second):
		t.Fatal("the desktop version did not end")
		return -1
	}
}

func (r *desktopRun) url(path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(r.port) + path
}

// answer registers a student and answers the question right. The answer
// waits in memory for the 5-second CSV flush.
func (r *desktopRun) answer(t *testing.T) string {
	t.Helper()
	user := postJSON(t, r.url("/api/register"), map[string]any{"username": "Оля"})
	userID, _ := user["user_id"].(string)
	if userID == "" {
		t.Fatalf("register: %v", user)
	}
	if reply := postJSON(t, r.url("/api/submit-answer"), map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 1}); reply["is_correct"] != true {
		t.Fatalf("submit-answer: %v", reply)
	}
	return userID
}

// answers are the rows of the answers CSV of the run.
func (r *desktopRun) answers(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "data", "math_0001.csv"))
	if err != nil {
		t.Fatalf("the answers CSV: %v", err)
	}
	return string(data)
}

// log is the text of the newest log file.
func (r *desktopRun) log(t *testing.T) (string, string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(r.dir, "logs", "*.log"))
	if len(files) == 0 {
		t.Fatal("no log file")
	}
	newest := files[len(files)-1]
	data, err := os.ReadFile(newest)
	if err != nil {
		t.Fatal(err)
	}
	return newest, string(data)
}

func postJSON(t *testing.T, url string, body any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer res.Body.Close()
	var reply map[string]any
	text, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(text, &reply); err != nil {
		t.Fatalf("POST %s: %d %s", url, res.StatusCode, text)
	}
	return reply
}

// The menu: the version and the student address, then the three pages and
// Quit. Each page item opens its page on the program's port.
func TestDesktopShowsTheMenuAndOpensPages(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t)
	items := run.tray.menu(t)
	if want := "WebQuiz " + version.Version + " — http://10.1.2.3:" + strconv.Itoa(run.port) + "/"; items[0].Title != want || !items[0].Disabled {
		t.Errorf("first line %+v, want %q", items[0], want)
	}
	if got := run.tray.tooltip; got != items[0].Title {
		t.Errorf("tooltip %q", got)
	}
	for action, path := range map[desktop.Action]string{
		desktop.ActionOpenAdmin:     "/admin/",
		desktop.ActionOpenLiveStats: "/live-stats/",
		desktop.ActionOpenFiles:     "/files/",
	} {
		run.tray.pick(t, action)
		if got := run.opened.wait(t); got != run.url(path) {
			t.Errorf("action %d opened %q, want %q", action, got, run.url(path))
		}
		if status, _ := get(t, run.url(path)); status != 200 {
			t.Errorf("GET %s: %d", path, status)
		}
	}
	if status, body := get(t, run.url("/")); status != 200 || !strings.Contains(body, "Math") {
		t.Errorf("the quiz page: %d", status)
	}
	if opened := run.opened.all(); len(opened) != 3 {
		t.Errorf("opened %v: with WEBQUIZ_IS_BINARY=0 only the picked pages open", opened)
	}
}

// Quit stops the server the way Ctrl+C does: the answer that waits for the
// 5-second CSV flush is written, the icon goes away and the program ends.
func TestDesktopQuitWritesWaitingAnswers(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t)
	run.tray.menu(t)
	userID := run.answer(t)

	run.tray.pick(t, desktop.ActionQuit)
	if code := run.exitCode(t); code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(run.answers(t), userID+",2 + 2?,4,4,True,") {
		t.Errorf("the answer is not in the CSV:\n%s", run.answers(t))
	}
	if run.tray.quitCount() == 0 {
		t.Error("the icon was not removed")
	}
	if _, err := http.Get(run.url("/")); err == nil {
		t.Error("the server still answers after Quit")
	}
	_, log := run.log(t)
	if !strings.Contains(log, " - webquiz.desktop - INFO - Quit from the tray menu") {
		t.Errorf("log:\n%s", log)
	}
}

// Ctrl+C or SIGTERM stops the desktop version the same way.
func TestDesktopStopsOnASignal(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t)
	run.tray.menu(t)
	userID := run.answer(t)

	run.cancel() // what a signal does
	if code := run.exitCode(t); code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(run.answers(t), userID+",") {
		t.Errorf("the answer is not in the CSV:\n%s", run.answers(t))
	}
	if run.tray.quitCount() == 0 {
		t.Error("the icon was not removed")
	}
}

// When the system ends the program (log out, shut down), the tray calls Exit
// and the program may end right after it: the answers must be written by then.
func TestDesktopWritesAnswersWhenTheSystemEndsIt(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t)
	run.tray.menu(t)
	userID := run.answer(t)

	run.tray.mu.Lock()
	exit := run.tray.events.Exit
	run.tray.mu.Unlock()
	exit()
	if !strings.Contains(run.answers(t), userID+",") {
		t.Errorf("the answer is not in the CSV right after Exit:\n%s", run.answers(t))
	}
	run.tray.Quit()
	if code := run.exitCode(t); code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
}

// With WEBQUIZ_IS_BINARY=1 (the default for the program file) the admin page
// opens in the browser at start, as in the console version.
func TestDesktopOpensTheAdminPageAtStart(t *testing.T) {
	run := newDesktopRun(t)
	t.Setenv("WEBQUIZ_IS_BINARY", "1")
	run.start(t)
	if got := run.opened.wait(t); got != run.url("/admin/") {
		t.Errorf("opened %q at start, want %q", got, run.url("/admin/"))
	}
}

// The menu follows the config's language.
func TestDesktopMenuInEnglish(t *testing.T) {
	run := newDesktopRun(t)
	writeFile(t, filepath.Join(run.dir, "webquiz.yaml"), "language: en\n")
	run.start(t)
	var titles []string
	for _, item := range run.tray.menu(t) {
		titles = append(titles, item.Title)
	}
	got := strings.Join(titles, "|")
	if !strings.Contains(got, "|Open admin panel|Open live stats|Open file manager||Quit") {
		t.Errorf("menu: %s", got)
	}
}

// A laptop may join the network after the start: the student address in the
// menu follows.
func TestDesktopUpdatesTheStudentAddress(t *testing.T) {
	run := newDesktopRun(t)
	run.addresses.set()
	run.start(t)
	if first := run.tray.menu(t)[0].Title; !strings.HasSuffix(first, " — http://127.0.0.1:"+strconv.Itoa(run.port)+"/") {
		t.Errorf("first line without a network: %q", first)
	}
	run.addresses.set("192.168.7.20")
	want := " — http://192.168.7.20:" + strconv.Itoa(run.port) + "/"
	for {
		if first := run.tray.menu(t)[0].Title; strings.HasSuffix(first, want) {
			break
		}
	}
}

// A port that another program holds (often a WebQuiz that already runs):
// the menu says so, the pages cannot be opened, the message box (Windows)
// and the log have the error, and Quit ends the program with code 1.
func TestDesktopShowsWhyItCouldNotStart(t *testing.T) {
	run := newDesktopRun(t)
	taken, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(run.port))
	if err != nil {
		t.Skipf("cannot hold port %d: %v", run.port, err)
	}
	defer taken.Close()
	run.start(t)
	items := run.tray.menu(t)
	want := fmt.Sprintf("WebQuiz %s — Помилка: порт %d зайнятий іншою програмою. Можливо, WebQuiz уже запущено", version.Version, run.port)
	if items[0].Title != want {
		t.Errorf("first line %q, want %q", items[0].Title, want)
	}
	for _, item := range items {
		if item.URL != "" && !item.Disabled {
			t.Errorf("%q can be picked while the server does not run", item.Title)
		}
	}
	logFile, log := run.log(t)
	message := run.messages.wait(t)
	shown, found := strings.CutPrefix(message[strings.Index(message, "\n\n")+2:], "Подробиці є в журналі: ")
	if !strings.HasPrefix(message, "Помилка: порт ") || !found || !filepath.IsAbs(shown) || !sameFile(t, shown, logFile) {
		t.Errorf("message box %q; the log file is %s", message, logFile)
	}
	if !strings.Contains(log, " - webquiz.desktop - ERROR - WebQuiz could not start: listen tcp4 0.0.0.0:"+strconv.Itoa(run.port)) {
		t.Errorf("log:\n%s", log)
	}
	run.tray.pick(t, desktop.ActionQuit)
	if code := run.exitCode(t); code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
}

// A broken webquiz.yaml: as in the console version the server runs with the
// default settings; the menu, the message box and the log say what is wrong.
func TestDesktopShowsABrokenConfig(t *testing.T) {
	run := newDesktopRun(t)
	writeFile(t, filepath.Join(run.dir, "webquiz.yaml"), "server: [port\n")
	run.start(t)
	items := run.tray.menu(t)
	if !strings.HasPrefix(items[0].Title, "WebQuiz "+version.Version+" — Помилка: Error loading config from "+filepath.Join(run.dir, "webquiz.yaml")) {
		t.Errorf("first line %q", items[0].Title)
	}
	for _, item := range items {
		if item.URL != "" && item.Disabled {
			t.Errorf("%q is disabled, but the server runs", item.Title)
		}
	}
	if message := run.messages.wait(t); !strings.Contains(message, "Error loading config from") {
		t.Errorf("message box %q", message)
	}
	if status, _ := get(t, run.url("/")); status != 200 {
		t.Errorf("the server does not run: %d", status)
	}
	_, log := run.log(t)
	if !strings.Contains(log, " - webquiz.desktop - ERROR - Error loading config from ") {
		t.Errorf("log:\n%s", log)
	}
	run.tray.pick(t, desktop.ActionQuit)
	if code := run.exitCode(t); code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
}

// -d is not supported in the desktop version: the log says so and the
// program runs as usual, with the tray icon.
func TestDesktopRunsNormallyWithDaemonFlag(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t, "-d")
	items := run.tray.menu(t)
	if !strings.HasSuffix(items[0].Title, "http://10.1.2.3:"+strconv.Itoa(run.port)+"/") {
		t.Errorf("first line %q", items[0].Title)
	}
	if _, err := os.Stat(filepath.Join(run.dir, "webquiz.pid")); err == nil {
		t.Error("the desktop version wrote a PID file")
	}
	_, log := run.log(t)
	if !strings.Contains(log, " - webquiz.desktop - WARNING - Daemon mode (-d) is not supported in the desktop version") {
		t.Errorf("log:\n%s", log)
	}
}

// --version prints the version (the admin panel's version check runs the
// program file with it) and shows no tray.
func TestDesktopVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tray := newFakeTray()
	code := runDesktop(context.Background(), []string{"--version"}, desktopDeps{tray: tray, stdout: &stdout, stderr: &stderr})
	if code != 0 || stdout.String() != "webquiz "+version.Version+"\n" || stderr.Len() != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	select {
	case <-tray.ran:
		t.Error("--version showed the tray")
	default:
	}
}

// Old macOS versions start an app with a -psn_... argument; it is not an
// option of the program.
func TestDesktopIgnoresTheMacOSProcessArgument(t *testing.T) {
	run := newDesktopRun(t)
	run.start(t, "-psn_0_1234567")
	if items := run.tray.menu(t); !strings.Contains(items[0].Title, "http://") {
		t.Errorf("first line %q", items[0].Title)
	}
	if run.stderr.Len() != 0 {
		t.Errorf("stderr: %s", run.stderr.String())
	}
}

// A Windows program without a console has no stdout and stderr: the desktop
// version writes nothing there, so a write can never fail or block. The log
// file gets the lines instead.
func TestDesktopWritesNothingToTheConsole(t *testing.T) {
	run := newDesktopRun(t)
	readOut, writeOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	readErr, writeErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writeOut, writeErr
	restored := false
	restore := func() {
		if !restored {
			os.Stdout, os.Stderr = oldOut, oldErr
			restored = true
		}
	}
	defer restore()

	run.start(t)
	run.tray.menu(t)
	run.answer(t)
	run.tray.pick(t, desktop.ActionOpenAdmin)
	run.tray.pick(t, desktop.ActionQuit)
	run.exitCode(t)
	restore()
	writeOut.Close()
	writeErr.Close()
	out, _ := io.ReadAll(readOut)
	errs, _ := io.ReadAll(readErr)
	if len(out) != 0 || len(errs) != 0 || run.stdout.Len() != 0 || run.stderr.Len() != 0 {
		t.Errorf("written to the console:\nstdout: %s\nstderr: %s\n%s%s", out, errs, run.stdout.String(), run.stderr.String())
	}
	_, log := run.log(t)
	if !strings.Contains(log, "Switched to quiz: math.yaml") || !strings.Contains(log, " - webquiz.desktop - INFO - Opening "+run.url("/admin/")) {
		t.Errorf("log:\n%s", log)
	}
}

// sameFile tells whether two paths name the same file (on macOS a temp
// folder has two paths: /var/... and /private/var/...).
func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}
