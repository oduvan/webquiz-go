package server_test

import (
	"encoding/json"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// completionQuiz is a quiz with show_right_answer false and
// show_answers_on_completion true unless the options say otherwise.
func completionQuiz(title string, questions []any, extra map[string]any) map[string]any {
	quiz := map[string]any{"title": title, "show_right_answer": false, "show_answers_on_completion": true, "questions": questions}
	for key, value := range extra {
		quiz[key] = value
	}
	return quiz
}

// oneQuestion is a four-option question with the right option at index correct.
func oneQuestion(text string, options []any, correct int) []any {
	return []any{map[string]any{"question": text, "options": options, "correct_answer": correct}}
}

// resultsOf reads final_results of a student from verify-user.
func resultsOf(t *testing.T, ts *testutil.TestServer, userID string) map[string]any {
	t.Helper()
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	if res.Status != 200 {
		t.Fatalf("verify-user: %d %s", res.Status, res.Body)
	}
	return finalResults(t, res.JSON(t))
}

// rowAt returns test_results[i].
func rowAt(t *testing.T, results map[string]any, i int) map[string]any {
	t.Helper()
	rows, _ := results["test_results"].([]any)
	if i >= len(rows) {
		t.Fatalf("no result row %d in %v", i, results)
	}
	return rows[i].(map[string]any)
}

// requireAnswersShown checks that the row has correct_answer and is_correct (or neither).
func requireAnswersShown(t *testing.T, what string, row map[string]any, shown bool) {
	t.Helper()
	for _, key := range []string{"correct_answer", "is_correct"} {
		if _, ok := row[key]; ok != shown {
			t.Errorf("%s: %s present = %v, want %v (row %v)", what, key, ok, shown, row)
		}
	}
}

// py: tests/test_show_answers_on_completion.py::test_basic_functionality_answers_hidden_until_all_complete
func TestShowAnswersBasicFunctionality(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Show Answers On Completion Quiz", []any{
		map[string]any{"question": "What is 2 + 2?", "options": []any{"3", "4", "5", "6"}, "correct_answer": 1},
		map[string]any{"question": "What is 3 + 3?", "options": []any{"5", "6", "7", "8"}, "correct_answer": 1},
	}, nil))
	user1 := ts.RegisterStudent(t, "user1")
	user2 := ts.RegisterStudent(t, "user2")

	answerOK(t, ts, user1, 1, 1)
	answerOK(t, ts, user1, 2, 1)

	res := ts.GetJSON(t, "/api/verify-user/"+user1)
	if res.Status != 200 {
		t.Fatalf("verify-user: %d", res.Status)
	}
	body := res.JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	first := finalResults(t, body)
	if first["show_answers_on_completion"] != true || first["all_completed"] != false {
		t.Errorf("flags: %v %v", first["show_answers_on_completion"], first["all_completed"])
	}
	for i := range first["test_results"].([]any) {
		requireAnswersShown(t, "before everyone finished", rowAt(t, first, i), false)
	}

	answerOK(t, ts, user2, 1, 1)
	answerOK(t, ts, user2, 2, 1)

	for _, id := range []string{user1, user2} {
		after := resultsOf(t, ts, id)
		if after["show_answers_on_completion"] != true && id == user1 {
			t.Errorf("show_answers_on_completion: %v", after["show_answers_on_completion"])
		}
		if after["all_completed"] != true {
			t.Errorf("all_completed for %s: %v", id, after["all_completed"])
		}
		for i := range after["test_results"].([]any) {
			requireAnswersShown(t, "after everyone finished", rowAt(t, after, i), true)
		}
	}
}

// py: tests/test_show_answers_on_completion.py::test_waiting_message_displayed_when_not_all_complete
func TestShowAnswersWaitingFlags(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Waiting Message Quiz", oneQuestion("What is 5 + 5?", []any{"8", "9", "10", "11"}, 2), nil))
	user1 := ts.RegisterStudent(t, "user1")
	ts.RegisterStudent(t, "user2")
	answerOK(t, ts, user1, 1, 2)

	body := ts.GetJSON(t, "/api/verify-user/"+user1).JSON(t)
	if body["test_completed"] != true {
		t.Errorf("test_completed: %v", body["test_completed"])
	}
	fr := finalResults(t, body)
	if fr["show_answers_on_completion"] != true || fr["all_completed"] != false {
		t.Errorf("flags: %v %v", fr["show_answers_on_completion"], fr["all_completed"])
	}
}

// py: tests/test_show_answers_on_completion.py::test_dynamic_behavior_new_registration_hides_answers
func TestShowAnswersNewRegistrationHidesAnswers(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Dynamic Behavior Quiz", oneQuestion("What is 7 + 7?", []any{"12", "13", "14", "15"}, 2), nil))
	user1 := ts.RegisterStudent(t, "user1")
	user2 := ts.RegisterStudent(t, "user2")
	answerOK(t, ts, user1, 1, 2)
	answerOK(t, ts, user2, 1, 2)

	fr := resultsOf(t, ts, user1)
	if fr["all_completed"] != true {
		t.Errorf("all_completed: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "both finished", rowAt(t, fr, 0), true)

	user3 := ts.RegisterStudent(t, "user3")
	fr = resultsOf(t, ts, user1)
	if fr["all_completed"] != false {
		t.Errorf("all_completed after a new registration: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "after user3 registered", rowAt(t, fr, 0), false)

	answerOK(t, ts, user3, 1, 2)
	fr = resultsOf(t, ts, user1)
	if fr["all_completed"] != true {
		t.Errorf("all_completed after user3 finished: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "user3 finished", rowAt(t, fr, 0), true)
}

// py: tests/test_show_answers_on_completion.py::test_approval_mode_counts_only_approved
func TestShowAnswersApprovalCountsOnlyApproved(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Approval Mode Quiz", oneQuestion("What is 9 + 9?", []any{"16", "17", "18", "19"}, 2), nil),
		func(o *testutil.Options) { o.Config = map[string]any{"registration": map[string]any{"approve": true}} })
	user1 := ts.RegisterStudent(t, "user1")
	user2 := ts.RegisterStudent(t, "user2")
	ts.RegisterStudent(t, "user3") // never approved

	admin := ts.AdminClient(t, "test123")
	for _, id := range []string{user1, user2} {
		if res := admin.Put(t, "/api/admin/approve-user", map[string]any{"user_id": id, "approved": true}); res.Status != 200 {
			t.Fatalf("approve-user: %d %s", res.Status, res.Body)
		}
	}
	answerOK(t, ts, user1, 1, 2)
	answerOK(t, ts, user2, 1, 2)

	// Only approved students count; user3 has not finished.
	fr := resultsOf(t, ts, user1)
	if fr["all_completed"] != true {
		t.Errorf("all_completed: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "approved students finished", rowAt(t, fr, 0), true)
}

// py: tests/test_show_answers_on_completion.py::test_single_student_immediate_visibility
func TestShowAnswersSingleStudentImmediate(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Single Student Quiz", oneQuestion("What is 4 + 4?", []any{"6", "7", "8", "9"}, 2), nil))
	userID := ts.RegisterStudent(t, "user1")
	answerOK(t, ts, userID, 1, 2)
	fr := resultsOf(t, ts, userID)
	if fr["all_completed"] != true || fr["show_answers_on_completion"] != true {
		t.Errorf("flags: %v %v", fr["all_completed"], fr["show_answers_on_completion"])
	}
	requireAnswersShown(t, "single student", rowAt(t, fr, 0), true)
}

// py: tests/test_show_answers_on_completion.py::test_interaction_with_show_right_answer_true
func TestShowAnswersWithShowRightAnswerTrue(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Precedence Quiz", oneQuestion("What is 6 + 6?", []any{"10", "11", "12", "13"}, 2),
		map[string]any{"show_right_answer": true}))
	user1 := ts.RegisterStudent(t, "user1")
	ts.RegisterStudent(t, "user2")
	answerOK(t, ts, user1, 1, 2)

	// show_right_answer: true wins, although not everybody has finished.
	fr := resultsOf(t, ts, user1)
	if fr["all_completed"] != false {
		t.Errorf("all_completed: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "show_right_answer true", rowAt(t, fr, 0), true)
}

// py: tests/test_show_answers_on_completion.py::test_interaction_with_show_right_answer_false
func TestShowAnswersWithBothOff(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Never Show Quiz", oneQuestion("What is 8 + 8?", []any{"14", "15", "16", "17"}, 2),
		map[string]any{"show_answers_on_completion": false}))
	userID := ts.RegisterStudent(t, "user1")
	answerOK(t, ts, userID, 1, 2)
	fr := resultsOf(t, ts, userID)
	if fr["all_completed"] != true || fr["show_answers_on_completion"] != false {
		t.Errorf("flags: %v %v", fr["all_completed"], fr["show_answers_on_completion"])
	}
	requireAnswersShown(t, "both options off", rowAt(t, fr, 0), false)
}

// py: tests/test_show_answers_on_completion.py::test_works_with_randomize_questions
func TestShowAnswersWithRandomizedQuestions(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("Randomized Quiz", []any{
		map[string]any{"question": "What is 1 + 1?", "options": []any{"1", "2", "3", "4"}, "correct_answer": 1},
		map[string]any{"question": "What is 2 + 2?", "options": []any{"2", "3", "4", "5"}, "correct_answer": 2},
	}, map[string]any{"randomize_questions": true}))

	register := func(name string) (string, []int) {
		data := ts.PostJSON(t, "/api/register", map[string]any{"username": name}).JSON(t)
		var order []int
		for _, id := range data["question_order"].([]any) {
			n, _ := id.(json.Number).Int64()
			order = append(order, int(n))
		}
		return data["user_id"].(string), order
	}
	user1, order1 := register("user1")
	user2, order2 := register("user2")

	for _, id := range order1 {
		answerOf(t, ts, user1, id, 1)
	}
	fr := resultsOf(t, ts, user1)
	if fr["all_completed"] != false {
		t.Errorf("all_completed: %v", fr["all_completed"])
	}
	requireAnswersShown(t, "user1 alone", rowAt(t, fr, 0), false)

	for _, id := range order2 {
		answerOf(t, ts, user2, id, 1)
	}
	for _, id := range []string{user1, user2} {
		fr := resultsOf(t, ts, id)
		if fr["all_completed"] != true {
			t.Errorf("all_completed for %s: %v", id, fr["all_completed"])
		}
		requireAnswersShown(t, "both finished", rowAt(t, fr, 0), true)
	}
}

// py: tests/test_show_answers_on_completion.py::test_api_response_structure
func TestShowAnswersAPIResponseStructure(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("API Structure Quiz", oneQuestion("Test question?", []any{"A", "B", "C", "D"}, 0), nil))
	userID := ts.RegisterStudent(t, "user1")
	answerOK(t, ts, userID, 1, 0)
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	if res.Status != 200 {
		t.Fatalf("verify-user: %d", res.Status)
	}
	fr := finalResults(t, res.JSON(t))
	for _, key := range []string{"all_completed", "show_answers_on_completion", "test_results", "correct_count", "total_count", "percentage", "total_time"} {
		if _, ok := fr[key]; !ok {
			t.Errorf("final_results has no %s: %v", key, fr)
		}
	}
	if _, ok := fr["all_completed"].(bool); !ok {
		t.Errorf("all_completed must be a boolean: %v", fr["all_completed"])
	}
	if _, ok := fr["show_answers_on_completion"].(bool); !ok {
		t.Errorf("show_answers_on_completion must be a boolean: %v", fr["show_answers_on_completion"])
	}
	if _, ok := fr["test_results"].([]any); !ok {
		t.Errorf("test_results must be a list: %v", fr["test_results"])
	}
}

// py: tests/test_show_answers_on_completion.py::test_no_students_edge_case
func TestShowAnswersNoStudents(t *testing.T) {
	t.Parallel()
	ts := quizServer(t, completionQuiz("No Students Quiz", oneQuestion("Test?", []any{"A", "B"}, 0), nil))
	res := ts.GetJSON(t, "/api/verify-user/999999")
	if res.Status != 200 {
		t.Fatalf("verify-user: %d %s", res.Status, res.Body)
	}
	if res.JSON(t)["valid"] != false {
		t.Errorf("valid: %s", res.Body)
	}
}
