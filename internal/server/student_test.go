package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// socket is a WebSocket client of a test server.
type socket struct {
	t    *testing.T
	conn *websocket.Conn
}

func openSocket(t *testing.T, ts *testutil.TestServer, path string) *socket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return &socket{t: t, conn: conn}
}

// next reads the next message.
func (s *socket) next() map[string]any {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		s.t.Fatalf("read: %v", err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		s.t.Fatalf("not JSON: %s", data)
	}
	return message
}

// Bad requests get Python's answers: the exception text with 500 where the
// Python handler failed, its own 400/404 replies otherwise.
func TestRequestErrorsLikePython(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"registration": map[string]any{"fields": []any{"Grade"}}}})
	userID := ts.PostJSON(t, "/api/register", map[string]any{"username": "Ann", "grade": "5"}).JSON(t)["user_id"].(string)
	cases := []struct {
		method, path string
		body         any
		status       int
		error        string
	}{
		{"POST", "/api/register", []any{1}, 500, "list indices must be integers or slices, not str"},
		{"POST", "/api/register", "7", 500, "'int' object is not subscriptable"},
		{"POST", "/api/register", map[string]any{}, 500, "'username'"},
		{"POST", "/api/register", map[string]any{"username": 5}, 500, "'int' object has no attribute 'strip'"},
		{"POST", "/api/register", map[string]any{"username": "Bob", "grade": nil}, 500, "'NoneType' object has no attribute 'strip'"},
		{"POST", "/api/register", map[string]any{"username": "  "}, 400, "Ім'я користувача не може бути порожнім"},
		{"POST", "/api/register", map[string]any{"username": "Bob", "grade": " "}, 400, `Поле "Grade" не може бути порожнім`},
		{"POST", "/api/register", map[string]any{"username": " Ann ", "grade": "6"}, 400, "Ім'я користувача вже існує"},
		{"PUT", "/api/update-registration", []any{1}, 500, "'list' object has no attribute 'get'"},
		{"PUT", "/api/update-registration", map[string]any{"user_id": ""}, 400, "User ID is required"},
		{"PUT", "/api/update-registration", map[string]any{"user_id": []any{1}}, 500, "unhashable type: 'list'"},
		{"PUT", "/api/update-registration", map[string]any{"user_id": 123456}, 404, "User not found"},
		{"POST", "/api/question-start", map[string]any{"user_id": userID}, 500, "'question_id'"},
		{"POST", "/api/question-start", map[string]any{"user_id": "000000", "question_id": 1}, 404, "Користувача не знайдено"},
		{"POST", "/api/question-start", map[string]any{"user_id": userID, "question_id": 9}, 404, "Питання не знайдено"},
		{"POST", "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1}, 500, "'selected_answer'"},
		{"POST", "/api/submit-answer", map[string]any{"user_id": map[string]any{}, "question_id": 1, "selected_answer": 1}, 500, "unhashable type: 'dict'"},
		{"POST", "/api/submit-answer", map[string]any{"user_id": userID, "question_id": []any{1}, "selected_answer": 1}, 500, "unhashable type: 'list'"},
		{"POST", "/api/submit-answer", map[string]any{"user_id": userID, "question_id": "1", "selected_answer": 1}, 404, "Питання не знайдено"},
		{"POST", "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": ""}, 400, "Порожня відповідь не приймається"},
	}
	for _, c := range cases {
		res := ts.Do(t, c.method, c.path, c.body)
		if res.Status != c.status || res.JSON(t)["error"] != c.error {
			t.Errorf("%s %s %v: %d %s, want %d %q", c.method, c.path, c.body, res.Status, res.Body, c.status, c.error)
		}
	}
	// A question id 1.0 or true is question 1, and the answer 1.0 equals 1, as
	// in Python. (json.Number sends 1.0 as written; a Go float would go out as 1.)
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": json.Number("1.0"), "selected_answer": 1})
	if res.Status != 200 || res.JSON(t)["is_correct"] != true {
		t.Errorf("question_id 1.0: %d %s", res.Status, res.Body)
	}
	first := res.Body
	res = ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": true, "selected_answer": json.Number("1.0")})
	if res.Status != 200 || string(res.Body) != string(first) {
		t.Errorf("a repeated answer 1.0 to question true gets the stored reply: %d %s", res.Status, res.Body)
	}
	if res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 0}); res.Status != 409 {
		t.Errorf("another answer to question 1: %d %s, want 409", res.Status, res.Body)
	}
}

// The admin page hears about students who wait for approval: the initial
// state lists them, then new_registration, registration_updated and
// user_approved arrive. The live stats page hears about the student only at
// the approval.
func TestAdminSocketRegistrationMessages(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"registration": map[string]any{"approve": true, "fields": []any{"Grade"}},
	}})
	early := ts.PostJSON(t, "/api/register", map[string]any{"username": "Early", "grade": "1"}).JSON(t)["user_id"].(string)

	admin := openSocket(t, ts, "/ws/admin")
	initial := admin.next()
	pending, _ := initial["pending_users"].(map[string]any)
	if initial["type"] != "initial_state" || initial["requires_approval"] != true || initial["tunnel"] != nil ||
		len(pending) != 1 || len(initial["transfer_requests"].([]any)) != 0 {
		t.Fatalf("initial state: %v", initial)
	}
	if user := pending[early].(map[string]any); user["username"] != "Early" || user["registration_fields"].(map[string]any)["grade"] != "1" {
		t.Errorf("pending user: %v", user)
	}
	live := openSocket(t, ts, "/ws/live-stats")
	if users := live.next()["users"].(map[string]any); len(users) != 0 {
		t.Errorf("a student who waits is not on the live stats page: %v", users)
	}

	userID := ts.PostJSON(t, "/api/register", map[string]any{"username": "Ann", "grade": "5"}).JSON(t)["user_id"].(string)
	message := admin.next()
	if message["type"] != "new_registration" || message["user_id"] != userID || message["username"] != "Ann" ||
		message["registration_fields"].(map[string]any)["grade"] != "5" {
		t.Errorf("new_registration: %v", message)
	}

	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": userID, "username": "Anna", "grade": "6"})
	if res.Status != 200 {
		t.Fatalf("update: %d %s", res.Status, res.Body)
	}
	data := res.JSON(t)["user_data"].(map[string]any)
	if data["user_id"] != userID || data["username"] != "Anna" || data["grade"] != "6" || data["approved"] != false || data["registered_at"] == "" {
		t.Errorf("user_data: %v", data)
	}
	message = admin.next()
	if message["type"] != "registration_updated" || message["username"] != "Anna" || message["registration_fields"].(map[string]any)["grade"] != "6" {
		t.Errorf("registration_updated: %v", message)
	}

	adminClient := ts.AdminClient(t, "test123")
	res = adminClient.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID})
	if reply := res.JSON(t); res.Status != 200 || reply["message"] != "User approved successfully" || reply["user_id"] != userID {
		t.Errorf("approve: %d %s", res.Status, res.Body)
	}
	if message := admin.next(); message["type"] != "user_approved" || message["user_id"] != userID {
		t.Errorf("user_approved: %v", message)
	}
	if message := live.next(); message["type"] != "user_registered" || message["user_id"] != userID || message["question_id"] != 1.0 ||
		message["total_points"] != 1.0 || message["time_taken"] != nil {
		t.Errorf("live stats at the approval: %v", message)
	}
	res = adminClient.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID})
	if reply := res.JSON(t); res.Status != 200 || reply["message"] != "User already approved" || reply["user_id"] != nil {
		t.Errorf("second approval: %d %s", res.Status, res.Body)
	}
	for body, want := range map[string]int{`{}`: 400, `{"user_id": "999999"}`: 404, `[1]`: 500} {
		if res := adminClient.Put(t, "/api/admin/approve-user", body); res.Status != want {
			t.Errorf("approve %s: %d %s, want %d", body, res.Status, res.Body, want)
		}
	}
	if res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": userID, "username": "Late"}); res.Status != 400 {
		t.Errorf("update after the approval: %d %s", res.Status, res.Body)
	}
}

// switch-quiz checks the start settings first (each problem, in the order of
// the request); bad settings change nothing. The reply names the new CSV
// file and the settings the run uses.
func TestSwitchQuizStartSettings(t *testing.T) {
	t.Parallel()
	quiz := func(title string) map[string]any {
		return map[string]any{"title": title, "show_right_answer": true, "time_limit": map[string]any{"per_question": 30},
			"questions": []any{map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0}}}
	}
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"a.yaml": quiz("A"), "b.yaml": quiz("B")}})
	admin := ts.AdminClient(t, "test123")
	switchTo := func(body string) *testutil.Response { return admin.PostJSON(t, "/api/admin/switch-quiz", body) }

	if res := switchTo(`{"quiz_filename": "a.yaml"}`); res.Status != 200 {
		t.Fatalf("switch to a.yaml: %d %s", res.Status, res.Body)
	}
	res := switchTo(`{"quiz_filename": "b.yaml", "settings": {"title": "x", "show_right_answer": "no", "time_limit": {"per_question": -1}, "show_final_list": 1}}`)
	want := []any{
		"'title' cannot be changed when starting a quiz",
		"'show_right_answer' must be a boolean (true or false)",
		"'time_limit.per_question' must be a whole number of seconds (0 or more)",
		"'show_final_list' must be a boolean (true or false)",
	}
	reply := res.JSON(t)
	if res.Status != 400 || len(reply["errors"].([]any)) != len(want) {
		t.Fatalf("bad settings: %d %s", res.Status, res.Body)
	}
	for i, problem := range reply["errors"].([]any) {
		if problem != want[i] {
			t.Errorf("problem %d: %q, want %q", i, problem, want[i])
		}
	}
	if reply["error"] != strings.Join([]string{want[0].(string), want[1].(string), want[2].(string), want[3].(string)}, "; ") {
		t.Errorf("error text: %v", reply["error"])
	}
	if res := switchTo(`{"quiz_filename": "b.yaml", "settings": []}`); res.Status != 400 || res.JSON(t)["error"] != "'settings' must be an object" {
		t.Errorf("settings not an object: %d %s", res.Status, res.Body)
	}
	if state := liveStatsState(t, ts); state["current_quiz"] != "a.yaml" {
		t.Errorf("bad settings switched the quiz: %v", state["current_quiz"])
	}

	res = switchTo(`{"quiz_filename": "b.yaml", "settings": {"show_right_answer": false, "time_limit": {"whole_quiz": 600}}}`)
	reply = res.JSON(t)
	settings, _ := reply["settings"].(map[string]any)
	if res.Status != 200 || reply["success"] != true || reply["message"] != "Switched to quiz: b.yaml" || reply["current_quiz"] != "b.yaml" ||
		reply["csv_file"] != "b_0001.csv" || settings["show_right_answer"] != false || settings["show_final_list"] != true ||
		settings["time_limit"].(map[string]any)["whole_quiz"] != json.Number("600") {
		t.Errorf("switch with settings: %d %s", res.Status, res.Body)
	}
	res = switchTo(`{"quiz_filename": "b.yaml", "settings": {"time_limit": null}}`)
	if settings := res.JSON(t)["settings"].(map[string]any); res.Status != 200 || settings["time_limit"] != nil || settings["show_right_answer"] != true {
		t.Errorf("time_limit null: %d %s", res.Status, res.Body)
	}

	for body, wantError := range map[string]string{
		`{}`:                             "'quiz_filename'",
		`{"quiz_filename": "nope.yaml"}`: "Quiz file not found: nope.yaml",
		`{"quiz_filename": 5}`:           "join() argument must be str, bytes, or os.PathLike object, not 'int'",
	} {
		if res := switchTo(body); res.Status != http.StatusInternalServerError || res.JSON(t)["error"] != wantError {
			t.Errorf("%s: %d %s, want 500 %q", body, res.Status, res.Body, wantError)
		}
	}
}

// A time limit timeout reaches the live stats page, the reply, the results
// screen and the CSV files.
func TestTimeoutEverywhere(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{"q.yaml": map[string]any{
		"title": "Timed", "time_limit": map[string]any{"per_question": 30},
		"questions": []any{
			map[string]any{"question": "Q1", "options": []any{"A", "B"}, "correct_answer": 0, "points": 2.5},
			map[string]any{"question": "Q2", "checker": "assert user_answer == 'x'", "correct_value": "x"},
		},
	}}})
	live := openSocket(t, ts, "/ws/live-stats")
	live.next()
	userID := ts.RegisterStudent(t, "Ann")
	if message := live.next(); message["type"] != "user_registered" || message["total_points"] != 3.5 {
		t.Fatalf("user_registered: %v", message)
	}
	start := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 1}).JSON(t)
	if remaining, _ := start["remaining_seconds"].(json.Number).Float64(); start["status"] != "success" || remaining <= 29 || remaining > 30 {
		t.Errorf("question-start: %v", start)
	}
	if message := live.next(); message["type"] != "state_update" || message["state"] != "think" || message["question_points"] != 2.5 {
		t.Errorf("think: %v", message)
	}
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": nil})
	reply := res.JSON(t)
	if res.Status != 200 || reply["timed_out"] != true || reply["is_correct"] != false || reply["correct_answer"] != json.Number("0") || reply["test_completed"] != false {
		t.Errorf("timeout reply: %d %s", res.Status, res.Body)
	}
	if message := live.next(); message["state"] != "timeout" || message["earned_points"] != 0.0 || message["completed"] != false {
		t.Errorf("timeout on the live stats page: %v", message)
	}
	ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 2})
	live.next()
	res = ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 2, "selected_answer": "x"})
	if reply := res.JSON(t); reply["is_correct"] != true || reply["is_text_question"] != true || reply["correct_value"] != "x" || reply["test_completed"] != true {
		t.Errorf("text answer: %s", res.Body)
	}
	if message := live.next(); message["state"] != "ok" || message["completed"] != true || message["completed_at"] == nil {
		t.Errorf("last answer on the live stats page: %v", message)
	}

	results := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)["final_results"].(map[string]any)
	rows := results["test_results"].([]any)
	first := rows[0].(map[string]any)
	if results["earned_points"] != json.Number("1") || results["total_points"] != json.Number("3.5") || results["points_percentage"] != json.Number("29") ||
		results["percentage"] != json.Number("50") || first["timed_out"] != true || first["selected_answer"] != "" || first["correct_answer"] != "A" ||
		first["points"] != json.Number("2.5") || first["earned_points"] != json.Number("0") || first["file"] != nil || first["image"] != nil {
		t.Errorf("final results: %v", results)
	}

	ts.Server.Close()
	answers := csvLines(t, ts.CSVDir+"/q_0001.csv")
	if len(answers) != 3 || !strings.HasPrefix(answers[1], userID+",Q1,,A,False,") || !strings.HasPrefix(answers[2], userID+",Q2,x,x,True,") {
		t.Errorf("answers CSV:\n%s", strings.Join(answers, "\n"))
	}
	users := csvLines(t, ts.CSVDir+"/q_0001.users.csv")
	if len(users) != 2 || !strings.HasSuffix(users[1], ",2,1,1,3.5,0:00") {
		t.Errorf("users CSV:\n%s", strings.Join(users, "\n"))
	}
}
