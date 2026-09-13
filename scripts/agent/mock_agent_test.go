package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard) // the handler logs every batch
	os.Exit(m.Run())
}

// validate re-implements the spec §3.6 ValidateAgentResponse rules locally
// (the pipeline package is not imported by development tools).
func validate(req models.AgentRequest, resp *models.AgentResponse) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	if resp.EventID != req.EventID {
		return fmt.Errorf("event_id %q != %q", resp.EventID, req.EventID)
	}
	if resp.Zone != req.Zone {
		return fmt.Errorf("zone %q != %q", resp.Zone, req.Zone)
	}
	if len(resp.Decisions) != len(req.Devices) {
		return fmt.Errorf("%d decisions for %d devices", len(resp.Decisions), len(req.Devices))
	}
	zoneOf := map[string]models.ZoneType{}
	for _, d := range req.Devices {
		zoneOf[d.Phone] = d.Zone
	}
	seen := map[string]bool{}
	for _, d := range resp.Decisions {
		devZone, ok := zoneOf[d.Phone]
		switch {
		case !ok:
			return fmt.Errorf("decision for a phone not in the request")
		case seen[d.Phone]:
			return fmt.Errorf("duplicate decision")
		case !models.ValidAction(d.Action):
			return fmt.Errorf("invalid action %q", d.Action)
		case !models.ValidZone(d.ZoneConfirmed):
			return fmt.Errorf("invalid zone_confirmed %q", d.ZoneConfirmed)
		case !d.ZoneEscalated && d.ZoneConfirmed != devZone:
			return fmt.Errorf("zone_confirmed differs without escalation")
		case d.ZoneEscalated && models.ZoneRank(d.ZoneConfirmed) <= models.ZoneRank(devZone):
			return fmt.Errorf("escalation not strictly more severe")
		case d.RescuePriority < 0 || d.RescuePriority > 10:
			return fmt.Errorf("rescue_priority %d out of range", d.RescuePriority)
		}
		seen[d.Phone] = true
		rescue := d.Action == models.ActionRescue || d.Action == models.ActionBoth
		sms := d.Action == models.ActionSMS || d.Action == models.ActionBoth
		switch {
		case rescue && d.RescuePriority == 0:
			return fmt.Errorf("rescue action with priority 0")
		case !rescue && d.RescuePriority > 0:
			return fmt.Errorf("non-rescue action with priority > 0")
		case sms && d.SMSMessage == "":
			return fmt.Errorf("sms action with empty message")
		case math.IsNaN(d.Confidence) || math.IsInf(d.Confidence, 0) || d.Confidence < 0 || d.Confidence > 1:
			return fmt.Errorf("confidence %v", d.Confidence)
		}
	}
	for phone := range zoneOf {
		if !seen[phone] {
			return fmt.Errorf("request phone missing from decisions")
		}
	}
	return nil
}

func request(zone models.ZoneType, shelters []models.Shelter, statuses ...models.ReachabilityStatus) models.AgentRequest {
	req := models.AgentRequest{
		EventID: "EQ-TEST-1", DisasterType: models.Earthquake, Severity: 6.8,
		AftershockRisk: models.AftershockHigh, Zone: zone, BatchIndex: 3,
		NearestShelters: shelters,
		NetworkStatus:   models.NetworkStatus{CongestionLevel: models.CongestionHigh, QoSStatus: models.QoSActive},
	}
	for i, st := range statuses {
		req.Devices = append(req.Devices, models.TriagedDevice{
			Phone: fmt.Sprintf("+3671999%04d", 1001+i), Latitude: 47.5, Longitude: 19.04,
			LocationRadiusM: 500, LastLocationTime: "2026-08-18T10:00:00Z",
			ReachabilityStatus: st, LastStatusTime: "2026-08-18T10:00:00Z",
			Zone: zone, DistanceKm: float64(i),
		})
	}
	return req
}

func post(t *testing.T, body []byte) (int, []byte) {
	t.Helper()
	srv := httptest.NewServer(newHandler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/decide", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var budapestShelter = []models.Shelter{{
	Name: "Várkert Bazár (fixture)", Address: "Ybl Miklós tér 6, 1013 Budapest",
	Location: models.Coordinates{Lat: 47.4958, Lng: 19.0414}, DistanceKm: 0.3, Capacity: 1500,
}}

func TestValidEarthquakeBatches(t *testing.T) {
	all := []models.ReachabilityStatus{models.NotConnected, models.ReachableSMS, models.ReachableData, models.NotConnected}
	for _, zone := range []models.ZoneType{models.ZoneRed, models.ZoneOrange, models.ZoneGreen} {
		for _, shelters := range [][]models.Shelter{budapestShelter, {}} {
			t.Run(fmt.Sprintf("%s/shelters=%d", zone, len(shelters)), func(t *testing.T) {
				req := request(zone, shelters, all...)
				status, body := post(t, mustJSON(t, req))
				if status != http.StatusOK {
					t.Fatalf("status %d: %s", status, body)
				}
				// Strict decode: the reply has exactly the contract fields.
				dec := json.NewDecoder(bytes.NewReader(body))
				dec.DisallowUnknownFields()
				var resp models.AgentResponse
				if err := dec.Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if err := validate(req, &resp); err != nil {
					t.Fatalf("ValidateAgentResponse rules: %v", err)
				}
				if strings.Contains(string(body), "shelter_name") || strings.Contains(strings.ToLower(string(body)), "yellow") {
					t.Fatalf("forbidden content in %s", body)
				}
				if strings.Contains(resp.GovNarrative, "+36") || strings.Contains(resp.GovNarrative, "Casablanca") || resp.GovNarrative == "" {
					t.Fatalf("narrative must be counts only: %q", resp.GovNarrative)
				}
				for _, d := range resp.Decisions {
					if d.SMSMessage == "" {
						continue
					}
					if strings.Contains(d.SMSMessage, "Casablanca") {
						t.Fatalf("hard-coded Casablanca shelter: %q", d.SMSMessage)
					}
					if len(shelters) > 0 && !strings.Contains(d.SMSMessage, shelters[0].Name) {
						t.Fatalf("SMS does not name the nearest shelter: %q", d.SMSMessage)
					}
					if len([]rune(d.SMSMessage)) > 320 {
						t.Fatalf("SMS longer than 320 chars")
					}
				}
			})
		}
	}
}

func TestRulesPerZone(t *testing.T) {
	req := request(models.ZoneRed, nil, models.NotConnected, models.ReachableSMS)
	resp, p := decide(&req)
	if p != nil {
		t.Fatal(p.Error)
	}
	if resp.Decisions[0].Action != models.ActionRescue || resp.Decisions[0].RescuePriority != 1 {
		t.Fatalf("unreachable red: %+v", resp.Decisions[0])
	}
	if resp.Decisions[1].Action != models.ActionBoth || !resp.RequestQoS {
		t.Fatalf("reachable red: %+v qos=%v", resp.Decisions[1], resp.RequestQoS)
	}

	green := request(models.ZoneGreen, nil, models.NotConnected, models.ReachableData)
	resp, p = decide(&green)
	if p != nil {
		t.Fatal(p.Error)
	}
	if resp.Decisions[0].Action != models.ActionNone || resp.Decisions[1].Action != models.ActionSMS || resp.RequestQoS {
		t.Fatalf("green: %+v qos=%v", resp.Decisions, resp.RequestQoS)
	}
}

func TestOtherDisasterTypesAre422(t *testing.T) {
	for _, typ := range []models.DisasterType{models.Flood, models.Heatwave} {
		req := request(models.ZoneRed, nil, models.ReachableData)
		req.DisasterType = typ
		status, body := post(t, mustJSON(t, req))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d", typ, status)
		}
		if strings.TrimSpace(string(body)) != `{"error":"unsupported disaster_type for mock agent"}` {
			t.Fatalf("%s: body %s", typ, body)
		}
	}
}

func TestRejectsMalformedRequests(t *testing.T) {
	valid := mustJSON(t, request(models.ZoneRed, nil, models.ReachableData))
	var generic map[string]any
	json.Unmarshal(valid, &generic)

	withField := func(k string, v any) []byte {
		m := map[string]any{}
		for key, val := range generic {
			m[key] = val
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return mustJSON(t, m)
	}

	cases := map[string]struct {
		body   []byte
		status int
	}{
		"unknown field":      {withField("shelter_name", "x"), http.StatusBadRequest},
		"missing field":      {withField("network_status", nil), http.StatusBadRequest},
		"null shelters":      {bytes.Replace(valid, []byte(`"nearest_shelters":[]`), []byte(`"nearest_shelters":null`), 1), http.StatusBadRequest},
		"missing device key": {bytes.Replace(valid, []byte(`"location_radius_m":500,`), nil, 1), http.StatusBadRequest},
		"trailing data":      {append(append([]byte{}, valid...), []byte(`{}`)...), http.StatusBadRequest},
		"not an object":      {[]byte(`[]`), http.StatusBadRequest},
		"yellow zone":        {bytes.ReplaceAll(valid, []byte(`"red"`), []byte(`"yellow"`)), http.StatusUnprocessableEntity},
		"no devices":         {mustJSON(t, request(models.ZoneRed, nil)), http.StatusUnprocessableEntity},
		"mixed zones": {func() []byte {
			r := request(models.ZoneRed, nil, models.ReachableData, models.ReachableData)
			r.Devices[1].Zone = models.ZoneGreen
			return mustJSON(t, r)
		}(), http.StatusUnprocessableEntity},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if status, body := post(t, c.body); status != c.status {
				t.Fatalf("status %d (want %d): %s", status, c.status, body)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(newHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
