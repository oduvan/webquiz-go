package server_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// socketSend writes a text message to the socket.
func socketSend(t *testing.T, s *socket, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		t.Fatalf("write to the socket: %v", err)
	}
}

// randomizedQuiz has n questions Q1..Qn and randomize_questions on.
func randomizedQuiz(n int, title string, randomize bool) map[string]any {
	options := [][]any{{"A", "B"}, {"C", "D"}, {"E", "F"}, {"G", "H"}, {"I", "J"}, {"K", "L"}}
	var questions []any
	for i := 0; i < n; i++ {
		questions = append(questions, map[string]any{
			"question": fmt.Sprintf("Q%d", i+1), "options": options[i], "correct_answer": i % 2,
		})
	}
	return map[string]any{"title": title, "randomize_questions": randomize, "questions": questions}
}

// firstOfOrder reads question_order[0] of a registration reply.
func firstOfOrder(t *testing.T, registration map[string]any) (order []any, first float64) {
	t.Helper()
	order, _ = registration["question_order"].([]any)
	if len(order) == 0 {
		t.Fatalf("no question_order: %v", registration)
	}
	return order, floatOf(t, order[0])
}

// py: tests/test_live_stats_websocket.py::test_live_stats_websocket_shows_correct_first_question
func TestLiveStatsWebSocketShowsCorrectFirstQuestion(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, randomizedQuiz(5, "Randomized Quiz", true), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	if initial := live.next(); initial["type"] != "initial_state" { // empty on the first connection
		t.Fatalf("first message: %v", initial)
	}
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"})
	if res.Status != 200 {
		t.Fatalf("register: %d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	_, first := firstOfOrder(t, data)

	message := live.next()
	if message["type"] != "user_registered" || message["user_id"] != data["user_id"] || message["username"] != "testuser" {
		t.Errorf("message: %v", message)
	}
	if message["question_id"] != first {
		t.Errorf("expected first question %v in the WebSocket message, got %v", first, message["question_id"])
	}
	if message["state"] != "think" {
		t.Errorf("state: %v", message["state"])
	}
}

// py: tests/test_live_stats_websocket.py::test_live_stats_websocket_no_duplicate_questions
func TestLiveStatsWebSocketNoDuplicateQuestions(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, randomizedQuiz(3, "Randomized Quiz", true), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	if initial := live.next(); initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"})
	if res.Status != 200 {
		t.Fatalf("register: %d %s", res.Status, res.Body)
	}
	_, first := firstOfOrder(t, res.JSON(t))
	message := live.next()
	if message["type"] != "user_registered" {
		t.Fatalf("message: %v", message)
	}
	// The question in the WebSocket message is the first question of the student's order.
	if message["question_id"] != first {
		t.Errorf("WebSocket shows question %v, but the first in the order is %v", message["question_id"], first)
	}
}

// py: tests/test_live_stats_websocket.py::test_live_stats_websocket_with_approval_workflow
func TestLiveStatsWebSocketWithApprovalWorkflow(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, randomizedQuiz(4, "Randomized Quiz", true), map[string]any{"registration": map[string]any{"approve": true}})
	live := openSocket(t, ts, "/ws/live-stats")
	if initial := live.next(); initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	data := ts.PostJSON(t, "/api/register", map[string]any{"username": "testuser"}).JSON(t)
	userID := data["user_id"].(string)
	_, first := firstOfOrder(t, data)

	admin := ts.AdminClient(t, "test123")
	if res := admin.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID}); res.Status != 200 {
		t.Fatalf("approve-user: %d %s", res.Status, res.Body)
	}
	message := live.next()
	// The approval sends the same type as the registration.
	if message["type"] != "user_registered" || message["user_id"] != userID {
		t.Errorf("message: %v", message)
	}
	if message["question_id"] != first {
		t.Errorf("expected first question %v after the approval, got %v", first, message["question_id"])
	}
}

// py: tests/test_live_stats_websocket.py::test_live_stats_websocket_without_randomization
func TestLiveStatsWebSocketWithoutRandomization(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, randomizedQuiz(3, "Non-Randomized Quiz", false), nil)
	userID := ts.RegisterStudent(t, "testuser")
	initial := openSocket(t, ts, "/ws/live-stats").next()
	if initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	stats, ok := initial["live_stats"].(map[string]any)[userID].(map[string]any)
	if !ok {
		t.Fatalf("the user is not in live_stats: %v", initial["live_stats"])
	}
	// Question ids are strings in JSON; question 1 is in the "think" state.
	first, ok := stats["1"].(map[string]any)
	if !ok {
		t.Fatalf("question 1 not found in the user stats: %v", stats)
	}
	if first["state"] != "think" {
		t.Errorf("question 1 state: %v", first["state"])
	}
}

// py: tests/test_live_stats_websocket.py::test_live_stats_websocket_multiple_users_different_orders
func TestLiveStatsWebSocketMultipleUsersDifferentOrders(t *testing.T) {
	t.Parallel()
	ts := startTestYAML(t, randomizedQuiz(6, "Randomized Quiz", true), nil)
	live := openSocket(t, ts, "/ws/live-stats")
	if initial := live.next(); initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	firstQuestions := map[any]bool{}
	for i := 0; i < 5; i++ {
		if res := ts.PostJSON(t, "/api/register", map[string]any{"username": fmt.Sprintf("user%d", i)}); res.Status != 200 {
			t.Fatalf("register: %d %s", res.Status, res.Body)
		}
		message := live.next()
		if message["type"] != "user_registered" {
			t.Fatalf("message: %v", message)
		}
		firstQuestions[message["question_id"]] = true
	}
	// 6 questions and 5 users: very likely some users start with different questions.
	if len(firstQuestions) <= 1 {
		t.Errorf("expected different first questions, all got: %v", firstQuestions)
	}
}

// pongAfterPing sends a ping and expects a pong.
func pongAfterPing(t *testing.T, live *socket) {
	t.Helper()
	socketSend(t, live, `{"type": "ping"}`)
	if response := live.next(); response["type"] != "pong" {
		t.Errorf("response to ping: %v", response)
	}
}

// py: tests/test_live_stats_websocket.py::test_websocket_ping_pong
func TestWebSocketPingPong(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	live := openSocket(t, ts, "/ws/live-stats")
	if initial := live.next(); initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	pongAfterPing(t, live)
}

// py: tests/test_live_stats_websocket.py::test_websocket_client_disconnect_cleanup
func TestWebSocketClientDisconnectCleanup(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	first := openSocket(t, ts, "/ws/live-stats")
	first.next() // initial_state
	first.conn.Close(websocket.StatusNormalClosure, "")

	// The first client is gone. A second one still works, and a broadcast does not fail.
	second := openSocket(t, ts, "/ws/live-stats")
	if initial := second.next(); initial["type"] != "initial_state" {
		t.Fatalf("first message: %v", initial)
	}
	ts.RegisterStudent(t, "testuser")
	if message := second.next(); message["type"] != "user_registered" {
		t.Errorf("message: %v", message)
	}
}

// py: tests/test_live_stats_websocket.py::test_websocket_invalid_json_message
func TestWebSocketInvalidJSONMessage(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next() // initial_state
	socketSend(t, live, "invalid{json}data")
	pongAfterPing(t, live) // the connection stays open
}

// py: tests/test_live_stats_websocket.py::test_websocket_multiple_simultaneous_clients
func TestWebSocketMultipleSimultaneousClients(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	clients := []*socket{
		openSocket(t, ts, "/ws/live-stats"),
		openSocket(t, ts, "/ws/live-stats"),
		openSocket(t, ts, "/ws/live-stats"),
	}
	for i, client := range clients {
		if initial := client.next(); initial["type"] != "initial_state" {
			t.Fatalf("client %d: first message %v", i+1, initial)
		}
	}
	ts.RegisterStudent(t, "broadcasttest")
	for i, client := range clients {
		message := client.next()
		if message["type"] != "user_registered" || message["username"] != "broadcasttest" {
			t.Errorf("client %d: %v", i+1, message)
		}
	}
}
