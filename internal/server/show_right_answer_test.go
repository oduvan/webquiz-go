package server_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// py: tests/test_show_right_answer.py::test_show_right_answer_true_explicit
func TestShowRightAnswerTrueExplicit(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Show Answers Quiz", "show_right_answer": true, "questions": []any{
		map[string]any{"question": "What is 3 + 3?", "options": []any{"5", "6", "7", "8"}, "correct_answer": 1},
	}})
	userID := ts.RegisterStudent(t, "testuser")
	data := answerOK(t, ts, userID, 1, 0) // a wrong answer (5)
	if _, ok := data["correct_answer"]; !ok {
		t.Fatalf("correct_answer missing: %v", data)
	}
	numIs(t, "correct_answer", data["correct_answer"], "1")
	if data["is_correct"] != false {
		t.Errorf("is_correct: %v", data["is_correct"])
	}

	verify := ts.GetJSON(t, "/api/verify-user/"+userID)
	if verify.Status != 200 {
		t.Fatalf("verify-user: %d", verify.Status)
	}
	body := verify.JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	rows, _ := finalResults(t, body)["test_results"].([]any)
	if len(rows) != 1 {
		t.Fatalf("test_results: %v", rows)
	}
	if got := rows[0].(map[string]any)["correct_answer"]; got != "6" { // the text of the right option
		t.Errorf("correct_answer in the results: %v", got)
	}
}

// hiddenAnswerQuiz is a one-question quiz with show_right_answer false.
func hiddenAnswerQuiz(title, text string, options []any, correct int) map[string]any {
	return map[string]any{"title": title, "show_right_answer": false, "questions": []any{
		map[string]any{"question": text, "options": options, "correct_answer": correct},
	}}
}

// requireNoAnswerFields checks that a reply or result row has neither of the answer fields.
func requireNoAnswerFields(t *testing.T, what string, data map[string]any) {
	t.Helper()
	for _, key := range []string{"correct_answer", "is_correct"} {
		if _, ok := data[key]; ok {
			t.Errorf("%s must not have %s: %v", what, key, data)
		}
	}
}

// py: tests/test_show_right_answer.py::test_show_right_answer_false
func TestShowRightAnswerFalse(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, hiddenAnswerQuiz("Hide Answers Quiz", "What is 4 + 4?", []any{"6", "7", "8", "9"}, 2))
	userID := ts.RegisterStudent(t, "testuser")
	requireNoAnswerFields(t, "the reply", answerOK(t, ts, userID, 1, 0))

	verify := ts.GetJSON(t, "/api/verify-user/"+userID)
	if verify.Status != 200 {
		t.Fatalf("verify-user: %d", verify.Status)
	}
	body := verify.JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	rows, _ := finalResults(t, body)["test_results"].([]any)
	if len(rows) != 1 {
		t.Fatalf("test_results: %v", rows)
	}
	requireNoAnswerFields(t, "the result row", rows[0].(map[string]any))
}

// py: tests/test_show_right_answer.py::test_show_right_answer_correct_answers_not_leaked
func TestShowRightAnswerCorrectAnswersNotLeaked(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, hiddenAnswerQuiz("Hide Answers Quiz", "What is 5 + 5?", []any{"8", "9", "10", "11"}, 2))
	userID := ts.RegisterStudent(t, "testuser")
	requireNoAnswerFields(t, "the reply to a correct answer", answerOK(t, ts, userID, 1, 2))
}

// py: tests/test_show_right_answer.py::test_show_right_answer_multiple_questions
func TestShowRightAnswerMultipleQuestions(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, map[string]any{"title": "Multi Question Quiz", "show_right_answer": false, "questions": []any{
		map[string]any{"question": "What is 1 + 1?", "options": []any{"1", "2", "3", "4"}, "correct_answer": 1},
		map[string]any{"question": "What is 2 * 2?", "options": []any{"2", "3", "4", "5"}, "correct_answer": 2},
	}})
	userID := ts.RegisterStudent(t, "testuser")
	requireNoAnswerFields(t, "reply 1", answerOK(t, ts, userID, 1, 0)) // wrong
	requireNoAnswerFields(t, "reply 2", answerOK(t, ts, userID, 2, 2)) // right

	body := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	rows, _ := finalResults(t, body)["test_results"].([]any)
	if len(rows) != 2 {
		t.Fatalf("test_results: %v", rows)
	}
	for _, row := range rows {
		if _, ok := row.(map[string]any)["correct_answer"]; ok {
			t.Errorf("the result row must not have correct_answer: %v", row)
		}
	}
}

// py: tests/test_show_right_answer.py::test_show_right_answer_admin_quiz_update
func TestShowRightAnswerAdminQuizUpdate(t *testing.T) {
	t.Skip("needs the quiz PUT endpoint from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"update_test.yaml": map[string]any{
		"title": "Original Quiz", "show_right_answer": true,
		"questions": []any{map[string]any{"question": "Original question?", "options": []any{"A", "B"}, "correct_answer": 0}},
	}}})
	admin := ts.AdminClient(t, "test123")
	updated := map[string]any{
		"title": "Updated Quiz", "show_right_answer": false,
		"questions": []any{map[string]any{"question": "Updated question?", "options": []any{"X", "Y", "Z"}, "correct_answer": 1}},
	}
	if res := admin.Put(t, "/api/admin/quiz/update_test.yaml", map[string]any{"mode": "wizard", "quiz_data": updated}); res.Status != 200 {
		t.Fatalf("PUT quiz: %d %s", res.Status, res.Body)
	}
	res := admin.GetJSON(t, "/api/admin/quiz/update_test.yaml")
	if res.Status != 200 {
		t.Fatalf("GET quiz: %d %s", res.Status, res.Body)
	}
	var quiz map[string]any
	content, _ := res.JSON(t)["content"].(string)
	if err := yaml.Unmarshal([]byte(content), &quiz); err != nil {
		t.Fatal(err)
	}
	if quiz["show_right_answer"] != false {
		t.Errorf("show_right_answer after the update: %v", quiz["show_right_answer"])
	}
}
