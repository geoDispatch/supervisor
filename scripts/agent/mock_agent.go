// Command mock_agent is a DEVELOPMENT TOOL: a rule-based stand-in for the
// Python AI agent, so the supervisor can run end to end without an LLM. It
// handles EARTHQUAKES ONLY and applies fixed rules per Go zone and
// reachability; nothing is "decided" by a model.
//
//	POST /decide  one zone batch (models.AgentRequest) → models.AgentResponse
//	GET  /health  liveness
//
// Replies always pass the supervisor's pipeline.ValidateAgentResponse:
// event_id and zone echoed, one decision per device, zone_confirmed = the
// device's Go zone, never escalated, rescue actions with priority >= 1,
// non-rescue actions with priority 0, SMS actions with a non-empty message.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/geodispatch/supervisor/internal/models"
)

const (
	maxRequestBytes = 1 << 20
	maxBatchDevices = 20  // ai_request.json devices maxItems
	maxShelterRunes = 120 // keeps the SMS well under the 320-char contract limit
)

var e164 = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// zoneRule is the fixed behaviour for one Go zone.
type zoneRule struct {
	reachable      models.ActionType // action when the device is reachable
	unreachable    models.ActionType // action when it is NOT_CONNECTED
	rescuePriority int               // used only by rescue actions (1 = most urgent)
	confidence     float64
	alert          string // first sentence of the SMS
}

var rules = map[models.ZoneType]zoneRule{
	models.ZoneRed: {
		reachable: models.ActionBoth, unreachable: models.ActionRescue, rescuePriority: 1, confidence: 0.99,
		alert: "EARTHQUAKE ALERT (red zone): evacuate now, avoid damaged buildings and lifts.",
	},
	models.ZoneOrange: {
		reachable: models.ActionBoth, unreachable: models.ActionRescue, rescuePriority: 2, confidence: 0.92,
		alert: "EARTHQUAKE WARNING (orange zone): move to open ground away from damaged buildings.",
	},
	models.ZoneGreen: {
		reachable: models.ActionSMS, unreachable: models.ActionNone, rescuePriority: 0, confidence: 0.75,
		alert: "EARTHQUAKE NOTICE (green zone): low risk here. Expect aftershocks and follow official guidance.",
	},
}

// problem is an error reply: {"error": "...", "detail": "..."}.
type problem struct {
	status int
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func fail(status int, msg, detail string) *problem {
	return &problem{status: status, Error: msg, Detail: detail}
}

// decodeRequest reads exactly one AgentRequest: every field present (at
// every level), no unknown field, nothing after the object.
func decodeRequest(body io.Reader) (*models.AgentRequest, *problem) {
	raw, err := io.ReadAll(io.LimitReader(body, maxRequestBytes+1))
	if err != nil {
		return nil, fail(http.StatusBadRequest, "invalid_json", "cannot read body")
	}
	if len(raw) > maxRequestBytes {
		return nil, fail(http.StatusRequestEntityTooLarge, "request_too_large", "")
	}
	if err := requireFields(raw, reflect.TypeOf(models.AgentRequest{}), ""); err != nil {
		return nil, fail(http.StatusBadRequest, "invalid_json", err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req models.AgentRequest
	if err := dec.Decode(&req); err != nil {
		return nil, fail(http.StatusBadRequest, "invalid_json", models.RedactPhones(err.Error()))
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fail(http.StatusBadRequest, "invalid_json", "trailing data after the JSON object")
	}
	return &req, nil
}

// requireFields checks that raw is an object holding every json-tagged
// field of struct type t, recursing into nested structs and slices of
// structs. json.Decoder alone silently zero-fills missing fields.
func requireFields(raw json.RawMessage, t reflect.Type, path string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return fmt.Errorf("%s: must be a JSON object", orRoot(path))
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		v, ok := obj[name]
		if !ok || string(v) == "null" {
			return fmt.Errorf("%s: required", path+"/"+name)
		}
		switch ft := f.Type; {
		case ft.Kind() == reflect.Struct:
			if err := requireFields(v, ft, path+"/"+name); err != nil {
				return err
			}
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			var items []json.RawMessage
			if err := json.Unmarshal(v, &items); err != nil {
				return fmt.Errorf("%s: must be an array", path+"/"+name)
			}
			for j, it := range items {
				if err := requireFields(it, ft.Elem(), fmt.Sprintf("%s/%s/%d", path, name, j)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func orRoot(path string) string {
	if path == "" {
		return "body"
	}
	return path
}

// decide applies the rules to one batch. The request must be an earthquake
// batch whose devices all belong to req.Zone.
func decide(req *models.AgentRequest) (*models.AgentResponse, *problem) {
	if req.DisasterType != models.Earthquake {
		return nil, fail(http.StatusUnprocessableEntity, "unsupported disaster_type for mock agent", "")
	}
	rule, ok := rules[req.Zone]
	if !ok {
		return nil, fail(http.StatusUnprocessableEntity, "invalid zone", "zone must be red, orange or green")
	}
	if n := len(req.Devices); n == 0 || n > maxBatchDevices {
		return nil, fail(http.StatusUnprocessableEntity, "invalid batch size", fmt.Sprintf("devices must hold 1..%d entries", maxBatchDevices))
	}

	seen := make(map[string]bool, len(req.Devices))
	decisions := make([]models.DeviceDecision, 0, len(req.Devices))
	var reachable, sms, rescue int
	for i, d := range req.Devices {
		switch {
		case !e164.MatchString(d.Phone):
			return nil, fail(http.StatusUnprocessableEntity, "invalid device", fmt.Sprintf("devices/%d/phone is not E.164", i))
		case seen[d.Phone]:
			return nil, fail(http.StatusUnprocessableEntity, "invalid device", fmt.Sprintf("devices/%d/phone is duplicated", i))
		case d.Zone != req.Zone:
			// The supervisor never mixes zones in one request.
			return nil, fail(http.StatusUnprocessableEntity, "mixed-zone batch", fmt.Sprintf("devices/%d/zone differs from zone", i))
		}
		seen[d.Phone] = true

		action := rule.unreachable
		switch d.ReachabilityStatus {
		case models.ReachableData, models.ReachableSMS:
			action = rule.reachable
			reachable++
		case models.NotConnected:
		default:
			return nil, fail(http.StatusUnprocessableEntity, "invalid device", fmt.Sprintf("devices/%d/reachability_status is unknown", i))
		}

		dec := models.DeviceDecision{
			Phone:         d.Phone,
			ZoneConfirmed: d.Zone,
			ZoneEscalated: false,
			Action:        action,
			Confidence:    rule.confidence,
			Reasoning:     fmt.Sprintf("mock rule: %s zone, %s, action %s", d.Zone, d.ReachabilityStatus, action),
		}
		if action == models.ActionSMS || action == models.ActionBoth {
			dec.SMSMessage = smsText(rule, req)
			sms++
		}
		if action == models.ActionRescue || action == models.ActionBoth {
			dec.RescuePriority = rule.rescuePriority
			rescue++
		}
		decisions = append(decisions, dec)
	}

	return &models.AgentResponse{
		EventID:   req.EventID,
		Zone:      req.Zone,
		Decisions: decisions,
		// Counts only: no phone, no place name.
		GovNarrative: fmt.Sprintf(
			"Mock agent (development tool), %s zone batch %d: %d device(s), %d reachable, %d unreachable. SMS requested for %d, rescue requested for %d.",
			req.Zone, req.BatchIndex, len(decisions), reachable, len(decisions)-reachable, sms, rescue),
		RequestQoS: req.Zone != models.ZoneGreen && reachable > 0,
		Confidence: rule.confidence,
	}, nil
}

// smsText names the request's nearest shelter when there is one; the mock
// never invents a shelter.
func smsText(rule zoneRule, req *models.AgentRequest) string {
	var b strings.Builder
	b.WriteString(rule.alert)
	if req.TsunamiRisk {
		b.WriteString(" Tsunami risk: move away from the coast.")
	}
	if len(req.NearestShelters) > 0 && strings.TrimSpace(req.NearestShelters[0].Name) != "" {
		b.WriteString(" Nearest shelter: ")
		b.WriteString(truncateRunes(strings.TrimSpace(req.NearestShelters[0].Name), maxShelterRunes))
		b.WriteString(".")
	} else {
		b.WriteString(" Follow local authority instructions.")
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, problem{Error: "method not allowed"})
		return
	}
	req, p := decodeRequest(r.Body)
	if p == nil {
		var resp *models.AgentResponse
		resp, p = decide(req)
		if p == nil {
			log.Printf("decide: event=%s zone=%s batch=%d devices=%d", req.EventID, req.Zone, req.BatchIndex, len(resp.Decisions))
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
	log.Printf("decide rejected (%d): %s %s", p.status, p.Error, p.Detail)
	writeJSON(w, p.status, p)
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/decide", handleDecide)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mock": true, "disaster_types": []string{string(models.Earthquake)}})
	})
	return mux
}

// withDelay holds every /decide answer for d, the way a model takes time to
// think. The supervisor sends one batch at a time, so a demo event's decisions
// then land batch by batch instead of all at once.
func withDelay(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/decide" {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// msEnv reads a millisecond count from the environment; 0 when unset or bad.
func msEnv(key string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Millisecond
}

func main() {
	// 8082, not 5000: macOS binds 5000 for the AirPlay Receiver, so a mock on
	// 5000 either fails to start there or has the supervisor posting to AirPlay.
	addr := flag.String("addr", ":8082", "listen address")
	delay := flag.Duration("delay", msEnv("MOCK_AGENT_DELAY_MS"),
		"time taken per /decide batch (default $MOCK_AGENT_DELAY_MS, 0 = answer at once)")
	flag.Parse()

	handler := newHandler()
	if *delay > 0 {
		handler = withDelay(handler, *delay)
		log.Printf("mock AI agent: each batch takes %s", *delay)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("mock AI agent (DEVELOPMENT TOOL, earthquake rules only) listening on %s", *addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
