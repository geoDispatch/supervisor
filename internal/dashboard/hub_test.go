package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/origin"
	"github.com/geodispatch/supervisor/internal/zones"
	"github.com/gorilla/websocket"
)

// Every blocking read in these tests has this deadline, so a bug fails the
// test instead of hanging it.
const testTimeout = 5 * time.Second

const allowedOrigin = "http://dashboard.example"

// fakeClock returns a strictly increasing time (1 ms per call), so every
// frame has a distinct timestamp and "replay keeps the ORIGINAL timestamp"
// is actually checked.
type fakeClock struct {
	mu sync.Mutex
	ms int64
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ms++
	return time.UnixMilli(c.ms)
}

func testOptions(opts Options) Options {
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 2 * time.Second
	}
	if opts.Heartbeat == 0 {
		opts.Heartbeat = time.Hour // tests that want heartbeats set a short one
	}
	if opts.Now == nil {
		opts.Now = (&fakeClock{ms: 1757700000000}).Now
	}
	opts.Origins = origin.NewPolicy(origin.EnvProduction, []string{allowedOrigin})
	return opts
}

// startHub serves a hub on an httptest server. Cleanups run LIFO, so clients
// dialled later are closed before the hub.
func startHub(t *testing.T, opts Options) (*Hub, *httptest.Server) {
	t.Helper()
	h := NewHub(testOptions(opts))
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	return h, srv
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, srv *httptest.Server, hdr http.Header) *testClient {
	t.Helper()
	return dialWith(t, websocket.DefaultDialer, srv, hdr)
}

func dialWith(t *testing.T, d *websocket.Dialer, srv *httptest.Server, hdr http.Header) *testClient {
	t.Helper()
	conn, _, err := d.Dial(wsURL(srv), hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn}
}

type frame struct {
	models.Envelope
	raw []byte
}

var envelopeFields = []string{"v", "type", "event_id", "seq", "timestamp", "replay", "payload"}

// decodeFrame checks the envelope shape (exactly the seven fields, strict
// decode, v 2, timestamp > 0) and returns it.
func decodeFrame(t *testing.T, raw []byte) frame {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("frame is not a JSON object: %v: %s", err, raw)
	}
	if len(keys) != len(envelopeFields) {
		t.Fatalf("envelope has %d fields, want %d: %s", len(keys), len(envelopeFields), raw)
	}
	for _, k := range envelopeFields {
		if _, ok := keys[k]; !ok {
			t.Fatalf("envelope misses %q: %s", k, raw)
		}
	}
	var env models.Envelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("strict envelope decode: %v: %s", err, raw)
	}
	if env.V != models.ContractVersion || env.Timestamp <= 0 {
		t.Fatalf("bad envelope v=%d timestamp=%d", env.V, env.Timestamp)
	}
	if models.IsControlType(env.Type) != (env.Seq == 0) {
		t.Fatalf("%s frame has seq %d", env.Type, env.Seq)
	}
	return frame{Envelope: env, raw: raw}
}

func decodePayload(t *testing.T, f frame, dst any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(f.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("decode %s payload: %v: %s", f.Type, err, f.Payload)
	}
}

func (c *testClient) read() frame {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(testTimeout))
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return decodeFrame(c.t, raw)
}

// readSnapshot reads snapshot_begin, the replay and snapshot_end.
func (c *testClient) readSnapshot() (begin frame, replay []frame, end frame) {
	c.t.Helper()
	begin = c.read()
	if begin.Type != models.TypeSnapshotBegin || begin.Replay {
		c.t.Fatalf("first frame = %s (replay %v), want snapshot_begin", begin.Type, begin.Replay)
	}
	for {
		f := c.read()
		if f.Type == models.TypeSnapshotEnd {
			if f.Replay || f.EventID != begin.EventID {
				c.t.Fatalf("snapshot_end replay=%v event_id=%q, begin event_id=%q", f.Replay, f.EventID, begin.EventID)
			}
			return begin, replay, f
		}
		if !f.Replay {
			c.t.Fatalf("frame %s seq %d inside the snapshot has replay false", f.Type, f.Seq)
		}
		replay = append(replay, f)
	}
}

// readUntilSeq reads live frames up to and including seq.
func (c *testClient) readUntilSeq(seq int64, each func(frame)) {
	c.t.Helper()
	for {
		f := c.read()
		if each != nil {
			each(f)
		}
		if f.Seq == seq {
			return
		}
		if f.Seq > seq {
			c.t.Fatalf("read past seq %d (got %d)", seq, f.Seq)
		}
	}
}

func headSeq(h *Hub) int64 {
	_, _, _, seq := h.Held()
	return seq
}

// ── Fixtures ──────────────────────────────────────────────────

func startPayload() models.EventStartPayload {
	return models.EventStartPayload{
		DisasterType:    models.Earthquake,
		Severity:        6.8,
		Epicenter:       models.Coordinates{Lat: 33.5731, Lng: -7.5898},
		RadiusKm:        15,
		DepthKm:         10.5,
		AftershockRisk:  models.AftershockHigh,
		SensorTimestamp: 1757699999000,
		ZoneBands:       zones.Bands(),
	}
}

func phone(i int) string { return fmt.Sprintf("+2126%08d", i) }

func device(p string, stage models.Stage) models.DeviceUpdatePayload {
	d := models.DeviceUpdatePayload{
		Phone:              p,
		Latitude:           33.57,
		Longitude:          -7.58,
		LocationAccuracyM:  500,
		Zone:               models.ZoneRed,
		DistanceKm:         1.2,
		ReachabilityStatus: models.NotConnected,
		Stage:              stage,
		SMSStatus:          models.SMSStatusNotRequested,
		RescueStatus:       models.RescueStatusNotRequested,
	}
	if stage == models.StageDecided {
		a := models.ActionRescue
		conf := 0.9
		d.Action, d.Confidence = &a, &conf
		d.RescuePriority, d.RescueFlag, d.RescueStatus = 1, true, models.RescueStatusRecorded
	}
	return d
}

func contextPayload(n int, qos models.QoSStatus) models.EventContextPayload {
	return models.EventContextPayload{
		DevicesInRadius: n,
		SheltersStatus:  models.SheltersStatusOK,
		Network:         models.NetworkContext{CongestionLevel: models.CongestionHigh, QoSStatus: qos},
		NetworkSource:   models.NetworkSourceMockCAMARA,
		SMSGateway:      models.SMSGatewayNotConfigured,
	}
}

func summaryPayload(triaged int) models.ZoneSummaryPayload {
	return models.ZoneSummaryPayload{Red: models.ZoneStats{Total: triaged}, DevicesInRadius: triaged, Triaged: triaged}
}

func errorPayload(i int) models.ErrorPayload {
	return models.ErrorPayload{Code: models.ErrCAMARATimeout, Message: fmt.Sprintf("location lookup timed out (%d)", i), Stage: models.ErrorStageTriage}
}

func completePayload(status models.Lifecycle) models.EventCompletePayload {
	return models.EventCompletePayload{Status: status, DurationMs: 1834}
}

// ── Tests ─────────────────────────────────────────────────────

func TestIdleConnectionGetsEmptySnapshotThenHeartbeat(t *testing.T) {
	_, srv := startHub(t, Options{Heartbeat: 20 * time.Millisecond})
	c := dial(t, srv, nil)

	begin, replay, end := c.readSnapshot()
	var bp models.SnapshotBeginPayload
	decodePayload(t, begin, &bp)
	if begin.EventID != "" || begin.Seq != 0 || bp != (models.SnapshotBeginPayload{HeadSeq: 0, Active: false, Lifecycle: models.LifecycleIdle}) {
		t.Fatalf("snapshot_begin = %s", begin.raw)
	}
	if len(replay) != 0 {
		t.Fatalf("idle snapshot replayed %d frames", len(replay))
	}
	var ep models.SnapshotEndPayload
	decodePayload(t, end, &ep)
	if end.EventID != "" || ep != (models.SnapshotEndPayload{HeadSeq: 0, Replayed: 0}) {
		t.Fatalf("snapshot_end = %s", end.raw)
	}

	hb := c.read()
	var hp models.HeartbeatPayload
	decodePayload(t, hb, &hp)
	if hb.Type != models.TypeHeartbeat || hb.EventID != "" || hb.Replay ||
		hp != (models.HeartbeatPayload{HeadSeq: 0, Active: false, Lifecycle: models.LifecycleIdle}) {
		t.Fatalf("third frame = %s, want an idle heartbeat", hb.raw)
	}
}

func TestHeartbeatCarriesHeldState(t *testing.T) {
	h, srv := startHub(t, Options{Heartbeat: 20 * time.Millisecond})
	const id = "EQ-HB"
	h.BeginEvent(id, startPayload())
	h.PublishDevice(id, device(phone(1), models.StageTriaged))

	c := dial(t, srv, nil)
	c.readSnapshot()
	hb := c.read()
	var hp models.HeartbeatPayload
	decodePayload(t, hb, &hp)
	if hb.Type != models.TypeHeartbeat || hb.EventID != id ||
		hp != (models.HeartbeatPayload{HeadSeq: 2, Active: true, Lifecycle: models.LifecycleRunning}) {
		t.Fatalf("heartbeat while running = %s", hb.raw)
	}

	h.CompleteEvent(id, completePayload(models.LifecycleCompleted))
	for {
		f := c.read()
		if f.Type == models.TypeEventComplete {
			break
		}
		if f.Type != models.TypeHeartbeat {
			t.Fatalf("unexpected %s before event_complete", f.Type)
		}
	}
	hb = c.read()
	decodePayload(t, hb, &hp)
	if hb.Type != models.TypeHeartbeat || hb.EventID != id ||
		hp != (models.HeartbeatPayload{HeadSeq: 3, Active: false, Lifecycle: models.LifecycleCompleted}) {
		t.Fatalf("heartbeat after completion = %s", hb.raw)
	}
}

func TestLateJoinReplaysInContractOrder(t *testing.T) {
	h, srv := startHub(t, Options{})
	const id = "EQ-LATE"
	early := dial(t, srv, nil)
	early.readSnapshot()

	a, b, c := phone(1), phone(2), phone(3)
	h.BeginEvent(id, startPayload())                                                                      // 1
	h.PublishContext(id, contextPayload(3, models.QoSInactive))                                           // 2
	h.PublishDevice(id, device(a, models.StageTriaged))                                                   // 3
	h.PublishDevice(id, device(b, models.StageTriaged))                                                   // 4
	h.PublishDevice(id, device(c, models.StageTriaged))                                                   // 5
	h.PublishSummary(id, summaryPayload(3))                                                               // 6
	h.PublishError(id, errorPayload(1))                                                                   // 7
	h.PublishDevice(id, device(a, models.StageDecided))                                                   // 8
	h.PublishNarrative(id, models.NarrativePayload{Zone: models.ZoneGreen, Narrative: "g1"})              // 9
	h.PublishNarrative(id, models.NarrativePayload{Zone: models.ZoneRed, Narrative: "r1"})                // 10
	h.PublishError(id, errorPayload(2))                                                                   // 11
	h.PublishDevice(id, device(b, models.StageDecisionFailed))                                            // 12
	h.PublishNarrative(id, models.NarrativePayload{Zone: models.ZoneRed, Narrative: "r2", BatchIndex: 1}) // 13
	h.PublishSummary(id, summaryPayload(3))                                                               // 14
	h.PublishContext(id, contextPayload(3, models.QoSActive))                                             // 15

	live := map[int64]frame{}
	early.readUntilSeq(15, func(f frame) { live[f.Seq] = f })

	late := dial(t, srv, nil)
	begin, replay, end := late.readSnapshot()

	var bp models.SnapshotBeginPayload
	decodePayload(t, begin, &bp)
	if begin.EventID != id || bp != (models.SnapshotBeginPayload{HeadSeq: 15, Active: true, Lifecycle: models.LifecycleRunning}) {
		t.Fatalf("snapshot_begin = %s", begin.raw)
	}

	// event_start, latest context, devices by seq (C 5, A 8, B 12), latest
	// summary, narratives red then green, errors ascending.
	wantSeqs := []int64{1, 15, 5, 8, 12, 14, 13, 9, 7, 11}
	wantTypes := []string{
		models.TypeEventStart, models.TypeEventContext,
		models.TypeDeviceUpdate, models.TypeDeviceUpdate, models.TypeDeviceUpdate,
		models.TypeZoneSummary, models.TypeNarrativeUpdate, models.TypeNarrativeUpdate,
		models.TypeError, models.TypeError,
	}
	if len(replay) != len(wantSeqs) {
		t.Fatalf("replayed %d frames, want %d", len(replay), len(wantSeqs))
	}
	for i, f := range replay {
		if f.Seq != wantSeqs[i] || f.Type != wantTypes[i] {
			t.Fatalf("replay[%d] = %s seq %d, want %s seq %d", i, f.Type, f.Seq, wantTypes[i], wantSeqs[i])
		}
		orig := live[f.Seq]
		if orig.Replay || f.EventID != orig.EventID || f.Timestamp != orig.Timestamp || !bytes.Equal(f.Payload, orig.Payload) {
			t.Fatalf("replay of seq %d differs from the live frame:\n live   %s\n replay %s", f.Seq, orig.raw, f.raw)
		}
	}

	var ep models.SnapshotEndPayload
	decodePayload(t, end, &ep)
	if ep != (models.SnapshotEndPayload{HeadSeq: 15, Replayed: len(wantSeqs)}) {
		t.Fatalf("snapshot_end = %s", end.raw)
	}

	// Snapshot() replays exactly the same bytes a client receives.
	snap := h.Snapshot()
	if len(snap) != len(replay)+2 {
		t.Fatalf("Snapshot() has %d frames, want %d", len(snap), len(replay)+2)
	}
	for i, f := range replay {
		if !bytes.Equal(snap[i+1], f.raw) {
			t.Fatalf("Snapshot()[%d] differs from the received replay frame", i+1)
		}
	}

	// The live stream continues right after head_seq, on both clients.
	h.PublishDevice(id, device(c, models.StageDecided))
	for _, cl := range []*testClient{late, early} {
		f := cl.read()
		if f.Seq != ep.HeadSeq+1 || f.Replay || f.Type != models.TypeDeviceUpdate {
			t.Fatalf("first live frame after snapshot = %s", f.raw)
		}
	}
}

func TestTwoClientsReceiveIdenticalLiveSequences(t *testing.T) {
	h, srv := startHub(t, Options{})
	const id = "EQ-TWIN"
	a := dial(t, srv, nil)
	b := dial(t, srv, nil)
	a.readSnapshot()
	b.readSnapshot()

	h.BeginEvent(id, startPayload())
	// Concurrent publishers still yield one gap-free sequence.
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 75; i++ {
				switch i % 3 {
				case 0:
					h.PublishDevice(id, device(phone(w*100+i), models.StageTriaged))
				case 1:
					h.PublishSummary(id, summaryPayload(i))
				default:
					h.PublishError(id, errorPayload(i))
				}
			}
		}(w)
	}
	wg.Wait()
	h.CompleteEvent(id, completePayload(models.LifecycleCompleted))
	head := headSeq(h)
	if head != 1+300+1 {
		t.Fatalf("head_seq = %d, want 302", head)
	}

	var fa, fb []frame
	a.readUntilSeq(head, func(f frame) { fa = append(fa, f) })
	b.readUntilSeq(head, func(f frame) { fb = append(fb, f) })
	if len(fa) != int(head) || len(fb) != int(head) {
		t.Fatalf("received %d and %d frames, want %d each", len(fa), len(fb), head)
	}
	for i := range fa {
		if fa[i].Seq != int64(i+1) || fa[i].Replay {
			t.Fatalf("frame %d has seq %d replay %v", i, fa[i].Seq, fa[i].Replay)
		}
		if !bytes.Equal(fa[i].raw, fb[i].raw) {
			t.Fatalf("clients differ at seq %d:\n %s\n %s", i+1, fa[i].raw, fb[i].raw)
		}
	}
	if fa[0].Type != models.TypeEventStart || fa[len(fa)-1].Type != models.TypeEventComplete {
		t.Fatalf("stream runs %s … %s", fa[0].Type, fa[len(fa)-1].Type)
	}
}

// view is the state a dashboard rebuilds from frames.
type view struct {
	t          *testing.T
	eventID    string
	lastSeq    int64
	devices    map[string]models.DeviceUpdatePayload
	context    *models.EventContextPayload
	summary    *models.ZoneSummaryPayload
	narratives map[models.ZoneType]models.NarrativePayload
	errors     []models.ErrorPayload
}

func newView(t *testing.T) *view {
	return &view{t: t, devices: map[string]models.DeviceUpdatePayload{}, narratives: map[models.ZoneType]models.NarrativePayload{}}
}

func (v *view) apply(f frame) {
	if models.IsControlType(f.Type) {
		return
	}
	// Replay order is not seq order (errors come after the summary), so
	// track the highest seq seen.
	v.eventID = f.EventID
	if f.Seq > v.lastSeq {
		v.lastSeq = f.Seq
	}
	switch f.Type {
	case models.TypeDeviceUpdate:
		var p models.DeviceUpdatePayload
		decodePayload(v.t, f, &p)
		v.devices[p.Phone] = p
	case models.TypeEventContext:
		var p models.EventContextPayload
		decodePayload(v.t, f, &p)
		v.context = &p
	case models.TypeZoneSummary:
		var p models.ZoneSummaryPayload
		decodePayload(v.t, f, &p)
		v.summary = &p
	case models.TypeNarrativeUpdate:
		var p models.NarrativePayload
		decodePayload(v.t, f, &p)
		v.narratives[p.Zone] = p
	case models.TypeError:
		var p models.ErrorPayload
		decodePayload(v.t, f, &p)
		v.errors = append(v.errors, p)
	}
}

func (v *view) equal(o *view) bool {
	return v.eventID == o.eventID && v.lastSeq == o.lastSeq &&
		reflect.DeepEqual(v.devices, o.devices) && reflect.DeepEqual(v.context, o.context) &&
		reflect.DeepEqual(v.summary, o.summary) && reflect.DeepEqual(v.narratives, o.narratives) &&
		reflect.DeepEqual(v.errors, o.errors)
}

func TestReconnectDuringEventRecoversIdenticalState(t *testing.T) {
	h, srv := startHub(t, Options{})
	const id = "EQ-RECONNECT"
	steady := dial(t, srv, nil)
	steady.readSnapshot()
	sv := newView(t)

	h.BeginEvent(id, startPayload())
	h.PublishContext(id, contextPayload(20, models.QoSRequested))
	for i := 0; i < 20; i++ {
		h.PublishDevice(id, device(phone(i), models.StageTriaged))
	}
	h.PublishSummary(id, summaryPayload(20))

	// A client joins mid-event, then drops.
	first := dial(t, srv, nil)
	first.readSnapshot()
	first.conn.Close()

	for i := 0; i < 20; i += 2 {
		h.PublishDevice(id, device(phone(i), models.StageDecided))
	}
	h.PublishError(id, errorPayload(1))
	h.PublishNarrative(id, models.NarrativePayload{Zone: models.ZoneRed, Narrative: "red batch"})
	h.PublishContext(id, contextPayload(20, models.QoSActive))
	h.PublishSummary(id, summaryPayload(20))

	// It reconnects: the snapshot alone must rebuild what the steady client has.
	again := dial(t, srv, nil)
	av := newView(t)
	_, replay, end := again.readSnapshot()
	for _, f := range replay {
		av.apply(f)
	}
	steady.readUntilSeq(headSeq(h), sv.apply)
	if !sv.equal(av) {
		t.Fatalf("reconnected state differs: steady seq %d, %d devices, %d errors; snapshot seq %d, %d devices, %d errors",
			sv.lastSeq, len(sv.devices), len(sv.errors), av.lastSeq, len(av.devices), len(av.errors))
	}
	var ep models.SnapshotEndPayload
	decodePayload(t, end, &ep)
	if ep.HeadSeq != sv.lastSeq {
		t.Fatalf("snapshot head_seq %d, steady client at %d", ep.HeadSeq, sv.lastSeq)
	}

	// And both stay identical through the rest of the event.
	for i := 1; i < 20; i += 2 {
		h.PublishDevice(id, device(phone(i), models.StageDecisionFailed))
	}
	h.CompleteEvent(id, completePayload(models.LifecycleCompletedWithFailures))
	steady.readUntilSeq(headSeq(h), sv.apply)
	again.readUntilSeq(headSeq(h), av.apply)
	if !sv.equal(av) || len(av.devices) != 20 {
		t.Fatalf("states diverged after reconnect (%d devices)", len(av.devices))
	}
}

func TestFinishedEventIsRetainedUntilNextBegin(t *testing.T) {
	h, srv := startHub(t, Options{})
	h.BeginEvent("EQ-A", startPayload())
	h.PublishContext("EQ-A", contextPayload(2, models.QoSInactive))
	h.PublishDevice("EQ-A", device(phone(1), models.StageDecided))
	h.PublishDevice("EQ-A", device(phone(2), models.StageDecisionFailed))
	h.PublishSummary("EQ-A", summaryPayload(2))
	h.CompleteEvent("EQ-A", completePayload(models.LifecycleCompletedWithFailures))

	if id, lc, active, seq := h.Held(); id != "EQ-A" || lc != models.LifecycleCompletedWithFailures || active || seq != 6 {
		t.Fatalf("Held() = %q %s %v %d", id, lc, active, seq)
	}

	c := dial(t, srv, nil)
	begin, replay, end := c.readSnapshot()
	var bp models.SnapshotBeginPayload
	decodePayload(t, begin, &bp)
	if begin.EventID != "EQ-A" || bp != (models.SnapshotBeginPayload{HeadSeq: 6, Active: false, Lifecycle: models.LifecycleCompletedWithFailures}) {
		t.Fatalf("snapshot_begin = %s", begin.raw)
	}
	if len(replay) != 6 {
		t.Fatalf("replayed %d frames, want 6", len(replay))
	}
	last := replay[len(replay)-1]
	var cp models.EventCompletePayload
	decodePayload(t, last, &cp)
	if last.Type != models.TypeEventComplete || last.Seq != 6 || cp.Status != models.LifecycleCompletedWithFailures || cp.FatalError != nil {
		t.Fatalf("last replayed frame = %s", last.raw)
	}
	var ep models.SnapshotEndPayload
	decodePayload(t, end, &ep)
	if ep != (models.SnapshotEndPayload{HeadSeq: 6, Replayed: 6}) {
		t.Fatalf("snapshot_end = %s", end.raw)
	}

	// A new event resets the recorder and restarts seq at 1.
	h.BeginEvent("EQ-B", startPayload())
	if f := c.read(); f.Type != models.TypeEventStart || f.EventID != "EQ-B" || f.Seq != 1 || f.Replay {
		t.Fatalf("live frame after BeginEvent = %s", f.raw)
	}
	if id, lc, active, seq := h.Held(); id != "EQ-B" || lc != models.LifecycleRunning || !active || seq != 1 {
		t.Fatalf("Held() = %q %s %v %d", id, lc, active, seq)
	}
	d := dial(t, srv, nil)
	begin, replay, _ = d.readSnapshot()
	decodePayload(t, begin, &bp)
	if begin.EventID != "EQ-B" || bp.HeadSeq != 1 || len(replay) != 1 || replay[0].Type != models.TypeEventStart {
		t.Fatalf("snapshot of the new event: begin %s, %d replayed", begin.raw, len(replay))
	}

	// Late frames of the old event are ignored; the new one continues at 2.
	h.PublishDevice("EQ-A", device(phone(1), models.StageDecided))
	h.PublishDevice("EQ-B", device(phone(1), models.StageTriaged))
	for _, cl := range []*testClient{c, d} {
		if f := cl.read(); f.EventID != "EQ-B" || f.Seq != 2 {
			t.Fatalf("next frame = %s", f.raw)
		}
	}
}

func TestPublishingForAnotherEventIsIgnored(t *testing.T) {
	h := NewHub(testOptions(Options{}))
	defer h.Close()
	check := func(step string, wantID string, wantLC models.Lifecycle, wantActive bool, wantSeq int64) {
		t.Helper()
		id, lc, active, seq := h.Held()
		if id != wantID || lc != wantLC || active != wantActive || seq != wantSeq {
			t.Fatalf("%s: Held() = %q %s %v %d", step, id, lc, active, seq)
		}
		if got := h.Stats().FramesPublished; got != wantSeq {
			t.Fatalf("%s: FramesPublished = %d, want %d", step, got, wantSeq)
		}
	}

	h.PublishDevice("EQ-1", device(phone(1), models.StageTriaged))
	h.CompleteEvent("EQ-1", completePayload(models.LifecycleCompleted))
	h.BeginEvent("", startPayload())
	check("nothing held", "", models.LifecycleIdle, false, 0)

	h.BeginEvent("EQ-1", startPayload())
	h.PublishDevice("EQ-2", device(phone(1), models.StageTriaged))
	h.PublishContext("EQ-2", contextPayload(1, models.QoSInactive))
	h.PublishSummary("", summaryPayload(1))
	h.PublishError("EQ-2", errorPayload(1))
	h.PublishNarrative("EQ-2", models.NarrativePayload{Zone: models.ZoneRed, Narrative: "x"})
	h.CompleteEvent("EQ-2", completePayload(models.LifecycleCompleted))
	check("foreign event ids", "EQ-1", models.LifecycleRunning, true, 1)

	h.CompleteEvent("EQ-1", completePayload(models.LifecycleRunning))                   // not terminal
	h.PublishNarrative("EQ-1", models.NarrativePayload{Zone: "yellow", Narrative: "x"}) // not a zone
	check("invalid payloads", "EQ-1", models.LifecycleRunning, true, 1)

	h.CompleteEvent("EQ-1", completePayload(models.LifecycleNoDevices))
	h.PublishDevice("EQ-1", device(phone(1), models.StageTriaged))
	h.CompleteEvent("EQ-1", completePayload(models.LifecycleCompleted))
	check("after event_complete", "EQ-1", models.LifecycleNoDevices, false, 2)

	snap := h.Snapshot()
	var types []string
	for _, raw := range snap {
		types = append(types, decodeFrame(t, raw).Type)
	}
	want := []string{models.TypeSnapshotBegin, models.TypeEventStart, models.TypeEventComplete, models.TypeSnapshotEnd}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("snapshot types = %v, want %v", types, want)
	}
}

func TestErrorRingKeepsLatest100(t *testing.T) {
	h := NewHub(testOptions(Options{}))
	defer h.Close()
	h.BeginEvent("EQ-ERR", startPayload())
	for i := 0; i < 150; i++ {
		h.PublishError("EQ-ERR", errorPayload(i)) // seq 2..151
	}
	snap := h.Snapshot()
	var seqs []int64
	for _, raw := range snap {
		if f := decodeFrame(t, raw); f.Type == models.TypeError {
			seqs = append(seqs, f.Seq)
		}
	}
	if len(seqs) != 100 || seqs[0] != 52 || seqs[99] != 151 {
		t.Fatalf("replayed %d errors, seq %v…%v; want 100, 52…151", len(seqs), seqs[0], seqs[len(seqs)-1])
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Fatalf("errors not in ascending seq order at %d: %d after %d", i, seqs[i], seqs[i-1])
		}
	}
	var ep models.SnapshotEndPayload
	decodePayload(t, decodeFrame(t, snap[len(snap)-1]), &ep)
	if ep != (models.SnapshotEndPayload{HeadSeq: 151, Replayed: 101}) {
		t.Fatalf("snapshot_end = %+v", ep)
	}
}

func TestNarrativeAndErrorTextAreRedacted(t *testing.T) {
	h := NewHub(testOptions(Options{}))
	defer h.Close()
	h.BeginEvent("EQ-PII", startPayload())
	h.PublishNarrative("EQ-PII", models.NarrativePayload{Zone: models.ZoneRed, Narrative: "Call +212600000001 now"})
	h.PublishError("EQ-PII", models.ErrorPayload{Code: models.ErrSMSFailed, Message: "gateway rejected +212600000004", Phone: "+212600000004", Stage: models.ErrorStageDispatch})
	for _, raw := range h.Snapshot() {
		f := decodeFrame(t, raw)
		switch f.Type {
		case models.TypeNarrativeUpdate:
			var p models.NarrativePayload
			decodePayload(t, f, &p)
			if strings.Contains(p.Narrative, "600000001") {
				t.Fatalf("narrative leaks a phone: %q", p.Narrative)
			}
		case models.TypeError:
			var p models.ErrorPayload
			decodePayload(t, f, &p)
			if strings.Contains(p.Message, "600000004") || p.Phone != "+212600000004" {
				t.Fatalf("error payload = %+v (message must be masked, phone field kept)", p)
			}
		}
	}
}

func TestSlowClientIsDisconnectedWithoutBlocking(t *testing.T) {
	// WriteTimeout is long enough that the slow client's pending write is
	// still waiting when the test starts reading, so the 1013 close frame
	// can actually reach it.
	h, srv := startHub(t, Options{QueueLimit: 16, WriteTimeout: 5 * time.Second})
	const id = "EQ-SLOW"

	// The slow client never reads. A tiny receive buffer makes the kernel
	// stop accepting sooner, which is when its hub queue starts to grow.
	slowDialer := &websocket.Dialer{
		HandshakeTimeout: testTimeout,
		NetDial: func(network, addr string) (net.Conn, error) {
			conn, err := net.Dial(network, addr)
			if tc, ok := conn.(*net.TCPConn); ok && err == nil {
				_ = tc.SetReadBuffer(4096)
			}
			return conn, err
		},
	}
	slow := dialWith(t, slowDialer, srv, nil)
	healthy := dial(t, srv, nil)
	healthy.readSnapshot()

	seqs := make(chan int64, 1024)
	readErr := make(chan error, 1)
	go func() {
		for {
			_ = healthy.conn.SetReadDeadline(time.Now().Add(testTimeout))
			_, raw, err := healthy.conn.ReadMessage()
			if err != nil {
				readErr <- err
				return
			}
			var env models.Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				readErr <- err
				return
			}
			if env.Type != models.TypeHeartbeat {
				seqs <- env.Seq
			}
		}
	}()
	var healthySeq int64
	waitHealthy := func() {
		t.Helper()
		head := headSeq(h)
		for healthySeq < head {
			select {
			case s := <-seqs:
				if s != healthySeq+1 {
					t.Fatalf("healthy client got seq %d after %d", s, healthySeq)
				}
				healthySeq = s
			case err := <-readErr:
				t.Fatalf("healthy client: %v", err)
			case <-time.After(testTimeout):
				t.Fatalf("healthy client stuck at seq %d of %d", healthySeq, head)
			}
		}
	}

	var slowest time.Duration
	text := strings.Repeat("x", 2000)
	publishBurst := func() {
		t.Helper()
		// Bursts of 8 then wait for the healthy client: its queue never gets
		// near the limit, while the slow one's grows once its socket is full.
		for i := 0; i < 8; i++ {
			start := time.Now()
			h.PublishNarrative(id, models.NarrativePayload{Zone: models.ZoneRed, Narrative: text})
			if d := time.Since(start); d > slowest {
				slowest = d
			}
		}
		waitHealthy()
	}

	h.BeginEvent(id, startPayload())
	waitHealthy()
	deadline := time.Now().Add(30 * time.Second)
	for h.Stats().SlowClientDisconnects == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("slow client never disconnected (max queue depth %d)", h.Stats().MaxQueueDepth)
		}
		publishBurst()
	}
	for i := 0; i < 10; i++ {
		publishBurst()
	}

	if slowest > time.Second {
		t.Fatalf("a publish took %v: publishing blocked on a slow client", slowest)
	}
	st := h.Stats()
	if st.SlowClientDisconnects != 1 || st.Clients != 1 || st.MaxQueueDepth < 16 || st.FramesPublished != headSeq(h) {
		t.Fatalf("stats = %+v (head_seq %d)", st, headSeq(h))
	}

	// The slow client gets a gap-free prefix of the stream, then 1013.
	_ = slow.conn.SetReadDeadline(time.Now().Add(testTimeout))
	var last int64
	for {
		_, raw, err := slow.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseTryAgainLater) {
				t.Fatalf("slow client ended with %v, want close 1013", err)
			}
			break
		}
		f := decodeFrame(t, raw)
		if f.Seq != 0 {
			if f.Seq != last+1 {
				t.Fatalf("slow client got seq %d after %d", f.Seq, last)
			}
			last = f.Seq
		}
	}
	if last == 0 || last >= headSeq(h) {
		t.Fatalf("slow client received up to seq %d of %d", last, headSeq(h))
	}
}

func TestOriginPolicyOnUpgrade(t *testing.T) {
	_, srv := startHub(t, Options{})

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv), http.Header{"Origin": {"http://evil.example"}})
	if err == nil {
		t.Fatal("upgrade from a disallowed origin succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed origin: resp %v, err %v; want 403", resp, err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"origin_not_allowed"`) {
		t.Fatalf("403 body = %s", body)
	}

	for name, hdr := range map[string]http.Header{
		"allowlisted": {"Origin": {allowedOrigin}},
		"no origin":   nil,
		"same origin": {"Origin": {srv.URL}},
	} {
		c := dial(t, srv, hdr)
		if f := c.read(); f.Type != models.TypeSnapshotBegin {
			t.Fatalf("%s: first frame %s", name, f.Type)
		}
	}
}

func TestCloseDisconnectsClientsAndRefusesNewOnes(t *testing.T) {
	h, srv := startHub(t, Options{})
	c := dial(t, srv, nil)
	c.readSnapshot()

	done := make(chan struct{})
	go func() {
		h.Close()
		close(done)
	}()
	_ = c.conn.SetReadDeadline(time.Now().Add(testTimeout))
	if _, _, err := c.conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("client ended with %v, want close 1001", err)
	}
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Close did not return")
	}
	if n := h.Stats().Clients; n != 0 {
		t.Fatalf("%d clients after Close", n)
	}

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("after Close: status %d, want 503", resp.StatusCode)
	}
	h.Close() // idempotent
}
