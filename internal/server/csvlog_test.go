package server_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// oldConfig is a webquiz.yaml as a teacher kept it with WebQuiz 1.19:
// comments, blank lines and Ukrainian values.
const oldConfig = `# Налаштування WebQuiz
server:
  host: "127.0.0.1"   # тільки цей комп'ютер

paths:
  quizzes_dir: "quizzes"
  logs_dir: "logs"
  csv_dir: "data"
  static_dir: "static"

admin:
  master_key: "test123"

registration:
  fields:
    - "Клас"   # номер класу
`

// oldQuiz is a quiz file with a Ukrainian name and text.
const oldQuiz = `title: Математика
questions:
  - question: Скільки буде 2 + 2?
    options: ["3", "4"]
    correct_answer: 1
`

// Review Focus 3: a folder left by WebQuiz 1.19 already has CSV and log
// files. The new run takes the next free numbers and leaves the old files as
// they are.
func TestCSVNumberingContinuesAfterExistingFiles(t *testing.T) {
	// Not parallel: the folders of the config are relative to the working
	// folder, as in Python, and t.Chdir changes it for the whole program.
	dir := t.TempDir()
	files := map[string]string{
		"webquiz.yaml":                       oldConfig,
		"quizzes/Математика.yaml":            oldQuiz,
		"data/Математика_0001.csv":           "user_id,question,selected_answer,correct_answer,is_correct,time_taken_seconds\r\n123456,old,3,4,False,2.5\r\n",
		"data/Математика_0001.users.csv":     "old users 1\r\n",
		"data/Математика_0002.csv":           "old answers 2\r\n",
		"data/Математика_0002.users.csv":     "old users 2\r\n",
		"data/Математика_0003.users.csv":     "only the users file of run 3 is left\r\n",
		"data/Інший_0004.csv":                "another quiz\r\n",
		"logs/0001.log":                      "old log\n",
		"logs/0002.log":                      "old log\n",
		"static/index.html":                  "old page",
		"quizzes/imgs/.keep":                 "",
		"quizzes/attach/.keep":               "",
		"data/Математика_0005.answers.csv":   "not a run file\r\n",
		"data/Математика_0004.answers_x.csv": "not a run file\r\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Chdir(dir) // the relative paths of the config are taken from here, as in Python
	cfg, err := config.Load(filepath.Join(dir, "webquiz.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	srv, err := server.New(cfg, server.Options{Logger: discardLogger(&logs)})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	srv.Start(context.Background())
	client := &testutil.Client{BaseURL: httpServer.URL, HTTP: httpServer.Client()}

	if got := filepath.Base(srv.LogFile()); got != "0003.log" {
		t.Errorf("log file %s, want 0003.log", got)
	}
	res := client.PostJSON(t, "/api/register", map[string]any{"username": "Оля", "клас": "5-А"})
	if res.Status != 200 {
		t.Fatalf("register: %d %s", res.Status, res.Body)
	}
	userID := res.JSON(t)["user_id"].(string)
	if res := client.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 1}); res.Status != 200 {
		t.Fatalf("submit: %d %s", res.Status, res.Body)
	}
	srv.Close() // writes the CSV files

	answers := csvLines(t, filepath.Join(dir, "data", "Математика_0004.csv"))
	if len(answers) != 2 || answers[0] != "user_id,question,selected_answer,correct_answer,is_correct,time_taken_seconds" ||
		!strings.HasPrefix(answers[1], userID+",Скільки буде 2 + 2?,4,4,True,") {
		t.Errorf("new answers CSV:\n%s", strings.Join(answers, "\n"))
	}
	users := csvLines(t, filepath.Join(dir, "data", "Математика_0004.users.csv"))
	if len(users) != 2 || users[0] != "user_id,username,клас,registered_at,total_questions_asked,correct_answers,earned_points,total_points,total_time" ||
		!strings.HasPrefix(users[1], userID+",Оля,5-А,") || !strings.HasSuffix(users[1], ",1,1,1,1,0:00") {
		t.Errorf("new users CSV:\n%s", strings.Join(users, "\n"))
	}
	for name, content := range files {
		if strings.HasPrefix(name, "data/") || strings.HasPrefix(name, "logs/") {
			if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != content {
				t.Errorf("old file %s changed: %q", name, got)
			}
		}
	}
	if config, _ := os.ReadFile(filepath.Join(dir, "webquiz.yaml")); string(config) != oldConfig {
		t.Errorf("the config file changed:\n%s", config)
	}
}

// Review Focus 4: a checker that the Starlark checker cannot compile is
// reported in the log when the quiz loads; good checkers are not.
func TestQuizLoadWarnsAboutBadChecker(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"checkers.yaml": map[string]any{
		"title": "Checkers",
		"questions": []any{
			map[string]any{"question": "Good", "checker": "assert to_int(user_answer) == 4", "correct_value": "4"},
			map[string]any{"question": "Try", "checker": "try:\n    x = int(user_answer)\nexcept ValueError:\n    x = 0\nassert x == 4", "correct_value": "4"},
			map[string]any{"question": "Syntax", "checker": "if True\n    pass", "correct_value": "4"},
			map[string]any{"question": "Empty", "checker": "", "correct_value": "4"},
			map[string]any{"question": "Choice", "options": []any{"A"}, "correct_answer": 0},
		},
	}}})

	log := ts.ReadLog(t)
	path := filepath.Join(ts.QuizzesDir, "checkers.yaml")
	for _, want := range []string{
		" - WARNING - " + path + ": Question 2 checker does not compile, its answers will be marked wrong: 'try' is not supported in checker code (line 1)",
		" - WARNING - " + path + ": Question 3 checker does not compile, its answers will be marked wrong: ",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	for _, question := range []string{"Question 1 ", "Question 4 ", "Question 5 "} {
		if strings.Contains(log, question+"checker") {
			t.Errorf("no warning expected for %s:\n%s", question, log)
		}
	}

	// The quiz still runs: an answer to the broken checker is wrong.
	userID := ts.RegisterStudent(t, "Ann")
	ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": "4"})
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 2, "selected_answer": "4"})
	if data := res.JSON(t); res.Status != 200 || data["is_correct"] != false || data["checker_error"] == nil {
		t.Errorf("answer to the broken checker: %d %s", res.Status, res.Body)
	}

	// Loading the quiz again (a switch) warns again.
	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "checkers.yaml"}); res.Status != 200 {
		t.Fatalf("switch: %d %s", res.Status, res.Body)
	}
	if got := strings.Count(ts.ReadLog(t), "Question 2 checker does not compile"); got != 2 {
		t.Errorf("the warning was written %d times, want 2 (start and switch)", got)
	}
}
