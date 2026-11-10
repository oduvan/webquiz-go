package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// pythonTransfer is testdata/python_transfer.json, written by
// testdata/gen_python_transfer.py from the file transfer of WebQuiz 1.19.
type pythonTransfer struct {
	WebQuizVersion string                       `json:"webquiz_version"`
	Setup          map[string]map[string]string `json:"setup"`
	InUse          map[string][]string          `json:"in_use"`
	Offered        []any                        `json:"offered"`
	Choices        []any                        `json:"choices"`
	CraftedOffer   any                          `json:"crafted_offer"`
	Captures       map[string]any               `json:"captures"`
}

// TestTransferJSONMatchesPython checks that a Go server and a Python 1.19
// server on the same tunnel server understand each other (Review Focus 5).
// It runs the steps of gen_python_transfer.py with the Go manager and
// compares everything Python recorded: the offer, download and result
// requests between the servers with their answers, what the managers give
// the admin pages, the admin WebSocket messages, the error answers and the
// files the receiver has at the end. The JSON must be the same: keys, value
// types, null where Python wrote null, and the values. Random ids, tokens
// and times are compared by the place where each one appears first.
func TestTransferJSONMatchesPython(t *testing.T) {
	data, err := os.ReadFile("testdata/python_transfer.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var python pythonTransfer
	if err := decoder.Decode(&python); err != nil {
		t.Fatal(err)
	}
	if python.WebQuizVersion != "1.19" || len(python.Captures) != 19 {
		t.Fatalf("unexpected fixture: version %q, %d captures", python.WebQuizVersion, len(python.Captures))
	}

	got := replayPythonSteps(t, python)

	want := canonical(python.Captures).(map[string]any)
	have := canonical(asJSON(t, got)).(map[string]any)
	keys := make([]string, 0, len(want))
	for key := range want {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		w := want[key]
		h, ok := have[key]
		if !ok {
			t.Errorf("%s: missing in the Go run", key)
			continue
		}
		if diff := jsonDiff(key, w, h); diff != "" {
			t.Errorf("%s differs from Python:\n%s\nPython: %s\nGo:     %s", key, diff, compact(w), compact(h))
		}
	}
}

// replayPythonSteps does what main() of gen_python_transfer.py does and
// returns the same captures.
func replayPythonSteps(t *testing.T, python pythonTransfer) map[string]any {
	t.Helper()
	tunnel := newFakeTunnel(t)
	tunnel.startRecording()
	root := t.TempDir()
	machines := map[string]*machine{}
	for _, name := range []string{"room-a", "room-b", "room-c"} {
		machines[name] = tunnel.add(t, root, name)
	}
	a, b := machines["room-a"], machines["room-b"]
	for name, setup := range python.Setup {
		for relative, text := range setup {
			kind, fileName, _ := strings.Cut(relative, "/")
			machines[name].write(t, kind, fileName, text)
		}
	}
	for name, inUse := range python.InUse {
		paths := []string{""} // Python's list starts with None
		for _, relative := range inUse {
			kind, fileName, _ := strings.Cut(relative, "/")
			paths = append(paths, machines[name].path(kind, fileName))
		}
		machines[name].setFilesInUse(paths...)
	}
	captures := map[string]any{}
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	// 1. A offers files to B
	offer, err := a.manager.Send(ctx, "room-b", python.Offered)
	must(err)
	captures["send_return"] = offer
	captures["offer_http"] = tunnel.logSince(tunnel.logLength() - 1)[0]
	captures["receiver_request_message"] = b.lastMessage(t)
	captures["receiver_state"] = state(t, b)
	captures["sender_state_waiting"] = state(t, a)

	// 2. B accepts with some choices
	requestID := listIncoming(t, b)[0].ID
	mark := tunnel.logLength()
	result, err := b.manager.Accept(ctx, requestID, python.Choices)
	must(err)
	captures["accept_return"] = result
	captures["accept_http"] = tunnel.logSince(mark)
	captures["receiver_removed_message"] = b.lastMessage(t)
	captures["sender_result_message"] = a.lastMessage(t)
	captures["sender_state_accepted"] = state(t, a)
	captures["receiver_files"] = machineFiles(t, b)

	// 3. A offers another file, B rejects it
	_, err = a.manager.Send(ctx, "room-b", files(file("quizzes", "math.yaml")))
	must(err)
	requestID = listIncoming(t, b)[0].ID
	mark = tunnel.logLength()
	must(b.manager.Reject(ctx, requestID))
	captures["reject_http"] = tunnel.logSince(mark)
	captures["reject_removed_message"] = b.lastMessage(t)
	captures["reject_result_message"] = a.lastMessage(t)

	// 4. An offer with odd sizes and checksums
	mark = tunnel.logLength()
	crafted, err := json.Marshal(python.CraftedOffer)
	must(err)
	post(t, tunnel.baseURL+"/room-b/api/transfer/offer", "application/json", string(crafted))
	captures["crafted_offer_http"] = tunnel.logSince(mark)
	captures["crafted_request_message"] = b.lastMessage(t)

	// 5. Errors of the public endpoints
	mark = tunnel.logLength()
	token := strings.Repeat("x", 43)
	base := tunnel.baseURL
	post(t, base+"/room-b/api/transfer/offer", "", "not json")
	post(t, base+"/room-b/api/transfer/offer", "application/json", `["not", "a dict"]`)
	post(t, base+"/room-b/api/transfer/offer", "application/json", `{"from": "../x", "token": "`+token+`", "files": []}`)
	post(t, base+"/room-b/api/transfer/offer", "application/json", `{"from": "room-a", "token": "short", "files": []}`)
	post(t, base+"/room-b/api/transfer/offer", "application/json", `{"from": "room-a", "token": "`+token+`", "files": []}`)
	post(t, base+"/room-b/api/transfer/offer", "application/json",
		`{"from": "room-a", "token": "`+token+`", "files": [{"type": "logs", "name": "../a.log"}]}`)
	get(t, base+"/room-a/api/transfer/download/"+token+"/quizzes/geo.yaml")
	post(t, base+"/room-a/api/transfer/result", "", "not json")
	post(t, base+"/room-a/api/transfer/result", "application/json", `{"token": "`+token+`", "status": "hacked"}`)
	post(t, base+"/room-a/api/transfer/result", "application/json", `{"token": "`+token+`", "status": "accepted"}`)
	aLog := `{"from": "room-a", "token": "` + token + `", "files": [{"type": "logs", "name": "a.log"}]}`
	b.setConnected(false)
	post(t, base+"/room-b/api/transfer/offer", "application/json", aLog)
	b.setConnected(true)
	for range 21 {
		post(t, base+"/room-c/api/transfer/offer", "application/json", aLog)
	}
	var publicErrors []recorded
	for _, entry := range tunnel.logSince(mark) {
		if entry.Status != http.StatusAccepted {
			publicErrors = append(publicErrors, entry)
		}
	}
	captures["public_errors_http"] = publicErrors

	// 6. Errors that the sender's admin sees
	adminErrors := map[string]any{}
	asError := func(name string, err error) {
		t.Helper()
		te := transferErr(t, err)
		adminErrors[name] = map[string]any{"status": te.Status, "error": te.Message}
	}
	_, err = a.manager.Send(ctx, "room-x", files(file("quizzes", "math.yaml")))
	asError("offline", err)
	b.setConnected(false)
	_, err = a.manager.Send(ctx, "room-b", files(file("quizzes", "math.yaml")))
	asError("refused", err)
	b.setConnected(true)
	a.setConnected(false)
	_, err = a.manager.Send(ctx, "room-b", python.Offered)
	asError("send_without_tunnel", err)
	_, err = b.manager.Accept(ctx, "unknown", nil)
	asError("accept_unknown", err)
	asError("reject_unknown", b.manager.Reject(ctx, "unknown"))
	captures["admin_errors"] = adminErrors
	captures["state_without_tunnel"] = state(t, a)
	return captures
}

func post(t *testing.T, url, contentType, body string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
}

func get(t *testing.T, url string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
}

// machineFiles is Machine.files(): every file as "<kind>/<name>": text,
// quizzes as their YAML data.
func machineFiles(t *testing.T, m *machine) map[string]any {
	t.Helper()
	result := map[string]any{}
	for _, kind := range []string{"quizzes", "logs", "csv", "images", "attachments"} {
		for _, name := range m.files(t, kind) {
			text := m.read(t, kind, name)
			if kind != "quizzes" {
				result[kind+"/"+name] = text
				continue
			}
			value, err := pyval.LoadYAML([]byte(text))
			if err != nil {
				t.Fatalf("%s does not load: %v\n%s", name, err, text)
			}
			result[kind+"/"+name] = map[string]any{"yaml": value}
		}
	}
	return result
}

var (
	isoTime       = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d{6})?$`)
	hexID         = regexp.MustCompile(`^[0-9a-f]{16}$`)
	urlsafeToken  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	downloadToken = regexp.MustCompile(`/download/([A-Za-z0-9_-]{43})/`)
)

// canonical replaces random values with placeholders numbered in the order
// they first appear (map keys in sorted order): ids "<id1>", tokens
// "<token1>", times "<time>".
func canonical(v any) any {
	c := &canonicalizer{ids: map[string]string{}, tokens: map[string]string{}}
	return c.walk("", v)
}

type canonicalizer struct {
	ids, tokens map[string]string
}

func (c *canonicalizer) walk(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, k := range keys {
			out[k] = c.walk(k, x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = c.walk(key, item)
		}
		return out
	case string:
		switch {
		case (key == "created" || key == "received") && isoTime.MatchString(x):
			return "<time>"
		case key == "id" && hexID.MatchString(x):
			return placeholder(c.ids, "id", x)
		case key == "token" && urlsafeToken.MatchString(x):
			return placeholder(c.tokens, "token", x)
		case key == "path":
			return downloadToken.ReplaceAllStringFunc(x, func(m string) string {
				return "/download/" + placeholder(c.tokens, "token", m[len("/download/"):len(m)-1]) + "/"
			})
		}
	}
	return v
}

func placeholder(seen map[string]string, kind, value string) string {
	if p, ok := seen[value]; ok {
		return p
	}
	p := fmt.Sprintf("<%s%d>", kind, len(seen)+1)
	seen[value] = p
	return p
}

// jsonDiff describes the first place where two JSON values differ: a
// missing or extra key, another type (null vs a value too), or another value.
func jsonDiff(path string, want, have any) string {
	switch w := want.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: Python has an object, Go has %s", path, compact(have))
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range h {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			wv, inW := w[k]
			hv, inH := h[k]
			switch {
			case !inH:
				return fmt.Sprintf("%s.%s: missing in Go", path, k)
			case !inW:
				return fmt.Sprintf("%s.%s: Go has a key Python did not have", path, k)
			}
			if d := jsonDiff(path+"."+k, wv, hv); d != "" {
				return d
			}
		}
		return ""
	case []any:
		h, ok := have.([]any)
		if !ok {
			return fmt.Sprintf("%s: Python has a list, Go has %s", path, compact(have))
		}
		if len(w) != len(h) {
			return fmt.Sprintf("%s: Python has %d items, Go has %d", path, len(w), len(h))
		}
		for i := range w {
			if d := jsonDiff(fmt.Sprintf("%s[%d]", path, i), w[i], h[i]); d != "" {
				return d
			}
		}
		return ""
	}
	if fmt.Sprintf("%T", want) != fmt.Sprintf("%T", have) || !equalScalar(want, have) {
		return fmt.Sprintf("%s: Python %s, Go %s", path, compact(want), compact(have))
	}
	return ""
}

func equalScalar(a, b any) bool {
	return slices.Equal([]byte(compact(a)), []byte(compact(b)))
}

func compact(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

// The fixture must stay in sync with its generator: the Go run must ask for
// the same files in the same order as the script.
func TestPythonFixtureIsFromTheScript(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("testdata", "gen_python_transfer.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{`send_offer("room-b", OFFERED)`, `accept(request_id, CHOICES)`, `reject(request_id)`, `for _ in range(21):`} {
		if !bytes.Contains(script, []byte(step)) {
			t.Errorf("the script has no %q any more: update replayPythonSteps", step)
		}
	}
}
