package server_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_admin_quiz_management.py.

// py: tests/test_admin_quiz_management.py::test_admin_create_quiz_wizard_mode
func TestAdminCreateQuizWizardMode(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "math_basics", "mode": "wizard",
		"quiz_data": map[string]any{
			"title": "Math Basics", "description": "Basic mathematics quiz",
			"questions": []any{
				choice("What is 5 + 3?", []any{"6", "7", "8", "9"}, 2),
				choice("What is 10 - 4?", []any{"5", "6", "7", "8"}, 1),
			},
		},
	})
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || data["filename"] != "math_basics.yaml" ||
		!strings.Contains(data["message"].(string), "math_basics.yaml") {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	saved := readQuizFile(t, ts, "math_basics.yaml")
	if saved["title"] != "Math Basics" || len(saved["questions"].([]any)) != 2 {
		t.Errorf("saved file: %v", saved)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_create_quiz_text_mode
func TestAdminCreateQuizTextMode(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	content := `title: Science Quiz
description: Basic science questions
questions:
  - question: What is H2O?
    options:
      - Oxygen
      - Water
      - Hydrogen
      - Salt
    correct_answer: 1
  - question: How many planets in our solar system?
    options:
      - "7"
      - "8"
      - "9"
      - "10"
    correct_answer: 1
`
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{"filename": "science_quiz", "mode": "text", "content": content})
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || !strings.Contains(data["message"].(string), "science_quiz.yaml") {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// Text mode writes the text as it is (without the end whitespace).
	written, err := os.ReadFile(filepath.Join(ts.QuizzesDir, "science_quiz.yaml"))
	if err != nil || string(written) != strings.TrimSpace(content) {
		t.Errorf("written file %q, %v", written, err)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_create_quiz_already_exists
func TestAdminCreateQuizAlreadyExists(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"existing_quiz.yaml": oneQuestionQuiz("Existing Quiz", "Test question?")}})
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "existing_quiz", "mode": "wizard",
		"quiz_data": map[string]any{"title": "Duplicate Quiz", "questions": []any{choice("New?", []any{"Yes", "No"}, 0)}},
	})
	if res.Status != 409 || !strings.Contains(errorString(t, res), "already exists") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_get_quiz_content
func TestAdminGetQuizContent(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"geography.yaml": map[string]any{
		"title":     "Geography Quiz",
		"questions": []any{choice("Capital of France?", []any{"London", "Paris", "Berlin", "Madrid"}, 1)},
	}}})
	res := admin.GetJSON(t, "/api/admin/quiz/geography.yaml")
	data := res.JSON(t)
	if res.Status != 200 || data["filename"] != "geography.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	content, _ := data["content"].(string)
	parsed, _ := data["parsed"].(map[string]any)
	if !strings.Contains(content, "Geography Quiz") || parsed["title"] != "Geography Quiz" || len(parsed["questions"].([]any)) != 1 {
		t.Errorf("content %q parsed %v", content, parsed)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_get_nonexistent_quiz
func TestAdminGetNonexistentQuiz(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := admin.GetJSON(t, "/api/admin/quiz/nonexistent.yaml")
	if res.Status != 404 || !strings.Contains(errorString(t, res), "not found") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// getSaved reads a quiz through the API and parses the YAML text it returns.
func getContent(t *testing.T, admin *testutil.Client, name string) string {
	t.Helper()
	res := admin.GetJSON(t, "/api/admin/quiz/"+name)
	if res.Status != 200 {
		t.Fatalf("get %s: %d %s", name, res.Status, res.Body)
	}
	return res.JSON(t)["content"].(string)
}

// py: tests/test_admin_quiz_management.py::test_admin_update_quiz_wizard_mode
func TestAdminUpdateQuizWizardMode(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"update_test.yaml": oneQuestionQuiz("Original Quiz", "Original question?")}})
	res := admin.Put(t, "/api/admin/quiz/update_test.yaml", map[string]any{
		"mode": "wizard",
		"quiz_data": map[string]any{
			"title": "Updated Quiz", "questions": []any{choice("Updated question?", []any{"X", "Y", "Z"}, 1)},
		},
	})
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || !strings.Contains(data["message"].(string), "updated successfully") || data["backup_created"] == nil {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	getContent(t, admin, "update_test.yaml")
	saved := readQuizFile(t, ts, "update_test.yaml")
	question := saved["questions"].([]any)[0].(map[string]any)
	if saved["title"] != "Updated Quiz" || question["question"] != "Updated question?" ||
		!slices.Equal(toStrings(question["options"]), []string{"X", "Y", "Z"}) || question["correct_answer"] != 1 {
		t.Errorf("saved file: %v", saved)
	}
	if _, err := os.Stat(filepath.Join(ts.QuizzesDir, "update_test.yaml.backup")); err != nil {
		t.Errorf("no backup: %v", err)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, item := range v.([]any) {
		out = append(out, item.(string))
	}
	return out
}

// py: tests/test_admin_quiz_management.py::test_admin_update_quiz_text_mode
func TestAdminUpdateQuizTextMode(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"text_update.yaml": oneQuestionQuiz("Text Update Test", "Before update?")}})
	res := admin.Put(t, "/api/admin/quiz/text_update.yaml", map[string]any{"mode": "text", "content": `title: Updated via Text
questions:
  - question: After update?
    options: ['X', 'Y', 'Z']
    correct_answer: 2
`})
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || data["backup_created"] == nil {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	saved := readQuizFile(t, ts, "text_update.yaml")
	question := saved["questions"].([]any)[0].(map[string]any)
	if saved["title"] != "Updated via Text" || question["question"] != "After update?" ||
		!slices.Equal(toStrings(question["options"]), []string{"X", "Y", "Z"}) || question["correct_answer"] != 2 {
		t.Errorf("saved file: %v", saved)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_delete_quiz
func TestAdminDeleteQuiz(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"delete_me.yaml": oneQuestionQuiz("Quiz to Delete", "Will be deleted?"),
		"keep_me.yaml":   oneQuestionQuiz("Quiz to Keep", "Will stay?"),
	}})
	res := admin.Delete(t, "/api/admin/quiz/delete_me.yaml")
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || !strings.Contains(data["message"].(string), "deleted successfully") ||
		data["backup_created"] != "delete_me.yaml.deleted_backup" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if _, err := os.Stat(filepath.Join(ts.QuizzesDir, "delete_me.yaml")); !os.IsNotExist(err) {
		t.Error("the quiz file is still there")
	}
	if _, err := os.Stat(filepath.Join(ts.QuizzesDir, "delete_me.yaml.deleted_backup")); err != nil {
		t.Errorf("no backup: %v", err)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_delete_active_quiz
func TestAdminDeleteActiveQuiz(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"active_quiz.yaml": oneQuestionQuiz("Active Quiz", "Am I active?")}})
	res := admin.Delete(t, "/api/admin/quiz/active_quiz.yaml")
	if res.Status != 400 || !strings.Contains(errorString(t, res), "currently active") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_delete_nonexistent_quiz
func TestAdminDeleteNonexistentQuiz(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := admin.Delete(t, "/api/admin/quiz/nonexistent.yaml")
	if res.Status != 404 || !strings.Contains(errorString(t, res), "not found") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

func validate(t *testing.T, admin *testutil.Client, content string) map[string]any {
	t.Helper()
	res := admin.PostJSON(t, "/api/admin/validate-quiz", map[string]any{"content": content})
	if res.Status != 200 {
		t.Fatalf("validate: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

func errorList(data map[string]any) []string {
	items, _ := data["errors"].([]any)
	var out []string
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}

func anyContains(list []string, part string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.Contains(s, part) })
}

// py: tests/test_admin_quiz_management.py::test_validate_quiz_valid_structure
func TestValidateQuizValidStructure(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	data := validate(t, admin, `title: Valid Quiz
questions:
  - question: Is this valid?
    options: ['Yes', 'No', 'Maybe']
    correct_answer: 0
  - question: Another question?
    options: ['A', 'B']
    correct_answer: 1
`)
	if data["valid"] != true || data["question_count"].(interface{ String() string }).String() != "2" || data["parsed"] == nil {
		t.Errorf("%v", data)
	}
}

// py: tests/test_admin_quiz_management.py::test_validate_quiz_missing_or_empty_questions
func TestValidateQuizMissingOrEmptyQuestions(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	missing := validate(t, admin, "title: Invalid Quiz\ndescription: This quiz has no questions\n")
	if missing["valid"] != false || !anyContains(errorList(missing), "questions") {
		t.Errorf("missing questions: %v", missing)
	}
	empty := validate(t, admin, "title: Empty Quiz\nquestions: []\n")
	if empty["valid"] != false || !anyContains(errorList(empty), "принаймні одне питання") {
		t.Errorf("empty questions: %v", empty)
	}
}

// py: tests/test_admin_quiz_management.py::test_validate_quiz_invalid_yaml
func TestValidateQuizInvalidYAML(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	data := validate(t, admin, `title: Invalid YAML
questions:
  - question: What's wrong here?
    options: ['A', 'B'
    correct_answer: 0  # Missing closing bracket above
`)
	if data["valid"] != false || !anyContains(errorList(data), "YAML syntax error") {
		t.Errorf("%v", data)
	}
}

// py: tests/test_admin_quiz_management.py::test_validate_quiz_invalid_question_structure
func TestValidateQuizInvalidQuestionStructure(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	incomplete := validate(t, admin, `title: Incomplete Quiz
questions:
  - question: Where are my options?
    correct_answer: 0
  - options: ['A', 'B', 'C']
    # Missing question and correct_answer
`)
	if incomplete["valid"] != false || len(errorList(incomplete)) == 0 {
		t.Errorf("incomplete: %v", incomplete)
	}
	badIndex := validate(t, admin, `title: Invalid Index Quiz
questions:
  - question: What's the answer?
    options: ['A', 'B']
    correct_answer: 5  # Out of range!
`)
	if badIndex["valid"] != false || !anyContains(errorList(badIndex), "out of range") {
		t.Errorf("index: %v", badIndex)
	}
}

// py: tests/test_admin_quiz_management.py::test_validate_quiz_image_only_questions
func TestValidateQuizImageOnlyQuestions(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	data := validate(t, admin, `title: Image Quiz
questions:
  - image: /imgs/question1.png
    options: ['A', 'B', 'C']
    correct_answer: 1
  - question: Text question too
    options: ['X', 'Y']
    correct_answer: 0
`)
	if data["valid"] != true || data["question_count"].(interface{ String() string }).String() != "2" {
		t.Errorf("%v", data)
	}
}

// py: tests/test_admin_quiz_management.py::test_update_active_quiz_affects_server_state
func TestUpdateActiveQuizAffectsServerState(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"active_quiz.yaml": oneQuestionQuiz("Active Quiz Original", "Original active question?")}})
	list := admin.GetJSON(t, "/api/admin/list-quizzes")
	if list.JSON(t)["current_quiz"] != "active_quiz.yaml" {
		t.Fatalf("list: %s", list.Body)
	}
	res := admin.Put(t, "/api/admin/quiz/active_quiz.yaml", map[string]any{
		"mode": "wizard",
		"quiz_data": map[string]any{
			"title": "Active Quiz Updated", "questions": []any{choice("Updated active question?", []any{"X", "Y", "Z", "W"}, 2)},
		},
	})
	if res.Status != 200 || res.JSON(t)["success"] != true {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	saved := readQuizFile(t, ts, "active_quiz.yaml")
	question := saved["questions"].([]any)[0].(map[string]any)
	if saved["title"] != "Active Quiz Updated" || question["question"] != "Updated active question?" || len(question["options"].([]any)) != 4 {
		t.Errorf("saved file: %v", saved)
	}
	// The running quiz was reloaded: the quiz page carries the new question.
	page := ts.GetJSON(t, "/")
	if !strings.Contains(page.Text(), "Updated active question?") {
		t.Errorf("the quiz page still shows the old quiz: %.300s", page.Body)
	}
}

// py: tests/test_admin_quiz_management.py::test_create_then_switch_to_new_quiz
func TestCreateThenSwitchToNewQuiz(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	create := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{
		"filename": "switch_target", "mode": "wizard",
		"quiz_data": map[string]any{"title": "New Quiz for Switch", "questions": []any{choice("Switch to me?", []any{"Yes", "No"}, 0)}},
	})
	if create.Status != 200 {
		t.Fatalf("create: %d %s", create.Status, create.Body)
	}
	res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "switch_target.yaml"})
	data := res.JSON(t)
	if res.Status != 200 || data["success"] != true || !strings.Contains(data["message"].(string), "switch_target.yaml") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_quiz_management.py::test_quiz_operations_without_auth
func TestQuizOperationsWithoutAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/admin/quiz/test.yaml", nil},
		{"POST", "/api/admin/create-quiz", map[string]any{"filename": "test", "mode": "wizard", "quiz_data": map[string]any{}}},
		{"PUT", "/api/admin/quiz/test.yaml", map[string]any{"mode": "text", "content": "test"}},
		{"DELETE", "/api/admin/quiz/test.yaml", nil},
		{"POST", "/api/admin/validate-quiz", map[string]any{"content": "test: content"}},
	} {
		if res := ts.Do(t, c.method, c.path, c.body); res.Status != 401 {
			t.Errorf("%s %s should require auth, got %d", c.method, c.path, res.Status)
		}
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_list_images_empty_directory
func TestAdminListImagesEmptyDirectory(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := admin.GetJSON(t, "/api/admin/list-images")
	images, ok := res.JSON(t)["images"].([]any)
	if res.Status != 200 || !ok || len(images) != 0 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_quiz_management.py::test_admin_list_images_with_files
func TestAdminListImagesWithFiles(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{})
	imgs := filepath.Join(ts.QuizzesDir, "imgs")
	if err := os.MkdirAll(imgs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test1.png", "Test2.jpg", "test3.gif", "diagram.svg", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(imgs, name), []byte("fake image content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := admin.GetJSON(t, "/api/admin/list-images")
	images := res.JSON(t)["images"].([]any)
	if res.Status != 200 || len(images) != 4 { // only image files
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	var names []string
	for _, item := range images {
		image := item.(map[string]any)
		if !strings.HasPrefix(image["path"].(string), "/imgs/") || image["path"] != "/imgs/"+image["filename"].(string) {
			t.Errorf("image %v", image)
		}
		names = append(names, image["filename"].(string))
	}
	// Sorted without regard to case.
	if want := []string{"diagram.svg", "test1.png", "Test2.jpg", "test3.gif"}; !slices.Equal(names, want) {
		t.Errorf("order %v, want %v", names, want)
	}
}
