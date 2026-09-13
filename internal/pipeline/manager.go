package pipeline

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/zones"
)

// insertEventTimeout bounds the events-table insert. Submit holds the submit
// lock while it runs, so a hung database must not stall every later /sensor.
const insertEventTimeout = 5 * time.Second

// errShutdown is the cancellation cause of a run stopped by Shutdown.
var errShutdown = errors.New("the supervisor is shutting down; the pipeline was cancelled")

// SubmitResult is the HTTP reply to POST /sensor step 7: status and JSON body.
type SubmitResult struct {
	Status int
	Body   map[string]any
}

// held is the event the manager holds: the running one, or the last finished
// one until the next event starts.
type held struct {
	input     models.SensorInput
	lifecycle models.Lifecycle
	active    bool
}

// Manager enforces the single-incident rule and runs at most one pipeline.
type Manager struct {
	cfg   Config
	store Store
	net   Network
	ai    Decider
	sms   Messenger
	pub   Publisher
	now   func() time.Time

	// submitMu serialises Submit end to end, including the InsertEvent round
	// trip, so two concurrent submits can never both start a pipeline.
	submitMu sync.Mutex

	// mu guards the fields below. It is only held briefly, so Status never
	// waits for the database.
	mu     sync.Mutex
	held   *held
	closed bool
	cancel context.CancelCauseFunc // cancels the running pipeline
	done   chan struct{}           // closed when the running pipeline returns

	wg sync.WaitGroup
}

// NewManager builds a Manager. cfg is normalised (see Config).
func NewManager(cfg Config, store Store, net Network, ai Decider, sms Messenger, pub Publisher) *Manager {
	return &Manager{
		cfg:   cfg.normalised(),
		store: store,
		net:   net,
		ai:    ai,
		sms:   sms,
		pub:   pub,
		now:   time.Now,
	}
}

// Submit applies spec §2.1 step 7 to a validated input and, when accepted,
// starts its pipeline in the background. It never waits for the pipeline;
// ctx only bounds the InsertEvent call.
func (m *Manager) Submit(ctx context.Context, in *models.SensorInput) SubmitResult {
	m.submitMu.Lock()
	defer m.submitMu.Unlock()

	m.mu.Lock()
	closed, h := m.closed, m.held
	var cur held
	if h != nil {
		cur = *h
	}
	m.mu.Unlock()

	if closed {
		return SubmitResult{http.StatusServiceUnavailable, map[string]any{"error": "shutting_down"}}
	}
	if h != nil {
		switch {
		case cur.input.EventID == in.EventID && cur.input == *in:
			return SubmitResult{http.StatusOK, map[string]any{
				"status":    "duplicate",
				"event_id":  in.EventID,
				"lifecycle": string(cur.lifecycle),
			}}
		case cur.input.EventID == in.EventID:
			return conflict(in.EventID, "event_id already used with a different payload")
		case cur.active:
			return SubmitResult{http.StatusConflict, map[string]any{
				"error":           "pipeline_busy",
				"active_event_id": cur.input.EventID,
			}}
		}
	}

	ictx, cancel := context.WithTimeout(ctx, insertEventTimeout)
	inserted, err := m.store.InsertEvent(ictx, in)
	cancel()
	if err != nil {
		log.Printf("[pipeline] event %s: InsertEvent failed: %s", in.EventID, models.RedactPhones(err.Error()))
		return SubmitResult{http.StatusServiceUnavailable, map[string]any{"error": "database_unavailable"}}
	}
	if !inserted {
		// The id exists in the events table but is not the held event, e.g.
		// it was used before a restart. Its frames are gone, so it cannot be
		// reported as a duplicate.
		return conflict(in.EventID, "event_id already used")
	}

	m.start(*in)
	return SubmitResult{http.StatusAccepted, map[string]any{
		"status":           "accepted",
		"event_id":         in.EventID,
		"contract_version": models.ContractVersion,
	}}
}

func conflict(eventID, detail string) SubmitResult {
	return SubmitResult{http.StatusConflict, map[string]any{
		"error":    "event_id_conflict",
		"event_id": eventID,
		"detail":   detail,
	}}
}

// start records in as the held, running event, emits event_start and runs
// the pipeline in a goroutine. event_start is published before Submit
// returns 202, so a dashboard that connects right after the reply already
// receives the new event in its snapshot. The caller holds submitMu.
func (m *Manager) start(in models.SensorInput) {
	started := m.now()
	base, cancel := context.WithCancelCause(context.Background())
	done := make(chan struct{})

	m.mu.Lock()
	m.held = &held{input: in, lifecycle: models.LifecycleRunning, active: true}
	m.cancel = cancel
	m.done = done
	m.mu.Unlock()

	m.pub.BeginEvent(in.EventID, models.EventStartPayload{
		DisasterType:    in.DisasterType,
		Severity:        in.Severity,
		Epicenter:       in.Epicenter,
		RadiusKm:        in.RadiusKm,
		DepthKm:         in.DepthKm,
		TsunamiRisk:     in.TsunamiRisk,
		AftershockRisk:  in.AftershockRisk,
		SensorTimestamp: in.Timestamp,
		ZoneBands:       zones.Bands(),
	})

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(done)
		defer cancel(nil)

		ctx, stop := context.WithTimeoutCause(base, m.cfg.PipelineTimeout,
			errors.New("pipeline deadline of "+m.cfg.PipelineTimeout.String()+" exceeded"))
		defer stop()

		r := newRun(m, in, started)
		complete := r.execute(ctx)

		// event_complete goes out BEFORE the held event is marked inactive:
		// once it is inactive a new event may start, and the hub refuses
		// frames for an event that is no longer held.
		m.pub.CompleteEvent(in.EventID, complete)
		m.mu.Lock()
		if m.held != nil && m.held.input.EventID == in.EventID {
			m.held.lifecycle = complete.Status
			m.held.active = false
		}
		m.mu.Unlock()
		log.Printf("[pipeline] event %s finished: %s in %dms", in.EventID, complete.Status, complete.DurationMs)
	}()
}

// Wait blocks until no pipeline is running (tests and shutdown).
func (m *Manager) Wait() {
	m.wg.Wait()
}

// Status reports the held event ("" and idle when there is none).
func (m *Manager) Status() (eventID string, lifecycle models.Lifecycle, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held == nil {
		return "", models.LifecycleIdle, false
	}
	return m.held.input.EventID, m.held.lifecycle, m.held.active
}

// Shutdown refuses new submits (503 shutting_down) and waits for the running
// pipeline. When ctx ends first the pipeline is cancelled; it then reports a
// fatal INTERNAL_ERROR and its event_complete{failed}, and Shutdown waits up
// to cancelGrace more for that. It returns ctx's error if the pipeline had to
// be cancelled, and an error if it still had not returned after the grace.
func (m *Manager) Shutdown(ctx context.Context) error {
	const cancelGrace = 2 * time.Second

	m.submitMu.Lock() // no Submit is half-way through a start
	m.mu.Lock()
	m.closed = true
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	m.submitMu.Unlock()

	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	cancel(errShutdown)
	select {
	case <-done:
		return ctx.Err()
	case <-time.After(cancelGrace):
		return errors.New("pipeline did not stop within " + cancelGrace.String() + " of being cancelled")
	}
}
