package config

import (
	"path/filepath"
	"testing"
)

const tunnelYAML = `tunnel:
  server: tunnel.example.com
  public_key: keys/id_ed25519.pub
  private_key: keys/id_ed25519
`

// py: tests/test_tunnel.py::test_tunnel_config_defaults
func TestTunnelConfigDefaults(t *testing.T) {
	tunnel := Default().Tunnel
	if tunnel.Server != "" || tunnel.PublicKey != "" || tunnel.PrivateKey != "" || tunnel.Config != nil {
		t.Errorf("Tunnel = %+v, want no values (Python None)", tunnel)
	}
}

// py: tests/test_tunnel.py::test_tunnel_config_with_values
func TestTunnelConfigWithValues(t *testing.T) {
	tunnel := TunnelConfig{Server: "example.com", PublicKey: "/path/to/public", PrivateKey: "/path/to/private"}
	tunnel.ResolveKeyPaths()
	if tunnel.Server != "example.com" || tunnel.PublicKey != "/path/to/public" || tunnel.PrivateKey != "/path/to/private" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_tunnel_config_with_socket_name
func TestTunnelConfigWithSocketName(t *testing.T) {
	tunnel := TunnelConfig{Server: "example.com", PublicKey: "/path/to/public", PrivateKey: "/path/to/private", SocketName: "my-custom-socket"}
	tunnel.ResolveKeyPaths()
	if tunnel.Server != "example.com" || tunnel.SocketName != "my-custom-socket" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_tunnel_config_path_resolution
func TestTunnelConfigPathResolution(t *testing.T) {
	// Python resolved the key paths when the TunnelConfig was made; in Go,
	// Load calls ResolveKeyPaths. Both paths of the test are checked here.
	binaryDir := "/test/binary/dir"
	t.Setenv("WEBQUIZ_BINARY_DIR", binaryDir)
	tunnel := TunnelConfig{Server: "example.com", PublicKey: "keys/public.pub", PrivateKey: "keys/private"}
	tunnel.ResolveKeyPaths()
	if tunnel.Server != "example.com" ||
		tunnel.PublicKey != filepath.Join(binaryDir, "keys/public.pub") ||
		tunnel.PrivateKey != filepath.Join(binaryDir, "keys/private") {
		t.Errorf("Tunnel = %+v", tunnel)
	}
	loaded := load(t, tunnelYAML).Tunnel
	if loaded.PublicKey != filepath.Join(binaryDir, "keys/id_ed25519.pub") || loaded.PrivateKey != filepath.Join(binaryDir, "keys/id_ed25519") {
		t.Errorf("loaded Tunnel = %+v", loaded)
	}
}

// py: tests/test_tunnel.py::test_tunnel_config_absolute_paths_unchanged
func TestTunnelConfigAbsolutePathsUnchanged(t *testing.T) {
	t.Setenv("WEBQUIZ_BINARY_DIR", "/test/binary/dir")
	tunnel := TunnelConfig{Server: "example.com", PublicKey: "/absolute/path/public.pub", PrivateKey: "/absolute/path/private"}
	tunnel.ResolveKeyPaths()
	if tunnel.PublicKey != "/absolute/path/public.pub" || tunnel.PrivateKey != "/absolute/path/private" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_load_config_with_tunnel
func TestLoadConfigWithTunnel(t *testing.T) {
	noBinaryDir(t)
	tunnel := load(t, tunnelYAML).Tunnel
	if tunnel.Server != "tunnel.example.com" || tunnel.PublicKey != "keys/id_ed25519.pub" || tunnel.PrivateKey != "keys/id_ed25519" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_load_config_with_tunnel_socket_name
func TestLoadConfigWithTunnelSocketName(t *testing.T) {
	noBinaryDir(t)
	tunnel := load(t, tunnelYAML+"  socket_name: my-custom-socket\n").Tunnel
	if tunnel.Server != "tunnel.example.com" || tunnel.PublicKey != "keys/id_ed25519.pub" ||
		tunnel.PrivateKey != "keys/id_ed25519" || tunnel.SocketName != "my-custom-socket" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_load_config_without_tunnel
func TestLoadConfigWithoutTunnel(t *testing.T) {
	tunnel := load(t, "server:\n  port: 8080\n").Tunnel
	if tunnel.Server != "" || tunnel.PublicKey != "" || tunnel.PrivateKey != "" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}

// py: tests/test_tunnel.py::test_load_config_with_nested_tunnel_config
func TestLoadConfigWithNestedTunnelConfig(t *testing.T) {
	noBinaryDir(t)
	tunnel := load(t, tunnelYAML+`  config:
    username: tunneluser
    socket_directory: /var/run/tunnels
    base_url: https://tunnel.example.com/tests
`).Tunnel
	if tunnel.Server != "tunnel.example.com" || tunnel.PublicKey != "keys/id_ed25519.pub" || tunnel.PrivateKey != "keys/id_ed25519" {
		t.Errorf("Tunnel = %+v", tunnel)
	}
	want := TunnelServerConfig{Username: "tunneluser", SocketDirectory: "/var/run/tunnels", BaseURL: "https://tunnel.example.com/tests"}
	if tunnel.Config == nil || *tunnel.Config != want {
		t.Errorf("Tunnel.Config = %+v, want %+v", tunnel.Config, want)
	}
}

// py: tests/test_tunnel.py::test_load_config_with_tunnel_no_nested_config
func TestLoadConfigWithTunnelNoNestedConfig(t *testing.T) {
	tunnel := load(t, tunnelYAML).Tunnel
	if tunnel.Server != "tunnel.example.com" || tunnel.Config != nil {
		t.Errorf("Tunnel = %+v", tunnel)
	}
}
