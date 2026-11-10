package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_admin_unite_quizzes.py. The four browser tests of that file
// (single select, multi select, "(поточний)" mark, delete button) test the
// old admin page and belong to the web tests of Task 12.

func simpleQuiz(title, text string, options []any, correct any) map[string]any {
	return map[string]any{"title": title, "questions": []any{choice(text, options, correct)}}
}

func quizPair() map[string]any {
	return map[string]any{
		"quiz1.yaml": simpleQuiz("Quiz 1", "Q1?", []any{"A", "B"}, 0),
		"quiz2.yaml": simpleQuiz("Quiz 2", "Q2?", []any{"C", "D"}, 1),
	}
}

func unite(t *testing.T, admin *testutil.Client, names []any, newName any) *testutil.Response {
	t.Helper()
	return admin.PostJSON(t, "/api/admin/unite-quizzes", map[string]any{"quiz_filenames": names, "new_name": newName})
}

// py: tests/test_admin_unite_quizzes.py::test_unite_two_quizzes_success
func TestUniteTwoQuizzesSuccess(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{"title": "Quiz 1", "questions": []any{
			choice("Q1 from quiz1?", []any{"A", "B"}, 0), choice("Q2 from quiz1?", []any{"C", "D"}, 1)}},
		"quiz2.yaml": simpleQuiz("Quiz 2", "Q1 from quiz2?", []any{"E", "F"}, 0),
	}})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "united")
	data := res.JSON(t)
	sources, _ := data["source_quizzes"].([]any)
	if res.Status != 200 || data["success"] != true || data["filename"] != "united.yaml" || !isNum(data["total_questions"], "3") ||
		len(sources) != 2 || sources[0] != "quiz1.yaml" || sources[1] != "quiz2.yaml" {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if _, has := data["warning"]; has {
		t.Error("warning without duplicates")
	}
	if got := len(readQuizFile(t, ts, "united.yaml")["questions"].([]any)); got != 3 {
		t.Errorf("file has %d questions", got)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_three_quizzes_success
func TestUniteThreeQuizzesSuccess(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": simpleQuiz("Quiz 1", "Q1?", []any{"A", "B"}, 0),
		"quiz2.yaml": simpleQuiz("Quiz 2", "Q2?", []any{"C", "D"}, 1),
		"quiz3.yaml": simpleQuiz("Quiz 3", "Q3?", []any{"E", "F"}, 0),
	}})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml", "quiz3.yaml"}, "all_united")
	data := res.JSON(t)
	if res.Status != 200 || !isNum(data["total_questions"], "3") || len(data["source_quizzes"].([]any)) != 3 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_takes_config_from_first_quiz
func TestUniteTakesConfigFromFirstQuiz(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{
			"title": "First Quiz Title", "description": "First quiz description", "show_right_answer": false, "randomize_questions": true,
			"questions": []any{choice("Q1?", []any{"A", "B"}, 0)},
		},
		"quiz2.yaml": map[string]any{
			"title": "Second Quiz Title", "description": "Second quiz description", "show_right_answer": true, "randomize_questions": false,
			"questions": []any{choice("Q2?", []any{"C", "D"}, 1)},
		},
	}})
	if res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "united_config"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	united := readQuizFile(t, ts, "united_config.yaml")
	if united["title"] != "First Quiz Title" || united["description"] != "First quiz description" ||
		united["show_right_answer"] != false || united["randomize_questions"] != true {
		t.Errorf("%v", united)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_preserves_question_order
func TestUnitePreservesQuestionOrder(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{"title": "Quiz 1", "questions": []any{
			choice("First question", []any{"A", "B"}, 0), choice("Second question", []any{"C", "D"}, 1)}},
		"quiz2.yaml": simpleQuiz("Quiz 2", "Third question", []any{"E", "F"}, 0),
	}})
	if res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "order_test"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	questions := readQuizFile(t, ts, "order_test.yaml")["questions"].([]any)
	for i, want := range []string{"First question", "Second question", "Third question"} {
		if questions[i].(map[string]any)["question"] != want {
			t.Errorf("question %d: %v, want %s", i, questions[i], want)
		}
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_requires_at_least_two_quizzes
func TestUniteRequiresAtLeastTwoQuizzes(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"quiz1.yaml": simpleQuiz("Quiz 1", "Q1?", []any{"A", "B"}, 0)}})
	res := unite(t, admin, []any{"quiz1.yaml"}, "should_fail")
	if res.Status != 400 || !strings.Contains(errorString(t, res), "принаймні 2") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_empty_quiz_list_error
func TestUniteEmptyQuizListError(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	res := unite(t, admin, []any{}, "empty_test")
	if res.Status != 400 || !strings.Contains(errorString(t, res), "принаймні 2") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_missing_new_name_error
func TestUniteMissingNewNameError(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: quizPair()})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "")
	if res.Status != 400 || !strings.Contains(errorString(t, res), "обов'язкове") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_existing_filename_error
func TestUniteExistingFilenameError(t *testing.T) {
	t.Parallel()
	quizzes := quizPair()
	quizzes["existing.yaml"] = simpleQuiz("Existing Quiz", "Q?", []any{"X", "Y"}, 0)
	_, admin := withAdmin(t, testutil.Options{Quizzes: quizzes})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "existing")
	if res.Status != 409 || !strings.Contains(errorString(t, res), "вже існує") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_nonexistent_quiz_error
func TestUniteNonexistentQuizError(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"quiz1.yaml": simpleQuiz("Quiz 1", "Q1?", []any{"A", "B"}, 0)}})
	res := unite(t, admin, []any{"quiz1.yaml", "nonexistent.yaml"}, "united")
	if res.Status != 404 || !strings.Contains(errorString(t, res), "не знайдено") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_without_auth
func TestUniteWithoutAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: quizPair()})
	res := ts.PostJSON(t, "/api/admin/unite-quizzes", map[string]any{"quiz_filenames": []any{"quiz1.yaml", "quiz2.yaml"}, "new_name": "united"})
	if res.Status != 401 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_warns_on_duplicate_questions
func TestUniteWarnsOnDuplicateQuestions(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": simpleQuiz("Quiz 1", "Same question text", []any{"A", "B"}, 0),
		"quiz2.yaml": map[string]any{"title": "Quiz 2", "questions": []any{
			choice("Same question text", []any{"C", "D"}, 1), choice("Unique question", []any{"E", "F"}, 0)}},
	}})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "with_dups")
	data := res.JSON(t)
	warning, _ := data["warning"].(string)
	if res.Status != 200 || data["success"] != true || !isNum(data["total_questions"], "3") || warning != "Знайдено 1 можливих дублікатів питань" {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_same_text_different_file_not_duplicate
func TestUniteSameTextDifferentFileNotDuplicate(t *testing.T) {
	t.Parallel()
	first := choice("Same question text", []any{"A", "B"}, 0)
	first["file"] = "file1.xlsx"
	second := choice("Same question text", []any{"C", "D"}, 1)
	second["file"] = "file2.xlsx"
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{"title": "Quiz 1", "questions": []any{first}},
		"quiz2.yaml": map[string]any{"title": "Quiz 2", "questions": []any{second}},
	}})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "no_dups")
	data := res.JSON(t)
	if _, has := data["warning"]; res.Status != 200 || data["success"] != true || !isNum(data["total_questions"], "2") || has {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_with_yaml_extension_in_name
func TestUniteWithYAMLExtensionInName(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: quizPair()})
	res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "united.yaml")
	if res.Status != 200 || res.JSON(t)["filename"] != "united.yaml" {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_preserves_optional_config_fields
func TestUnitePreservesOptionalConfigFields(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{
			"title": "Quiz 1", "description": "Test description", "show_right_answer": false, "randomize_questions": true,
			"show_answers_on_completion": true, "show_final_list": false,
			"questions": []any{choice("Q1?", []any{"A", "B"}, 0)},
		},
		"quiz2.yaml": simpleQuiz("Quiz 2", "Q2?", []any{"C", "D"}, 1),
	}})
	if res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "with_options"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	united := readQuizFile(t, ts, "with_options.yaml")
	if united["description"] != "Test description" || united["show_right_answer"] != false || united["randomize_questions"] != true ||
		united["show_answers_on_completion"] != true || united["show_final_list"] != false {
		t.Errorf("%v", united)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_does_not_copy_quiz_level_min_correct
func TestUniteDoesNotCopyQuizLevelMinCorrect(t *testing.T) {
	t.Parallel()
	multi := choice("Pick the even numbers", []any{"2", "3", "4", "6"}, []any{0, 2, 3})
	multi["min_correct"] = 2
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{"title": "Quiz 1", "min_correct": 2, "questions": []any{multi}},
		"quiz2.yaml": simpleQuiz("Quiz 2", "Q2?", []any{"C", "D"}, 1),
	}})
	if res := unite(t, admin, []any{"quiz1.yaml", "quiz2.yaml"}, "no_top_min_correct"); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	united := readQuizFile(t, ts, "no_top_min_correct.yaml")
	if _, has := united["min_correct"]; has {
		t.Error("min_correct was copied to the top level")
	}
	if got := united["questions"].([]any)[0].(map[string]any)["min_correct"]; got != 2 {
		t.Errorf("question min_correct: %v", got)
	}
}

// py: tests/test_admin_unite_quizzes.py::test_unite_invalid_quiz_structure_error
func TestUniteInvalidQuizStructureError(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml":   simpleQuiz("Valid Quiz", "Q1?", []any{"A", "B"}, 0),
		"invalid.yaml": map[string]any{"title": "Invalid Quiz"}, // no questions
	}})
	res := unite(t, admin, []any{"quiz1.yaml", "invalid.yaml"}, "should_fail")
	if res.Status != 400 || !strings.Contains(errorString(t, res), "неправильну структуру") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}
