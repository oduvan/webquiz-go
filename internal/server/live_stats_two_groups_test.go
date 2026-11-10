package server_test

import (
	"encoding/json"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// twoGroupQuiz is the Test Quiz of the Python tests; it has two questions
// unless a third is asked for.
func twoGroupQuiz(questions int, extra map[string]any) map[string]any {
	all := []any{
		map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0},
		map[string]any{"question": "Q2", "options": []any{"C", "D"}, "correct_answer": 1},
		map[string]any{"question": "Q3", "options": []any{"E", "F"}, "correct_answer": 0},
	}
	quiz := map[string]any{"title": "Test Quiz", "questions": all[:questions]}
	for key, value := range extra {
		quiz[key] = value
	}
	return quiz
}

// startTestYAML starts a server with the quiz in test.yaml (the file name of
// the Python tests) and optional config.
func startTestYAML(t *testing.T, quiz map[string]any, config map[string]any) *testutil.TestServer {
	t.Helper()
	return testutil.StartServer(t, testutil.Options{Config: config, Quizzes: map[string]any{"test.yaml": quiz}})
}

// py: tests/test_live_stats_two_groups.py::test_initial_state_includes_completed_users
func TestInitialStateIncludesCompletedUsers(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(2, nil), nil)
	userID := ts.RegisterStudent(t, "testuser")
	initial := openSocket(t, ts, "/ws/live-stats").next()
	if initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	completed, ok := initial["completed_users"].(map[string]any)
	if !ok {
		t.Fatalf("completed_users missing: %v", initial)
	}
	if value, present := completed[userID]; !present || value != false { // not completed yet
		t.Errorf("completed_users: %v", completed)
	}
}

// py: tests/test_live_stats_two_groups.py::test_state_update_includes_completed_flag
func TestStateUpdateIncludesCompletedFlag(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(2, nil), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	userID := ts.RegisterStudent(t, "testuser")
	live.next() // user_registered
	answerOK(t, ts, userID, 1, 0)
	message := live.next()
	if message["type"] != "state_update" {
		t.Fatalf("message: %v", message)
	}
	if completed, ok := message["completed"]; !ok || completed != false { // 1 of 2 questions answered
		t.Errorf("completed: %v", message)
	}
}

// py: tests/test_live_stats_two_groups.py::test_user_completes_test_completed_flag_true
func TestUserCompletesCompletedFlagTrue(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(2, nil), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	userID := ts.RegisterStudent(t, "testuser")
	live.next() // user_registered
	answerOK(t, ts, userID, 1, 0)
	if first := live.next(); first["completed"] != false {
		t.Errorf("after the first answer: %v", first)
	}
	answerOK(t, ts, userID, 2, 1)
	if last := live.next(); last["completed"] != true {
		t.Errorf("after the last answer: %v", last)
	}
}

// py: tests/test_live_stats_two_groups.py::test_multiple_users_different_completion_status
func TestMultipleUsersDifferentCompletionStatus(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(3, nil), nil)
	user1 := ts.RegisterStudent(t, "user1")
	user2 := ts.RegisterStudent(t, "user2")
	answerOK(t, ts, user1, 1, 0)
	answerOK(t, ts, user1, 2, 1)
	answerOK(t, ts, user1, 3, 0)
	answerOK(t, ts, user2, 1, 0) // user2 answers only the first question

	initial := openSocket(t, ts, "/ws/live-stats").next()
	if initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	completed := initial["completed_users"].(map[string]any)
	if completed[user1] != true || completed[user2] != false {
		t.Errorf("completed_users: %v", completed)
	}
}

// py: tests/test_live_stats_two_groups.py::test_completed_users_tracked_across_reconnection
func TestCompletedUsersTrackedAcrossReconnection(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(2, nil), nil)
	userID := ts.RegisterStudent(t, "testuser")
	answerOK(t, ts, userID, 1, 0)
	answerOK(t, ts, userID, 2, 1)
	initial := openSocket(t, ts, "/ws/live-stats").next() // a new client
	if initial["completed_users"].(map[string]any)[userID] != true {
		t.Errorf("completed_users: %v", initial["completed_users"])
	}
}

// py: tests/test_live_stats_two_groups.py::test_no_users_empty_completed_dict
func TestNoUsersEmptyCompletedDict(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	initial := openSocket(t, ts, "/ws/live-stats").next()
	completed, ok := initial["completed_users"].(map[string]any)
	if initial["type"] != "initial_state" || !ok || len(completed) != 0 {
		t.Errorf("initial_state: %v", initial)
	}
}

// py: tests/test_live_stats_two_groups.py::test_quiz_switch_resets_completed_users
func TestQuizSwitchResetsCompletedUsers(t *testing.T) {
	t.Parallel()
	quiz := twoGroupQuiz(1, nil)
	// default.yaml loads by itself; the test then switches to quiz2.yaml.
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"default.yaml": quiz, "quiz2.yaml": quiz}})
	userID := ts.RegisterStudent(t, "testuser")
	answerOK(t, ts, userID, 1, 0)

	live := openSocket(t, ts, "/ws/live-stats")
	initial := live.next()
	if initial["completed_users"].(map[string]any)[userID] != true {
		t.Fatalf("completed_users: %v", initial["completed_users"])
	}
	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "quiz2.yaml"}); res.Status != 200 {
		t.Fatalf("switch-quiz: %d %s", res.Status, res.Body)
	}
	if message := live.next(); message["type"] != "quiz_switched" {
		t.Errorf("message after the switch: %v", message)
	}
	// After the switch no user exists: the quiz switch resets all state.
	again := openSocket(t, ts, "/ws/live-stats").next()
	if users, _ := again["completed_users"].(map[string]any); len(users) != 0 {
		t.Errorf("completed_users after the switch: %v", users)
	}
}

// py: tests/test_live_stats_two_groups.py::test_completion_with_randomized_questions
func TestCompletionWithRandomizedQuestions(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(3, map[string]any{"title": "Randomized Quiz", "randomize_questions": true}), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	registration := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"}).JSON(t)
	userID := registration["user_id"].(string)
	order := registration["question_order"].([]any)
	live.next() // user_registered
	for i, id := range order {
		questionID, _ := id.(json.Number).Int64()
		answerOK(t, ts, userID, int(questionID), 0)
		message := live.next()
		if isLast := i == len(order)-1; message["completed"] != isLast {
			t.Errorf("after question %d (%d of %d): %v", questionID, i+1, len(order), message)
		}
	}
}

// py: tests/test_live_stats_two_groups.py::test_approval_workflow_completion_tracking
func TestApprovalWorkflowCompletionTracking(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, twoGroupQuiz(2, nil), map[string]any{"registration": map[string]any{"approve": true}})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	userID := ts.RegisterStudent(t, "testuser")
	admin := ts.AdminClient(t, "test123")
	if res := admin.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID}); res.Status != 200 {
		t.Fatalf("approve-user: %d %s", res.Status, res.Body)
	}
	live.next() // user_registered after the approval
	answerOK(t, ts, userID, 1, 0)
	if message := live.next(); message["completed"] != false {
		t.Errorf("after the first answer: %v", message)
	}
	answerOK(t, ts, userID, 2, 1)
	if message := live.next(); message["completed"] != true {
		t.Errorf("after the last answer: %v", message)
	}
}
