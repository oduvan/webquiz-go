package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// post sends a JSON request without the testing helpers, so that it can run
// in its own goroutine (t.Fatal must not be called there).
func post(ts *testutil.TestServer, path string, body any) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	res, err := ts.HTTP.Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	reply, err := io.ReadAll(res.Body)
	return res.StatusCode, reply, err
}

// reply is the result of one request sent by atOnce.
type reply struct {
	status int
	body   []byte
	err    error
}

// atOnce sends the same request from n goroutines that all start together,
// like a page that retries while its first request is still running.
func atOnce(ts *testutil.TestServer, n int, path string, body any) []reply {
	replies := make([]reply, n)
	start := make(chan struct{})
	var requests sync.WaitGroup
	for i := range n {
		requests.Go(func() {
			<-start
			status, data, err := post(ts, path, body)
			replies[i] = reply{status, data, err}
		})
	}
	close(start)
	requests.Wait()
	return replies
}

// checkSameReplies fails unless every reply is 200 with the same body.
func checkSameReplies(t *testing.T, replies []reply) {
	t.Helper()
	for i, r := range replies {
		if r.err != nil {
			t.Fatalf("request %d: %v", i, r.err)
		}
		if r.status != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, r.status, r.body)
		}
		if !bytes.Equal(r.body, replies[0].body) {
			t.Fatalf("request %d got another reply:\n%s\nfirst:\n%s", i, r.body, replies[0].body)
		}
	}
}

// liveStatsState returns the initial_state message of the live stats socket.
func liveStatsState(t *testing.T, ts *testutil.TestServer) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/live-stats", nil)
	if err != nil {
		t.Fatalf("dial the live stats socket: %v", err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read the initial state: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil || state["type"] != "initial_state" {
		t.Fatalf("not an initial state: %s", data)
	}
	return state
}

// csvLines returns the lines of a CSV file the server wrote.
func csvLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\r\n"), "\r\n")
}

// Review Focus 1: the same answer arrives 20 times at once (a retry after a
// timeout, a double click). Python handled requests one by one; Go runs them
// in parallel. Every request must get the same reply and the answer must be
// stored once. The checker question also covers the path where the answer is
// checked without the lock.
func TestConcurrentSubmitSameAnswer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		question map[string]any
		answer   any
	}{
		{"choice", map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 1}, 1},
		{"checker", map[string]any{"question": "Q1", "checker": "assert user_answer.strip() == '4', 'Expected 4'", "correct_value": "4"}, "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"q.yaml": map[string]any{
				"title": "Retry",
				"questions": []any{
					tc.question,
					map[string]any{"question": "Q2", "options": []any{"C", "D"}, "correct_answer": 0},
				},
			}}})
			userID := ts.RegisterStudent(t, "Ann")
			if res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 1}); res.Status != 200 {
				t.Fatalf("question-start: %d %s", res.Status, res.Body)
			}

			replies := atOnce(ts, 20, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": tc.answer})
			checkSameReplies(t, replies)

			verify := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
			if verify["test_completed"] != false || verify["next_question_index"] != json.Number("1") {
				t.Fatalf("after one answer sent 20 times: %v", verify)
			}
			if res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 2, "selected_answer": 0}); res.Status != 200 {
				t.Fatalf("second answer: %d %s", res.Status, res.Body)
			}
			results := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)["final_results"].(map[string]any)
			if results["total_count"] != json.Number("2") || len(results["test_results"].([]any)) != 2 {
				t.Fatalf("final results: %v", results)
			}
			if completed := liveStatsState(t, ts)["completed_users"].(map[string]any); completed[userID] != true {
				t.Errorf("live stats: completed_users %v", completed)
			}

			ts.Server.Close() // writes the waiting rows
			if lines := csvLines(t, filepath.Join(ts.CSVDir, "q_0001.csv")); len(lines) != 3 {
				t.Errorf("the answers CSV must have the header and 2 rows:\n%s", strings.Join(lines, "\n"))
			}
		})
	}
}

// Review Focus 1: a registration sent 20 times at once with the same token
// makes one student, and every request gets that student.
func TestConcurrentRegisterSameToken(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"registration": map[string]any{"fields": []any{"Grade"}}}})
	replies := atOnce(ts, 20, "/api/register", map[string]any{"username": "Ann", "grade": "7", "registration_token": "token-1"})
	checkSameReplies(t, replies)

	var first map[string]any
	if err := json.Unmarshal(replies[0].body, &first); err != nil {
		t.Fatal(err)
	}
	userID, _ := first["user_id"].(string)
	if len(userID) != 6 || first["username"] != "Ann" {
		t.Fatalf("registration reply: %v", first)
	}
	if users := liveStatsState(t, ts)["users"].(map[string]any); len(users) != 1 || users[userID] != "Ann" {
		t.Fatalf("live stats users: %v", users)
	}
	for _, body := range []map[string]any{
		{"username": "Ann", "grade": "7"},
		{"username": "Ann", "grade": "7", "registration_token": "token-2"},
	} {
		if res := ts.PostJSON(t, "/api/register", body); res.Status != http.StatusBadRequest {
			t.Errorf("%v: %d %s, want 400 (the name is taken)", body, res.Status, res.Body)
		}
	}

	ts.Server.Close() // writes the users CSV
	lines := csvLines(t, filepath.Join(ts.CSVDir, "test_quiz_0001.users.csv"))
	if len(lines) != 2 || !strings.HasPrefix(lines[1], userID+",Ann,7,") {
		t.Errorf("the users CSV must have the header and one student:\n%s", strings.Join(lines, "\n"))
	}
}

// slowChecker runs until the checker time limit (2 s) stops it.
const slowChecker = "s = 'ab' * 200000\nwhile True:\n    t = s.upper()"

// Review Focus 4: a checker that runs for its whole time limit does not hold
// up other students, and its answer is marked wrong.
func TestSlowCheckerDoesNotBlockOtherStudents(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"q.yaml": map[string]any{
		"title": "Slow",
		"questions": []any{
			map[string]any{"question": "Type anything", "checker": slowChecker, "correct_value": "x"},
			map[string]any{"question": "Q2", "options": []any{"A", "B"}, "correct_answer": 0},
		},
	}}})
	slowID := ts.RegisterStudent(t, "Slow")

	type result struct {
		reply
		took time.Duration
	}
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		status, body, err := post(ts, "/api/submit-answer", map[string]any{"user_id": slowID, "question_id": 1, "selected_answer": "x"})
		done <- result{reply{status, body, err}, time.Since(started)}
	}()
	time.Sleep(300 * time.Millisecond) // the slow checker is running now

	other := time.Now()
	otherID := ts.RegisterStudent(t, "Other")
	ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": otherID, "question_id": 2})
	answer := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": otherID, "question_id": 2, "selected_answer": 0})
	verify := ts.GetJSON(t, "/api/verify-user/"+otherID)
	took := time.Since(other)
	if answer.Status != 200 || answer.JSON(t)["is_correct"] != true || verify.Status != 200 {
		t.Fatalf("the other student: %d %s / %d %s", answer.Status, answer.Body, verify.Status, verify.Body)
	}
	if took > time.Second {
		t.Errorf("the other student waited %v while the slow checker ran", took)
	}
	select {
	case r := <-done:
		t.Fatalf("the slow checker finished too early (%v): %d %s", r.took, r.status, r.body)
	default:
	}

	r := <-done
	if r.err != nil || r.status != 200 {
		t.Fatalf("slow answer: %v %d %s", r.err, r.status, r.body)
	}
	var data map[string]any
	if err := json.Unmarshal(r.body, &data); err != nil {
		t.Fatal(err)
	}
	if data["is_correct"] != false || !strings.Contains(fmt.Sprint(data["checker_error"]), "2s") {
		t.Errorf("slow answer reply: %s", r.body)
	}
	if r.took > 4*time.Second {
		t.Errorf("the slow answer took %v; the checker limit is 2 s", r.took)
	}
}
