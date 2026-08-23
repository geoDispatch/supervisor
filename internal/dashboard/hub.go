package dashboard

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Hub struct {
	clients    map[*websocket.Conn]bool
	mu         sync.Mutex
	msgCh      chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]bool),
		msgCh:   make(chan []byte, 512),
	}
}

// Run serializes all writes through a single goroutine — gorilla/websocket
// does not support concurrent writes so this is the correct pattern.
func (h *Hub) Run() {
	for msg := range h.msgCh {
		h.mu.Lock()
		for conn := range h.clients {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				delete(h.clients, conn)
				conn.Close()
			}
		}
		h.mu.Unlock()
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WS upgrade error:", err)
		return
	}
	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
}

func (h *Hub) broadcast(msgType string, payload interface{}, eventID string, timestamp int64) {
	data, err := json.Marshal(map[string]interface{}{
		"type":      msgType,
		"event_id":  eventID,
		"timestamp": timestamp,
		"payload":   payload,
	})
	if err != nil {
		return
	}
	select {
	case h.msgCh <- data:
	default:
		// drop if channel full — never block the pipeline
	}
}

func (h *Hub) BroadcastEventStart(input *models.SensorInput) {
	h.broadcast("event_start", models.EventStart{
		DisasterType:   string(input.DisasterType),
		Severity:       input.Severity,
		Epicenter:      input.Epicenter,
		RadiusKm:       input.RadiusKm,
		TsunamiRisk:    input.TsunamiRisk,
		AftershockRisk: string(input.AftershockRisk),
	}, input.EventID, input.Timestamp)
}

func (h *Hub) BroadcastDeviceUpdate(update models.DeviceUpdate, eventID string) {
	h.broadcast("device_update", update, eventID, 0)
}

func (h *Hub) BroadcastError(err models.ErrorUpdate, eventID string) {
	h.broadcast("error", err, eventID, 0)
}

func (h *Hub) BroadcastZoneSummary(summary models.ZoneSummary, eventID string) {
	h.broadcast("zone_summary", summary, eventID, 0)
}

func (h *Hub) BroadcastNarrative(zone models.ZoneType, narrative, eventID string) {
	h.broadcast("narrative_update", map[string]string{
		"zone":      string(zone),
		"narrative": narrative,
	}, eventID, 0)
}