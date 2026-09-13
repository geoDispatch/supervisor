package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// Distances inside the 15 km test radius: red ≤ 4.95, orange ≤ 9.9, green ≤ 15.
const (
	redKm    = 2.0
	orangeKm = 7.0
	greenKm  = 12.0
)

func TestRunZeroPhones(t *testing.T) {
	h := newHarness()
	c := h.run(t, "EQ-0")

	fs := h.pub.all()
	if len(fs) != 3 || fs[1].typ != models.TypeEventContext {
		t.Fatalf("frames = %v, want event_start, event_context, event_complete", types(fs))
	}
	ctx := fs[1].payload.(models.EventContextPayload)
	if ctx.DevicesInRadius != 0 || ctx.Network.CongestionLevel != models.CongestionUnknown || ctx.Network.QoSStatus != models.QoSInactive {
		t.Errorf("event_context = %+v", ctx)
	}
	if ctx.NetworkSource != models.NetworkSourceMockCAMARA || ctx.SMSGateway != models.SMSGatewayNotConfigured {
		t.Errorf("declared source/gateway = %q/%q", ctx.NetworkSource, ctx.SMSGateway)
	}
	if c.Status != models.LifecycleNoDevices || c.DevicesInRadius != 0 || c.FatalError != nil {
		t.Errorf("event_complete = %+v", c)
	}
	start := fs[0].payload.(models.EventStartPayload)
	if start.ZoneBands != (models.ZoneBands{Red: 0.33, Orange: 0.66, Green: 1.0}) || start.SensorTimestamp != 1757699999000 {
		t.Errorf("event_start = %+v", start)
	}
}

func TestRunHappyPathFrames(t *testing.T) {
	h := newHarness()
	h.store.shelters = []models.Shelter{{Name: "Stadium", Capacity: 5000}}
	h.device(1, redKm, false)
	h.device(2, orangeKm, true)
	h.device(3, greenKm, true)
	h.ai = echoAgent(func(d models.TriagedDevice) models.DeviceDecision {
		if d.Zone == models.ZoneRed {
			return decision(d, models.ActionRescue)
		}
		return decision(d, models.ActionNone)
	})

	c := h.run(t, "EQ-1")
	if c.Status != models.LifecycleCompleted || c.DevicesInRadius != 3 || c.DevicesTriaged != 3 || c.DevicesDecided != 3 {
		t.Fatalf("event_complete = %+v", c)
	}
	if errs := h.pub.errors(); len(errs) != 0 {
		t.Fatalf("unexpected errors %+v", errs)
	}
	ctx := h.pub.lastContext(t)
	if ctx.SheltersStatus != models.SheltersStatusOK || len(ctx.Shelters) != 1 || ctx.Network.CongestionLevel != models.CongestionHigh {
		t.Errorf("event_context = %+v", ctx)
	}

	// Every device: triaged first, then decided.
	perPhone := map[string][]models.Stage{}
	for _, f := range h.pub.ofType(models.TypeDeviceUpdate) {
		p := f.payload.(models.DeviceUpdatePayload)
		perPhone[p.Phone] = append(perPhone[p.Phone], p.Stage)
		if p.Stage == models.StageTriaged && (p.Action != nil || p.Confidence != nil || p.EscalatedZone != nil) {
			t.Errorf("triaged frame for %s carries decision fields", p.Phone)
		}
	}
	for phone, stages := range perPhone {
		if len(stages) != 2 || stages[0] != models.StageTriaged || stages[1] != models.StageDecided {
			t.Errorf("%s stages = %v", phone, stages)
		}
	}
	red := h.pub.latestDevices()[phoneN(1)]
	if red.Zone != models.ZoneRed || !red.RescueFlag || red.RescueStatus != models.RescueStatusRecorded ||
		red.RescuePriority != 1 || red.Confidence == nil || *red.Confidence != 0.8 || red.Reachable {
		t.Errorf("red device = %+v", red)
	}
	if n := len(h.store.logged); n != 3 {
		t.Errorf("%d device logs written, want 3", n)
	}
	// The wire never carries the AI reasoning or the SMS text.
	if w := h.pub.wireJSON(t); strings.Contains(w, "audit only") || strings.Contains(w, "reasoning") {
		t.Error("reasoning leaked into a frame")
	}
}

func TestRunSMSFailure(t *testing.T) {
	h := newHarness()
	h.sms = &fakeSMS{configured: true, status: models.SMSStatusFailed}
	h.ai = actionAgent(models.ActionSMS)
	h.device(1, orangeKm, true)
	h.device(2, greenKm, true)

	c := h.run(t, "EQ-SMS")
	for phone, d := range h.pub.latestDevices() {
		if d.SMSStatus != models.SMSStatusFailed || d.SMSSent {
			t.Errorf("%s: sms_status %s sms_sent %v", phone, d.SMSStatus, d.SMSSent)
		}
	}
	errs := h.pub.errors()
	if len(errs) != 2 {
		t.Fatalf("errors = %+v, want 2 SMS_FAILED", errs)
	}
	for _, e := range errs {
		if e.Code != models.ErrSMSFailed || e.Fatal || e.Stage != models.ErrorStageDispatch || e.Phone == "" {
			t.Errorf("error = %+v", e)
		}
	}
	if c.Status != models.LifecycleCompletedWithFailures || c.Failures.SMS != 2 || c.SMSNotSentNoGateway != 0 {
		t.Errorf("event_complete = %+v", c)
	}
	if s := h.pub.lastSummary(t); s.Orange.SMSFailed != 1 || s.Green.SMSFailed != 1 || s.Orange.SMSSent != 0 {
		t.Errorf("summary = %+v", s)
	}
	if h.pub.lastContext(t).SMSGateway != models.SMSGatewayConfigured {
		t.Error("sms_gateway should be configured")
	}
}

func TestRunSMSNotConfigured(t *testing.T) {
	h := newHarness()
	h.ai = actionAgent(models.ActionBoth)
	h.device(1, redKm, true)
	h.device(2, orangeKm, true)

	c := h.run(t, "EQ-NOSMS")
	for phone, d := range h.pub.latestDevices() {
		if d.SMSStatus != models.SMSStatusNotConfigured || d.SMSSent {
			t.Errorf("%s: sms_status %s sms_sent %v", phone, d.SMSStatus, d.SMSSent)
		}
	}
	if h.sms.sends != 0 {
		t.Errorf("Send called %d times without a gateway", h.sms.sends)
	}
	if errs := h.pub.errors(); len(errs) != 0 {
		t.Errorf("errors = %+v, want none", errs)
	}
	if c.Status != models.LifecycleCompleted || c.SMSNotSentNoGateway != 2 || c.Failures.SMS != 0 {
		t.Errorf("event_complete = %+v", c)
	}
	if s := h.pub.lastSummary(t); s.Red.SMSSent != 0 || s.Red.SMSFailed != 0 {
		t.Errorf("summary = %+v", s)
	}
}

func TestRunSMSSent(t *testing.T) {
	h := newHarness()
	h.sms = &fakeSMS{configured: true, status: models.SMSStatusSent}
	h.ai = actionAgent(models.ActionSMS)
	h.device(1, orangeKm, true)
	c := h.run(t, "EQ-SENT")
	d := h.pub.latestDevices()[phoneN(1)]
	if d.SMSStatus != models.SMSStatusSent || !d.SMSSent || c.Status != models.LifecycleCompleted {
		t.Fatalf("device = %+v, complete = %+v", d, c)
	}
	if s := h.pub.lastSummary(t); s.Orange.SMSSent != 1 {
		t.Errorf("summary = %+v", s)
	}
}

func TestRunInvalidAgentResponses(t *testing.T) {
	const intruder = "+212699999999"
	cases := []struct {
		name   string
		mutate func(*models.AgentResponse)
	}{
		{"unknown phone", func(r *models.AgentResponse) { r.Decisions[0].Phone = intruder }},
		{"duplicate phone", func(r *models.AgentResponse) { r.Decisions[1].Phone = r.Decisions[0].Phone }},
		{"zone mismatch", func(r *models.AgentResponse) { r.Zone = models.ZoneGreen }},
		{"missing decision", func(r *models.AgentResponse) { r.Decisions = r.Decisions[1:] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			h.device(1, redKm, true)
			h.device(2, redKm+0.5, true)
			h.device(3, orangeKm, true)
			valid := actionAgent(models.ActionNone)
			h.ai = &fakeAI{decide: func(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
				resp, _ := valid.decide(ctx, req)
				if req.Zone == models.ZoneRed {
					tc.mutate(resp)
				}
				return resp, nil
			}}

			c := h.run(t, "EQ-BAD")
			devs := h.pub.latestDevices()
			for _, p := range []string{phoneN(1), phoneN(2)} {
				if devs[p].Stage != models.StageDecisionFailed || devs[p].Action != nil {
					t.Errorf("%s = %s, want decision_failed with null action", p, devs[p].Stage)
				}
			}
			if devs[phoneN(3)].Stage != models.StageDecided {
				t.Errorf("orange batch should still be decided, got %s", devs[phoneN(3)].Stage)
			}
			errs := h.pub.errors()
			if len(errs) != 1 || errs[0].Code != models.ErrAgentInvalidResponse || errs[0].Fatal || errs[0].Stage != models.ErrorStageDecision {
				t.Fatalf("errors = %+v", errs)
			}
			if strings.Contains(h.pub.wireJSON(t), intruder) {
				t.Error("the unknown phone appears in a frame")
			}
			if _, ok := devs[intruder]; ok {
				t.Error("device_update published for the unknown phone")
			}
			if c.Status != models.LifecycleCompletedWithFailures || c.Failures.Decision != 2 || c.DevicesDecided != 1 {
				t.Errorf("event_complete = %+v", c)
			}
			if s := h.pub.lastSummary(t); s.Red.DecisionFailed != 2 || s.Red.Decided != 0 || s.Orange.Decided != 1 {
				t.Errorf("summary = %+v", s)
			}
			if len(h.store.logged) != 1 || len(h.store.flagged) != 0 {
				t.Errorf("dispatch ran for a rejected batch: logged %v flagged %v", h.store.logged, h.store.flagged)
			}
		})
	}
}

func TestRunAgentTransportError(t *testing.T) {
	h := newHarness()
	h.device(1, redKm, true)
	h.device(2, greenKm, true)
	h.ai = &fakeAI{decide: func(context.Context, models.AgentRequest) (*models.AgentResponse, error) {
		return nil, errors.New("agent returned HTTP 500 for +212600000001")
	}}

	c := h.run(t, "EQ-AGENT")
	for p, d := range h.pub.latestDevices() {
		if d.Stage != models.StageDecisionFailed {
			t.Errorf("%s = %s", p, d.Stage)
		}
	}
	errs := h.pub.errors()
	if len(errs) != 2 {
		t.Fatalf("errors = %+v, want one AGENT_ERROR per batch", errs)
	}
	for _, e := range errs {
		if e.Code != models.ErrAgentError || e.Fatal || e.Phone != "" {
			t.Errorf("error = %+v", e)
		}
		if strings.Contains(e.Message, "+212600000001") {
			t.Errorf("message leaks a phone: %q", e.Message)
		}
	}
	if c.Status != models.LifecycleCompletedWithFailures || c.Failures.Decision != 2 {
		t.Errorf("event_complete = %+v", c)
	}
}

func TestRunSummaryReachabilityComesFromCAMARA(t *testing.T) {
	h := newHarness()
	h.device(1, redKm, true)
	h.device(2, redKm+0.1, true)
	h.device(3, redKm+0.2, false)
	h.ai = actionAgent(models.ActionNone) // "none" must not make a device unreachable

	h.run(t, "EQ-REACH")
	s := h.pub.lastSummary(t)
	if s.Red.Total != 3 || s.Red.Reachable != 2 || s.Red.Unreachable != 1 || s.Red.Decided != 3 {
		t.Fatalf("red stats = %+v", s.Red)
	}
	if s.DevicesInRadius != 3 || s.Triaged != 3 || s.LocationFailed != 0 {
		t.Errorf("summary = %+v", s)
	}
	if d := h.pub.latestDevices()[phoneN(1)]; !d.Reachable || d.ReachabilityStatus != models.ReachableData {
		t.Errorf("device = %+v", d)
	}
}

func TestRunOrangeRescue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flagErr error
		status  models.RescueStatus
	}{
		{"recorded", nil, models.RescueStatusRecorded},
		{"failed", errors.New("insert into rescue_flags: connection reset"), models.RescueStatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			h.store.flagErr = tc.flagErr
			h.ai = actionAgent(models.ActionRescue)
			p := h.device(1, orangeKm, true)

			c := h.run(t, "EQ-RESCUE")
			d := h.pub.latestDevices()[p]
			if d.Zone != models.ZoneOrange || !d.RescueFlag || d.RescueStatus != tc.status {
				t.Fatalf("device = %+v", d)
			}
			if s := h.pub.lastSummary(t); s.Orange.RescueFlagged != 1 {
				t.Errorf("orange rescue_flagged = %d", s.Orange.RescueFlagged)
			}
			errs := h.pub.errors()
			if tc.flagErr == nil {
				if len(errs) != 0 || c.Status != models.LifecycleCompleted {
					t.Errorf("errors %+v, complete %+v", errs, c)
				}
				return
			}
			if len(errs) != 1 || errs[0].Code != models.ErrDBError || errs[0].Fatal || errs[0].Phone != p || errs[0].Stage != models.ErrorStageDispatch {
				t.Errorf("errors = %+v", errs)
			}
			if c.Status != models.LifecycleCompletedWithFailures || c.Failures.Rescue != 1 {
				t.Errorf("event_complete = %+v", c)
			}
		})
	}
}

func TestRunEscalationKeepsGoZone(t *testing.T) {
	h := newHarness()
	p := h.device(1, greenKm, true)
	h.ai = echoAgent(func(d models.TriagedDevice) models.DeviceDecision {
		dd := decision(d, models.ActionSMS)
		dd.ZoneEscalated, dd.ZoneConfirmed = true, models.ZoneOrange
		return dd
	})

	h.run(t, "EQ-ESC")
	d := h.pub.latestDevices()[p]
	if d.Zone != models.ZoneGreen || !d.ZoneEscalated || d.EscalatedZone == nil || *d.EscalatedZone != models.ZoneOrange {
		t.Fatalf("device = %+v", d)
	}
	if s := h.pub.lastSummary(t); s.Green.Total != 1 || s.Orange.Total != 0 {
		t.Errorf("summary counts the escalated zone: %+v", s)
	}
}

func TestRunLocationFailure(t *testing.T) {
	h := newHarness()
	ok := h.device(1, redKm, true)
	broken := h.device(2, orangeKm, true)
	slow := h.device(3, greenKm, true)
	h.net.locErr[broken] = errors.New("HTTP 500 from CAMARA")
	h.net.locErr[slow] = fmt.Errorf("location: %w", context.DeadlineExceeded)

	c := h.run(t, "EQ-LOC")
	devs := h.pub.latestDevices()
	if len(devs) != 1 || devs[ok].Stage != models.StageDecided {
		t.Fatalf("devices = %v, want only %s", devs, ok)
	}
	byPhone := map[string]models.ErrorPayload{}
	for _, e := range h.pub.errors() {
		byPhone[e.Phone] = e
	}
	if e := byPhone[broken]; e.Code != models.ErrCAMARAError || e.Stage != models.ErrorStageTriage || e.Fatal {
		t.Errorf("broken error = %+v", e)
	}
	if e := byPhone[slow]; e.Code != models.ErrCAMARATimeout {
		t.Errorf("timeout error = %+v", e)
	}
	if c.Status != models.LifecycleCompletedWithFailures || c.Failures.Location != 2 || c.DevicesTriaged != 1 || c.DevicesInRadius != 3 {
		t.Errorf("event_complete = %+v", c)
	}
	if s := h.pub.lastSummary(t); s.LocationFailed != 2 || s.Triaged != 1 {
		t.Errorf("summary = %+v", s)
	}
	for _, r := range h.ai.requests() {
		for _, d := range r.Devices {
			if d.Phone != ok {
				t.Errorf("AI asked about untriaged device %s", d.Phone)
			}
		}
	}
}

func TestRunReachabilityFailure(t *testing.T) {
	h := newHarness()
	p := h.device(1, redKm, true)
	h.net.reachErr[p] = context.DeadlineExceeded

	c := h.run(t, "EQ-RF")
	d := h.pub.latestDevices()[p]
	if d.ReachabilityStatus != models.NotConnected || !d.ReachabilityAssumed || d.Reachable {
		t.Fatalf("device = %+v", d)
	}
	errs := h.pub.errors()
	if len(errs) != 1 || errs[0].Code != models.ErrCAMARATimeout || errs[0].Phone != p || errs[0].Stage != models.ErrorStageTriage {
		t.Fatalf("errors = %+v", errs)
	}
	if c.Status != models.LifecycleCompletedWithFailures || c.Failures.Reachability != 1 || c.DevicesDecided != 1 {
		t.Errorf("event_complete = %+v", c)
	}
	reqs := h.ai.requests()
	if len(reqs) != 1 || reqs[0].Devices[0].ReachabilityStatus != models.NotConnected {
		t.Errorf("AI request = %+v", reqs)
	}
}

func TestRunPhoneLookupFailureIsFatal(t *testing.T) {
	h := newHarness()
	h.store.phonesErr = errors.New("relation \"devices\" does not exist")

	c := h.run(t, "EQ-DB")
	fs := h.pub.all()
	if len(fs) != 3 || fs[1].typ != models.TypeError {
		t.Fatalf("frames = %v, want event_start, error, event_complete", types(fs))
	}
	e := fs[1].payload.(models.ErrorPayload)
	if e.Code != models.ErrDBError || !e.Fatal || e.Stage != models.ErrorStageLookup || e.Phone != "" {
		t.Errorf("error = %+v", e)
	}
	if c.Status != models.LifecycleFailed || c.FatalError == nil || c.FatalError.Code != models.ErrDBError || c.FatalError.Message != e.Message {
		t.Errorf("event_complete = %+v", c)
	}
}

func TestRunShelterFailureIsNotFatal(t *testing.T) {
	h := newHarness()
	h.store.sheltersErr = errors.New("shelters: timeout")
	h.device(1, redKm, true)

	c := h.run(t, "EQ-SH")
	ctx := h.pub.lastContext(t)
	if ctx.SheltersStatus != models.SheltersStatusUnavailable || ctx.Shelters != nil {
		t.Errorf("event_context = %+v", ctx)
	}
	errs := h.pub.errors()
	if len(errs) != 1 || errs[0].Code != models.ErrDBError || errs[0].Fatal || errs[0].Stage != models.ErrorStageContext {
		t.Fatalf("errors = %+v", errs)
	}
	// A shelter outage is not one of the per-step failures.
	if c.Status != models.LifecycleCompleted || c.DevicesDecided != 1 {
		t.Errorf("event_complete = %+v", c)
	}
}

func TestRunAreaNetworkFailuresAreNotFatal(t *testing.T) {
	h := newHarness()
	h.net.qosErr = errors.New("qos: 503")
	h.net.congErr = errors.New("congestion: 500")
	h.device(1, redKm, true)

	c := h.run(t, "EQ-NET")
	ctx := h.pub.lastContext(t)
	if ctx.Network.QoSStatus != models.QoSFailed || ctx.Network.CongestionLevel != models.CongestionUnknown {
		t.Errorf("network = %+v", ctx.Network)
	}
	codes := map[models.ErrorCode]bool{}
	for _, e := range h.pub.errors() {
		codes[e.Code] = !e.Fatal
	}
	if !codes[models.ErrQoSFailed] || !codes[models.ErrCAMARAError] {
		t.Errorf("errors = %+v", h.pub.errors())
	}
	if c.Status != models.LifecycleCompleted {
		t.Errorf("status = %s", c.Status)
	}
}

func TestRunNarrativeAndQoSUpgrade(t *testing.T) {
	h := newHarness()
	h.device(1, redKm, true)
	h.device(2, orangeKm, true)
	h.ai = &fakeAI{decide: func(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
		resp, _ := actionAgent(models.ActionNone).decide(ctx, req)
		resp.GovNarrative = "  1 device in " + string(req.Zone) + "; call +212600000001 ok  "
		resp.RequestQoS = true
		return resp, nil
	}}

	h.run(t, "EQ-NAR")
	ns := h.pub.ofType(models.TypeNarrativeUpdate)
	if len(ns) != 2 {
		t.Fatalf("%d narratives, want 2", len(ns))
	}
	for i, f := range ns {
		n := f.payload.(models.NarrativePayload)
		if n.BatchIndex != i || strings.Contains(n.Narrative, "+212600000001") || strings.HasPrefix(n.Narrative, " ") {
			t.Errorf("narrative %d = %+v", i, n)
		}
	}
	if n := ns[0].payload.(models.NarrativePayload); n.Zone != models.ZoneRed {
		t.Errorf("first narrative zone = %s", n.Zone)
	}
	if h.net.upgrades != 1 {
		t.Errorf("UpgradeQoS called %d times, want once per event", h.net.upgrades)
	}
	ctxs := h.pub.ofType(models.TypeEventContext)
	if len(ctxs) != 2 || ctxs[1].payload.(models.EventContextPayload).Network.QoSStatus != models.QoSActive {
		t.Fatalf("event_context frames = %d, want the initial one and a qos active re-emit", len(ctxs))
	}
	// The second AI request sees the upgraded network.
	if reqs := h.ai.requests(); reqs[1].NetworkStatus.QoSStatus != models.QoSActive {
		t.Errorf("second request network = %+v", reqs[1].NetworkStatus)
	}
}

func TestRunQoSUpgradeFailure(t *testing.T) {
	h := newHarness()
	h.net.upgradeErr = errors.New("qos extend: 409")
	h.device(1, redKm, true)
	h.ai = &fakeAI{decide: func(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
		resp, _ := actionAgent(models.ActionNone).decide(ctx, req)
		resp.RequestQoS = true
		return resp, nil
	}}
	c := h.run(t, "EQ-QOS")
	errs := h.pub.errors()
	if len(errs) != 1 || errs[0].Code != models.ErrQoSFailed || errs[0].Fatal {
		t.Fatalf("errors = %+v", errs)
	}
	if n := len(h.pub.ofType(models.TypeEventContext)); n != 1 {
		t.Errorf("%d event_context frames, want 1", n)
	}
	if c.Status != models.LifecycleCompleted {
		t.Errorf("status = %s", c.Status)
	}
}

func TestRunMixedZonesSendsZonePureBatches(t *testing.T) {
	h := newHarness()
	// 60 devices spread over every zone, interleaved so the input order
	// never matches the zone order.
	for i := 0; i < 60; i++ {
		km := []float64{redKm, orangeKm, greenKm}[i%3] + float64(i)/100
		h.device(i+1, km, i%4 != 0)
	}
	h.ai = actionAgent(models.ActionNone)

	c := h.run(t, "EQ-60")
	reqs := h.ai.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d AI requests, want 3 (20 per zone)", len(reqs))
	}
	lastRank := 4
	for i, r := range reqs {
		if r.BatchIndex != i {
			t.Errorf("request %d has batch_index %d", i, r.BatchIndex)
		}
		if len(r.Devices) == 0 || len(r.Devices) > 20 {
			t.Errorf("request %d has %d devices", i, len(r.Devices))
		}
		for _, d := range r.Devices {
			if d.Zone != r.Zone {
				t.Errorf("request %d (%s) contains a %s device", i, r.Zone, d.Zone)
			}
		}
		if rank := models.ZoneRank(r.Zone); rank > lastRank {
			t.Errorf("request %d zone %s after a less severe zone", i, r.Zone)
		} else {
			lastRank = rank
		}
	}
	if c.DevicesDecided != 60 || c.Status != models.LifecycleCompleted {
		t.Errorf("event_complete = %+v", c)
	}
}

func TestRunSmallBatchSize(t *testing.T) {
	h := newHarness()
	h.cfg.BatchSize = 4
	for i := 0; i < 10; i++ {
		h.device(i+1, redKm+float64(i)/100, true)
	}
	h.run(t, "EQ-B4")
	var sizes []int
	for _, r := range h.ai.requests() {
		sizes = append(sizes, len(r.Devices))
	}
	if fmt.Sprint(sizes) != "[4 4 2]" {
		t.Errorf("batch sizes = %v, want [4 4 2]", sizes)
	}
}

func TestRunCamaraConcurrencyBound(t *testing.T) {
	h := newHarness()
	h.cfg.CamaraConcurrency = 3
	h.net.locDelay = 5 * time.Millisecond
	for i := 0; i < 30; i++ {
		h.device(i+1, redKm, true)
	}
	h.run(t, "EQ-CONC")
	if h.net.maxInFlight > 3 {
		t.Fatalf("%d concurrent Location calls, want ≤ 3", h.net.maxInFlight)
	}
	if h.net.maxInFlight < 2 {
		t.Errorf("max in-flight %d: triage does not fan out", h.net.maxInFlight)
	}
}

func TestRunPipelineTimeoutIsFatal(t *testing.T) {
	h := newHarness()
	h.cfg.PipelineTimeout = 50 * time.Millisecond
	h.device(1, redKm, true)
	h.ai = &fakeAI{decide: func(ctx context.Context, _ models.AgentRequest) (*models.AgentResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}

	c := h.run(t, "EQ-SLOW")
	errs := h.pub.errors()
	if len(errs) != 1 || errs[0].Code != models.ErrInternalError || !errs[0].Fatal || errs[0].Stage != models.ErrorStagePipeline {
		t.Fatalf("errors = %+v, want one fatal INTERNAL_ERROR (no AGENT_ERROR blamed)", errs)
	}
	if c.Status != models.LifecycleFailed || c.FatalError == nil || !strings.Contains(c.FatalError.Message, "deadline") {
		t.Errorf("event_complete = %+v", c)
	}
}

func TestRunDeviceLogFailureIsReported(t *testing.T) {
	h := newHarness()
	h.store.logErr = errors.New("device_logs: disk full")
	p := h.device(1, redKm, true)
	c := h.run(t, "EQ-LOG")
	errs := h.pub.errors()
	if len(errs) != 1 || errs[0].Code != models.ErrDBError || errs[0].Fatal || errs[0].Phone != p {
		t.Fatalf("errors = %+v", errs)
	}
	if d := h.pub.latestDevices()[p]; d.Stage != models.StageDecided {
		t.Errorf("device = %+v", d)
	}
	if c.DevicesDecided != 1 {
		t.Errorf("event_complete = %+v", c)
	}
}

func types(fs []frame) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.typ
	}
	return out
}
