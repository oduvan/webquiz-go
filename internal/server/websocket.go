package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The WebSocket routes. Each handler builds its initial state under s.mu and
// calls serveWebSocket: websocketLiveStats (livestats.go) and websocketAdmin
// (admin_socket.go).

// broadcastLiveStats sends a message to every live stats page
// (broadcast_to_websockets). Call it while holding s.mu, right after the
// state change it reports: a page that connects at the same moment then gets
// the change either in its initial state or as this message, never neither.
// It only queues the message, so it is cheap under the lock.
func (s *Server) broadcastLiveStats(message any) { s.liveStatsClients.broadcast(message) }

// broadcastAdmin sends a message to every admin page
// (broadcast_to_admin_websockets), with the same rules as broadcastLiveStats.
func (s *Server) broadcastAdmin(message any) { s.adminClients.broadcast(message) }

// wsQueueSize is how many messages may wait for one client. A client that
// falls further behind is disconnected; its page connects again and gets a
// fresh initial state.
const wsQueueSize = 256

// wsWriteTimeout is how long one message may take to send.
const wsWriteTimeout = 10 * time.Second

// wsHub is the list of connected clients of one WebSocket route, like
// Python's websocket_clients and admin_websocket_clients lists.
type wsHub struct {
	connName string // for "New <connName> client connected"
	sendName string // for "Failed to send message to <sendName> client"
	log      *slog.Logger

	mu      sync.Mutex
	clients map[*wsClient]bool
	closed  bool
	wg      sync.WaitGroup // the goroutines of the clients
}

func newWSHub(connName, sendName string, log *slog.Logger) *wsHub {
	return &wsHub{connName: connName, sendName: sendName, log: log, clients: map[*wsClient]bool{}}
}

// wsClient is one connection. Messages wait in send until its writer
// goroutine sends them, in order.
type wsClient struct {
	conn     *websocket.Conn
	send     chan []byte
	done     chan struct{}
	stopOnce sync.Once
}

func (c *wsClient) stop() { c.stopOnce.Do(func() { close(c.done) }) }

// queue puts a message in the client's queue without waiting; false when
// the queue is full.
func (c *wsClient) queue(message []byte) bool {
	select {
	case c.send <- message:
		return true
	default:
		return false
	}
}

// add registers a client and queues its initial message first. It returns
// false when the hub is closed. Each added client is counted until done.
func (h *wsHub) add(c *wsClient, initial []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	if initial != nil {
		c.queue(initial)
	}
	h.clients[c] = true
	h.wg.Add(1)
	return true
}

// done removes a client that has finished.
func (h *wsHub) done(c *wsClient) {
	h.remove(c)
	h.wg.Done()
}

func (h *wsHub) remove(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// count is the number of connected clients.
func (h *wsHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// broadcast queues a message (encoded once as JSON) for every client.
// Clients whose queue is full are disconnected.
func (h *wsHub) broadcast(message any) {
	data, err := encodeJSON(message)
	if err != nil {
		h.log.Error(fmt.Sprintf("Failed to encode a %s message: %v", h.sendName, err))
		return
	}
	for _, c := range h.queueAll(data) {
		h.log.Warn(fmt.Sprintf("Failed to send message to %s client: too many messages are waiting", h.sendName))
		c.stop()
		_ = c.conn.CloseNow()
	}
}

// queueAll queues data for every client and returns (and forgets) the
// clients whose queue was full.
func (h *wsHub) queueAll(data []byte) (dropped []*wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if !c.queue(data) {
			delete(h.clients, c)
			dropped = append(dropped, c)
		}
	}
	return dropped
}

// closeAll disconnects every client, turns new clients away and waits until
// the goroutines of the clients have finished.
func (h *wsHub) closeAll() {
	for _, c := range h.closeHub() {
		c.stop()
		_ = c.conn.CloseNow()
	}
	h.wg.Wait()
}

// closeHub turns new clients away and returns the connected ones.
func (h *wsHub) closeHub() []*wsClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	return clients
}

// serveWebSocket is _handle_websocket_connection: it accepts the WebSocket,
// sends initialState() first, then every broadcast of the hub, and answers
// {"type": "ping"} with {"type": "pong"} until the client leaves.
// initialState runs under s.mu, together with joining the hub (see
// broadcastLiveStats). Any origin is accepted, as aiohttp did; the routes
// are for the local network only.
func (s *Server) serveWebSocket(w http.ResponseWriter, r *http.Request, hub *wsHub, initialState func() any) error {
	return s.serveWebSocketJoin(w, r, hub, func(c *wsClient) bool { return s.joinHub(hub, c, initialState) })
}

// serveWebSocketJoin is serveWebSocket with its own join step: join adds
// the client to the hub with its initial message (see joinHub) and tells
// whether it did.
func (s *Server) serveWebSocketJoin(w http.ResponseWriter, r *http.Request, hub *wsHub, join func(c *wsClient) bool) error {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return nil // Accept has answered the request
	}
	c := &wsClient{conn: conn, send: make(chan []byte, wsQueueSize), done: make(chan struct{})}
	joined := false
	defer func() {
		if !joined {
			_ = conn.CloseNow() // also when initialState panicked
		}
	}()
	if joined = join(c); !joined {
		return nil
	}
	s.log.Info(fmt.Sprintf("New %s client connected. Total clients: %d", hub.connName, hub.count()))

	ctx, cancel := context.WithCancel(s.ctx)
	var writer sync.WaitGroup
	writer.Go(func() { s.writeMessages(ctx, c, hub) })
	defer func() {
		c.stop()
		_ = conn.CloseNow()
		cancel()
		writer.Wait()
		hub.done(c)
		s.log.Info(fmt.Sprintf("%s client disconnected. Total clients: %d", hub.connName, hub.count()))
	}()

	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return nil // the client left, or the server closes
		}
		if kind != websocket.MessageText {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			s.log.Warn(fmt.Sprintf("Error processing %s message: %v", hub.connName, err))
			continue
		}
		if message["type"] == "ping" && !c.queue([]byte(`{"type":"pong"}`)) {
			return nil
		}
	}
}

// joinHub takes the initial state and adds the client to the hub in one
// step under mu (see broadcastLiveStats). It is false when the hub is closed.
func (s *Server) joinHub(hub *wsHub, c *wsClient, initialState func() any) bool {
	s.mu.Lock()
	defer s.mu.Unlock() // a panic in initialState must not leave mu locked
	initial, err := encodeJSON(initialState())
	if err != nil {
		s.log.Error(fmt.Sprintf("Error sending initial state to %s client: %v", hub.connName, err))
		initial = nil
	}
	return hub.add(c, initial)
}

// writeMessages sends the client's queued messages in order until the
// client stops or a send fails.
func (s *Server) writeMessages(ctx context.Context, c *wsClient, hub *wsHub) {
	for {
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		case message := <-c.send:
			writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := c.conn.Write(writeCtx, websocket.MessageText, message)
			cancel()
			if err != nil {
				s.log.Warn(fmt.Sprintf("Failed to send message to %s client: %v", hub.sendName, err))
				c.stop()
				_ = c.conn.CloseNow()
				return
			}
		}
	}
}
