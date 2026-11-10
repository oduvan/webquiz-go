package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// The Python tests slept to pass a time limit. These ports move a test clock
// (ServerOptions.Now) by the same amount instead.

func timedRows(t *testing.T, ts *testutil.TestServer, userID string) []any {
	t.Helper()
	data := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
	if data["test_completed"] != true {
		t.Fatalf("the test is not completed: %v", data)
	}
	return finalResults(t, data)["test_results"].([]any)
}

func submitTimed(t *testing.T, ts *testutil.TestServer, userID string, questionID int, answer any) *testutil.Response {
	t.Helper()
	return ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": questionID, "selected_answer": answer})
}

// py: tests/test_time_limits_submit.py::test_answer_within_grace_counts
func TestAnswerWithinGraceCounts(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 1}, nil, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(1200 * time.Millisecond) // after the limit, but inside the grace
	res := submitTimed(t, ts, userID, 1, 0)
	data := res.JSON(t)
	if res.Status != 200 || data["is_correct"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
	if _, ok := data["timed_out"]; ok {
		t.Errorf("timed_out must be absent: %v", data)
	}
}

// py: tests/test_time_limits_submit.py::test_late_answer_is_stored_as_timeout
func TestLateAnswerIsStoredAsTimeout(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 1}, nil, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(1800 * time.Millisecond) // after the limit and the grace
	res := submitTimed(t, ts, userID, 1, 0)
	data := res.JSON(t)
	if res.Status != 200 || data["timed_out"] != true || data["is_correct"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	row := timedRows(t, ts, userID)[0].(map[string]any)
	if row["selected_answer"] != "" || row["timed_out"] != true {
		t.Errorf("result row: %v", row)
	}
}

// py: tests/test_time_limits_submit.py::test_empty_answer_on_limited_question_is_timeout
func TestEmptyAnswerOnLimitedQuestionIsTimeout(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"per_question": 30}, nil, nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	res := submitTimed(t, ts, userID, 1, nil)
	if res.Status != 200 || res.JSON(t)["timed_out"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_submit.py::test_empty_answer_without_time_limit_is_rejected
func TestEmptyAnswerWithoutTimeLimitIsRejected(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(nil, nil, nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if res := submitTimed(t, ts, userID, 1, nil); res.Status != 400 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_submit.py::test_empty_answer_on_unlimited_question_is_rejected
func TestEmptyAnswerOnUnlimitedQuestionIsRejected(t *testing.T) {
	t.Parallel()
	quizzes := timedQuizFile(map[string]any{"per_question": 30}, []any{choiceQuestion("Q?", map[string]any{"time_limit": 0})}, nil)
	ts := startTimed(t, newStepClock(), quizzes, nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if res := submitTimed(t, ts, userID, 1, ""); res.Status != 400 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_submit.py::test_empty_answer_skips_checker
func TestEmptyAnswerSkipsChecker(t *testing.T) {
	t.Parallel()
	textQuestion := map[string]any{"question": "Type anything", "checker": "x = 1", "correct_value": ""}
	quizzes := timedQuizFile(map[string]any{"per_question": 30}, []any{textQuestion}, map[string]any{"show_right_answer": true})
	ts := startTimed(t, newStepClock(), quizzes, nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	res := submitTimed(t, ts, userID, 1, "")
	if res.Status != 200 || res.JSON(t)["is_correct"] != false {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_submit.py::test_answer_after_timeout_returns_stored_timeout
func TestAnswerAfterTimeoutReturnsStoredTimeout(t *testing.T) {
	t.Parallel()
	quizzes := timedQuizFile(map[string]any{"per_question": 30}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, map[string]any{"randomize_questions": true})
	ts := startTimed(t, newStepClock(), quizzes, nil)
	registration := ts.PostJSON(t, "/api/register", map[string]any{"username": "S"}).JSON(t)
	userID := registration["user_id"].(string)
	firstID := int(floatOf(t, registration["question_order"].([]any)[0]))
	startQuestion(t, ts, userID, firstID)
	if submitTimed(t, ts, userID, firstID, nil).JSON(t)["timed_out"] != true {
		t.Fatal("the empty answer must be a timeout")
	}
	late := submitTimed(t, ts, userID, firstID, 0) // a real answer arrives after the timeout
	if late.Status != 200 || late.JSON(t)["timed_out"] != true {
		t.Errorf("%d %s", late.Status, late.Body)
	}
}

func threeQuestions() []any {
	return []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil), choiceQuestion("Q3?", nil)}
}

// py: tests/test_time_limits_submit.py::test_whole_quiz_empty_answer_ends_quiz
func TestWholeQuizEmptyAnswerEndsQuiz(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"whole_quiz": 600}, threeQuestions(), nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	res := submitTimed(t, ts, userID, 1, nil)
	if res.Status != 200 || res.JSON(t)["test_completed"] != true {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	rows := timedRows(t, ts, userID)
	if len(rows) != 3 {
		t.Fatalf("rows: %v", rows)
	}
	for _, r := range rows {
		row := r.(map[string]any)
		if row["selected_answer"] != "" || row["timed_out"] != true {
			t.Errorf("row: %v", row)
		}
	}
	numIs(t, "time_taken of row 2", rows[1].(map[string]any)["time_taken"], "0")
	numIs(t, "time_taken of row 3", rows[2].(map[string]any)["time_taken"], "0")
}

// py: tests/test_time_limits_submit.py::test_whole_quiz_empty_answer_from_continue_screen
func TestWholeQuizEmptyAnswerFromContinueScreen(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"whole_quiz": 600}, threeQuestions(), nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if res := submitTimed(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// The student is on the "Continue" screen: Q2 was never opened.
	res := submitTimed(t, ts, userID, 2, nil)
	if res.Status != 200 || res.JSON(t)["test_completed"] != true {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	rows := timedRows(t, ts, userID)
	first := rows[0].(map[string]any)
	if first["selected_answer"] != "A" || first["timed_out"] == true {
		t.Errorf("row 1: %v", first)
	}
	if rows[1].(map[string]any)["timed_out"] != true || rows[2].(map[string]any)["timed_out"] != true {
		t.Errorf("rows 2 and 3: %v", rows[1:])
	}
}

// py: tests/test_time_limits_submit.py::test_whole_quiz_late_answer_ends_quiz
func TestWholeQuizLateAnswerEndsQuiz(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"whole_quiz": 1}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(1800 * time.Millisecond)
	data := submitTimed(t, ts, userID, 1, 0).JSON(t)
	if data["timed_out"] != true || data["test_completed"] != true {
		t.Errorf("%v", data)
	}
}

// py: tests/test_time_limits_submit.py::test_timeout_row_in_answers_csv
// The Python test slept 6 s for the CSV flush; the Go test closes the server, which writes the rows.
func TestTimeoutRowInAnswersCSV(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"per_question": 30}, nil, nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if submitTimed(t, ts, userID, 1, nil).JSON(t)["timed_out"] != true {
		t.Fatal("the empty answer must be a timeout")
	}
	ts.Server.Close()
	var answerFiles []string
	for _, name := range csvNames(t, ts.CSVDir) {
		if strings.HasSuffix(name, ".csv") && !strings.HasSuffix(name, ".users.csv") {
			answerFiles = append(answerFiles, name)
		}
	}
	if len(answerFiles) != 1 {
		t.Fatalf("answer files: %v", answerFiles)
	}
	lines := csvLines(t, ts.CSVDir+"/"+answerFiles[0])
	if lines[0] != "user_id,question,selected_answer,correct_answer,is_correct,time_taken_seconds" {
		t.Errorf("header: %s", lines[0])
	}
	if len(lines) < 2 {
		t.Fatalf("no answer row: %v", lines)
	}
	cells := strings.Split(lines[1], ",")
	if cells[2] != "" || cells[4] != "False" {
		t.Errorf("row: %s", lines[1])
	}
}

// py: tests/test_time_limits_submit.py::test_live_stats_gets_timeout_state
func TestLiveStatsGetsTimeoutState(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"per_question": 30}, nil, nil), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	submitTimed(t, ts, userID, 1, nil)
	var states []string
	for i := 0; i < 5; i++ {
		message := live.next()
		if message["type"] == "state_update" {
			states = append(states, message["state"].(string))
		}
		if len(states) > 0 && states[len(states)-1] == "timeout" {
			return
		}
	}
	t.Errorf("no timeout state, got %v", states)
}

// ---- Requests sent by hand (not by the quiz page) must not avoid the limit ----

// py: tests/test_time_limits_submit.py::test_answer_without_question_start_counts_from_registration
func TestAnswerWithoutQuestionStartCountsFromRegistration(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 1}, nil, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	clock.Advance(1800 * time.Millisecond) // no question-start is ever sent
	if submitTimed(t, ts, userID, 1, 0).JSON(t)["timed_out"] != true {
		t.Error("the answer must be a timeout")
	}
}

// py: tests/test_time_limits_submit.py::test_answer_without_question_start_counts_from_previous_answer
func TestAnswerWithoutQuestionStartCountsFromPreviousAnswer(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 1}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if res := submitTimed(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	clock.Advance(1800 * time.Millisecond) // no question-start for Q2
	if submitTimed(t, ts, userID, 2, 0).JSON(t)["timed_out"] != true {
		t.Error("the answer must be a timeout")
	}
}

// py: tests/test_time_limits_submit.py::test_continue_screen_time_still_not_counted
func TestContinueScreenTimeStillNotCounted(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 1}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	if res := submitTimed(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	clock.Advance(1800 * time.Millisecond) // the student stays on the Continue screen
	startQuestion(t, ts, userID, 2)
	data := submitTimed(t, ts, userID, 2, 0).JSON(t)
	if _, ok := data["timed_out"]; ok {
		t.Errorf("timed_out must be absent: %v", data)
	}
}

// py: tests/test_time_limits_submit.py::test_whole_quiz_without_question_start_counts_from_registration
func TestWholeQuizWithoutQuestionStartCountsFromRegistration(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"whole_quiz": 1}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, nil), shortGrace)
	userID := ts.RegisterStudent(t, "Student")
	clock.Advance(1800 * time.Millisecond) // no question-start is ever sent
	data := submitTimed(t, ts, userID, 1, 0).JSON(t)
	if data["timed_out"] != true || data["test_completed"] != true {
		t.Errorf("%v", data)
	}
}

// py: tests/test_time_limits_submit.py::test_question_start_with_unknown_question_is_rejected
func TestQuestionStartWithUnknownQuestionIsRejected(t *testing.T) {
	t.Parallel()
	ts := startTimed(t, newStepClock(), timedQuizFile(map[string]any{"per_question": 30}, nil, nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 999})
	if res.Status != 404 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_time_limits_submit.py::test_question_start_for_a_later_question_does_not_block_the_open_clock
func TestQuestionStartForALaterQuestionDoesNotBlockTheOpenClock(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 30}, []any{choiceQuestion("Q1?", nil), choiceQuestion("Q2?", nil)}, nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 2) // not the student's question yet
	startQuestion(t, ts, userID, 1)
	clock.Advance(1100 * time.Millisecond)
	if remaining := floatOf(t, startQuestion(t, ts, userID, 1)["remaining_seconds"]); remaining > 29 { // Q1's clock is running
		t.Errorf("remaining_seconds: %v", remaining)
	}
}

// py: tests/test_time_limits_submit.py::test_answer_to_another_question_does_not_stop_the_open_clock
func TestAnswerToAnotherQuestionDoesNotStopTheOpenClock(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	ts := startTimed(t, clock, timedQuizFile(map[string]any{"per_question": 30}, threeQuestions(), nil), nil)
	userID := ts.RegisterStudent(t, "Student")
	startQuestion(t, ts, userID, 1)
	clock.Advance(1100 * time.Millisecond)
	if res := submitTimed(t, ts, userID, 2, 0); res.Status != 200 { // no order check without randomization
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if remaining := floatOf(t, startQuestion(t, ts, userID, 1)["remaining_seconds"]); remaining > 29 { // Q1's clock still runs
		t.Errorf("remaining_seconds: %v", remaining)
	}
}
