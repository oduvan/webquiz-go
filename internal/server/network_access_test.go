package server_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/oduvan/webquiz-go/internal/server"
	"github.com/oduvan/webquiz-go/internal/testutil"
)

// py: tests/test_network_access.py::test_spoofed_localhost_through_tunnel_is_blocked
func TestSpoofedLocalhostThroughTunnelIsBlocked(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{Config: map[string]any{
		"admin": map[string]any{"trusted_ips": []any{"127.0.0.1"}},
	}})
	headers := []testutil.RequestOption{
		testutil.WithHeader("X-Forwarded-For", "127.0.0.1, 8.8.8.8"),
		testutil.WithHeader("X-Real-IP", "8.8.8.8"),
	}
	if res := ts.GetJSON(t, "/api/files/list", headers...); res.Status != 403 {
		t.Errorf("files list with spoofed header: %d", res.Status)
	}
	if res := ts.PostJSON(t, "/api/admin/auth", nil, headers...); res.Status != 403 {
		t.Errorf("auth with spoofed header: %d", res.Status)
	}
	// The real localhost (no proxy headers) is still trusted. Python checked
	// for 200; the file list handler is written in another task (it answers
	// 501 until then), so this checks that the guard lets the request in.
	if res := ts.GetJSON(t, "/api/files/list"); res.Status == 403 {
		t.Errorf("files list from localhost: %d", res.Status)
	}
}

func assertHTML(t *testing.T, res *testutil.Response, what string) {
	t.Helper()
	if res.Status != 200 || !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("%s: %d %s", what, res.Status, res.Header.Get("Content-Type"))
	}
}

// assertLocalNetworkError checks a 403 with {"error": ...}; with text, the
// error also says what the Ukrainian message says.
func assertForbidden(t *testing.T, res *testutil.Response, what, text string) {
	t.Helper()
	if res.Status != 403 {
		t.Errorf("%s: status %d, want 403", what, res.Status)
		return
	}
	message, ok := res.JSON(t)["error"].(string)
	if !ok {
		t.Errorf("%s: no error field: %s", what, res.Body)
	} else if text != "" && !strings.Contains(message, text) {
		t.Errorf("%s: error %q has no %q", what, message, text)
	}
}

// py: tests/test_network_access.py::test_admin_page_from_local_ip
func TestAdminPageFromLocalIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertHTML(t, ts.GetJSON(t, "/admin/"), "admin page from localhost")
}

// py: tests/test_network_access.py::test_admin_page_from_private_ip
func TestAdminPageFromPrivateIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertHTML(t, ts.GetJSON(t, "/admin/", testutil.FromIP("192.168.1.100")), "admin page from 192.168.1.100")
}

// py: tests/test_network_access.py::test_admin_page_from_public_ip
func TestAdminPageFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertForbidden(t, ts.GetJSON(t, "/admin/", testutil.FromIP("8.8.8.8")), "admin page from 8.8.8.8", "локальної мережі")
}

// py: tests/test_network_access.py::test_live_stats_page_from_local_ip
func TestLiveStatsPageFromLocalIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertHTML(t, ts.GetJSON(t, "/live-stats/"), "live stats page from localhost")
}

// py: tests/test_network_access.py::test_live_stats_page_from_public_ip
func TestLiveStatsPageFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertForbidden(t, ts.GetJSON(t, "/live-stats/", testutil.FromIP("1.1.1.1")), "live stats from 1.1.1.1", "")
}

// py: tests/test_network_access.py::test_files_page_from_local_ip
func TestFilesPageFromLocalIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertHTML(t, ts.GetJSON(t, "/files/"), "files page from localhost")
}

// py: tests/test_network_access.py::test_files_page_from_public_ip
func TestFilesPageFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertForbidden(t, ts.GetJSON(t, "/files/", testutil.FromIP("93.184.216.34")), "files page from 93.184.216.34", "")
}

// py: tests/test_network_access.py::test_multiple_private_ip_ranges
func TestMultiplePrivateIPRanges(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.0.1", "192.168.255.254"} {
		if res := ts.GetJSON(t, "/admin/", testutil.FromIP(ip)); res.Status != 200 {
			t.Errorf("access denied for private IP %s: %d", ip, res.Status)
		}
	}
}

// py: tests/test_network_access.py::test_multiple_public_ip_addresses
func TestMultiplePublicIPAddresses(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "151.101.1.195"} {
		if res := ts.GetJSON(t, "/admin/", testutil.FromIP(ip)); res.Status != 403 {
			t.Errorf("access allowed for public IP %s: %d", ip, res.Status)
		}
	}
}

// py: tests/test_network_access.py::test_x_real_ip_header
func TestXRealIPHeader(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	if res := ts.GetJSON(t, "/admin/", testutil.WithHeader("X-Real-IP", "8.8.8.8")); res.Status != 403 {
		t.Errorf("public X-Real-IP: %d", res.Status)
	}
	if res := ts.GetJSON(t, "/admin/", testutil.WithHeader("X-Real-IP", "192.168.1.1")); res.Status != 200 {
		t.Errorf("private X-Real-IP: %d", res.Status)
	}
}

// py: tests/test_network_access.py::test_invalid_ip_format
func TestInvalidIPFormat(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertForbidden(t, ts.GetJSON(t, "/admin/", testutil.FromIP("not-an-ip")), "admin page from not-an-ip", "")
}

// py: tests/test_network_access.py::test_quiz_page_not_restricted
func TestQuizPageNotRestricted(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertHTML(t, ts.GetJSON(t, "/", testutil.FromIP("8.8.8.8")), "quiz page from 8.8.8.8")
}

// py: tests/test_network_access.py::test_admin_api_from_local_ip
func TestAdminAPIFromLocalIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"})
	if res.Status != 200 || res.JSON(t)["authenticated"] != true {
		t.Errorf("auth from localhost: %d %s", res.Status, res.Body)
	}
}

// py: tests/test_network_access.py::test_admin_api_from_public_ip
func TestAdminAPIFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	// The network check comes before the key check.
	res := ts.PostJSON(t, "/api/admin/auth", map[string]any{"master_key": "test123"}, testutil.FromIP("8.8.8.8"))
	assertForbidden(t, res, "auth from 8.8.8.8", "локальної мережі")
}

// py: tests/test_network_access.py::test_admin_list_quizzes_from_public_ip
func TestAdminListQuizzesFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	assertForbidden(t, ts.GetJSON(t, "/api/admin/list-quizzes", testutil.FromIP("1.1.1.1")), "list-quizzes from 1.1.1.1", "")
}

// py: tests/test_network_access.py::test_admin_approve_user_from_public_ip
func TestAdminApproveUserFromPublicIP(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.Put(t, "/api/admin/approve-user", map[string]any{"user_id": "123456"}, testutil.FromIP("93.184.216.34"))
	assertForbidden(t, res, "approve-user from 93.184.216.34", "")
}

// py: tests/test_network_interfaces.py::TestPort80Normalization::test_admin_page_urls_include_port_for_non_80
func TestAdminPageURLsIncludePortForNon80(t *testing.T) {
	t.Parallel()
	// Python took the machine's own addresses. Here the addresses are fixed,
	// so the test also works on a machine without a network.
	ts := testutil.StartServer(t, testutil.Options{ServerOptions: server.Options{
		NetworkInterfaces: func(bool) []string { return []string{"192.168.1.100"} },
	}})
	res := ts.GetJSON(t, "/admin/")
	if res.Status != 200 {
		t.Fatalf("admin page: %d", res.Status)
	}
	urls := jsonText(pageBoot(t, res.Text())["network_info"])
	want := `"quiz_url":"http://192.168.1.100:` + strconv.Itoa(ts.Port) + `/"`
	if !strings.Contains(urls, want) || !strings.Contains(urls, `"label":"Network Access (192.168.1.100)"`) {
		t.Errorf("network_info %s, want %s", urls, want)
	}
}
