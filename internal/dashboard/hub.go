package dashboard

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/geodispatch/supervisor/internal/models"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Hub struct {
	clients map[*websocket.Conn]bool
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*websocket.Conn]bool)}
}

func (h *Hub) Run() {}

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
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":      msgType,
		"event_id":  eventID,
		"timestamp": timestamp,
		"payload":   payload,
	})

	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// Broadcast wrappers matching main.go calls
func (h *Hub) BroadcastEventStart(input *models.SensorInput) {
	h.broadcast("event_start", models.EventStart{
		DisasterType: input.DisasterType, Severity: input.Severity,
		Epicenter: input.Epicenter, RadiusKm: input.RadiusKm,
		TsunamiRisk: input.TsunamiRisk, AftershockRisk: input.AftershockRisk,
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
	h.broadcast("narrative_update", map[string]string{"zone": string(zone), "narrative": narrative}, eventID, 0)
}