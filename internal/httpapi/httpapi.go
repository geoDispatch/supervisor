// Package httpapi serves the supervisor's HTTP endpoints (spec §2): POST
// /sensor, GET /health (readiness), GET /livez (liveness) and GET
// /capabilities. /ws belongs to the dashboard hub. Collaborators are small
// interfaces and funcs so main.go stays wiring only and the handlers are
// tested with fakes.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/origin"
	"github.com/geodispatch/supervisor/internal/pipeline"
	"github.com/geodispatch/supervisor/internal/sensor"
	"github.com/geodispatch/supervisor/internal/zones"
)

// DefaultCheckTimeout bounds each readiness probe (spec §2.3).
const DefaultCheckTimeout = 2 * time.Second

// maxReasonBytes caps the error text of a failed readiness check.
const maxReasonBytes = 200

// Incidents is the single-incident manager (*pipeline.Manager).
type Incidents interface {
	Submit(ctx context.Context, in *models.SensorInput) pipeline.SubmitResult
	Status() (eventID string, lifecycle models.Lifecycle, active bool)
}

// Check is one readiness probe of GET /health.
type Check struct {
	Name string
	// Mode, when set, is reported with a "checked" flag (the CAMARA check:
	// "mock" or "real").
	Mode string
	// Probe returns nil when the dependency is ready. A nil Probe means the
	// dependency is deliberately not probed: it is reported ok with
	// "checked": false, never as verified.
	Probe func(ctx context.Context) error
}

// WSStats is the websocket section of GET /health.
type WSStats struct {
	Clients               int
	FramesPublished       int64
	SlowClientDisconnects int64
	MaxQueueDepth         int
}

// Options configures the API.
type Options struct {
	Incidents    Incidents
	Origins      origin.Policy // /sensor refuses browser origins it does not allow
	MaxBodyBytes int64         // SENSOR_MAX_BODY_BYTES; ≤ 0 means sensor.DefaultMaxBodyBytes
	Checks       []Check
	WSStats      func() WSStats // nil reports zeros
	CheckTimeout time.Duration  // ≤ 0 means DefaultCheckTimeout
}

// API holds the handlers. Build it with New.
type API struct {
	opts         Options
	capabilities []byte
}

// New builds the API.
func New(opts Options) *API {
	if opts.CheckTimeout <= 0 {
		opts.CheckTimeout = DefaultCheckTimeout
	}
	caps, err := json.Marshal(capabilitiesBody())
	if err != nil { // static data: cannot fail
		panic(err)
	}
	return &API{opts: opts, capabilities: caps}
}

// Register adds /sensor, /health, /capabilities (behind the CORS middleware)
// and /livez to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.Handle("/sensor", a.opts.Origins.CORS(http.HandlerFunc(a.sensor)))
	mux.Handle("/health", a.opts.Origins.CORS(http.HandlerFunc(a.health)))
	mux.Handle("/capabilities", a.opts.Origins.CORS(http.HandlerFunc(a.capabilitiesHandler)))
	// Liveness is for orchestrators, not browsers: no CORS.
	mux.HandleFunc("/livez", livez)
}

// ── POST /sensor ──────────────────────────────────────────────

func (a *API) sensor(w http.ResponseWriter, r *http.Request) {
	// Step 2 of spec §2.1 sits between the method check and the body checks
	// that sensor.Decode performs. The CORS middleware lets disallowed
	// non-preflight requests through, so the refusal happens here.
	if r.Method == http.MethodPost && !a.opts.Origins.Allowed(r) {
		log.Printf("[http] POST /sensor refused: origin %q not allowed", r.Header.Get("Origin"))
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin_not_allowed"})
		return
	}
	in, problem := sensor.Decode(w, r, a.opts.MaxBodyBytes)
	if problem != nil {
		log.Printf("[http] %s /sensor rejected: %d %v", r.Method, problem.Status, problem.Body["error"])
		writeJSON(w, problem.Status, problem.Body)
		return
	}
	// The events-table insert must not be cut short by the client hanging
	// up: a committed row with no running pipeline would make the id
	// unusable. The manager bounds the call with its own timeout.
	res := a.opts.Incidents.Submit(context.WithoutCancel(r.Context()), in)
	log.Printf("[http] POST /sensor event %s: %d %v", in.EventID, res.Status, firstOf(res.Body, "status", "error"))
	writeJSON(w, res.Status, res.Body)
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// ── GET /capabilities ─────────────────────────────────────────

func capabilitiesBody() map[string]any {
	return map[string]any{
		"contract_version": models.ContractVersion,
		"disaster_types":   models.SupportedDisasterTypes,
		"limits": map[string]any{
			"event_id_max_length": sensor.EventIDMaxLength,
			"severity":            map[string]any{"min": sensor.SeverityMin, "max": sensor.SeverityMax},
			"radius_km":           map[string]any{"exclusive_min": 0, "max": sensor.RadiusKmMax},
			"depth_km":            map[string]any{"min": sensor.DepthKmMin, "max": sensor.DepthKmMax},
		},
		"zone_bands":      zones.Bands(),
		"single_incident": true,
	}
}

func (a *API) capabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if !readOnly(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(a.capabilities)
}

// ── GET /livez ────────────────────────────────────────────────

func livez(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// ── GET /health ───────────────────────────────────────────────

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if !readOnly(w, r) {
		return
	}
	checks, ready := a.runChecks(r.Context())

	id, lifecycle, active := a.opts.Incidents.Status()
	var ws WSStats
	if a.opts.WSStats != nil {
		ws = a.opts.WSStats()
	}
	status, code := "ready", http.StatusOK
	if !ready {
		status, code = "not_ready", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":           status,
		"contract_version": models.ContractVersion,
		"checks":           checks,
		"pipeline":         map[string]any{"event_id": id, "lifecycle": lifecycle, "active": active},
		"websocket": map[string]any{
			"clients":                 ws.Clients,
			"frames_published":        ws.FramesPublished,
			"slow_client_disconnects": ws.SlowClientDisconnects,
			"max_queue_depth":         ws.MaxQueueDepth,
		},
	})
}

// runChecks runs every probe concurrently, each with CheckTimeout, and
// reports whether all of them are ok.
func (a *API) runChecks(ctx context.Context) (map[string]map[string]any, bool) {
	results := make(map[string]map[string]any, len(a.opts.Checks))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, c := range a.opts.Checks {
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()
			res := a.runCheck(ctx, c)
			mu.Lock()
			results[c.Name] = res
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	ready := true
	for _, res := range results {
		if ok, _ := res["ok"].(bool); !ok {
			ready = false
		}
	}
	return results, ready
}

func (a *API) runCheck(ctx context.Context, c Check) map[string]any {
	res := map[string]any{}
	if c.Mode != "" {
		res["mode"] = c.Mode
		res["checked"] = c.Probe != nil
	}
	if c.Probe == nil {
		res["ok"] = true
		return res
	}

	cctx, cancel := context.WithTimeout(ctx, a.opts.CheckTimeout)
	defer cancel()
	began := time.Now()
	done := make(chan error, 1)
	go func() { done <- c.Probe(cctx) }()
	var err error
	select {
	case err = <-done:
	case <-cctx.Done(): // a probe that ignores its context still cannot stall /health
		err = cctx.Err()
	}
	res["latency_ms"] = time.Since(began).Milliseconds()
	res["ok"] = err == nil
	if err != nil {
		res["error"] = a.reason(err)
	}
	return res
}

var userinfo = regexp.MustCompile(`://[^/@\s]*@`)

// reason turns a probe error into a short string without secrets or phones.
func (a *API) reason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out after " + a.opts.CheckTimeout.String()
	}
	s := userinfo.ReplaceAllString(err.Error(), "://***@")
	s = models.RedactPhones(s)
	if len(s) > maxReasonBytes {
		cut := maxReasonBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// ── helpers ───────────────────────────────────────────────────

// readOnly allows GET and HEAD, answering anything else with 405.
func readOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD, OPTIONS")
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
	return false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("[http] writing a %d reply failed: %v", status, err)
	}
}
