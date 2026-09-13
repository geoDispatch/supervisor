package sensor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

// validBody returns a fresh valid SensorInput as a generic JSON object so
// cases can delete or replace single members.
func validBody() map[string]any {
	return map[string]any{
		"event_id":        "EQ-20260912-0001",
		"disaster_type":   "earthquake",
		"timestamp":       1757699999000,
		"severity":        6.8,
		"epicenter":       map[string]any{"latitude": 33.5731, "longitude": -7.5898},
		"radius_km":       15,
		"depth_km":        10.5,
		"aftershock_risk": "HIGH",
		"tsunami_risk":    false,
	}
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decode(method, contentType, body string, max int64) (*httptest.ResponseRecorder, *models.SensorInput, *Problem) {
	r := httptest.NewRequest(method, "/sensor", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	in, p := Decode(w, r, max)
	return w, in, p
}

func TestDecodeValid(t *testing.T) {
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		_, in, p := decode(http.MethodPost, ct, encode(t, validBody())+" \n\t", 0)
		if p != nil {
			t.Fatalf("%s: rejected: %d %v", ct, p.Status, p.Body)
		}
		want := models.SensorInput{
			EventID: "EQ-20260912-0001", DisasterType: models.Earthquake, Timestamp: 1757699999000,
			Severity: 6.8, Epicenter: models.Coordinates{Lat: 33.5731, Lng: -7.5898},
			RadiusKm: 15, DepthKm: 10.5, AftershockRisk: models.AftershockHigh, TsunamiRisk: false,
		}
		if *in != want {
			t.Fatalf("%s: got %+v, want %+v", ct, *in, want)
		}
	}
}

func TestDecodeBoundaryValuesAccepted(t *testing.T) {
	cases := map[string]any{
		"severity":  0,
		"radius_km": 500,
		"depth_km":  800,
		"timestamp": 1.757699999e12, // integral value written as a float
		"event_id":  strings.Repeat("a", 64),
	}
	for field, v := range cases {
		b := validBody()
		b[field] = v
		if _, _, p := decode(http.MethodPost, "application/json", encode(t, b), 0); p != nil {
			t.Errorf("%s=%v rejected: %v", field, v, p.Body)
		}
	}
	b := validBody()
	b["epicenter"] = map[string]any{"latitude": -90, "longitude": 180}
	b["severity"] = 10
	b["depth_km"] = 0
	b["tsunami_risk"] = true
	if _, in, p := decode(http.MethodPost, "application/json", encode(t, b), 0); p != nil || !in.TsunamiRisk {
		t.Errorf("edge values rejected: %v", p)
	}
}

func TestDecodeMethod(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		w, _, p := decode(m, "application/json", encode(t, validBody()), 0)
		if p == nil || p.Status != http.StatusMethodNotAllowed {
			t.Fatalf("%s: got %v, want 405", m, p)
		}
		if got := w.Header().Get("Allow"); got != "POST, OPTIONS" {
			t.Errorf("%s: Allow = %q", m, got)
		}
	}
}

func TestDecodeContentType(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/jsonx", "multipart/form-data; boundary=x", ";;"} {
		_, _, p := decode(http.MethodPost, ct, encode(t, validBody()), 0)
		if p == nil || p.Status != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: got %v, want 415", ct, p)
		}
	}
}

func TestDecodeTooLarge(t *testing.T) {
	body := encode(t, validBody())
	_, _, p := decode(http.MethodPost, "application/json", body, int64(len(body)-1))
	if p == nil || p.Status != http.StatusRequestEntityTooLarge || p.Body["error"] != "payload_too_large" {
		t.Fatalf("got %v, want 413", p)
	}
	if _, _, p := decode(http.MethodPost, "application/json", body, int64(len(body))); p != nil {
		t.Fatalf("body of exactly the limit rejected: %v", p.Body)
	}
	// The default limit applies when none is given.
	big := `{"event_id":"` + strings.Repeat("a", int(DefaultMaxBodyBytes)) + `"}`
	if _, _, p := decode(http.MethodPost, "application/json", big, 0); p == nil || p.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("default limit: got %v, want 413", p)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	valid := encode(t, validBody())
	cases := map[string]string{
		"empty":              "",
		"whitespace only":    "   ",
		"malformed":          `{"event_id": `,
		"trailing garbage":   valid + " x",
		"trailing comma":     valid + ",",
		"two objects":        valid + valid,
		"two objects spaced": valid + "\n" + valid,
		"array":              `[` + valid + `]`,
		"string":             `"hello"`,
		"number":             `42`,
		"null":               `null`,
		"true":               `true`,
		"duplicate key":      `{"event_id":"A","event_id":"B"}`,
		"nested duplicate":   strings.Replace(valid, `"latitude":33.5731`, `"latitude":33.5731,"latitude":1`, 1),
	}
	for name, body := range cases {
		_, _, p := decode(http.MethodPost, "application/json", body, 0)
		if p == nil || p.Status != http.StatusBadRequest || p.Body["error"] != "invalid_json" {
			t.Errorf("%s: got %v, want 400 invalid_json", name, p)
			continue
		}
		if d, _ := p.Body["detail"].(string); d == "" {
			t.Errorf("%s: no detail", name)
		}
	}
}

// fieldsOf asserts a 422 and returns its field map.
func fieldsOf(t *testing.T, p *Problem) map[string]string {
	t.Helper()
	if p == nil || p.Status != http.StatusUnprocessableEntity || p.Body["error"] != "validation_failed" {
		t.Fatalf("got %v, want 422 validation_failed", p)
	}
	return p.Body["fields"].(map[string]string)
}

func TestDecodeMissingFields(t *testing.T) {
	for field := range validBody() {
		b := validBody()
		delete(b, field)
		_, _, p := decode(http.MethodPost, "application/json", encode(t, b), 0)
		f := fieldsOf(t, p)
		if f[field] != ReasonRequired || len(f) != 1 {
			t.Errorf("missing %s: fields = %v", field, f)
		}
	}
	for _, sub := range []string{"latitude", "longitude"} {
		b := validBody()
		delete(b["epicenter"].(map[string]any), sub)
		f := fieldsOf(t, decodeProblem(t, b))
		if f["epicenter."+sub] != ReasonRequired {
			t.Errorf("missing epicenter.%s: fields = %v", sub, f)
		}
	}
	// Every problem is reported at once.
	f := fieldsOf(t, decodeProblem(t, map[string]any{}))
	if len(f) != 9 {
		t.Errorf("empty object: %d fields reported, want 9: %v", len(f), f)
	}
}

func decodeProblem(t *testing.T, body map[string]any) *Problem {
	t.Helper()
	_, _, p := decode(http.MethodPost, "application/json", encode(t, body), 0)
	return p
}

func TestDecodeUnknownFields(t *testing.T) {
	b := validBody()
	b["shelter_name"] = "x"
	b["epicenter"].(map[string]any)["altitude"] = 3
	f := fieldsOf(t, decodeProblem(t, b))
	if f["shelter_name"] != ReasonUnknownField || f["epicenter.altitude"] != ReasonUnknownField || len(f) != 2 {
		t.Fatalf("fields = %v", f)
	}
}

func TestDecodeFieldRules(t *testing.T) {
	cases := []struct {
		name  string
		field string // JSON path reported
		set   func(map[string]any)
	}{
		{"event_id empty", "event_id", func(b map[string]any) { b["event_id"] = "" }},
		{"event_id too long", "event_id", func(b map[string]any) { b["event_id"] = strings.Repeat("a", 65) }},
		{"event_id leading dash", "event_id", func(b map[string]any) { b["event_id"] = "-EQ1" }},
		{"event_id space", "event_id", func(b map[string]any) { b["event_id"] = "EQ 1" }},
		{"event_id slash", "event_id", func(b map[string]any) { b["event_id"] = "EQ/1" }},
		{"event_id number", "event_id", func(b map[string]any) { b["event_id"] = 12 }},
		{"event_id null", "event_id", func(b map[string]any) { b["event_id"] = nil }},
		{"disaster_type unknown", "disaster_type", func(b map[string]any) { b["disaster_type"] = "tornado" }},
		{"disaster_type case", "disaster_type", func(b map[string]any) { b["disaster_type"] = "Earthquake" }},
		{"disaster_type number", "disaster_type", func(b map[string]any) { b["disaster_type"] = 1 }},
		{"timestamp zero", "timestamp", func(b map[string]any) { b["timestamp"] = 0 }},
		{"timestamp negative", "timestamp", func(b map[string]any) { b["timestamp"] = -5 }},
		{"timestamp fraction", "timestamp", func(b map[string]any) { b["timestamp"] = 1.5 }},
		{"timestamp string", "timestamp", func(b map[string]any) { b["timestamp"] = "1757699999000" }},
		{"severity below", "severity", func(b map[string]any) { b["severity"] = -0.1 }},
		{"severity above", "severity", func(b map[string]any) { b["severity"] = 10.01 }},
		{"severity string", "severity", func(b map[string]any) { b["severity"] = "6.8" }},
		{"severity bool", "severity", func(b map[string]any) { b["severity"] = true }},
		{"latitude above", "epicenter.latitude", func(b map[string]any) { b["epicenter"].(map[string]any)["latitude"] = 90.5 }},
		{"latitude below", "epicenter.latitude", func(b map[string]any) { b["epicenter"].(map[string]any)["latitude"] = -91 }},
		{"latitude string", "epicenter.latitude", func(b map[string]any) { b["epicenter"].(map[string]any)["latitude"] = "33" }},
		{"longitude above", "epicenter.longitude", func(b map[string]any) { b["epicenter"].(map[string]any)["longitude"] = 180.1 }},
		{"longitude below", "epicenter.longitude", func(b map[string]any) { b["epicenter"].(map[string]any)["longitude"] = -181 }},
		{"epicenter not object", "epicenter", func(b map[string]any) { b["epicenter"] = []any{33.5, -7.5} }},
		{"epicenter null", "epicenter", func(b map[string]any) { b["epicenter"] = nil }},
		{"radius zero", "radius_km", func(b map[string]any) { b["radius_km"] = 0 }},
		{"radius negative", "radius_km", func(b map[string]any) { b["radius_km"] = -1 }},
		{"radius above", "radius_km", func(b map[string]any) { b["radius_km"] = 500.001 }},
		{"depth negative", "depth_km", func(b map[string]any) { b["depth_km"] = -0.5 }},
		{"depth above", "depth_km", func(b map[string]any) { b["depth_km"] = 801 }},
		{"aftershock unknown", "aftershock_risk", func(b map[string]any) { b["aftershock_risk"] = "EXTREME" }},
		{"aftershock lowercase", "aftershock_risk", func(b map[string]any) { b["aftershock_risk"] = "high" }},
		{"tsunami string", "tsunami_risk", func(b map[string]any) { b["tsunami_risk"] = "false" }},
		{"tsunami number", "tsunami_risk", func(b map[string]any) { b["tsunami_risk"] = 0 }},
		{"tsunami null", "tsunami_risk", func(b map[string]any) { b["tsunami_risk"] = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := validBody()
			tc.set(b)
			f := fieldsOf(t, decodeProblem(t, b))
			if f[tc.field] == "" || len(f) != 1 {
				t.Errorf("fields = %v, want only %s", f, tc.field)
			}
		})
	}
}

func TestDecodeNonFiniteNumbers(t *testing.T) {
	// JSON has no NaN/Infinity literals; an overflowing literal is the only
	// way to reach a non-finite float.
	body := strings.Replace(encode(t, validBody()), `"severity":6.8`, `"severity":1e999`, 1)
	f := fieldsOf(t, func() *Problem { _, _, p := decode(http.MethodPost, "application/json", body, 0); return p }())
	if f["severity"] != "must be a finite number" {
		t.Fatalf("fields = %v", f)
	}
}

func TestDecodeUnsupportedDisasterTypes(t *testing.T) {
	for _, typ := range []string{"flood", "heatwave"} {
		b := validBody()
		b["disaster_type"] = typ
		f := fieldsOf(t, decodeProblem(t, b))
		if f["disaster_type"] != ReasonUnsupported {
			t.Errorf("%s: fields = %v, want %q", typ, f, ReasonUnsupported)
		}
	}
	b := validBody()
	b["disaster_type"] = "tornado"
	if f := fieldsOf(t, decodeProblem(t, b)); f["disaster_type"] == ReasonUnsupported {
		t.Error("an unknown type is reported as unsupported instead of invalid")
	}
}
