package models

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskPhone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"+212600000001", "+212 6** *** 001"}, // Morocco, 3-digit country code
		{"+212612345678", "+212 6** *** 678"},
		{"+36719991001", "+36 7** *** 001"}, // Hungary, 2-digit country code
		{"+14155550123", "+1 4** *** 123"},  // NANP, 1-digit country code
		{"+2126001", "***"},                 // too few national digits
		{"+21", "***"},
		{"", "***"},
		{"212600000001", "***"},      // no leading +
		{"+0212600000001", "***"},    // country code cannot start with 0
		{"+2126000000012345", "***"}, // longer than E.164 allows
		{"+212 600 000 001", "***"},
		{"not a phone", "***"},
	}
	for _, c := range cases {
		if got := MaskPhone(c.in); got != c.want {
			t.Errorf("MaskPhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactPhones(t *testing.T) {
	cases := []struct{ in, want string }{
		{"lookup failed for +212600000001: timeout", "lookup failed for +212 6** *** 001: timeout"},
		{"device +36719991001 not found", "device +36 7** *** 001 not found"},
		{"phone=212600000001", "phone=***"},                     // bare run
		{"ids 12345678 and 123456789012345", "ids *** and ***"}, // 8 and 15 digits
		{"HTTP 404 after 5000ms (batch 12)", "HTTP 404 after 5000ms (batch 12)"},
		{"seven 1234567 stays", "seven 1234567 stays"},
		{"a,+212600000001,+212600000002", "a,+212 6** *** 001,+212 6** *** 002"},
		{"long 12345678901234567890 run", "long *** run"},
		{"", ""},
	}
	for _, c := range cases {
		if got := RedactPhones(c.in); got != c.want {
			t.Errorf("RedactPhones(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestZoneRankAndValidZone(t *testing.T) {
	if !(ZoneRank(ZoneGreen) < ZoneRank(ZoneOrange) && ZoneRank(ZoneOrange) < ZoneRank(ZoneRed)) {
		t.Fatalf("rank order wrong: green=%d orange=%d red=%d",
			ZoneRank(ZoneGreen), ZoneRank(ZoneOrange), ZoneRank(ZoneRed))
	}
	for _, z := range []ZoneType{"", "yellow", "RED"} {
		if ZoneRank(z) != 0 || ValidZone(z) {
			t.Errorf("zone %q should be invalid with rank 0", z)
		}
	}
	for _, z := range []ZoneType{ZoneRed, ZoneOrange, ZoneGreen} {
		if !ValidZone(z) {
			t.Errorf("zone %q should be valid", z)
		}
	}
}

func TestDisasterCapability(t *testing.T) {
	cases := map[DisasterType]string{
		Earthquake: CapabilityOperational,
		Flood:      CapabilityUnsupported,
		Heatwave:   CapabilityUnsupported,
		"tornado":  CapabilityUnsupported,
	}
	for dt, want := range cases {
		if got := DisasterCapability(dt); got != want {
			t.Errorf("DisasterCapability(%q) = %q, want %q", dt, got, want)
		}
	}
}

func TestLifecycleTerminal(t *testing.T) {
	for _, l := range []Lifecycle{LifecycleCompleted, LifecycleCompletedWithFailures, LifecycleNoDevices, LifecycleFailed} {
		if !l.Terminal() {
			t.Errorf("%q should be terminal", l)
		}
	}
	for _, l := range []Lifecycle{LifecycleIdle, LifecycleRunning, ""} {
		if l.Terminal() {
			t.Errorf("%q should not be terminal", l)
		}
	}
}

// decodeStrict mirrors how the contracts test and the dashboard treat frames:
// unknown fields are an error.
func decodeStrict(t *testing.T, data []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("strict decode of %s: %v", data, err)
	}
}

func TestDeviceUpdateNullsRoundTrip(t *testing.T) {
	in := DeviceUpdatePayload{
		Phone:              "+212600000001",
		Latitude:           33.5731,
		Longitude:          -7.5898,
		LocationAccuracyM:  500,
		Zone:               ZoneRed,
		ReachabilityStatus: NotConnected,
		Stage:              StageTriaged,
		SMSStatus:          SMSStatusNotRequested,
		RescueStatus:       RescueStatusNotRequested,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"action":null`, `"confidence":null`, `"escalated_zone":null`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("marshalled triaged frame lacks %s: %s", want, data)
		}
	}
	for _, banned := range []string{"reasoning", "sms_message", "shelter_name"} {
		if strings.Contains(string(data), banned) {
			t.Errorf("device_update must never carry %q: %s", banned, data)
		}
	}

	var out DeviceUpdatePayload
	decodeStrict(t, data, &out)
	if out.Action != nil || out.Confidence != nil || out.EscalatedZone != nil {
		t.Errorf("nulls did not round-trip to nil: %+v", out)
	}
	if out != in {
		t.Errorf("round trip changed the payload:\n in  %+v\n out %+v", in, out)
	}
}

func TestDeviceUpdateDecidedRoundTrip(t *testing.T) {
	action := ActionBoth
	escalated := ZoneRed
	confidence := 0.0 // a real zero must survive, not turn into null
	in := DeviceUpdatePayload{
		Phone: "+212600000004", Zone: ZoneOrange, Stage: StageDecided,
		Action: &action, ZoneEscalated: true, EscalatedZone: &escalated,
		RescuePriority: 2, Confidence: &confidence,
		SMSStatus: SMSStatusNotConfigured, RescueFlag: true, RescueStatus: RescueStatusRecorded,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"confidence":0`) {
		t.Errorf("confidence 0 must marshal as 0: %s", data)
	}
	var out DeviceUpdatePayload
	decodeStrict(t, data, &out)
	if out.Action == nil || *out.Action != ActionBoth ||
		out.EscalatedZone == nil || *out.EscalatedZone != ZoneRed ||
		out.Confidence == nil || *out.Confidence != 0 {
		t.Errorf("pointer fields did not round-trip: %s", data)
	}
}

func TestEventContextNilSheltersMarshalAsArray(t *testing.T) {
	p := EventContextPayload{
		SheltersStatus: SheltersStatusUnavailable,
		Network:        NetworkContext{CongestionLevel: CongestionUnknown, QoSStatus: QoSInactive},
		NetworkSource:  NetworkSourceMockCAMARA,
		SMSGateway:     SMSGatewayNotConfigured,
	}
	for name, v := range map[string]any{"value": p, "pointer": &p} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"shelters":[]`) {
			t.Errorf("%s: nil shelters must marshal as []: %s", name, data)
		}
	}

	// The same must hold when the payload is nested (e.g. marshalled by the hub).
	nested, err := json.Marshal(struct {
		P EventContextPayload `json:"payload"`
	}{p})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nested), `"shelters":[]`) {
		t.Errorf("nested: nil shelters must marshal as []: %s", nested)
	}

	var out EventContextPayload
	data, _ := json.Marshal(p)
	decodeStrict(t, data, &out)
	if out.SheltersStatus != SheltersStatusUnavailable || len(out.Shelters) != 0 {
		t.Errorf("round trip: %+v", out)
	}
}

func TestAgentRequestNilSlicesMarshalAsArrays(t *testing.T) {
	data, err := json.Marshal(AgentRequest{EventID: "EQ-1", Zone: ZoneRed})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"devices":[]`, `"nearest_shelters":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("AgentRequest lacks %s: %s", want, data)
		}
	}
}

func TestEventCompleteFatalErrorNull(t *testing.T) {
	data, err := json.Marshal(EventCompletePayload{Status: LifecycleCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fatal_error":null`) {
		t.Errorf("fatal_error must be null when absent: %s", data)
	}
}

func TestEnvelopeFieldNames(t *testing.T) {
	data, err := json.Marshal(Envelope{V: ContractVersion, Type: TypeHeartbeat, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"v", "type", "event_id", "seq", "timestamp", "replay", "payload"}
	if len(fields) != len(want) {
		t.Fatalf("envelope has %d fields, want %d: %s", len(fields), len(want), data)
	}
	for _, k := range want {
		if _, ok := fields[k]; !ok {
			t.Errorf("envelope lacks %q: %s", k, data)
		}
	}
	if !IsControlType(TypeHeartbeat) || IsControlType(TypeEventStart) {
		t.Error("IsControlType misclassifies frame types")
	}
}
