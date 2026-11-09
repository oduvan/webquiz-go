package tunnel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

// waitTimeout is how long a test waits for something that should happen soon.
const waitTimeout = 10 * time.Second

// testManager is a Manager with the records a test looks at.
type testManager struct {
	*Manager
	statuses *statusLog
	logs     *syncBuffer
}

// newTestManager makes a manager that records its status reports and its log.
// Its SSH dial and HTTP transport fail the test until the test sets its own,
// so a test cannot reach the network by mistake. Close runs at cleanup.
func newTestManager(t *testing.T, cfg Config, localPort int) *testManager {
	t.Helper()
	statuses := &statusLog{}
	logs := &syncBuffer{}
	m := New(cfg, localPort, statuses.add)
	m.SetLogger(slog.New(slog.NewTextHandler(logs, nil)))
	m.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Errorf("unexpected SSH connection to %s", addr)
		return nil, errors.New("this test allows no SSH connection")
	}
	m.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected HTTP request to %s", r.URL)
		return nil, errors.New("this test allows no HTTP request")
	})
	t.Cleanup(m.Close)
	return &testManager{Manager: m, statuses: statuses, logs: logs}
}

// quiet makes a manager from New write its log nowhere, and closes it at
// cleanup.
func quiet(t *testing.T, m *Manager) *Manager {
	t.Helper()
	m.SetLogger(slog.New(slog.DiscardHandler))
	t.Cleanup(m.Close)
	return m
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// statusLog keeps every status the manager reports, in order.
type statusLog struct {
	mu   sync.Mutex
	list []Status
}

func (l *statusLog) add(s Status) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = append(l.list, s)
}

func (l *statusLog) all() []Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.list)
}

// waitFor waits until the reports so far satisfy done, and returns them.
func (l *statusLog) waitFor(t *testing.T, what string, done func([]Status) bool) []Status {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		list := l.all()
		if done(list) {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; statuses so far: %+v", what, list)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// syncBuffer is a buffer that many goroutines can write to.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// keyPaths returns paths for a key pair in a new temporary folder.
func keyPaths(t *testing.T) (publicPath, privatePath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "id_ed25519.pub"), filepath.Join(dir, "id_ed25519")
}

// checkGoroutines fails the test when, at the end of it, more goroutines run
// than at its start. Call it first in a test, so that its check runs after
// all other cleanup (the manager's Close, the test servers' Close).
func checkGoroutines(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(waitTimeout)
		for runtime.NumGoroutine() > before {
			if time.Now().After(deadline) {
				stacks := make([]byte, 1<<20)
				stacks = stacks[:runtime.Stack(stacks, true)]
				t.Errorf("%d goroutines still run, %d ran before the test:\n%s",
					runtime.NumGoroutine(), before, stacks)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
}

// tcpServer is a plain TCP server on 127.0.0.1 that runs handle for each
// connection. Create it before the manager, so it closes after the manager.
type tcpServer struct {
	listener net.Listener
	accepted chan net.Conn // gets each connection when it is accepted
	wg       sync.WaitGroup

	mu    sync.Mutex
	conns []net.Conn
}

func newTCPServer(t *testing.T, handle func(net.Conn)) *tcpServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &tcpServer{listener: listener, accepted: make(chan net.Conn, 100)}
	s.wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()
			s.accepted <- conn
			s.wg.Go(func() { handle(conn) })
		}
	})
	t.Cleanup(func() {
		listener.Close()
		s.mu.Lock()
		for _, conn := range s.conns {
			conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *tcpServer) port() int { return s.listener.Addr().(*net.TCPAddr).Port }

// dial is a manager dial function that connects to this server.
func (s *tcpServer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, s.listener.Addr().String())
}

// nextConn waits until the server accepts a connection, and returns it.
func (s *tcpServer) nextConn(t *testing.T) net.Conn {
	t.Helper()
	select {
	case conn := <-s.accepted:
		return conn
	case <-time.After(waitTimeout):
		t.Fatal("no connection reached the server")
		return nil
	}
}

// keepOpen is a tcpServer handler that reads and never answers: a silent SSH
// server, or a local server that holds a connection open.
func keepOpen(conn net.Conn) { _, _ = io.Copy(io.Discard, conn) }

// fakeWeb serves tunnel_config.yaml over HTTP and HTTPS. Its transport sends
// every request to it, whatever host the URL names: port 443 goes to the
// HTTPS server, any other port to the HTTP one. So a test sees the exact URL
// the manager asked for and whether TLS was used, without leaving this machine.
// The test certificate is valid for example.com.
type fakeWeb struct {
	transport *http.Transport

	mu       sync.Mutex
	status   int
	location string
	body     string
	requests []string // "scheme://host/path" of each request, as received
}

func newFakeWeb(t *testing.T, body string) *fakeWeb {
	t.Helper()
	w := &fakeWeb{status: http.StatusOK, body: body}
	handler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		w.mu.Lock()
		w.requests = append(w.requests, scheme+"://"+r.Host+r.URL.RequestURI())
		status, location, body := w.status, w.location, w.body
		w.mu.Unlock()
		if location != "" {
			rw.Header().Set("Location", location)
		}
		rw.WriteHeader(status)
		_, _ = io.WriteString(rw, body)
	})
	plain := httptest.NewServer(handler)
	secure := httptest.NewTLSServer(handler)
	transport := secure.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		target := plain.Listener.Addr().String()
		if _, port, _ := net.SplitHostPort(addr); port == "443" {
			target = secure.Listener.Addr().String()
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", target)
	}
	w.transport = transport
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		plain.Close()
		secure.Close()
	})
	return w
}

// respond sets the status code and Location header of the next replies.
func (w *fakeWeb) respond(status int, location string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status, w.location = status, location
}

func (w *fakeWeb) requestURLs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.requests)
}
