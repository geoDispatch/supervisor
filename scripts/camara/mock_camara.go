package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// fixtures.json contains the named, human-reviewable development scenarios.
//
//go:embed fixtures.json
var fixturesJSON []byte

var e164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

type fixtureDocument struct {
	Description string             `json:"description"`
	Geographies []fixtureGeography `json:"geographies"`
}

type fixtureGeography struct {
	Name       string             `json:"name"`
	Epicenter  models.Coordinates `json:"epicenter"`
	ObservedAt string             `json:"observed_at"`
	Devices    []fixtureDevice    `json:"devices"`
}

type fixtureDevice struct {
	Phone        string                    `json:"phone"`
	Latitude     float64                   `json:"latitude"`
	Longitude    float64                   `json:"longitude"`
	AccuracyM    float64                   `json:"accuracy_m"`
	Reachability models.ReachabilityStatus `json:"reachability"`
}

type deviceFixture struct {
	location     models.CAMARALocationResponse
	reachability models.CAMARAReachabilityResponse
}

func loadFixtures(raw []byte) (map[string]deviceFixture, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var document fixtureDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode fixtures: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if len(document.Geographies) == 0 {
		return nil, fmt.Errorf("fixtures must contain at least one geography")
	}

	devices := make(map[string]deviceFixture)
	for _, geography := range document.Geographies {
		if strings.TrimSpace(geography.Name) == "" {
			return nil, fmt.Errorf("fixture geography name is required")
		}
		if _, err := time.Parse(time.RFC3339, geography.ObservedAt); err != nil {
			return nil, fmt.Errorf("geography %q observed_at: %w", geography.Name, err)
		}
		for _, device := range geography.Devices {
			if !e164Pattern.MatchString(device.Phone) {
				return nil, fmt.Errorf("geography %q has invalid E.164 phone %q", geography.Name, device.Phone)
			}
			if device.Latitude < -90 || device.Latitude > 90 || device.Longitude < -180 || device.Longitude > 180 {
				return nil, fmt.Errorf("device %s has invalid coordinates", device.Phone)
			}
			if device.AccuracyM <= 0 {
				return nil, fmt.Errorf("device %s accuracy_m must be positive", device.Phone)
			}
			switch device.Reachability {
			case models.ReachableData, models.ReachableSMS, models.NotConnected:
			default:
				return nil, fmt.Errorf("device %s has invalid reachability %q", device.Phone, device.Reachability)
			}
			if _, duplicate := devices[device.Phone]; duplicate {
				return nil, fmt.Errorf("duplicate fixture phone %s", device.Phone)
			}
			devices[device.Phone] = makeFixture(
				device.Latitude,
				device.Longitude,
				device.AccuracyM,
				geography.ObservedAt,
				device.Reachability,
			)
		}
	}
	return devices, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode fixtures: unexpected trailing JSON value")
		}
		return fmt.Errorf("decode fixtures: %w", err)
	}
	return nil
}

func makeFixture(lat, lng, accuracy float64, observedAt string, reachability models.ReachabilityStatus) deviceFixture {
	return deviceFixture{
		location: models.CAMARALocationResponse{
			LastLocationTime: observedAt,
			Area: models.CAMARALocationArea{
				AreaType: "CIRCLE",
				Center:   models.Coordinates{Lat: lat, Lng: lng},
				Radius:   accuracy,
			},
		},
		reachability: models.CAMARAReachabilityResponse{
			LastStatusTime:     observedAt,
			ReachabilityStatus: reachability,
		},
	}
}

// loadAllFixtures is fixtures.json's named scenarios plus the generated demo
// areas (areas.go), which cover every other seeded subscriber.
func loadAllFixtures() (map[string]deviceFixture, error) {
	devices, err := loadFixtures(fixturesJSON)
	if err != nil {
		return nil, err
	}
	addDemoAreas(devices)
	return devices, nil
}

func getPhone(r *http.Request) string {
	if phone := r.URL.Query().Get("phone"); phone != "" {
		return phone
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func lookup(w http.ResponseWriter, r *http.Request, devices map[string]deviceFixture, selectResponse func(deviceFixture) any) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"status": http.StatusMethodNotAllowed, "code": "METHOD_NOT_ALLOWED", "message": "method not allowed",
		})
		return
	}
	phone := getPhone(r)
	if phone == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"status": http.StatusBadRequest, "code": "BAD_REQUEST", "message": "phone is required",
		})
		return
	}
	device, ok := devices[phone]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": http.StatusNotFound, "code": "NOT_FOUND", "message": "device not found",
		})
		return
	}
	writeJSON(w, http.StatusOK, selectResponse(device))
}

func newHandler(devices map[string]deviceFixture) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"status": http.StatusMethodNotAllowed, "code": "METHOD_NOT_ALLOWED", "message": "method not allowed",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "devices": len(devices)})
	})
	location := func(w http.ResponseWriter, r *http.Request) {
		lookup(w, r, devices, func(device deviceFixture) any { return device.location })
	}
	reachability := func(w http.ResponseWriter, r *http.Request) {
		lookup(w, r, devices, func(device deviceFixture) any { return device.reachability })
	}
	mux.HandleFunc("/location", location)
	mux.HandleFunc("/location/", location)
	mux.HandleFunc("/reachability", reachability)
	mux.HandleFunc("/reachability/", reachability)
	mux.HandleFunc("/qos", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, models.CAMARACongestionResponse{
			Level: models.CongestionCritical, Timestamp: "2026-09-13T10:00:00Z",
		})
	})
	mux.HandleFunc("/geofencing", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	})
	return mux
}

// withLatency delays each location and reachability answer by a random
// duration up to max, the way a real network API takes its time, so a demo
// event's devices arrive over a second or two instead of in one frame. Keep
// max well under CAMARA_REACHABILITY_TIMEOUT_MS (1 s by default).
func withLatency(next http.Handler, max time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/location") || strings.HasPrefix(r.URL.Path, "/reachability") {
			time.Sleep(time.Duration(rand.Int63n(int64(max) + 1)))
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
	latency := flag.Duration("latency", msEnv("MOCK_CAMARA_LATENCY_MS"),
		"upper bound of a random delay added to each lookup (default $MOCK_CAMARA_LATENCY_MS, 0 = none)")
	flag.Parse()

	devices, err := loadAllFixtures()
	if err != nil {
		log.Fatalf("load CAMARA fixtures: %v", err)
	}
	handler := newHandler(devices)
	if *latency > 0 {
		handler = withLatency(handler, *latency)
		log.Printf("Mock CAMARA: lookups delayed by up to %s", *latency)
	}
	server := &http.Server{
		Addr:              ":8081",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Mock CAMARA server listening on http://localhost:8081 with %d devices", len(devices))
	log.Fatal(server.ListenAndServe())
}
