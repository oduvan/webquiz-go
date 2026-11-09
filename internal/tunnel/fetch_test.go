package tunnel

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// py: tests/test_tunnel.py::test_fetch_tunnel_config_no_server
func TestFetchTunnelConfigNoServer(t *testing.T) {
	m := newTestManager(t, Config{}, 8080) // no server; any HTTP request fails the test

	got, _ := m.fetchTunnelConfig(context.Background())

	if got != nil {
		t.Errorf("fetchTunnelConfig() = %+v, want nil", got)
	}
}

// py: tests/test_tunnel.py::test_fetch_tunnel_config_success
func TestFetchTunnelConfigSuccess(t *testing.T) {
	web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: https://example.com/tests\n")
	m := newTestManager(t, Config{Server: "example.com"}, 8080)
	m.transport = web.transport

	got, err := m.fetchTunnelConfig(context.Background())

	if err != nil {
		t.Fatalf("fetchTunnelConfig() error = %v", err)
	}
	want := ServerConfig{Username: "tunneluser", SocketDirectory: "/var/run/tunnels", BaseURL: "https://example.com/tests"}
	if got == nil || *got != want {
		t.Errorf("fetchTunnelConfig() = %+v, want %+v", got, want)
	}
	// HTTPS is used for a domain name, and there is one request.
	if requests := web.requestURLs(); !slices.Equal(requests, []string{"https://example.com/tunnel_config.yaml"}) {
		t.Errorf("requests = %q, want one to https://example.com/tunnel_config.yaml", requests)
	}
}

// py: tests/test_tunnel.py::test_fetch_tunnel_config_from_ip_address
func TestFetchTunnelConfigFromIPAddress(t *testing.T) {
	web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: http://192.168.1.100/tests\n")
	m := newTestManager(t, Config{Server: "192.168.1.100"}, 8080)
	m.transport = web.transport

	got, err := m.fetchTunnelConfig(context.Background())

	if err != nil {
		t.Fatalf("fetchTunnelConfig() error = %v", err)
	}
	want := ServerConfig{Username: "tunneluser", SocketDirectory: "/var/run/tunnels", BaseURL: "http://192.168.1.100/tests"}
	if got == nil || *got != want {
		t.Errorf("fetchTunnelConfig() = %+v, want %+v", got, want)
	}
	// HTTP is used for an IP address.
	if requests := web.requestURLs(); !slices.Equal(requests, []string{"http://192.168.1.100/tunnel_config.yaml"}) {
		t.Errorf("requests = %q, want one to http://192.168.1.100/tunnel_config.yaml", requests)
	}
}

// py: tests/test_tunnel.py::test_fetch_tunnel_config_from_ipv6_address
//
// Python built "http://2001:db8::1/tunnel_config.yaml" and its test checked
// that string on a mocked client. Without brackets around the address the
// URL cannot be used: httpx refuses it ("Invalid port: 'db8::1'"), and so does
// Go. CLAUDE.md says IPv6 addresses use HTTP, so the Go version puts the
// brackets in and the request really goes out over HTTP.
func TestFetchTunnelConfigFromIPv6Address(t *testing.T) {
	web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: http://[2001:db8::1]/tests\n")
	m := newTestManager(t, Config{Server: "2001:db8::1"}, 8080)
	m.transport = web.transport

	got, err := m.fetchTunnelConfig(context.Background())

	if err != nil || got == nil {
		t.Fatalf("fetchTunnelConfig() = %+v, %v, want the config", got, err)
	}
	if requests := web.requestURLs(); !slices.Equal(requests, []string{"http://[2001:db8::1]/tunnel_config.yaml"}) {
		t.Errorf("requests = %q, want one to http://[2001:db8::1]/tunnel_config.yaml", requests)
	}
}

// py: tests/test_tunnel.py::test_fetch_tunnel_config_uses_local_config
func TestFetchTunnelConfigUsesLocalConfig(t *testing.T) {
	web := newFakeWeb(t, "username: fromserver\nsocket_directory: /server\nbase_url: https://example.com/server\n")
	local := ServerConfig{Username: "localuser", SocketDirectory: "/local/sockets", BaseURL: "https://local.example.com/app"}
	m := newTestManager(t, Config{Server: "example.com", Config: &local}, 8080)
	m.transport = web.transport

	got, err := m.fetchTunnelConfig(context.Background())

	if err != nil {
		t.Fatalf("fetchTunnelConfig() error = %v", err)
	}
	if got == nil || *got != local {
		t.Errorf("fetchTunnelConfig() = %+v, want the local config %+v", got, local)
	}
	if requests := web.requestURLs(); len(requests) != 0 {
		t.Errorf("requests = %q, want none: the local config is used without asking the server", requests)
	}
}

// py: tests/test_tunnel.py::test_fetch_tunnel_config_missing_fields
func TestFetchTunnelConfigMissingFields(t *testing.T) {
	// socket_directory is missing.
	web := newFakeWeb(t, "username: tunneluser\nbase_url: https://example.com/tests\n")
	m := newTestManager(t, Config{Server: "example.com"}, 8080)
	m.transport = web.transport

	got, _ := m.fetchTunnelConfig(context.Background())

	if got != nil {
		t.Errorf("fetchTunnelConfig() = %+v, want nil", got)
	}
	if st := m.Status(); !strings.Contains(string(st.Error), "socket_directory") {
		t.Errorf("status error = %q, want it to name socket_directory", st.Error)
	}
}

// Not a port: Python had no test for a server that answers with an error.
// httpx did not follow redirects and failed on any status outside 2xx; the
// status error says so, with the code and the URL.
func TestFetchTunnelConfigBadStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		location string
		wantText string
	}{
		{"not found", http.StatusNotFound, "", "Client error '404 Not Found' for url 'https://example.com/tunnel_config.yaml'"},
		{"server error", http.StatusBadGateway, "", "Server error '502 Bad Gateway' for url 'https://example.com/tunnel_config.yaml'"},
		{"redirect is not followed", http.StatusFound, "https://example.com/moved.yaml", "Redirect response '302 Found' for url 'https://example.com/tunnel_config.yaml'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: https://example.com/tests\n")
			web.respond(tc.status, tc.location)
			m := newTestManager(t, Config{Server: "example.com"}, 8080)
			m.transport = web.transport

			got, err := m.fetchTunnelConfig(context.Background())

			if got != nil || err == nil {
				t.Fatalf("fetchTunnelConfig() = %+v, %v, want an error", got, err)
			}
			st := m.Status()
			if !strings.HasPrefix(string(st.Error), "Failed to fetch config from server: "+tc.wantText) {
				t.Errorf("status error = %q, want it to start with %q", st.Error, "Failed to fetch config from server: "+tc.wantText)
			}
			if err.Error() != string(st.Error) {
				t.Errorf("error = %q, want the status error %q", err, st.Error)
			}
			if requests := web.requestURLs(); len(requests) != 1 {
				t.Errorf("requests = %q, want exactly one", requests)
			}
		})
	}
}

// py: tests/test_tunnel.py::test_generate_socket_id
func TestGenerateSocketID(t *testing.T) {
	socketID := generateSocketID()

	if len(socketID) != 6 && len(socketID) != 8 {
		t.Errorf("socket id %q has %d characters, want 6 or 8", socketID, len(socketID))
	}
	if strings.Trim(socketID, "0123456789abcdef") != "" {
		t.Errorf("socket id %q has characters that are not lowercase hex", socketID)
	}
	ids := map[string]bool{}
	for range 10 {
		ids[generateSocketID()] = true
	}
	if len(ids) < 2 {
		t.Errorf("10 socket ids were all the same: %v", ids)
	}
}
