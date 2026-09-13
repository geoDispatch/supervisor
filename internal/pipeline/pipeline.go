// Package pipeline runs one disaster event end to end (spec §3.6): device
// lookup, area context, CAMARA triage, zone-pure AI batches, dispatch and the
// v2 frames that describe every step. It depends only on the consumer
// interfaces below plus models and zones, so every collaborator can be faked.
package pipeline

import (
	"context"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// Store is the database the pipeline needs.
type Store interface {
	PhonesNearEpicenter(ctx context.Context, epicenter models.Coordinates, radiusKm float64) ([]string, error)
	NearestShelters(ctx context.Context, center models.Coordinates, limit int) ([]models.Shelter, error)
	// InsertEvent reports inserted=false when the event id already exists.
	InsertEvent(ctx context.Context, in *models.SensorInput) (inserted bool, err error)
	InsertDeviceLog(ctx context.Context, eventID string, d models.DeviceDecision) error
	FlagRescue(ctx context.Context, eventID string, d models.DeviceDecision) error
}

// Network is the CAMARA client (mock or Nokia NaC).
type Network interface {
	Location(ctx context.Context, phone string) (*models.CAMARALocationResponse, error)
	Reachability(ctx context.Context, phone string) (*models.CAMARAReachabilityResponse, error)
	RequestQoS(ctx context.Context, epicenter models.Coordinates, phone string) (models.NetworkStatus, error)
	UpgradeQoS(ctx context.Context, epicenter models.Coordinates) error
	Congestion(ctx context.Context, epicenter models.Coordinates, phone string) (models.CongestionLevel, error)
	Source() string // "mock_camara" | "nokia_nac", declared by configuration
}

// Decider is the AI agent.
type Decider interface {
	Decide(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error)
}

// Messenger sends SMS. Send reports what actually happened: sent, failed or
// not_configured (nothing was sent).
type Messenger interface {
	Send(ctx context.Context, phone, message string) models.SMSStatus
	Configured() bool
}

// Publisher receives the v2 frames of an event. *dashboard.Hub implements it.
type Publisher interface {
	BeginEvent(string, models.EventStartPayload)
	PublishContext(string, models.EventContextPayload)
	PublishDevice(string, models.DeviceUpdatePayload)
	PublishSummary(string, models.ZoneSummaryPayload)
	PublishNarrative(string, models.NarrativePayload)
	PublishError(string, models.ErrorPayload)
	CompleteEvent(string, models.EventCompletePayload)
}

// MaxBatchSize is the largest AI request the agent contract allows.
const MaxBatchSize = 20

// Defaults used when a Config field is zero or negative (spec §2.5).
const (
	DefaultCamaraConcurrency   = 50
	DefaultReachabilityTimeout = time.Second
	DefaultAgentTimeout        = 120 * time.Second
	DefaultPipelineTimeout     = 900 * time.Second
)

// Config tunes a Manager. NewManager replaces out-of-range values with the
// defaults above and clamps BatchSize to 1..MaxBatchSize.
type Config struct {
	BatchSize           int           // devices per AI request
	CamaraConcurrency   int           // triage fan-out: concurrent CAMARA device lookups
	ReachabilityTimeout time.Duration // per reachability lookup
	AgentTimeout        time.Duration // per AI request
	PipelineTimeout     time.Duration // whole run; exceeding it is a fatal INTERNAL_ERROR
}

func (c Config) normalised() Config {
	c.BatchSize = clampBatchSize(c.BatchSize)
	if c.CamaraConcurrency <= 0 {
		c.CamaraConcurrency = DefaultCamaraConcurrency
	}
	if c.ReachabilityTimeout <= 0 {
		c.ReachabilityTimeout = DefaultReachabilityTimeout
	}
	if c.AgentTimeout <= 0 {
		c.AgentTimeout = DefaultAgentTimeout
	}
	if c.PipelineTimeout <= 0 {
		c.PipelineTimeout = DefaultPipelineTimeout
	}
	return c
}

func clampBatchSize(n int) int {
	switch {
	case n < 1:
		return 1
	case n > MaxBatchSize:
		return MaxBatchSize
	}
	return n
}
