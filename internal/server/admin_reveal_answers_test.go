package server_test

import (
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_admin_reveal_answers.py: the "Show answers" button.

func revealQuiz(title string, showRight, onCompletion bool, questions ...any) map[string]any {
	quiz := map[string]any{"title": title, "show_right_answer": showRight, "questions": questions}
	if onCompletion {
		quiz["show_answers_on_completion"] = true
	}
	return quiz
}

func answer(t *testing.T, ts *testutil.TestServer, userID string, question, selected int) {
	t.Helper()
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": question, "selected_answer": selected})
	if res.Status != 200 {
		t.Fatalf("submit-answer: %d %s", res.Status, res.Body)
	}
}

// finalResults is final_results of GET /api/verify-user/{user_id}.
func finalResults(t *testing.T, ts *testutil.TestServer, userID string) map[string]any {
	t.Helper()
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	if res.Status != 200 {
		t.Fatalf("verify-user: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)["final_results"].(map[string]any)
}

// checkResultFields checks that every result row has (or has not) the
// correct answer and the verdict.
func checkResultFields(t *testing.T, results map[string]any, shown bool) {
	t.Helper()
	rows := results["test_results"].([]any)
	if len(rows) == 0 {
		t.Fatal("no result rows")
	}
	for _, row := range rows {
		_, hasAnswer := row.(map[string]any)["correct_answer"]
		_, hasVerdict := row.(map[string]any)["is_correct"]
		if hasAnswer != shown || hasVerdict != shown {
			t.Errorf("row %v: correct_answer shown %v, is_correct shown %v, want %v", row, hasAnswer, hasVerdict, shown)
		}
	}
}

func twoPlusTwo() []any {
	return []any{
		choice("What is 2 + 2?", []any{"3", "4", "5", "6"}, 1),
		choice("What is 3 + 3?", []any{"5", "6", "7", "8"}, 1),
	}
}

// py: tests/test_admin_reveal_answers.py::test_basic_manual_reveal_flow
func TestBasicManualRevealFlow(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("Manual Reveal Test Quiz", false, true, twoPlusTwo()...)}})
	user1, user2 := ts.RegisterStudent(t, "user1"), ts.RegisterStudent(t, "user2")
	answer(t, ts, user1, 1, 1)
	answer(t, ts, user1, 2, 1)

	verify := ts.GetJSON(t, "/api/verify-user/"+user1).JSON(t)
	results := verify["final_results"].(map[string]any)
	if verify["test_completed"] != true || results["all_completed"] != false {
		t.Fatalf("before: %v", verify)
	}
	checkResultFields(t, results, false)

	force := admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	data := force.JSON(t)
	if force.Status != 200 || data["success"] != true || data["forced"] != true || !isNum(data["completed_count"], "1") || !isNum(data["total_count"], "2") {
		t.Fatalf("force: %d %s", force.Status, force.Body)
	}
	after := finalResults(t, ts, user1)
	if after["all_completed"] != true {
		t.Errorf("all_completed after force: %v", after["all_completed"])
	}
	checkResultFields(t, after, true)

	answer(t, ts, user2, 1, 1)
	answer(t, ts, user2, 2, 1)
	second := finalResults(t, ts, user2)
	if second["all_completed"] != true {
		t.Errorf("second student: %v", second["all_completed"])
	}
	checkResultFields(t, second, true)
}

// py: tests/test_admin_reveal_answers.py::test_show_right_answer_precedence
func TestShowRightAnswerPrecedence(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("Precedence Test Quiz", true, true,
		choice("What is 2 + 2?", []any{"3", "4", "5", "6"}, 1))}})
	user := ts.RegisterStudent(t, "user1")
	answer(t, ts, user, 1, 1)
	checkResultFields(t, finalResults(t, ts, user), true)
}

// py: tests/test_admin_reveal_answers.py::test_reset_on_quiz_switch
func TestResetOnQuizSwitch(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{
		"quiz1.yaml": revealQuiz("Quiz 1", false, true, choice("Q1?", []any{"A", "B"}, 0)),
		"quiz2.yaml": revealQuiz("Quiz 2", false, true, choice("Q2?", []any{"C", "D"}, 1)),
	}})
	admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "quiz1.yaml"})
	user1 := ts.RegisterStudent(t, "user1")
	ts.RegisterStudent(t, "user2")
	answer(t, ts, user1, 1, 0)

	before := finalResults(t, ts, user1)
	rows := before["test_results"].([]any)
	if _, has := rows[0].(map[string]any)["correct_answer"]; before["all_completed"] != false || has {
		t.Fatalf("before: %v", before)
	}
	if res := admin.PostJSON(t, "/api/admin/force-show-answers", nil); res.Status != 200 {
		t.Fatalf("force: %d", res.Status)
	}
	if finalResults(t, ts, user1)["all_completed"] != true {
		t.Fatal("answers are not shown after the force")
	}
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "quiz2.yaml"}); res.Status != 200 {
		t.Fatalf("switch: %d %s", res.Status, res.Body)
	}
	list := admin.GetJSON(t, "/api/admin/list-quizzes")
	if list.Status != 200 || list.JSON(t)["force_all_completed"] != false {
		t.Errorf("list: %d %s", list.Status, list.Body)
	}
}

// py: tests/test_admin_reveal_answers.py::test_new_student_after_manual_reveal
func TestNewStudentAfterManualReveal(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("New Student Test Quiz", false, true,
		choice("What is 2 + 2?", []any{"3", "4", "5", "6"}, 1))}})
	user1 := ts.RegisterStudent(t, "user1")
	answer(t, ts, user1, 1, 1)
	admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	user2 := ts.RegisterStudent(t, "user2")
	answer(t, ts, user2, 1, 0) // a wrong answer
	results := finalResults(t, ts, user2)
	if results["all_completed"] != true {
		t.Errorf("all_completed: %v", results["all_completed"])
	}
	checkResultFields(t, results, true)
}

// py: tests/test_admin_reveal_answers.py::test_authentication_required
func TestForceShowAnswersAuthenticationRequired(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("Auth Test Quiz", false, false, choice("Q?", []any{"A", "B"}, 0))}})
	if res := ts.PostJSON(t, "/api/admin/force-show-answers", nil); res.Status != 401 && res.Status != 403 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_reveal_answers.py::test_force_show_answers_idempotent
func TestForceShowAnswersIdempotent(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("Idempotent Test Quiz", false, true, choice("Q?", []any{"A", "B"}, 0))}})
	for i := 1; i <= 2; i++ {
		res := admin.PostJSON(t, "/api/admin/force-show-answers", nil)
		if res.Status != 200 || res.JSON(t)["forced"] != true {
			t.Errorf("call %d: %d %s", i, res.Status, res.Body)
		}
	}
}

// py: tests/test_admin_reveal_answers.py::test_force_show_with_no_students
func TestForceShowWithNoStudents(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("No Students Test Quiz", false, true, choice("Q?", []any{"A", "B"}, 0))}})
	res := admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	data := res.JSON(t)
	if res.Status != 200 || data["forced"] != true || !isNum(data["completed_count"], "0") || !isNum(data["total_count"], "0") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_reveal_answers.py::test_admin_list_quizzes_includes_flag
func TestAdminListQuizzesIncludesFlag(t *testing.T) {
	t.Parallel()
	_, admin := withAdmin(t, testutil.Options{Quizzes: map[string]any{"default.yaml": revealQuiz("Flag Test Quiz", false, false, choice("Q?", []any{"A", "B"}, 0))}})
	first := admin.GetJSON(t, "/api/admin/list-quizzes").JSON(t)
	if first["force_all_completed"] != false || first["show_answers_on_completion"] != false {
		t.Errorf("before: %v", first)
	}
	admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	second := admin.GetJSON(t, "/api/admin/list-quizzes").JSON(t)
	if second["force_all_completed"] != true || second["show_answers_on_completion"] != false {
		t.Errorf("after: %v", second)
	}
}

// py: tests/test_admin_reveal_answers.py::test_manual_reveal_with_approval_mode
func TestManualRevealWithApprovalMode(t *testing.T) {
	t.Parallel()
	ts, admin := withAdmin(t, testutil.Options{
		Config:  map[string]any{"registration": map[string]any{"approve": true}},
		Quizzes: map[string]any{"default.yaml": revealQuiz("Approval Mode Test Quiz", false, true, choice("Q?", []any{"A", "B"}, 0))},
	})
	user1 := ts.RegisterStudent(t, "user1")
	ts.RegisterStudent(t, "user2")
	if res := admin.Put(t, "/api/admin/approve-user", map[string]any{"user_id": user1}); res.Status != 200 {
		t.Fatalf("approve: %d %s", res.Status, res.Body)
	}
	answer(t, ts, user1, 1, 0)
	// user1 is the only approved student and has finished, so the answers show.
	if finalResults(t, ts, user1)["all_completed"] != true {
		t.Error("all_completed should be true before the force")
	}
	res := admin.PostJSON(t, "/api/admin/force-show-answers", nil)
	if res.Status != 200 || res.JSON(t)["forced"] != true {
		t.Errorf("force: %d %s", res.Status, res.Body)
	}
}
