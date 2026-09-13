package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/zones"
)

// Limits from the v2 contract.
const (
	maxShelters     = 3
	maxNarrativeLen = 2000 // characters
)

// run is the state of one pipeline execution. Frames are published outside
// mu; mu only guards the counters and the latest state of every device.
type run struct {
	m       *Manager
	in      models.SensorInput
	id      string
	started time.Time

	mu              sync.Mutex
	devicesInRadius int
	devices         map[string]*models.DeviceUpdatePayload // latest state per triaged phone
	triaged         []models.TriagedDevice
	failures        models.FailureCounts
	smsNoGateway    int
	decided         int
	context         models.EventContextPayload // latest event_context
	network         models.NetworkStatus       // sent to the AI with every batch
	qosUpgraded     bool
}

func newRun(m *Manager, in models.SensorInput, started time.Time) *run {
	return &run{
		m:       m,
		in:      in,
		id:      in.EventID,
		started: started,
		devices: make(map[string]*models.DeviceUpdatePayload),
	}
}

// execute runs spec §3.6 steps 2–12 (step 1, event_start, is published by
// Manager.start) and returns the event_complete payload. Every goroutine it
// starts has returned by then, so nothing can be published after it.
func (r *run) execute(ctx context.Context) (complete models.EventCompletePayload) {
	defer func() {
		// A bug must not leave the event without its event_complete, or the
		// manager busy forever.
		if p := recover(); p != nil {
			log.Printf("[pipeline] event %s: panic: %s", r.id, models.RedactPhones(fmt.Sprint(p)))
			complete = r.fail(models.ErrInternalError, models.ErrorStagePipeline, "internal error in the pipeline")
		}
	}()
	epi := r.in.Epicenter

	// 2. Registered devices inside the radius.
	phones, err := r.m.store.PhonesNearEpicenter(ctx, epi, r.in.RadiusKm)
	if ctx.Err() != nil {
		return r.failCtx(ctx)
	}
	if err != nil {
		return r.fail(models.ErrDBError, models.ErrorStageLookup, "device lookup failed: "+redact(err))
	}
	phones = dedupe(phones)
	r.devicesInRadius = len(phones)

	// 3. Shelters: a failure is reported but does not stop the event.
	shelters, sheltersStatus := r.lookupShelters(ctx)
	if ctx.Err() != nil {
		return r.failCtx(ctx)
	}
	r.context = models.EventContextPayload{
		DevicesInRadius: len(phones),
		SheltersStatus:  sheltersStatus,
		Shelters:        shelters,
		NetworkSource:   r.m.net.Source(),
		SMSGateway:      models.SMSGatewayNotConfigured,
	}
	if r.m.sms.Configured() {
		r.context.SMSGateway = models.SMSGatewayConfigured
	}

	// 4. Nobody to reach: no network calls at all.
	if len(phones) == 0 {
		r.context.Network = models.NetworkContext{CongestionLevel: models.CongestionUnknown, QoSStatus: models.QoSInactive}
		r.m.pub.PublishContext(r.id, r.context)
		return r.complete()
	}

	// 5. Area network state, then the first event_context.
	r.network = r.areaNetwork(ctx, phones[0])
	r.context.Network = models.NetworkContext{CongestionLevel: r.network.CongestionLevel, QoSStatus: r.network.QoSStatus}
	r.m.pub.PublishContext(r.id, r.context)
	if ctx.Err() != nil {
		return r.failCtx(ctx)
	}

	// 6–7. Triage, then the first zone_summary.
	r.triage(ctx, phones)
	if ctx.Err() != nil {
		return r.failCtx(ctx)
	}
	r.publishSummary()
	log.Printf("[pipeline] event %s: %d/%d devices triaged", r.id, len(r.triaged), len(phones))

	// 8–11. One zone-pure AI request at a time.
	for i, batch := range PartitionBatches(r.triaged, r.m.cfg.BatchSize) {
		if ctx.Err() != nil || r.decideBatch(ctx, i, batch) {
			return r.failCtx(ctx)
		}
	}
	return r.complete()
}

// lookupShelters returns at most maxShelters shelters and the shelters_status.
func (r *run) lookupShelters(ctx context.Context) ([]models.Shelter, string) {
	shelters, err := r.m.store.NearestShelters(ctx, r.in.Epicenter, maxShelters)
	if err != nil {
		r.report(ctx, models.ErrDBError, models.ErrorStageContext, "", "shelter query failed: "+redact(err))
		return nil, models.SheltersStatusUnavailable
	}
	if len(shelters) > maxShelters {
		shelters = shelters[:maxShelters]
	}
	return shelters, models.SheltersStatusOK
}

// areaNetwork requests QoS and reads congestion concurrently for the area,
// using the first device as the CAMARA anchor. Failures are non-fatal.
func (r *run) areaNetwork(ctx context.Context, anchor string) models.NetworkStatus {
	var (
		wg         sync.WaitGroup
		qos        models.NetworkStatus
		qosErr     error
		congestion models.CongestionLevel
		congErr    error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		qos, qosErr = r.m.net.RequestQoS(ctx, r.in.Epicenter, anchor)
	}()
	go func() {
		defer wg.Done()
		congestion, congErr = r.m.net.Congestion(ctx, r.in.Epicenter, anchor)
	}()
	wg.Wait()

	ns := qos
	if qosErr == nil && !validQoS(ns.QoSStatus) {
		qosErr = fmt.Errorf("unrecognised QoS status %q", ns.QoSStatus)
	}
	if qosErr != nil {
		ns = models.NetworkStatus{QoSStatus: models.QoSFailed}
		r.report(ctx, models.ErrQoSFailed, models.ErrorStageContext, "", "QoS request failed: "+redact(qosErr))
	}
	switch {
	case congErr != nil:
		congestion = models.CongestionUnknown
		r.report(ctx, camaraCode(congErr), models.ErrorStageContext, "", "congestion lookup failed: "+redact(congErr))
	case !validCongestion(congestion):
		congestion = models.CongestionUnknown
	}
	ns.CongestionLevel = congestion
	return ns
}

// triage locates every phone with at most CamaraConcurrency lookups in flight.
func (r *run) triage(ctx context.Context, phones []string) {
	sem := make(chan struct{}, r.m.cfg.CamaraConcurrency)
	var wg sync.WaitGroup
	for _, phone := range phones {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			r.triageOne(ctx, p)
		}(phone)
	}
	wg.Wait()
}

func (r *run) triageOne(ctx context.Context, phone string) {
	var (
		wg       sync.WaitGroup
		loc      *models.CAMARALocationResponse
		locErr   error
		reach    *models.CAMARAReachabilityResponse
		reachErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		loc, locErr = r.m.net.Location(ctx, phone)
	}()
	go func() {
		defer wg.Done()
		rctx, cancel := context.WithTimeout(ctx, r.m.cfg.ReachabilityTimeout)
		defer cancel()
		reach, reachErr = r.m.net.Reachability(rctx, phone)
	}()
	wg.Wait()

	if locErr == nil {
		locErr = checkLocation(loc)
	}
	if locErr != nil {
		r.mu.Lock()
		r.failures.Location++
		r.mu.Unlock()
		log.Printf("[pipeline] event %s: location failed for %s: %s", r.id, models.MaskPhone(phone), redact(locErr))
		r.report(ctx, camaraCode(locErr), models.ErrorStageTriage, phone, "location lookup failed: "+redact(locErr))
		return
	}

	status, assumed := models.NotConnected, true
	if reachErr == nil && reach != nil && validReachability(reach.ReachabilityStatus) {
		status, assumed = reach.ReachabilityStatus, false
	} else {
		if reachErr == nil {
			reachErr = errors.New("unrecognised reachability status")
		}
		r.mu.Lock()
		r.failures.Reachability++
		r.mu.Unlock()
		r.report(ctx, camaraCode(reachErr), models.ErrorStageTriage, phone,
			"reachability lookup failed, NOT_CONNECTED assumed: "+redact(reachErr))
	}

	dist := zones.Haversine(loc.Area.Center, r.in.Epicenter)
	zone := zones.Assign(dist, r.in.RadiusKm)
	dev := models.TriagedDevice{
		Phone:              phone,
		Latitude:           loc.Area.Center.Lat,
		Longitude:          loc.Area.Center.Lng,
		LocationRadiusM:    loc.Area.Radius,
		LastLocationTime:   loc.LastLocationTime,
		ReachabilityStatus: status,
		Zone:               zone,
		DistanceKm:         dist,
	}
	if !assumed {
		dev.LastStatusTime = reach.LastStatusTime
	}
	p := models.DeviceUpdatePayload{
		Phone:               phone,
		Latitude:            dev.Latitude,
		Longitude:           dev.Longitude,
		LocationAccuracyM:   dev.LocationRadiusM,
		Zone:                zone,
		DistanceKm:          dist,
		Reachable:           status != models.NotConnected,
		ReachabilityStatus:  status,
		ReachabilityAssumed: assumed,
		Stage:               models.StageTriaged,
		SMSStatus:           models.SMSStatusNotRequested,
		RescueStatus:        models.RescueStatusNotRequested,
	}
	r.mu.Lock()
	r.triaged = append(r.triaged, dev)
	r.devices[phone] = &p
	r.mu.Unlock()
	r.m.pub.PublishDevice(r.id, p)
}

// decideBatch sends one AI request and applies its answer (steps 9–11). It
// returns true when the pipeline context ended during the request, so the
// caller stops with a fatal error instead of blaming the agent.
func (r *run) decideBatch(ctx context.Context, idx int, batch []models.TriagedDevice) (aborted bool) {
	zone := batch[0].Zone
	req := models.AgentRequest{
		EventID:         r.id,
		DisasterType:    r.in.DisasterType,
		Severity:        r.in.Severity,
		AftershockRisk:  r.in.AftershockRisk,
		TsunamiRisk:     r.in.TsunamiRisk,
		Zone:            zone,
		BatchIndex:      idx,
		Devices:         batch,
		NearestShelters: r.context.Shelters,
		NetworkStatus:   r.network,
	}
	began := time.Now()
	dctx, cancel := context.WithTimeout(ctx, r.m.cfg.AgentTimeout)
	resp, err := r.m.ai.Decide(dctx, req)
	cancel()
	if err != nil && ctx.Err() != nil {
		return true
	}

	code := models.ErrAgentError
	if err == nil {
		if verr := ValidateAgentResponse(req, resp); verr != nil {
			code, err = models.ErrAgentInvalidResponse, verr
		}
	}
	if err != nil {
		what := "agent request failed"
		if code == models.ErrAgentInvalidResponse {
			what = "agent response rejected"
		}
		msg := fmt.Sprintf("batch %d (%s, %d devices): %s: %s", idx, zone, len(batch), what, redact(err))
		log.Printf("[pipeline] event %s: %s", r.id, msg)
		r.report(ctx, code, models.ErrorStageDecision, "", msg)
		r.failBatch(batch)
		r.publishSummary()
		return false
	}

	var wg sync.WaitGroup
	for _, d := range resp.Decisions {
		wg.Add(1)
		go func(d models.DeviceDecision) {
			defer wg.Done()
			r.dispatch(ctx, d)
		}(d)
	}
	wg.Wait()
	r.publishSummary()
	log.Printf("[pipeline] event %s: batch %d (%s) decided %d devices in %s",
		r.id, idx, zone, len(batch), time.Since(began).Round(time.Millisecond))

	if n := narrative(resp.GovNarrative); n != "" {
		r.m.pub.PublishNarrative(r.id, models.NarrativePayload{Zone: zone, Narrative: n, BatchIndex: idx})
	}
	if resp.RequestQoS {
		r.upgradeQoS(ctx)
	}
	return false
}

// failBatch re-publishes every device of a batch the AI could not decide.
func (r *run) failBatch(batch []models.TriagedDevice) {
	for _, d := range batch {
		r.mu.Lock()
		r.failures.Decision++
		p := r.devices[d.Phone]
		p.Stage = models.StageDecisionFailed
		out := *p
		r.mu.Unlock()
		r.m.pub.PublishDevice(r.id, out)
	}
}

// dispatch carries out one validated decision and publishes the device as
// decided. The zone stays Go's; an AI escalation is an annotation only.
func (r *run) dispatch(ctx context.Context, d models.DeviceDecision) {
	smsStatus := models.SMSStatusNotRequested
	if wantsSMS(d.Action) {
		smsStatus = models.SMSStatusNotConfigured
		if r.m.sms.Configured() {
			smsStatus = r.m.sms.Send(ctx, d.Phone, d.SMSMessage)
		}
		switch smsStatus {
		case models.SMSStatusSent:
		case models.SMSStatusNotConfigured:
			r.mu.Lock()
			r.smsNoGateway++
			r.mu.Unlock()
		default: // failed, or a value the messenger should never return
			smsStatus = models.SMSStatusFailed
			r.mu.Lock()
			r.failures.SMS++
			r.mu.Unlock()
			r.report(ctx, models.ErrSMSFailed, models.ErrorStageDispatch, d.Phone, "SMS gateway did not accept the message")
		}
	}

	rescueStatus := models.RescueStatusNotRequested
	if wantsRescue(d.Action) {
		rescueStatus = models.RescueStatusRecorded
		if err := r.m.store.FlagRescue(ctx, r.id, d); err != nil {
			rescueStatus = models.RescueStatusFailed
			r.mu.Lock()
			r.failures.Rescue++
			r.mu.Unlock()
			r.report(ctx, models.ErrDBError, models.ErrorStageDispatch, d.Phone, "rescue flag could not be recorded: "+redact(err))
		}
	}

	// The audit row is not a dispatch outcome, so its failure is reported but
	// not counted in failures.* (the contract has no field for it).
	if err := r.m.store.InsertDeviceLog(ctx, r.id, d); err != nil {
		log.Printf("[pipeline] event %s: device log failed for %s: %s", r.id, models.MaskPhone(d.Phone), redact(err))
		r.report(ctx, models.ErrDBError, models.ErrorStageDispatch, d.Phone, "decision audit log could not be written: "+redact(err))
	}

	action, confidence := d.Action, d.Confidence
	r.mu.Lock()
	r.decided++
	p := r.devices[d.Phone] // ValidateAgentResponse guarantees the phone was triaged
	p.Stage = models.StageDecided
	p.Action = &action
	p.Confidence = &confidence
	p.RescuePriority = d.RescuePriority
	p.ZoneEscalated = d.ZoneEscalated
	p.EscalatedZone = nil
	if d.ZoneEscalated {
		z := d.ZoneConfirmed
		p.EscalatedZone = &z
	}
	p.SMSStatus = smsStatus
	p.SMSSent = smsStatus == models.SMSStatusSent
	p.RescueFlag = wantsRescue(d.Action)
	p.RescueStatus = rescueStatus
	out := *p
	r.mu.Unlock()
	r.m.pub.PublishDevice(r.id, out)
}

// upgradeQoS runs the AI-requested QoS upgrade once per event.
func (r *run) upgradeQoS(ctx context.Context) {
	if r.qosUpgraded {
		return
	}
	if err := r.m.net.UpgradeQoS(ctx, r.in.Epicenter); err != nil {
		r.report(ctx, models.ErrQoSFailed, models.ErrorStageDispatch, "", "QoS upgrade failed: "+redact(err))
		return
	}
	r.qosUpgraded = true
	if r.network.QoSStatus != models.QoSActive {
		r.network.QoSStatus = models.QoSActive
		r.context.Network.QoSStatus = models.QoSActive
		r.m.pub.PublishContext(r.id, r.context)
	}
}

// publishSummary publishes zone_summary from the latest state of every
// triaged device. Reachability comes from CAMARA, never from the AI action.
func (r *run) publishSummary() {
	r.mu.Lock()
	s := models.ZoneSummaryPayload{
		DevicesInRadius: r.devicesInRadius,
		Triaged:         len(r.devices),
		LocationFailed:  r.failures.Location,
	}
	for _, p := range r.devices {
		var z *models.ZoneStats
		switch p.Zone {
		case models.ZoneRed:
			z = &s.Red
		case models.ZoneOrange:
			z = &s.Orange
		default:
			z = &s.Green
		}
		z.Total++
		if p.Reachable {
			z.Reachable++
		} else {
			z.Unreachable++
		}
		switch p.Stage {
		case models.StageDecided:
			z.Decided++
		case models.StageDecisionFailed:
			z.DecisionFailed++
		}
		switch p.SMSStatus {
		case models.SMSStatusSent:
			z.SMSSent++
		case models.SMSStatusFailed:
			z.SMSFailed++
		}
		if p.RescueFlag {
			z.RescueFlagged++
		}
	}
	r.mu.Unlock()
	r.m.pub.PublishSummary(r.id, s)
}

// report publishes a non-fatal error frame. Once the pipeline context has
// ended the run is about to fail with a fatal error that explains it, so
// follow-on errors caused by the cancellation are only logged.
func (r *run) report(ctx context.Context, code models.ErrorCode, stage models.ErrorStage, phone, msg string) {
	msg = models.RedactPhones(msg)
	if ctx.Err() != nil {
		log.Printf("[pipeline] event %s: %s (not published: pipeline stopping): %s", r.id, code, msg)
		return
	}
	r.m.pub.PublishError(r.id, models.ErrorPayload{Code: code, Message: msg, Phone: phone, Stage: stage})
}

// complete builds the event_complete of a run that was not stopped.
func (r *run) complete() models.EventCompletePayload {
	p := r.counts()
	f := p.Failures
	switch {
	case p.DevicesInRadius == 0:
		p.Status = models.LifecycleNoDevices
	case f.Location+f.Reachability+f.Decision+f.SMS+f.Rescue > 0:
		p.Status = models.LifecycleCompletedWithFailures
	default:
		p.Status = models.LifecycleCompleted
	}
	return p
}

// fail publishes the fatal error frame and builds event_complete{failed}.
func (r *run) fail(code models.ErrorCode, stage models.ErrorStage, msg string) models.EventCompletePayload {
	msg = models.RedactPhones(msg)
	log.Printf("[pipeline] event %s: FATAL %s: %s", r.id, code, msg)
	r.m.pub.PublishError(r.id, models.ErrorPayload{Code: code, Message: msg, Fatal: true, Stage: stage})
	p := r.counts()
	p.Status = models.LifecycleFailed
	p.FatalError = &models.FatalError{Code: code, Message: msg}
	return p
}

// failCtx fails the run because its context ended: the pipeline deadline
// passed or the supervisor is shutting down.
func (r *run) failCtx(ctx context.Context) models.EventCompletePayload {
	reason := "pipeline stopped"
	if cause := context.Cause(ctx); cause != nil {
		reason = cause.Error()
	}
	return r.fail(models.ErrInternalError, models.ErrorStagePipeline, reason)
}

func (r *run) counts() models.EventCompletePayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return models.EventCompletePayload{
		DurationMs:          time.Since(r.started).Milliseconds(),
		DevicesInRadius:     r.devicesInRadius,
		DevicesTriaged:      len(r.triaged),
		DevicesDecided:      r.decided,
		Failures:            r.failures,
		SMSNotSentNoGateway: r.smsNoGateway,
	}
}

// ── helpers ───────────────────────────────────────────────────

// redact turns an upstream error into frame-safe text.
func redact(err error) string {
	return models.RedactPhones(err.Error())
}

// camaraCode maps a CAMARA failure to CAMARA_TIMEOUT when a deadline passed,
// else CAMARA_ERROR.
func camaraCode(err error) models.ErrorCode {
	if isTimeout(err) {
		return models.ErrCAMARATimeout
	}
	return models.ErrCAMARAError
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var t interface{ Timeout() bool } // net.Error, *url.Error, *camara.Error
	return errors.As(err, &t) && t.Timeout()
}

// checkLocation rejects a location the zone maths cannot use.
func checkLocation(loc *models.CAMARALocationResponse) error {
	if loc == nil {
		return errors.New("empty location response")
	}
	c, rad := loc.Area.Center, loc.Area.Radius
	if !finite(c.Lat) || !finite(c.Lng) || c.Lat < -90 || c.Lat > 90 || c.Lng < -180 || c.Lng > 180 {
		return errors.New("location response has invalid coordinates")
	}
	if !finite(rad) || rad < 0 {
		return errors.New("location response has an invalid accuracy radius")
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func validReachability(s models.ReachabilityStatus) bool {
	return s == models.ReachableData || s == models.ReachableSMS || s == models.NotConnected
}

func validQoS(s models.QoSStatus) bool {
	switch s {
	case models.QoSInactive, models.QoSRequested, models.QoSActive, models.QoSFailed:
		return true
	}
	return false
}

func validCongestion(c models.CongestionLevel) bool {
	switch c {
	case models.CongestionLow, models.CongestionMedium, models.CongestionHigh, models.CongestionCritical, models.CongestionUnknown:
		return true
	}
	return false
}

// dedupe drops repeated phones, keeping the first occurrence.
func dedupe(phones []string) []string {
	seen := make(map[string]bool, len(phones))
	out := phones[:0:0]
	for _, p := range phones {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// narrative prepares gov_narrative for the dashboard: trimmed, phone numbers
// masked, at most maxNarrativeLen characters. "" means nothing to publish.
func narrative(s string) string {
	s = models.RedactPhones(strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > maxNarrativeLen {
		s = string([]rune(s)[:maxNarrativeLen])
	}
	return s
}
