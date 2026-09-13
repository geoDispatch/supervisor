package models

import "encoding/json"

// ─────────────────────────────────────────────────────────────
// WEBSOCKET CONTRACT v2
// Go supervisor → dashboard, GET /ws. Canonical schema:
// contracts/examples/ws_update.json. JSON tags here must match it exactly;
// the contracts drift test compares the field sets.
//
// Nullable fields are pointers WITHOUT omitempty so they marshal as null —
// the contract requires every field to be present.
// ─────────────────────────────────────────────────────────────

// ContractVersion is the value of the envelope "v" field and of
// "contract_version" in HTTP replies.
const ContractVersion = 2

// Envelope wraps every frame. All seven fields are always present.
type Envelope struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`     // one of the Type* constants
	EventID   string          `json:"event_id"` // "" only on control frames while no event is held
	Seq       int64           `json:"seq"`      // per-event, event_start = 1; control frames = 0
	Timestamp int64           `json:"timestamp"`
	Replay    bool            `json:"replay"` // true only inside a connection snapshot
	Payload   json.RawMessage `json:"payload"`
}

// Frame types. Event frames carry seq ≥ 1; control frames carry seq 0.
const (
	TypeEventStart      = "event_start"
	TypeEventContext    = "event_context"
	TypeDeviceUpdate    = "device_update"
	TypeZoneSummary     = "zone_summary"
	TypeNarrativeUpdate = "narrative_update"
	TypeError           = "error"
	TypeEventComplete   = "event_complete"

	TypeSnapshotBegin = "snapshot_begin"
	TypeSnapshotEnd   = "snapshot_end"
	TypeHeartbeat     = "heartbeat"
)

// IsControlType reports whether t is a control frame type (seq 0, may carry
// event_id ""). Every other known type is an event frame.
func IsControlType(t string) bool {
	return t == TypeSnapshotBegin || t == TypeSnapshotEnd || t == TypeHeartbeat
}

// ── Enums ─────────────────────────────────────────────────────

// Stage is where a device is in the pipeline.
type Stage string

const (
	StageTriaged        Stage = "triaged" // located, awaiting the AI
	StageDecided        Stage = "decided"
	StageDecisionFailed Stage = "decision_failed"
)

// SMSStatus is what actually happened to a device's SMS.
type SMSStatus string

const (
	SMSStatusNotRequested  SMSStatus = "not_requested"
	SMSStatusSent          SMSStatus = "sent"           // the gateway accepted it
	SMSStatusFailed        SMSStatus = "failed"         // the gateway rejected it or was unreachable
	SMSStatusNotConfigured SMSStatus = "not_configured" // AI asked for SMS but no gateway exists: NOTHING was sent
)

// RescueStatus is whether a rescue flag was persisted.
type RescueStatus string

const (
	RescueStatusNotRequested RescueStatus = "not_requested"
	RescueStatusRecorded     RescueStatus = "recorded" // row written to rescue_flags
	RescueStatusFailed       RescueStatus = "failed"
)

// Lifecycle is the state of the held event. The terminal values double as
// event_complete.status.
type Lifecycle string

const (
	LifecycleIdle                  Lifecycle = "idle"
	LifecycleRunning               Lifecycle = "running"
	LifecycleCompleted             Lifecycle = "completed"
	LifecycleCompletedWithFailures Lifecycle = "completed_with_failures"
	LifecycleNoDevices             Lifecycle = "no_devices"
	LifecycleFailed                Lifecycle = "failed"
)

// Terminal reports whether l is a valid event_complete status.
func (l Lifecycle) Terminal() bool {
	switch l {
	case LifecycleCompleted, LifecycleCompletedWithFailures, LifecycleNoDevices, LifecycleFailed:
		return true
	}
	return false
}

// ErrorCode classifies an error frame.
type ErrorCode string

const (
	ErrCAMARATimeout        ErrorCode = "CAMARA_TIMEOUT"
	ErrCAMARAError          ErrorCode = "CAMARA_ERROR"
	ErrAgentError           ErrorCode = "AGENT_ERROR"            // transport / HTTP failure
	ErrAgentInvalidResponse ErrorCode = "AGENT_INVALID_RESPONSE" // reply failed ValidateAgentResponse
	ErrSMSFailed            ErrorCode = "SMS_FAILED"
	ErrDBError              ErrorCode = "DB_ERROR"
	ErrQoSFailed            ErrorCode = "QOS_FAILED"
	ErrInternalError        ErrorCode = "INTERNAL_ERROR" // e.g. pipeline deadline exceeded
)

// ErrorStage is the pipeline step an error frame comes from.
type ErrorStage string

const (
	ErrorStageLookup   ErrorStage = "lookup"
	ErrorStageContext  ErrorStage = "context"
	ErrorStageTriage   ErrorStage = "triage"
	ErrorStageDecision ErrorStage = "decision"
	ErrorStageDispatch ErrorStage = "dispatch"
	ErrorStagePipeline ErrorStage = "pipeline"
)

// event_context string values.
const (
	SheltersStatusOK          = "ok"
	SheltersStatusUnavailable = "unavailable" // the shelter DB query failed

	// Network sources are DECLARED by configuration; nothing verifies them.
	NetworkSourceMockCAMARA = "mock_camara"
	NetworkSourceNokiaNAC   = "nokia_nac"

	SMSGatewayConfigured    = "configured"
	SMSGatewayNotConfigured = "not_configured"
)

// ── Payloads ──────────────────────────────────────────────────

// ZoneBands holds the OUTER edge of each band as a fraction of radius_km.
type ZoneBands struct {
	Red    float64 `json:"red"`
	Orange float64 `json:"orange"`
	Green  float64 `json:"green"`
}

// EventStartPayload — type event_start (seq 1).
type EventStartPayload struct {
	DisasterType    DisasterType   `json:"disaster_type"`
	Severity        float64        `json:"severity"`
	Epicenter       Coordinates    `json:"epicenter"`
	RadiusKm        float64        `json:"radius_km"`
	DepthKm         float64        `json:"depth_km"`
	TsunamiRisk     bool           `json:"tsunami_risk"`
	AftershockRisk  AftershockRisk `json:"aftershock_risk"`
	SensorTimestamp int64          `json:"sensor_timestamp"` // SensorInput.Timestamp, Unix ms
	ZoneBands       ZoneBands      `json:"zone_bands"`
}

// NetworkContext is the network part of event_context.
type NetworkContext struct {
	CongestionLevel CongestionLevel `json:"congestion_level"`
	QoSStatus       QoSStatus       `json:"qos_status"`
}

// EventContextPayload — type event_context. Latest replaces the previous one.
type EventContextPayload struct {
	DevicesInRadius int            `json:"devices_in_radius"`
	SheltersStatus  string         `json:"shelters_status"` // SheltersStatus* constants
	Shelters        []Shelter      `json:"shelters"`        // ≤ 3; marshals as [] when nil
	Network         NetworkContext `json:"network"`
	NetworkSource   string         `json:"network_source"` // NetworkSource* constants
	SMSGateway      string         `json:"sms_gateway"`    // SMSGateway* constants
}

// MarshalJSON sends a nil Shelters slice as [] — the contract never allows null.
func (p EventContextPayload) MarshalJSON() ([]byte, error) {
	type plain EventContextPayload // same fields, no MarshalJSON: avoids recursion
	q := plain(p)
	if q.Shelters == nil {
		q.Shelters = []Shelter{}
	}
	return json.Marshal(q)
}

// DeviceUpdatePayload — type device_update. Full current state of one device;
// it replaces the previous frame for the same phone. Never carries the AI
// reasoning, the SMS text or a shelter name.
type DeviceUpdatePayload struct {
	Phone               string             `json:"phone"`
	Latitude            float64            `json:"latitude"`
	Longitude           float64            `json:"longitude"`
	LocationAccuracyM   float64            `json:"location_accuracy_m"`
	Zone                ZoneType           `json:"zone"`        // Go haversine band — authoritative
	DistanceKm          float64            `json:"distance_km"` // Go haversine
	Reachable           bool               `json:"reachable"`   // ReachabilityStatus != NOT_CONNECTED
	ReachabilityStatus  ReachabilityStatus `json:"reachability_status"`
	ReachabilityAssumed bool               `json:"reachability_assumed"` // lookup failed, NOT_CONNECTED assumed
	Stage               Stage              `json:"stage"`
	Action              *ActionType        `json:"action"` // null unless Stage == decided
	ZoneEscalated       bool               `json:"zone_escalated"`
	EscalatedZone       *ZoneType          `json:"escalated_zone"` // null unless ZoneEscalated
	RescuePriority      int                `json:"rescue_priority"`
	Confidence          *float64           `json:"confidence"` // null unless Stage == decided
	SMSStatus           SMSStatus          `json:"sms_status"`
	SMSSent             bool               `json:"sms_sent"`    // SMSStatus == sent
	RescueFlag          bool               `json:"rescue_flag"` // Action ∈ {rescue_flag, both}, any zone
	RescueStatus        RescueStatus       `json:"rescue_status"`
}

// ZoneStats are the cumulative counters of one zone.
type ZoneStats struct {
	Total          int `json:"total"`
	Reachable      int `json:"reachable"`
	Unreachable    int `json:"unreachable"`
	Decided        int `json:"decided"`
	DecisionFailed int `json:"decision_failed"`
	SMSSent        int `json:"sms_sent"`
	SMSFailed      int `json:"sms_failed"`
	RescueFlagged  int `json:"rescue_flagged"`
}

// ZoneSummaryPayload — type zone_summary. Cumulative replacement state
// computed from the latest state of every triaged device (not an increment).
type ZoneSummaryPayload struct {
	Red             ZoneStats `json:"red"`
	Orange          ZoneStats `json:"orange"`
	Green           ZoneStats `json:"green"`
	DevicesInRadius int       `json:"devices_in_radius"`
	Triaged         int       `json:"triaged"`
	LocationFailed  int       `json:"location_failed"`
}

// NarrativePayload — type narrative_update. Latest per zone wins. Narrative
// must already be passed through RedactPhones.
type NarrativePayload struct {
	Zone       ZoneType `json:"zone"`
	Narrative  string   `json:"narrative"`
	BatchIndex int      `json:"batch_index"`
}

// ErrorPayload — type error. Phone is "" when the error is not device
// specific; Message never contains a phone number (use RedactPhones on
// upstream text). Fatal is true iff the pipeline stops because of it.
type ErrorPayload struct {
	Code    ErrorCode  `json:"code"`
	Message string     `json:"message"`
	Phone   string     `json:"phone"`
	Fatal   bool       `json:"fatal"`
	Stage   ErrorStage `json:"stage"`
}

// FailureCounts are the per-step failure totals in event_complete.
type FailureCounts struct {
	Location     int `json:"location"`
	Reachability int `json:"reachability"`
	Decision     int `json:"decision"`
	SMS          int `json:"sms"`
	Rescue       int `json:"rescue"`
}

// FatalError is event_complete.fatal_error when the status is failed.
type FatalError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// EventCompletePayload — type event_complete, sent exactly once per event.
type EventCompletePayload struct {
	Status              Lifecycle     `json:"status"` // a Terminal() value
	DurationMs          int64         `json:"duration_ms"`
	DevicesInRadius     int           `json:"devices_in_radius"`
	DevicesTriaged      int           `json:"devices_triaged"`
	DevicesDecided      int           `json:"devices_decided"`
	Failures            FailureCounts `json:"failures"`
	SMSNotSentNoGateway int           `json:"sms_not_sent_no_gateway"`
	FatalError          *FatalError   `json:"fatal_error"` // null unless Status == failed
}

// SnapshotBeginPayload — type snapshot_begin, the first frame on every connection.
type SnapshotBeginPayload struct {
	HeadSeq   int64     `json:"head_seq"`
	Active    bool      `json:"active"`
	Lifecycle Lifecycle `json:"lifecycle"`
}

// SnapshotEndPayload — type snapshot_end.
type SnapshotEndPayload struct {
	HeadSeq  int64 `json:"head_seq"`
	Replayed int   `json:"replayed"`
}

// HeartbeatPayload — type heartbeat, sent periodically on every connection.
type HeartbeatPayload struct {
	HeadSeq   int64     `json:"head_seq"`
	Active    bool      `json:"active"`
	Lifecycle Lifecycle `json:"lifecycle"`
}
