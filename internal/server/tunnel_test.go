package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
