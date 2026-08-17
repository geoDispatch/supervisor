// Rule: Go calculates zones. AI decides actions. Never reversed.
// Rule: All timestamps Unix milliseconds (int64)
// Rule: All phones E.164 format (+212XXXXXXXXX)
// Rule: All zones lowercase "red" | "orange" | "green"

package models
import "encoding/json"

type Coordinates struct {
    Lat float64 `json:"latitude"`
    Lng float64 `json:"longitude"`
}

// ── Disaster Types ────────────────────────────────────────────

type DisasterType string
const (
	Earthquake DisasterType = "earthquake"
	Flood      DisasterType = "flood" 			// to be implemented later
	Heatwave   DisasterType = "heatwave"		// to be implemented later
)

// ── Zone types ────────────────────────────────────────────────

type ZoneType string
const (
	ZoneRed    ZoneType = "red"
	ZoneOrange ZoneType = "orange"
	ZoneGreen  ZoneType = "green"
)

// ── Action types ───────────────────────────────────────────────

type ActionType string
const (
	ActionSMS        ActionType = "sms"
	ActionRescue     ActionType = "rescue_flag"
	ActionBoth       ActionType = "both"
	ActionNone       ActionType = "none"
)

// ── Network congestion levels (from CAMARA Congestion API) ────

type CongestionLevel string
const (
	CongestionLow      CongestionLevel = "LOW"
	CongestionMedium   CongestionLevel = "MEDIUM"
	CongestionHigh     CongestionLevel = "HIGH"
	CongestionCritical CongestionLevel = "CRITICAL"
)

// ── QoS status (from CAMARA QoS on Demand API) ───────────────

type QoSStatus string
const (
	QoSInactive  QoSStatus = "inactive"
	QoSRequested QoSStatus = "requested"
	QoSActive    QoSStatus = "active"
	QoSFailed    QoSStatus = "failed"
)

// ── Reachability status (exact Nokia NaC values) ─────────────

type ReachabilityStatus string
const (
	ReachableData ReachabilityStatus = "CONNECTED_DATA"
	ReachableSMS  ReachabilityStatus = "CONNECTED_SMS"
	NotConnected  ReachabilityStatus = "NOT_CONNECTED"
)

// ── AftershockRisk status  ────────────────────────────────────

type AftershockRisk string
const (
    AftershockLow    AftershockRisk = "LOW"
    AftershockMedium AftershockRisk = "MEDIUM"
    AftershockHigh   AftershockRisk = "HIGH"
)

// ─────────────────────────────────────────────────────────────
// SENSOR INPUT
// What the disaster sensor POSTs to Go supervisor
// Source: USGS GeoJSON standard (earthquake)
// Endpoint: POST /sensor
// ─────────────────────────────────────────────────────────────

type SensorInput struct {
	EventID        string       	`json:"event_id"`
	DisasterType   DisasterType 	`json:"disaster_type"`
	Timestamp      int64        	`json:"timestamp"`       // Unix ms
	Severity       float64      	`json:"severity"`        // Richter for quake
	Epicenter      Coordinates  	`json:"epicenter"`
	RadiusKm       float64      	`json:"radius_km"`       // estimated affected radius
	DepthKm        float64      	`json:"depth_km"`        // earthquake depth (0 for others)
	AftershockRisk AftershockRisk   `json:"aftershock_risk"`
	TsunamiRisk    bool         	`json:"tsunami_risk"`    // earthquake coastal events
}

// ─────────────────────────────────────────────────────────────
// RAW CAMARA API RESPONSES
// These match Nokia NaC exact response shapes
// ─────────────────────────────────────────────────────────────

// Location Retrieval API response
type CAMARALocationArea struct {
	AreaType string      `json:"areaType"` // always "CIRCLE" in Nokia NaC
	Center   Coordinates `json:"center"`
	Radius   float64     `json:"radius"`   // accuracy in metres (~500m urban)
}

type CAMARALocationResponse struct {
	LastLocationTime string             `json:"lastLocationTime"` // ISO8601
	Area             CAMARALocationArea `json:"area"`
}

// Device Reachability API response
type CAMARAReachabilityResponse struct {
	LastStatusTime     string             `json:"lastStatusTime"`
	ReachabilityStatus ReachabilityStatus `json:"reachabilityStatus"`
}

// Congestion Insights API response
type CAMARACongestionResponse struct {
	Level     CongestionLevel `json:"level"`
	Timestamp string          `json:"timestamp"` // ISO8601
}

// ─────────────────────────────────────────────────────────────
// TRIAGED DEVICE
// After Go processes raw CAMARA data + haversine calculation
// ─────────────────────────────────────────────────────────────

type TriagedDevice struct {
	// Identity
	Phone string `json:"phone"` // E.164 format

	// From CAMARA Location Retrieval (raw values preserved)
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	LocationRadiusM  float64 `json:"location_radius_m"`  // accuracy
	LastLocationTime string  `json:"last_location_time"` // ISO8601

	// From CAMARA Reachability (raw values preserved)
	ReachabilityStatus ReachabilityStatus `json:"reachability_status"`
	LastStatusTime     string             `json:"last_status_time"` // ISO8601

	// Calculated by Go — never by AI, never by CAMARA
	Zone       ZoneType `json:"zone"`        // "red"|"orange"|"green"
	DistanceKm float64  `json:"distance_km"` // haversine result
}

// ─────────────────────────────────────────────────────────────
// SHELTER
// From PostGIS nearest shelter query
// Populated by scripts/seed_shelters.sql
// ─────────────────────────────────────────────────────────────

type Shelter struct {
	Name       string      `json:"name"`
	Address    string      `json:"address"`
	Location   Coordinates `json:"location"`
	DistanceKm float64     `json:"distance_km"`
	Capacity   int         `json:"capacity"`
}

// ─────────────────────────────────────────────────────────────
// NETWORK STATUS
// Aggregated from CAMARA Congestion + QoS APIs
// Tells AI agent how healthy the network is right now
// ─────────────────────────────────────────────────────────────

type NetworkStatus struct {
	CongestionLevel CongestionLevel `json:"congestion_level"`
	SMSDeliveryRate float64         `json:"sms_delivery_rate"` // 0.0-1.0
	QoSStatus       QoSStatus       `json:"qos_status"`
}

// ─────────────────────────────────────────────────────────────
// AGENT REQUEST
// Go supervisor → Python AI agent
// Endpoint: POST http://localhost:5000/decide
// One request per zone batch (red / orange / green separately)
// ─────────────────────────────────────────────────────────────

type AgentRequest struct {
	EventID        string       	`json:"event_id"`
	DisasterType   DisasterType 	`json:"disaster_type"`
	Severity       float64      	`json:"severity"`
	AftershockRisk AftershockRisk   `json:"aftershock_risk"`
	TsunamiRisk    bool         	`json:"tsunami_risk"`

	// Devices in this batch (one zone only per request)
	Zone       ZoneType        `json:"zone"`        // which zone this batch is
	BatchIndex int             `json:"batch_index"` // 0=red, 1=orange, 2=green
	Devices    []TriagedDevice `json:"devices"`

	// Environment context
	NearestShelters []Shelter 	  `json:"nearest_shelters"`
	NetworkStatus   NetworkStatus `json:"network_status"`
}

// ─────────────────────────────────────────────────────────────
// AGENT RESPONSE
// Python AI agent → Go supervisor
// Go acts on this immediately — dispatch + dashboard update
// ─────────────────────────────────────────────────────────────

type DeviceDecision struct {
    Phone          string     `json:"phone"`
    ZoneConfirmed  ZoneType   `json:"zone_confirmed"`
    ZoneEscalated  bool       `json:"zone_escalated"`
    Action         ActionType `json:"action"`
    SMSMessage     string     `json:"sms_message"`
    ShelterName    string     `json:"shelter_name"`
    RescuePriority int        `json:"rescue_priority"`
    Confidence     float64    `json:"confidence"`
    Reasoning      string     `json:"reasoning"`
}

type AgentResponse struct {
    EventID      string           `json:"event_id"`
    Zone         ZoneType         `json:"zone"`
    Decisions    []DeviceDecision `json:"decisions"`
    GovNarrative string           `json:"gov_narrative"`
    RequestQoS   bool             `json:"request_qos"`
    Confidence   float64          `json:"confidence"`
}

// ─────────────────────────────────────────────────────────────
// WEBSOCKET MESSAGES
// Go supervisor → dashboard
// ─────────────────────────────────────────────────────────────

// WSUpdate is the envelope for all WebSocket messages
// Type field tells which payload struct to expect
type WSUpdate struct {
	Type      string          `json:"type"`      // see WSType constants below
	EventID   string          `json:"event_id"`
	Timestamp int64           `json:"timestamp"` // Unix ms
	Payload   json.RawMessage `json:"payload"`   // deferred parsing based on Type
}

// WSType constants — what switches on
const (
	WSTypeEventStart      = "event_start"      // disaster detected, map initialises
	WSTypeDeviceUpdate    = "device_update"     // one dot appears/updates on map
	WSTypeZoneSummary     = "zone_summary"      // sidebar counters update
	WSTypeNarrativeUpdate = "narrative_update"  // AI situation report text
	WSTypeError           = "error"             // something failed
)

// Payload when Type = "event_start"
type EventStart struct {
    DisasterType   DisasterType 	`json:"disaster_type"`
    Severity       float64      	`json:"severity"`
    Epicenter      Coordinates  	`json:"epicenter"`
    RadiusKm       float64      	`json:"radius_km"`
    TsunamiRisk    bool         	`json:"tsunami_risk"`
    AftershockRisk AftershockRisk 	`json:"aftershock_risk"`
}

// Payload when Type = "device_update"
// One message per device as data streams in
type DeviceUpdate struct {
	Phone      string     `json:"phone"`
	Latitude   float64    `json:"latitude"`
	Longitude  float64    `json:"longitude"`
	Zone       ZoneType   `json:"zone"`
	Reachable  bool       `json:"reachable"`
	SMSSent    bool       `json:"sms_sent"`
	RescueFlag bool       `json:"rescue_flag"`
}

// Payload when Type = "zone_summary"
// Sidebar counters — sent after each zone batch completes
type ZoneSummary struct {
	RedTotal     int `json:"red_total"`
	RedReachable int `json:"red_reachable"`
	RedRescue    int `json:"red_rescue"`
	OrangeTotal  int `json:"orange_total"`
	OrangeReachable int `json:"orange_reachable"`
	GreenTotal   int `json:"green_total"`
	GreenReachable int `json:"green_reachable"`
}

// Payload when Type = "error"
// Something failed — dashboard shows warning to gov official
type ErrorCode string
const (
    ErrCAMARATimeout ErrorCode = "CAMARA_TIMEOUT"
    ErrAgentError    ErrorCode = "AGENT_ERROR"
    ErrSMSFailed     ErrorCode = "SMS_FAILED"
    ErrDBError       ErrorCode = "DB_ERROR"
    ErrQoSFailed     ErrorCode = "QOS_FAILED"
)

type ErrorUpdate struct {
    Code    ErrorCode `json:"code"`
    Message string    `json:"message"`
    Phone   string    `json:"phone"`
    Fatal   bool      `json:"fatal"`
}