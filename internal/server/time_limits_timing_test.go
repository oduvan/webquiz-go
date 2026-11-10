package server_test

import (
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func singleQuestionQuiz(extra map[string]any, questions ...any) map[string]any {
	quiz := map[string]any{"title": "T", "questions": questions}
	for key, value := range extra {
		quiz[key] = value
	}
	return map[string]any{"q.yaml": quiz}
}

// py: tests/test_time_limits_timing.py::test_first_question_clock_starts_at_question_start
func TestFirstQuestionClockStartsAtQuestionStart(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(nil, choiceQuestion("Q?", nil)), nil)
	userID := ts.RegisterStudent(t, "Student")
	clock.Advance(1200 * time.Millisecond) // the page has not shown the question yet
	startQuestion(t, ts, userID, 1)
	res := submitTimed(t, ts, userID, 1, 0)
	if res.Status != 200 || floatOf(t, res.JSON(t)["time_taken"]) >= 1.0 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_timing.py::test_first_question_clock_starts_after_approval_at_question_start
func TestFirstQuestionClockStartsAfterApproval(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(nil, choiceQuestion("Q?", nil)), nil, func(o *testutil.Options) {
		o.Config = map[string]any{"registration": map[string]any{"approve": true}}
	})
	userID := ts.RegisterStudent(t, "Student")
	admin := ts.AdminClient(t, "test123")
	if approve := admin.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID}); approve.Status != 200 {
		t.Fatalf("approve-user: %d %s", approve.Status, approve.Body)
	}
	clock.Advance(1200 * time.Millisecond) // the student has not pressed "Check status" yet
	startQuestion(t, ts, userID, 1)
	res := submitTimed(t, ts, userID, 1, 0)
	if res.Status != 200 || floatOf(t, res.JSON(t)["time_taken"]) >= 1.0 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_timing.py::test_question_start_returns_remaining_and_reload_does_not_reset
func TestQuestionStartReturnsRemainingAndReloadDoesNotReset(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"per_question": 30}}, choiceQuestion("Q?", nil)), nil)
	userID := ts.RegisterStudent(t, "Student")
	first := startQuestion(t, ts, userID, 1)
	if remaining := floatOf(t, first["remaining_seconds"]); remaining <= 29 || remaining > 30 {
		t.Errorf("remaining_seconds: %v", remaining)
	}
	if _, ok := first["quiz_remaining_seconds"]; ok {
		t.Errorf("quiz_remaining_seconds must be absent: %v", first)
	}
	clock.Advance(1100 * time.Millisecond)
	again := startQuestion(t, ts, userID, 1) // a page reload sends question-start again
	if remaining := floatOf(t, again["remaining_seconds"]); remaining > 29 {
		t.Errorf("remaining_seconds after a reload: %v", remaining)
	}
}

// py: tests/test_time_limits_timing.py::test_question_start_returns_quiz_remaining
func TestQuestionStartReturnsQuizRemaining(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"whole_quiz": 600}}, choiceQuestion("Q?", nil)), nil)
	userID := ts.RegisterStudent(t, "Student")
	data := startQuestion(t, ts, userID, 1)
	if remaining := floatOf(t, data["quiz_remaining_seconds"]); remaining <= 599 || remaining > 600 {
		t.Errorf("quiz_remaining_seconds: %v", remaining)
	}
	if _, ok := data["remaining_seconds"]; ok {
		t.Errorf("remaining_seconds must be absent: %v", data)
	}
}

// onlySuccess checks that a question-start reply is exactly {"status": "success"}.
func onlySuccess(t *testing.T, data map[string]any) {
	t.Helper()
	if len(data) != 1 || data["status"] != "success" {
		t.Errorf("question-start reply: %v", data)
	}
}

// py: tests/test_time_limits_timing.py::test_unlimited_question_has_no_remaining
func TestUnlimitedQuestionHasNoRemaining(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"per_question": 30}}, choiceQuestion("Q?", map[string]any{"time_limit": 0})), nil)
	userID := ts.RegisterStudent(t, "Student")
	onlySuccess(t, startQuestion(t, ts, userID, 1))
}

// The Python tests looked for "time_limit": 20 in the page text and for
// "const quizTimeLimit = 0". The Go page holds these in its boot data.
// py: tests/test_time_limits_timing.py::test_page_contains_time_limits
func TestPageContainsTimeLimits(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"per_question": 60}},
		choiceQuestion("Q1?", nil), choiceQuestion("Q2?", map[string]any{"time_limit": 20})), nil)
	boot := pageBoot(t, ts.GetJSON(t, "/").Text())
	numIs(t, "time_limit of Q1", question(t, boot, 0)["time_limit"], "60")
	numIs(t, "time_limit of Q2", question(t, boot, 1)["time_limit"], "20")
	numIs(t, "quiz_time_limit", boot["quiz_time_limit"], "0")
}

// py: tests/test_time_limits_timing.py::test_page_contains_whole_quiz_time_limit
func TestPageContainsWholeQuizTimeLimit(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), singleQuestionQuiz(map[string]any{"time_limit": map[string]any{"whole_quiz": 900}}, choiceQuestion("Q?", nil)), nil)
	numIs(t, "quiz_time_limit", pageBoot(t, ts.GetJSON(t, "/").Text())["quiz_time_limit"], "900")
}

// py: tests/test_time_limits_timing.py::test_malformed_time_limit_loads_without_limits
func TestMalformedTimeLimitLoadsWithoutLimits(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), singleQuestionQuiz(map[string]any{"time_limit": "soon"}, choiceQuestion("Q?", nil)), nil)
	userID := ts.RegisterStudent(t, "Student")
	onlySuccess(t, startQuestion(t, ts, userID, 1))
	numIs(t, "quiz_time_limit", pageBoot(t, ts.GetJSON(t, "/").Text())["quiz_time_limit"], "0")
}
