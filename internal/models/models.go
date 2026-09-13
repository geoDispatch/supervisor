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
	Flood      DisasterType = "flood"    // accepted by the schema, not implemented (see SupportedDisasterTypes)
	Heatwave   DisasterType = "heatwave" // accepted by the schema, not implemented (see SupportedDisasterTypes)
)

// Capability values reported per disaster type by GET /capabilities.
const (
	CapabilityOperational = "operational"
	CapabilityUnsupported = "unsupported"
)

// SupportedDisasterTypes lists every disaster type the contract knows and
// whether this supervisor can actually run a pipeline for it. /sensor accepts
// only the operational ones. Read-only: never mutate it at runtime.
var SupportedDisasterTypes = map[DisasterType]string{
	Earthquake: CapabilityOperational,
	Flood:      CapabilityUnsupported,
	Heatwave:   CapabilityUnsupported,
}

// DisasterCapability returns "operational" or "unsupported" for t. Types the
// contract does not know are "unsupported" too — callers that must tell
// "unknown" from "not implemented" check SupportedDisasterTypes directly.
func DisasterCapability(t DisasterType) string {
	if c, ok := SupportedDisasterTypes[t]; ok {
		return c
	}
	return CapabilityUnsupported
}

// ── Zone types ────────────────────────────────────────────────

type ZoneType string

const (
	ZoneRed    ZoneType = "red"
	ZoneOrange ZoneType = "orange"
	ZoneGreen  ZoneType = "green"
)

// ValidZone reports whether z is one of the three zone values.
func ValidZone(z ZoneType) bool {
	return ZoneRank(z) > 0
}

// ZoneRank orders zones by severity (green 1 < orange 2 < red 3) so an AI
// escalation can be checked as "strictly more severe". Invalid zones rank 0.
func ZoneRank(z ZoneType) int {
	switch z {
	case ZoneGreen:
		return 1
	case ZoneOrange:
		return 2
	case ZoneRed:
		return 3
	}
	return 0
}

// ── Action types ───────────────────────────────────────────────

type ActionType string

const (
	ActionSMS    ActionType = "sms"
	ActionRescue ActionType = "rescue_flag"
	ActionBoth   ActionType = "both"
	ActionNone   ActionType = "none"
)

// ValidAction reports whether a is one of the four action values.
func ValidAction(a ActionType) bool {
	switch a {
	case ActionSMS, ActionRescue, ActionBoth, ActionNone:
		return true
	}
	return false
}

// ── Network congestion levels (from CAMARA Congestion API) ────

type CongestionLevel string

const (
	CongestionLow      CongestionLevel = "LOW"
	CongestionMedium   CongestionLevel = "MEDIUM"
	CongestionHigh     CongestionLevel = "HIGH"
	CongestionCritical CongestionLevel = "CRITICAL"
	CongestionUnknown  CongestionLevel = "UNKNOWN"
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
	EventID        string         `json:"event_id"`
	DisasterType   DisasterType   `json:"disaster_type"`
	Timestamp      int64          `json:"timestamp"` // Unix ms
	Severity       float64        `json:"severity"`  // Richter for quake
	Epicenter      Coordinates    `json:"epicenter"`
	RadiusKm       float64        `json:"radius_km"` // estimated affected radius
	DepthKm        float64        `json:"depth_km"`  // earthquake depth (0 for others)
	AftershockRisk AftershockRisk `json:"aftershock_risk"`
	TsunamiRisk    bool           `json:"tsunami_risk"` // earthquake coastal events
}

// ─────────────────────────────────────────────────────────────
// RAW CAMARA API RESPONSES
// These match Nokia NaC exact response shapes
// ─────────────────────────────────────────────────────────────

// Location Retrieval API response
type CAMARALocationArea struct {
	AreaType string      `json:"areaType"` // always "CIRCLE" in Nokia NaC
	Center   Coordinates `json:"center"`
	Radius   float64     `json:"radius"` // accuracy in metres (~500m urban)
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
// Endpoint: POST $AGENT_URL (Python agent :8000/decide, mock agent :8082/decide)
// One request per zone batch (red / orange / green separately)
// ─────────────────────────────────────────────────────────────

type AgentRequest struct {
	EventID        string         `json:"event_id"`
	DisasterType   DisasterType   `json:"disaster_type"`
	Severity       float64        `json:"severity"`
	AftershockRisk AftershockRisk `json:"aftershock_risk"`
	TsunamiRisk    bool           `json:"tsunami_risk"`

	// Devices in this batch (one zone only per request)
	Zone       ZoneType        `json:"zone"`        // which zone this batch is
	BatchIndex int             `json:"batch_index"` // request counter from 0 (zones may span several batches)
	Devices    []TriagedDevice `json:"devices"`

	// Environment context
	NearestShelters []Shelter     `json:"nearest_shelters"`
	NetworkStatus   NetworkStatus `json:"network_status"`
}

// MarshalJSON sends nil slices as []. The agent schema requires arrays, and
// "no shelters" (none nearby or the DB query failed) is a normal case.
func (r AgentRequest) MarshalJSON() ([]byte, error) {
	type plain AgentRequest // same fields, no MarshalJSON: avoids recursion
	p := plain(r)
	if p.Devices == nil {
		p.Devices = []TriagedDevice{}
	}
	if p.NearestShelters == nil {
		p.NearestShelters = []Shelter{}
	}
	return json.Marshal(p)
}

// ─────────────────────────────────────────────────────────────
// AGENT RESPONSE
// Python AI agent → Go supervisor
// Go validates it, then dispatches and publishes device updates
// ─────────────────────────────────────────────────────────────

type DeviceDecision struct {
	Phone          string     `json:"phone"`
	ZoneConfirmed  ZoneType   `json:"zone_confirmed"`
	ZoneEscalated  bool       `json:"zone_escalated"`
	Action         ActionType `json:"action"`
	SMSMessage     string     `json:"sms_message"`
	RescuePriority int        `json:"rescue_priority"`
	Confidence     float64    `json:"confidence"`
	Reasoning      string     `json:"reasoning"` // audit log only — never published to the dashboard
}

type AgentResponse struct {
	EventID      string           `json:"event_id"`
	Zone         ZoneType         `json:"zone"`
	Decisions    []DeviceDecision `json:"decisions"`
	GovNarrative string           `json:"gov_narrative"`
	RequestQoS   bool             `json:"request_qos"`
	Confidence   float64          `json:"confidence"`
}
