package server_test

import (
	"net/http"
	"testing"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func sessionCookie(t *testing.T, res *testutil.Response) *http.Cookie {
	t.Helper()
	for _, cookie := range res.Cookies {
		if cookie.Name == "admin_session" {
			return cookie
		}
	}
	t.Fatalf("no admin_session cookie in %v", res.Header.Values("Set-Cookie"))
	return nil
}

// POST /api/admin/auth with the right key: a session cookie like aiohttp's
// set_cookie(httponly=True, samesite="Strict", path="/").
func TestAdminAuthSetsSessionCookie(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	if res.Status != http.StatusOK {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	data := res.JSON(t)
	if data["authenticated"] != true || data["message"] != "Admin authentication successful" {
		t.Errorf("body %s", res.Body)
	}
	cookie := sessionCookie(t, res)
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie attributes: %+v", cookie)
	}
	if len(cookie.Value) != 43 { // secrets.token_urlsafe(32)
		t.Errorf("session token %q has %d characters, want 43", cookie.Value, len(cookie.Value))
	}

	again := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	if sessionCookie(t, again).Value == cookie.Value {
		t.Error("every login must get a new session token")
	}
}

func TestAdminAuthRejectsWrongKeys(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{"language": "en"}})
	for _, body := range []any{
		map[string]any{"master_key": "wrong"},
		map[string]any{"master_key": ""},
		map[string]any{"master_key": 123},
		map[string]any{"other": "test123"},
		[]any{"test123"},
		"not json",
		nil,
	} {
		res := ts.PostJSON(t, "/api/admin/auth", body)
		if res.Status != http.StatusUnauthorized || res.JSON(t)["error"] != "Invalid or missing master key" {
			t.Errorf("body %v: %d %s", body, res.Status, res.Body)
		}
		for _, cookie := range res.Cookies {
			if cookie.Name == "admin_session" {
				t.Errorf("body %v: got a session cookie", body)
			}
		}
	}
}

// A trusted IP gets a session without the key.
func TestAdminAuthTrustedIPNeedsNoKey(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}},
	}})
	res := ts.PostJSON(t, "/api/admin/auth", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	sessionCookie(t, res)
}

func TestCheckSession(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})

	res := ts.GetJSON(t, "/api/admin/check-session")
	if data := res.JSON(t); res.Status != 401 || data["valid"] != false || data["reason"] != "No session cookie" {
		t.Errorf("no cookie: %d %s", res.Status, res.Body)
	}
	res = ts.GetJSON(t, "/api/admin/check-session", testutil.WithHeader("Cookie", "admin_session=unknown"))
	if data := res.JSON(t); res.Status != 401 || data["valid"] != false || data["reason"] != "Invalid session" {
		t.Errorf("unknown cookie: %d %s", res.Status, res.Body)
	}
	res = ts.GetJSON(t, "/api/admin/check-session", testutil.WithCookies(ts.AdminCookies(t, "test123")...))
	if data := res.JSON(t); res.Status != 200 || data["valid"] != true || data["message"] != "Session is valid" {
		t.Errorf("valid cookie: %d %s", res.Status, res.Body)
	}

	// A client with a cookie jar keeps the session, like the admin's browser.
	admin := ts.AdminClient(t, "test123")
	if res := admin.GetJSON(t, "/api/admin/check-session"); res.Status != 200 {
		t.Errorf("admin client: %d %s", res.Status, res.Body)
	}
}
