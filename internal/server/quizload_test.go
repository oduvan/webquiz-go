package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/timelimits"
)

// newTestServer makes a server in a temp folder with the given quiz files
// (name -> YAML text). It is for tests inside this package, which cannot use
// internal/testutil (that package imports this one).
func newTestServer(t *testing.T, quizzes map[string]string, change func(*config.Config, *Options)) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Admin.MasterKey = "test123"
	cfg.Admin.TrustedIPs = nil
	cfg.Paths = config.PathsConfig{
		QuizzesDir: filepath.Join(dir, "quizzes"),
		LogsDir:    filepath.Join(dir, "logs"),
		CSVDir:     filepath.Join(dir, "data"),
		StaticDir:  filepath.Join(dir, "static"),
	}
	if err := os.MkdirAll(cfg.Paths.QuizzesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range quizzes {
		if err := os.WriteFile(filepath.Join(cfg.Paths.QuizzesDir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if change != nil {
		change(cfg, &opts)
	}
	s, err := New(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

const twoQuestions = "title: %s\nquestions:\n- {question: Q1, options: [a, b], correct_answer: 0}\n- {question: Q2, options: [a, b], correct_answer: 1}\n"

func quizText(title string) string { return strings.Replace(twoQuestions, "%s", title, 1) }

func readQuizBoot(t *testing.T, s *Server) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.staticDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	var boot map[string]any
	if err := json.Unmarshal([]byte(bootText(t, string(data))), &boot); err != nil {
		t.Fatal(err)
	}
	return boot
}

// switch_quiz: the state of the old run is gone, the quiz page shows the
// new quiz, and the live stats pages hear "quiz_switched".
func TestSwitchQuiz(t *testing.T) {
	s := newTestServer(t, map[string]string{"a.yaml": quizText("Quiz A"), "default.yaml": quizText("Default")}, nil)
	if got := readQuizBoot(t, s)["title"]; got != "Default" {
		t.Fatalf("start quiz %v", got)
	}

	listener := &wsClient{send: make(chan []byte, 4), done: make(chan struct{})}
	s.liveStatsClients.add(listener, nil)
	defer s.liveStatsClients.done(listener)
	s.mu.Lock()
	s.users["123456"] = &User{ID: "123456", Username: "Ann"}
	s.forceAllCompleted = true
	s.mu.Unlock()

	if err := s.SwitchQuiz("a.yaml", nil); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if len(s.users) != 0 || s.forceAllCompleted || s.currentQuizName() != "a.yaml" || s.quiz.Title != "Quiz A" {
		t.Errorf("after the switch: users %v, force %v, quiz %q %q", s.users, s.forceAllCompleted, s.currentQuizName(), s.quiz.Title)
	}
	s.mu.Unlock()
	if got := readQuizBoot(t, s)["title"]; got != "Quiz A" {
		t.Errorf("quiz page title %v", got)
	}
	var message map[string]any
	if err := json.Unmarshal(<-listener.send, &message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != "quiz_switched" || message["current_quiz"] != "a.yaml" || message["total_questions"] != float64(2) ||
		message["message"] != "Quiz switched to: a.yaml" {
		t.Errorf("message %v", message)
	}
	questions := message["questions"].([]any)
	if first := questions[0].(map[string]any); first["correct_answer"] != float64(0) || first["id"] != float64(1) {
		t.Errorf("live stats get the whole questions: %v", first)
	}

	if err := s.SwitchQuiz("missing.yaml", nil); err == nil || err.Error() != "Quiz file not found: missing.yaml" {
		t.Errorf("missing file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.quizzesDir, "broken.yaml"), []byte("title: no questions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SwitchQuiz("broken.yaml", nil); err == nil {
		t.Error("a broken quiz must fail")
	}
	s.mu.Lock()
	if s.currentQuizName() != "a.yaml" {
		t.Errorf("a failed switch changed the running quiz to %q", s.currentQuizName())
	}
	s.mu.Unlock()
}

// Start settings override the quiz file for this run only; a restart of the
// same quiz without settings keeps them, another quiz drops them.
func TestStartSettings(t *testing.T) {
	quizA := "title: A\nshow_right_answer: true\ntime_limit: {per_question: 30}\nquestions:\n- {question: Q1, options: [a, b], correct_answer: 0, time_limit: 10}\n"
	s := newTestServer(t, map[string]string{"default.yaml": quizA, "b.yaml": quizText("B")}, nil)

	settings := map[string]any{"show_right_answer": false, "show_explanation": true, "time_limit": map[string]any{"whole_quiz": json.Number("600")}}
	if err := s.SwitchQuiz("default.yaml", settings); err != nil {
		t.Fatal(err)
	}
	settings["show_right_answer"] = true // the server keeps its own copy

	check := func(wantRight bool, wantLimit timelimits.Settings, wantQuestion int) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.quiz.ShowRightAnswer != wantRight || s.quiz.TimeLimit != wantLimit || s.quiz.QuestionTimeLimits[1] != wantQuestion {
			t.Errorf("show_right_answer %v, time limit %+v, question limit %d", s.quiz.ShowRightAnswer, s.quiz.TimeLimit, s.quiz.QuestionTimeLimits[1])
		}
	}
	check(false, timelimits.Settings{Mode: timelimits.ModeWholeQuiz, Seconds: 600}, 0)
	if boot := readQuizBoot(t, s); boot["show_right_answer"] != false || boot["quiz_time_limit"] != float64(600) {
		t.Errorf("quiz page: %v %v", boot["show_right_answer"], boot["quiz_time_limit"])
	}

	if err := s.SwitchQuiz("default.yaml", nil); err != nil { // a restart (config save) keeps them
		t.Fatal(err)
	}
	check(false, timelimits.Settings{Mode: timelimits.ModeWholeQuiz, Seconds: 600}, 0)

	if err := s.SwitchQuiz("default.yaml", map[string]any{"time_limit": nil}); err != nil {
		t.Fatal(err)
	}
	check(true, timelimits.NoLimit, 0)

	if err := s.SwitchQuiz("b.yaml", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SwitchQuiz("default.yaml", nil); err != nil { // another quiz in between dropped them
		t.Fatal(err)
	}
	check(true, timelimits.Settings{Mode: timelimits.ModePerQuestion, Seconds: 30}, 10)
}

// When the web build lacks the quiz page, static_dir/index.html is the
// template error page (templates/template_error.html).
func TestQuizPageTemplateError(t *testing.T) {
	var logs bytes.Buffer
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, func(_ *config.Config, opts *Options) {
		opts.Pages = fstest.MapFS{"admin.html": {Data: []byte(fakePage)}}
		opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	data, err := os.ReadFile(filepath.Join(s.staticDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Template Error") {
		t.Errorf("index.html:\n%s", data)
	}
	if !strings.Contains(logs.String(), "Failed to load template index.html") {
		t.Errorf("the log must say why:\n%s", logs.String())
	}
}

// Options: zero values give the defaults; given values are used.
func TestOptionsDefaults(t *testing.T) {
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, nil)
	if s.flushInterval != DefaultFlushInterval || s.checkInterval != time.Second || s.grace != 5*time.Second || s.now == nil {
		t.Errorf("defaults: flush %v, check %v, grace %v", s.flushInterval, s.checkInterval, s.grace)
	}
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s = newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, func(_ *config.Config, opts *Options) {
		opts.FlushInterval = 20 * time.Millisecond
		opts.CheckInterval = 30 * time.Millisecond
		opts.Grace = NoGrace
		opts.Now = func() time.Time { return clock }
	})
	if s.flushInterval != 20*time.Millisecond || s.checkInterval != 30*time.Millisecond || s.grace != 0 || !s.now().Equal(clock) {
		t.Errorf("given: flush %v, check %v, grace %v, now %v", s.flushInterval, s.checkInterval, s.grace, s.now())
	}
}
