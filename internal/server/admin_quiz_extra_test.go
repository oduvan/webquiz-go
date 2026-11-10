package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Tests for parts of the admin quiz API that no Python test covers.

func TestListFilesAndCheckerTemplates(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Config: map[string]any{
		"checker_templates": []any{map[string]any{"name": "Number", "code": "to_int(answer) == 42"}},
	}})
	if res := admin.GetJSON(t, "/api/admin/list-files"); res.Status != 200 || len(res.JSON(t)["files"].([]any)) != 0 {
		t.Errorf("empty list-files: %d %s", res.Status, res.Body)
	}
	attach := filepath.Join(ts.QuizzesDir, "attach")
	if err := os.MkdirAll(filepath.Join(attach, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"b.xlsx": "12345", "A.txt": "x"} {
		if err := os.WriteFile(filepath.Join(attach, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := admin.GetJSON(t, "/api/admin/list-files").JSON(t)["files"].([]any)
	if len(files) != 2 { // the folder is not listed
		t.Fatalf("files: %v", files)
	}
	first, second := files[0].(map[string]any), files[1].(map[string]any)
	if first["filename"] != "A.txt" || first["path"] != "/attach/A.txt" || !isNum(first["size"], "1") ||
		second["filename"] != "b.xlsx" || !isNum(second["size"], "5") {
		t.Errorf("files: %v", files)
	}

	res := admin.GetJSON(t, "/api/admin/list-checker-templates")
	templates := res.JSON(t)["templates"].([]any)
	if res.Status != 200 || len(templates) != 1 || templates[0].(map[string]any)["name"] != "Number" ||
		templates[0].(map[string]any)["code"] != "to_int(answer) == 42" {
		t.Errorf("templates: %d %s", res.Status, res.Body)
	}
	plain := testutil.StartServer(t, testutil.Options{})
	if res := plain.AdminClient(t, "test123").GetJSON(t, "/api/admin/list-checker-templates"); string(res.Body) != `{"templates":[]}` {
		t.Errorf("no templates: %s", res.Body)
	}
}

// A quiz name with a folder part never reaches a file outside quizzes_dir.
func TestQuizNamesStayInsideTheQuizzesFolder(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	outside := filepath.Join(filepath.Dir(ts.QuizzesDir), "secret.yaml")
	if err := os.WriteFile(outside, []byte("title: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := admin.GetJSON(t, "/api/admin/quiz/..%2Fsecret.yaml"); res.Status != 404 {
		t.Errorf("get: %d %s", res.Status, res.Body)
	}
	if res := admin.Delete(t, "/api/admin/quiz/..%2Fsecret.yaml"); res.Status != 404 {
		t.Errorf("delete: %d %s", res.Status, res.Body)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the file outside was touched: %v", err)
	}
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "../evil", "mode": "wizard", "quiz_data": oneQuestionQuiz("T", "Q"),
	})
	if res.Status != 400 {
		t.Errorf("create: %d %s", res.Status, res.Body)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ts.QuizzesDir), "evil.yaml")); err == nil {
		t.Error("create wrote outside the quizzes folder")
	}
}

// The admin pages hear about "Show answers", with the Ukrainian text.
func TestForceShowAnswersTellsTheAdminSocket(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	socket := openSocket(t, ts, "/ws/admin")
	socket.next() // the first message of the admin socket
	admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	message := socket.next()
	if message["type"] != "answers_forced" || message["forced"] != true || message["message"] != "Відповіді тепер доступні для всіх учнів" {
		t.Errorf("message: %v", message)
	}
}

// Floats keep their ".0" through a save and through GET, as with PyYAML and
// json.dumps; a whole-number time limit stays a whole number.
func TestSavedQuizKeepsNumberTypes(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	question := choice("Q?", []any{"A", "B"}, 0)
	question["points"] = json.Number("2.0")
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "numbers", "mode": "wizard",
		"quiz_data": map[string]any{"title": "yes", "time_limit": map[string]any{"per_question": 30}, "questions": []any{question}},
	})
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	data, _ := os.ReadFile(filepath.Join(ts.QuizzesDir, "numbers.yaml"))
	text := string(data)
	if !strings.Contains(text, "points: 2.0") || !strings.Contains(text, "per_question: 30\n") || !strings.Contains(text, "title: \"yes\"") && !strings.Contains(text, "title: 'yes'") {
		t.Errorf("file:\n%s", text)
	}
	get := admin.GetJSON(t, "/api/admin/quiz/numbers.yaml")
	if !strings.Contains(get.Text(), `"points":2.0`) || !strings.Contains(get.Text(), `"per_question":30`) {
		t.Errorf("GET body: %s", get.Body)
	}
}

// A broken YAML file still opens in the editor, with the YAML error.
func TestGetQuizWithBrokenYAML(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"broken.yaml": "title: [oops\r\nquestions: []\r\n", "other.yaml": oneQuestionQuiz("Other", "Q?"), // two files: none starts by itself
	}})
	res := admin.GetJSON(t, "/api/admin/quiz/broken.yaml")
	data := res.JSON(t)
	if _, has := data["parsed"]; res.Status != 200 || !has || data["parsed"] != nil || data["yaml_error"] == nil ||
		data["content"] != "title: [oops\nquestions: []\n" { // line ends are read as text mode does
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// Saving a quiz that cannot start keeps the file saved; deleting leaves a
// backup that holds the old text.
func TestUpdateWritesBackupWithTheOldText(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"one.yaml": oneQuestionQuiz("Old title", "Q?"),
		"two.yaml": oneQuestionQuiz("Two", "Q?"),
	}})
	before, _ := os.ReadFile(filepath.Join(ts.QuizzesDir, "one.yaml"))
	res := admin.Put(t, "/api/admin/quiz/one.yaml", map[string]any{"mode": "wizard", "quiz_data": oneQuestionQuiz("New title", "Q?")})
	if res.Status != 200 || res.JSON(t)["backup_created"] != "one.yaml.backup" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	backup, _ := os.ReadFile(filepath.Join(ts.QuizzesDir, "one.yaml.backup"))
	if string(backup) != string(before) {
		t.Errorf("backup %q, want %q", backup, before)
	}
	// A bad save answers 400 and leaves the file as it was.
	bad := admin.Put(t, "/api/admin/quiz/one.yaml", map[string]any{"mode": "wizard", "quiz_data": map[string]any{"title": "x"}})
	if bad.Status != 400 || errorString(t, bad) != "Неправильна структура даних квізу" {
		t.Errorf("bad save: %d %s", bad.Status, bad.Body)
	}
	if got := readQuizFile(t, ts, "one.yaml")["title"]; got != "New title" {
		t.Errorf("title after a bad save: %v", got)
	}
}
