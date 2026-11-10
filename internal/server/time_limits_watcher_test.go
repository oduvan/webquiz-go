package server_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// fastWatcher is the environment of the watcher tests: a short grace and a
// watcher that looks every 20 ms (the Python tests used 0.3 s and 0.2 s and
// slept 2 s; here a test clock passes the time).
var fastWatcher = map[string]string{"WEBQUIZ_TIME_LIMIT_GRACE_SECONDS": "0.3", "WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL": "0.02"}

func verifyUser(t *testing.T, ts *testutil.TestServer, userID string) map[string]any {
	t.Helper()
	return ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
}

// waitUntil polls until the check is true (the watcher runs on its own timer).
func waitUntil(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// nextQuestionIndex reads next_question_index of verify-user.
func nextQuestionIndex(t *testing.T, ts *testutil.TestServer, userID string) string {
	t.Helper()
	number, _ := verifyUser(t, ts, userID)["next_question_index"].(json.Number)
	return number.String()
}

// py: tests/test_time_limits_watcher.py::test_quiet_student_gets_timeout
func TestQuietStudentGetsTimeout(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"per_question": 1}}, choiceQuestion("Q?", nil), choiceQuestion("Q?", nil)), fastWatcher)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(2 * time.Second) // the page sends nothing
	waitUntil(t, "the watcher to store the timeout", func() bool { return nextQuestionIndex(t, ts, userID) == "1" })
	if verifyUser(t, ts, userID)["test_completed"] != false {
		t.Errorf("test_completed: %v", verifyUser(t, ts, userID))
	}
}

// py: tests/test_time_limits_watcher.py::test_next_question_clock_does_not_start_on_its_own
func TestNextQuestionClockDoesNotStartOnItsOwn(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"per_question": 1}}, choiceQuestion("Q?", nil), choiceQuestion("Q?", nil)), fastWatcher)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(2 * time.Second)
	waitUntil(t, "the watcher to store the timeout", func() bool { return nextQuestionIndex(t, ts, userID) == "1" })
	clock.Advance(2 * time.Second)     // question 2 was never shown, so it must not time out
	time.Sleep(300 * time.Millisecond) // about 15 watcher rounds
	if got := nextQuestionIndex(t, ts, userID); got != "1" {
		t.Errorf("next_question_index: %s", got)
	}
}

// py: tests/test_time_limits_watcher.py::test_whole_quiz_ends_for_quiet_student_in_student_order
func TestWholeQuizEndsForQuietStudentInStudentOrder(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"whole_quiz": 1}, "randomize_questions": true},
		choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil), choiceQuestion("Q3?", nil)), fastWatcher)
	registration := ts.PostJSON(t, "/api/register", map[string]any{"username": "S"}).JSON(t)
	userID := registration["user_id"].(string)
	order := registration["question_order"].([]any)
	startQuestion(t, ts, userID, int(floatOf(t, order[0])))
	clock.Advance(2 * time.Second)
	waitUntil(t, "the quiz to end", func() bool { return verifyUser(t, ts, userID)["test_completed"] == true })

	rows := finalResults(t, verifyUser(t, ts, userID))["test_results"].([]any)
	if len(rows) != len(order) {
		t.Fatalf("rows: %v", rows)
	}
	for i, id := range order {
		row := rows[i].(map[string]any)
		if want := fmt.Sprintf("Q%s?", id.(json.Number)); row["question"] != want {
			t.Errorf("row %d: question %v, want %s", i, row["question"], want)
		}
		if row["timed_out"] != true || row["selected_answer"] != "" {
			t.Errorf("row %d: %v", i, row)
		}
	}
}

// py: tests/test_time_limits_watcher.py::test_whole_quiz_timeouts_complete_the_class
func TestWholeQuizTimeoutsCompleteTheClass(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(map[string]any{
		"time_limit": map[string]any{"whole_quiz": 1}, "show_right_answer": false, "show_answers_on_completion": true,
	}, choiceQuestion("Q?", nil), choiceQuestion("Q?", nil)), fastWatcher)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(2 * time.Second)
	waitUntil(t, "the quiz to end", func() bool { return verifyUser(t, ts, userID)["test_completed"] == true })
	if finalResults(t, verifyUser(t, ts, userID))["all_completed"] != true {
		t.Errorf("all_completed: %v", verifyUser(t, ts, userID))
	}
}

// py: tests/test_time_limits_watcher.py::test_quiz_switch_while_clock_runs
func TestQuizSwitchWhileClockRuns(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	quizzes := map[string]any{
		"a.yaml": map[string]any{"title": "A", "time_limit": map[string]any{"per_question": 1}, "questions": []any{choiceQuestion("Q?", nil)}},
		"b.yaml": map[string]any{"title": "B", "time_limit": map[string]any{"per_question": 1}, "questions": []any{choiceQuestion("Q?", nil)}},
	}
	ts := startTimed(t, clock, quizzes, fastWatcher)
	admin := ts.AdminClient(t, "test123")
	switchTo := func(filename string) {
		if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": filename}); res.Status != 200 {
			t.Fatalf("switch-quiz %s: %d %s", filename, res.Status, res.Body)
		}
	}
	switchTo("a.yaml") // with two quiz files no quiz is active until the admin picks one
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	switchTo("b.yaml")
	clock.Advance(1500 * time.Millisecond)
	time.Sleep(300 * time.Millisecond) // the watcher runs about 15 times

	if verifyUser(t, ts, userID)["valid"] != false { // the old student is gone
		t.Errorf("the old student must be gone: %v", verifyUser(t, ts, userID))
	}
	newUser := ts.RegisterStudent(t, "NewStudent")
	if verifyUser(t, ts, newUser)["valid"] != true {
		t.Errorf("the new student must be valid: %v", verifyUser(t, ts, newUser))
	}
}
