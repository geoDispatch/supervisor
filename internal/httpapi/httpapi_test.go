package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/origin"
	"github.com/geodispatch/supervisor/internal/pipeline"
)

type fakeIncidents struct {
	mu        sync.Mutex
	result    pipeline.SubmitResult
	submitted []models.SensorInput
	ctxErr    error // Err() of the ctx Submit received
	id        string
	lifecycle models.Lifecycle
	active    bool
}

func (f *fakeIncidents) Submit(ctx context.Context, in *models.SensorInput) pipeline.SubmitResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitted = append(f.submitted, *in)
	f.ctxErr = ctx.Err()
	return f.result
}

func (f *fakeIncidents) Status() (string, models.Lifecycle, bool) {
	return f.id, f.lifecycle, f.active
}

const validSensor = `{"event_id":"EQ-1","disaster_type":"earthquake","timestamp":1757699999000,"severity":6.8,` +
	`"epicenter":{"latitude":33.5731,"longitude":-7.5898},"radius_km":15,"depth_km":10.5,` +
	`"aftershock_risk":"HIGH","tsunami_risk":false}`

func newServer(t *testing.T, inc *fakeIncidents, checks []Check) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	New(Options{
		Incidents:    inc,
		Origins:      origin.NewPolicy(origin.EnvProduction, []string{"https://ops.example.org"}),
		Checks:       checks,
		CheckTimeout: 50 * time.Millisecond,
		WSStats: func() WSStats {
			return WSStats{Clients: 1, FramesPublished: 120, SlowClientDisconnects: 2, MaxQueueDepth: 44}
		},
	}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, contentType, body string, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if resp.Header.Get("Content-Type") == "application/json" && method != http.MethodHead {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("%s %s: body is not JSON: %v", method, path, err)
		}
	}
	return resp, out
}

func TestSensorMapsSubmitResult(t *testing.T) {
	for _, res := range []pipeline.SubmitResult{
		{Status: 202, Body: map[string]any{"status": "accepted", "event_id": "EQ-1", "contract_version": 2}},
		{Status: 200, Body: map[string]any{"status": "duplicate", "event_id": "EQ-1", "lifecycle": "running"}},
		{Status: 409, Body: map[string]any{"error": "pipeline_busy", "active_event_id": "EQ-0"}},
		{Status: 409, Body: map[string]any{"error": "event_id_conflict", "event_id": "EQ-1", "detail": "event_id already used"}},
		{Status: 503, Body: map[string]any{"error": "database_unavailable"}},
	} {
		inc := &fakeIncidents{result: res}
		srv := newServer(t, inc, nil)
		resp, body := do(t, srv, http.MethodPost, "/sensor", "application/json", validSensor, nil)
		if resp.StatusCode != res.Status {
			t.Errorf("status = %d, want %d", resp.StatusCode, res.Status)
		}
		want, _ := json.Marshal(res.Body)
		got, _ := json.Marshal(body)
		if string(got) != string(want) {
			t.Errorf("body = %s, want %s", got, want)
		}
		if len(inc.submitted) != 1 || inc.submitted[0].EventID != "EQ-1" || inc.submitted[0].RadiusKm != 15 {
			t.Errorf("submitted = %+v", inc.submitted)
		}
		if inc.ctxErr != nil {
			t.Errorf("Submit got a cancelled context: %v", inc.ctxErr)
		}
	}
}

func TestSensorMapsProblems(t *testing.T) {
	inc := &fakeIncidents{result: pipeline.SubmitResult{Status: 202, Body: map[string]any{}}}
	srv := newServer(t, inc, nil)

	resp, body := do(t, srv, http.MethodGet, "/sensor", "", "", nil)
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST, OPTIONS" || body["error"] != "method_not_allowed" {
		t.Errorf("GET: %d %v Allow=%q", resp.StatusCode, body, resp.Header.Get("Allow"))
	}
	resp, _ = do(t, srv, http.MethodPost, "/sensor", "text/plain", validSensor, nil)
	if resp.StatusCode != 415 {
		t.Errorf("text/plain: %d", resp.StatusCode)
	}
	resp, body = do(t, srv, http.MethodPost, "/sensor", "application/json", validSensor+validSensor, nil)
	if resp.StatusCode != 400 || body["error"] != "invalid_json" {
		t.Errorf("two objects: %d %v", resp.StatusCode, body)
	}
	flood := strings.Replace(validSensor, "earthquake", "flood", 1)
	resp, body = do(t, srv, http.MethodPost, "/sensor", "application/json; charset=utf-8", flood, nil)
	fields, _ := body["fields"].(map[string]any)
	if resp.StatusCode != 422 || body["error"] != "validation_failed" || fields["disaster_type"] != "unsupported: not implemented by this supervisor" {
		t.Errorf("flood: %d %v", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if len(inc.submitted) != 0 {
		t.Errorf("rejected requests reached the manager: %d", len(inc.submitted))
	}
}

func TestSensorOrigins(t *testing.T) {
	inc := &fakeIncidents{result: pipeline.SubmitResult{Status: 202, Body: map[string]any{"status": "accepted"}}}
	srv := newServer(t, inc, nil)

	resp, body := do(t, srv, http.MethodPost, "/sensor", "application/json", validSensor, map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != 403 || body["error"] != "origin_not_allowed" {
		t.Fatalf("disallowed origin: %d %v", resp.StatusCode, body)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("ACAO set for a disallowed origin")
	}
	if len(inc.submitted) != 0 {
		t.Fatal("a disallowed origin reached the manager")
	}

	resp, _ = do(t, srv, http.MethodPost, "/sensor", "application/json", validSensor, map[string]string{"Origin": "https://ops.example.org"})
	if resp.StatusCode != 202 || resp.Header.Get("Access-Control-Allow-Origin") != "https://ops.example.org" {
		t.Errorf("allowed origin: %d ACAO=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	resp, _ = do(t, srv, http.MethodOptions, "/sensor", "", "", map[string]string{
		"Origin": "https://ops.example.org", "Access-Control-Request-Method": "POST",
	})
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("preflight: %d", resp.StatusCode)
	}
	resp, _ = do(t, srv, http.MethodOptions, "/sensor", "", "", map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != 403 {
		t.Errorf("disallowed preflight: %d", resp.StatusCode)
	}
	// Method is checked before the origin (spec §2.1 order).
	resp, _ = do(t, srv, http.MethodGet, "/sensor", "", "", map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != 405 {
		t.Errorf("GET with a disallowed origin: %d, want 405", resp.StatusCode)
	}
}

func TestHealthReady(t *testing.T) {
	inc := &fakeIncidents{id: "EQ-7", lifecycle: models.LifecycleRunning, active: true}
	srv := newServer(t, inc, []Check{
		{Name: "database", Probe: func(context.Context) error { return nil }},
		{Name: "agent", Probe: func(context.Context) error { return nil }},
		{Name: "camara", Mode: "mock", Probe: func(context.Context) error { return nil }},
	})
	resp, body := do(t, srv, http.MethodGet, "/health", "", "", nil)
	if resp.StatusCode != 200 || body["status"] != "ready" || body["contract_version"] != float64(2) {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	checks := body["checks"].(map[string]any)
	db := checks["database"].(map[string]any)
	if db["ok"] != true || db["latency_ms"] == nil || db["mode"] != nil || db["checked"] != nil {
		t.Errorf("database = %v", db)
	}
	cam := checks["camara"].(map[string]any)
	if cam["ok"] != true || cam["mode"] != "mock" || cam["checked"] != true || cam["latency_ms"] == nil {
		t.Errorf("camara = %v", cam)
	}
	wantPipeline := map[string]any{"event_id": "EQ-7", "lifecycle": "running", "active": true}
	if !reflect.DeepEqual(body["pipeline"], wantPipeline) {
		t.Errorf("pipeline = %v", body["pipeline"])
	}
	wantWS := map[string]any{"clients": float64(1), "frames_published": float64(120), "slow_client_disconnects": float64(2), "max_queue_depth": float64(44)}
	if !reflect.DeepEqual(body["websocket"], wantWS) {
		t.Errorf("websocket = %v", body["websocket"])
	}
}

func TestHealthNotReady(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	inc := &fakeIncidents{lifecycle: models.LifecycleIdle}
	srv := newServer(t, inc, []Check{
		{Name: "database", Probe: func(context.Context) error {
			return errors.New(`dial postgres://geo:s3cret@db:5432/geodispatch failed for +212600000001`)
		}},
		{Name: "agent", Probe: func(context.Context) error { <-block; return nil }}, // ignores its context
		{Name: "camara", Mode: "real"}, // not probed
	})
	began := time.Now()
	resp, body := do(t, srv, http.MethodGet, "/health", "", "", nil)
	if time.Since(began) > 2*time.Second {
		t.Error("a hung probe stalled /health")
	}
	if resp.StatusCode != 503 || body["status"] != "not_ready" {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	checks := body["checks"].(map[string]any)
	db := checks["database"].(map[string]any)
	reason, _ := db["error"].(string)
	if db["ok"] != false || reason == "" || strings.Contains(reason, "s3cret") || strings.Contains(reason, "+212600000001") {
		t.Errorf("database = %v", db)
	}
	agent := checks["agent"].(map[string]any)
	if agent["ok"] != false || !strings.Contains(agent["error"].(string), "timed out") {
		t.Errorf("agent = %v", agent)
	}
	cam := checks["camara"].(map[string]any)
	if !reflect.DeepEqual(cam, map[string]any{"ok": true, "mode": "real", "checked": false}) {
		t.Errorf("camara = %v", cam)
	}
	if p := body["pipeline"].(map[string]any); p["event_id"] != "" || p["lifecycle"] != "idle" || p["active"] != false {
		t.Errorf("pipeline = %v", p)
	}
}

func TestCapabilities(t *testing.T) {
	srv := newServer(t, &fakeIncidents{}, nil)
	resp, body := do(t, srv, http.MethodGet, "/capabilities", "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var want map[string]any
	json.Unmarshal([]byte(`{
	  "contract_version": 2,
	  "disaster_types": { "earthquake": "operational", "flood": "unsupported", "heatwave": "unsupported" },
	  "limits": {
	    "event_id_max_length": 64,
	    "severity": { "min": 0, "max": 10 },
	    "radius_km": { "exclusive_min": 0, "max": 500 },
	    "depth_km": { "min": 0, "max": 800 }
	  },
	  "zone_bands": { "red": 0.33, "orange": 0.66, "green": 1.0 },
	  "single_incident": true
	}`), &want)
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("capabilities = %v\nwant %v", body, want)
	}
	resp, _ = do(t, srv, http.MethodPost, "/capabilities", "application/json", "{}", nil)
	if resp.StatusCode != 405 {
		t.Errorf("POST /capabilities: %d", resp.StatusCode)
	}
}

func TestLivez(t *testing.T) {
	srv := newServer(t, &fakeIncidents{}, nil)
	resp, err := srv.Client().Get(srv.URL + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if resp.StatusCode != 200 || string(buf[:n]) != "ok" {
		t.Fatalf("%d %q", resp.StatusCode, buf[:n])
	}
}
