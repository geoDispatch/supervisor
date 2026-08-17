package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

var phoneLocations = map[string]map[string]interface{}{
	// ── RED zone — within ~3km of epicenter (33.5731, -7.5898) ──────────
	"+212600000001": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5731, "longitude": -7.5898}, "radius": 500.0}}, // 0.00km — epicenter
	"+212600000002": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5740, "longitude": -7.5880}, "radius": 500.0}}, // 0.20km
	"+212600000003": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5720, "longitude": -7.5920}, "radius": 500.0}}, // 0.25km
	"+212600000004": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5750, "longitude": -7.5850}, "radius": 500.0}}, // 0.49km
	"+212600000005": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5710, "longitude": -7.5950}, "radius": 500.0}}, // 0.60km
	"+212600000006": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5760, "longitude": -7.5820}, "radius": 500.0}}, // 0.80km
	"+212600000007": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5800, "longitude": -7.5780}, "radius": 500.0}}, // 1.20km
	"+212600000008": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5700, "longitude": -7.6000}, "radius": 500.0}}, // 1.30km
	"+212600000009": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5820, "longitude": -7.5700}, "radius": 500.0}}, // 1.80km
	"+212600000010": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5680, "longitude": -7.6050}, "radius": 500.0}}, // 1.90km
	"+212600000011": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5800, "longitude": -7.5700}, "radius": 500.0}}, // 2.00km
	"+212600000012": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5660, "longitude": -7.6100}, "radius": 500.0}}, // 2.40km
	"+212600000013": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5840, "longitude": -7.5620}, "radius": 500.0}}, // 2.70km
	"+212600000014": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5640, "longitude": -7.6150}, "radius": 500.0}}, // 2.90km

	// ── ORANGE zone — 3km to 10km ─────────────────────────────────────────
	"+212600000015": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5850, "longitude": -7.5500}, "radius": 500.0}}, // 3.80km
	"+212600000016": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5600, "longitude": -7.6200}, "radius": 500.0}}, // 4.10km
	"+212600000017": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5900, "longitude": -7.5400}, "radius": 500.0}}, // 4.70km
	"+212600000018": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5550, "longitude": -7.6300}, "radius": 500.0}}, // 5.20km
	"+212600000019": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5950, "longitude": -7.5300}, "radius": 500.0}}, // 5.80km
	"+212600000020": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5500, "longitude": -7.6400}, "radius": 500.0}}, // 6.10km
	"+212600000021": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6000, "longitude": -7.5200}, "radius": 500.0}}, // 6.50km
	"+212600000022": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5450, "longitude": -7.6500}, "radius": 500.0}}, // 7.00km
	"+212600000023": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6050, "longitude": -7.5100}, "radius": 500.0}}, // 7.40km
	"+212600000024": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5400, "longitude": -7.6600}, "radius": 500.0}}, // 8.00km
	"+212600000025": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6100, "longitude": -7.5000}, "radius": 500.0}}, // 8.40km
	"+212600000026": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5350, "longitude": -7.6700}, "radius": 500.0}}, // 8.90km
	"+212600000027": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6150, "longitude": -7.4900}, "radius": 500.0}}, // 9.30km
	"+212600000028": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5300, "longitude": -7.6800}, "radius": 500.0}}, // 9.80km

	// ── GREEN zone — beyond 10km ──────────────────────────────────────────
	"+212600000029": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6200, "longitude": -7.4800}, "radius": 500.0}}, // 10.50km
	"+212600000030": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5250, "longitude": -7.6900}, "radius": 500.0}}, // 11.00km
	"+212600000031": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6250, "longitude": -7.4700}, "radius": 500.0}}, // 11.60km
	"+212600000032": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5200, "longitude": -7.7000}, "radius": 500.0}}, // 12.10km
	"+212600000033": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6300, "longitude": -7.4600}, "radius": 500.0}}, // 12.70km
	"+212600000034": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5150, "longitude": -7.7100}, "radius": 500.0}}, // 13.20km
	"+212600000035": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6350, "longitude": -7.4500}, "radius": 500.0}}, // 13.80km
	"+212600000036": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5100, "longitude": -7.7200}, "radius": 500.0}}, // 14.30km
	"+212600000037": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6400, "longitude": -7.4400}, "radius": 500.0}}, // 14.90km
	"+212600000038": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5050, "longitude": -7.7300}, "radius": 500.0}}, // 15.50km
	"+212600000039": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.6450, "longitude": -7.4300}, "radius": 500.0}}, // 16.10km
	"+212600000040": {"lastLocationTime": "2026-08-18T10:00:00Z", "area": map[string]interface{}{"areaType": "CIRCLE", "center": map[string]float64{"latitude": 33.5000, "longitude": -7.7400}, "radius": 500.0}}, // 16.70km
}

var phoneReachability = map[string]map[string]interface{}{
	// RED zone — heavy casualties, mix of unreachable and reachable
	"+212600000001": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // epicenter — likely buried
	"+212600000002": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // very close — no signal
	"+212600000003": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // very close — no signal
	"+212600000004": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},    // barely reachable
	"+212600000005": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // no signal
	"+212600000006": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},    // SMS only
	"+212600000007": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},   // data available
	"+212600000008": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // no signal
	"+212600000009": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},   // data available
	"+212600000010": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // no signal
	"+212600000011": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},    // SMS only
	"+212600000012": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // no signal
	"+212600000013": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},   // data available
	"+212600000014": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},    // SMS only

	// ORANGE zone — network congested but mostly reachable
	"+212600000015": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000016": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // tower damaged
	"+212600000017": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000018": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},
	"+212600000019": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000020": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // tower damaged
	"+212600000021": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000022": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000023": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},
	"+212600000024": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000025": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000026": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "NOT_CONNECTED"},    // tower damaged
	"+212600000027": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000028": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},

	// GREEN zone — network stable, almost all reachable
	"+212600000029": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000030": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000031": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000032": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},
	"+212600000033": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000034": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000035": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000036": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000037": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_SMS"},
	"+212600000038": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000039": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
	"+212600000040": {"lastStatusTime": "2026-08-18T10:00:00Z", "reachabilityStatus": "CONNECTED_DATA"},
}

func getPhone(r *http.Request) string {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) >= 3 {
			phone = parts[2]
		}
	}
	return phone
}

func handleLocation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	phone := getPhone(r)
	if loc, ok := phoneLocations[phone]; ok {
		json.NewEncoder(w).Encode(loc)
	} else {
		json.NewEncoder(w).Encode(phoneLocations["+212600000001"])
	}
}

func handleReachability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	phone := getPhone(r)
	if reach, ok := phoneReachability[phone]; ok {
		json.NewEncoder(w).Encode(reach)
	} else {
		json.NewEncoder(w).Encode(phoneReachability["+212600000001"])
	}
}

func handleQoS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"level":      "CRITICAL",
		"timestamp":  "2026-08-18T10:00:00Z",
		"qos_status": "active",
	})
}

func handleGeofencing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func main() {
	http.HandleFunc("/location", handleLocation)
	http.HandleFunc("/reachability", handleReachability)
	http.HandleFunc("/qos", handleQoS)
	http.HandleFunc("/geofencing", handleGeofencing)

	log.Println("🟢 Mock CAMARA Server running on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}