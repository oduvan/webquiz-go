package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeSSHServer is a small SSH server that does for WebQuiz what a tunnel
// server does. On "streamlocal-forward@openssh.com" it listens on a Unix
// socket, and it sends every connection to that socket back to the client as
// a "forwarded-streamlocal@openssh.com" channel. It accepts one user with one
// key (see authorize).
//
// It makes the sockets in a short temporary folder, not at the paths the
// client asks for: those paths belong to the real tunnel server, and socket
// paths have a length limit. socketFor tells which socket serves a path.
type fakeSSHServer struct {
	listener net.Listener
	config   *ssh.ServerConfig
	sockDir  string
	wg       sync.WaitGroup

	mu        sync.Mutex
	user      string
	key       ssh.PublicKey // the only client key the server accepts
	closing   bool
	failDials int                      // how many of the next dials fail
	dialed    []string                 // addresses the manager dialed
	logins    []string                 // users that logged in
	forwards  []string                 // socket paths the client asked to listen on
	cancels   []string                 // socket paths the client asked to stop listening on
	sockets   map[string]string        // asked path -> socket made for it
	conns     map[*ssh.ServerConn]bool // open SSH connections
	visitors  map[net.Conn]bool        // open connections to the sockets
	ended     int                      // SSH connections that have ended
}

// newFakeSSHServer starts a server on 127.0.0.1. Create it before the
// manager, so that the manager closes first at cleanup.
func newFakeSSHServer(t *testing.T) *fakeSSHServer {
	t.Helper()
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	sockDir, err := os.MkdirTemp("", "wq")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSSHServer{
		listener: listener,
		sockDir:  sockDir,
		sockets:  map[string]string{},
		conns:    map[*ssh.ServerConn]bool{},
		visitors: map[net.Conn]bool{},
	}
	s.config = &ssh.ServerConfig{PublicKeyCallback: s.checkKey}
	s.config.AddHostKey(hostSigner)
	s.wg.Go(s.acceptLoop)
	t.Cleanup(s.close)
	return s
}

// authorize lets user log in with the key of publicKeyLine (a .pub line).
func (s *fakeSSHServer) authorize(t *testing.T, user, publicKeyLine string) {
	t.Helper()
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKeyLine))
	if err != nil {
		t.Fatalf("bad public key %q: %v", publicKeyLine, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user, s.key = user, key
}

func (s *fakeSSHServer) checkKey(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key != nil && meta.User() == s.user && bytes.Equal(key.Marshal(), s.key.Marshal()) {
		return nil, nil
	}
	return nil, fmt.Errorf("key not accepted for user %q", meta.User())
}

// dial is the manager's dial function. It records the address the manager
// asked for and connects to this server instead.
func (s *fakeSSHServer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	s.mu.Lock()
	s.dialed = append(s.dialed, addr)
	fail := s.failDials > 0
	if fail {
		s.failDials--
	}
	s.mu.Unlock()
	if fail {
		return nil, errors.New("connection refused")
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, s.listener.Addr().String())
}

// failNextDials makes the next n dials fail, as when the server is down.
func (s *fakeSSHServer) failNextDials(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failDials = n
}

// dropConnections closes every SSH connection, as when the network goes down.
func (s *fakeSSHServer) dropConnections() {
	s.mu.Lock()
	conns := make([]*ssh.ServerConn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

func (s *fakeSSHServer) close() {
	s.mu.Lock()
	s.closing = true
	visitors := make([]net.Conn, 0, len(s.visitors))
	for conn := range s.visitors {
		visitors = append(visitors, conn)
	}
	s.mu.Unlock()
	s.listener.Close()
	s.dropConnections()
	for _, conn := range visitors {
		conn.Close()
	}
	s.wg.Wait()
	os.RemoveAll(s.sockDir)
}

func (s *fakeSSHServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Go(func() { s.serveConn(conn) })
	}
}

// serveConn runs one SSH connection until it ends.
func (s *fakeSSHServer) serveConn(netConn net.Conn) {
	conn, channels, requests, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		netConn.Close()
		return
	}
	s.mu.Lock()
	s.logins = append(s.logins, conn.User())
	s.conns[conn] = true
	closing := s.closing
	s.mu.Unlock()
	if closing {
		conn.Close()
	}
	s.wg.Go(func() {
		for channel := range channels {
			channel.Reject(ssh.Prohibited, "this server only forwards sockets")
		}
	})

	listeners := map[string]net.Listener{}
	for request := range requests {
		var message struct{ SocketPath string }
		switch request.Type {
		case "streamlocal-forward@openssh.com":
			if ssh.Unmarshal(request.Payload, &message) != nil {
				request.Reply(false, nil)
				continue
			}
			s.mu.Lock()
			socketPath := filepath.Join(s.sockDir, fmt.Sprintf("s%d", len(s.forwards)))
			s.forwards = append(s.forwards, message.SocketPath)
			s.sockets[message.SocketPath] = socketPath
			s.mu.Unlock()
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				request.Reply(false, nil)
				continue
			}
			listeners[message.SocketPath] = listener
			request.Reply(true, nil)
			s.wg.Go(func() { s.serveSocket(conn, message.SocketPath, listener) })
		case "cancel-streamlocal-forward@openssh.com":
			if ssh.Unmarshal(request.Payload, &message) != nil {
				request.Reply(false, nil)
				continue
			}
			s.mu.Lock()
			s.cancels = append(s.cancels, message.SocketPath)
			s.mu.Unlock()
			if listener := listeners[message.SocketPath]; listener != nil {
				listener.Close()
				delete(listeners, message.SocketPath)
			}
			request.Reply(true, nil)
		default:
			request.Reply(false, nil)
		}
	}
	// The connection has ended.
	for _, listener := range listeners {
		listener.Close()
	}
	conn.Close()
	s.mu.Lock()
	delete(s.conns, conn)
	s.ended++
	s.mu.Unlock()
}

// serveSocket sends each connection to a socket to the client over SSH.
func (s *fakeSSHServer) serveSocket(conn *ssh.ServerConn, askedPath string, listener net.Listener) {
	for {
		visitor, err := listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.visitors[visitor] = true
		s.mu.Unlock()
		s.wg.Go(func() {
			defer func() {
				s.mu.Lock()
				delete(s.visitors, visitor)
				s.mu.Unlock()
				visitor.Close()
			}()
			payload := ssh.Marshal(&struct{ SocketPath, Reserved string }{askedPath, ""})
			channel, requests, err := conn.OpenChannel("forwarded-streamlocal@openssh.com", payload)
			if err != nil {
				return
			}
			s.wg.Go(func() { ssh.DiscardRequests(requests) })
			pipe(visitor, channel)
		})
	}
}

// pipe copies data both ways between a visitor and an SSH channel. When the
// visitor stops sending, the channel gets an end of data; when the channel's
// data ends, both are closed.
func pipe(visitor net.Conn, channel ssh.Channel) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Copy(channel, visitor)
		channel.CloseWrite()
	}()
	io.Copy(visitor, channel)
	visitor.Close()
	channel.Close()
	<-done
}

// socketFor returns the socket the server made for a path the client asked
// to listen on, or "" when the client has not asked for it.
func (s *fakeSSHServer) socketFor(askedPath string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sockets[askedPath]
}

// dialSocket connects to the socket for askedPath, as the tunnel server's web
// server does for a visitor of the public URL.
func (s *fakeSSHServer) dialSocket(t *testing.T, askedPath string) net.Conn {
	t.Helper()
	socketPath := s.socketFor(askedPath)
	if socketPath == "" {
		t.Fatalf("the client did not ask to listen on %s; it asked for %q", askedPath, s.forwardPaths())
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("connecting to the socket for %s: %v", askedPath, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func (s *fakeSSHServer) dialedAddrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.dialed)
}

func (s *fakeSSHServer) loginUsers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logins)
}

func (s *fakeSSHServer) forwardPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.forwards)
}

func (s *fakeSSHServer) cancelPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.cancels)
}

// waitEnded waits until n SSH connections have ended.
func (s *fakeSSHServer) waitEnded(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		s.mu.Lock()
		ended := s.ended
		s.mu.Unlock()
		if ended >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d SSH connections ended, want %d", ended, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
