package server_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// pageBoot reads the boot JSON of an HTML page.
func pageBoot(t *testing.T, html string) map[string]any {
	t.Helper()
	const open = `<script id="webquiz-boot" type="application/json">`
	start := strings.Index(html, open)
	if start < 0 {
		t.Fatalf("no boot script in the page:\n%s", html)
	}
	rest := html[start+len(open):]
	end := strings.Index(rest, "</script>")
	if end < 0 {
		t.Fatal("the boot script is not closed")
	}
	var boot map[string]any
	decoder := json.NewDecoder(strings.NewReader(rest[:end]))
	decoder.UseNumber()
	if err := decoder.Decode(&boot); err != nil {
		t.Fatalf("boot is not JSON: %v\n%s", err, rest[:end])
	}
	return boot
}

func readIndex(t *testing.T, ts *testutil.TestServer) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ts.StaticDir, "index.html"))
	if err != nil {
		t.Fatalf("static_dir/index.html: %v", err)
	}
	return string(data)
}

func question(t *testing.T, boot map[string]any, index int) map[string]any {
	t.Helper()
	questions, ok := boot["questions"].([]any)
	if !ok || index >= len(questions) {
		t.Fatalf("no question %d in boot questions %v", index, boot["questions"])
	}
	return questions[index].(map[string]any)
}

func jsonText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// The server writes static_dir/index.html when it starts (Python's
// create_default_index_html) and serves it at "/".
func TestQuizPageWrittenOnStart(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	html := readIndex(t, ts)

	if !strings.Contains(html, "<title>Test Quiz</title>") {
		t.Errorf("title missing:\n%s", html)
	}
	if !strings.Contains(html, `src="./assets/`) {
		t.Errorf("the quiz page must load ./assets/ (relative):\n%s", html)
	}
	if strings.Contains(html, `"correct_answer":`) || strings.Contains(html, `"correct_answer"`) {
		t.Errorf("the page must not hold correct answers:\n%s", html)
	}
	boot := pageBoot(t, html)
	checks := map[string]any{
		"state":             "quiz",
		"title":             "Test Quiz",
		"language":          "uk",
		"show_right_answer": true,
		"show_final_list":   true,
		"username_label":    "Ім'я користувача",
	}
	for key, want := range checks {
		if boot[key] != want {
			t.Errorf("boot %s = %v, want %v", key, boot[key], want)
		}
	}
	if jsonText(boot["quiz_time_limit"]) != "0" || jsonText(boot["registration_fields"]) != "[]" || jsonText(boot["quizzes"]) != "[]" {
		t.Errorf("boot: %s", jsonText(boot))
	}
	if version, _ := boot["version"].(string); version == "" {
		t.Errorf("boot version missing: %v", boot["version"])
	}
	translations, _ := boot["translations"].(map[string]any)
	if translations["submit_answer"] == nil || translations["register_button"] == nil {
		t.Errorf("boot translations incomplete: %v", translations)
	}
	q := question(t, boot, 0)
	if jsonText(q) != `{"id":1,"is_multiple_choice":false,"options":["3","4","5","6"],"points":1,"question":"What is 2 + 2?","time_limit":0}` {
		t.Errorf("question 1: %s", jsonText(q))
	}

	res := ts.GetJSON(t, "/")
	if res.Status != 200 || res.Header.Get("Content-Type") != "text/html; charset=utf-8" || res.Text() != html {
		t.Errorf("GET / = %d %q, want the index file", res.Status, res.Header.Get("Content-Type"))
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("GET / has no ETag")
	}
	if again := ts.GetJSON(t, "/", testutil.WithHeader("If-None-Match", etag)); again.Status != 304 {
		t.Errorf("GET / with the same ETag: %d, want 304", again.Status)
	}
	// Writing the page again (a quiz switch) gives a new tag, even within the same second.
	if err := os.WriteFile(filepath.Join(ts.StaticDir, "index.html"), []byte(html+" "), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := ts.GetJSON(t, "/", testutil.WithHeader("If-None-Match", etag)); again.Status != 200 {
		t.Errorf("GET / after a new page with the old ETag: %d, want 200", again.Status)
	}
}

// Question data for the page, as create_default_index_html built it.
func TestQuizPageQuestions(t *testing.T) {
	t.Parallel()
	quizText := `title: Shapes
time_limit:
  per_question: 30
questions:
- question: Pick one
  options: [a, b]
  correct_answer: 0
  image: /imgs/q.png
  file: data.xlsx
  points: 0.5
- question: Pick two
  options: [a, b, c]
  correct_answer: [0, 2]
  time_limit: 45
- question: Pick at least one
  options: [a, b, c]
  correct_answer: [0, 1]
  min_correct: 1
  file: /attach/x.pdf
- question: Type it
  checker: "assert user_answer == 'x'"
  default_value: start
  correct_value: x
  points: 2.0
- options: [/imgs/a.png, /imgs/b.png]
  correct_answer: 1
  question: ""
  image: ""
`
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"shapes.yaml": quizText}})
	boot := pageBoot(t, readIndex(t, ts))
	want := []string{
		`{"file":"/attach/data.xlsx","id":1,"image":"/imgs/q.png","is_multiple_choice":false,"options":["a","b"],"points":0.5,"question":"Pick one","time_limit":30}`,
		`{"id":2,"is_multiple_choice":true,"min_correct":2,"options":["a","b","c"],"points":1,"question":"Pick two","time_limit":45}`,
		`{"file":"/attach/x.pdf","id":3,"is_multiple_choice":true,"min_correct":1,"options":["a","b","c"],"points":1,"question":"Pick at least one","time_limit":30}`,
		`{"default_value":"start","id":4,"points":2,"question":"Type it","time_limit":30,"type":"text"}`,
		`{"id":5,"is_multiple_choice":false,"options":["/imgs/a.png","/imgs/b.png"],"points":1,"time_limit":30}`,
	}
	for i, w := range want {
		if got := jsonText(question(t, boot, i)); got != w {
			t.Errorf("question %d:\n got %s\nwant %s", i+1, got, w)
		}
	}
	if jsonText(boot["quiz_time_limit"]) != "0" {
		t.Errorf("per_question mode has no whole-quiz limit, got %v", boot["quiz_time_limit"])
	}

	whole := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"w.yaml": "time_limit: {whole_quiz: 600}\nquestions:\n- {question: Q, options: [a, b], correct_answer: 0, time_limit: 5}\n"}})
	boot = pageBoot(t, readIndex(t, whole))
	if jsonText(boot["quiz_time_limit"]) != "600" || jsonText(question(t, boot, 0)["time_limit"]) != "0" {
		t.Errorf("whole_quiz: %s", jsonText(boot))
	}
}

// Registration labels and the interface language come from the config.
func TestQuizPageRegistrationAndLanguage(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"language": "en",
		"registration": map[string]any{
			"username_label":  "Name <full>",
			"fields":          []any{"Group <class>", "School Name"},
			"disabled_fields": []any{"Hidden"},
		},
	}})
	html := readIndex(t, ts)
	if strings.Contains(html, "<full>") || strings.Contains(html, "<class>") {
		t.Errorf("labels must not appear as raw HTML:\n%s", html)
	}
	boot := pageBoot(t, html)
	if boot["language"] != "en" || boot["username_label"] != "Name <full>" {
		t.Errorf("boot: %s", jsonText(boot))
	}
	if got := jsonText(boot["registration_fields"]); got != `[{"label":"Group \u003cclass\u003e","name":"group_\u003cclass\u003e"},{"label":"School Name","name":"school_name"}]` {
		t.Errorf("registration_fields %s", got)
	}
	if boot["translations"].(map[string]any)["submit_answer"] != "Submit answer" {
		t.Errorf("English texts expected, got %v", boot["translations"].(map[string]any)["submit_answer"])
	}

	plain := testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}})
	if got := pageBoot(t, readIndex(t, plain))["username_label"]; got != "Username" {
		t.Errorf("default English username label %v", got)
	}
}

// Several quiz files and none called default.yaml: the page asks the admin
// to choose (quiz_selection_required.html in Python).
func TestSelectionPageForSeveralQuizzes(t *testing.T) {
	t.Parallel()
	q := map[string]any{"questions": []any{map[string]any{"question": "Q?", "options": []any{"A", "B"}, "correct_answer": 0}}}
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"first.yaml":  map[string]any{"title": "First <one>", "questions": q["questions"]},
		"second.yml":  q,
		"third.yaml":  map[string]any{"title": 7, "questions": q["questions"]},
		"notes.txt":   "not a quiz",
		"broken.yaml": "title: [unclosed",
	}})
	html := readIndex(t, ts)
	if !strings.Contains(html, "<title>WebQuiz - Admin Selection Required</title>") || strings.Contains(html, "<one>") {
		t.Errorf("selection page:\n%s", html)
	}
	boot := pageBoot(t, html)
	if boot["state"] != "quiz_selection_required" || jsonText(boot["questions"]) != "[]" {
		t.Errorf("boot: %s", jsonText(boot))
	}
	want := `[{"filename":"broken.yaml","title":null},{"filename":"first.yaml","title":"First \u003cone\u003e"},{"filename":"second.yml","title":null},{"filename":"third.yaml","title":"7"}]`
	if got := jsonText(boot["quizzes"]); got != want {
		t.Errorf("quizzes\n got %s\nwant %s", got, want)
	}
}

func TestDefaultYAMLIsChosenAmongSeveral(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"default.yaml", "default.yml"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := func(title string) map[string]any {
				return map[string]any{"title": title, "questions": []any{map[string]any{"question": "Q?", "options": []any{"A", "B"}, "correct_answer": 0}}}
			}
			ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
				"a.yaml": q("A quiz"),
				name:     q("The default"),
				"z.yaml": q("Z quiz"),
			}})
			if boot := pageBoot(t, readIndex(t, ts)); boot["state"] != "quiz" || boot["title"] != "The default" {
				t.Errorf("boot: %s", jsonText(boot))
			}
		})
	}
}

// No quiz files: the server writes default.yaml exactly like the Python
// version (yaml.dump of the same data) and starts it.
func TestNoQuizFilesCreatesTheDefaultQuiz(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{}})
	data, err := os.ReadFile(filepath.Join(ts.QuizzesDir, "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "questions:\n- correct_answer: 1\n  options:\n  - '3'\n  - '4'\n  - '5'\n  - '6'\n  question: Скільки буде 2 + 2?\n" +
		"- correct_answer: 2\n  options:\n  - Харків\n  - Львів\n  - Київ\n  - Одеса\n  question: Яка столиця України?\n" +
		"show_right_answer: true\ntitle: Тестовий Quiz\n"
	if string(data) != want {
		t.Errorf("default.yaml:\n%s\nwant:\n%s", data, want)
	}
	if boot := pageBoot(t, readIndex(t, ts)); boot["title"] != "Тестовий Quiz" || len(boot["questions"].([]any)) != 2 {
		t.Errorf("boot: %s", jsonText(boot))
	}
	for _, dir := range []string{"imgs", "attach"} {
		if info, err := os.Stat(filepath.Join(ts.QuizzesDir, dir)); err != nil || !info.IsDir() {
			t.Errorf("quizzes/%s must exist: %v", dir, err)
		}
	}
}

// Like Python, the server does not start when the quiz it must load is
// broken: New returns the error.
func TestBrokenQuizStopsTheStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Paths = config.PathsConfig{
		QuizzesDir: filepath.Join(dir, "quizzes"),
		LogsDir:    filepath.Join(dir, "logs"),
		CSVDir:     filepath.Join(dir, "data"),
		StaticDir:  filepath.Join(dir, "static"),
	}
	if err := os.MkdirAll(cfg.Paths.QuizzesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Paths.QuizzesDir, "only.yaml"), []byte("title: no questions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	srv, err := server.New(cfg, server.Options{Logger: discardLogger(&log)})
	if err == nil {
		srv.Close()
		t.Fatal("New must fail for a quiz without questions")
	}
	if !strings.Contains(err.Error(), "questions") {
		t.Errorf("error %q should name the problem", err)
	}
}
