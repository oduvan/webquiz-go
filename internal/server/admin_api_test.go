package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

// Ports of test_admin_api.py and test_admin_session_auth.py. The login and
// check-session handlers belong to Task 6; their own tests are in
// admin_auth_test.go. The tests here run the Python checks through the
// whole admin API.

func login(t *testing.T, ts *testutil.TestServer) []*http.Cookie {
	t.Helper()
	return ts.AdminCookies(t, "test123")
}

// py: tests/test_admin_api.py::test_admin_auth_endpoint_with_valid_key
func TestAdminAuthEndpointWithValidKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	data := res.JSON(t)
	if res.Status != 200 || data["authenticated"] != true || data["message"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_auth_endpoint_without_key
func TestAdminAuthEndpointWithoutKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", nil)
	if res.Status != 401 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_auth_endpoint_with_invalid_key
func TestAdminAuthEndpointWithInvalidKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "wrong_key"})
	if res.Status != 401 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_list_quizzes_endpoint
func TestAdminListQuizzesEndpoint(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/list-quizzes", testutil.WithCookies(login(t, ts)...))
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	quizzes, ok := data["quizzes"].([]any)
	if _, has := data["current_quiz"]; !has || !ok || len(quizzes) == 0 {
		t.Fatalf("body %s", res.Body)
	}
	var found map[string]any
	for _, item := range quizzes {
		entry := item.(map[string]any)
		if _, has := entry["filename"]; !has {
			t.Errorf("entry without filename: %v", entry)
		}
		if _, has := entry["title"]; !has {
			t.Errorf("entry without title: %v", entry)
		}
		if entry["filename"] == "test_quiz.yaml" {
			found = entry
		}
	}
	if found == nil || found["title"] != "Test Quiz" {
		t.Errorf("test_quiz.yaml entry: %v", found)
	}
}

// py: tests/test_admin_api.py::test_admin_list_quizzes_without_title
func TestAdminListQuizzesWithoutTitle(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Quizzes: map[string]any{
		"test_quiz.yaml": map[string]any{
			"title":     "Test Quiz",
			"questions": []any{map[string]any{"question": "What is 2 + 2?", "options": []any{"3", "4", "5"}, "correct_answer": 1}},
		},
		"no_title.yaml": map[string]any{
			"questions": []any{map[string]any{"question": "What is 1 + 1?", "options": []any{"1", "2", "3"}, "correct_answer": 1}},
		},
	}})
	res := ts.GetJSON(t, "/api/admin/list-quizzes", testutil.WithCookies(login(t, ts)...))
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	var found map[string]any
	for _, item := range res.JSON(t)["quizzes"].([]any) {
		if entry := item.(map[string]any); entry["filename"] == "no_title.yaml" {
			found = entry
		}
	}
	if title, has := found["title"]; found == nil || !has || title != nil {
		t.Errorf("no_title.yaml entry: %v", found)
	}
}

// py: tests/test_admin_api.py::test_admin_list_quizzes_without_auth
func TestAdminListQuizzesWithoutAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.GetJSON(t, "/api/admin/list-quizzes"); res.Status != 401 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_switch_quiz_endpoint
func TestAdminSwitchQuizEndpoint(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := testutil.WithCookies(login(t, ts)...)
	if res := ts.GetJSON(t, "/api/admin/list-quizzes", cookies); res.Status != 200 {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	res := ts.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "test_quiz.yaml"}, cookies)
	message, _ := res.JSON(t)["message"].(string)
	if res.Status != 200 || !strings.Contains(message, "test_quiz.yaml") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_switch_quiz_nonexistent_file
func TestAdminSwitchQuizNonexistentFile(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "nonexistent.yaml"},
		testutil.WithCookies(login(t, ts)...))
	if res.Status != 500 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_switch_quiz_without_auth
func TestAdminSwitchQuizWithoutAuth(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.PostJSON(t, "/api/admin/switch-quiz", map[string]any{"quiz_filename": "test_quiz.yaml"}); res.Status != 401 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_interface_webpage
func TestAdminInterfaceWebpage(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/admin/")
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(strings.ToLower(res.Text()), "admin") {
		t.Errorf("%d %s %.200s", res.Status, res.Header.Get("Content-Type"), res.Body)
	}
}

// py: tests/test_admin_api.py::test_admin_endpoints_require_master_key_configuration
func TestAdminEndpointsRequireMasterKeyConfiguration(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"master_key": nil}}})
	if res := ts.PostJSON(t, "/api/admin/auth", nil); res.Status != 403 {
		t.Errorf("auth: %d %s", res.Status, res.Body)
	}
	if res := ts.GetJSON(t, "/api/admin/list-quizzes"); res.Status != 403 {
		t.Errorf("list-quizzes: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_trusted_ip_bypass_authentication
func TestTrustedIPBypassAuthentication(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}}}})
	res := ts.PostJSON(t, "/api/admin/auth", nil)
	if res.Status != 200 || res.JSON(t)["authenticated"] != true {
		t.Fatalf("auth: %d %s", res.Status, res.Body)
	}
	list := ts.GetJSON(t, "/api/admin/list-quizzes", testutil.WithCookies(res.Cookies...))
	if _, has := list.JSON(t)["quizzes"]; list.Status != 200 || !has {
		t.Errorf("list: %d %s", list.Status, list.Body)
	}
}

// py: tests/test_admin_api.py::test_non_trusted_ip_requires_authentication
func TestNonTrustedIPRequiresAuthentication(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"192.168.1.100"}}}})
	if res := ts.PostJSON(t, "/api/admin/auth", nil); res.Status != 401 {
		t.Errorf("without key: %d %s", res.Status, res.Body)
	}
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	if res.Status != 200 || res.JSON(t)["authenticated"] != true {
		t.Errorf("with key: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_api.py::test_trusted_ip_with_proxy_headers
func TestTrustedIPWithProxyHeaders(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"192.168.1.50", "10.0.0.100"}}}})
	for _, c := range []struct {
		header, value string
		status        int
	}{
		{"X-Forwarded-For", "192.168.1.1, 192.168.1.50", 200}, // the proxy appends the real IP last
		{"X-Forwarded-For", "192.168.1.50, 192.168.1.1", 401}, // a trusted IP put first by the client does not count
		{"X-Real-IP", "10.0.0.100", 200},
		{"X-Forwarded-For", "192.168.1.200", 401},
	} {
		res := ts.PostJSON(t, "/api/admin/auth", nil, testutil.WithHeader(c.header, c.value))
		if res.Status != c.status {
			t.Errorf("%s: %s: %d, want %d", c.header, c.value, res.Status, c.status)
		}
	}
}

// py: tests/test_admin_api.py::test_multiple_trusted_ips_configuration
func TestMultipleTrustedIPsConfiguration(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"admin": map[string]any{"trusted_ips": []any{"127.0.0.1", "192.168.1.10", "10.0.0.5"}}}})
	if res := ts.PostJSON(t, "/api/admin/auth", nil); res.Status != 200 {
		t.Errorf("localhost: %d", res.Status)
	}
	if res := ts.PostJSON(t, "/api/admin/auth", nil, testutil.WithHeader("X-Forwarded-For", "192.168.1.10")); res.Status != 200 {
		t.Errorf("forwarded: %d", res.Status)
	}
	if res := ts.PostJSON(t, "/api/admin/auth", nil, testutil.WithHeader("X-Real-IP", "10.0.0.5")); res.Status != 200 {
		t.Errorf("real ip: %d", res.Status)
	}
	// 127.0.0.1 is trusted, but the forwarded address is what counts.
	if res := ts.PostJSON(t, "/api/admin/auth", nil, testutil.WithHeader("X-Forwarded-For", "192.168.1.99")); res.Status != 401 {
		t.Errorf("not trusted: %d", res.Status)
	}
}

// py: tests/test_admin_session_auth.py::test_admin_auth_sets_session_cookie
func TestAdminAuthSetsSessionCookieLong(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	if res.Status != 200 || len(sessionCookie(t, res).Value) == 0 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_session_auth.py::test_check_session_valid_cookie
func TestCheckSessionValidCookie(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/check-session", testutil.WithCookies(login(t, ts)...))
	if res.Status != 200 || res.JSON(t)["valid"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_session_auth.py::test_check_session_no_cookie
func TestCheckSessionNoCookie(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/check-session")
	if res.Status != 401 || res.JSON(t)["valid"] != false {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_session_auth.py::test_check_session_invalid_cookie
func TestCheckSessionInvalidCookie(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/check-session", testutil.WithCookies(&http.Cookie{Name: "admin_session", Value: "invalid_token_12345"}))
	if res.Status != 401 || res.JSON(t)["valid"] != false {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_session_auth.py::test_admin_endpoint_with_session_cookie
func TestAdminEndpointWithSessionCookie(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/admin/list-quizzes", testutil.WithCookies(login(t, ts)...))
	if _, has := res.JSON(t)["quizzes"]; res.Status != 200 || !has {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_admin_session_auth.py::test_session_persists_across_requests
func TestSessionPersistsAcrossRequests(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := testutil.WithCookies(login(t, ts)...)
	for i := 0; i < 3; i++ {
		if res := ts.GetJSON(t, "/api/admin/list-quizzes", cookies); res.Status != 200 {
			t.Fatalf("request %d: %d", i, res.Status)
		}
	}
	if res := ts.GetJSON(t, "/api/admin/check-session", cookies); res.Status != 200 {
		t.Errorf("check-session: %d", res.Status)
	}
}

// py: tests/test_admin_session_auth.py::test_multiple_sessions
func TestMultipleSessions(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	first, second := login(t, ts), login(t, ts)
	if first[0].Value == second[0].Value {
		t.Error("two logins got the same token")
	}
	for i, cookies := range [][]*http.Cookie{first, second} {
		if res := ts.GetJSON(t, "/api/admin/check-session", testutil.WithCookies(cookies...)); res.Status != 200 {
			t.Errorf("session %d: %d", i+1, res.Status)
		}
	}
}

// py: tests/test_admin_session_auth.py::test_session_cookie_for_files_endpoint
//
// /api/files/list is built in Task 10 and answers 501 until then. This test
// checks that the session cookie opens the admin guard of that route (not
// 401 or 403). Task 10 should check the 200 and the "quizzes" key too.
func TestSessionCookieForFilesEndpoint(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.GetJSON(t, "/api/files/list", testutil.WithCookies(login(t, ts)...))
	if res.Status == 401 || res.Status == 403 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
	if res := ts.GetJSON(t, "/api/files/list"); res.Status != 401 {
		t.Errorf("without a cookie: %d", res.Status)
	}
}
