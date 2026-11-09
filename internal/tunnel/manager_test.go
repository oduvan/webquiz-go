package tunnel

import (
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// py: tests/test_tunnel.py::test_tunnel_manager_initialization
func TestManagerInitialization(t *testing.T) {
	cfg := Config{Server: "example.com"}

	m := quiet(t, New(cfg, 8080, nil))

	if !reflect.DeepEqual(m.cfg, cfg) {
		t.Errorf("config = %+v, want %+v", m.cfg, cfg)
	}
	if m.localPort != 8080 {
		t.Errorf("local port = %d, want 8080", m.localPort)
	}
	if m.sess != nil {
		t.Error("a new manager should have no connection and no listener")
	}
	if m.socketID != "" {
		t.Errorf("socket id = %q, want none", m.socketID)
	}
	st := m.Status()
	if st.Connected {
		t.Error("a new manager should not be connected")
	}
	if st.URL != "" {
		t.Errorf("url = %q, want none", st.URL)
	}
	if st.KeysStatus != "unchecked" {
		t.Errorf("keys_status = %q, want %q", st.KeysStatus, "unchecked")
	}
}

// py: tests/test_tunnel.py::test_get_status
func TestGetStatus(t *testing.T) {
	m := quiet(t, New(Config{Server: "example.com"}, 8080, nil))

	st := m.Status()

	// Python returned a dict. The Go status reaches the admin page as JSON,
	// so the check is on the JSON keys.
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"connected", "url", "error", "keys_status"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("status JSON %s has no %q", data, key)
		}
	}
	if fields["connected"] != false {
		t.Errorf("connected = %v, want false", fields["connected"])
	}
}

// py: tests/test_tunnel.py::test_status_callback
func TestStatusCallback(t *testing.T) {
	var reported []Status
	m := quiet(t, New(Config{Server: "example.com"}, 8080, func(s Status) { reported = append(reported, s) }))

	m.notifyStatus() // trigger a status report

	if len(reported) != 1 {
		t.Fatalf("callback called %d times, want 1", len(reported))
	}
	if reported[0].Connected {
		t.Error("reported status says connected, want not connected")
	}
}

// Not a port. Python logged an error from the status callback and went on.
// A panic in the callback is logged, and the manager keeps working.
func TestStatusCallbackPanicIsLogged(t *testing.T) {
	logs := &syncBuffer{}
	m := New(Config{Server: "example.com"}, 8080, func(Status) { panic("broken callback") })
	m.SetLogger(slog.New(slog.NewTextHandler(logs, nil)))

	m.notifyStatus()

	if !strings.Contains(logs.String(), "Error in status callback: broken callback") {
		t.Errorf("the log has no \"Error in status callback: broken callback\":\n%s", logs)
	}
	m.Close() // reports the status again; it must not hang or panic
}

// py: tests/test_tunnel.py::test_public_endpoint_requires_connection
func TestPublicEndpointRequiresConnection(t *testing.T) {
	m := quiet(t, New(Config{Server: "example.com"}, 8080, nil))
	if _, _, ok := m.PublicEndpoint(); ok {
		t.Fatal("a new manager has no public endpoint")
	}

	m.mu.Lock()
	m.socketID = "abc123"
	m.baseURL = "https://example.com/tests"
	m.mu.Unlock()
	if _, _, ok := m.PublicEndpoint(); ok {
		t.Fatal("no public endpoint while the status is not connected (connection lost)")
	}

	m.mu.Lock()
	m.status.Connected = true
	m.mu.Unlock()
	baseURL, socketID, ok := m.PublicEndpoint()
	if !ok || baseURL != "https://example.com/tests" || socketID != "abc123" {
		t.Errorf("PublicEndpoint() = %q, %q, %v, want %q, %q, true",
			baseURL, socketID, ok, "https://example.com/tests", "abc123")
	}
}
