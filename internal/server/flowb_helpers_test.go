package server_test

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Helpers of the student-flow ports, part B. The Python tests of these files
// used requests against a server on a port; these helpers do the same with
// the in-process test server.

// quizServer starts a server with the one quiz file default.yaml (the quiz
// of the Python fixture) and optional extra options.
func quizServer(t *testing.T, quiz map[string]any, opts ...func(*testutil.Options)) *testutil.TestServer {
	t.Helper()
	o := testutil.Options{Quizzes: map[string]any{"default.yaml": quiz}}
	for _, apply := range opts {
		apply(&o)
	}
	return testutil.StartServer(t, o)
}

// answerOf sends an answer and returns the status and the JSON reply.
func answerOf(t *testing.T, ts *testutil.TestServer, userID string, questionID int, answer any) (int, map[string]any) {
	t.Helper()
	res := ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": questionID, "selected_answer": answer})
	return res.Status, res.JSON(t)
}

// answerOK sends an answer, requires status 200 and returns the reply.
func answerOK(t *testing.T, ts *testutil.TestServer, userID string, questionID int, answer any) map[string]any {
	t.Helper()
	status, data := answerOf(t, ts, userID, questionID, answer)
	if status != http.StatusOK {
		t.Fatalf("submit-answer q%d %v: status %d, reply %v", questionID, answer, status, data)
	}
	return data
}

// numIs checks that a JSON number (json.Number) has the value want, written
// as text (for example "9", "0.3").
func numIs(t *testing.T, what string, got any, want string) {
	t.Helper()
	number, ok := got.(json.Number)
	if !ok {
		t.Errorf("%s: got %v (%T), want the number %s", what, got, got, want)
		return
	}
	if number.String() != want {
		t.Errorf("%s: got %s, want %s", what, number, want)
	}
}

// csvFileEnding returns the content of the first file in the CSV folder whose name
// ends with suffix, after the server wrote its waiting rows (the server is
// closed first, which flushes: the Python tests slept for the 5 s flush).
func csvFileEnding(t *testing.T, ts *testutil.TestServer, suffix string) string {
	t.Helper()
	ts.Server.Close()
	entries, err := os.ReadDir(ts.CSVDir)
	if err != nil {
		t.Fatalf("read the CSV folder: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no file ending with %q in %s (found %v)", suffix, ts.CSVDir, entries)
	}
	data, err := os.ReadFile(filepath.Join(ts.CSVDir, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// csvRecords reads CSV text as a list of rows, each a map header -> value.
func csvRecords(t *testing.T, text string) []map[string]string {
	t.Helper()
	rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("CSV: %v\n%s", err, text)
	}
	if len(rows) == 0 {
		return nil
	}
	var out []map[string]string
	for _, row := range rows[1:] {
		record := map[string]string{}
		for i, name := range rows[0] {
			if i < len(row) {
				record[name] = row[i]
			}
		}
		out = append(out, record)
	}
	return out
}

// stepClock is a clock that tests move by hand, so a test need not sleep to
// pass a time limit. It goes to the server as ServerOptions.Now.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// shortGrace is the environment of the Python tests (SHORT_GRACE): answers
// count until half a second after the limit.
var shortGrace = map[string]string{"WEBQUIZ_TIME_LIMIT_GRACE_SECONDS": "0.5"}

// timedQuizFile is quiz() of the time limit tests: a quiz "Timed" in the
// file timed.yaml. A nil timeLimit leaves the quiz without a limit.
func timedQuizFile(timeLimit any, questions []any, settings map[string]any) map[string]any {
	if questions == nil {
		questions = []any{choiceQuestion("Q?", nil)}
	}
	data := map[string]any{"title": "Timed", "questions": questions}
	for key, value := range settings {
		data[key] = value
	}
	if timeLimit != nil {
		data["time_limit"] = timeLimit
	}
	return map[string]any{"timed.yaml": data}
}

// choiceQuestion is question() of the time limit tests: two options, the
// first one right.
func choiceQuestion(text string, extra map[string]any) map[string]any {
	q := map[string]any{"question": text, "options": []any{"A", "B"}, "correct_answer": 0}
	for key, value := range extra {
		q[key] = value
	}
	return q
}

// startTimed starts a server on the test clock with the given quiz files and
// environment (nil: the default 5 s grace).
func startTimed(t *testing.T, clock *stepClock, quizzes map[string]any, env map[string]string, configure ...func(*testutil.Options)) *testutil.TestServer {
	t.Helper()
	opts := testutil.Options{Quizzes: quizzes, Env: env}
	opts.ServerOptions.Now = clock.Now
	for _, apply := range configure {
		apply(&opts)
	}
	return testutil.StartServer(t, opts)
}

// startQuestion sends question-start and requires status 200.
func startQuestion(t *testing.T, ts *testutil.TestServer, userID string, questionID int) map[string]any {
	t.Helper()
	res := ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": questionID})
	if res.Status != http.StatusOK {
		t.Fatalf("question-start q%d: %d %s", questionID, res.Status, res.Body)
	}
	return res.JSON(t)
}

// floatOf reads a JSON number.
func floatOf(t *testing.T, v any) float64 {
	t.Helper()
	number, ok := v.(json.Number)
	if !ok {
		t.Fatalf("want a number, got %v (%T)", v, v)
	}
	f, err := number.Float64()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// csvNames lists the file names in a CSV folder.
func csvNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}
