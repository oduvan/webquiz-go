package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// choice makes a choice question as the Python tests wrote them.
func choice(text string, options []any, correct any) map[string]any {
	return map[string]any{"question": text, "options": options, "correct_answer": correct}
}

// one makes a quiz with a title and a single two-option question.
func oneQuestionQuiz(title, text string) map[string]any {
	return map[string]any{"title": title, "questions": []any{choice(text, []any{"A", "B"}, 0)}}
}

// withAdmin starts a server and logs in; the client keeps the session.
func withAdmin(t *testing.T, opts testutil.Options) (*testutil.TestServer, *testutil.Client) {
	t.Helper()
	ts := testutil.StartServer(t, opts)
	return ts, ts.AdminClient(t, "test123")
}

// readQuizFile parses a quiz file of the server (yaml.safe_load).
func readQuizFile(t *testing.T, ts *testutil.TestServer, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ts.QuizzesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pyval.LoadYAML(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return parsed.(map[string]any)
}

// errorString is the "error" field of a reply as text.
func errorString(t *testing.T, res *testutil.Response) string {
	t.Helper()
	text, _ := res.JSON(t)["error"].(string)
	return text
}

// isNum tells whether a decoded JSON value is the number written as want
// ("2", "1.5").
func isNum(v any, want string) bool {
	n, ok := v.(json.Number)
	return ok && n.String() == want
}
