package server

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/oduvan/webquiz-go/internal/config"
	"github.com/oduvan/webquiz-go/internal/tunnel"
)

// initTunnel is initialize_tunnel: when the config names a tunnel server, it
// makes the tunnel manager and checks (or makes) the SSH keys. It does not
// connect: the admin does that with a button.
func (s *Server) initTunnel() {
	tunnelCfg := s.cfg.Tunnel
	if tunnelCfg.Server == "" {
		s.log.Info("SSH tunnel not configured, skipping initialization")
		return
	}
	s.log.Info(fmt.Sprintf("Initializing SSH tunnel for server: %s", tunnelCfg.Server))
	s.tunnelNames.Store(&tunnelNames{server: tunnelCfg.Server, socketName: tunnelCfg.SocketName})
	s.tunnel = tunnel.New(tunnelSettings(tunnelCfg), s.cfg.Server.Port, s.onTunnelStatus)
	s.tunnel.SetLogger(s.log.With(loggerKey, loggerTunnel))

	keysExisted := fileExists(tunnelCfg.PublicKey) && fileExists(tunnelCfg.PrivateKey)
	if _, err := s.tunnel.EnsureKeys(); err != nil {
		s.log.Warn(fmt.Sprintf("SSH tunnel keys not ready: %v", err))
		return
	}
	message := "SSH keys found and validated"
	if !keysExisted {
		message = fmt.Sprintf("SSH keys generated at %s", tunnelCfg.PrivateKey)
	}
	s.log.Info(fmt.Sprintf("SSH tunnel initialized successfully: %s", message))
}

// tunnelSettings is the tunnel package's view of the "tunnel" section.
func tunnelSettings(t config.TunnelConfig) tunnel.Config {
	settings := tunnel.Config{Server: t.Server, PublicKey: t.PublicKey, PrivateKey: t.PrivateKey, SocketName: t.SocketName}
	if t.Config != nil {
		settings.Config = &tunnel.ServerConfig{Username: t.Config.Username, SocketDirectory: t.Config.SocketDirectory, BaseURL: t.Config.BaseURL}
	}
	return settings
}

// applyTunnelConfig gives a changed "tunnel" section to the running tunnel
// (config hot reload, apply_config_changes): new connections use it, and its
// status messages name the new server. The caller disconnects an open
// tunnel, as Python did. Without a tunnel manager (no tunnel at start) it
// does nothing: like Python, adding a tunnel needs a restart.
func (s *Server) applyTunnelConfig(t config.TunnelConfig) {
	if s.tunnel == nil {
		return
	}
	s.tunnelNames.Store(&tunnelNames{server: t.Server, socketName: t.SocketName})
	s.tunnel.SetConfig(tunnelSettings(t))
}

// onTunnelStatus is the tunnel's status callback: it tells the admin pages
// about the change. It runs on a tunnel goroutine and must not take mu (the
// tunnel may report while a handler that holds mu disconnects it).
func (s *Server) onTunnelStatus(status tunnel.Status) {
	s.broadcastAdmin(map[string]any{"type": "tunnel_status", "tunnel": s.tunnelInfo(status)})
}

// withTunnelInfo calls fn with the tunnel status for the admin page (nil
// when no tunnel is configured). While fn runs, the tunnel holds back its
// status reports (tunnel.Manager.WithStatus): a change that happens
// meanwhile reaches the admin pages after fn, so a page that joins the hub
// in fn cannot miss it. fn must not call the tunnel's Connect, Disconnect or
// Close, and nobody may call them while holding mu (fn takes mu).
func (s *Server) withTunnelInfo(fn func(tunnelInfo any)) {
	if s.tunnel == nil {
		fn(nil)
		return
	}
	s.tunnel.WithStatus(func(status tunnel.Status) { fn(s.tunnelInfo(status)) })
}

// tunnelInfo is the tunnel status for the admin page: configured, server and
// socket_name, then the status fields (connected, url, error, keys_status,
// public_key_content) with the tunnel package's JSON rules (null for an
// empty value, as Python's None).
func (s *Server) tunnelInfo(status tunnel.Status) map[string]any {
	info := map[string]any{"configured": true, "server": nil, "socket_name": nil}
	if names := s.tunnelNames.Load(); names != nil {
		info["server"] = nullIfEmpty(names.server)
		info["socket_name"] = nullIfEmpty(names.socketName)
	}
	if data, err := json.Marshal(status); err == nil {
		var fields map[string]any
		if json.Unmarshal(data, &fields) == nil {
			for key, value := range fields {
				info[key] = value
			}
		}
	}
	return info
}

// nullIfEmpty is nil (JSON null, Python None) for "", else the text.
func nullIfEmpty(text string) any {
	if text == "" {
		return nil
	}
	return text
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
