package server_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Tests of the file manager API beyond the Python tests: saving a quiz file
// (PUT /api/files/quizzes/save/{filename}), viewing and downloading files,
// and the order of the file lists. The expected answers are those of
// server.py files_save_quiz, files_view and files_download.

const savedQuiz = "title: Edited\nquestions:\n- question: New?\n  options: [x, y]\n  correct_answer: 1\n"

// twoQuizServer runs a.yaml; b.yaml is another quiz file.
func twoQuizServer(t *testing.T) *testutil.TestServer {
	t.Helper()
	quiz := "title: %s\nquestions:\n- question: Q?\n  options: [a, b]\n  correct_answer: 0\n"
	return testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"a.yaml":       strings.Replace(quiz, "%s", "A", 1),
		"b.yaml":       strings.Replace(quiz, "%s", "B", 1),
		"default.yaml": strings.Replace(quiz, "%s", "Default", 1),
	}})
}

func TestSaveQuizFileWritesItAndKeepsABackup(t *testing.T) {
	t.Parallel()
	ts := twoQuizServer(t)
	path := filepath.Join(ts.QuizzesDir, "b.yaml")
	old := readTestFile(t, path)
	oldTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.Local)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	res := ts.AdminClient(t, "test123").Put(t, "/api/files/quizzes/save/b.yaml", map[string]any{"content": "\n  " + savedQuiz + "\n\n"})
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	want := map[string]any{"success": true, "message": `Quiz "b.yaml" saved successfully (backup created)`, "backup": path + ".backup"}
	if got := res.JSON(t); jsonText(got) != jsonText(want) {
		t.Errorf("reply %v, want %v", got, want)
	}
	// The content is saved stripped, as Python's .strip() did.
	if got := readTestFile(t, path); got != strings.TrimSpace(savedQuiz) {
		t.Errorf("saved %q", got)
	}
	if got := readTestFile(t, path+".backup"); got != old {
		t.Errorf("backup %q", got)
	}
	info, err := os.Stat(path + ".backup")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(oldTime) { // shutil.copy2 kept the time
		t.Errorf("backup time %v, want %v", info.ModTime(), oldTime)
	}
}

func TestSaveQuizFileErrors(t *testing.T) {
	t.Parallel()
	ts := twoQuizServer(t)
	admin := ts.AdminClient(t, "test123")
	cases := []struct {
		name, file string
		body       any
		status     int
		error      string
	}{
		{"no content", "b.yaml", map[string]any{}, 400, "Content is required"},
		{"only spaces", "b.yaml", map[string]any{"content": " \n\t "}, 400, "Content is required"},
		{"content first, then the name", "..b.yaml", map[string]any{"content": ""}, 400, "Content is required"},
		{"name with ..", "..b.yaml", map[string]any{"content": savedQuiz}, 400, "Invalid filename"},
		{"name with a slash", "x%2Fb.yaml", map[string]any{"content": savedQuiz}, 400, "Invalid filename"},
		{"no such file", "new.yaml", map[string]any{"content": savedQuiz}, 404, "Quiz file not found"},
		{"not JSON", "b.yaml", "not json", 500, ""},
		{"a list", "b.yaml", []any{"content"}, 500, "'list' object has no attribute 'get'"},
		{"null content", "b.yaml", map[string]any{"content": nil}, 500, "'NoneType' object has no attribute 'strip'"},
		{"number content", "b.yaml", map[string]any{"content": 5}, 500, "'int' object has no attribute 'strip'"},
	}
	for _, c := range cases {
		res := admin.Put(t, "/api/files/quizzes/save/"+c.file, c.body)
		if res.Status != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, res.Status, res.Body, c.status)
			continue
		}
		if text, _ := res.JSON(t)["error"].(string); c.error != "" && text != c.error {
			t.Errorf("%s: error %q, want %q", c.name, text, c.error)
		}
	}

	res := admin.Put(t, "/api/files/quizzes/save/b.yaml", map[string]any{"content": "questions: [unclosed"})
	if text, _ := res.JSON(t)["error"].(string); res.Status != 400 || !strings.HasPrefix(text, "Неправильний YAML: ") {
		t.Errorf("broken YAML: %d %s", res.Status, res.Body)
	}
	res = admin.Put(t, "/api/files/quizzes/save/b.yaml", map[string]any{"content": "title: no questions"})
	data := res.JSON(t)
	if res.Status != 400 || data["error"] != "Неправильна структура даних квізу" || jsonText(data["errors"]) != `["Квіз повинен містити поле 'questions'"]` {
		t.Errorf("invalid quiz: %d %s", res.Status, res.Body)
	}
	if _, err := os.Stat(filepath.Join(ts.QuizzesDir, "b.yaml.backup")); err == nil {
		t.Error("a refused save made a backup")
	}
}

// Saving the running quiz restarts it with the new content, like the quiz
// editor does, and keeps the admin's start settings; saving another quiz
// file leaves the running quiz and its students alone.
func TestSaveRunningQuizRestartsIt(t *testing.T) {
	t.Parallel()
	ts := twoQuizServer(t)
	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "a.yaml", "settings": map[string]any{"show_right_answer": false}}); res.Status != 200 {
		t.Fatalf("switch: %d %s", res.Status, res.Body)
	}
	userID := ts.RegisterStudent(t, "Ann")

	if res := admin.Put(t, "/api/files/quizzes/save/b.yaml", map[string]any{"content": savedQuiz}); res.Status != 200 {
		t.Fatalf("save b.yaml: %d %s", res.Status, res.Body)
	}
	if res := ts.GetJSON(t, "/api/verify-user/"+userID); res.Status != 200 || res.JSON(t)["valid"] != true {
		t.Fatalf("saving another quiz removed the student: %d %s", res.Status, res.Body)
	}
	if boot := pageBoot(t, readIndex(t, ts)); boot["title"] != "A" {
		t.Errorf("title %v", boot["title"])
	}

	if res := admin.Put(t, "/api/files/quizzes/save/a.yaml", map[string]any{"content": savedQuiz}); res.Status != 200 {
		t.Fatalf("save a.yaml: %d %s", res.Status, res.Body)
	}
	boot := pageBoot(t, readIndex(t, ts))
	if boot["title"] != "Edited" || boot["show_right_answer"] != false {
		t.Errorf("after the save: title %v, show_right_answer %v", boot["title"], boot["show_right_answer"])
	}
	if res := ts.GetJSON(t, "/api/verify-user/"+userID); res.JSON(t)["valid"] != false {
		t.Errorf("the restart kept the student: %s", res.Body)
	}
	if log := ts.ReadLog(t); !strings.Contains(log, "Reloading active quiz after edit: a.yaml") || !strings.Contains(log, "Saved quiz file: a.yaml") {
		t.Errorf("log:\n%s", log)
	}
}

func TestViewFiles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	writeTestFile(t, filepath.Join(ts.LogsDir, "lines.log"), "a\r\nb\rc\n")
	writeTestFile(t, filepath.Join(ts.LogsDir, "binary.log"), "\xff\xfe")
	if err := os.MkdirAll(filepath.Join(ts.LogsDir, "folder.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(ts.CSVDir, "big.csv")
	writeTestFile(t, big, "")
	if err := os.Truncate(big, 10*1024*1024+1); err != nil {
		t.Fatal(err)
	}

	// Python read the file in text mode: "\r\n" and "\r" became "\n".
	res := admin.GetJSON(t, "/api/files/logs/view/lines.log")
	if res.Status != 200 || res.Text() != "a\nb\nc\n" || res.Header.Get("Content-Disposition") != `inline; filename="lines.log"` {
		t.Errorf("lines.log: %d %q %q", res.Status, res.Body, res.Header.Get("Content-Disposition"))
	}
	for path, want := range map[string]string{
		"/api/files/logs/view/binary.log":   `{"error":"File contains non-UTF-8 content. Use download instead."}`,
		"/api/files/logs/view/folder.log":   `{"error":"Path is not a file"}`,
		"/api/files/static/view/index.html": `{"error":"Invalid file type"}`,
		"/api/files/csv/view/big.csv":       `{"error":"File too large for viewing (>10MB). Size: 10485761 bytes. Use download instead.","size":10485761}`,
	} {
		res := admin.GetJSON(t, path)
		if res.Status != 400 || res.Text() != want {
			t.Errorf("%s: %d %s, want %s", path, res.Status, res.Body, want)
		}
	}
}

func TestDownloadFiles(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	writeTestFile(t, filepath.Join(ts.CSVDir, "r.csv"), "a,b\r\n1,2\r\n")
	writeTestFile(t, filepath.Join(ts.LogsDir, "x.log"), "line\n")
	for path, want := range map[string][3]string{
		"/api/files/csv/download/r.csv":                {"text/csv", `attachment; filename="r.csv"`, "a,b\r\n1,2\r\n"},
		"/api/files/logs/download/x.log":               {"text/plain", `attachment; filename="x.log"`, "line\n"},
		"/api/files/quizzes/download/test_quiz.yaml":   {"text/yaml", `attachment; filename="test_quiz.yaml"`, readTestFile(t, filepath.Join(ts.QuizzesDir, "test_quiz.yaml"))},
		"/api/files/quizzes/download/test_quiz.yaml.x": {"", "", ""},
	} {
		res := admin.GetJSON(t, path)
		if want[0] == "" {
			if res.Status != 404 {
				t.Errorf("%s: %d", path, res.Status)
			}
			continue
		}
		if res.Status != 200 || res.Header.Get("Content-Type") != want[0] || res.Header.Get("Content-Disposition") != want[1] || res.Text() != want[2] {
			t.Errorf("%s: %d %q %q %q", path, res.Status, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"), res.Body)
		}
	}
	// A download can be resumed, as with aiohttp's FileResponse.
	res := admin.GetJSON(t, "/api/files/csv/download/r.csv", testutil.WithHeader("Range", "bytes=5-"))
	if res.Status != http.StatusPartialContent || res.Text() != "1,2\r\n" {
		t.Errorf("range: %d %q", res.Status, res.Body)
	}
}

func TestListFilesNewestFirst(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for i, name := range []string{"old.csv", "new.csv", "middle.csv"} {
		path := filepath.Join(ts.CSVDir, name)
		writeTestFile(t, path, strings.Repeat("x", i))
		modified := time.Date(2026, 3, 4, 5, 6, 7+i*0, 0, time.Local).Add(map[int]time.Duration{0: 0, 1: 2 * time.Hour, 2: time.Hour + 250*time.Millisecond}[i])
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	var names, times []string
	for _, f := range listFiles(t, ts.AdminClient(t, "test123")).CSV {
		if strings.HasSuffix(f.Name, ".csv") && !strings.HasPrefix(f.Name, "test_quiz") {
			names = append(names, f.Name)
			times = append(times, f.Modified.(string))
		}
	}
	if jsonText(names) != `["new.csv","middle.csv","old.csv"]` {
		t.Errorf("order %v", names)
	}
	if jsonText(times) != `["2026-03-04T07:06:07","2026-03-04T06:06:07.250000","2026-03-04T05:06:07"]` {
		t.Errorf("times %v", times)
	}
	var sizes []json.Number
	for _, f := range listFiles(t, ts.AdminClient(t, "test123")).CSV {
		if f.Name == "middle.csv" {
			sizes = append(sizes, f.Size.(json.Number))
		}
	}
	if len(sizes) != 1 || sizes[0] != "2" {
		t.Errorf("size of middle.csv: %v", sizes)
	}
}
