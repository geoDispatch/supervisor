package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
 
	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/agent"
	"github.com/geodispatch/supervisor/internal/camara"
	"github.com/geodispatch/supervisor/internal/dashboard"
	"github.com/geodispatch/supervisor/internal/database"
	"github.com/geodispatch/supervisor/internal/dispatch"
	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/sensor"
	"github.com/geodispatch/supervisor/internal/zones"
)

func main() {
	t0 := time.Now()

	// Step 1 — Load config from .env
	cfg := config.Load()
	log.Printf("[T+%dms] GeoDispatch supervisor starting...", ms(t0))

	// Step 2 — Connect to PostgreSQL
	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Printf("[T+%dms] database connected", ms(t0))

	// Step 3 — Start WebSocket hub
	hub := dashboard.NewHub()
	go hub.Run()
	log.Printf("[T+%dms] websocket hub started", ms(t0))

	// Step 4 — Register HTTP routes
	mux := http.NewServeMux()
 
	mux.HandleFunc("/sensor", func(w http.ResponseWriter, r *http.Request) {
		tEvent := time.Now()
		log.Printf("[T+%dms] POST /sensor received", ms(t0))
 
		input, err := sensor.Parse(r)
		if err != nil {
			log.Printf("[T+%dms] sensor parse error: %v", ms(t0), err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
 
		w.WriteHeader(http.StatusAccepted)
		go runPipeline(cfg, db, hub, input, tEvent, t0)
	})
 
	mux.HandleFunc("/ws", hub.ServeWS)
 
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Step 5 — Start HTTP server
	addr := ":" + cfg.ServerPort
	log.Printf("[T+%dms] GeoDispatch supervisor listening on %s", ms(t0), addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

/* UTILS */

func ms(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

func runPipeline(
	cfg *config.Config,
	db *database.DB,
	hub *dashboard.Hub,
	input *models.SensorInput,
	tEvent time.Time,
	tBoot time.Time,
) {
	log.Printf("[T+%dms] pipeline start — event_id=%s type=%s severity=%.1f",
		ms(tBoot), input.EventID, input.DisasterType, input.Severity)

	ctx := context.Background()

	// Send the event_start to WS
	hub.BroadcastEventStart(input)
	log.Printf("[T+%dms] dashboard: event_start sent", ms(tBoot))


	// Launch two goRoutines
	// A: geofencing + QoS (area-level, one call each, not per device)
	// B: nearest shelter from PostGIS
	var (
		nearestShelters []models.Shelter
		networkStatus 	models.NetworkStatus
		wgAreaCalls   	sync.WaitGroup
	)
	wgAreaCalls.Add(2)

	// Goroutine A — area-level CAMARA calls
	go func() {
		defer wgAreaCalls.Done()
		tA := time.Now()
 
		// Creating 2 other goRoutines A1 and A2
		var wgA sync.WaitGroup
		wgA.Add(2)
 
		// A1: Geofencing boundary check for the epicenter area
		go func() {
			defer wgA.Done()
			camara.CheckGeofencing(ctx, cfg, input.Epicenter, input.RadiusKm)
			log.Printf("[T+%dms] geofencing done", ms(tBoot))
		}()

		// A2: QoS on Demand — request network priority for disaster area
		go func() {
			defer wgA.Done()
			status, err := camara.RequestQoS(ctx, cfg, input.Epicenter)
			if err != nil {
				log.Printf("[T+%dms] QoS request failed: %v", ms(tBoot), err)
				networkStatus = models.NetworkStatus{QoSStatus: models.QoSFailed}
				return
			}
			networkStatus = status
			log.Printf("[T+%dms] QoS done — status=%s", ms(tBoot), status.QoSStatus)
		}()

		wgA.Wait()
		log.Printf("[T+%dms] goroutine A done in %dms", ms(tBoot), ms(tA))
	}()

	// Goroutine B — DB shelter query
	go func() {
		defer wgAreaCalls.Done()
		tB := time.Now()

		shelters, err := db.NearestShelters(ctx, input.Epicenter, 3)
		if err != nil {
			log.Printf("[T+%dms] shelter query failed: %v", ms(tBoot), err)
			return
		}
		if len(shelters) == 0 {
			log.Printf("[T+%dms] no shelters found near epicenter", ms(tBoot))
			return
		}
		nearestShelters = shelters
		log.Printf("[T+%dms] goroutine B done in %dms — %d shelters found (closest: %s %.1fkm)",
			ms(tBoot), ms(tB), len(shelters), shelters[0].Name, shelters[0].DistanceKm)
	}()

	// ── Per-device CAMARA fetch — fires immediately, doesn't wait for A or B ──
	//
	// Semaphore 50: 200 devices → 4 rounds × ~400ms = ~1.6s
	// Within each device slot: location + reachability fire in parallel.
	// Each resolved device → haversine → pushed into min-heap sorted by distance_km.
	//
	// Min-heap threshold: every 20 devices resolved → pop closest 20 → fire to AI agent.
	// AI batch fires BEFORE all devices are resolved (early fire optimisation).

	phones, err := db.PhonesNearEpicenter(ctx, input.Epicenter, input.RadiusKm)
	if err != nil {
		log.Printf("[T+%dms] phones query failed: %v", ms(tBoot), err)
		return
	}
	log.Printf("[T+%dms] %d phones in affected radius", ms(tBoot), len(phones))
 
	heap := zones.NewMinHeap()
	batchCh := make(chan []models.TriagedDevice, 20) // buffered — AI consumes async
	var wgDevices sync.WaitGroup
	sem := make(chan struct{}, 50) // semaphore: 50 concurrent device slots

	// Heap flusher — runs in its own goroutine, fires AI batches as heap fills
	go func() {
		const batchSize = 20
		for device := range heap.Stream() {
			if heap.Len() >= batchSize {
				batch := heap.PopN(batchSize)
				log.Printf("[T+%dms] batch ready — %d devices, closest=%.2fkm",
					ms(tBoot), len(batch), batch[0].DistanceKm)
				batchCh <- batch
			}
		}

		if heap.Len() > 0 {
			remainder := heap.PopAll()
			log.Printf("[T+%dms] flushing remainder — %d devices", ms(tBoot), len(remainder))
			batchCh <- remainder
		}
		close(batchCh)
	}()

	// Each device (phone) gets its own goRoutine
	for _, phone := range phones {
		wgDevices.Add(1)
		sem <- struct{}{} // acquire slot

		go func(p string) {
			defer wgDevices.Done()
			defer func() { <-sem }() // release slot
 
			tDev := time.Now()

			// GUESS WHAT ? ANOTHER TWO GOROUTINES
			var (
				loc   *models.CAMARALocationResponse
				reach *models.CAMARAReachabilityResponse
				wgDev sync.WaitGroup
			)
			wgDev.Add(2)

			// location goRoutine
			go func() {
				defer wgDev.Done()
				l, err := camara.GetLocation(ctx, cfg, p)
				if err != nil {
					log.Printf("[T+%dms] location failed phone=%s: %v", ms(tBoot), p, err)
					return
				}
				loc = l
			}()

			// reachability goRoutine
			go func() {
				defer wgDev.Done()
				re, err := camara.GetReachability(ctx, cfg, p)
				if err != nil {
					log.Printf("[T+%dms] reachability failed phone=%s: %v", ms(tBoot), p, err)
					return
				}
				reach = re
			}()

			wgDev.Wait()

			if loc == nil || reach == nil {
				log.Printf("[T+%dms] skipping phone=%s (CAMARA partial failure)", ms(tBoot), p)
				return
			}
 
			// Haversine — zone assigned here
			distKm := zones.Haversine(loc.Area.Center, input.Epicenter)
			zone := zones.Assign(distKm, input.RadiusKm)
 
			device := models.TriagedDevice{
				Phone:              p,
				Latitude:           loc.Area.Center.Lat,
				Longitude:          loc.Area.Center.Lng,
				LocationRadiusM:    loc.Area.Radius,
				LastLocationTime:   loc.LastLocationTime,
				ReachabilityStatus: reach.ReachabilityStatus,
				LastStatusTime:     reach.LastStatusTime,
				Zone:               zone,
				DistanceKm:         distKm,
			}
 
			heap.Push(device)
 
			log.Printf("[T+%dms] device resolved phone=%s zone=%s dist=%.2fkm in %dms",
				ms(tBoot), p, zone, distKm, ms(tDev))

			// Stream device dot to dashboard immediately
			hub.BroadcastDeviceUpdate(models.DeviceUpdate{
				Phone:     p,
				Latitude:  device.Latitude,
				Longitude: device.Longitude,
				Zone:      zone,
				Reachable: reach.ReachabilityStatus != models.NotConnected,
			}, input.EventID)
		}(phone)
	}

	// Wait for all device goroutines, then signal heap flusher
	wgDevices.Wait()
	heap.Close()
	log.Printf("[T+%dms] all CAMARA device fetches complete", ms(tBoot))

	// ── Wait for area calls (A + B) before building AI requests 
	wgAreaCalls.Wait()
	log.Printf("[T+%dms] area calls (geofencing + QoS + DB) complete", ms(tBoot))

	// ── AI agent — consume batches sequentially (closest → furthest) ─────────
	//
	// Sequential by batch: finish closest 20 entirely (SMS + rescue + WS) before
	// sending next batch to AI. Guarantees nearest people are handled first.

	agentClient := agent.NewClient(cfg.AgentURL)
	batchIdx := 0
	zoneSummary := models.ZoneSummary{}

	for batch := range batchCh {
		tBatch := time.Now()
		log.Printf("[T+%dms] sending batch %d to AI agent (%d devices)",
			ms(tBoot), batchIdx, len(batch))

		req := models.AgentRequest{
			EventID:        input.EventID,
			DisasterType:   input.DisasterType,
			Severity:       input.Severity,
			AftershockRisk: input.AftershockRisk,
			TsunamiRisk:    input.TsunamiRisk,
			Zone:           batch[0].Zone,
			BatchIndex:     batchIdx,
			Devices:        batch,
			NearestShelter: shelter,
			NetworkStatus:  networkStatus,
		}

		resp, err := agentClient.Decide(ctx, req)
		if err != nil {
			log.Printf("[T+%dms] AI agent error batch %d: %v", ms(tBoot), batchIdx, err)
			
			hub.BroadcastError(models.ErrorUpdate{
				Code:    "AGENT_ERROR",
				Message: err.Error(),
				Fatal:   false,
			}, input.EventID)

			batchIdx++
			continue
		}

		log.Printf("[T+%dms] AI response batch %d received in %dms — confidence=%.2f",
			ms(tBoot), batchIdx, ms(tBatch), resp.Confidence)

		// Receive the batch's decision
		// Apply the decision (sms, rescue, both, none)
		// Modify the device state by ws

}