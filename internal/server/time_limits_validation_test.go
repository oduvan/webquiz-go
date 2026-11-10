package server_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// validateQuizText sends quiz text to the validate-quiz endpoint.
func validateQuizText(t *testing.T, admin *testutil.Client, quizYAML string) map[string]any {
	t.Helper()
	res := admin.PostJSON(t, "/api/admin/validate-quiz", map[string]any{"content": quizYAML})
	if res.Status != 200 {
		t.Fatalf("validate-quiz: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

func anyErrorContains(data map[string]any, parts ...string) bool {
	errors, _ := data["errors"].([]any)
	for _, e := range errors {
		text, _ := e.(string)
		all := true
		for _, part := range parts {
			all = all && strings.Contains(text, part)
		}
		if all {
			return true
		}
	}
	return false
}

// py: tests/test_time_limits_validation.py::test_validate_quiz_rejects_both_time_limit_modes
func TestValidateQuizRejectsBothTimeLimitModes(t *testing.T) {
	t.Skip("needs validate-quiz from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	data := validateQuizText(t, ts.AdminClient(t, "test123"), `title: Both modes
time_limit:
  per_question: 30
  whole_quiz: 600
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0`)
	if data["valid"] != false || !anyErrorContains(data, "time_limit") {
		t.Errorf("%v", data)
	}
}

// py: tests/test_time_limits_validation.py::test_validate_quiz_rejects_negative_question_time_limit
func TestValidateQuizRejectsNegativeQuestionTimeLimit(t *testing.T) {
	t.Skip("needs validate-quiz from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	data := validateQuizText(t, ts.AdminClient(t, "test123"), `title: Negative
time_limit:
  per_question: 30
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0
    time_limit: -5`)
	if data["valid"] != false || !anyErrorContains(data, "Question 1", "time_limit") {
		t.Errorf("%v", data)
	}
}

// py: tests/test_time_limits_validation.py::test_validate_quiz_accepts_question_time_limit_without_quiz_time_limit
func TestValidateQuizAcceptsQuestionTimeLimitWithoutQuizTimeLimit(t *testing.T) {
	t.Skip("needs validate-quiz from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	data := validateQuizText(t, ts.AdminClient(t, "test123"), `title: Question only
questions:
  - question: Test?
    options: ['A', 'B']
    correct_answer: 0
    time_limit: 10`)
	if data["valid"] != true {
		t.Errorf("%v", data)
	}
}

// py: tests/test_time_limits_validation.py::test_unite_copies_time_limit_from_first_quiz
func TestUniteCopiesTimeLimitFromFirstQuiz(t *testing.T) {
	t.Skip("needs unite-quizzes from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": map[string]any{"title": "Quiz 1", "time_limit": map[string]any{"per_question": 30},
			"questions": []any{map[string]any{"question": "Q1?", "options": []any{"A", "B"}, "correct_answer": 0, "time_limit": 10}}},
		"quiz2.yaml": map[string]any{"title": "Quiz 2",
			"questions": []any{map[string]any{"question": "Q2?", "options": []any{"C", "D"}, "correct_answer": 1}}},
	}})
	admin := ts.AdminClient(t, "test123")
	res := admin.PostJSON(t, "/api/admin/unite-quizzes", map[string]any{"quiz_filenames": []any{"quiz1.yaml", "quiz2.yaml"}, "new_name": "united_timed"})
	if res.Status != 200 {
		t.Fatalf("unite-quizzes: %d %s", res.Status, res.Body)
	}
	data, err := os.ReadFile(filepath.Join(ts.QuizzesDir, "united_timed.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var united struct {
		TimeLimit map[string]int `yaml:"time_limit"`
		Questions []struct {
			TimeLimit int `yaml:"time_limit"`
		} `yaml:"questions"`
	}
	if err := yaml.Unmarshal(data, &united); err != nil {
		t.Fatal(err)
	}
	if len(united.TimeLimit) != 1 || united.TimeLimit["per_question"] != 30 || united.Questions[0].TimeLimit != 10 {
		t.Errorf("united quiz:\n%s", data)
	}
}
