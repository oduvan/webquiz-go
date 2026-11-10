package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// localServerConfig holds tunnel server settings as the config file can give
// them, so connecting needs no HTTP request. base_url ends with "/" on
// purpose: the public URL must not get a double slash.
var localServerConfig = ServerConfig{
	Username:        "tunneluser",
	SocketDirectory: "/var/run/tunnels",
	BaseURL:         "https://example.com/tests/",
}

// newKeyedManager makes a manager with new keys, whose config gives the
// tunnel server settings itself (localServerConfig). socketName "" means
// random socket names. The tunnel forwards to 127.0.0.1:localPort. It
// returns the manager and its public key line.
func newKeyedManager(t *testing.T, socketName string, localPort int) (*testManager, string) {
	t.Helper()
	publicPath, privatePath := keyPaths(t)
	serverConfig := localServerConfig
	m := newTestManager(t, Config{
		Server:     "example.com",
		PublicKey:  publicPath,
		PrivateKey: privatePath,
		SocketName: socketName,
		Config:     &serverConfig,
	}, localPort)
	publicKey, err := m.EnsureKeys()
	if err != nil {
		t.Fatalf("EnsureKeys() error = %v", err)
	}
	return m, publicKey
}

// newTunnelSetup makes a test SSH server and a keyed manager (see
// newKeyedManager) that connects to it.
func newTunnelSetup(t *testing.T, socketName string, localPort int) (*testManager, *fakeSSHServer) {
	t.Helper()
	srv := newFakeSSHServer(t)
	m, publicKey := newKeyedManager(t, socketName, localPort)
	srv.authorize(t, "tunneluser", publicKey)
	m.dial = srv.dial
	return m, srv
}

// newLocalQuizServer starts an HTTP server on 127.0.0.1 in place of the
// local WebQuiz server and returns its port. Create it before the manager.
// Its replies have no length: the end of the connection ends them.
func newLocalQuizServer(t *testing.T) int {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "quiz page %s", r.URL.Path)
		w.(http.Flusher).Flush() // sends the headers without a Content-Length
	}))
	t.Cleanup(server.Close)
	return server.Listener.Addr().(*net.TCPAddr).Port
}

// get sends an HTTP/1.0 request for path over conn, as the tunnel server's
// nginx does by default, and returns the reply body.
func get(conn net.Conn, path string) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(waitTimeout)); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.0\r\nHost: example.com\r\n\r\n", path); err != nil {
		return "", err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return string(body), err
}

// instantClock stands in for the manager's waits between reconnects. It
// records each wait and returns at once.
type instantClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *instantClock) wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	return ctx.Err()
}

func (c *instantClock) all() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.waits)
}

// stepClock stands in for the manager's waits between reconnects. It sends
// each wait on waits, and the wait lasts until the test calls release or the
// manager stops waiting.
type stepClock struct {
	waits    chan time.Duration
	releases chan struct{}
}

func newStepClock() *stepClock {
	return &stepClock{waits: make(chan time.Duration, 100), releases: make(chan struct{})}
}

func (c *stepClock) wait(ctx context.Context, d time.Duration) error {
	c.waits <- d
	select {
	case <-c.releases:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// next waits until the manager starts its next wait, and returns its length.
func (c *stepClock) next(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.waits:
		return d
	case <-time.After(waitTimeout):
		t.Fatal("the manager did not start waiting to reconnect")
		return 0
	}
}

// release ends the wait the manager is in.
func (c *stepClock) release() { c.releases <- struct{}{} }

// expectNoWait fails the test if the manager starts waiting within d.
func (c *stepClock) expectNoWait(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case wait := <-c.waits:
		t.Errorf("the manager started a reconnect wait of %v, want none", wait)
	case <-time.After(d):
	}
}

func countConnected(statuses []Status) int {
	n := 0
	for _, status := range statuses {
		if status.Connected {
			n++
		}
	}
	return n
}

// py: tests/test_tunnel.py::test_connect_no_server
func TestConnectNoServer(t *testing.T) {
	m := newTestManager(t, Config{}, 8080) // no server

	err := m.Connect(context.Background())

	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not configured") {
		t.Fatalf("Connect() error = %v, want an error that says \"not configured\"", err)
	}
}

// py: tests/test_tunnel.py::test_connect_success
func TestConnectSuccess(t *testing.T) {
	checkGoroutines(t)
	web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: https://example.com/tests\n")
	srv := newFakeSSHServer(t)
	publicPath, privatePath := keyPaths(t)
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)
	// Generate keys first.
	publicKey, err := m.EnsureKeys()
	if err != nil {
		t.Fatalf("EnsureKeys() error = %v", err)
	}
	// The test server accepts only this user with this key, so a login shows
	// that the manager used the private key from the config.
	srv.authorize(t, "tunneluser", publicKey)
	m.transport = web.transport
	m.dial = srv.dial

	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	st := m.Status()
	if !st.Connected {
		t.Error("status is not connected")
	}
	if !strings.HasPrefix(string(st.URL), "https://example.com/tests/") {
		t.Errorf("url = %q, want it to start with https://example.com/tests/", st.URL)
	}
	m.mu.Lock()
	socketID := m.socketID
	m.mu.Unlock()
	if socketID == "" {
		t.Fatal("no socket id")
	}
	// With no socket_name in the config, the name is random: 6 or 8 hex characters.
	if (len(socketID) != 6 && len(socketID) != 8) || strings.Trim(socketID, "0123456789abcdef") != "" {
		t.Errorf("socket id = %q, want 6 or 8 lowercase hex characters", socketID)
	}
	if string(st.URL) != "https://example.com/tests/"+socketID+"/" {
		t.Errorf("url = %q, want https://example.com/tests/%s/", st.URL, socketID)
	}
	// One SSH connection, to the server of the config, as the user named in
	// tunnel_config.yaml, listening on a socket in its socket_directory.
	if got := srv.dialedAddrs(); !slices.Equal(got, []string{"example.com:22"}) {
		t.Errorf("dialed %q, want one dial to example.com:22", got)
	}
	if got := srv.loginUsers(); !slices.Equal(got, []string{"tunneluser"}) {
		t.Errorf("logins %q, want one login as tunneluser", got)
	}
	if got := srv.forwardPaths(); !slices.Equal(got, []string{"/var/run/tunnels/" + socketID}) {
		t.Errorf("forward requests %q, want /var/run/tunnels/%s", got, socketID)
	}
	if got := m.statuses.all(); len(got) != 1 || got[0] != st {
		t.Errorf("status reports %+v, want one report of %+v", got, st)
	}
}

// py: tests/test_tunnel.py::test_connect_with_custom_socket_name
func TestConnectWithCustomSocketName(t *testing.T) {
	checkGoroutines(t)
	web := newFakeWeb(t, "username: tunneluser\nsocket_directory: /var/run/tunnels\nbase_url: https://example.com/tests\n")
	srv := newFakeSSHServer(t)
	publicPath, privatePath := keyPaths(t)
	m := newTestManager(t, Config{
		Server:     "example.com",
		PublicKey:  publicPath,
		PrivateKey: privatePath,
		SocketName: "my-custom-socket",
	}, 8080)
	// Generate keys first.
	publicKey, err := m.EnsureKeys()
	if err != nil {
		t.Fatalf("EnsureKeys() error = %v", err)
	}
	srv.authorize(t, "tunneluser", publicKey)
	m.transport = web.transport
	m.dial = srv.dial

	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// The custom socket name is used in the remote socket path.
	if got := srv.forwardPaths(); !slices.Equal(got, []string{"/var/run/tunnels/my-custom-socket"}) {
		t.Errorf("forward requests %q, want /var/run/tunnels/my-custom-socket", got)
	}
	st := m.Status()
	if !st.Connected || st.URL != "https://example.com/tests/my-custom-socket/" {
		t.Errorf("status = %+v, want connected at https://example.com/tests/my-custom-socket/", st)
	}
	m.mu.Lock()
	socketID, baseURL := m.socketID, m.baseURL
	m.mu.Unlock()
	if socketID != "my-custom-socket" || baseURL != "https://example.com/tests" {
		t.Errorf("socket id, base url = %q, %q, want my-custom-socket, https://example.com/tests", socketID, baseURL)
	}
	gotBase, gotID, ok := m.PublicEndpoint()
	if !ok || gotBase != "https://example.com/tests" || gotID != "my-custom-socket" {
		t.Errorf("PublicEndpoint() = %q, %q, %v, want https://example.com/tests, my-custom-socket, true", gotBase, gotID, ok)
	}
	if got := srv.dialedAddrs(); !slices.Equal(got, []string{"example.com:22"}) {
		t.Errorf("dialed %q, want one dial to example.com:22", got)
	}
	if got := srv.loginUsers(); !slices.Equal(got, []string{"tunneluser"}) {
		t.Errorf("logins %q, want one login as tunneluser", got)
	}
}

// Not a port: Python mocked the SSH library, so no Python test sent data
// through the tunnel. Visitors of the socket on the tunnel server reach the
// local WebQuiz port, several at once, and get its replies.
func TestForwardReachesLocalPort(t *testing.T) {
	checkGoroutines(t)
	port := newLocalQuizServer(t)
	m, srv := newTunnelSetup(t, "abc123", port)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	var wg sync.WaitGroup
	for _, path := range []string{"/one", "/two", "/three"} {
		conn := srv.dialSocket(t, "/var/run/tunnels/abc123")
		wg.Go(func() {
			body, err := get(conn, path)
			if err != nil {
				t.Errorf("GET %s through the tunnel: %v", path, err)
				return
			}
			if body != "quiz page "+path {
				t.Errorf("GET %s through the tunnel = %q, want %q", path, body, "quiz page "+path)
			}
		})
	}
	wg.Wait()
}

// Not a port: asyncssh passed on the end of data in both directions. When a
// visitor stops sending, the local server sees the end of the request; when
// the local server's reply ends, the visitor sees that too.
func TestForwardPassesOnEndOfData(t *testing.T) {
	checkGoroutines(t)
	// This local server answers only after the end of the request.
	local := newTCPServer(t, func(conn net.Conn) {
		defer conn.Close()
		request, _ := io.ReadAll(conn)
		_, _ = conn.Write(bytes.ToUpper(request))
	})
	m, srv := newTunnelSetup(t, "abc123", local.port())
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	visitor := srv.dialSocket(t, "/var/run/tunnels/abc123").(*net.UnixConn)
	if err := visitor.SetDeadline(time.Now().Add(waitTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(visitor, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := visitor.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(visitor)

	if err != nil || string(reply) != "HELLO" {
		t.Errorf("reply = %q, %v, want \"HELLO\" and the end of the connection", reply, err)
	}
}

// Not a port. A forwarded connection that is still open when the tunnel
// closes is ended too, even when the local server keeps its side open, so
// Disconnect does not wait for it.
func TestDisconnectEndsOpenForwards(t *testing.T) {
	checkGoroutines(t)
	local := newTCPServer(t, keepOpen)
	m, srv := newTunnelSetup(t, "abc123", local.port())
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	visitor := srv.dialSocket(t, "/var/run/tunnels/abc123")
	if _, err := io.WriteString(visitor, "hello"); err != nil {
		t.Fatal(err)
	}
	local.nextConn(t) // the visitor's connection reached the local server

	disconnected := make(chan struct{})
	go func() {
		m.Disconnect()
		close(disconnected)
	}()

	select {
	case <-disconnected:
	case <-time.After(waitTimeout):
		t.Fatal("Disconnect waits for the open forwarded connection")
	}
}

// Not a port. A tunnel server that takes the TCP connection but never
// answers does not keep Connect waiting: the login gives up after
// loginTimeout (asyncssh's login_timeout, 120 seconds by default).
func TestConnectGivesUpAfterLoginTimeout(t *testing.T) {
	checkGoroutines(t)
	silent := newTCPServer(t, keepOpen)
	m, _ := newKeyedManager(t, "", 8080)
	m.dial = silent.dial
	m.loginTimeout = 100 * time.Millisecond

	result := make(chan error, 1)
	go func() { result <- m.Connect(context.Background()) }()

	select {
	case err := <-result:
		if err == nil || !strings.HasPrefix(err.Error(), "Failed to establish SSH tunnel: ") || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Connect() error = %v, want \"Failed to establish SSH tunnel: context deadline exceeded\"", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Connect still waits for the silent server")
	}
	if st := m.Status(); st.Connected || !strings.HasPrefix(string(st.Error), "Failed to establish SSH tunnel: ") {
		t.Errorf("status = %+v, want not connected with the error", st)
	}
}

// Not a port. Disconnect while Connect is still logging in stops the
// attempt: Connect returns at once with an error, and the status says
// disconnected. The stopped attempt is not reported as a failure.
func TestDisconnectStopsConnecting(t *testing.T) {
	checkGoroutines(t)
	silent := newTCPServer(t, keepOpen)
	m, _ := newKeyedManager(t, "", 8080)
	m.dial = silent.dial
	result := make(chan error, 1)
	go func() { result <- m.Connect(context.Background()) }()
	silent.nextConn(t) // the manager is in the SSH login

	m.Disconnect()

	select {
	case err := <-result:
		if err == nil {
			t.Error("Connect() succeeded, want an error")
		}
	case <-time.After(waitTimeout):
		t.Fatal("Connect still waits after Disconnect")
	}
	st := m.Status()
	if st.Connected || st.URL != "" || st.Error != "" {
		t.Errorf("status = %+v, want disconnected with no error", st)
	}
	if got := m.statuses.all(); len(got) != 1 || got[0] != st {
		t.Errorf("status reports %+v, want only the report of Disconnect", got)
	}
}

// Not a port. A run can be stopped (by Disconnect, Close or another Connect)
// just after its SSH connection opened. Connect then closes that connection
// without serving it. Visitors that reach the socket in that moment must not
// keep the close waiting. The SSH library keeps one new connection for the
// listener; with a second one it waits, and holds a lock that closing the
// listener needs too, until someone takes the first one. Before the fix
// nobody took it, so Connect, Disconnect and everything after them (config
// saves, shutdown) waited forever.
func TestStoppedRunClosesWithWaitingVisitors(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "abc123", 8080)
	opened := make(chan struct{})
	proceed := make(chan struct{})
	m.afterOpenForTests = func() {
		close(opened)
		<-proceed
	}
	result := make(chan error, 1)
	go func() { result <- m.Connect(context.Background()) }()
	select {
	case <-opened:
	case <-time.After(waitTimeout):
		close(proceed)
		t.Fatal("Connect did not open the SSH connection")
	}

	// Two visitors reach the socket.
	srv.dialSocket(t, "/var/run/tunnels/abc123")
	srv.dialSocket(t, "/var/run/tunnels/abc123")
	waitForGoroutine(t, "the SSH library to wait with the second visitor", func(stack string) bool {
		header, _, _ := strings.Cut(stack, "\n")
		// The name of the library function that waits. If a new version of
		// golang.org/x/crypto renames it, check whether the library still
		// waits like this before you change the name here.
		return strings.Contains(header, "[chan send") && strings.Contains(stack, "ssh.(*forwardList).forward(")
	})

	// Disconnect stops the run before Connect checks that its run is current.
	disconnected := make(chan struct{})
	go func() {
		m.Disconnect()
		close(disconnected)
	}()
	waitUntil(t, "Disconnect to stop the run", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.run == nil
	})
	close(proceed)

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Connect() error = %v, want the error of a stopped run", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Connect still waits while it closes the stopped run's connection")
	}
	select {
	case <-disconnected:
	case <-time.After(waitTimeout):
		t.Fatal("Disconnect still waits")
	}
	if got, want := srv.cancelPaths(), []string{"/var/run/tunnels/abc123"}; !slices.Equal(got, want) {
		t.Errorf("the client stopped listening on %q, want %q", got, want)
	}
	if st := m.Status(); st.Connected || st.URL != "" || st.Error != "" {
		t.Errorf("status = %+v, want disconnected with no error", st)
	}
}

// Not a port. When the SSH connection fails, Connect returns the reason, the
// status shows it and the callback reports it, as Python's connect() did.
// After a failed first Connect the manager does not keep trying: Python's
// _should_be_connected was still false.
func TestConnectFailureReportsError(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	clock := newStepClock()
	m.wait = clock.wait
	srv.failNextDials(1)

	err := m.Connect(context.Background())

	if err == nil || !strings.HasPrefix(err.Error(), "Failed to establish SSH tunnel: ") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Connect() error = %v, want \"Failed to establish SSH tunnel: ...connection refused\"", err)
	}
	st := m.Status()
	if st.Connected || st.URL != "" || string(st.Error) != err.Error() {
		t.Errorf("status = %+v, want not connected with error %q", st, err)
	}
	if got := m.statuses.all(); len(got) != 1 || got[0] != st {
		t.Errorf("status reports %+v, want one report of %+v", got, st)
	}
	if _, _, ok := m.PublicEndpoint(); ok {
		t.Error("PublicEndpoint() is ok after a failed Connect")
	}
	clock.expectNoWait(t, 200*time.Millisecond)
}

// py: tests/test_tunnel.py::test_disconnect
func TestDisconnect(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "abc123", 8080)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if baseURL, socketID, ok := m.PublicEndpoint(); !ok || baseURL != "https://example.com/tests" || socketID != "abc123" {
		t.Fatalf("PublicEndpoint() = %q, %q, %v, want https://example.com/tests, abc123, true", baseURL, socketID, ok)
	}

	m.Disconnect()

	m.mu.Lock()
	sess, socketID, baseURL := m.sess, m.socketID, m.baseURL
	m.mu.Unlock()
	if sess != nil {
		t.Error("the connection and the listener are still there")
	}
	if st := m.Status(); st.Connected || st.URL != "" {
		t.Errorf("status = %+v, want not connected and no url", st)
	}
	if socketID != "" || baseURL != "" {
		t.Errorf("socket id, base url = %q, %q, want none", socketID, baseURL)
	}
	if _, _, ok := m.PublicEndpoint(); ok {
		t.Error("PublicEndpoint() is ok after Disconnect")
	}
	// Python checked that listener.close() and connection.close() were each
	// called once. The test server sees what they do: one request to stop
	// listening on the socket, and the end of the connection.
	if got := srv.cancelPaths(); !slices.Equal(got, []string{"/var/run/tunnels/abc123"}) {
		t.Errorf("cancel requests %q, want one for /var/run/tunnels/abc123", got)
	}
	srv.waitEnded(t, 1)
	if conn, err := net.Dial("unix", srv.socketFor("/var/run/tunnels/abc123")); err == nil {
		conn.Close()
		t.Error("the socket on the tunnel server still takes visitors")
	}
	if got := m.statuses.all(); len(got) != 2 || got[1] != m.Status() {
		t.Errorf("status reports %+v, want connected, then disconnected", got)
	}
}

// py: tests/test_tunnel.py::test_cleanup
func TestCleanup(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	m.Close()

	m.mu.Lock()
	sess := m.sess
	m.mu.Unlock()
	if sess != nil {
		t.Error("the connection and the listener are still there")
	}
	if m.Status().Connected {
		t.Error("status is still connected")
	}
	srv.waitEnded(t, 1)
	// Close is final: the manager does not connect again.
	if err := m.Connect(context.Background()); err == nil {
		t.Error("Connect() after Close() succeeded")
	}
	if got := srv.dialedAddrs(); len(got) != 1 {
		t.Errorf("dialed %q, want only the first connection", got)
	}
}

// Not a port. Connect while the tunnel is up makes a new connection, as a
// second connect() did in Python. Python kept the first connection open with
// nothing pointing to it; Go closes it, and reports the tunnel down, then up.
func TestConnectReplacesLiveTunnel(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}

	srv.waitEnded(t, 1)
	if got := srv.loginUsers(); len(got) != 2 {
		t.Errorf("logins %q, want two", got)
	}
	if !m.Status().Connected {
		t.Error("status is not connected after the second Connect")
	}
	got := m.statuses.all()
	if len(got) != 3 || !got[0].Connected || got[1].Connected || got[1].Error != "" || !got[2].Connected {
		t.Errorf("status reports %+v, want connected, disconnected (no error), connected", got)
	}
}

// Not a port. When the config file is saved, the server gives the manager
// the new tunnel config (Python set tunnel_manager.config). The next Connect
// uses it.
func TestSetConfigIsUsedByNextConnect(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "first", 8080)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	m.Disconnect()
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	cfg.SocketName = "second"

	m.SetConfig(cfg)
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() after SetConfig() error = %v", err)
	}

	if got := srv.forwardPaths(); !slices.Equal(got, []string{"/var/run/tunnels/first", "/var/run/tunnels/second"}) {
		t.Errorf("forward requests %q, want first, then second", got)
	}
}

// Not a port: Python's reconnect loop had no test. When the connection is
// lost the manager reports it, waits 5 seconds and connects again; after each
// failure it waits twice as long, up to 300 seconds, as Python did. The test
// records the waits instead of sleeping.
func TestReconnectAfterConnectionLost(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	clock := &instantClock{}
	m.wait = clock.wait
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	_, firstID, _ := m.PublicEndpoint()

	srv.failNextDials(7)
	srv.dropConnections()

	got := m.statuses.waitFor(t, "the second connection", func(list []Status) bool { return countConnected(list) == 2 })
	if len(got) != 10 {
		t.Fatalf("status reports %+v, want connected, lost, 7 failures, connected", got)
	}
	if lost := got[1]; lost.Connected || lost.URL != "" || lost.Error != "Connection lost" {
		t.Errorf("report after the drop = %+v, want not connected with error \"Connection lost\"", lost)
	}
	for _, failed := range got[2:9] {
		if failed.Connected || !strings.HasPrefix(string(failed.Error), "Failed to establish SSH tunnel: ") {
			t.Errorf("report of a failed attempt = %+v, want not connected with error \"Failed to establish SSH tunnel: ...\"", failed)
		}
	}
	if again := got[9]; !again.Connected || again.Error != "" || !strings.HasPrefix(string(again.URL), "https://example.com/tests/") {
		t.Errorf("report after reconnecting = %+v, want connected", again)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second,
		80 * time.Second, 160 * time.Second, 300 * time.Second, 300 * time.Second}
	if waits := clock.all(); !slices.Equal(waits, want) {
		t.Errorf("waits before the attempts = %v, want %v", waits, want)
	}
	if dialed := srv.dialedAddrs(); len(dialed) != 9 {
		t.Errorf("dialed %d times, want 9 (the first connection and 8 attempts)", len(dialed))
	}
	// Like Python, a new connection gets a new random socket name. (Two random
	// names are the same about once in 70 million runs.)
	baseURL, socketID, ok := m.PublicEndpoint()
	if !ok || baseURL != "https://example.com/tests" || socketID == firstID {
		t.Errorf("PublicEndpoint() = %q, %q, %v, want https://example.com/tests and a socket name other than %q",
			baseURL, socketID, ok, firstID)
	}
	if forwards := srv.forwardPaths(); len(forwards) != 2 || forwards[1] != "/var/run/tunnels/"+socketID {
		t.Errorf("forward requests %q, want 2, the second for /var/run/tunnels/%s", forwards, socketID)
	}
	// The status report comes before the last log line. Close waits until the
	// manager has finished, so the log is complete after it.
	m.Close()
	logs := m.logs.String()
	for _, line := range []string{"SSH tunnel connection lost", "Attempting to reconnect in 5 seconds...", "Reconnected successfully: "} {
		if !strings.Contains(logs, line) {
			t.Errorf("the log has no %q:\n%s", line, logs)
		}
	}
}

// Not a port. Disconnect while the manager waits to reconnect stops the
// reconnecting, as Python's disconnect() cancelled its reconnect task.
func TestDisconnectStopsReconnect(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	clock := newStepClock()
	m.wait = clock.wait
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	srv.dropConnections()
	if wait := clock.next(t); wait != 5*time.Second {
		t.Fatalf("first reconnect wait = %v, want 5s", wait)
	}

	m.Disconnect()

	if dialed := srv.dialedAddrs(); len(dialed) != 1 {
		t.Errorf("dialed %d times, want 1: no reconnect attempt after Disconnect", len(dialed))
	}
	st := m.Status()
	if st.Connected || st.URL != "" || st.Error != "" {
		t.Errorf("status = %+v, want disconnected with no error", st)
	}
	if got := m.statuses.all(); got[len(got)-1] != st {
		t.Errorf("last status report = %+v, want %+v", got[len(got)-1], st)
	}
	clock.expectNoWait(t, 200*time.Millisecond)
}

// Not a port. After a lost connection Python kept reconnecting until
// disconnect() (_should_be_connected stayed true), also when the admin clicked
// Connect in between and that attempt failed. Go does the same.
func TestFailedConnectWhileReconnectingKeepsTrying(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	clock := newStepClock()
	m.wait = clock.wait
	if err := m.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	srv.dropConnections()
	if wait := clock.next(t); wait != 5*time.Second {
		t.Fatalf("first reconnect wait = %v, want 5s", wait)
	}
	srv.failNextDials(1)

	if err := m.Connect(context.Background()); err == nil {
		t.Fatal("Connect() succeeded, want the failure the test set up")
	}

	if wait := clock.next(t); wait != 5*time.Second {
		t.Fatalf("reconnect wait after the failed Connect = %v, want 5s", wait)
	}
	clock.release()
	m.statuses.waitFor(t, "the reconnected tunnel", func(list []Status) bool { return countConnected(list) == 2 })
	if !m.Status().Connected {
		t.Error("status is not connected after reconnecting")
	}
}

// Not a port. Python ran on one thread, Go runs admin requests in parallel.
// Connect, Disconnect, Status and PublicEndpoint called at the same time
// must not race or hang, and Close must leave no connection open.
func TestConcurrentConnectAndDisconnect(t *testing.T) {
	checkGoroutines(t)
	m, srv := newTunnelSetup(t, "", 8080)
	m.wait = (&instantClock{}).wait

	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for step := range 10 {
				if (worker+step)%2 == 0 {
					_ = m.Connect(context.Background())
				} else {
					m.Disconnect()
				}
				m.Status()
				m.PublicEndpoint()
			}
		})
	}
	wg.Wait()
	m.Close()

	if m.Status().Connected {
		t.Error("status is connected after Close")
	}
	if _, _, ok := m.PublicEndpoint(); ok {
		t.Error("PublicEndpoint() is ok after Close")
	}
	// Every SSH connection the server saw has ended.
	srv.waitEnded(t, len(srv.loginUsers()))
}
