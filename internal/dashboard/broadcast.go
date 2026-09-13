package dashboard

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"github.com/geodispatch/supervisor/internal/models"
)

// maxReplayedErrors is how many error frames the recorder keeps (and a
// snapshot replays): the most recent ones, seq ascending (spec §1.2).
const maxReplayedErrors = 100

// replayZoneOrder is the snapshot order of narratives.
var replayZoneOrder = [...]models.ZoneType{models.ZoneRed, models.ZoneOrange, models.ZoneGreen}

// recorder keeps the held event in replay form: every kept frame is stored
// already encoded with replay:true and its ORIGINAL seq and timestamp, so a
// snapshot is a list of shared byte slices. It is guarded by Hub.mu.
type recorder struct {
	eventID   string // "" until the first BeginEvent
	lifecycle models.Lifecycle
	active    bool  // a pipeline is running right now
	headSeq   int64 // last seq issued for eventID

	start      []byte
	context    []byte // latest
	devices    map[string]deviceFrame
	summary    []byte // latest
	narratives map[models.ZoneType][]byte
	errors     [][]byte // ≤ maxReplayedErrors, oldest first
	complete   []byte
}

// deviceFrame is the latest frame of one phone; seq orders the replay.
type deviceFrame struct {
	seq   int64
	frame []byte
}

func newRecorder() recorder {
	return recorder{
		lifecycle:  models.LifecycleIdle,
		devices:    map[string]deviceFrame{},
		narratives: map[models.ZoneType][]byte{},
	}
}

// BeginEvent drops the previous recording, holds eventID with lifecycle
// running and emits event_start with seq 1.
func (h *Hub) BeginEvent(eventID string, p models.EventStartPayload) {
	raw, ok := encodePayload(models.TypeEventStart, eventID, p)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if eventID == "" {
		log.Printf("dashboard: refused event_start: empty event_id")
		return
	}
	if h.rec.active {
		// The pipeline manager allows one incident at a time, so this is a
		// programming error. The new event still wins: it is what is running.
		log.Printf("dashboard: event %q began while %q was still running without event_complete; the old recording is discarded",
			eventID, h.rec.eventID)
	}
	h.rec = newRecorder()
	h.rec.eventID = eventID
	h.rec.lifecycle = models.LifecycleRunning
	h.rec.active = true
	h.emitLocked(models.TypeEventStart, raw, func(_ int64, replay []byte) {
		h.rec.start = replay
	})
}

// PublishContext emits event_context; the latest one replaces the previous.
func (h *Hub) PublishContext(eventID string, p models.EventContextPayload) {
	h.publish(eventID, models.TypeEventContext, p, func(_ int64, replay []byte) {
		h.rec.context = replay
	})
}

// PublishDevice emits device_update; the latest frame per phone is kept.
func (h *Hub) PublishDevice(eventID string, p models.DeviceUpdatePayload) {
	h.publish(eventID, models.TypeDeviceUpdate, p, func(seq int64, replay []byte) {
		h.rec.devices[p.Phone] = deviceFrame{seq: seq, frame: replay}
	})
}

// PublishSummary emits zone_summary; the latest one replaces the previous.
func (h *Hub) PublishSummary(eventID string, p models.ZoneSummaryPayload) {
	h.publish(eventID, models.TypeZoneSummary, p, func(_ int64, replay []byte) {
		h.rec.summary = replay
	})
}

// PublishNarrative emits narrative_update; the latest one per zone is kept.
// The text goes through RedactPhones even though the pipeline already does
// it: the hub is the only way out to browsers.
func (h *Hub) PublishNarrative(eventID string, p models.NarrativePayload) {
	if !models.ValidZone(p.Zone) {
		// A narrative the snapshot could not replay would make late clients
		// disagree with live ones.
		log.Printf("dashboard: refused narrative_update for event %q: invalid zone %q", eventID, p.Zone)
		return
	}
	p.Narrative = models.RedactPhones(p.Narrative)
	h.publish(eventID, models.TypeNarrativeUpdate, p, func(_ int64, replay []byte) {
		h.rec.narratives[p.Zone] = replay
	})
}

// PublishError emits an error frame; the latest maxReplayedErrors are kept.
// Message goes through RedactPhones as a last line of defence; Phone is the
// device the error belongs to and is sent as is (contract field).
func (h *Hub) PublishError(eventID string, p models.ErrorPayload) {
	p.Message = models.RedactPhones(p.Message)
	h.publish(eventID, models.TypeError, p, func(_ int64, replay []byte) {
		if len(h.rec.errors) == maxReplayedErrors {
			copy(h.rec.errors, h.rec.errors[1:])
			h.rec.errors[len(h.rec.errors)-1] = replay
			return
		}
		h.rec.errors = append(h.rec.errors, replay)
	})
}

// CompleteEvent emits event_complete, sets the lifecycle to p.Status and
// marks the event inactive. The event stays held (and replayed, including
// event_complete) until the next BeginEvent. Later publishes for it are
// refused: event_complete is terminal.
func (h *Hub) CompleteEvent(eventID string, p models.EventCompletePayload) {
	if !p.Status.Terminal() {
		log.Printf("dashboard: refused event_complete for event %q: status %q is not terminal", eventID, p.Status)
		return
	}
	h.publish(eventID, models.TypeEventComplete, p, func(_ int64, replay []byte) {
		h.rec.complete = replay
		h.rec.lifecycle = p.Status
		h.rec.active = false
	})
}

// publish emits one event frame for the held, still running event.
func (h *Hub) publish(eventID, typ string, payload any, record func(seq int64, replay []byte)) {
	// Encode outside the lock: it is the expensive part and needs no state.
	raw, ok := encodePayload(typ, eventID, payload)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if reason := h.refusalLocked(eventID); reason != "" {
		// Programming-error guard: frames of another event would corrupt the
		// seq stream of the held one.
		log.Printf("dashboard: refused %s for event %q: %s", typ, eventID, reason)
		return
	}
	h.emitLocked(typ, raw, record)
}

// refusalLocked says why a frame for eventID may not be published, or "".
func (h *Hub) refusalLocked(eventID string) string {
	switch {
	case h.rec.eventID == "":
		return "no event is held (BeginEvent was not called)"
	case eventID != h.rec.eventID:
		return fmt.Sprintf("the held event is %q", h.rec.eventID)
	case !h.rec.active:
		return "the event is already complete"
	}
	return ""
}

// emitLocked assigns the next seq, records the frame and enqueues it on
// every client. Caller holds h.mu.
func (h *Hub) emitLocked(typ string, raw json.RawMessage, record func(seq int64, replay []byte)) {
	env := models.Envelope{
		V:         models.ContractVersion,
		Type:      typ,
		EventID:   h.rec.eventID,
		Seq:       h.rec.headSeq + 1,
		Timestamp: h.nowMs(),
		Payload:   raw,
	}
	live, err := json.Marshal(env)
	if err != nil {
		log.Printf("dashboard: %s for event %q not published: encode envelope: %v", typ, env.EventID, err)
		return
	}
	env.Replay = true
	replay, err := json.Marshal(env)
	if err != nil {
		log.Printf("dashboard: %s for event %q not published: encode envelope: %v", typ, env.EventID, err)
		return
	}
	h.rec.headSeq = env.Seq
	record(env.Seq, replay)
	h.stats.FramesPublished++
	h.fanOutLocked(live)
}

// Snapshot returns exactly the frames a client connecting now would receive
// before the live stream: snapshot_begin, the replay, snapshot_end.
func (h *Hub) Snapshot() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}

// snapshotLocked builds the connection snapshot in the order of spec §1.2.
// Caller holds h.mu.
func (h *Hub) snapshotLocked() [][]byte {
	r := &h.rec
	replay := make([][]byte, 0, 4+len(r.devices)+len(r.narratives)+len(r.errors))
	if r.start != nil {
		replay = append(replay, r.start)
	}
	if r.context != nil {
		replay = append(replay, r.context)
	}
	devs := make([]deviceFrame, 0, len(r.devices))
	for _, d := range r.devices {
		devs = append(devs, d)
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].seq < devs[j].seq })
	for _, d := range devs {
		replay = append(replay, d.frame)
	}
	if r.summary != nil {
		replay = append(replay, r.summary)
	}
	for _, z := range replayZoneOrder {
		if f, ok := r.narratives[z]; ok {
			replay = append(replay, f)
		}
	}
	replay = append(replay, r.errors...)
	if r.complete != nil {
		replay = append(replay, r.complete)
	}

	now := h.nowMs()
	out := make([][]byte, 0, len(replay)+2)
	out = append(out, controlFrame(models.TypeSnapshotBegin, r.eventID, now, models.SnapshotBeginPayload{
		HeadSeq:   r.headSeq,
		Active:    r.active,
		Lifecycle: r.lifecycle,
	}))
	out = append(out, replay...)
	out = append(out, controlFrame(models.TypeSnapshotEnd, r.eventID, now, models.SnapshotEndPayload{
		HeadSeq:  r.headSeq,
		Replayed: len(replay),
	}))
	return out
}

// encodePayload marshals an event payload. It fails only on non-finite
// floats, which the pipeline must never produce; the frame is then not
// published (and no seq is consumed, so clients see no gap) and the failure
// is logged.
func encodePayload(typ, eventID string, payload any) (json.RawMessage, bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("dashboard: %s for event %q not published: encode payload: %v", typ, eventID, err)
		return nil, false
	}
	return raw, true
}

// controlFrame encodes a control frame (seq 0, replay false).
func controlFrame(typ, eventID string, ts int64, payload any) []byte {
	raw, err := json.Marshal(payload)
	var b []byte
	if err == nil {
		b, err = json.Marshal(models.Envelope{
			V:         models.ContractVersion,
			Type:      typ,
			EventID:   eventID,
			Seq:       0,
			Timestamp: ts,
			Payload:   raw,
		})
	}
	if err != nil {
		// Control payloads hold only strings, integers and booleans, so this
		// cannot happen; refuse to send a frame that breaks the contract.
		panic(fmt.Sprintf("dashboard: encode %s: %v", typ, err))
	}
	return b
}
