package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// requireUnlocked fails the test when mu stays locked. It then frees mu, so
// the test can stop: the handlers waiting for mu would keep the test HTTP
// server from closing.
func requireUnlocked(t *testing.T, s *Server, after string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.withLock(func() {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.mu.Unlock()
		t.Fatalf("mu is still locked after %s", after)
	}
}

// A panic while mu is held fails only that request: mu is free again, and
// the next request that needs mu is answered. (Python failed only the one
// request too; a forgotten unlock would make the whole server wait forever.)
func TestPanicWhileLockedDoesNotBlockTheServer(t *testing.T) {
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, nil)
	mux := http.NewServeMux()
	mux.Handle("GET /ws-broken", s.wrap(public, func(w http.ResponseWriter, r *http.Request) error {
		return s.serveWebSocket(w, r, s.liveStatsClients, func() any { panic("broken initial state") })
	}))
	mux.Handle("GET /locked-panic", s.wrap(public, func(w http.ResponseWriter, r *http.Request) error {
		s.withLock(func() { panic("broken state") })
		return nil
	}))
	mux.Handle("/", s.Handler())
	hs := httptest.NewServer(mux)
	defer hs.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	// The initial state of a WebSocket is built under mu.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http")+"/ws-broken", nil); err == nil {
		if _, _, err := conn.Read(ctx); err == nil {
			t.Error("the connection must be closed after the panic")
		}
		conn.CloseNow()
	}
	requireUnlocked(t, s, "a panic in a WebSocket's initial state")
	if got := s.liveStatsClients.count(); got != 0 {
		t.Errorf("%d clients joined despite the panic", got)
	}

	res, err := client.Get(hs.URL + "/locked-panic")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("a panic in a locked section: %d, want 500", res.StatusCode)
	}
	requireUnlocked(t, s, "a panic inside withLock")

	// Both of these take mu: isTrustedIP for the admin page, validSession for the check.
	for _, path := range []string{"/admin/", "/api/admin/check-session"} {
		request, _ := http.NewRequest(http.MethodGet, hs.URL+path, nil)
		request.Header.Set("Cookie", "admin_session=unknown")
		res, err := client.Do(request)
		if err != nil {
			t.Fatalf("GET %s after the panics: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s: %d", path, res.StatusCode)
		}
	}
}
