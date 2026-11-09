package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// hubTestServer is a Server with only what serveWebSocket needs, and an HTTP
// server whose one route serves the given hub.
func hubTestServer(t *testing.T, initial func() any) (*Server, *wsHub, string, *bytes.Buffer) {
	t.Helper()
	logs := &lockedWriter{w: &bytes.Buffer{}}
	s := &Server{log: slog.New(newPyLogHandler(logs, "webquiz.server"))}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	hub := newWSHub("WebSocket", "live stats WebSocket", s.log)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.serveWebSocket(w, r, hub, initial)
	}))
	t.Cleanup(func() {
		hub.closeAll()
		s.cancel()
		hs.Close()
	})
	return s, hub, "ws" + strings.TrimPrefix(hs.URL, "http"), logs.w.(*bytes.Buffer)
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func readMessage(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type %v", typ)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatalf("not JSON: %s", data)
	}
	return message
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// _handle_websocket_connection: the initial state comes first, then every
// broadcast; {"type": "ping"} gets {"type": "pong"}.
func TestWebSocketHub(t *testing.T) {
	_, hub, url, _ := hubTestServer(t, func() any { return map[string]any{"type": "initial_state", "n": 1} })
	first := dial(t, url)
	if got := readMessage(t, first); got["type"] != "initial_state" {
		t.Fatalf("first message %v", got)
	}
	second := dial(t, url)
	readMessage(t, second)
	waitFor(t, "two clients", func() bool { return hub.count() == 2 })

	for i := range 3 {
		hub.broadcast(map[string]any{"type": "state_update", "i": i})
	}
	for _, conn := range []*websocket.Conn{first, second} {
		for i := range 3 {
			if got := readMessage(t, conn); got["type"] != "state_update" || got["i"] != float64(i) {
				t.Fatalf("broadcast %d arrived as %v", i, got)
			}
		}
	}

	ctx := context.Background()
	if err := first.Write(ctx, websocket.MessageText, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	if err := first.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if got := readMessage(t, first); got["type"] != "pong" {
		t.Fatalf("ping answer %v", got)
	}

	second.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "the closed client to leave", func() bool { return hub.count() == 1 })
}

// A client that connects while the state changes gets the change either in
// its initial state or as a broadcast, never neither: serveWebSocket takes
// the initial state and joins the hub under s.mu, and broadcasts are queued
// under s.mu right after the change.
func TestWebSocketInitialStateAndBroadcastsDoNotMiss(t *testing.T) {
	var value int // guarded by s.mu
	s, hub, url, _ := hubTestServer(t, func() any { return map[string]any{"type": "initial_state", "value": value} })

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 1; i <= 200; i++ {
			s.mu.Lock()
			value = i
			hub.broadcast(map[string]any{"type": "value", "value": i})
			s.mu.Unlock()
			if i%20 == 0 {
				time.Sleep(time.Millisecond) // let the client connect in the middle
			}
		}
	})
	conn := dial(t, url)
	wg.Wait()

	last := int(readMessage(t, conn)["value"].(float64))
	for last < 200 {
		got := int(readMessage(t, conn)["value"].(float64))
		if got != last+1 {
			t.Fatalf("after %d came %d: a change was lost or repeated", last, got)
		}
		last = got
	}
}

// Close (closeAll) disconnects every client and waits for their handlers.
func TestWebSocketHubCloseAll(t *testing.T) {
	_, hub, url, _ := hubTestServer(t, func() any { return map[string]any{"type": "initial_state"} })
	conns := []*websocket.Conn{dial(t, url), dial(t, url)}
	for _, conn := range conns {
		readMessage(t, conn)
	}
	waitFor(t, "two clients", func() bool { return hub.count() == 2 })

	done := make(chan struct{})
	go func() {
		hub.closeAll()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closeAll did not return")
	}
	if hub.count() != 0 {
		t.Errorf("%d clients left", hub.count())
	}
	for _, conn := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := conn.Read(ctx)
		cancel()
		if err == nil {
			t.Error("a client is still connected after closeAll")
		}
	}
	// A client that comes after closeAll is turned away.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn, _, err := websocket.Dial(ctx, url, nil); err == nil {
		if _, _, err := conn.Read(ctx); err == nil {
			t.Error("a new client got a message after closeAll")
		}
		conn.CloseNow()
	}
}
