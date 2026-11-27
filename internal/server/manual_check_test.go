package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Text questions that the teacher checks by hand (manual_check: true). The
// student's answer waits in a review queue; teachers open the review page,
// which is fed over WS /ws/review, and mark each answer right or wrong.
// These are the tests of tests/test_manual_check.py (WebQuiz 1.x), plus a
// few for the Go server (heartbeat, review ids, the page).

func manualQuiz() map[string]any {
	return map[string]any{
		"title":             "Manual Check Quiz",
		"show_right_answer": true,
		"questions": []any{
			map[string]any{"question": "Why is the sky blue?", "manual_check": true, "correct_value": "Rayleigh scattering", "points": 2},
			map[string]any{"question": "2 + 2?", "options": []any{"3", "4"}, "correct_answer": 1},
			map[string]any{"question": "Name a prime number", "manual_check": true},
		},
	}
}

type manualOptions struct {
	quiz          map[string]any
	config        map[string]any
	serverOptions server.Options
}

func startManual(t *testing.T, opts manualOptions) *testutil.TestServer {
	t.Helper()
	quiz := opts.quiz
	if quiz == nil {
		quiz = manualQuiz()
	}
	return testutil.StartServer(t, testutil.Options{
		Quizzes:       map[string]any{"manual.yaml": quiz},
		Config:        opts.config,
		ServerOptions: opts.serverOptions,
	})
}

func manualSubmit(t *testing.T, ts *testutil.TestServer, userID string, questionID int, answer any) *testutil.Response {
	t.Helper()
	return ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": questionID, "selected_answer": answer})
}

func manualFinalResults(t *testing.T, ts *testutil.TestServer, userID string) map[string]any {
	t.Helper()
	data := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)
	if data["test_completed"] != true {
		t.Fatalf("test not completed: %v", data)
	}
	return data["final_results"].(map[string]any)
}

func manualRows(results map[string]any) []map[string]any {
	var rows []map[string]any
	for _, row := range results["test_results"].([]any) {
		rows = append(rows, row.(map[string]any))
	}
	return rows
}

// reviewSocket is a review page's WebSocket. A goroutine reads it all the
// time (a coder/websocket client answers the server's pings only while it
// reads, and a Read with a deadline closes the connection), so a test can
// wait for a message with a timeout.
type reviewSocket struct {
	t        *testing.T
	conn     *websocket.Conn
	messages chan map[string]any
	failed   chan error
}

func wsURL(ts *testutil.TestServer, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

// dialReview opens /ws/review without starting the reader (see startReading).
func dialReview(t *testing.T, ts *testutil.TestServer, header http.Header) (*reviewSocket, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, wsURL(ts, "/ws/review"), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, res, err
	}
	t.Cleanup(func() { conn.CloseNow() })
	return &reviewSocket{t: t, conn: conn, messages: make(chan map[string]any, 100), failed: make(chan error, 1)}, res, nil
}

func (ws *reviewSocket) startReading() *reviewSocket {
	go func() {
		for {
			_, data, err := ws.conn.Read(context.Background())
			if err != nil {
				ws.failed <- err
				return
			}
			var message map[string]any
			if err := json.Unmarshal(data, &message); err != nil {
				message = map[string]any{"type": "not JSON", "data": string(data)}
			}
			ws.messages <- message
		}
	}()
	return ws
}

// readNow reads one message on the test's goroutine, for a socket without
// the reader (a page that stops answering pings once the test stops reading).
func (ws *reviewSocket) readNow() map[string]any {
	ws.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := ws.conn.Read(ctx)
	if err != nil {
		ws.t.Fatalf("read: %v", err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		ws.t.Fatalf("not JSON: %s", data)
	}
	return message
}

func sessionHeader(t *testing.T, ts *testutil.TestServer) http.Header {
	t.Helper()
	header := http.Header{}
	for _, cookie := range ts.AdminCookies(t, "test123") {
		header.Add("Cookie", cookie.Name+"="+cookie.Value)
	}
	return header
}

// connectReview opens a review page's socket with the admin session.
func connectReview(t *testing.T, ts *testutil.TestServer) *reviewSocket {
	t.Helper()
	ws, _, err := dialReview(t, ts, sessionHeader(t, ts))
	if err != nil {
		t.Fatalf("dial /ws/review: %v", err)
	}
	return ws.startReading()
}

// read returns the next message, or nil when none comes in time.
func (ws *reviewSocket) read(timeout time.Duration) map[string]any {
	ws.t.Helper()
	select {
	case message := <-ws.messages:
		return message
	case err := <-ws.failed:
		ws.t.Fatalf("read: %v", err)
	case <-time.After(timeout):
	}
	return nil
}

// state reads messages until a review_state that matches ok arrives.
func (ws *reviewSocket) state(ok func(map[string]any) bool) map[string]any {
	ws.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			ws.t.Fatal("no matching review_state")
		}
		message := ws.read(left)
		if message == nil {
			ws.t.Fatal("no matching review_state")
		}
		if message["type"] == "review_state" && ok(message) {
			return message
		}
	}
}

// latestItem reads until nothing more comes for half a second and returns
// the item of the last review_state.
func (ws *reviewSocket) latestItem() map[string]any {
	ws.t.Helper()
	var item map[string]any
	for {
		message := ws.read(500 * time.Millisecond)
		if message == nil {
			return item
		}
		if message["type"] == "review_state" {
			item, _ = message["item"].(map[string]any)
		}
	}
}

func (ws *reviewSocket) send(message map[string]any) {
	ws.t.Helper()
	data, _ := json.Marshal(message)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ws.conn.Write(ctx, websocket.MessageText, data); err != nil {
		ws.t.Fatalf("write: %v", err)
	}
}

func (ws *reviewSocket) verdict(reviewID any, isCorrect any) {
	ws.send(map[string]any{"type": "verdict", "review_id": reviewID, "is_correct": isCorrect})
}

func anyState(map[string]any) bool { return true }

func hasItem(state map[string]any) bool { return state["item"] != nil }

func noItem(state map[string]any) bool { return state["item"] == nil }

func item(state map[string]any) map[string]any { return state["item"].(map[string]any) }

// ---------------------------------------------------------------------------
// Quiz validation
// ---------------------------------------------------------------------------

func validateManual(t *testing.T, ts *testutil.TestServer, question map[string]any) map[string]any {
	t.Helper()
	content, err := testutil.YAML(map[string]any{"title": "T", "questions": []any{question}})
	if err != nil {
		t.Fatal(err)
	}
	admin := ts.AdminClient(t, "test123")
	res := admin.PostJSON(t, "/api/admin/validate-quiz", map[string]any{"content": string(content)})
	if res.Status != http.StatusOK {
		t.Fatalf("validate-quiz: %d %s", res.Status, res.Body)
	}
	return res.JSON(t)
}

func errorsContain(result map[string]any, parts ...string) bool {
	list, _ := result["errors"].([]any)
	for _, e := range list {
		text, _ := e.(string)
		all := true
		for _, part := range parts {
			all = all && strings.Contains(text, part)
		}
		if all {
			return true
		}
	}
	return false
}

func TestManualQuestionWithoutCheckerIsValid(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	if result := validateManual(t, ts, map[string]any{"question": "Q", "manual_check": true}); result["valid"] != true {
		t.Errorf("result = %v, want valid", result)
	}
}

func TestManualCheckMustBeBoolean(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	result := validateManual(t, ts, map[string]any{"question": "Q", "manual_check": "yes", "checker": ""})
	if result["valid"] != false || !errorsContain(result, "manual_check") {
		t.Errorf("result = %v, want an error about manual_check", result)
	}
}

func TestManualCheckWithCheckerCodeIsRefused(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	result := validateManual(t, ts, map[string]any{"question": "Q", "manual_check": true, "checker": "assert user_answer"})
	if result["valid"] != false || !errorsContain(result, "manual_check", "checker") {
		t.Errorf("result = %v, want an error about manual_check and checker", result)
	}
}

func TestManualCheckWithOptionsIsRefused(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	result := validateManual(t, ts, map[string]any{"question": "Q", "manual_check": true, "options": []any{"a", "b"}, "correct_answer": 0})
	if result["valid"] != false || !errorsContain(result, "manual_check") {
		t.Errorf("result = %v, want an error about manual_check", result)
	}
}

func TestManualCheckFalseKeepsChoiceQuestion(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	result := validateManual(t, ts, map[string]any{"question": "Q", "manual_check": false, "options": []any{"a", "b"}, "correct_answer": 0})
	if result["valid"] != true {
		t.Errorf("result = %v, want valid", result)
	}
}

func TestManualQuestionIsATextQuestionOnThePage(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	page := ts.GetJSON(t, "/").Text()
	if !strings.Contains(page, `"type":"text"`) {
		t.Error("the manual question is not a text question on the quiz page")
	}
	// The sample answer for the teacher never reaches the student page
	if strings.Contains(page, "Rayleigh scattering") {
		t.Error("the sample answer is on the student page")
	}
}

// ---------------------------------------------------------------------------
// Student answers
// ---------------------------------------------------------------------------

func TestManualSubmitReplySaysTheAnswerWaitsForTheTeacher(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	userID := ts.RegisterStudent(t, "Ann")
	res := manualSubmit(t, ts, userID, 1, "Light scatters")
	if res.Status != http.StatusOK {
		t.Fatalf("submit: %d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	if data["pending_review"] != true || data["is_text_question"] != true {
		t.Errorf("reply = %v, want pending_review and is_text_question", data)
	}
	for _, key := range []string{"is_correct", "correct_value", "checker_error"} {
		if _, ok := data[key]; ok {
			t.Errorf("reply has %s: %v", key, data)
		}
	}
}

func TestManualSubmitReplyWithoutShowRightAnswer(t *testing.T) {
	t.Parallel()
	quiz := manualQuiz()
	quiz["show_right_answer"] = false
	ts := startManual(t, manualOptions{quiz: quiz})
	data := manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters").JSON(t)
	if data["pending_review"] != true {
		t.Errorf("reply = %v, want pending_review", data)
	}
	for _, key := range []string{"is_correct", "is_text_question"} {
		if _, ok := data[key]; ok {
			t.Errorf("reply has %s: %v", key, data)
		}
	}
}

func TestManualRepeatedSubmitReturnsTheSameReply(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	userID := ts.RegisterStudent(t, "Ann")
	first := manualSubmit(t, ts, userID, 1, "Light scatters").JSON(t)
	second := manualSubmit(t, ts, userID, 1, "Light scatters").JSON(t)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("first %v, second %v, want the same reply", first, second)
	}
	if res := manualSubmit(t, ts, userID, 1, "Other"); res.Status != http.StatusConflict {
		t.Errorf("another answer: %d, want 409", res.Status)
	}
}

func TestManualEmptyAnswerIsRefused(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	if res := manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, ""); res.Status != http.StatusBadRequest {
		t.Errorf("empty answer: %d, want 400", res.Status)
	}
}

// The explanation step works as for other questions.
func TestManualSubmitReplyHasTheExplanation(t *testing.T) {
	t.Parallel()
	quiz := manualQuiz()
	quiz["show_explanation"] = true
	quiz["questions"].([]any)[0].(map[string]any)["explanation"] = "**Scattering**"
	ts := startManual(t, manualOptions{quiz: quiz})
	data := manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters").JSON(t)
	if html, _ := data["explanation_html"].(string); !strings.Contains(html, "<strong>Scattering</strong>") {
		t.Errorf("reply = %v, want the explanation", data)
	}
}

func answerAll(t *testing.T, ts *testutil.TestServer, userID string) {
	t.Helper()
	manualSubmit(t, ts, userID, 1, "Light scatters")
	manualSubmit(t, ts, userID, 2, 1)
	manualSubmit(t, ts, userID, 3, "7")
}

func TestManualFinalResultsShowWaitingAnswers(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	userID := ts.RegisterStudent(t, "Ann")
	answerAll(t, ts, userID)
	results := manualFinalResults(t, ts, userID)
	for key, want := range map[string]json.Number{"pending_count": "2", "correct_count": "1", "earned_points": "1", "total_points": "4"} {
		if results[key] != want {
			t.Errorf("%s = %v, want %v", key, results[key], want)
		}
	}
	rows := manualRows(results)
	if rows[0]["pending_review"] != true || rows[0]["is_correct"] != nil || rows[0]["earned_points"] != json.Number("0") {
		t.Errorf("row 1 = %v, want waiting with is_correct null and 0 points", rows[0])
	}
	if value, ok := rows[0]["is_correct"]; !ok || value != nil {
		t.Errorf("row 1 must have is_correct: null, got %v", rows[0])
	}
	if _, ok := rows[1]["pending_review"]; ok {
		t.Errorf("row 2 = %v, want no pending_review", rows[1])
	}
	if rows[2]["pending_review"] != true {
		t.Errorf("row 3 = %v, want pending_review", rows[2])
	}
}

func TestManualFinalResultsKeepPendingMarkWhenAnswersAreHidden(t *testing.T) {
	t.Parallel()
	quiz := manualQuiz()
	quiz["show_right_answer"] = false
	ts := startManual(t, manualOptions{quiz: quiz})
	userID := ts.RegisterStudent(t, "Ann")
	answerAll(t, ts, userID)
	results := manualFinalResults(t, ts, userID)
	if results["pending_count"] != json.Number("2") {
		t.Errorf("pending_count = %v, want 2", results["pending_count"])
	}
	row := manualRows(results)[0]
	if _, ok := row["is_correct"]; ok {
		t.Errorf("row = %v, want no is_correct", row)
	}
	if row["pending_review"] != true {
		t.Errorf("row = %v, want pending_review", row)
	}
}

// A quiz without manual questions still says pending_count 0.
func TestFinalResultsHavePendingCountZero(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	userID := ts.RegisterStudent(t, "Ann")
	ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 1})
	if results := manualFinalResults(t, ts, userID); results["pending_count"] != json.Number("0") {
		t.Errorf("pending_count = %v, want 0", results["pending_count"])
	}
}

// ---------------------------------------------------------------------------
// Review page and WebSocket
// ---------------------------------------------------------------------------

func TestReviewPageIsServed(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{config: map[string]any{"language": "en"}})
	res := ts.GetJSON(t, "/review/")
	if res.Status != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /review/: %d %s", res.Status, res.Header.Get("Content-Type"))
	}
	page := res.Text()
	if !strings.Contains(page, "<title>✍️ Answer review</title>") {
		t.Error("the page has no title in the config language")
	}
	boot := bootData(t, page)
	if boot["is_trusted_ip"] != false || boot["language"] != "en" {
		t.Errorf("boot = %v", boot)
	}
	texts, _ := boot["translations"].(map[string]any)
	if texts["review_correct"] != "✓ Right" || texts["connection_lost"] == nil {
		t.Errorf("boot translations = %v", texts)
	}
}

func bootData(t *testing.T, page string) map[string]any {
	t.Helper()
	const open = `<script id="webquiz-boot" type="application/json">`
	start := strings.Index(page, open)
	if start < 0 {
		t.Fatal("no boot data")
	}
	rest := page[start+len(open):]
	var boot map[string]any
	if err := json.Unmarshal([]byte(rest[:strings.Index(rest, "</script>")]), &boot); err != nil {
		t.Fatal(err)
	}
	return boot
}

func TestReviewPageTrustedIP(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}}}})
	if boot := bootData(t, ts.GetJSON(t, "/review/").Text()); boot["is_trusted_ip"] != true {
		t.Errorf("boot = %v, want is_trusted_ip", boot)
	}
}

func TestReviewPageIsForTheLocalNetworkOnly(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	if res := ts.GetJSON(t, "/review/", testutil.FromIP("8.8.8.8")); res.Status != http.StatusForbidden {
		t.Errorf("GET /review/ from a public address: %d, want 403", res.Status)
	}
	if res := ts.NoRedirects().GetJSON(t, "/review"); res.Status < 300 || res.Status > 399 || res.Header.Get("Location") != "/review/" {
		t.Errorf("GET /review: %d %q, want a redirect to /review/", res.Status, res.Header.Get("Location"))
	}
}

func TestReviewWebSocketNeedsAdminSession(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	_, res, err := dialReview(t, ts, nil)
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Errorf("dial without a session: %v %v, want 401", res, err)
	}
	_, res, err = dialReview(t, ts, http.Header{"X-Forwarded-For": {"8.8.8.8"}})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("dial from a public address: %v %v, want 403", res, err)
	}
}

func TestReviewWebSocketTrustedIPNeedsNoSession(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}}}})
	ws, _, err := dialReview(t, ts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state := ws.startReading().state(anyState); state["item"] != nil {
		t.Errorf("state = %v, want no item", state)
	}
}

func TestReviewerGetsAnswerWithoutStudentName(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	want := map[string]any{"type": "review_state", "item": nil, "pending_count": 0.0, "has_manual": true, "already_decided": false}
	if state := ws.state(anyState); !reflect.DeepEqual(state, want) {
		t.Errorf("first state = %v, want %v", state, want)
	}

	userID := ts.RegisterStudent(t, "Secret Name")
	manualSubmit(t, ts, userID, 1, "Light scatters")
	state := ws.state(hasItem)
	got := item(state)
	wantItem := map[string]any{
		"review_id": got["review_id"], "question": "Why is the sky blue?", "image": nil,
		"answer": "Light scatters", "correct_value": "Rayleigh scattering", "points": 2.0,
	}
	if !reflect.DeepEqual(got, wantItem) {
		t.Errorf("item = %v, want %v", got, wantItem)
	}
	if _, isText := got["review_id"].(string); !isText {
		t.Errorf("review_id = %#v, want text", got["review_id"])
	}
	if state["pending_count"] != 1.0 {
		t.Errorf("pending_count = %v, want 1", state["pending_count"])
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "Secret Name") || strings.Contains(string(raw), userID) {
		t.Errorf("the state names the student: %s", raw)
	}
}

func TestReviewVerdictUpdatesStudentResults(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	answerAll(t, ts, userID)

	state := ws.state(hasItem)
	if item(state)["answer"] != "Light scatters" {
		t.Fatalf("first item = %v", item(state))
	}
	ws.verdict(item(state)["review_id"], true)

	state = ws.state(func(s map[string]any) bool { return hasItem(s) && item(s)["answer"] == "7" })
	if state["pending_count"] != 1.0 {
		t.Errorf("pending_count = %v, want 1", state["pending_count"])
	}
	results := manualFinalResults(t, ts, userID)
	rows := manualRows(results)
	if results["pending_count"] != json.Number("1") || rows[0]["is_correct"] != true || results["earned_points"] != json.Number("3") || results["correct_count"] != json.Number("2") {
		t.Errorf("results = %v", results)
	}
	if _, ok := rows[0]["pending_review"]; ok {
		t.Errorf("row 1 = %v, want no pending_review", rows[0])
	}
	if rows[0]["earned_points"] != json.Number("2") {
		t.Errorf("row 1 earned_points = %v, want 2", rows[0]["earned_points"])
	}

	ws.verdict(item(state)["review_id"], false)
	state = ws.state(noItem)
	if state["pending_count"] != 0.0 {
		t.Errorf("pending_count = %v, want 0", state["pending_count"])
	}
	results = manualFinalResults(t, ts, userID)
	if results["pending_count"] != json.Number("0") || manualRows(results)[2]["is_correct"] != false || results["earned_points"] != json.Number("3") {
		t.Errorf("results = %v", results)
	}
}

// A verdict keeps the time the student finished.
func TestReviewVerdictKeepsCompletedAt(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	answerAll(t, ts, userID)
	live := openSocket(t, ts, "/ws/live-stats")
	before := live.next()["completion_times"].(map[string]any)[userID]
	if before == nil {
		t.Fatal("no completion time")
	}
	ws.verdict(item(ws.state(hasItem))["review_id"], true)
	ws.state(func(s map[string]any) bool { return s["pending_count"] == 1.0 })
	after := openSocket(t, ts, "/ws/live-stats").next()["completion_times"].(map[string]any)[userID]
	if after != before {
		t.Errorf("completed_at %v after the verdict, want %v", after, before)
	}
}

func TestReviewVerdictThatIsNotBooleanIsIgnored(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters")
	state := ws.state(hasItem)
	ws.verdict(item(state)["review_id"], "yes")
	again := ws.state(anyState)
	if !reflect.DeepEqual(again["item"], state["item"]) || again["pending_count"] != 1.0 {
		t.Errorf("after a bad verdict: %v, want %v", again, state)
	}
}

// The review id may come as a number: Python's str() made "7" of 7.
func TestReviewVerdictWithANumberAsReviewID(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters")
	state := ws.state(hasItem)
	if item(state)["review_id"] != "1" {
		t.Fatalf("review_id = %v, want 1", item(state)["review_id"])
	}
	ws.verdict(1, true)
	if state := ws.state(anyState); state["pending_count"] != 0.0 || state["already_decided"] != false {
		t.Errorf("after the verdict: %v", state)
	}
}

func TestReviewPingGetsPong(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	ws.send(map[string]any{"type": "ping"})
	if message := ws.read(3 * time.Second); message["type"] != "pong" {
		t.Errorf("reply to ping = %v, want pong", message)
	}
}

func TestReviewTwoReviewersGetDifferentAnswers(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	first := connectReview(t, ts)
	second := connectReview(t, ts)
	first.state(anyState)
	second.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "A"), 1, "answer A")
	manualSubmit(t, ts, ts.RegisterStudent(t, "B"), 1, "answer B")
	time.Sleep(500 * time.Millisecond)
	firstItem, secondItem := first.latestItem(), second.latestItem()
	if firstItem == nil || secondItem == nil {
		t.Fatalf("items %v %v, want one each", firstItem, secondItem)
	}
	got := []string{firstItem["answer"].(string), secondItem["answer"].(string)}
	slices.Sort(got)
	if !reflect.DeepEqual(got, []string{"answer A", "answer B"}) {
		t.Errorf("answers %v, want answer A and answer B", got)
	}
}

func TestReviewFirstVerdictWinsWhenTwoReviewersSeeTheSameAnswer(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	first := connectReview(t, ts)
	first.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	manualSubmit(t, ts, userID, 1, "Light scatters")
	firstState := first.state(hasItem)

	second := connectReview(t, ts)
	secondState := second.state(anyState)
	// The only waiting answer is open on the first page, so the second page waits
	if secondState["item"] != nil || secondState["pending_count"] != 1.0 {
		t.Fatalf("second page = %v, want no item and 1 waiting", secondState)
	}
	// The teacher asks to help and gets the same answer
	second.send(map[string]any{"type": "take_shared"})
	secondState = second.state(hasItem)
	if item(secondState)["review_id"] != item(firstState)["review_id"] {
		t.Fatalf("shared item %v, want %v", item(secondState), item(firstState))
	}

	first.verdict(item(firstState)["review_id"], false)
	if state := first.state(func(s map[string]any) bool { return s["pending_count"] == 0.0 }); state["item"] != nil {
		t.Errorf("first page = %v, want no item", state)
	}
	// The other page moves on by itself
	if state := second.state(noItem); state["pending_count"] != 0.0 {
		t.Errorf("second page = %v, want 0 waiting", state)
	}

	second.verdict(item(secondState)["review_id"], true)
	late := second.state(anyState)
	if late["already_decided"] != true || late["item"] != nil {
		t.Errorf("late verdict = %v, want already_decided and no item", late)
	}
	// already_decided comes once
	second.send(map[string]any{"type": "take_shared"})
	if again := second.state(anyState); again["already_decided"] != false {
		t.Errorf("next state = %v, want already_decided false", again)
	}

	manualSubmit(t, ts, userID, 2, 1)
	manualSubmit(t, ts, userID, 3, "7")
	if row := manualRows(manualFinalResults(t, ts, userID))[0]; row["is_correct"] != false {
		t.Errorf("row 1 = %v, want the first verdict (wrong)", row)
	}
}

func TestReviewAnswerOfClosedPageGoesToWaitingPage(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	first := connectReview(t, ts)
	first.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "A"), 1, "answer A")
	manualSubmit(t, ts, ts.RegisterStudent(t, "B"), 1, "answer B")
	if state := first.state(hasItem); item(state)["answer"] != "answer A" {
		t.Fatalf("first page = %v, want answer A", state)
	}

	second := connectReview(t, ts)
	secondState := second.state(anyState)
	if secondState["item"] == nil || item(secondState)["answer"] != "answer B" {
		t.Fatalf("second page = %v, want answer B", secondState)
	}
	second.verdict(item(secondState)["review_id"], true)
	// Answer A is open on the first page, so the second page waits ...
	if waiting := second.state(func(s map[string]any) bool { return s["pending_count"] == 1.0 }); waiting["item"] != nil {
		t.Errorf("second page = %v, want no item", waiting)
	}
	// ... until the teacher asks to help
	second.send(map[string]any{"type": "take_shared"})
	if shared := second.state(hasItem); item(shared)["answer"] != "answer A" {
		t.Errorf("shared = %v, want answer A", shared)
	}
	first.conn.Close(websocket.StatusNormalClosure, "")
	second.conn.Close(websocket.StatusNormalClosure, "")

	third := connectReview(t, ts)
	waitForState := third.state(hasItem)
	if item(waitForState)["answer"] != "answer A" {
		t.Errorf("third page = %v, want answer A", waitForState)
	}
}

// A page that does not answer the heartbeat ping (a phone that went to
// sleep) is dropped, and its answer goes to a waiting page.
func TestReviewHeartbeatDropsASleepingPage(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{serverOptions: server.Options{ReviewHeartbeat: 200 * time.Millisecond}})
	sleeping, _, err := dialReview(t, ts, sessionHeader(t, ts))
	if err != nil {
		t.Fatal(err)
	}
	sleeping.readNow() // the first state
	manualSubmit(t, ts, ts.RegisterStudent(t, "A"), 1, "answer A")
	if state := sleeping.readNow(); state["item"] == nil {
		t.Fatalf("sleeping page = %v, want the answer", state)
	}
	// From now on nothing reads this socket, so it never answers a ping.

	awake := connectReview(t, ts)
	if state := awake.state(anyState); state["item"] != nil {
		t.Fatalf("second page = %v, want no item while the first one shows it", state)
	}
	// The second page answers pings (its reader runs) and gets the answer
	// once the first page is dropped.
	if state := awake.state(hasItem); item(state)["answer"] != "answer A" {
		t.Errorf("item = %v, want answer A", item(state))
	}
}

func TestReviewQuizRestartClearsTheQueue(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	ws := connectReview(t, ts)
	ws.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters")
	ws.state(hasItem)

	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "manual.yaml"}); res.Status != http.StatusOK {
		t.Fatalf("switch-quiz: %d %s", res.Status, res.Body)
	}
	state := ws.state(noItem)
	if state["pending_count"] != 0.0 || state["has_manual"] != true {
		t.Errorf("after the restart: %v", state)
	}
}

func TestReviewHasManualIsFalseForQuizWithoutManualQuestions(t *testing.T) {
	t.Parallel()
	quiz := map[string]any{"title": "Plain", "questions": []any{map[string]any{"question": "Q", "options": []any{"a", "b"}, "correct_answer": 0}}}
	ts := startManual(t, manualOptions{quiz: quiz})
	ws := connectReview(t, ts)
	if state := ws.state(anyState); state["has_manual"] != false {
		t.Errorf("state = %v, want has_manual false", state)
	}
}

func TestAdminWebSocketReportsReviewStatus(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	adminWS := openSocket(t, ts, "/ws/admin")
	initial := adminWS.next()
	if want := map[string]any{"has_manual": true, "pending_count": 0.0, "reviewers": 0.0}; !reflect.DeepEqual(initial["review"], want) {
		t.Errorf("initial review = %v, want %v", initial["review"], want)
	}

	ws := connectReview(t, ts)
	ws.state(anyState)
	manualSubmit(t, ts, ts.RegisterStudent(t, "Ann"), 1, "Light scatters")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		message := adminWS.next()
		if message["type"] == "review_status" && message["pending_count"] == 1.0 {
			want := map[string]any{"type": "review_status", "has_manual": true, "pending_count": 1.0, "reviewers": 1.0}
			if !reflect.DeepEqual(message, want) {
				t.Errorf("status = %v, want %v", message, want)
			}
			return
		}
	}
	t.Fatal("no review_status with 1 waiting answer")
}

func TestLiveStatsShowPendingThenVerdict(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next()
	ws := connectReview(t, ts)
	ws.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	manualSubmit(t, ts, userID, 1, "Light scatters")

	nextStateUpdate := func() map[string]any {
		for {
			message := live.next()
			if message["type"] == "state_update" && message["state"] != "think" {
				return message
			}
		}
	}
	pending := nextStateUpdate()
	if pending["question_id"] != 1.0 || pending["state"] != "pending" || pending["earned_points"] != 0.0 {
		t.Errorf("pending update = %v", pending)
	}

	ws.verdict(item(ws.state(hasItem))["review_id"], true)
	decided := nextStateUpdate()
	if decided["question_id"] != 1.0 || decided["user_id"] != userID || decided["state"] != "ok" || decided["earned_points"] != 2.0 {
		t.Errorf("decided update = %v", decided)
	}
	for _, key := range []string{"username", "time_taken", "total_questions", "total_points", "question_points", "completed", "completed_at"} {
		if _, ok := decided[key]; !ok {
			t.Errorf("decided update has no %s: %v", key, decided)
		}
	}
	// A page that connects now sees the verdict in its initial state
	cells := openSocket(t, ts, "/ws/live-stats").next()["live_stats"].(map[string]any)[userID].(map[string]any)
	if cells["1"].(map[string]any)["state"] != "ok" {
		t.Errorf("live stats cells = %v, want question 1 ok", cells)
	}
}

// ---------------------------------------------------------------------------
// CSV files
// ---------------------------------------------------------------------------

func manualCSVColumn(t *testing.T, ts *testutil.TestServer, name, column string) []string {
	t.Helper()
	path := filepath.Join(ts.CSVDir, name)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	_, rows := partAReadCSV(t, path)
	values := []string{}
	for _, row := range rows {
		values = append(values, row[column])
	}
	return values
}

func waitForColumn(t *testing.T, ts *testutil.TestServer, name, column string, want []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := manualCSVColumn(t, ts, name, column)
		if reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s column %s = %q, want %q", name, column, got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManualCSVRowIsRewrittenAfterVerdict(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{
		config:        map[string]any{"extra_answers_with_users_csv": true},
		serverOptions: server.Options{FlushInterval: 20 * time.Millisecond},
	})
	ws := connectReview(t, ts)
	ws.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	manualSubmit(t, ts, userID, 1, "Light scatters")
	manualSubmit(t, ts, userID, 2, 1)

	waitForColumn(t, ts, "manual_0001.csv", "is_correct", []string{"", "True"})
	waitForColumn(t, ts, "manual_0001.answers_with_users.csv", "is_correct", []string{"", "True"})

	ws.verdict(item(ws.state(hasItem))["review_id"], true)
	ws.state(noItem)
	manualSubmit(t, ts, userID, 3, "7")

	waitForColumn(t, ts, "manual_0001.csv", "is_correct", []string{"True", "True", ""})
	waitForColumn(t, ts, "manual_0001.csv", "selected_answer", []string{"Light scatters", "4", "7"})
	waitForColumn(t, ts, "manual_0001.answers_with_users.csv", "is_correct", []string{"True", "True", ""})
	waitForColumn(t, ts, "manual_0001.answers_with_users.csv", "username", []string{"Ann", "Ann", "Ann"})
	waitForColumn(t, ts, "manual_0001.users.csv", "correct_answers", []string{"2"})
	waitForColumn(t, ts, "manual_0001.users.csv", "earned_points", []string{"3"})

	// The header is written once, and no temp file stays behind
	data, err := os.ReadFile(filepath.Join(ts.CSVDir, "manual_0001.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "user_id,question") != 1 {
		t.Errorf("the answers CSV has the header more than once:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(ts.CSVDir, "manual_0001.csv.tmp")); err == nil {
		t.Error("the temp file is still there")
	}
}

// A verdict that comes before the first flush: the answer is written once,
// already decided.
func TestManualVerdictBeforeTheFirstFlush(t *testing.T) {
	t.Parallel()
	ts := startManual(t, manualOptions{serverOptions: server.Options{FlushInterval: time.Hour}})
	ws := connectReview(t, ts)
	ws.state(anyState)
	userID := ts.RegisterStudent(t, "Ann")
	manualSubmit(t, ts, userID, 1, "Light scatters")
	ws.verdict(item(ws.state(hasItem))["review_id"], false)
	ws.state(noItem)
	// A quiz switch writes the waiting rows of the old run
	admin := ts.AdminClient(t, "test123")
	if res := admin.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "manual.yaml"}); res.Status != http.StatusOK {
		t.Fatalf("switch-quiz: %d %s", res.Status, res.Body)
	}
	if got := manualCSVColumn(t, ts, "manual_0001.csv", "is_correct"); !reflect.DeepEqual(got, []string{"False"}) {
		t.Errorf("is_correct = %q, want [False]", got)
	}
}
