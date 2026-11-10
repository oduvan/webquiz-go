package server_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func paThreeQuestions(extra map[string]any) map[string]any {
	quiz := map[string]any{
		"title": "Retry Quiz",
		"questions": []any{
			map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0},
			map[string]any{"question": "Q2", "options": []any{"C", "D"}, "correct_answer": 1},
			map[string]any{"question": "Q3", "options": []any{"E", "F"}, "correct_answer": 0},
		},
	}
	for key, value := range extra {
		quiz[key] = value
	}
	return quiz
}

func paRetryServer(t *testing.T, quiz map[string]any, config map[string]any) *testutil.TestServer {
	t.Helper()
	return testutil.StartServer(t, testutil.Options{Config: config, Quizzes: map[string]any{"quiz.yaml": quiz}})
}

func paRegisterWith(t *testing.T, ts *testutil.TestServer, name string, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"username": name}
	for key, value := range extra {
		body[key] = value
	}
	return partARegister(t, ts, body)
}

func paSameJSON(t *testing.T, a, b *testutil.Response) bool {
	t.Helper()
	return reflect.DeepEqual(a.JSON(t), b.JSON(t))
}

// py: tests/test_connection_retry.py::test_repeated_answer_returns_stored_result
func TestRepeatedAnswerReturnsStoredResult(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	userID := paRegisterWith(t, ts, "student", nil)["user_id"].(string)

	first := partAAnswer(t, ts, userID, 1, 0)
	if first.Status != 200 {
		t.Fatalf("%d %s", first.Status, first.Body)
	}
	time.Sleep(200 * time.Millisecond)
	retry := partAAnswer(t, ts, userID, 1, 0)
	if retry.Status != 200 || !paSameJSON(t, retry, first) {
		t.Errorf("retry must return the stored result, including time_taken: %d %s vs %s", retry.Status, retry.Body, first.Body)
	}

	// The repeated answer must not count as a second answer.
	if res := partAAnswer(t, ts, userID, 2, 1); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	data := paVerifyUser(t, ts, userID)
	if data["test_completed"] != false || paNextIndex(t, data) != "2" {
		t.Errorf("quiz must not be completed early by a repeated answer: %v", data)
	}
	if res := partAAnswer(t, ts, userID, 3, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	data = paVerifyUser(t, ts, userID)
	results, _ := data["final_results"].(map[string]any)
	if data["test_completed"] != true || fmt.Sprint(results["total_count"]) != "3" || fmt.Sprint(results["correct_count"]) != "3" {
		t.Fatalf("%v", data)
	}
	var questions []string
	for _, item := range results["test_results"].([]any) {
		questions = append(questions, item.(map[string]any)["question"].(string))
	}
	if !slices.Equal(questions, []string{"Q1", "Q2", "Q3"}) {
		t.Errorf("questions %v", questions)
	}
}

// py: tests/test_connection_retry.py::test_different_answer_to_answered_question_rejected
func TestDifferentAnswerToAnsweredQuestionRejected(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	userID := paRegisterWith(t, ts, "student", nil)["user_id"].(string)
	if res := partAAnswer(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	res := partAAnswer(t, ts, userID, 1, 1)
	if res.Status != 409 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
	if got := paNextIndex(t, paVerifyUser(t, ts, userID)); got != "1" {
		t.Errorf("next_question_index = %s", got)
	}
	partAAnswer(t, ts, userID, 2, 1)
	partAAnswer(t, ts, userID, 3, 0)
	results := paVerifyUser(t, ts, userID)["final_results"].(map[string]any)
	first := results["test_results"].([]any)[0].(map[string]any)
	if fmt.Sprint(results["total_count"]) != "3" || first["is_correct"] != true {
		t.Errorf("first answer must stay recorded: %v", results)
	}
}

// py: tests/test_connection_retry.py::test_repeated_text_answer_returns_stored_result
func TestRepeatedTextAnswerReturnsStoredResult(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, map[string]any{
		"title": "Text Retry",
		"questions": []any{
			map[string]any{"question": "Type 42", "checker": "assert to_int(user_answer) == 42", "correct_value": "42"},
			map[string]any{"question": "Q2", "options": []any{"A", "B"}, "correct_answer": 0},
		},
	}, nil)
	userID := paRegisterWith(t, ts, "student", nil)["user_id"].(string)
	first := partAAnswer(t, ts, userID, 1, "41")
	retry := partAAnswer(t, ts, userID, 1, "41")
	if retry.Status != 200 || !paSameJSON(t, retry, first) || retry.JSON(t)["is_correct"] != false {
		t.Errorf("%d %s vs %s", retry.Status, retry.Body, first.Body)
	}
	if res := partAAnswer(t, ts, userID, 1, "42"); res.Status != 409 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_connection_retry.py::test_repeated_answer_with_randomization
func TestRepeatedAnswerWithRandomization(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(map[string]any{"randomize_questions": true}), nil)
	data := paRegisterWith(t, ts, "student", nil)
	userID := data["user_id"].(string)
	order := paOrderOf(t, data)
	first := partAAnswer(t, ts, userID, order[0], 0)
	retry := partAAnswer(t, ts, userID, order[0], 0)
	if retry.Status != 200 || !paSameJSON(t, retry, first) {
		t.Errorf("%d %s vs %s", retry.Status, retry.Body, first.Body)
	}
	if got := paNextIndex(t, paVerifyUser(t, ts, userID)); got != "1" {
		t.Errorf("next_question_index = %s", got)
	}
}

// py: tests/test_connection_retry.py::test_repeated_last_answer_after_completion
func TestRepeatedLastAnswerAfterCompletion(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(map[string]any{"randomize_questions": true}), nil)
	data := paRegisterWith(t, ts, "student", nil)
	userID := data["user_id"].(string)
	order := paOrderOf(t, data)
	var last *testutil.Response
	for _, id := range order {
		last = partAAnswer(t, ts, userID, id, 0)
		if last.Status != 200 {
			t.Fatalf("%d %s", last.Status, last.Body)
		}
	}
	retry := partAAnswer(t, ts, userID, order[len(order)-1], 0)
	if retry.Status != 200 || !paSameJSON(t, retry, last) {
		t.Errorf("%d %s vs %s", retry.Status, retry.Body, last.Body)
	}
	results := paVerifyUser(t, ts, userID)["final_results"].(map[string]any)
	if fmt.Sprint(results["total_count"]) != "3" {
		t.Errorf("%v", results)
	}
}

// py: tests/test_connection_retry.py::test_repeated_answer_with_hidden_feedback
func TestRepeatedAnswerWithHiddenFeedback(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(map[string]any{"show_right_answer": false}), nil)
	userID := paRegisterWith(t, ts, "student", nil)["user_id"].(string)
	partAAnswer(t, ts, userID, 1, 1)
	retry := partAAnswer(t, ts, userID, 1, 1)
	if _, has := retry.JSON(t)["is_correct"]; retry.Status != 200 || has {
		t.Errorf("%d %s", retry.Status, retry.Body)
	}
}

// py: tests/test_connection_retry.py::test_late_question_start_for_answered_question_is_ignored
func TestLateQuestionStartForAnsweredQuestionIsIgnored(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	userID := paRegisterWith(t, ts, "student", nil)["user_id"].(string)
	if res := partAAnswer(t, ts, userID, 1, 0); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// A late retry of the notice for the question that is already answered.
	if res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 1}); res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	time.Sleep(1500 * time.Millisecond)
	ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 2})
	res := partAAnswer(t, ts, userID, 2, 1)
	if taken := paFloatField(t, res.JSON(t), "time_taken"); taken >= 1 {
		t.Errorf("timing for question 2 must start at its own notice, time_taken = %v", taken)
	}
}

// py: tests/test_connection_retry.py::test_question_start_unknown_user_returns_404
func TestQuestionStartUnknownUserReturns404(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": "000000", "question_id": 1})
	if res.Status != 404 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_connection_retry.py::test_repeated_registration_returns_same_user
func TestRepeatedRegistrationReturnsSameUser(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	token := map[string]any{"registration_token": "token-1"}
	first := paRegisterWith(t, ts, "anna", token)
	retry := paRegisterWith(t, ts, "anna", token)
	if retry["user_id"] != first["user_id"] || retry["username"] != "anna" || retry["approved"] != true {
		t.Errorf("first %v retry %v", first, retry)
	}
}

// py: tests/test_connection_retry.py::test_registration_token_does_not_bypass_unique_username
func TestRegistrationTokenDoesNotBypassUniqueUsername(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), nil)
	paRegisterWith(t, ts, "anna", map[string]any{"registration_token": "token-1"})
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "anna", "registration_token": "token-2"})
	if res.Status != 400 {
		t.Errorf("another token: %d %s", res.Status, res.Body)
	}
	if res := ts.PostJSON(t, "/api/register", map[string]any{"username": "anna"}); res.Status != 400 {
		t.Errorf("no token: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_connection_retry.py::test_repeated_registration_keeps_question_order
func TestRepeatedRegistrationKeepsQuestionOrder(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(map[string]any{"randomize_questions": true}), nil)
	token := map[string]any{"registration_token": "token-1"}
	first := paRegisterWith(t, ts, "anna", token)
	retry := paRegisterWith(t, ts, "anna", token)
	if !slices.Equal(paOrderOf(t, retry), paOrderOf(t, first)) {
		t.Errorf("first %v retry %v", first, retry)
	}
}

// py: tests/test_connection_retry.py::test_repeated_registration_with_approval
func TestRepeatedRegistrationWithApproval(t *testing.T) {
	t.Parallel()
	ts := paRetryServer(t, paThreeQuestions(nil), map[string]any{"registration": map[string]any{"approve": true}})
	token := map[string]any{"registration_token": "token-1"}
	first := paRegisterWith(t, ts, "anna", token)
	if first["requires_approval"] != true || first["approved"] != false {
		t.Fatalf("%v", first)
	}
	retry := paRegisterWith(t, ts, "anna", token)
	if retry["user_id"] != first["user_id"] || retry["approved"] != false {
		t.Errorf("retry %v", retry)
	}
	paApproveUser(t, ts, first["user_id"].(string), testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	if retry := paRegisterWith(t, ts, "anna", token); retry["approved"] != true {
		t.Errorf("after approval: %v", retry)
	}
}
