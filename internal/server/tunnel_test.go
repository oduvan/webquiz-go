package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/tunnel"
)

// initialize_tunnel: with a tunnel server in the config the keys are made
// (or checked) at start, without connecting; the admin page shows the
// public key, and status changes reach the admin pages.
func TestTunnelSetup(t *testing.T) {
	keys := t.TempDir()
	withTunnel := func(logs *bytes.Buffer) func(*config.Config, *Options) {
		return func(cfg *config.Config, opts *Options) {
			cfg.Tunnel = config.TunnelConfig{
				Server:     "tunnel.example.com",
				PublicKey:  filepath.Join(keys, "id_ed25519.pub"),
				PrivateKey: filepath.Join(keys, "id_ed25519"),
				Config:     &config.TunnelServerConfig{Username: "u", SocketDirectory: "/tmp/s", BaseURL: "https://tunnel.example.com/t"},
			}
			opts.Logger = slog.New(slog.NewTextHandler(logs, nil))
		}
	}

	var first bytes.Buffer
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, withTunnel(&first))
	if s.tunnel == nil {
		t.Fatal("no tunnel manager")
	}
	if !strings.Contains(first.String(), "SSH tunnel initialized successfully: SSH keys generated at "+filepath.Join(keys, "id_ed25519")) {
		t.Errorf("log:\n%s", first.String())
	}
	if status := s.tunnel.Status(); status.Connected || status.KeysStatus != tunnel.KeysOK {
		t.Errorf("status after start: %+v", status)
	}

	var second bytes.Buffer
	again := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, withTunnel(&second))
	if !strings.Contains(second.String(), "SSH tunnel initialized successfully: SSH keys found and validated") {
		t.Errorf("log of the second start:\n%s", second.String())
	}

	publicKey, _ := os.ReadFile(filepath.Join(keys, "id_ed25519.pub"))
	rec := httptest.NewRecorder()
	if err := again.serveAdminPage(rec, httptest.NewRequest(http.MethodGet, "/admin/", nil)); err != nil {
		t.Fatal(err)
	}
	var boot map[string]any
	if err := json.Unmarshal([]byte(bootText(t, rec.Body.String())), &boot); err != nil {
		t.Fatal(err)
	}
	if boot["tunnel_public_key"] != strings.TrimSpace(string(publicKey)) || !strings.HasPrefix(boot["tunnel_public_key"].(string), "ssh-ed25519 ") {
		t.Errorf("tunnel_public_key %q", boot["tunnel_public_key"])
	}

	listener := &wsClient{send: make(chan []byte, 4), done: make(chan struct{})}
	again.adminClients.add(listener, nil)
	defer again.adminClients.done(listener)
	again.onTunnelStatus(tunnel.Status{KeysStatus: tunnel.KeysOK, PublicKeyContent: "ssh-ed25519 AAAA"})
	var message map[string]any
	if err := json.Unmarshal(<-listener.send, &message); err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(message["tunnel"])
	want := `{"configured":true,"connected":false,"error":null,"keys_status":"ok","public_key_content":"ssh-ed25519 AAAA","server":"tunnel.example.com","socket_name":null,"url":null}`
	if message["type"] != "tunnel_status" || string(info) != want {
		t.Errorf("message %v\n got %s\nwant %s", message["type"], info, want)
	}

	// A changed tunnel section (config save) names the new server.
	again.applyTunnelConfig(config.TunnelConfig{Server: "other.example.com", SocketName: "room-1"})
	if info := again.tunnelInfo(tunnel.Status{}); info["server"] != "other.example.com" || info["socket_name"] != "room-1" {
		t.Errorf("after the change: %v", info)
	}
}

func TestNoTunnelWithoutServer(t *testing.T) {
	var logs bytes.Buffer
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, func(_ *config.Config, opts *Options) {
		opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	if s.tunnel != nil || !strings.Contains(logs.String(), "SSH tunnel not configured, skipping initialization") {
		t.Errorf("tunnel %v, log:\n%s", s.tunnel, logs.String())
	}
	s.applyTunnelConfig(config.TunnelConfig{Server: "later.example.com"}) // needs a restart, as in Python
	if s.tunnel != nil {
		t.Error("a tunnel added by a config change must wait for a restart")
	}
}

// Not a port. An admin page that connects while the tunnel reports a status
// change gets that report: the report waits until the page has joined the
// hub and comes after the page's initial state. Before, the page read the
// status, then the tunnel changed it and reported it to the pages that were
// already there, and only then the new page joined: it kept the old status.
func TestAdminSocketMissesNoTunnelStatus(t *testing.T) {
	keys := t.TempDir()
	s := newTestServer(t, map[string]string{"q.yaml": quizText("Q")}, func(cfg *config.Config, _ *Options) {
		cfg.Tunnel = config.TunnelConfig{
			Server:     "tunnel.example.com",
			PublicKey:  filepath.Join(keys, "id_ed25519.pub"),
			PrivateKey: filepath.Join(keys, "id_ed25519"),
			Config:     &config.TunnelServerConfig{Username: "u", SocketDirectory: "/tmp/s", BaseURL: "https://tunnel.example.com/t"},
		}
	})
	page := &wsClient{send: make(chan []byte, 4), done: make(chan struct{})}

	// The page has made its initial state and waits to join the hub.
	s.adminClients.mu.Lock()
	hubLocked := true
	joinResult := make(chan bool, 1)
	go func() { joinResult <- s.joinAdminSocket(page) }()
	joined := sync.OnceValue(func() bool { return <-joinResult })
	disconnected := make(chan struct{})
	disconnecting := false
	// Also when the test fails: let the page join, take it out of the hub
	// again (it has no connection for the server's Close) and wait for the
	// tunnel's Disconnect.
	defer func() {
		if hubLocked {
			s.adminClients.mu.Unlock()
		}
		if joined() {
			s.adminClients.done(page)
		}
		if disconnecting {
			<-disconnected
		}
	}()
	waitForStack(t, "the page to wait for the hub", func(stack string) bool {
		return stackWaitsForLockIn(stack, "server.(*wsHub).add(")
	})

	// At this moment the tunnel reports a status: the report must wait.
	disconnecting = true
	go func() {
		s.tunnel.Disconnect()
		close(disconnected)
	}()
	waitForStack(t, "the tunnel report to wait for the page", func(stack string) bool {
		return stackWaitsForLockIn(stack, "tunnel.(*Manager).notifyStatus(")
	})
	s.adminClients.mu.Unlock()
	hubLocked = false

	if !joined() {
		t.Fatal("the page did not join")
	}
	for _, want := range []string{"initial_state", "tunnel_status"} {
		select {
		case data := <-page.send:
			var message map[string]any
			if err := json.Unmarshal(data, &message); err != nil {
				t.Fatal(err)
			}
			if message["type"] != want {
				t.Errorf("message %v, want type %s", message, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the page got no %s message", want)
		}
	}
}

// waitForStack waits until the stack of some goroutine, as runtime.Stack
// writes it, makes match return true, and fails the test after 10 seconds.
func waitForStack(t *testing.T, what string, match func(stack string) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		buf := make([]byte, 1<<16)
		for {
			n := runtime.Stack(buf, true)
			if n < len(buf) {
				buf = buf[:n]
				break
			}
			buf = make([]byte, 2*len(buf))
		}
		for _, stack := range strings.Split(string(buf), "\n\n") {
			if match(stack) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stackWaitsForLockIn tells whether a goroutine stack waits to lock a
// sync.Mutex in the function fn, for example "server.(*wsHub).add(".
func stackWaitsForLockIn(stack, fn string) bool {
	header, rest, _ := strings.Cut(stack, "\n")
	if !strings.Contains(header, "[sync.Mutex.Lock") {
		return false
	}
	// Function lines start without a tab. The line after
	// "sync.(*Mutex).Lock(" names the function that called it.
	var funcs []string
	for _, line := range strings.Split(rest, "\n") {
		if line != "" && !strings.HasPrefix(line, "\t") {
			funcs = append(funcs, line)
		}
	}
	for i, f := range funcs {
		if strings.HasPrefix(f, "sync.(*Mutex).Lock(") && i+1 < len(funcs) {
			return strings.Contains(funcs[i+1], fn)
		}
	}
	return false
}
