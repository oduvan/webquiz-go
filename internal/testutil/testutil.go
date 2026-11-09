// Package testutil starts WebQuiz servers for tests. It is the Go version of
// tests/conftest.py from the Python project.
//
// StartServer is custom_webquiz_server: it makes a temp folder, writes the
// quiz files and a config file (the same default config as conftest.py:
// master key "test123", no trusted IPs), loads the config with config.Load
// and runs the server in this process on a free port of 127.0.0.1. The
// server stops when the test ends.
//
//	func TestSomething(t *testing.T) {
//		t.Parallel()
//		ts := testutil.StartServer(t, testutil.Options{
//			Config:  map[string]any{"registration": map[string]any{"approve": true}},
//			Quizzes: map[string]any{"math.yaml": map[string]any{
//				"title":     "Math",
//				"questions": []any{map[string]any{"question": "2+2?", "options": []any{"3", "4"}, "correct_answer": 1}},
//			}},
//		})
//		admin := ts.AdminClient(t, "test123") // keeps the session cookie
//		res := admin.GetJSON(t, "/api/admin/list-quizzes")
//		if res.Status != 200 { t.Fatal(res.Text()) }
//		data := res.JSON(t)     // numbers are json.Number
//		userID := ts.RegisterStudent(t, "Alice")
//		res = ts.PostJSON(t, "/api/submit-answer", map[string]any{"user_id": userID, "question_id": 1, "selected_answer": 1})
//		res = ts.GetJSON(t, "/admin/", testutil.FromIP("8.8.8.8")) // a public address: 403
//		_ = data
//	}
//
// Request bodies are encoded with encoding/json: a whole float such as 1.0
// goes out as 1. To send 1.0 (a test of "1 and 1.0 differ") put
// json.Number("1.0") in the body, or send the JSON text as a string. Quiz and
// config values are written with YAML (like PyYAML), which keeps 1.0 a float.
//
// Tests that need the real program use BuildBinary.
package testutil

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/pyval"
	"github.com/oduvan/webquiz-go/internal/server"
)

// Options describe a test server, like the arguments of
// conftest.custom_webquiz_server.
type Options struct {
	// Config is merged into the default test config: nested maps are merged
	// key by key, other values replace the default. Relative paths in
	// "paths" and the tunnel key paths are taken inside the temp folder,
	// as the Python tests ran the server in their temp folder.
	Config map[string]any

	// Quizzes maps quiz file names to their content. A string or []byte
	// value is written as it is (YAML text); any other value is written
	// with YAML (like yaml.dump). Nil gives the default quiz test_quiz.yaml;
	// an empty map gives no quiz files at all.
	Quizzes map[string]any

	// Env holds environment variables the Python tests gave the server
	// process. A server in this process cannot read real environment
	// variables (tests run in parallel), so only these names work:
	// WEBQUIZ_TIME_LIMIT_GRACE_SECONDS and WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL
	// (seconds, like Python's float()) and WEBQUIZ_MASTER_KEY. Another name
	// fails the test: use BuildBinary for those.
	Env map[string]string

	// ServerOptions go to server.New as they are: for example Now (a test
	// clock) or FlushInterval (a short CSV flush, so a test need not wait
	// 5 s for the CSV files). A nil Logger means the test log. Values from
	// Env win over Grace and CheckInterval set here.
	ServerOptions server.Options
}

// TestServer is a server running in this process with its own temp folder.
// Its embedded Client sends requests without keeping cookies.
type TestServer struct {
	*Client

	URL  string // "http://127.0.0.1:<port>"
	Port int    // also written into the config as server.port

	Dir        string // the temp folder
	ConfigPath string // the config file the server was started with
	QuizzesDir string // absolute paths of the server's folders
	LogsDir    string
	CSVDir     string
	StaticDir  string

	Config *config.Config // the loaded config; do not change it
	Server *server.Server
}

// DefaultQuizzes is the quiz set of conftest.py: one quiz, test_quiz.yaml.
func DefaultQuizzes() map[string]any {
	return map[string]any{
		"test_quiz.yaml": map[string]any{
			"title":       "Test Quiz",
			"description": "A test quiz for admin API testing",
			"questions": []any{
				map[string]any{"question": "What is 2 + 2?", "options": []any{"3", "4", "5", "6"}, "correct_answer": 1},
			},
		},
	}
}

// DefaultConfig is the config of conftest.py for a server on port. Its
// folders are relative: StartServer puts them inside the temp folder.
func DefaultConfig(port int) map[string]any {
	return map[string]any{
		"server": map[string]any{"port": port},
		"paths": map[string]any{
			"quizzes_dir": "quizzes",
			"logs_dir":    "logs",
			"csv_dir":     "data",
			"static_dir":  "static",
		},
		"admin": map[string]any{"master_key": "test123", "trusted_ips": []any{}},
	}
}

// DeepMerge merges override into base, like deep_merge in conftest.py: when
// both values are maps, they are merged key by key; otherwise the override
// replaces the value. base is changed; maps from override are copied, so
// later merges do not change the caller's maps.
func DeepMerge(base, override map[string]any) map[string]any {
	for key, value := range override {
		current, currentIsMap := base[key].(map[string]any)
		next, nextIsMap := value.(map[string]any)
		switch {
		case currentIsMap && nextIsMap:
			DeepMerge(current, next)
		case nextIsMap:
			base[key] = DeepMerge(map[string]any{}, next)
		default:
			base[key] = value
		}
	}
	return base
}

// StartServer starts a server for one test and stops it when the test ends.
func StartServer(t testing.TB, opts Options) *TestServer {
	t.Helper()
	dir := t.TempDir()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testutil: listen: %v", err)
	}
	started := false
	defer func() {
		if !started {
			listener.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port

	settings := DeepMerge(DefaultConfig(port), opts.Config)
	DeepMerge(settings, map[string]any{"server": map[string]any{"port": port}})
	insideDir(settings, dir)

	paths := settings["paths"].(map[string]any)
	quizzesDir := pathSetting(t, paths, "quizzes_dir")
	if err := os.MkdirAll(quizzesDir, 0o755); err != nil {
		t.Fatalf("testutil: %v", err)
	}
	quizzes := opts.Quizzes
	if quizzes == nil {
		quizzes = DefaultQuizzes()
	}
	for name, content := range quizzes {
		writeQuiz(t, filepath.Join(quizzesDir, name), content)
	}

	configPath := filepath.Join(dir, "webquiz.yaml")
	text, err := YAML(settings)
	if err != nil {
		t.Fatalf("testutil: write the config: %v", err)
	}
	if err := os.WriteFile(configPath, text, 0o644); err != nil {
		t.Fatalf("testutil: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("testutil: the test config does not load: %v\n%s", err, text)
	}
	cfg.ConfigPath = configPath

	serverOpts := opts.ServerOptions
	if serverOpts.Logger == nil {
		serverOpts.Logger = testLogger(t)
	}
	applyEnv(t, opts.Env, cfg, &serverOpts)

	srv, err := server.New(cfg, serverOpts)
	if err != nil {
		t.Fatalf("testutil: the server did not start: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(srv.Handler())
	httpServer.Listener.Close()
	httpServer.Listener = listener
	httpServer.Start()
	started = true
	srv.Start(context.Background())
	t.Cleanup(func() {
		httpServer.Close()
		srv.Close()
	})

	url := "http://" + listener.Addr().String()
	return &TestServer{
		Client:     &Client{BaseURL: url, HTTP: httpServer.Client()},
		URL:        url,
		Port:       port,
		Dir:        dir,
		ConfigPath: configPath,
		QuizzesDir: cfg.Paths.QuizzesDir,
		LogsDir:    cfg.Paths.LogsDir,
		CSVDir:     cfg.Paths.CSVDir,
		StaticDir:  cfg.Paths.StaticDir,
		Config:     cfg,
		Server:     srv,
	}
}

// insideDir makes the relative folders and key paths of the config relative
// to dir.
func insideDir(settings map[string]any, dir string) {
	resolve := func(section map[string]any, key string) {
		if path, ok := section[key].(string); ok && path != "" && !filepath.IsAbs(path) {
			section[key] = filepath.Join(dir, path)
		}
	}
	if paths, ok := settings["paths"].(map[string]any); ok {
		for _, key := range []string{"quizzes_dir", "logs_dir", "csv_dir", "static_dir"} {
			resolve(paths, key)
		}
	}
	if tunnel, ok := settings["tunnel"].(map[string]any); ok {
		resolve(tunnel, "public_key")
		resolve(tunnel, "private_key")
	}
}

func pathSetting(t testing.TB, paths map[string]any, key string) string {
	t.Helper()
	path, ok := paths[key].(string)
	if !ok || path == "" {
		t.Fatalf("testutil: paths.%s must be text, got %v", key, paths[key])
	}
	return path
}

func writeQuiz(t testing.TB, path string, content any) {
	t.Helper()
	var data []byte
	switch c := content.(type) {
	case string:
		data = []byte(c)
	case []byte:
		data = c
	default:
		var err error
		if data, err = YAML(c); err != nil {
			t.Fatalf("testutil: write the quiz %s: %v", filepath.Base(path), err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("testutil: %v", err)
	}
}

// applyEnv uses the environment variables of the Python tests.
func applyEnv(t testing.TB, env map[string]string, cfg *config.Config, opts *server.Options) {
	t.Helper()
	for name, value := range env {
		switch name {
		case "WEBQUIZ_TIME_LIMIT_GRACE_SECONDS":
			seconds := envSeconds(t, name, value)
			opts.Grace = time.Duration(seconds * float64(time.Second))
			if opts.Grace <= 0 {
				opts.Grace = server.NoGrace
			}
		case "WEBQUIZ_TIME_LIMIT_CHECK_INTERVAL":
			opts.CheckInterval = time.Duration(envSeconds(t, name, value) * float64(time.Second))
		case "WEBQUIZ_MASTER_KEY":
			if value != "" { // load_config_with_overrides: the variable wins over the file
				cfg.Admin.MasterKey = value
			}
		default:
			t.Fatalf("testutil: the environment variable %s cannot be given to a server in this process; use BuildBinary", name)
		}
	}
}

func envSeconds(t testing.TB, name, value string) float64 {
	t.Helper()
	seconds, ok := pyval.ToFloat(value)
	if !ok {
		t.Fatalf("testutil: %s=%q is not a number", name, value)
	}
	return seconds
}

// AdminCookies logs in with the master key (POST /api/admin/auth, like
// get_admin_session) and returns the session cookie. The test fails unless
// the login works.
func (ts *TestServer) AdminCookies(t testing.TB, key string) []*http.Cookie {
	t.Helper()
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": key})
	if res.Status != http.StatusOK {
		t.Fatalf("admin login failed: %d %s", res.Status, res.Body)
	}
	return res.Cookies
}

// AdminClient returns a client that is logged in as admin with key.
func (ts *TestServer) AdminClient(t testing.TB, key string) *Client {
	t.Helper()
	client := ts.NewClient()
	res := client.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": key})
	if res.Status != http.StatusOK {
		t.Fatalf("admin login failed: %d %s", res.Status, res.Body)
	}
	return client
}

// RegisterStudent registers a student (POST /api/register, like
// register_student in conftest.py) and returns the new user_id.
func (ts *TestServer) RegisterStudent(t testing.TB, name string) string {
	t.Helper()
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": name})
	if res.Status != http.StatusOK {
		t.Fatalf("register %q: %d %s", name, res.Status, res.Body)
	}
	userID, ok := res.JSON(t)["user_id"].(string)
	if !ok {
		t.Fatalf("register %q: no user_id in %s", name, res.Body)
	}
	return userID
}

// LogFile is the path of the server's log file (logs/NNNN.log).
func (ts *TestServer) LogFile() string { return ts.Server.LogFile() }

// ReadLog returns what the server has written to its log file so far.
func (ts *TestServer) ReadLog(t testing.TB) string {
	t.Helper()
	data, err := os.ReadFile(ts.LogFile())
	if err != nil {
		t.Fatalf("read the server log: %v", err)
	}
	return string(data)
}

// testLogger writes the server log to t.Log, so a failing test shows it.
// Nothing is written once the test has finished.
func testLogger(t testing.TB) *slog.Logger {
	w := &testWriter{t: t}
	t.Cleanup(w.stop) // registered first, so it runs after the server has stopped
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

type testWriter struct {
	mu      sync.Mutex
	t       testing.TB
	stopped bool
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.t.Log(string(trimNewline(p)))
	}
	return len(p), nil
}

func (w *testWriter) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
}

func trimNewline(p []byte) []byte {
	if len(p) > 0 && p[len(p)-1] == '\n' {
		return p[:len(p)-1]
	}
	return p
}

var _ io.Writer = (*testWriter)(nil)
