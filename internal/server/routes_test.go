package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// route is one route of create_app (server.py of WebQuiz 1.19) with the
// guard of its Python handler: "public", "local" (local_network_only) or
// "admin" (admin_auth_required). The paths carry sample values for the
// {parameters}. This list is written from the Python code, not from the Go
// route table, so the test finds a route the Go server forgot.
type route struct {
	method, path, guard string
}

var pythonRoutes = []route{
	{"POST", "/api/register", "public"},
	{"PUT", "/api/update-registration", "public"},
	{"POST", "/api/submit-answer", "public"},
	{"POST", "/api/question-start", "public"},
	{"GET", "/api/verify-user/123456", "public"},

	{"GET", "/admin/", "local"},
	{"POST", "/api/admin/auth", "local"},
	{"GET", "/api/admin/check-session", "local"},
	{"GET", "/api/admin/version-check", "local"},
	{"PUT", "/api/admin/approve-user", "admin"},
	{"POST", "/api/admin/force-show-answers", "admin"},
	{"GET", "/api/admin/list-quizzes", "admin"},
	{"POST", "/api/admin/switch-quiz", "admin"},
	{"GET", "/api/admin/quiz/test_quiz.yaml", "admin"},
	{"POST", "/api/admin/preview-explanation", "admin"},
	{"POST", "/api/admin/create-quiz", "admin"},
	{"PUT", "/api/admin/quiz/test_quiz.yaml", "admin"},
	{"DELETE", "/api/admin/quiz/test_quiz.yaml", "admin"},
	{"POST", "/api/admin/unite-quizzes", "admin"},
	{"POST", "/api/admin/validate-quiz", "admin"},
	{"GET", "/api/admin/list-images", "admin"},
	{"GET", "/api/admin/list-files", "admin"},
	{"GET", "/api/admin/list-checker-templates", "admin"},
	{"POST", "/api/admin/download-quiz", "admin"},
	{"PUT", "/api/admin/config", "admin"},

	{"POST", "/api/admin/tunnel/connect", "admin"},
	{"POST", "/api/admin/tunnel/disconnect", "admin"},

	{"GET", "/api/admin/transfer", "admin"},
	{"POST", "/api/admin/transfer/send", "admin"},
	{"POST", "/api/admin/transfer/abc123/accept", "admin"},
	{"POST", "/api/admin/transfer/abc123/reject", "admin"},
	{"POST", "/api/transfer/offer", "public"},
	{"GET", "/api/transfer/download/token123/quizzes/test_quiz.yaml", "public"},
	{"POST", "/api/transfer/result", "public"},

	{"GET", "/ws/admin", "local"},

	{"GET", "/files/", "local"},
	{"GET", "/files/csv/answers.csv", "local"},
	{"GET", "/attach/data.txt", "public"},
	{"GET", "/api/files/list", "admin"},
	{"GET", "/api/files/logs/view/0001.log", "admin"},
	{"GET", "/api/files/logs/download/0001.log", "admin"},
	{"PUT", "/api/files/quizzes/save/test_quiz.yaml", "admin"},

	{"GET", "/live-stats/", "local"},
	{"GET", "/ws/live-stats", "local"},

	{"GET", "/", "public"},
}

// startForRoutes starts a server with an attachment file, so that the real
// /attach/ handler of a later task finds it.
func startForRoutes(t *testing.T, opts testutil.Options) *testutil.TestServer {
	t.Helper()
	ts := testutil.StartServer(t, opts)
	attach := filepath.Join(ts.QuizzesDir, "attach", "data.txt")
	if err := os.WriteFile(attach, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ts
}

// A route that exists answers neither 405 (wrong method) nor the 404 of the
// static fallback. A handler may still answer 404 itself, with a JSON error
// (an unknown transfer token, say). Routes that a later task builds answer
// 501 with {"error": "not implemented"}.
func TestEveryPythonRouteIsServed(t *testing.T) {
	t.Parallel()
	for _, r := range pythonRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			t.Parallel()
			ts := startForRoutes(t, testutil.Options{})
			cookies := ts.AdminCookies(t, "test123")
			res := ts.Do(t, r.method, r.path, nil, testutil.WithCookies(cookies...))
			if res.Status == http.StatusMethodNotAllowed {
				t.Fatalf("405: the route is missing for this method (Allow: %s)", res.Header.Get("Allow"))
			}
			if res.Status == http.StatusNotFound && !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
				t.Fatalf("404 from the static fallback: the route is missing: %s", res.Body)
			}
			if res.Status == http.StatusNotImplemented {
				if got := res.JSON(t)["error"]; got != "not implemented" {
					t.Fatalf("a stub must answer {\"error\": \"not implemented\"}, got %s", res.Body)
				}
			}
		})
	}
}

// Like aiohttp with its static route at "/": GET on a path without a GET
// route asks the static folder (404), any other method on any path without
// that method gets 405 with the allowed methods.
func TestUnknownPathsAndWrongMethods(t *testing.T) {
	t.Parallel()
	ts := startForRoutes(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	cases := []struct {
		method, path string
		status       int
		allow        []string // methods the Allow header must list for a 405
	}{
		{"GET", "/api/nothing", 404, nil},
		{"POST", "/api/nothing", 405, []string{"GET"}},
		{"GET", "/api/register", 404, nil}, // POST-only route: GET goes to the static folder
		{"POST", "/api/verify-user/123456", 405, []string{"GET"}},
		{"POST", "/", 405, []string{"GET"}},
		{"PATCH", "/api/admin/quiz/test_quiz.yaml", 405, []string{"GET", "PUT", "DELETE"}},
		{"POST", "/api/admin/list-quizzes", 405, []string{"GET"}},
		{"GET", "/api/verify-user/", 404, nil},
		{"GET", "/files/csv/a/b.csv", 404, nil},
	}
	for _, c := range cases {
		res := ts.Do(t, c.method, c.path, nil, testutil.WithCookies(cookies...))
		if res.Status != c.status {
			t.Errorf("%s %s: status %d, want %d (%s)", c.method, c.path, res.Status, c.status, res.Body)
			continue
		}
		allow := res.Header.Get("Allow")
		for _, method := range c.allow {
			if !strings.Contains(allow, method) {
				t.Errorf("%s %s: Allow %q does not list %s", c.method, c.path, allow, method)
			}
		}
	}
}

// Go's router sends /admin to /admin/ (also /files and /live-stats). aiohttp
// answered 404 there; the redirect makes the "Admin panel" address that the
// CLI prints (without the slash) work.
func TestPagesWithoutSlashRedirect(t *testing.T) {
	t.Parallel()
	ts := startForRoutes(t, testutil.Options{})
	for _, path := range []string{"/admin", "/files", "/live-stats"} {
		res := ts.NoRedirects().GetJSON(t, path)
		if res.Status < 300 || res.Status > 399 || res.Header.Get("Location") != path+"/" {
			t.Errorf("GET %s: %d Location %q, want a redirect to %s/", path, res.Status, res.Header.Get("Location"), path)
		}
	}
}

func routesWithGuard(guard string) []route {
	var routes []route
	for _, r := range pythonRoutes {
		if r.guard == guard {
			routes = append(routes, r)
		}
	}
	return routes
}

func errorText(t *testing.T, res *testutil.Response) any {
	t.Helper()
	return res.JSON(t)["error"]
}

// admin_auth_required without a session cookie: 401 with the translated text.
func TestAdminRoutesNeedASession(t *testing.T) {
	t.Parallel()
	for _, language := range []struct{ code, text string }{
		{"uk", "Недійсний або відсутній сеанс"},
		{"en", "Invalid or missing session"},
	} {
		t.Run(language.code, func(t *testing.T) {
			t.Parallel()
			ts := startForRoutes(t, testutil.Options{Config: map[string]any{"language": language.code}})
			for _, r := range routesWithGuard("admin") {
				for _, opts := range [][]testutil.RequestOption{
					nil,
					{testutil.WithHeader("Cookie", "admin_session=not-a-session")},
				} {
					res := ts.Do(t, r.method, r.path, nil, opts...)
					if res.Status != http.StatusUnauthorized || errorText(t, res) != language.text {
						t.Errorf("%s %s: %d %s, want 401 %q", r.method, r.path, res.Status, res.Body, language.text)
					}
				}
			}
		})
	}
}

// Both guards let only private addresses in (Python's ipaddress is_private).
// A public address gets 403 "local network only", text that is not an IP
// address gets 403 "invalid IP address". Public routes stay open.
func TestGuardsBlockClientsOutsideTheLocalNetwork(t *testing.T) {
	t.Parallel()
	ts := startForRoutes(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	const local = "Доступ заборонено: тільки для локальної мережі"
	const badIP = "Доступ заборонено: невірна IP адреса"
	for _, r := range pythonRoutes {
		for _, c := range []struct {
			ip   string
			text string
		}{{"8.8.8.8", local}, {"100.64.0.1", local}, {"not-an-ip", badIP}} {
			res := ts.Do(t, r.method, r.path, nil, testutil.WithCookies(cookies...), testutil.FromIP(c.ip))
			if r.guard == "public" {
				if res.Status == http.StatusForbidden {
					t.Errorf("%s %s from %s: 403 on a public route: %s", r.method, r.path, c.ip, res.Body)
				}
				continue
			}
			if res.Status != http.StatusForbidden || errorText(t, res) != c.text {
				t.Errorf("%s %s from %s: %d %s, want 403 %q", r.method, r.path, c.ip, res.Status, res.Body, c.text)
			}
		}
		for _, ip := range []string{"192.168.1.20", "10.1.2.3", "172.16.5.4", "127.0.0.1", "::1", "fe80::1"} {
			res := ts.Do(t, r.method, r.path, nil, testutil.WithCookies(cookies...), testutil.FromIP(ip))
			if res.Status == http.StatusForbidden || (r.guard == "admin" && res.Status == http.StatusUnauthorized) {
				t.Errorf("%s %s from %s: %d %s, want the route's own answer", r.method, r.path, ip, res.Status, res.Body)
			}
		}
	}
}

// A trusted IP needs no session; the check is an exact text match of the
// address, as in Python ("client_ip in trusted_ips").
func TestTrustedIPNeedsNoSession(t *testing.T) {
	t.Parallel()
	ts := startForRoutes(t, testutil.Options{Config: map[string]any{
		"admin": map[string]any{"trusted_ips": []any{"192.168.1.50"}},
	}})
	for _, r := range routesWithGuard("admin") {
		res := ts.Do(t, r.method, r.path, nil, testutil.FromIP("192.168.1.50"))
		if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
			t.Errorf("%s %s from a trusted IP: %d %s", r.method, r.path, res.Status, res.Body)
		}
		res = ts.Do(t, r.method, r.path, nil, testutil.FromIP("192.168.1.51"))
		if res.Status != http.StatusUnauthorized {
			t.Errorf("%s %s from an untrusted IP: %d, want 401", r.method, r.path, res.Status)
		}
	}
}

// Without a master key the admin routes are off: 403 for everyone except
// trusted IPs. The message has no translation in Python either.
func TestAdminIsOffWithoutMasterKey(t *testing.T) {
	t.Parallel()
	ts := startForRoutes(t, testutil.Options{Config: map[string]any{
		"admin": map[string]any{"master_key": nil, "trusted_ips": []any{"10.0.0.7"}},
	}})
	const off = "Admin functionality disabled - no master key set"
	for _, r := range routesWithGuard("admin") {
		res := ts.Do(t, r.method, r.path, nil)
		if res.Status != http.StatusForbidden || errorText(t, res) != off {
			t.Errorf("%s %s: %d %s, want 403 %q", r.method, r.path, res.Status, res.Body, off)
		}
		res = ts.Do(t, r.method, r.path, nil, testutil.FromIP("10.0.0.7"))
		if res.Status == http.StatusForbidden || res.Status == http.StatusUnauthorized {
			t.Errorf("%s %s from a trusted IP: %d %s", r.method, r.path, res.Status, res.Body)
		}
	}
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "anything"})
	if res.Status != http.StatusForbidden || errorText(t, res) != off {
		t.Errorf("auth without a master key: %d %s", res.Status, res.Body)
	}
}
