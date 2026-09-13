package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
)

var phoneLocations = make(map[string]map[string]interface{})
var phoneReachability = make(map[string]map[string]interface{})

func init() {
	// Fixed seed ensures the mock generates the exact same coordinates every time it boots
	rand.Seed(2026) 
	
	epicenterLat := 33.5731
	epicenterLng := -7.5898
	lastTime := "2026-09-13T10:00:00Z"

	for i := 1; i <= 300; i++ {
		// Formats exactly to match DB: +212600000001 through +212600000300
		phone := fmt.Sprintf("+21260000%04d", i)
		
		var lat, lng float64
		var reach string
		angle := rand.Float64() * 2 * math.Pi

		// RED ZONE (1 - 150)
		if i <= 150 {
			radius := math.Sqrt(rand.Float64() * 0.002025)
			lat = epicenterLat + radius*math.Sin(angle)
			lng = epicenterLng + radius*math.Cos(angle)
			
			// 50% unreachable, 30% SMS, 20% Data
			prob := rand.Float64()
			if prob < 0.5 {
				reach = "NOT_CONNECTED"
			} else if prob < 0.8 {
				reach = "CONNECTED_SMS"
			} else {
				reach = "CONNECTED_DATA"
			}

		// ORANGE ZONE (151 - 250)
		} else if i <= 250 {
			radius := math.Sqrt(rand.Float64()*0.006075 + 0.002025)
			lat = epicenterLat + radius*math.Sin(angle)
			lng = epicenterLng + radius*math.Cos(angle)
			
			// 15% unreachable, 60% SMS, 25% Data
			prob := rand.Float64()
			if prob < 0.15 {
				reach = "NOT_CONNECTED"
			} else if prob < 0.75 {
				reach = "CONNECTED_SMS"
			} else {
				reach = "CONNECTED_DATA"
			}

		// GREEN ZONE (251 - 300)
		} else {
			radius := math.Sqrt(rand.Float64()*0.0115 + 0.0081)
			lat = epicenterLat + radius*math.Sin(angle)
			lng = epicenterLng + radius*math.Cos(angle)
			
			// 95% Data, 5% SMS, 0% unreachable
			if rand.Float64() < 0.05 {
				reach = "CONNECTED_SMS"
			} else {
				reach = "CONNECTED_DATA"
			}
		}

		// Populate the maps
		phoneLocations[phone] = map[string]interface{}{
			"lastLocationTime": lastTime,
			"area": map[string]interface{}{
				"areaType": "CIRCLE",
				"center": map[string]float64{"latitude": lat, "longitude": lng},
				"radius":   500.0,
			},
		}
		
		phoneReachability[phone] = map[string]interface{}{
			"lastStatusTime":     lastTime,
			"reachabilityStatus": reach,
		}
	}
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
		"timestamp":  "2026-09-13T10:00:00Z",
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