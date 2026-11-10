package server_test

import (
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_admin_quiz_editor.py: the editor settings survive a save
// and come back through GET /api/admin/quiz/{filename}.

func twoQuestions() []any {
	return []any{choice("Q1", []any{"A", "B"}, 0), choice("Q2", []any{"C", "D"}, 1)}
}

// parsedQuiz is the "parsed" part of GET /api/admin/quiz/{filename}.
func parsedQuiz(t *testing.T, admin *testutil.Client, name string) map[string]any {
	t.Helper()
	res := admin.GetJSON(t, "/api/admin/quiz/"+name)
	if res.Status != 200 {
		t.Fatalf("get %s: %d %s", name, res.Status, res.Body)
	}
	return res.JSON(t)["parsed"].(map[string]any)
}

func wizardCreate(t *testing.T, admin *testutil.Client, name string, quizData map[string]any) {
	t.Helper()
	res := admin.PostJSON(t, "/api/admin/create-quiz", map[string]any{"filename": name, "mode": "wizard", "quiz_data": quizData})
	if res.Status != 200 || res.JSON(t)["message"] == nil {
		t.Fatalf("create %s: %d %s", name, res.Status, res.Body)
	}
}

func wizardUpdate(t *testing.T, admin *testutil.Client, name string, quizData map[string]any) {
	t.Helper()
	res := admin.Put(t, "/api/admin/quiz/"+name, map[string]any{"mode": "wizard", "quiz_data": quizData})
	if res.Status != 200 {
		t.Fatalf("update %s: %d %s", name, res.Status, res.Body)
	}
}

// py: tests/test_admin_quiz_editor.py::test_create_quiz_with_randomize_questions_via_wizard
func TestCreateQuizWithRandomizeQuestionsViaWizard(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	wizardCreate(t, admin, "test_randomized", map[string]any{
		"title": "Test Quiz", "show_right_answer": true, "randomize_questions": true, "questions": twoQuestions(),
	})
	parsed := parsedQuiz(t, admin, "test_randomized.yaml")
	if parsed["randomize_questions"] != true || parsed["show_right_answer"] != true {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_create_quiz_without_randomize_questions_defaults_false
func TestCreateQuizWithoutRandomizeQuestionsDefaultsFalse(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	wizardCreate(t, admin, "test_no_randomize", map[string]any{
		"title": "Test Quiz", "show_right_answer": false,
		"questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	})
	parsed := parsedQuiz(t, admin, "test_no_randomize.yaml")
	if value, has := parsed["randomize_questions"]; has && value != false {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_edit_quiz_to_add_randomize_questions
func TestEditQuizToAddRandomizeQuestions(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"editable.yaml": map[string]any{"title": "Original Quiz", "questions": twoQuestions()}}})
	if value, has := parsedQuiz(t, admin, "editable.yaml")["randomize_questions"]; has && value != false {
		t.Fatalf("the quiz already randomizes: %v", value)
	}
	wizardUpdate(t, admin, "editable.yaml", map[string]any{
		"title": "Updated Quiz", "show_right_answer": true, "randomize_questions": true, "questions": twoQuestions(),
	})
	parsed := parsedQuiz(t, admin, "editable.yaml")
	if parsed["randomize_questions"] != true || parsed["title"] != "Updated Quiz" {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_randomize_questions_preserved_after_edit
func TestRandomizeQuestionsPreservedAfterEdit(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"randomized.yaml": map[string]any{
		"title": "Randomized Quiz", "randomize_questions": true, "questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	}}})
	original := parsedQuiz(t, admin, "randomized.yaml")
	wizardUpdate(t, admin, "randomized.yaml", map[string]any{
		"title": "Updated Title", "randomize_questions": true, "questions": original["questions"],
	})
	parsed := parsedQuiz(t, admin, "randomized.yaml")
	if parsed["randomize_questions"] != true || parsed["title"] != "Updated Title" {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_disable_randomize_questions_via_edit
func TestDisableRandomizeQuestionsViaEdit(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"to_disable.yaml": map[string]any{
		"title": "Randomized Quiz", "randomize_questions": true, "questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	}}})
	if parsedQuiz(t, admin, "to_disable.yaml")["randomize_questions"] != true {
		t.Fatal("the quiz does not randomize at the start")
	}
	wizardUpdate(t, admin, "to_disable.yaml", map[string]any{
		"title": "No Longer Randomized", "randomize_questions": false, "questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	})
	parsed := parsedQuiz(t, admin, "to_disable.yaml")
	if parsed["randomize_questions"] != false || parsed["title"] != "No Longer Randomized" {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_create_quiz_with_show_answers_on_completion
func TestCreateQuizWithShowAnswersOnCompletion(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{})
	wizardCreate(t, admin, "test_completion_answers", map[string]any{
		"title": "Completion Answers Quiz", "show_right_answer": false, "show_answers_on_completion": true, "questions": twoQuestions(),
	})
	parsed := parsedQuiz(t, admin, "test_completion_answers.yaml")
	if parsed["show_answers_on_completion"] != true || parsed["show_right_answer"] != false {
		t.Errorf("%v", parsed)
	}
}

// py: tests/test_admin_quiz_editor.py::test_edit_quiz_to_enable_show_answers_on_completion
func TestEditQuizToEnableShowAnswersOnCompletion(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"enable_completion.yaml": map[string]any{
		"title": "Original Quiz", "show_right_answer": false, "questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	}}})
	if value, has := parsedQuiz(t, admin, "enable_completion.yaml")["show_answers_on_completion"]; has && value != false {
		t.Fatalf("already on: %v", value)
	}
	wizardUpdate(t, admin, "enable_completion.yaml", map[string]any{
		"title": "Updated Quiz", "show_right_answer": false, "show_answers_on_completion": true,
		"questions": []any{choice("Q1", []any{"A", "B"}, 0)},
	})
	parsed := parsedQuiz(t, admin, "enable_completion.yaml")
	if parsed["show_answers_on_completion"] != true || parsed["title"] != "Updated Quiz" {
		t.Errorf("%v", parsed)
	}
}
