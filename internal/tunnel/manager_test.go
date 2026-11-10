package tunnel

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
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
	// Python checked status["url"] is None. The admin page gets the status as
	// JSON, where None is null.
	expectNull(t, st, "url", "error", "public_key_content")
}

// statusJSON returns the status as the admin page gets it: the JSON text and
// its fields.
func statusJSON(t *testing.T, st Status) ([]byte, map[string]any) {
	t.Helper()
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return data, fields
}

// expectNull checks that the status JSON has each key, with the value null.
func expectNull(t *testing.T, st Status, keys ...string) {
	t.Helper()
	data, fields := statusJSON(t, st)
	for _, key := range keys {
		if value, ok := fields[key]; !ok || value != nil {
			t.Errorf("%s in the status JSON %s is %#v, want null (Python's None)", key, data, value)
		}
	}
}

// py: tests/test_tunnel.py::test_get_status
func TestGetStatus(t *testing.T) {
	m := quiet(t, New(Config{Server: "example.com"}, 8080, nil))

	st := m.Status()

	// Python returned a dict. The Go status reaches the admin page as JSON,
	// so the check is on the JSON keys.
	data, fields := statusJSON(t, st)
	for _, key := range []string{"connected", "url", "error", "keys_status"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("status JSON %s has no %q", data, key)
		}
	}
	if fields["connected"] != false {
		t.Errorf("connected = %v, want false", fields["connected"])
	}
	expectNull(t, st, "url", "error", "public_key_content")
}

// Not a port. A status with values writes them as JSON strings, and only the
// empty ones are null, as Python's None. The server adds its own fields
// (configured, server, socket_name) by embedding Status, and they stay.
func TestStatusJSON(t *testing.T) {
	st := Status{
		Connected:        true,
		URL:              "https://example.com/tests/abc123/",
		KeysStatus:       "ok",
		PublicKeyContent: "ssh-ed25519 AAAA webquiz@tunnel",
	}
	data, err := json.Marshal(st)
	want := `{"connected":true,"url":"https://example.com/tests/abc123/","error":null,` +
		`"keys_status":"ok","public_key_content":"ssh-ed25519 AAAA webquiz@tunnel"}`
	if err != nil || string(data) != want {
		t.Errorf("status JSON = %s, %v, want %s", data, err, want)
	}

	type adminTunnel struct {
		Configured bool `json:"configured"`
		Status
	}
	data, err = json.Marshal(adminTunnel{Configured: true, Status: Status{KeysStatus: "unchecked"}})
	want = `{"configured":true,"connected":false,"url":null,"error":null,` +
		`"keys_status":"unchecked","public_key_content":null}`
	if err != nil || string(data) != want {
		t.Errorf("JSON of a struct that embeds Status = %s, %v, want %s", data, err, want)
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

// Not a port. WithStatus holds back status reports while its function runs:
// a change that happens meanwhile is reported after the function returns.
// The admin WebSocket relies on it to put the status in a new page's first
// message without missing a change that happens at the same moment.
func TestWithStatusHoldsBackReports(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "abc123", 8080)
	srv.failNextDials(1)
	result := make(chan error, 1)

	m.WithStatus(func(status Status) {
		if status.Error != "" {
			t.Errorf("status = %+v, want no error yet", status)
		}
		go func() { result <- m.Connect(context.Background()) }()
		// The failed Connect changes the status and then waits to report it.
		waitForGoroutine(t, "the failed Connect to wait with its report", func(stack string) bool {
			return waitsForLockIn(stack, "tunnel.(*Manager).notifyStatus(")
		})
		if st := m.Status(); !strings.HasPrefix(string(st.Error), "Failed to establish SSH tunnel: ") {
			t.Errorf("status while the report waits = %+v, want the error of the failed Connect", st)
		}
		if got := m.statuses.all(); len(got) != 0 {
			t.Errorf("reports while WithStatus runs: %+v, want none", got)
		}
	})

	select {
	case err := <-result:
		if err == nil {
			t.Error("Connect() succeeded, want the failure the test set up")
		}
	case <-time.After(waitTimeout):
		t.Fatal("Connect still waits after WithStatus returned")
	}
	reports := m.statuses.all()
	if len(reports) != 1 || !strings.HasPrefix(string(reports[0].Error), "Failed to establish SSH tunnel: ") {
		t.Errorf("reports = %+v, want one with the error of the failed Connect", reports)
	}
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
