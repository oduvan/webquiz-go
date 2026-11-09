package tunnel

import (
	"crypto/rand"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// py: tests/test_tunnel.py::test_ensure_keys_not_configured
func TestEnsureKeysNotConfigured(t *testing.T) {
	m := newTestManager(t, Config{}, 8080) // no key paths

	publicKey, err := m.EnsureKeys()

	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not configured") {
		t.Fatalf("EnsureKeys() error = %v, want an error that says \"not configured\"", err)
	}
	if publicKey != "" {
		t.Errorf("EnsureKeys() public key = %q, want none", publicKey)
	}
	if got := m.Status().KeysStatus; got != "not_configured" {
		t.Errorf("keys_status = %q, want %q", got, "not_configured")
	}
}

// py: tests/test_tunnel.py::test_ensure_keys_generate_new
func TestEnsureKeysGenerateNew(t *testing.T) {
	publicPath, privatePath := keyPaths(t)
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)
	for _, path := range []string{publicPath, privatePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist yet (stat error: %v)", path, err)
		}
	}

	publicKey, err := m.EnsureKeys()

	if err != nil {
		t.Fatalf("EnsureKeys() error = %v", err)
	}
	// Python checked that the returned message said "generated". EnsureKeys
	// returns the public key instead, and the manager logs what it did.
	if !strings.Contains(m.logs.String(), "SSH keys generated") {
		t.Errorf("the log does not say the keys were generated:\n%s", m.logs.String())
	}
	privateInfo, err := os.Stat(privatePath)
	if err != nil {
		t.Fatalf("private key file: %v", err)
	}
	// Windows has no Unix permission bits.
	if runtime.GOOS != "windows" && privateInfo.Mode().Perm() != 0o600 {
		t.Errorf("private key permissions = %o, want 600", privateInfo.Mode().Perm())
	}
	publicContent, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatalf("public key file: %v", err)
	}
	if !strings.HasPrefix(string(publicContent), "ssh-ed25519 ") || !strings.Contains(string(publicContent), "webquiz@tunnel") {
		t.Errorf("public key file = %q, want \"ssh-ed25519 ... webquiz@tunnel\"", publicContent)
	}
	if publicKey != strings.TrimSpace(string(publicContent)) {
		t.Errorf("EnsureKeys() public key = %q, want the line of the file %q", publicKey, publicContent)
	}
	st := m.Status()
	if st.KeysStatus != "ok" {
		t.Errorf("keys_status = %q, want %q", st.KeysStatus, "ok")
	}
	if string(st.PublicKeyContent) != publicKey {
		t.Errorf("public_key_content = %q, want %q", st.PublicKeyContent, publicKey)
	}

	// The private key is in OpenSSH format, as Python 1.19 wrote it, and the
	// two files are one key pair.
	privateContent, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(privateContent), "-----BEGIN OPENSSH PRIVATE KEY-----\n") {
		t.Errorf("private key file does not start with the OpenSSH header:\n%s", privateContent)
	}
	signer, err := ssh.ParsePrivateKey(privateContent)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey(publicContent)
	if err != nil {
		t.Fatalf("public key does not parse: %v", err)
	}
	signature, err := signer.Sign(rand.Reader, []byte("webquiz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := public.Verify([]byte("webquiz"), signature); err != nil {
		t.Errorf("the public key does not belong to the private key: %v", err)
	}
}

// py: tests/test_tunnel.py::test_ensure_keys_both_exist
func TestEnsureKeysBothExist(t *testing.T) {
	publicPath, privatePath := keyPaths(t)
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)
	// Generate keys first.
	firstKey, err := m.EnsureKeys()
	if err != nil {
		t.Fatalf("first EnsureKeys() error = %v", err)
	}
	privateBefore, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.status.KeysStatus = "unchecked" // reset, as the Python test did
	m.mu.Unlock()

	// Check again: the existing keys are validated, not replaced.
	publicKey, err := m.EnsureKeys()

	if err != nil {
		t.Fatalf("EnsureKeys() error = %v", err)
	}
	// Python checked that the returned message said "validated".
	if !strings.Contains(m.logs.String(), "SSH keys validated") {
		t.Errorf("the log does not say the keys were validated:\n%s", m.logs.String())
	}
	if publicKey != firstKey {
		t.Errorf("EnsureKeys() public key = %q, want the existing key %q", publicKey, firstKey)
	}
	privateAfter, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(privateAfter) != string(privateBefore) {
		t.Error("the private key file changed; existing keys must be kept")
	}
	if got := m.Status().KeysStatus; got != "ok" {
		t.Errorf("keys_status = %q, want %q", got, "ok")
	}
}

// py: tests/test_tunnel.py::test_ensure_keys_only_public_exists
func TestEnsureKeysOnlyPublicExists(t *testing.T) {
	publicPath, privatePath := keyPaths(t)
	if err := os.WriteFile(publicPath, []byte("fake public key"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)

	_, err := m.EnsureKeys()

	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "private key is missing") {
		t.Fatalf("EnsureKeys() error = %v, want an error that says \"private key is missing\"", err)
	}
	if got := m.Status().KeysStatus; got != "partial" {
		t.Errorf("keys_status = %q, want %q", got, "partial")
	}
}

// py: tests/test_tunnel.py::test_ensure_keys_only_private_exists
func TestEnsureKeysOnlyPrivateExists(t *testing.T) {
	publicPath, privatePath := keyPaths(t)
	if err := os.WriteFile(privatePath, []byte("fake private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)

	_, err := m.EnsureKeys()

	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "public key is missing") {
		t.Fatalf("EnsureKeys() error = %v, want an error that says \"public key is missing\"", err)
	}
	if got := m.Status().KeysStatus; got != "partial" {
		t.Errorf("keys_status = %q, want %q", got, "partial")
	}
}

// Not a port: Python had no test for this branch. Key files that exist but
// do not hold keys are reported as invalid, and nothing is overwritten.
func TestEnsureKeysInvalid(t *testing.T) {
	publicPath, privatePath := keyPaths(t)
	for _, path := range []string{publicPath, privatePath} {
		if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestManager(t, Config{Server: "example.com", PublicKey: publicPath, PrivateKey: privatePath}, 8080)

	_, err := m.EnsureKeys()

	if err == nil || !strings.HasPrefix(err.Error(), "Keys exist but are invalid: ") {
		t.Fatalf("EnsureKeys() error = %v, want \"Keys exist but are invalid: ...\"", err)
	}
	st := m.Status()
	if st.KeysStatus != "invalid" {
		t.Errorf("keys_status = %q, want %q", st.KeysStatus, "invalid")
	}
	if string(st.Error) != err.Error() {
		t.Errorf("status error = %q, want %q", st.Error, err.Error())
	}
	content, err := os.ReadFile(privatePath)
	if err != nil || string(content) != "not a key" {
		t.Errorf("the private key file was changed: %q, %v", content, err)
	}
}
