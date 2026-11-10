package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/server"
)

// The YAML that tests write must read back (with PyYAML's rules) as the
// values the test gave, floats included.
func TestYAMLReadsBackLikePyYAML(t *testing.T) {
	data := map[string]any{
		"float":    30.0,
		"big":      1e17,
		"small":    1e-7,
		"tenth":    0.1,
		"negzero":  math.Copysign(0, -1),
		"inf":      math.Inf(1),
		"int":      30,
		"yes":      "yes",
		"on":       "on",
		"time":     "15:00",
		"none":     nil,
		"bool":     true,
		"list":     []any{1, "x", 2.5, []string{"a"}},
		"nested":   map[string]any{"b": 1, "a": []map[string]any{{"k": "v"}}},
		"unicode":  "Скільки буде 2 + 2?",
		"multline": "line one\nline two\n",
	}
	text, err := YAML(data)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := pyval.LoadYAML(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	m := loaded.(map[string]any)
	kinds := map[string]pyval.Kind{
		"float": pyval.KindFloat, "big": pyval.KindFloat, "small": pyval.KindFloat, "tenth": pyval.KindFloat,
		"negzero": pyval.KindFloat, "inf": pyval.KindFloat, "int": pyval.KindInt, "yes": pyval.KindStr,
		"on": pyval.KindStr, "time": pyval.KindStr, "none": pyval.KindNone, "bool": pyval.KindBool,
	}
	for key, kind := range kinds {
		if got := pyval.KindOf(m[key]); got != kind {
			t.Errorf("%s read back as %s (%v), want %s\n%s", key, got, m[key], kind, text)
		}
	}
	for _, key := range []string{"float", "big", "small", "tenth", "int", "yes", "on", "time", "bool", "unicode", "multline"} {
		if !pyval.Equal(m[key], data[key]) {
			t.Errorf("%s read back as %v, want %v", key, m[key], data[key])
		}
	}
	if !strings.Contains(string(text), "float: 30.0\n") || !strings.Contains(string(text), "big: 1.0e+17\n") || !strings.Contains(string(text), `"yes": "yes"`) {
		t.Errorf("floats are not written like PyYAML:\n%s", text)
	}
	if got := pyval.Str(m["list"]); got != "[1, 'x', 2.5, ['a']]" {
		t.Errorf("list read back as %s", got)
	}
	if got := pyval.Str(m["nested"]); got != "{'a': [{'k': 'v'}], 'b': 1}" {
		t.Errorf("nested read back as %s", got)
	}
	if strings.Index(string(text), "big:") > strings.Index(string(text), "time:") {
		t.Errorf("keys must be sorted, like yaml.dump:\n%s", text)
	}
}

func TestDeepMerge(t *testing.T) {
	base := DefaultConfig(8080)
	override := map[string]any{
		"admin":        map[string]any{"master_key": "other"},
		"registration": map[string]any{"approve": true},
		"server":       "not a map",
	}
	merged := DeepMerge(base, override)
	want := map[string]any{
		"server": "not a map",
		"paths": map[string]any{
			"quizzes_dir": "quizzes", "logs_dir": "logs", "csv_dir": "data", "static_dir": "static",
		},
		"admin":        map[string]any{"master_key": "other", "trusted_ips": []any{}},
		"registration": map[string]any{"approve": true},
	}
	if !reflect.DeepEqual(merged, want) {
		t.Errorf("merged %v\nwant %v", merged, want)
	}
	merged["registration"].(map[string]any)["approve"] = false
	if override["registration"].(map[string]any)["approve"] != true {
		t.Error("DeepMerge must copy the maps of override")
	}
}

func TestStartServerLikeConftest(t *testing.T) {
	t.Parallel()
	ts := StartServer(t, Options{Config: map[string]any{
		"paths":        map[string]any{"quizzes_dir": "my_quizzes"},
		"registration": map[string]any{"fields": []any{"Grade"}},
	}})
	if ts.Config.Admin.MasterKey != "test123" || len(ts.Config.Admin.TrustedIPs) != 0 {
		t.Errorf("admin config %+v", ts.Config.Admin)
	}
	if ts.Config.Server.Port != ts.Port || !strings.HasSuffix(ts.URL, ":"+strconv.Itoa(ts.Port)) {
		t.Errorf("port %d in config, %d served at %s", ts.Config.Server.Port, ts.Port, ts.URL)
	}
	if ts.QuizzesDir != filepath.Join(ts.Dir, "my_quizzes") || ts.StaticDir != filepath.Join(ts.Dir, "static") {
		t.Errorf("paths %s %s", ts.QuizzesDir, ts.StaticDir)
	}
	if _, err := os.Stat(filepath.Join(ts.QuizzesDir, "test_quiz.yaml")); err != nil {
		t.Errorf("default quiz: %v", err)
	}
	if ts.Config.ConfigPath != ts.ConfigPath || ts.Config.Registration.Fields[0] != "Grade" {
		t.Errorf("config %+v", ts.Config)
	}
	if res := ts.GetJSON(t, "/"); res.Status != 200 {
		t.Errorf("GET /: %d", res.Status)
	}
	cookies := ts.AdminCookies(t, "test123")
	if res := ts.GetJSON(t, "/api/admin/check-session", WithCookies(cookies...)); res.Status != 200 {
		t.Errorf("check-session with the cookies: %d %s", res.Status, res.Body)
	}
	if !strings.Contains(ts.ReadLog(t), "Switched to quiz: test_quiz.yaml") {
		t.Error("the log has no quiz switch")
	}
}

func TestStartServerMasterKeyFromEnv(t *testing.T) {
	t.Parallel()
	ts := StartServer(t, Options{Env: map[string]string{
		"WEBQUIZ_MASTER_KEY":                "from-env",
		"WEBQUIZ_TIME_LIMIT_GRACE_SECONDS":  "0.5",
		"WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL": "0.2",
	}})
	if res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"}); res.Status != 401 {
		t.Errorf("the file key must lose to WEBQUIZ_MASTER_KEY: %d", res.Status)
	}
	ts.AdminCookies(t, "from-env")
}

func TestQuizzesAsRawText(t *testing.T) {
	t.Parallel()
	ts := StartServer(t, Options{Quizzes: map[string]any{
		"raw.yaml": "title: Raw\nquestions:\n- question: Q\n  options: [a, b]\n  correct_answer: 1.0\n",
	}})
	data, _ := os.ReadFile(filepath.Join(ts.QuizzesDir, "raw.yaml"))
	if !strings.Contains(string(data), "correct_answer: 1.0") {
		t.Errorf("raw text changed: %s", data)
	}
}

// ServerOptions reach server.New; Env still wins for the time limit values.
func TestServerOptionsPassThrough(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	var mu sync.Mutex
	ts := StartServer(t, Options{
		ServerOptions: server.Options{
			Logger:        slog.New(slog.NewTextHandler(&lockedBuffer{mu: &mu, buf: &logs}, nil)),
			FlushInterval: 20 * time.Millisecond,
			Now:           func() time.Time { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) },
		},
		Env: map[string]string{"WEBQUIZ_TIME_LIMIT_GRACE_SECONDS": "0"},
	})
	ts.GetJSON(t, "/")
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(logs.String(), "Switched to quiz: test_quiz.yaml") {
		t.Errorf("the given Logger got no log lines:\n%s", logs.String())
	}
}

type lockedBuffer struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Bodies go out through json.Marshal: a whole float loses its ".0", and
// json.Number keeps the text as written (how a test sends 1.0).
func TestJSONBodies(t *testing.T) {
	t.Parallel()
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Content-Type", r.Header.Get("Content-Type"))
		_, _ = w.Write(body)
	}))
	defer echo.Close()
	client := &Client{BaseURL: echo.URL, HTTP: echo.Client()}
	res := client.PostJSON(t, "/", map[string]any{"float": 1.0, "number": json.Number("1.0")})
	if res.Text() != `{"float":1,"number":1.0}` || res.Header.Get("X-Content-Type") != "application/json" {
		t.Errorf("JSON body %s (%s)", res.Body, res.Header.Get("X-Content-Type"))
	}
	if res := client.PostJSON(t, "/", `{"raw": 1.0}`); res.Text() != `{"raw": 1.0}` || res.Header.Get("X-Content-Type") != "" {
		t.Errorf("raw body %s (%s)", res.Body, res.Header.Get("X-Content-Type"))
	}
}

func TestBuildBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	path := BuildBinary(t)
	if again := BuildBinary(t); again != path {
		t.Errorf("BuildBinary built twice: %s and %s", path, again)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(output), "webquiz ") {
		t.Errorf("%s --version: %v %q", path, err, output)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	RemoveBinary()
	os.Exit(code)
}
