package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// session is one SSH connection to the tunnel server, with the socket the
// server listens on for it.
type session struct {
	conn     net.Conn      // the TCP connection under SSH
	client   *ssh.Client   // the SSH connection
	listener net.Listener  // the socket on the tunnel server
	url      string        // the public URL of this connection
	done     chan struct{} // closed when the SSH connection has ended

	closeOnce sync.Once
	mu        sync.Mutex            // guards closed and locals
	closed    bool                  // close has run: no new local connections
	locals    map[net.Conn]struct{} // open connections to the local server
}

// open connects to server over SSH as user, with the private key in the file
// privateKeyPath, and asks the server to listen on socketPath. It gives up
// after loginTimeout, or when ctx ends.
func (m *Manager) open(ctx context.Context, server, privateKeyPath, user, socketPath string) (*session, error) {
	keyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// Python connected with known_hosts=None: the host key of the tunnel
		// server is not checked.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	ctx, cancel := context.WithTimeout(ctx, m.loginTimeout)
	defer cancel()
	addr := net.JoinHostPort(server, "22")
	conn, err := m.dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// The SSH login and the forward request read from conn and do not watch
	// ctx. Closing conn when ctx ends stops them.
	stopWatching := context.AfterFunc(ctx, func() { conn.Close() })
	clientConn, channels, requests, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		stopWatching()
		conn.Close()
		return nil, contextError(ctx, err)
	}
	client := ssh.NewClient(clientConn, channels, requests)
	listener, err := client.ListenUnix(socketPath)
	if err != nil {
		stopWatching()
		client.Close()
		return nil, contextError(ctx, err)
	}
	if !stopWatching() {
		// ctx ended just now, and conn is closed or about to be.
		client.Close()
		return nil, ctx.Err()
	}
	return &session{
		conn:     conn,
		client:   client,
		listener: listener,
		done:     make(chan struct{}),
		locals:   map[net.Conn]struct{}{},
	}, nil
}

// contextError returns the error of ctx when ctx has ended, because err then
// only says that the connection was closed. Otherwise it returns err.
func contextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// waitClosed waits until the SSH connection has ended, then closes done.
func (s *session) waitClosed() {
	_ = s.client.Wait()
	close(s.done)
}

// serve takes each connection that reaches the socket on the tunnel server
// and sends it to localAddr, the local WebQuiz server. It returns when the
// socket is closed. The goroutines it starts are counted in wg.
func (s *session) serve(wg *sync.WaitGroup, localAddr string) {
	for {
		remote, err := s.listener.Accept()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			// This one connection could not be opened. Keep taking the others:
			// the SSH library stalls the connection when they are not taken.
			continue
		}
		wg.Go(func() { s.forward(remote, localAddr) })
	}
}

// forward copies data both ways between a visitor's connection and the local
// WebQuiz server, until both directions have ended.
func (s *session) forward(remote net.Conn, localAddr string) {
	defer remote.Close()
	local, err := net.Dial("tcp", localAddr)
	if err != nil {
		return
	}
	if !s.track(local) {
		local.Close()
		return
	}
	defer s.untrack(local)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(local, remote)
		closeWrite(local)
	}()
	_, _ = io.Copy(remote, local)
	closeWrite(remote)
	<-done
}

// closeWrite tells the other side that no more data comes, while the
// connection can still receive.
func closeWrite(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = c.CloseWrite()
	}
}

// track remembers an open connection to the local server, so that close can
// end it. It returns false when the session is already closed.
func (s *session) track(local net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.locals[local] = struct{}{}
	return true
}

// untrack forgets a connection to the local server and closes it.
func (s *session) untrack(local net.Conn) {
	s.mu.Lock()
	delete(s.locals, local)
	s.mu.Unlock()
	local.Close()
}

// close closes the socket on the tunnel server, then the SSH connection, as
// Python did, and then the open connections to the local server. The tunnel
// server gets at most timeout to confirm that it closed the socket: on a dead
// link that answer never comes.
func (s *session) close(timeout time.Duration) {
	s.closeOnce.Do(func() {
		_ = s.conn.SetDeadline(time.Now().Add(timeout))
		_ = s.listener.Close()
		_ = s.client.Close()
		s.mu.Lock()
		s.closed = true
		locals := make([]net.Conn, 0, len(s.locals))
		for local := range s.locals {
			locals = append(locals, local)
		}
		s.mu.Unlock()
		for _, local := range locals {
			local.Close()
		}
	})
}
