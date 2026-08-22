package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ── flags ─────────────────────────────────────────────────────────────────────

var (
	host     = flag.String("host", "http://localhost:8080", "supervisor base URL")
	eventID  = flag.String("event", "evt-test-001", "event_id to send")
	severity = flag.Float64("severity", 6.2, "earthquake magnitude (Richter)")
)

// ── payload ───────────────────────────────────────────────────────────────────

type coordinates struct {
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

type sensorPayload struct {
	EventID        string      `json:"event_id"`
	DisasterType   string      `json:"disaster_type"`
	Timestamp      int64       `json:"timestamp"`
	Severity       float64     `json:"severity"`
	Epicenter      coordinates `json:"epicenter"`
	RadiusKm       float64     `json:"radius_km"`
	DepthKm        float64     `json:"depth_km"`
	AftershockRisk string      `json:"aftershock_risk"`
	TsunamiRisk    bool        `json:"tsunami_risk"`
}

func main() {
	flag.Parse()

	payload := sensorPayload{
		EventID:      *eventID,
		DisasterType: "earthquake",
		Timestamp:    time.Now().UnixMilli(),
		Severity:     *severity,
		Epicenter: coordinates{
			Lat: 33.5731,
			Lng: -7.5898,
		},
		RadiusKm:       15.0,
		DepthKm:        10.5,
		AftershockRisk: "MEDIUM",
		TsunamiRisk:    false,
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}

	url := *host + "/sensor"

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  🌍 GeoDispatch — simulate disaster\n")
	fmt.Printf("  Target  : %s\n", url)
	fmt.Printf("  Event   : %s\n", payload.EventID)
	fmt.Printf("  Type    : %s\n", payload.DisasterType)
	fmt.Printf("  Severity: %.1f\n", payload.Severity)
	fmt.Printf("  Epicenter: %.4f, %.4f\n", payload.Epicenter.Lat, payload.Epicenter.Lng)
	fmt.Printf("  Radius  : %.1f km\n", payload.RadiusKm)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	t0 := time.Now()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		log.Fatalf("POST failed: %v\n\nIs the supervisor running on %s?", err, *host)
	}
	defer resp.Body.Close()

	elapsed := time.Since(t0)
	respBody, _ := io.ReadAll(resp.Body)

	fmt.Printf("  HTTP %d  (%s)\n", resp.StatusCode, elapsed)
	if len(bytes.TrimSpace(respBody)) > 0 {
		fmt.Printf("  Body: %s\n", respBody)
	}
	fmt.Println()

	if resp.StatusCode == http.StatusAccepted {
		fmt.Println("  ✅ Event accepted — pipeline is running.")
		fmt.Println("     Watch supervisor logs:  docker compose -f docker-compose.dev.yml logs -f supervisor")
	} else {
		fmt.Printf("  ❌ Unexpected status %d\n", resp.StatusCode)
	}
	fmt.Println()
}