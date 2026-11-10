package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_quiz_rename.py. Two quiz files are used wherever the Python
// test wanted no quiz to start by itself.

func dummyQuiz() map[string]any {
	return map[string]any{"title": "Dummy", "questions": []any{choice("Q", []any{"A", "B"}, 0)}}
}

func titledQuiz(title string) map[string]any {
	return map[string]any{"title": title, "questions": []any{choice("Q1", []any{"A", "B"}, 0)}}
}

// renameTo sends the wizard update of the Python tests.
func renameTo(t *testing.T, admin *testutil.Client, oldName string, newName any, quizData map[string]any) *testutil.Response {
	t.Helper()
	return admin.Put(t, "/api/admin/quiz/"+oldName, map[string]any{"filename": newName, "mode": "wizard", "quiz_data": quizData})
}

func getStatus(t *testing.T, admin *testutil.Client, name string) int {
	t.Helper()
	return admin.GetJSON(t, "/api/admin/quiz/"+name).Status
}

// py: tests/test_quiz_rename.py::test_rename_quiz_basic
func TestRenameQuizBasic(t *testing.T) {
	t.Parallel()
	quizData := map[string]any{"title": "Original Quiz", "questions": []any{choice("Q1", []any{"A", "B"}, 0), choice("Q2", []any{"C", "D"}, 1)}}
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"original_name.yaml": quizData, "dummy.yaml": dummyQuiz()}})
	res := renameTo(t, admin, "original_name.yaml", "renamed_quiz", quizData)
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || data["renamed"] != true || data["filename"] != "renamed_quiz.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "original_name.yaml"); got != 404 {
		t.Errorf("old quiz: %d, want 404", got)
	}
	get := admin.GetJSON(t, "/api/admin/quiz/renamed_quiz.yaml")
	if get.Status != 200 || get.JSON(t)["parsed"].(map[string]any)["title"] != "Original Quiz" {
		t.Errorf("new quiz: %d %s", get.Status, get.Body)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_with_extension
func TestRenameQuizWithExtension(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"test.yaml": titledQuiz("Test Quiz"), "dummy.yaml": dummyQuiz()}})
	res := renameTo(t, admin, "test.yaml", "renamed.yaml", titledQuiz("Test Quiz"))
	data := res.JSON(t)
	if res.Status != 200 || data["renamed"] != true || data["filename"] != "renamed.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "renamed.yaml"); got != 200 {
		t.Errorf("new quiz: %d", got)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_no_change
func TestRenameQuizNoChange(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"unchanged.yaml": titledQuiz("Original Title")}})
	res := renameTo(t, admin, "unchanged.yaml", "unchanged", titledQuiz("Updated Title"))
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || data["renamed"] != false || data["filename"] != "unchanged.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	get := admin.GetJSON(t, "/api/admin/quiz/unchanged.yaml")
	if get.Status != 200 || get.JSON(t)["parsed"].(map[string]any)["title"] != "Updated Title" {
		t.Errorf("%d %s", get.Status, get.Body)
	}
}

// py: tests/test_quiz_rename.py::test_rename_active_quiz_blocked
func TestRenameActiveQuizBlocked(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"active.yaml": titledQuiz("Active Quiz")}})
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "active.yaml"}); res.Status != 200 {
		t.Fatalf("switch: %d", res.Status)
	}
	res := renameTo(t, admin, "active.yaml", "renamed_active", titledQuiz("Active Quiz"))
	text := errorString(t, res)
	if res.Status != 409 || !strings.Contains(text, "Cannot rename active quiz") || !strings.Contains(text, "switch to a different quiz") {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "active.yaml"); got != 200 {
		t.Errorf("original quiz: %d, want 200", got)
	}
	if got := getStatus(t, admin, "renamed_active.yaml"); got != 404 {
		t.Errorf("renamed quiz: %d, want 404", got)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_filename_conflict
func TestRenameQuizFilenameConflict(t *testing.T) {
	t.Parallel()
	quizA := titledQuiz("Quiz A")
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz_a.yaml": quizA,
		"quiz_b.yaml": map[string]any{"title": "Quiz B", "questions": []any{choice("Q2", []any{"C", "D"}, 1)}},
	}})
	res := renameTo(t, admin, "quiz_a.yaml", "quiz_b", quizA)
	text := errorString(t, res)
	if res.Status != 409 || !strings.Contains(text, "already exists") || !strings.Contains(text, "quiz_b.yaml") {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	get := admin.GetJSON(t, "/api/admin/quiz/quiz_a.yaml")
	if get.Status != 200 || get.JSON(t)["parsed"].(map[string]any)["title"] != "Quiz A" {
		t.Errorf("quiz_a: %d %s", get.Status, get.Body)
	}
}

// py: tests/test_quiz_rename.py::test_rename_nonexistent_quiz
func TestRenameNonexistentQuiz(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := renameTo(t, admin, "nonexistent.yaml", "renamed", titledQuiz("Test"))
	if res.Status != 404 || !strings.Contains(strings.ToLower(errorString(t, res)), "not found") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_text_mode
func TestRenameQuizTextMode(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"text_quiz.yaml": titledQuiz("Text Mode Quiz"), "dummy.yaml": dummyQuiz()}})
	content := admin.GetJSON(t, "/api/admin/quiz/text_quiz.yaml").JSON(t)["content"]
	res := admin.Put(t, "/api/admin/quiz/text_quiz.yaml", map[string]any{"filename": "renamed_text_quiz", "mode": "text", "content": content})
	data := res.JSON(t)
	if res.Status != 200 || data["renamed"] != true || data["filename"] != "renamed_text_quiz.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "text_quiz.yaml"); got != 404 {
		t.Errorf("old quiz: %d", got)
	}
	if got := getStatus(t, admin, "renamed_text_quiz.yaml"); got != 200 {
		t.Errorf("new quiz: %d", got)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_preserves_content
func TestRenameQuizPreservesContent(t *testing.T) {
	t.Parallel()
	first := choice("Q1 with image", []any{"A", "B", "C"}, 1)
	first["image"] = "image1.jpg"
	first["points"] = 2
	quizData := map[string]any{
		"title": "Complex Quiz", "show_right_answer": true, "randomize_questions": true, "show_answers_on_completion": false,
		"questions": []any{first, map[string]any{"question": "Q2 text input", "default_value": "", "correct_value": "42", "checker": "answer == '42'"}},
	}
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"complex.yaml": quizData, "dummy.yaml": dummyQuiz()}})
	res := renameTo(t, admin, "complex.yaml", "complex_renamed", quizData)
	if res.Status != 200 || res.JSON(t)["renamed"] != true {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	parsed := parsedQuiz(t, admin, "complex_renamed.yaml")
	questions := parsed["questions"].([]any)
	if parsed["title"] != "Complex Quiz" || parsed["show_right_answer"] != true || parsed["randomize_questions"] != true ||
		parsed["show_answers_on_completion"] != false || len(questions) != 2 {
		t.Fatalf("%v", parsed)
	}
	q1, q2 := questions[0].(map[string]any), questions[1].(map[string]any)
	if q1["image"] != "image1.jpg" || !isNum(q1["points"], "2") || q2["checker"] != "answer == '42'" {
		t.Errorf("questions: %v", questions)
	}
}

// py: tests/test_quiz_rename.py::test_rename_after_switch_away
func TestRenameAfterSwitchAway(t *testing.T) {
	t.Parallel()
	quizA := titledQuiz("Quiz A")
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz_a.yaml": quizA,
		"quiz_b.yaml": map[string]any{"title": "Quiz B", "questions": []any{choice("Q2", []any{"C", "D"}, 1)}},
	}})
	switchTo := func(name string) {
		t.Helper()
		if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": name}); res.Status != 200 {
			t.Fatalf("switch to %s: %d", name, res.Status)
		}
	}
	switchTo("quiz_a.yaml")
	if res := renameTo(t, admin, "quiz_a.yaml", "quiz_a_renamed", quizA); res.Status != 409 {
		t.Fatalf("active rename: %d", res.Status)
	}
	switchTo("quiz_b.yaml")
	res := renameTo(t, admin, "quiz_a.yaml", "quiz_a_renamed", quizA)
	data := res.JSON(t)
	if res.Status != 200 || data["renamed"] != true || data["filename"] != "quiz_a_renamed.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "quiz_a_renamed.yaml"); got != 200 {
		t.Errorf("new quiz: %d", got)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_empty_filename
func TestRenameQuizEmptyFilename(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"test.yaml": titledQuiz("Test Quiz"), "dummy.yaml": dummyQuiz()}})
	res := renameTo(t, admin, "test.yaml", "", titledQuiz("Test Quiz"))
	data := res.JSON(t)
	if res.Status != 200 || data["renamed"] != false || data["filename"] != "test.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "test.yaml"); got != 200 {
		t.Errorf("quiz: %d", got)
	}
}

// py: tests/test_quiz_rename.py::test_rename_quiz_yml_extension
func TestRenameQuizYMLExtension(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"test.yaml": titledQuiz("Test Quiz"), "dummy.yaml": dummyQuiz()}})
	res := renameTo(t, admin, "test.yaml", "renamed.yml", titledQuiz("Test Quiz"))
	data := res.JSON(t)
	if res.Status != 200 || data["renamed"] != true || data["filename"] != "renamed.yml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if got := getStatus(t, admin, "renamed.yml"); got != 200 {
		t.Errorf("new quiz: %d", got)
	}
}
