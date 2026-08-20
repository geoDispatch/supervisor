package main

import (
	"encoding/json"
	"log"
	"net/http"
	"fmt"
)

// Minimal structs to match what the Go Supervisor sends and expects
type DeviceInRequest struct {
	Phone              string `json:"phone"`
	Zone               string `json:"zone"`
	ReachabilityStatus string `json:"reachability_status"`
}

type AgentRequest struct {
	EventID string           `json:"event_id"`
	Zone    string           `json:"zone"`
	Devices []DeviceInRequest `json:"devices"`
}

type DeviceDecision struct {
	Phone          string  `json:"phone"`
	ZoneConfirmed  string  `json:"zone_confirmed"`
	ZoneEscalated  bool    `json:"zone_escalated"`
	Action         string  `json:"action"` // "sms", "rescue_flag", "both", "none"
	SMSMessage     string  `json:"sms_message"`
	ShelterName    string  `json:"shelter_name"`
	RescuePriority int     `json:"rescue_priority"`
	Confidence     float64 `json:"confidence"`
	Reasoning      string  `json:"reasoning"`
}

type AgentResponse struct {
	EventID      string           `json:"event_id"`
	Zone         string           `json:"zone"`
	Decisions    []DeviceDecision `json:"decisions"`
	GovNarrative string           `json:"gov_narrative"`
	RequestQoS   bool             `json:"request_qos"`
	Confidence   float64          `json:"confidence"`
}

// zoneConfig holds all zone-specific decision parameters
type zoneConfig struct {
	action         string
	rescuePriority int
	confidence     float64
	requestQoS     bool
	shelterName    string
	smsTemplate    string
	reasoning      string
	govNarrative   string
}

var zoneProfiles = map[string]zoneConfig{
	"red": {
		action:         "both",
		rescuePriority: 1,
		confidence:     0.99,
		requestQoS:     true,
		shelterName:    "Casablanca Stadium Emergency Shelter",
		smsTemplate:    "🚨 CRITICAL ALERT: You are in a HIGH DANGER ZONE (RED). An earthquake has struck your area. EVACUATE IMMEDIATELY. Do NOT use elevators. Rescue teams are being dispatched to your location. Nearest shelter: %s. Call 112 if trapped.",
		reasoning:      "Device is in RED (critical) zone and reachable. Immediate SMS + rescue flag issued. QoS bandwidth prioritized.",
		govNarrative:   "RED zone batch processed. All reachable devices alerted and flagged for rescue. QoS requested for emergency traffic prioritization.",
	},
	"orange": {
		action:         "both",
		rescuePriority: 2,
		confidence:     0.92,
		requestQoS:     true,
		shelterName:    "Mohamed V Cultural Center Shelter",
		smsTemplate:    "⚠️ URGENT ALERT: You are in a HIGH-RISK ZONE (ORANGE). A significant earthquake has occurred nearby. EVACUATE NOW and move to high ground or open spaces. Avoid damaged structures. Nearest shelter: %s. Stay tuned to emergency broadcasts.",
		reasoning:      "Device is in ORANGE (high-risk) zone and reachable. SMS dispatched with evacuation order. Rescue team flagged at priority 2.",
		govNarrative:   "ORANGE zone batch processed. Evacuation advisories sent. Rescue teams on standby for escalation from red zone.",
	},
	"yellow": {
		action:         "sms",
		rescuePriority: 0,
		confidence:     0.85,
		requestQoS:     false,
		shelterName:    "Ain Diab Community Center",
		smsTemplate:    "📢 CAUTION: You are in a MODERATE RISK ZONE (YELLOW). An earthquake has been detected in your region. Be alert for aftershocks. Inspect your building for damage before re-entering. Nearest precautionary shelter: %s. Follow official guidance.",
		reasoning:      "Device is in YELLOW (moderate) zone and reachable. Precautionary SMS sent. No rescue flag yet — monitoring for escalation.",
		govNarrative:   "YELLOW zone batch processed. Precautionary advisories issued. Situation under active monitoring for potential zone escalation.",
	},
	"green": {
		action:         "sms",
		rescuePriority: 0,
		confidence:     0.75,
		requestQoS:     false,
		shelterName:    "N/A — Low Risk Area",
		smsTemplate:    "ℹ️ INFORMATION: An earthquake has occurred in a nearby region. Your area (GREEN zone) is currently LOW RISK. No immediate action required. Stay calm, stay indoors, and monitor official channels for updates. Emergency line: 112.",
		reasoning:      "Device is in GREEN (low-risk) zone and reachable. Informational SMS sent for situational awareness. No rescue action required.",
		govNarrative:   "GREEN zone batch processed. Informational alerts dispatched. Population advised to monitor channels. No rescue resources allocated.",
	},
}

// fallback for unknown zones
var defaultZoneConfig = zoneConfig{
	action:         "sms",
	rescuePriority: 0,
	confidence:     0.60,
	requestQoS:     false,
	shelterName:    "Nearest Designated Shelter",
	smsTemplate:    "⚠️ ALERT: An earthquake event has been detected. Your zone status is currently UNCLASSIFIED. Please evacuate to open areas and contact emergency services at 112. Nearest shelter: %s.",
	reasoning:      "Device zone is unclassified. Generic safety SMS dispatched as a precaution.",
	govNarrative:   "Unclassified zone batch processed. Generic alerts sent. Zone reclassification pending updated seismic data.",
}

func getZoneConfig(zone string) zoneConfig {
	if cfg, ok := zoneProfiles[zone]; ok {
		return cfg
	}
	return defaultZoneConfig
}

func handleDecide(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Read the request from the Go Supervisor
	var req AgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ Mock AI: Failed to parse request: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	log.Printf("🧠 Mock AI: Received batch for zone=%s with %d devices", req.Zone, len(req.Devices))

	decisions := make([]DeviceDecision, len(req.Devices))
	batchRequestQoS := false

	for i, dev := range req.Devices {
		decision := DeviceDecision{
			Phone:         dev.Phone,
			ZoneConfirmed: dev.Zone,
		}

		if dev.ReachabilityStatus == "NOT_CONNECTED" {
			// Unreachable — log for manual rescue review if in critical zones
			decision.Action = "none"
			decision.Confidence = 0.99
			decision.Reasoning = buildUnreachableReasoning(dev.Zone)

			// Still flag for rescue if in red/orange and unreachable
			if dev.Zone == "red" || dev.Zone == "orange" {
				decision.Action = "rescue_flag"
				decision.RescuePriority = zoneProfiles[dev.Zone].rescuePriority
				decision.Reasoning = fmt.Sprintf(
					"Device in %s zone is UNREACHABLE — cannot send SMS. Automatically flagging for physical rescue dispatch at priority %d.",
					dev.Zone, decision.RescuePriority,
				)
			}
		} else {
			// Reachable — apply zone-specific profile
			cfg := getZoneConfig(dev.Zone)
			decision.Action = cfg.action
			decision.SMSMessage = fmt.Sprintf(cfg.smsTemplate, cfg.shelterName)
			decision.ShelterName = cfg.shelterName
			decision.RescuePriority = cfg.rescuePriority
			decision.Confidence = cfg.confidence
			decision.Reasoning = cfg.reasoning

			// Detect zone escalation (device zone differs from batch zone)
			if dev.Zone != req.Zone {
				decision.ZoneEscalated = true
				decision.Reasoning += fmt.Sprintf(
					" NOTE: Device zone (%s) differs from batch zone (%s) — treated individually.",
					dev.Zone, req.Zone,
				)
			}

			if cfg.requestQoS {
				batchRequestQoS = true
			}
		}

		decisions[i] = decision
		log.Printf("   📱 %s | zone=%-7s | status=%-15s | action=%s",
			dev.Phone, dev.Zone, dev.ReachabilityStatus, decision.Action)
	}

	// Derive batch-level narrative from the dominant zone
	batchCfg := getZoneConfig(req.Zone)

	resp := AgentResponse{
		EventID:      req.EventID,
		Zone:         req.Zone,
		Decisions:    decisions,
		GovNarrative: batchCfg.govNarrative,
		RequestQoS:   batchRequestQoS,
		Confidence:   batchCfg.confidence,
	}

	json.NewEncoder(w).Encode(resp)
	log.Printf("✅ Mock AI: Sent %d decisions back to supervisor (QoS=%v)", len(decisions), batchRequestQoS)
}

func buildUnreachableReasoning(zone string) string {
	switch zone {
	case "red", "orange":
		return fmt.Sprintf("Device in %s zone is completely unreachable via network. Cannot send SMS. No rescue flag issued (non-critical reachability miss — re-evaluate on next cycle).", zone)
	default:
		return "Device is completely unreachable. No action taken. Will retry on next supervisor cycle."
	}
}

func main() {
	http.HandleFunc("/decide", handleDecide)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	port := ":5000"
	log.Printf("🟢 Mock AI Agent Server listening on http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}