package transfer

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// Tests of the quiz reference rewrite beyond the Python tests.

// The sender's quiz changed into broken YAML after it was offered: the
// receiver saves it as it came, and logs that the references could not be
// updated (Python's warning).
func TestBrokenReceivedQuizIsSavedWithAWarning(t *testing.T) {
	a, b, _ := twoMachines(t)
	writeQuizWithMedia(t, a)
	b.write(t, "images", "map.png", "another map") // map.png gets a new name
	request := sendFiles(t, a, b, files(file("quizzes", "geo.yaml")))
	a.write(t, "quizzes", "geo.yaml", "questions: [unclosed")

	result, err := b.manager.Accept(t.Context(), request.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Errorf("failed = %+v", result.Failed)
	}
	if got := b.read(t, "quizzes", "geo.yaml"); got != "questions: [unclosed" {
		t.Errorf("geo.yaml = %q", got)
	}
	if !strings.Contains(b.logText(), "Cannot update file references in received quiz: ") {
		t.Errorf("no warning in the log:\n%s", b.logText())
	}
}

// Merge keys, anchors and aliases stay usable: an alias that points to a
// renamed image gets the new name, the anchored value keeps its anchor.
func TestRewriteKeepsMergeKeysAndAnchors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	writeFile(t, path, "defaults: &d\n  points: 2\nimg: &i /imgs/map.png\nquestions:\n- question: Q\n  <<: *d\n  image: *i\n  options: [*i, b]\n  correct_answer: 0\n")

	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	text := readFile(t, path)
	for _, part := range []string{"<<: *d", "img: &i /imgs/map.png", "image: /imgs/m2.png", "options: [/imgs/m2.png, b]"} {
		if !strings.Contains(text, part) {
			t.Errorf("%q is missing in:\n%s", part, text)
		}
	}
	data, err := pyval.LoadYAML([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	question := data.(map[string]any)["questions"].([]any)[0].(map[string]any)
	if pyval.Str(question["points"]) != "2" {
		t.Errorf("the merge key does not work any more: %v", question)
	}
}

// ruamel.yaml refused a quiz with a repeated key; here every repeated
// reference is updated and PyYAML's reading (the last key) is right.
func TestRewriteHandlesRepeatedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	writeFile(t, path, "title: A\ntitle: B\nquestions:\n- question: Q\n  image: /imgs/map.png\n  image: /imgs/map.png\n  options: [a, b]\n  correct_answer: 0\n")
	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if text := readFile(t, path); strings.Count(text, "image: /imgs/m2.png") != 2 || !strings.Contains(text, "title: B") {
		t.Errorf("text:\n%s", text)
	}
}

// Values that are not text are not references: numbers, lists, null.
func TestRewriteSkipsValuesThatAreNotText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	text := "questions:\n- question: Q\n  image: null\n  file: 5\n  options: [[/imgs/map.png], 1, true]\n  correct_answer: 0\n"
	writeFile(t, path, text)
	changed, err := rewriteQuizMedia(path, map[mediaFile]string{{"images", "map.png"}: "m2.png"})
	if err != nil || changed || readFile(t, path) != text {
		t.Errorf("changed=%v err=%v\n%s", changed, err, readFile(t, path))
	}
}

// The sender's errors for each answer of the other side (send_offer): 502,
// 503 and 504 mean the server is not online, 404 and 405 an old WebQuiz
// without file transfer, any other 4xx/5xx a refusal with the other side's
// "error" text (or the status). A redirect is not followed and counts as
// a success, as with httpx.
func TestSendErrorsForEachAnswer(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		message string
		code    int
	}{
		{502, "502: Bad Gateway", "Server 'room-b' is not online", 404},
		{503, "", "Server 'room-b' is not online", 404},
		{504, "", "Server 'room-b' is not online", 404},
		{404, "404: Not Found", "Server 'room-b' does not support file transfer. Update WebQuiz there", 404},
		{405, "405: Method Not Allowed", "Server 'room-b' does not support file transfer. Update WebQuiz there", 404},
		{400, `{"error": "Invalid token"}`, "Server 'room-b' refused the request: Invalid token", 502},
		{429, `{"error": "Too many waiting requests on this server"}`, "Server 'room-b' refused the request: Too many waiting requests on this server", 502},
		{409, `{"error": ""}`, "Server 'room-b' refused the request: 409", 502},
		{400, `{"error": 5}`, "Server 'room-b' refused the request: 5", 502},
		{500, `["a list"]`, "Server 'room-b' refused the request: HTTP 500", 502},
		{500, "Internal Server Error", "Server 'room-b' refused the request: HTTP 500", 502},
		{302, "", "", 0},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status, " ", c.body), func(t *testing.T) {
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/start/room-b/api/transfer/offer" {
					t.Errorf("path %s", r.URL.Path)
				}
				if c.status == 302 {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer receiver.Close()
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "math.yaml"), "x")
			m := New(Deps{
				Directories: func() map[string]string { return map[string]string{"quizzes": dir} },
				Endpoint:    func() (string, string, bool) { return receiver.URL + "/start", "room-a", true },
				Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
			})

			offer, err := m.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml")))
			if c.code == 0 {
				if err != nil || offer.Status != "waiting" {
					t.Fatalf("offer %+v, err %v", offer, err)
				}
				return
			}
			te := transferErr(t, err)
			if te.Message != c.message || te.Status != c.code {
				t.Errorf("%d %q, want %d %q", te.Status, te.Message, c.code, c.message)
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.outgoing) != 0 {
				t.Errorf("a refused offer is kept: %+v", m.outgoing)
			}
		})
	}

	// Nobody answers at all.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "math.yaml"), "x")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	m := New(Deps{
		Directories: func() map[string]string { return map[string]string{"quizzes": dir} },
		Endpoint:    func() (string, string, bool) { return closed.URL, "room-a", true },
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	_, err := m.Send(t.Context(), "room-b", files(file("quizzes", "math.yaml")))
	if te := transferErr(t, err); te.Status != 502 || !strings.HasPrefix(te.Message, "Cannot reach the tunnel server: ") {
		t.Errorf("%d %q", te.Status, te.Message)
	}
}
