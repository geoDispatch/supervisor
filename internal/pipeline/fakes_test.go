package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// Deterministic fakes for every pipeline collaborator. No network, no clock
// dependence except where a test measures concurrency or a deadline.

var testEpicenter = models.Coordinates{Lat: 33.5731, Lng: -7.5898}

const testRadiusKm = 15.0

// kmPerDegLat converts a north offset in km to degrees with the same Earth
// radius the zones package uses, so distances round-trip through Haversine.
const kmPerDegLat = 6371.0 * 3.141592653589793 / 180.0

func phoneN(n int) string { return fmt.Sprintf("+2126%08d", n) }

func testInput(id string) *models.SensorInput {
	return &models.SensorInput{
		EventID:        id,
		DisasterType:   models.Earthquake,
		Timestamp:      1757699999000,
		Severity:       6.8,
		Epicenter:      testEpicenter,
		RadiusKm:       testRadiusKm,
		DepthKm:        10.5,
		AftershockRisk: models.AftershockHigh,
		TsunamiRisk:    false,
	}
}

// ── Store ─────────────────────────────────────────────────────

type fakeStore struct {
	mu sync.Mutex

	phones      []string
	phonesErr   error
	phonesGate  chan struct{} // when set, PhonesNearEpicenter waits for it to close
	shelters    []models.Shelter
	sheltersErr error
	notInserted bool // InsertEvent reports inserted=false
	insertErr   error
	flagErr     error
	logErr      error

	inserted []string
	flagged  []string
	logged   []string
}

func (s *fakeStore) PhonesNearEpicenter(ctx context.Context, _ models.Coordinates, _ float64) ([]string, error) {
	if s.phonesGate != nil {
		select {
		case <-s.phonesGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return append([]string(nil), s.phones...), s.phonesErr
}

func (s *fakeStore) NearestShelters(context.Context, models.Coordinates, int) ([]models.Shelter, error) {
	return s.shelters, s.sheltersErr
}

func (s *fakeStore) InsertEvent(_ context.Context, in *models.SensorInput) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return false, s.insertErr
	}
	if s.notInserted {
		return false, nil
	}
	for _, id := range s.inserted {
		if id == in.EventID {
			return false, nil
		}
	}
	s.inserted = append(s.inserted, in.EventID)
	return true, nil
}

func (s *fakeStore) InsertDeviceLog(_ context.Context, _ string, d models.DeviceDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logged = append(s.logged, d.Phone)
	return s.logErr
}

func (s *fakeStore) FlagRescue(_ context.Context, _ string, d models.DeviceDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flagged = append(s.flagged, d.Phone)
	return s.flagErr
}

// ── Network ───────────────────────────────────────────────────

type fakeNet struct {
	mu sync.Mutex

	coords     map[string]models.Coordinates // located phones
	locErr     map[string]error
	reachable  map[string]bool // default: CONNECTED_DATA
	reachErr   map[string]error
	locDelay   time.Duration
	qos        models.QoSStatus
	qosErr     error
	congestion models.CongestionLevel
	congErr    error
	upgradeErr error

	inFlight    int
	maxInFlight int
	upgrades    int
}

func newFakeNet() *fakeNet {
	return &fakeNet{
		coords:     map[string]models.Coordinates{},
		locErr:     map[string]error{},
		reachable:  map[string]bool{},
		reachErr:   map[string]error{},
		qos:        models.QoSRequested,
		congestion: models.CongestionHigh,
	}
}

// place registers phone at distKm north of the test epicentre.
func (n *fakeNet) place(phone string, distKm float64, reachable bool) {
	n.coords[phone] = models.Coordinates{Lat: testEpicenter.Lat + distKm/kmPerDegLat, Lng: testEpicenter.Lng}
	n.reachable[phone] = reachable
}

func (n *fakeNet) Location(ctx context.Context, phone string) (*models.CAMARALocationResponse, error) {
	n.mu.Lock()
	n.inFlight++
	if n.inFlight > n.maxInFlight {
		n.maxInFlight = n.inFlight
	}
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.inFlight--
		n.mu.Unlock()
	}()
	if n.locDelay > 0 {
		select {
		case <-time.After(n.locDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.locErr[phone]; err != nil {
		return nil, err
	}
	c, ok := n.coords[phone]
	if !ok {
		return nil, fmt.Errorf("device %s not found", phone)
	}
	return &models.CAMARALocationResponse{
		LastLocationTime: "2026-09-12T10:00:00Z",
		Area:             models.CAMARALocationArea{AreaType: "CIRCLE", Center: c, Radius: 500},
	}, nil
}

func (n *fakeNet) Reachability(_ context.Context, phone string) (*models.CAMARAReachabilityResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.reachErr[phone]; err != nil {
		return nil, err
	}
	st := models.ReachableData
	if r, ok := n.reachable[phone]; ok && !r {
		st = models.NotConnected
	}
	return &models.CAMARAReachabilityResponse{LastStatusTime: "2026-09-12T10:00:00Z", ReachabilityStatus: st}, nil
}

func (n *fakeNet) RequestQoS(context.Context, models.Coordinates, string) (models.NetworkStatus, error) {
	return models.NetworkStatus{QoSStatus: n.qos, SMSDeliveryRate: 0.9}, n.qosErr
}

func (n *fakeNet) UpgradeQoS(context.Context, models.Coordinates) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.upgrades++
	return n.upgradeErr
}

func (n *fakeNet) Congestion(context.Context, models.Coordinates, string) (models.CongestionLevel, error) {
	return n.congestion, n.congErr
}

func (n *fakeNet) Source() string { return models.NetworkSourceMockCAMARA }

// ── Decider ───────────────────────────────────────────────────

type fakeAI struct {
	mu     sync.Mutex
	decide func(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error)
	reqs   []models.AgentRequest
}

func (a *fakeAI) Decide(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
	a.mu.Lock()
	a.reqs = append(a.reqs, req)
	a.mu.Unlock()
	return a.decide(ctx, req)
}

func (a *fakeAI) requests() []models.AgentRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]models.AgentRequest(nil), a.reqs...)
}

// echoAgent answers every device with decide(dev), echoing event and zone.
func echoAgent(decide func(models.TriagedDevice) models.DeviceDecision) *fakeAI {
	return &fakeAI{decide: func(_ context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
		resp := &models.AgentResponse{EventID: req.EventID, Zone: req.Zone, Confidence: 0.9}
		for _, d := range req.Devices {
			resp.Decisions = append(resp.Decisions, decide(d))
		}
		return resp, nil
	}}
}

func decision(d models.TriagedDevice, action models.ActionType) models.DeviceDecision {
	dd := models.DeviceDecision{Phone: d.Phone, ZoneConfirmed: d.Zone, Action: action, Confidence: 0.8, Reasoning: "audit only"}
	if wantsSMS(action) {
		dd.SMSMessage = "Go to the nearest shelter"
	}
	if wantsRescue(action) {
		dd.RescuePriority = 1
	}
	return dd
}

func actionAgent(action models.ActionType) *fakeAI {
	return echoAgent(func(d models.TriagedDevice) models.DeviceDecision { return decision(d, action) })
}

// ── Messenger ─────────────────────────────────────────────────

type fakeSMS struct {
	mu         sync.Mutex
	configured bool
	status     models.SMSStatus
	sends      int
}

func (s *fakeSMS) Send(context.Context, string, string) models.SMSStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends++
	return s.status
}

func (s *fakeSMS) Configured() bool { return s.configured }

// ── Publisher ─────────────────────────────────────────────────

type frame struct {
	typ     string
	eventID string
	payload any
}

type recorder struct {
	mu     sync.Mutex
	frames []frame
}

func (r *recorder) add(typ, id string, p any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, frame{typ, id, p})
}

func (r *recorder) BeginEvent(id string, p models.EventStartPayload) {
	r.add(models.TypeEventStart, id, p)
}
func (r *recorder) PublishContext(id string, p models.EventContextPayload) {
	r.add(models.TypeEventContext, id, p)
}
func (r *recorder) PublishDevice(id string, p models.DeviceUpdatePayload) {
	r.add(models.TypeDeviceUpdate, id, p)
}
func (r *recorder) PublishSummary(id string, p models.ZoneSummaryPayload) {
	r.add(models.TypeZoneSummary, id, p)
}
func (r *recorder) PublishNarrative(id string, p models.NarrativePayload) {
	r.add(models.TypeNarrativeUpdate, id, p)
}
func (r *recorder) PublishError(id string, p models.ErrorPayload) { r.add(models.TypeError, id, p) }
func (r *recorder) CompleteEvent(id string, p models.EventCompletePayload) {
	r.add(models.TypeEventComplete, id, p)
}

func (r *recorder) all() []frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]frame(nil), r.frames...)
}

func (r *recorder) ofType(typ string) []frame {
	var out []frame
	for _, f := range r.all() {
		if f.typ == typ {
			out = append(out, f)
		}
	}
	return out
}

func (r *recorder) errors() []models.ErrorPayload {
	var out []models.ErrorPayload
	for _, f := range r.ofType(models.TypeError) {
		out = append(out, f.payload.(models.ErrorPayload))
	}
	return out
}

// latestDevices is the last device_update per phone.
func (r *recorder) latestDevices() map[string]models.DeviceUpdatePayload {
	out := map[string]models.DeviceUpdatePayload{}
	for _, f := range r.ofType(models.TypeDeviceUpdate) {
		p := f.payload.(models.DeviceUpdatePayload)
		out[p.Phone] = p
	}
	return out
}

func (r *recorder) lastSummary(t *testing.T) models.ZoneSummaryPayload {
	t.Helper()
	s := r.ofType(models.TypeZoneSummary)
	if len(s) == 0 {
		t.Fatal("no zone_summary published")
	}
	return s[len(s)-1].payload.(models.ZoneSummaryPayload)
}

func (r *recorder) lastContext(t *testing.T) models.EventContextPayload {
	t.Helper()
	s := r.ofType(models.TypeEventContext)
	if len(s) == 0 {
		t.Fatal("no event_context published")
	}
	return s[len(s)-1].payload.(models.EventContextPayload)
}

// complete checks the lifecycle invariants of one event and returns its
// event_complete: event_start first, event_complete last and exactly once,
// every frame for eventID.
func (r *recorder) complete(t *testing.T, eventID string) models.EventCompletePayload {
	t.Helper()
	fs := r.all()
	if len(fs) < 2 {
		t.Fatalf("only %d frames published", len(fs))
	}
	if fs[0].typ != models.TypeEventStart {
		t.Fatalf("first frame is %s, want event_start", fs[0].typ)
	}
	if last := fs[len(fs)-1]; last.typ != models.TypeEventComplete {
		t.Fatalf("last frame is %s, want event_complete", last.typ)
	}
	for i, f := range fs {
		if f.eventID != eventID {
			t.Fatalf("frame %d (%s) is for event %q, want %q", i, f.typ, f.eventID, eventID)
		}
		if f.typ == models.TypeEventStart && i != 0 {
			t.Fatalf("second event_start at frame %d", i)
		}
		if f.typ == models.TypeEventComplete && i != len(fs)-1 {
			t.Fatalf("event_complete at frame %d is not the last frame", i)
		}
	}
	return fs[len(fs)-1].payload.(models.EventCompletePayload)
}

// wireJSON is every frame payload marshalled as the hub would send it.
func (r *recorder) wireJSON(t *testing.T) string {
	t.Helper()
	var all []byte
	for _, f := range r.all() {
		b, err := json.Marshal(f.payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", f.typ, err)
		}
		all = append(all, b...)
	}
	return string(all)
}

// ── harness ───────────────────────────────────────────────────

type harness struct {
	store *fakeStore
	net   *fakeNet
	ai    *fakeAI
	sms   *fakeSMS
	pub   *recorder
	cfg   Config
}

func newHarness() *harness {
	return &harness{
		store: &fakeStore{},
		net:   newFakeNet(),
		ai:    actionAgent(models.ActionNone),
		sms:   &fakeSMS{},
		pub:   &recorder{},
		cfg:   Config{BatchSize: 20, CamaraConcurrency: 8, ReachabilityTimeout: time.Second, AgentTimeout: 5 * time.Second, PipelineTimeout: 10 * time.Second},
	}
}

// device registers phone n in the store and places it on the fake network.
func (h *harness) device(n int, distKm float64, reachable bool) string {
	p := phoneN(n)
	h.store.phones = append(h.store.phones, p)
	h.net.place(p, distKm, reachable)
	return p
}

func (h *harness) manager() *Manager {
	return NewManager(h.cfg, h.store, h.net, h.ai, h.sms, h.pub)
}

// run submits one event, waits for its pipeline and returns event_complete.
func (h *harness) run(t *testing.T, id string) models.EventCompletePayload {
	t.Helper()
	m := h.manager()
	res := m.Submit(context.Background(), testInput(id))
	if res.Status != 202 {
		t.Fatalf("Submit = %d %v, want 202", res.Status, res.Body)
	}
	m.Wait()
	return h.pub.complete(t, id)
}
