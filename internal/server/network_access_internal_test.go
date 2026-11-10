package server

import "testing"

// The five get_client_ip tests of test_network_access.py. Python built a
// stand-in request with a remote address and headers; here it is an
// *http.Request with RemoteAddr (host:port) and headers.

// py: tests/test_network_access.py::test_client_ip_uses_last_forwarded_entry_from_loopback
func TestClientIPUsesLastForwardedEntryFromLoopback(t *testing.T) {
	r := requestFrom("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "127.0.0.1, 8.8.8.8"})
	if got := getClientIP(r); got != "8.8.8.8" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_access.py::test_client_ip_prefers_forwarded_for_over_real_ip
func TestClientIPPrefersForwardedForOverRealIP(t *testing.T) {
	r := requestFrom("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "8.8.8.8", "X-Real-IP": "192.168.1.5"})
	if got := getClientIP(r); got != "8.8.8.8" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_access.py::test_client_ip_uses_real_ip_from_loopback
func TestClientIPUsesRealIPFromLoopback(t *testing.T) {
	r := requestFrom("127.0.0.1:5000", map[string]string{"X-Real-IP": " 8.8.4.4 "})
	if got := getClientIP(r); got != "8.8.4.4" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_access.py::test_client_ip_ignores_headers_from_network_clients
func TestClientIPIgnoresHeadersFromNetworkClients(t *testing.T) {
	r := requestFrom("192.168.1.20:5000", map[string]string{"X-Forwarded-For": "127.0.0.1", "X-Real-IP": "127.0.0.1"})
	if got := getClientIP(r); got != "192.168.1.20" {
		t.Errorf("got %q", got)
	}
}

// py: tests/test_network_access.py::test_client_ip_without_headers
func TestClientIPWithoutHeaders(t *testing.T) {
	if got := getClientIP(requestFrom("127.0.0.1:5000", nil)); got != "127.0.0.1" {
		t.Errorf("no headers: %q", got)
	}
	// Python: request.remote None. Go: no RemoteAddr.
	if got := getClientIP(requestFrom("", nil)); got != "127.0.0.1" {
		t.Errorf("no remote address: %q", got)
	}
	if got := getClientIP(requestFrom("127.0.0.1:5000", map[string]string{"X-Forwarded-For": " , "})); got != "127.0.0.1" {
		t.Errorf("empty forwarded entries: %q", got)
	}
}
