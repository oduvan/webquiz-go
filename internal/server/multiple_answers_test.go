package server_test

import (
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The Python tests called server.submit_answer with a mock request and
// hand-made state. These ports go through HTTP with a real quiz and also
// check that the answer counts as correct.

// py: tests/test_multiple_answers.py::TestMultipleAnswersAPI::test_submit_multiple_answers
func TestSubmitMultipleAnswers(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"test_quiz.yaml": map[string]any{
		"title": "Multiple",
		"questions": []any{
			map[string]any{"question": "Multiple choice test", "options": []any{"A", "B", "C", "D"}, "correct_answer": []any{0, 2}},
		},
	}}})
	userID := ts.RegisterStudent(t, "testuser")
	res := partAAnswer(t, ts, userID, 1, []any{0, 2})
	if res.Status != 200 || res.JSON(t)["is_correct"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_multiple_answers.py::TestMultipleAnswersAPI::test_backward_compatibility
func TestMultipleAnswersBackwardCompatibility(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"test_quiz.yaml": map[string]any{
		"title": "Single",
		"questions": []any{
			map[string]any{"question": "Traditional single answer", "options": []any{"A", "B", "C", "D"}, "correct_answer": 2},
		},
	}}})
	userID := ts.RegisterStudent(t, "testuser")
	res := partAAnswer(t, ts, userID, 1, 2)
	if res.Status != 200 || res.JSON(t)["is_correct"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}
