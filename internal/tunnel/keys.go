package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// keyComment ends the line of a public key that WebQuiz makes.
const keyComment = "webquiz@tunnel"

// EnsureKeys checks the SSH key files of the config. When neither file
// exists, it makes a new ED25519 key pair there. It returns the public key
// line, as written in the .pub file, and sets the keys status.
func (m *Manager) EnsureKeys() (publicKey string, err error) {
	m.keysMu.Lock()
	defer m.keysMu.Unlock()

	m.mu.Lock()
	publicPath, privatePath := m.cfg.PublicKey, m.cfg.PrivateKey
	m.mu.Unlock()

	if publicPath == "" || privatePath == "" {
		m.setKeysStatus(KeysNotConfigured, "SSH key paths not configured")
		return "", errors.New("SSH key paths not configured in tunnel section")
	}
	publicExists, privateExists := fileExists(publicPath), fileExists(privatePath)
	switch {
	case publicExists && !privateExists:
		const message = "Public key exists but private key is missing"
		m.setKeysStatus(KeysPartial, message)
		return "", errors.New(message)
	case privateExists && !publicExists:
		const message = "Private key exists but public key is missing"
		m.setKeysStatus(KeysPartial, message)
		return "", errors.New(message)
	case publicExists && privateExists:
		return m.checkKeys(publicPath, privatePath)
	}
	return m.makeKeys(publicPath, privatePath)
}

// checkKeys reads both key files and checks that they hold keys.
func (m *Manager) checkKeys(publicPath, privatePath string) (string, error) {
	publicKey, err := readKeys(publicPath, privatePath)
	if err != nil {
		message := "Keys exist but are invalid: " + err.Error()
		m.setKeysStatus(KeysInvalid, message)
		return "", errors.New(message)
	}
	m.keysReady(publicKey)
	m.log().Info("SSH keys validated successfully")
	return publicKey, nil
}

// makeKeys writes a new key pair to the two paths.
func (m *Manager) makeKeys(publicPath, privatePath string) (string, error) {
	m.log().Info("Generating new ED25519 SSH key pair...")
	publicKey, err := writeNewKeys(publicPath, privatePath)
	if err != nil {
		m.setKeysStatus(KeysError, "Failed to generate keys: "+err.Error())
		m.log().Error("Failed to generate SSH keys: " + err.Error())
		return "", errors.New("Failed to generate SSH keys: " + err.Error())
	}
	m.keysReady(publicKey)
	m.log().Info(fmt.Sprintf("SSH keys generated successfully at %s and %s", privatePath, publicPath))
	return publicKey, nil
}

// setKeysStatus records a failed key check and its message.
func (m *Manager) setKeysStatus(keysStatus, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.KeysStatus = keysStatus
	m.status.Error = OptionalString(message)
}

// keysReady records usable keys. Like Python, it leaves the status error as it is.
func (m *Manager) keysReady(publicKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.KeysStatus = KeysOK
	m.status.PublicKeyContent = OptionalString(publicKey)
}

// readKeys reads the key files, checks that they parse, and returns the
// public key line.
func readKeys(publicPath, privatePath string) (string, error) {
	publicData, err := os.ReadFile(publicPath)
	if err != nil {
		return "", err
	}
	privateData, err := os.ReadFile(privatePath)
	if err != nil {
		return "", err
	}
	if _, err := ssh.ParsePrivateKey(privateData); err != nil {
		return "", err
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey(publicData); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(publicData)), nil
}

// writeNewKeys makes an ED25519 key pair. The private key file is in OpenSSH
// format with no passphrase and only its owner may read it. The public key
// file is one line, "ssh-ed25519 <key> webquiz@tunnel", as Python 1.19 wrote it.
func writeNewKeys(publicPath, privatePath string) (string, error) {
	for _, path := range []string{privatePath, publicPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
			return "", err
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		return "", err
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", err
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))) + " " + keyComment
	if err := writeKeyFile(privatePath, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}
	if err := writeKeyFile(publicPath, []byte(publicKey+"\n"), 0o644); err != nil {
		return "", err
	}
	return publicKey, nil
}

// writeKeyFile writes a key file and gives it exactly the permissions perm.
func writeKeyFile(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
