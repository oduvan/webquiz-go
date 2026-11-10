package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"encoding/json"
)

// Results of Python 3.13's ipaddress (is_private, is_loopback), which the
// Python guards used.
var pythonAddresses = []struct {
	ip                string
	private, loopback bool
}{
	{"127.0.0.1", true, true},
	{"127.255.255.254", true, true},
	{"10.0.0.1", true, false},
	{"10.255.255.255", true, false},
	{"11.0.0.1", false, false},
	{"172.15.255.255", false, false},
	{"172.16.0.1", true, false},
	{"172.31.255.255", true, false},
	{"172.32.0.1", false, false},
	{"192.168.0.1", true, false},
	{"192.169.0.1", false, false},
	{"169.254.1.1", true, false},
	{"100.64.0.1", false, false},
	{"100.127.255.255", false, false},
	{"0.0.0.0", true, false},
	{"0.1.2.3", true, false},
	{"192.0.0.1", true, false},
	{"192.0.0.9", false, false},
	{"192.0.0.10", false, false},
	{"192.0.0.170", true, false},
	{"192.0.0.171", true, false},
	{"192.0.0.172", true, false},
	{"192.0.2.5", true, false},
	{"198.18.0.1", true, false},
	{"198.19.255.255", true, false},
	{"198.20.0.1", false, false},
	{"198.51.100.7", true, false},
	{"203.0.113.9", true, false},
	{"240.0.0.1", true, false},
	{"255.255.255.255", true, false},
	{"224.0.0.1", false, false},
	{"8.8.8.8", false, false},
	{"1.1.1.1", false, false},
	{"93.184.216.34", false, false},
	{"::1", true, true},
	{"::", true, false},
	{"::2", false, false},
	{"::ffff:127.0.0.1", true, true},
	{"::ffff:10.1.2.3", true, false},
	{"::ffff:8.8.8.8", false, false},
	{"::ffff:192.0.0.9", false, false},
	{"64:ff9b::1", false, false},
	{"64:ff9b:1::1", true, false},
	{"100::1", true, false},
	{"100::1:0:0:0:1", false, false},
	{"2001::1", true, false},
	{"2001:1::1", false, false},
	{"2001:1::2", false, false},
	{"2001:1::3", true, false},
	{"2001:2::1", true, false},
	{"2001:3::1", false, false},
	{"2001:4:112::1", false, false},
	{"2001:4:113::1", true, false},
	{"2001:10::1", true, false},
	{"2001:20::1", false, false},
	{"2001:30::1", false, false},
	{"2001:1ff::1", true, false},
	{"2001:200::1", false, false},
	{"2001:db8::1", true, false},
	{"2001:db9::1", false, false},
	{"2002::1", true, false},
	{"3fff::1", true, false},
	{"3fff:1000::1", false, false},
	{"fc00::1", true, false},
	{"fdff::1", true, false},
	{"fe80::1", true, false},
	{"fe80::1%eth0", true, false},
	{"febf::1", true, false},
	{"fec0::1", false, false},
	{"ff02::1", false, false},
	{"2a00:1450:4001::1", false, false},
	{"2606:4700::1111", false, false},
}

func TestIPRulesMatchPython(t *testing.T) {
	for _, c := range pythonAddresses {
		addr, err := netip.ParseAddr(c.ip)
		if err != nil {
			t.Errorf("%s: %v", c.ip, err)
			continue
		}
		if got := isPrivateAddress(addr); got != c.private {
			t.Errorf("isPrivateAddress(%s) = %v, Python says %v", c.ip, got, c.private)
		}
		if got := isLoopbackAddress(c.ip); got != c.loopback {
			t.Errorf("isLoopbackAddress(%s) = %v, Python says %v", c.ip, got, c.loopback)
		}
	}
	// Text that is not an address counts as loopback, as in Python (to be safe).
	for _, bad := range []string{"", "not-an-ip", "1.2.3.4:80", " 127.0.0.1", "010.0.0.1"} {
		if !isLoopbackAddress(bad) {
			t.Errorf("isLoopbackAddress(%q) = false, want true", bad)
		}
	}
}

func requestFrom(remote string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	return r
}

// get_client_ip: proxy headers count only when the connection comes from
// loopback; the last X-Forwarded-For entry wins over X-Real-IP.
func TestGetClientIP(t *testing.T) {
	cases := []struct {
		remote  string
		headers map[string]string
		want    string
	}{
		{"127.0.0.1:5000", map[string]string{"X-Forwarded-For": "127.0.0.1, 8.8.8.8"}, "8.8.8.8"},
		{"127.0.0.1:5000", map[string]string{"X-Forwarded-For": "8.8.8.8", "X-Real-IP": "192.168.1.5"}, "8.8.8.8"},
		{"127.0.0.1:5000", map[string]string{"X-Real-IP": " 8.8.4.4 "}, "8.8.4.4"},
		{"192.168.1.20:5000", map[string]string{"X-Forwarded-For": "127.0.0.1", "X-Real-IP": "127.0.0.1"}, "192.168.1.20"},
		{"127.0.0.1:5000", nil, "127.0.0.1"},
		{"127.0.0.1:5000", map[string]string{"X-Forwarded-For": " , "}, "127.0.0.1"},
		{"127.0.0.1:5000", map[string]string{"X-Forwarded-For": " , ", "X-Real-IP": "  "}, "127.0.0.1"},
		{"[::1]:5000", map[string]string{"X-Forwarded-For": "10.0.0.8"}, "10.0.0.8"},
		{"[2001:db8::5]:5000", map[string]string{"X-Forwarded-For": "8.8.8.8"}, "2001:db8::5"},
		{"127.0.0.1", map[string]string{"X-Real-IP": "8.8.8.8"}, "8.8.8.8"}, // no port, like Python's request.remote
		{"", nil, "127.0.0.1"},
	}
	for _, c := range cases {
		if got := getClientIP(requestFrom(c.remote, c.headers)); got != c.want {
			t.Errorf("getClientIP(%s, %v) = %q, want %q", c.remote, c.headers, got, c.want)
		}
	}
}

func testServerForMiddleware(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	s := &Server{log: slog.New(newPyLogHandler(&lockedWriter{w: &logs}, "webquiz.server"))}
	return s, &logs
}

// error_middleware: an error or a panic in a handler becomes a 500 with
// {"error": text}, and the log says what happened.
func TestErrorsBecome500(t *testing.T) {
	s, logs := testServerForMiddleware(t)
	cases := []struct {
		name    string
		handler handlerFunc
		text    string
	}{
		{"error", func(w http.ResponseWriter, r *http.Request) error { return errors.New("something broke") }, "something broke"},
		{"key error", func(w http.ResponseWriter, r *http.Request) error { return keyError("username") }, "'username'"},
		{"panic", func(w http.ResponseWriter, r *http.Request) error { panic("deep trouble") }, "deep trouble"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.serveErrors(c.handler).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/x", nil))
		if rec.Code != 500 || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s: %d %q", c.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != c.text {
			t.Errorf("%s: body %s", c.name, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "Error handling POST /api/x from") || !strings.Contains(logs.String(), c.text) {
			t.Errorf("%s: log %s", c.name, logs.String())
		}
	}

	// An error with a status (a body that is too big) keeps its status.
	rec := httptest.NewRecorder()
	s.serveErrors(func(w http.ResponseWriter, r *http.Request) error {
		_, err := readJSON(r)
		return err
	}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/x", strings.NewReader(`"`+strings.Repeat("x", maxBodySize)+`"`)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body: %d %s", rec.Code, rec.Body.String())
	}
}

// Request JSON keeps numbers as written (UseNumber), so 1 and 1.0 differ as
// in Python; anything after the value is an error like json.loads' "Extra data".
func TestReadJSON(t *testing.T) {
	read := func(body string) (any, error) {
		return readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	}
	value, err := read(`{"a": 1, "b": 1.0, "c": [1e3, -0]}`)
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	if m["a"] != json.Number("1") || m["b"] != json.Number("1.0") || m["c"].([]any)[0] != json.Number("1e3") {
		t.Errorf("numbers changed: %#v", m)
	}
	for _, bad := range []string{"", "   ", "{", `{"a": 1} x`, `{"a": 1}{"b": 2}`, "nope"} {
		if _, err := read(bad); err == nil {
			t.Errorf("readJSON(%q) must fail", bad)
		}
	}
	if value, err := read(" [1, 2] \n"); err != nil || len(value.([]any)) != 2 {
		t.Errorf("a list with spaces around it: %v %v", value, err)
	}
}

func TestWriteJSONKeepsTextAsWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeJSON(rec, 201, map[string]any{"text": "<b> & Ім'я"}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 201 || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := rec.Body.String(); got != `{"text":"<b> & Ім'я"}` {
		t.Errorf("body %s", got)
	}
}

func TestPyObject(t *testing.T) {
	if m, err := pyObject(map[string]any{"a": 1}); err != nil || m["a"] != 1 {
		t.Errorf("a map: %v %v", m, err)
	}
	for value, text := range map[string]string{
		`[1]`:  "'list' object has no attribute 'get'",
		`"x"`:  "'str' object has no attribute 'get'",
		`3`:    "'int' object has no attribute 'get'",
		`null`: "'NoneType' object has no attribute 'get'",
	} {
		decoded, _ := decodeJSON([]byte(value))
		if _, err := pyObject(decoded); err == nil || err.Error() != text {
			t.Errorf("pyObject(%s) error %v, want %q", value, err, text)
		}
	}
}
