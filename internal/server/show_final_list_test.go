package server_test

import (
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// finalListQuiz is a one-question quiz; showFinalList is left out when nil.
func finalListQuiz(title string, showFinalList any, question string, options []any, correct int) map[string]any {
	quiz := map[string]any{"title": title, "questions": []any{
		map[string]any{"question": question, "options": options, "correct_answer": correct},
	}}
	if showFinalList != nil {
		quiz["show_final_list"] = showFinalList
	}
	return quiz
}

// The Python tests looked for "let showFinalList = true;" in static/index.html.
// The Go page holds the value as show_final_list in its boot data.
func requireShowFinalList(t *testing.T, ts *testutil.TestServer, want bool) {
	t.Helper()
	if got := pageBoot(t, readIndex(t, ts))["show_final_list"]; got != want {
		t.Errorf("show_final_list in the page: %v, want %v", got, want)
	}
}

// py: tests/test_show_final_list.py::test_show_final_list_default_true
func TestShowFinalListDefaultTrue(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, finalListQuiz("Default Final List Quiz", nil, "What is 2 + 2?", []any{"3", "4", "5"}, 1))
	requireShowFinalList(t, ts, true)
}

// py: tests/test_show_final_list.py::test_show_final_list_true_explicit
func TestShowFinalListTrueExplicit(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, finalListQuiz("Show Final List Quiz", true, "What is 3 + 3?", []any{"5", "6", "7"}, 1))
	requireShowFinalList(t, ts, true)
}

// py: tests/test_show_final_list.py::test_show_final_list_false
func TestShowFinalListFalse(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, finalListQuiz("Hide Final List Quiz", false, "What is 4 + 4?", []any{"7", "8", "9"}, 1))
	requireShowFinalList(t, ts, false)
}

// py: tests/test_show_final_list.py::test_show_final_list_false_still_shows_score
func TestShowFinalListFalseStillShowsScore(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, finalListQuiz("Score Only Quiz", false, "Q1?", []any{"A", "B"}, 0))
	userID := ts.RegisterStudent(t, "testuser")
	answerOK(t, ts, userID, 1, 0)
	verify := ts.GetJSON(t, "/api/verify-user/"+userID)
	if verify.Status != 200 {
		t.Fatalf("verify-user: %d", verify.Status)
	}
	body := verify.JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	fr := replyFinalResults(t, body)
	numIs(t, "correct_count", fr["correct_count"], "1")
	numIs(t, "total_count", fr["total_count"], "1")
	numIs(t, "percentage", fr["percentage"], "100")
}

// py: tests/test_show_final_list.py::test_show_final_list_validation
func TestShowFinalListValidation(t *testing.T) {
	t.Skip("needs validate-quiz from Task 8")
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	admin := ts.AdminClient(t, "test123")
	res := admin.PostJSON(t, "/api/admin/validate-quiz", map[string]any{
		"content": "title: Test\nshow_final_list: notabool\nquestions:\n  - question: Q?\n    options: [A, B]\n    correct_answer: 0\n",
	})
	if res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	if data["valid"] != false {
		t.Errorf("valid: %v", data["valid"])
	}
	errors, _ := data["errors"].([]any)
	found := false
	for _, e := range errors {
		if text, _ := e.(string); strings.Contains(text, "show_final_list") {
			found = true
		}
	}
	if !found {
		t.Errorf("no error names show_final_list: %v", errors)
	}
}

// py: tests/test_show_final_list.py::test_show_final_list_in_quiz_switch
func TestShowFinalListInQuizSwitch(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"default.yaml": finalListQuiz("Quiz With List", true, "Q1?", []any{"A", "B"}, 0),
		"quiz2.yaml":   finalListQuiz("Quiz Without List", false, "Q2?", []any{"X", "Y"}, 1),
	}})
	requireShowFinalList(t, ts, true)
	admin := ts.AdminClient(t, "test123")
	res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "quiz2.yaml"})
	if res.Status != 200 {
		t.Fatalf("switch-quiz: %d %s", res.Status, res.Body)
	}
	requireShowFinalList(t, ts, false)
}
