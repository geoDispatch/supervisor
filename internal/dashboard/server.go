package dashboard

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// readLimit caps client→server messages. Clients have nothing to say; the
// reader exists only to process pongs and close frames.
const readLimit = 512

// ServeWS upgrades GET /ws. Disallowed origins get 403 before any upgrade.
// On success the connection is registered and handed its snapshot atomically
// (under the publishing lock), then served until it closes. ServeWS blocks for
// the life of the connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if !h.opts.Origins.Allowed(r) {
		log.Printf("dashboard: ws upgrade refused: origin %q not allowed", r.Header.Get("Origin"))
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
		return
	}
	if h.isClosed() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "shutting_down"})
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already answered with an HTTP error.
		log.Printf("dashboard: ws upgrade from %s failed: %v", r.RemoteAddr, err)
		return
	}
	c := newClient(h, conn)
	if !h.register(c) {
		// Close ran between the check above and the upgrade.
		msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "supervisor shutting down")
		_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(h.opts.WriteTimeout))
		conn.Close()
		return
	}
	go c.writeLoop()
	c.readLoop()
}

// register adds c with its snapshot as the first queued frames. Doing both
// under h.mu is what guarantees no gap and no duplicate between snapshot and
// live frames. The snapshot is exempt from QueueLimit: it is a bounded one-off
// burst, and refusing it would lock a client out of a large event for good.
func (h *Hub) register(c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	c.queue = h.snapshotLocked()
	h.noteDepthLocked(len(c.queue))
	h.clients[c] = struct{}{}
	h.wg.Add(2) // reader + writer; never after Close, which Waits
	return true
}

func (h *Hub) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// client is one connection: a bounded FIFO filled by the hub (under h.mu)
// and drained by a single writer goroutine, the only one writing data frames
// (gorilla/websocket allows one concurrent writer; WriteControl is the
// documented exception used for ping and close).
type client struct {
	hub  *Hub
	conn *websocket.Conn
	addr string // remote address, for logs

	mu      sync.Mutex
	queue   [][]byte
	stopped bool
	code    int // close code to send when stopping; 0 = just drop the connection
	text    string

	notify     chan struct{} // 1 slot: "the queue may be non-empty"
	quit       chan struct{} // closed by stop
	readerDone chan struct{} // closed when readLoop returns
}

func newClient(h *Hub, conn *websocket.Conn) *client {
	return &client{
		hub:        h,
		conn:       conn,
		addr:       conn.RemoteAddr().String(),
		notify:     make(chan struct{}, 1),
		quit:       make(chan struct{}),
		readerDone: make(chan struct{}),
	}
}

// enqueue appends frame unless the queue already holds limit frames; then it
// reports !ok and the hub disconnects the client. It never blocks.
func (c *client) enqueue(frame []byte, limit int) (depth int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return 0, true // going away; it resyncs from a snapshot on reconnect
	}
	if len(c.queue) >= limit {
		return len(c.queue), false
	}
	c.queue = append(c.queue, frame)
	select {
	case c.notify <- struct{}{}:
	default: // the writer is already due to look at the queue
	}
	return len(c.queue), true
}

// next pops the oldest queued frame.
func (c *client) next() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || len(c.queue) == 0 {
		return nil, false
	}
	f := c.queue[0]
	c.queue[0] = nil
	c.queue = c.queue[1:]
	return f, true
}

// stop asks the writer to close the connection, with a close frame when code
// is non-zero. The first call wins; it does no I/O, so the hub may call it
// under h.mu.
func (c *client) stop(code int, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true
	c.code, c.text = code, text
	c.queue = nil
	close(c.quit)
}

func (c *client) closeReason() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code, c.text
}

// writeLoop drains the queue with a write deadline per frame and pings the
// peer every PingInterval, also while the queue stays busy (the peer's pongs
// are what keep our read deadline alive).
func (c *client) writeLoop() {
	defer c.hub.wg.Done()
	ping := time.NewTicker(c.hub.opts.PingInterval)
	defer ping.Stop()
	for {
		// Stopping takes priority over anything else that is ready.
		select {
		case <-c.quit:
			c.finish()
			return
		default:
		}
		if frame, ok := c.next(); ok {
			if err := c.write(frame); err != nil {
				c.fail("write", err)
				return
			}
			select {
			case <-ping.C:
				if err := c.ping(); err != nil {
					c.fail("ping", err)
					return
				}
			default:
			}
			continue
		}
		select {
		case <-c.quit:
			c.finish()
			return
		case <-c.notify:
		case <-ping.C:
			if err := c.ping(); err != nil {
				c.fail("ping", err)
				return
			}
		}
	}
}

func (c *client) write(frame []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.hub.opts.WriteTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, frame)
}

func (c *client) ping() error {
	return c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.hub.opts.WriteTimeout))
}

// finish ends a stopped client: it sends the close frame if one was asked
// for, gives the peer up to WriteTimeout to answer the close handshake (so
// frames it has not read yet are not cut off by a TCP reset), then closes.
func (c *client) finish() {
	timeout := c.hub.opts.WriteTimeout
	if code, text := c.closeReason(); code != 0 {
		msg := websocket.FormatCloseMessage(code, text)
		if err := c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(timeout)); err == nil {
			t := time.NewTimer(timeout)
			select {
			case <-c.readerDone:
			case <-t.C:
			}
			t.Stop()
		}
	}
	c.conn.Close()
}

// fail handles a write error: the connection is dead or too slow for the
// write deadline. Closing the socket also ends readLoop.
func (c *client) fail(op string, err error) {
	if !errors.Is(err, websocket.ErrCloseSent) {
		log.Printf("dashboard: ws client %s: %s failed, disconnecting: %v", c.addr, op, err)
	}
	c.stop(0, "")
	c.hub.unregister(c)
	c.conn.Close()
}

// readLoop discards client messages and keeps the read deadline alive with
// pongs. Any error (peer closed, deadline, oversized message) ends the client.
func (c *client) readLoop() {
	defer c.hub.wg.Done()
	defer close(c.readerDone)
	deadline := 2 * c.hub.opts.PingInterval
	c.conn.SetReadLimit(readLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(deadline))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(deadline))
	})
	for {
		// NextReader discards the previous message unread.
		if _, _, err := c.conn.NextReader(); err != nil {
			break
		}
	}
	c.stop(0, "")
	c.hub.unregister(c)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
