// Package dashboard is the supervisor's WebSocket side (contract v2,
// contracts/examples/ws_update.json). The Hub is two things behind one lock:
//
//   - an event recorder that numbers every frame of the held event (seq) and
//     keeps what a late client needs to rebuild the full state, and
//   - a per-client fan-out: each connection has its own bounded queue and
//     writer goroutine, so a slow browser never slows the pipeline or the
//     other browsers.
//
// A new connection is registered and handed its snapshot under the same lock
// that serialises publishing, so the snapshot and the live stream meet with
// no gap and no duplicate.
//
// Files: hub.go (Hub, options, stats, heartbeat, shutdown), broadcast.go
// (publishing, recorder, snapshot) and server.go (ServeWS and the per-client
// reader/writer).
package dashboard

import (
	"log"
	"sync"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/origin"
	"github.com/gorilla/websocket"
)

// Defaults for zero Options fields; they match the config defaults (spec §2.5).
const (
	defaultQueueLimit   = 65536
	defaultWriteTimeout = 10 * time.Second
	defaultHeartbeat    = 15 * time.Second
	defaultPingInterval = 30 * time.Second
)

// Options configures a Hub. Zero values take the defaults above.
type Options struct {
	QueueLimit   int           // WS_CLIENT_QUEUE_LIMIT: frames queued per client before it is disconnected as slow
	WriteTimeout time.Duration // WS_WRITE_TIMEOUT_SEC: per-frame write deadline
	Heartbeat    time.Duration // WS_HEARTBEAT_SEC: heartbeat period
	PingInterval time.Duration // WebSocket ping period; the read deadline is twice this
	Origins      origin.Policy // who may upgrade; the zero Policy allows no-Origin and same-origin only
	// Now stamps frame timestamps (tests pin it); nil means time.Now. Network
	// deadlines always use the real clock.
	Now func() time.Time
}

// Stats are the counters reported under "websocket" by GET /health.
type Stats struct {
	Clients               int   `json:"clients"`                 // connected right now
	FramesPublished       int64 `json:"frames_published"`        // event frames (seq ≥ 1) issued; control frames are not counted
	SlowClientDisconnects int64 `json:"slow_client_disconnects"` // clients closed with 1013 for exceeding QueueLimit
	MaxQueueDepth         int   `json:"max_queue_depth"`         // deepest any client queue has been
}

// Hub records the held event and fans its frames out to every connection.
type Hub struct {
	opts     Options
	upgrader websocket.Upgrader

	// mu serialises seq assignment, recorder updates, fan-out enqueues and
	// client registration + snapshot. Nothing under it touches the network:
	// enqueueing only appends to a client's in-memory queue.
	mu      sync.Mutex
	closed  bool
	clients map[*client]struct{}
	rec     recorder
	stats   Stats // Clients is filled in by Stats()

	wg     sync.WaitGroup // heartbeat loop + every client's reader and writer
	stopHB chan struct{}
}

// NewHub returns a running Hub (its heartbeat ticker is started). Call Close
// on shutdown.
func NewHub(opts Options) *Hub {
	if opts.QueueLimit <= 0 {
		opts.QueueLimit = defaultQueueLimit
	}
	if opts.WriteTimeout <= 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = defaultHeartbeat
	}
	if opts.PingInterval <= 0 {
		opts.PingInterval = defaultPingInterval
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	h := &Hub{
		opts:    opts,
		clients: make(map[*client]struct{}),
		rec:     newRecorder(),
		stopHB:  make(chan struct{}),
	}
	h.upgrader = websocket.Upgrader{
		// ServeWS already refused disallowed origins with a JSON 403; checking
		// again here keeps the upgrader itself from ever accepting one.
		CheckOrigin: h.opts.Origins.Allowed,
	}
	h.wg.Add(1)
	go h.heartbeatLoop()
	return h
}

// Stats returns a copy of the counters.
func (h *Hub) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.stats
	s.Clients = len(h.clients)
	return s
}

// Held reports the event the hub holds: the running one, or the last
// finished one until the next BeginEvent. eventID is "" before any event.
func (h *Hub) Held() (eventID string, lifecycle models.Lifecycle, active bool, headSeq int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rec.eventID, h.rec.lifecycle, h.rec.active, h.rec.headSeq
}

// Close stops the heartbeat, closes every client with 1001 (going away) and
// waits for their goroutines. Later connections are refused with 503. The
// recorder stays readable (Held, Snapshot) and publishing keeps recording.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.stopHB)
	cs := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		cs = append(cs, c)
		delete(h.clients, c)
	}
	h.mu.Unlock()

	for _, c := range cs {
		c.stop(websocket.CloseGoingAway, "supervisor shutting down")
	}
	h.wg.Wait()
}

// heartbeatLoop enqueues a heartbeat to every client each period so clients
// can tell "quiet but alive" from "stalled" and spot lost frames (head_seq).
func (h *Hub) heartbeatLoop() {
	defer h.wg.Done()
	t := time.NewTicker(h.opts.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-h.stopHB:
			return
		case <-t.C:
			h.heartbeat()
		}
	}
}

func (h *Hub) heartbeat() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) == 0 {
		return
	}
	frame := controlFrame(models.TypeHeartbeat, h.rec.eventID, h.nowMs(), models.HeartbeatPayload{
		HeadSeq:   h.rec.headSeq,
		Active:    h.rec.active,
		Lifecycle: h.rec.lifecycle,
	})
	h.fanOutLocked(frame)
}

// fanOutLocked enqueues frame on every client. A client whose queue is full
// is disconnected as slow instead of being skipped: skipping would leave it
// with a silent gap, while a close makes it reconnect and get a fresh
// snapshot. Caller holds h.mu.
func (h *Hub) fanOutLocked(frame []byte) {
	for c := range h.clients {
		depth, ok := c.enqueue(frame, h.opts.QueueLimit)
		if !ok {
			h.dropSlowLocked(c)
			continue
		}
		h.noteDepthLocked(depth)
	}
}

// dropSlowLocked unregisters c and tells its writer to close it with 1013.
// The close itself happens in the writer goroutine, outside h.mu.
func (h *Hub) dropSlowLocked(c *client) {
	delete(h.clients, c)
	h.stats.SlowClientDisconnects++
	log.Printf("dashboard: ws client %s disconnected: queue limit %d exceeded (client too slow); it will get a fresh snapshot on reconnect",
		c.addr, h.opts.QueueLimit)
	c.stop(websocket.CloseTryAgainLater, "backpressure: client too slow")
}

func (h *Hub) noteDepthLocked(depth int) {
	if depth > h.stats.MaxQueueDepth {
		h.stats.MaxQueueDepth = depth
	}
}

// unregister forgets c; it is a no-op if c was already removed (slow
// disconnect or Close).
func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) nowMs() int64 { return h.opts.Now().UnixMilli() }
