package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/oduvan/webquiz-go/internal/config"
)

// adminCall sends a request to the server's handler from 127.0.0.1 with an
// admin session (the first call logs in).
type adminCall struct {
	t       *testing.T
	s       *Server
	cookies []*http.Cookie
}

func newAdminCall(t *testing.T, s *Server) *adminCall {
	a := &adminCall{t: t, s: s}
	res := a.do("POST", "/api/admin/auth", `{"master_key": "test123"}`)
	a.cookies = res.Result().Cookies()
	return a
}

func (a *adminCall) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	for _, c := range a.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.s.Handler().ServeHTTP(rec, req)
	return rec
}

func (a *adminCall) json(method, path, body string) (int, map[string]any) {
	rec := a.do(method, path, body)
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		a.t.Fatalf("%s %s: %d not JSON: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec.Code, reply
}

// serverWithConfigFile starts a server whose config was loaded from a file.
func serverWithConfigFile(t *testing.T, quizzes map[string]string, text string, change func(*config.Config, *Options)) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "webquiz.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, quizzes, func(cfg *config.Config, opts *Options) {
		cfg.ConfigPath = path
		if change != nil {
			change(cfg, opts)
		}
	})
	return s, path
}

// When applying the new settings fails, the file goes back to its old text,
// the answer says so, and the running settings stay.
func TestConfigSaveRollsBackWhenApplyingFails(t *testing.T) {
	const before = "# my config\nlanguage: uk\n"
	s, path := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, before, nil)
	s.configHook = func(*config.Config) error { return errors.New("disk is full") }
	admin := newAdminCall(t, s)

	status, reply := admin.json("PUT", "/api/admin/config", `{"content": "language: en\n"}`)
	if status != 500 || reply["success"] != false || reply["error"] != "Failed to apply configuration: disk is full. Changes rolled back." || reply["config_path"] != path {
		t.Errorf("%d %v", status, reply)
	}
	if data, _ := os.ReadFile(path); string(data) != before {
		t.Errorf("the file was not rolled back: %q", data)
	}
	if s.text("register_title") != "Зареєструйтеся для початку тесту" {
		t.Error("the language changed although the save failed")
	}
	// The next save works (the lock was released).
	s.configHook = nil
	if status, reply := admin.json("PUT", "/api/admin/config", `{"content": "language: en\n"}`); status != 200 {
		t.Errorf("%d %v", status, reply)
	}
}

// A panic while applying counts as a failure too.
func TestConfigSaveRollsBackOnPanic(t *testing.T) {
	const before = "language: uk\n"
	s, path := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, before, nil)
	s.configHook = func(*config.Config) error { panic("boom") }
	status, reply := newAdminCall(t, s).json("PUT", "/api/admin/config", `{"content": "language: en\n"}`)
	if status != 500 || !strings.Contains(reply["error"].(string), "boom. Changes rolled back.") {
		t.Errorf("%d %v", status, reply)
	}
	if data, _ := os.ReadFile(path); string(data) != before {
		t.Errorf("the file was not rolled back: %q", data)
	}
}

// Without a config file the answer is 400, and a body that is not an object
// fails like the Python handler did.
func TestConfigSaveWithoutConfigFileAndBadBodies(t *testing.T) {
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, nil)
	admin := newAdminCall(t, s)
	status, reply := admin.json("PUT", "/api/admin/config", `{"content": "language: en"}`)
	if status != 400 || reply["error"] != "No config file was specified. Server started without --config parameter." {
		t.Errorf("%d %v", status, reply)
	}

	s, _ = serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, "language: uk\n", nil)
	admin = newAdminCall(t, s)
	for body, want := range map[string]string{
		`[1]`:                     "'list' object has no attribute 'get'",
		`5`:                       "argument of type 'int' is not iterable",
		`null`:                    "argument of type 'NoneType' is not iterable",
		`"data"`:                  "string indices must be integers, not 'str'",
		`{"content": 5}`:          "'int' object has no attribute 'strip'",
		`{"content": [1]}`:        "'list' object has no attribute 'strip'",
		`{"content": "a: [1"}`:    "Invalid YAML syntax",
		`{"data": null}`:          "data must be a dictionary",
		`{"data": [1], "x": 1}`:   "data must be a dictionary",
		`{"data": {"server": 1}}`: "Configuration validation failed",
	} {
		_, reply := admin.json("PUT", "/api/admin/config", body)
		text, _ := reply["error"].(string)
		if !strings.Contains(text, want) {
			t.Errorf("body %s: error %q, want %q", body, text, want)
		}
	}
}

// A save that names a section's keys only counts those keys for the restart
// list, and the live settings of server and paths do not change.
func TestConfigSaveKeepsRunningServerSettings(t *testing.T) {
	s, _ := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, "language: uk\n", nil)
	admin := newAdminCall(t, s)
	_, reply := admin.json("PUT", "/api/admin/config", `{"content": "server:\n  host: 1.2.3.4\n  port: 9999\npaths:\n  logs_dir: elsewhere\nadmin:\n  master_key: other\n  trusted_ips: [\"9.9.9.9\"]\n"}`)
	want := `["server.host","server.port","paths.logs_dir","admin.master_key"]`
	got, _ := json.Marshal(reply["restart_required"])
	if string(got) != want {
		t.Errorf("restart_required %s, want %s", got, want)
	}
	if !strings.Contains(reply["message"].(string), "Server restart required for: server.host, server.port, paths.logs_dir, admin.master_key. Other changes applied.") {
		t.Errorf("message %v", reply["message"])
	}
	s.withLock(func() {
		if s.cfg.Server.Port == 9999 || s.cfg.Server.Host == "1.2.3.4" || s.masterKey != "test123" || s.cfg.Admin.MasterKey == "other" {
			t.Errorf("server settings changed: %+v", s.cfg.Server)
		}
		if len(s.cfg.Admin.TrustedIPs) != 1 || s.cfg.Admin.TrustedIPs[0] != "9.9.9.9" {
			t.Errorf("trusted_ips %v", s.cfg.Admin.TrustedIPs)
		}
	})
}

// With several quizzes and none chosen, no quiz runs: the save only applies.
func TestConfigSaveMessageWithoutRunningQuiz(t *testing.T) {
	s, _ := serverWithConfigFile(t, map[string]string{"a.yaml": quizText("A"), "b.yaml": quizText("B")}, "language: uk\n", nil)
	_, reply := newAdminCall(t, s).json("PUT", "/api/admin/config", `{"content": "language: en\n"}`)
	if reply["message"] != "Configuration saved and applied." {
		t.Errorf("message %v", reply["message"])
	}
	s.withLock(func() {
		if s.currentQuizFile != "" {
			t.Errorf("a quiz runs: %s", s.currentQuizFile)
		}
	})
}

// An empty file gives the defaults, and the answer's form data then comes
// from the running settings (Python read them from self.config).
func TestConfigSaveEmptyContentGivesDefaults(t *testing.T) {
	s, path := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, "language: en\nextra_answers_with_users_csv: true\n", nil)
	_, reply := newAdminCall(t, s).json("PUT", "/api/admin/config", `{"content": "  \n"}`)
	data, _ := json.Marshal(reply["config_data"])
	if reply["config_content"] != "" || string(data) != `{"extra_answers_with_users_csv":false,"language":"uk","registration":{}}` {
		t.Errorf("%v / %s", reply["config_content"], data)
	}
	if text, _ := os.ReadFile(path); len(text) != 0 {
		t.Errorf("file %q", text)
	}
}

// Saves run one at a time, so two partial updates at once both reach the file.
func TestConfigSavesAtTheSameTimeBothArrive(t *testing.T) {
	s, path := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, "# top\n", nil)
	admin := newAdminCall(t, s)
	var wg sync.WaitGroup
	for _, body := range []string{
		`{"data": {"registration": {"approve": true}}}`,
		`{"data": {"extra_answers_with_users_csv": true}}`,
		`{"data": {"language": "en"}}`,
	} {
		wg.Go(func() {
			if status, reply := admin.json("PUT", "/api/admin/config", body); status != 200 {
				t.Errorf("%s: %d %v", body, status, reply)
			}
		})
	}
	wg.Wait()
	data, _ := os.ReadFile(path)
	for _, want := range []string{"# top", "approve: true", "extra_answers_with_users_csv: true", "language: en"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the file lacks %q:\n%s", want, data)
		}
	}
}

// The tunnel buttons: no tunnel configured is a 400; a failed connection is a
// 500 with the reason, and the admin pages hear the status.
func TestTunnelEndpoints(t *testing.T) {
	plain := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, nil)
	for _, path := range []string{"/api/admin/tunnel/connect", "/api/admin/tunnel/disconnect"} {
		status, reply := newAdminCall(t, plain).json("POST", path, "")
		if status != 400 || reply["error"] != "Tunnel not configured" {
			t.Errorf("%s: %d %v", path, status, reply)
		}
	}

	keys := t.TempDir()
	s, _ := serverWithConfigFile(t, map[string]string{"q.yaml": quizText("Q")}, "language: uk\n", func(cfg *config.Config, _ *Options) {
		cfg.Tunnel = config.TunnelConfig{
			Server:     "127.0.0.1", // SSH is tried on port 22 here
			PublicKey:  filepath.Join(keys, "id.pub"),
			PrivateKey: filepath.Join(keys, "id"),
			Config:     &config.TunnelServerConfig{Username: "u", SocketDirectory: "/tmp/s", BaseURL: "https://tunnel.example.com/t"},
		}
	})
	listener := &wsClient{send: make(chan []byte, 16), done: make(chan struct{})}
	s.adminClients.add(listener, nil)
	defer s.adminClients.done(listener)
	admin := newAdminCall(t, s)

	status, reply := admin.json("POST", "/api/admin/tunnel/connect", "")
	if status != 500 || !strings.HasPrefix(reply["error"].(string), "Failed to establish SSH tunnel") {
		t.Fatalf("connect: %d %v", status, reply)
	}
	var sawError bool
	for len(listener.send) > 0 {
		var message map[string]any
		if err := json.Unmarshal(<-listener.send, &message); err != nil {
			t.Fatal(err)
		}
		if info, _ := message["tunnel"].(map[string]any); message["type"] == "tunnel_status" && info["connected"] == false && info["error"] != nil {
			sawError = true
		}
	}
	if !sawError {
		t.Error("the admin pages did not hear the failed connection")
	}

	status, reply = admin.json("POST", "/api/admin/tunnel/disconnect", "")
	if status != 200 || reply["success"] != true {
		t.Errorf("disconnect: %d %v", status, reply)
	}

	// A config save gives the tunnel its new settings.
	admin.json("PUT", "/api/admin/config", `{"data": {"tunnel": {"server": "other.example.com", "socket_name": "room-1"}}}`)
	if info := s.tunnelInfo(s.tunnel.Status()); info["server"] != "other.example.com" || info["socket_name"] != "room-1" {
		t.Errorf("tunnel info after the save: %v", info)
	}
}
