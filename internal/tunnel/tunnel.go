// Package tunnel makes a local WebQuiz server reachable from the internet
// through an SSH tunnel server.
//
// The manager connects to the tunnel server over SSH and asks it to listen
// on a Unix socket (OpenSSH's "streamlocal-forward"). Each connection to that
// socket comes back over SSH and goes on to the local WebQuiz port. The web
// server of the tunnel server publishes the socket at {base_url}/{socket name}/.
//
// It is a translation of tunnel.py from WebQuiz 1.19.
package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config is the tunnel section of webquiz.yaml. It has the fields of
// Python's TunnelConfig; an empty string stands for a missing value.
type Config struct {
	// Server is the host name or IP address of the SSH tunnel server.
	Server string `yaml:"server"`
	// PublicKey and PrivateKey are the paths of the SSH key files. When
	// neither file exists, EnsureKeys makes a new key pair there. The
	// manager uses the paths as given: resolving a relative path against
	// the base folder is the job of config loading, as in Python's TunnelConfig.
	PublicKey  string `yaml:"public_key"`
	PrivateKey string `yaml:"private_key"`
	// SocketName is a fixed name for the socket on the tunnel server. When it
	// is empty, every connection gets a random name of 6 or 8 hex characters.
	SocketName string `yaml:"socket_name"`
	// Config holds the tunnel server settings written in the config file.
	// When it is nil, they are downloaded from the server's tunnel_config.yaml.
	Config *ServerConfig `yaml:"config"`
}

// ServerConfig is what a tunnel server tells about itself in
// tunnel_config.yaml, or what the config file says instead.
type ServerConfig struct {
	Username        string `yaml:"username"`         // SSH user for tunnel connections
	SocketDirectory string `yaml:"socket_directory"` // folder of the sockets on the tunnel server
	BaseURL         string `yaml:"base_url"`         // public prefix: the quiz is at {base_url}/{socket name}/
}

// Status is what the admin panel shows about the tunnel. The JSON names are
// the keys of Python's status dict. URL, Error and PublicKeyContent are empty
// where Python had None, and their JSON is null then, as in Python.
//
// Status has no MarshalJSON of its own, so a struct that embeds it (the
// server adds configured, server and socket_name) keeps its other fields.
type Status struct {
	Connected        bool           `json:"connected"`
	URL              OptionalString `json:"url"`
	Error            OptionalString `json:"error"`
	KeysStatus       string         `json:"keys_status"`
	PublicKeyContent OptionalString `json:"public_key_content"`
}

// OptionalString is a string that Python could leave as None: the empty
// string stands for None and is written to JSON as null.
type OptionalString string

// MarshalJSON writes null for the empty string, and a JSON string otherwise.
func (s OptionalString) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(s))
}

// The values of Status.KeysStatus.
const (
	KeysUnchecked     = "unchecked"      // EnsureKeys has not run yet
	KeysNotConfigured = "not_configured" // a key path is missing in the config
	KeysPartial       = "partial"        // only one of the two key files exists
	KeysOK            = "ok"             // the keys are there and can be used
	KeysInvalid       = "invalid"        // both files exist, but they hold no usable key
	KeysError         = "error"          // making new keys failed
)

// Manager keeps one SSH tunnel and reports its status. Its methods are safe
// to call from many goroutines.
type Manager struct {
	localPort int
	onStatus  func(Status)

	// Waits, limits and the ways to reach the network. Tests change them
	// before they use the manager.
	reconnectDelay    time.Duration // the first wait before reconnecting
	maxReconnectDelay time.Duration // the longest wait between reconnects
	loginTimeout      time.Duration // limit for the SSH connection and the forward request
	fetchTimeout      time.Duration // limit for downloading tunnel_config.yaml
	closeTimeout      time.Duration // limit for the tunnel server's answer when the tunnel closes
	wait              func(ctx context.Context, d time.Duration) error
	dial              func(ctx context.Context, network, addr string) (net.Conn, error)
	transport         http.RoundTripper // nil means http.DefaultTransport

	logger atomic.Pointer[slog.Logger]

	keysMu   sync.Mutex // EnsureKeys runs one call at a time
	notifyMu sync.Mutex // status reports go out one at a time

	mu       sync.Mutex // guards the fields below
	cfg      Config
	status   Status
	run      *run     // the current run, see run; nil after Disconnect
	sess     *session // the open SSH connection with its remote socket, or nil
	socketID string   // the socket name of the open connection
	baseURL  string   // base_url of the open connection, without a trailing "/"
	want     bool     // Python's _should_be_connected: a lost connection is to be made again
	closed   bool     // Close was called
}

// run is the time from one Connect until the next Connect, Disconnect or
// Close. It owns the connection attempt of that Connect, the open session,
// the goroutines that serve it and the supervisor that reconnects. All of
// them are counted in wg, and stopping the run cancels ctx and waits for
// them, so nothing of an old run is left running.
type run struct {
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	supervised bool // a supervisor goroutine runs for this run (guarded by Manager.mu)
}

func newRun() *run {
	ctx, cancel := context.WithCancel(context.Background())
	return &run{ctx: ctx, cancel: cancel}
}

// errClosed is the error of Connect after Close.
var errClosed = errors.New("the tunnel manager is closed")

// New makes a manager for the tunnel that cfg describes. The tunnel forwards
// connections to 127.0.0.1:localPort, the local WebQuiz server.
//
// onStatus, when not nil, gets the new status each time the connection
// state changes: connected, connecting failed, connection lost, disconnected.
// It runs on the goroutine that made the change and must not call Connect,
// Disconnect or Close.
func New(cfg Config, localPort int, onStatus func(Status)) *Manager {
	return &Manager{
		localPort:         localPort,
		onStatus:          onStatus,
		reconnectDelay:    5 * time.Second,   // Python's first reconnect delay
		maxReconnectDelay: 300 * time.Second, // Python's longest reconnect delay
		loginTimeout:      120 * time.Second, // asyncssh's default login timeout
		fetchTimeout:      30 * time.Second,  // Python's httpx timeout
		closeTimeout:      5 * time.Second,   // Go only: Python waited without a limit, and could hang on a dead link
		wait:              sleep,
		dial:              (&net.Dialer{}).DialContext, // TCP keepalive is on by default, as in asyncssh
		cfg:               cfg,
		status:            Status{KeysStatus: KeysUnchecked},
	}
}

// SetLogger sets where the manager writes its log. By default it uses
// slog.Default().
func (m *Manager) SetLogger(logger *slog.Logger) {
	m.logger.Store(logger)
}

// log returns the logger to write to.
func (m *Manager) log() *slog.Logger {
	if logger := m.logger.Load(); logger != nil {
		return logger
	}
	return slog.Default()
}

// SetConfig replaces the tunnel config, as Python did when the config file
// was saved. An open tunnel keeps its connection; the next connection
// attempt uses the new config.
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Status returns the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// PublicEndpoint returns the public address of this server while the tunnel
// is connected: other WebQuiz servers on the same tunnel server are at
// {baseURL}/{their socket name}/. ok is false when the tunnel is not connected.
func (m *Manager) PublicEndpoint() (baseURL, socketID string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Connected && m.baseURL != "" && m.socketID != "" {
		return m.baseURL, m.socketID, true
	}
	return "", "", false
}

// notifyStatus sends the current status to onStatus. Reports go out one at a
// time, and each one carries the status at the moment it is sent, so the last
// report is always the current status.
func (m *Manager) notifyStatus() {
	if m.onStatus == nil {
		return
	}
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()
	status := m.Status()
	// Python logged an error from the callback and went on; a panic in the
	// callback must not stop the tunnel either.
	defer func() {
		if err := recover(); err != nil {
			m.log().Error(fmt.Sprintf("Error in status callback: %v", err))
		}
	}()
	m.onStatus(status)
}

// generateSocketID returns a random socket name of 6 or 8 hex characters.
func generateSocketID() string {
	var size [1]byte
	_, _ = rand.Read(size[:])
	data := make([]byte, 3+int(size[0]&1)) // 3 bytes = 6 hex characters, 4 bytes = 8
	_, _ = rand.Read(data)
	return hex.EncodeToString(data)
}

// sleep waits for d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Connect opens the tunnel, like Python's connect(): it checks the keys, gets
// the tunnel server settings, connects over SSH and asks the server to listen
// on the socket. Then the status says connected, with the public URL, and the
// manager reconnects whenever the connection is lost, until Disconnect or
// Close. The error says why connecting failed, in Python's words.
//
// When a tunnel is already up, Connect closes it and opens a new one, as a
// second connect() did in Python (Python also left the old one open).
func (m *Manager) Connect(ctx context.Context) error {
	r, err := m.startRun()
	if err != nil {
		return err
	}
	defer r.wg.Done()
	if _, err := m.attempt(ctx, r); err != nil {
		// Python kept reconnecting after a lost connection until disconnect(),
		// also when a connect() in between failed.
		m.mu.Lock()
		if m.want && m.run == r && !r.supervised {
			r.supervised = true
			r.wg.Go(func() { m.supervise(r, nil) })
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

// Disconnect closes the tunnel and stops reconnecting. The status then says
// not connected, with no URL and no error.
func (m *Manager) Disconnect() {
	m.mu.Lock()
	r, s := m.detachLocked()
	m.want = false
	m.mu.Unlock()

	m.stopRun(r, s)
	m.reportDisconnected(nil)
}

// Close disconnects for good: Connect fails afterwards. When Close returns,
// nothing of the tunnel runs any more.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.Disconnect()
}

// startRun makes a new run the current one, counting the caller in its wait
// group. It stops the previous run first; an open tunnel of that run is
// closed and reported as by Disconnect.
func (m *Manager) startRun() (*run, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errClosed
	}
	previous, s := m.detachLocked()
	r := newRun()
	r.wg.Add(1)
	m.run = r
	m.mu.Unlock()

	m.stopRun(previous, s)
	if s != nil {
		m.reportDisconnected(r)
	}
	return r, nil
}

// detachLocked takes the current run and its open session away from the
// manager, so that they can be stopped. The caller holds m.mu.
func (m *Manager) detachLocked() (*run, *session) {
	r, s := m.run, m.sess
	m.run, m.sess = nil, nil
	m.socketID, m.baseURL = "", ""
	return r, s
}

// reportDisconnected sets the status to not connected, with no URL and no
// error, and reports it. It leaves the status alone when a run other than
// current has started meanwhile: the newer run owns the status then.
func (m *Manager) reportDisconnected(current *run) {
	m.mu.Lock()
	if m.run == current {
		m.status.Connected = false
		m.status.URL = ""
		m.status.Error = ""
	}
	m.mu.Unlock()
	m.log().Info("SSH tunnel disconnected")
	m.notifyStatus()
}

// stopRun cancels r, closes its open session s (if any) and waits until all
// goroutines of r have ended. r must no longer be the current run.
func (m *Manager) stopRun(r *run, s *session) {
	if r == nil {
		return
	}
	r.cancel()
	if s != nil {
		s.close(m.closeTimeout)
	}
	r.wg.Wait()
}

// attempt makes one connection for run r, as Python's connect() did. On
// success the new session is the open one and the status says connected.
// A failed step puts its reason in the status error.
func (m *Manager) attempt(ctx context.Context, r *run) (*session, error) {
	// Stop when the caller gives up or the run is stopped.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopFollowingRun := context.AfterFunc(r.ctx, cancel)
	defer stopFollowingRun()

	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if cfg.Server == "" {
		m.mu.Lock()
		m.status.Error = OptionalString(errNoServer.Error())
		m.mu.Unlock()
		return nil, errNoServer
	}
	if _, err := m.EnsureKeys(); err != nil {
		return nil, err
	}
	serverConfig, err := m.fetchTunnelConfig(ctx)
	if err != nil {
		return nil, err
	}
	socketID := cfg.SocketName
	if socketID == "" {
		socketID = generateSocketID()
	}
	m.log().Info(fmt.Sprintf("Connecting to %s as %s...", cfg.Server, serverConfig.Username))
	s, err := m.open(ctx, cfg.Server, cfg.PrivateKey, serverConfig.Username, serverConfig.SocketDirectory+"/"+socketID)
	if err != nil {
		return nil, m.failed(r, err)
	}
	baseURL := strings.TrimRight(serverConfig.BaseURL, "/")
	s.url = baseURL + "/" + socketID + "/"

	m.mu.Lock()
	if m.run != r {
		// Disconnect, Close or another Connect came first.
		m.mu.Unlock()
		s.close(m.closeTimeout)
		return nil, fmt.Errorf("Failed to establish SSH tunnel: %w", context.Canceled)
	}
	m.sess = s
	m.socketID, m.baseURL = socketID, baseURL
	m.status.Connected = true
	m.status.URL = OptionalString(s.url)
	m.status.Error = ""
	m.want = true
	localAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(m.localPort))
	r.wg.Go(s.waitClosed)
	r.wg.Go(func() { s.serve(&r.wg, localAddr) })
	if !r.supervised {
		r.supervised = true
		r.wg.Go(func() { m.supervise(r, s) })
	}
	m.mu.Unlock()

	m.log().Info("SSH tunnel established! Public URL: " + s.url)
	m.notifyStatus()
	return s, nil
}

// failed records a failed SSH connection, as Python's connect() did: the
// status says why and the callback hears it. A stopped run changes nothing.
func (m *Manager) failed(r *run, err error) error {
	err = fmt.Errorf("Failed to establish SSH tunnel: %w", err)
	m.mu.Lock()
	current := m.run == r
	if current {
		m.status.Connected = false
		m.status.URL = ""
		m.status.Error = OptionalString(err.Error())
	}
	m.mu.Unlock()
	if current {
		m.log().Error(err.Error())
		m.notifyStatus()
	}
	return err
}

// supervise watches the connection of run r and makes it again when it is
// lost, like Python's _monitor_connection and _auto_reconnect. s is the open
// session, or nil to start with reconnecting.
func (m *Manager) supervise(r *run, s *session) {
	for {
		if s != nil {
			select {
			case <-s.done:
			case <-r.ctx.Done():
				return
			}
			if !m.lost(r, s) {
				return
			}
		}
		s = m.reconnect(r)
		if s == nil {
			return
		}
	}
}

// lost records that the connection s ended by itself. It returns false when
// the run was stopped meanwhile, and then changes nothing.
func (m *Manager) lost(r *run, s *session) bool {
	m.mu.Lock()
	if m.run != r || m.sess != s {
		m.mu.Unlock()
		return false
	}
	m.sess = nil
	m.status.Connected = false
	m.status.URL = ""
	m.status.Error = "Connection lost"
	m.mu.Unlock()
	s.close(m.closeTimeout) // free what is left of the connection
	m.log().Warn("SSH tunnel connection lost")
	m.notifyStatus()
	return true
}

// reconnect tries to connect again. It waits reconnectDelay before the first
// try, and after each failure twice as long as before, up to
// maxReconnectDelay. It returns the new session, or nil when the run stops.
func (m *Manager) reconnect(r *run) *session {
	delay := m.reconnectDelay
	for {
		m.log().Info(fmt.Sprintf("Attempting to reconnect in %s seconds...", seconds(delay)))
		if err := m.wait(r.ctx, delay); err != nil {
			m.log().Info("Reconnect cancelled")
			return nil
		}
		m.log().Info("Reconnecting SSH tunnel...")
		s, err := m.attempt(r.ctx, r)
		if err == nil {
			m.log().Info("Reconnected successfully: " + s.url)
			return s
		}
		if r.ctx.Err() != nil {
			m.log().Info("Reconnect cancelled")
			return nil
		}
		m.log().Warn("Reconnect attempt failed: " + err.Error())
		delay = min(delay*2, m.maxReconnectDelay)
	}
}

// seconds writes d as a number of seconds, as Python wrote its delays: 5, 300.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}
