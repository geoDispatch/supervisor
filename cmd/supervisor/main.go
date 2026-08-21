package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"sort"

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

// ─────────────────────────────────────────────
//  PER-DEVICE TIMING RECORD
// ─────────────────────────────────────────────
type deviceTiming struct {
	tSensor        time.Time
	tCAMARAStart   time.Time
	tCAMARADone    time.Time
	tAgentSent     time.Time
	tAgentReceived time.Time
	tSMSSent       time.Time
}

type deviceResult struct {
	decision models.DeviceDecision
	distKm   float64
	timing   deviceTiming
}

// ─────────────────────────────────────────────
//  LOGGING HELPERS
// ─────────────────────────────────────────────

func elapsedMS(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

func zoneTag(zone string) string {
	switch zone {
	case "red":
		return "🔴 RED   "
	case "orange":
		return "🟠 ORANGE"
	case "yellow":
		return "🟡 YELLOW"
	case "green":
		return "🟢 GREEN "
	default:
		return "⬜ ???   "
	}
}

func reachTag(status string) string {
	if status == string(models.NotConnected) {
		return "📵 UNREACH"
	}
	return "📶 REACH  "
}

func actionTag(action models.ActionType) string {
	switch action {
	case models.ActionSMS:
		return "💬 SMS    "
	case models.ActionRescue:
		return "🚁 RESCUE "
	case models.ActionBoth:
		return "💬+🚁 BOTH"
	default:
		return "⏭  none  "
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s + strings.Repeat(" ", n-len(s))
	}
	return s[:n-1] + "…"
}

func durStr(a, b time.Time) string {
	d := b.Sub(a)
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// ─────────────────────────────────────────────
//  BATCH TABLE
// ─────────────────────────────────────────────

func printBatchTable(batchIdx int, results []deviceResult, batchDur time.Duration) {
	const w        = 105
	const shelterW = 24

	div := strings.Repeat("═", w)

	// ── TABLE 1: device info ─────────────────────────────────────────────
	fmt.Printf("╔%s╗\n", div)
	fmt.Printf("║  %-*s║\n", w-2,
		fmt.Sprintf("BATCH #%d — %d devices — completed in %s", batchIdx, len(results), batchDur))
	fmt.Printf("╠%s╣\n", div)
	fmt.Printf("║  %-16s %-9s %-9s %-11s %-11s %-5s %-5s %-*s║\n",
		"PHONE", "ZONE", "DIST(km)", "ACTION", "REACH", "PRIO", "CONF",
		shelterW + 6, "SHELTER")
	fmt.Printf("╠%s╣\n", div)

	for _, r := range results {
		prio := "—"
		if r.decision.RescuePriority > 0 {
			prio = fmt.Sprintf("P%d", r.decision.RescuePriority)
		}
		fmt.Printf("║  %-16s %-9s %-7s %-10s %-10s %-5s %-5s %-*s║\n",
			r.decision.Phone,
			zoneTag(string(r.decision.ZoneConfirmed)),
			fmt.Sprintf("%.2f", r.distKm),
			actionTag(r.decision.Action),
			reachTag(""),
			prio,
			fmt.Sprintf("%.0f%%", r.decision.Confidence*100),
			shelterW+6, truncate(r.decision.ShelterName, shelterW),
		)
	}

	// ── TABLE 2: time info ─────────────────────────────────────────────
	fmt.Printf("╚%s╝\n", div)
	fmt.Println()

	const tw   = 90
	tdiv := strings.Repeat("═", tw)

	fmt.Printf("╔%s╗\n", tdiv)
	fmt.Printf("║  %-*s║\n", tw-2, "TIMING BREAKDOWN — /sensor → SMS dispatched")
	fmt.Printf("╠%s╣\n", tdiv)
	fmt.Printf("║  %-16s  %-14s  %-13s  %-15s  %-13s  %-7s║\n",
		"PHONE", "SENSOR→CAMARA", "CAMARA→AGENT", "AGENT→DECISION", "DECISION→SMS", "TOTAL")
	fmt.Printf("╠%s╣\n", tdiv)

	for _, r := range results {
		t := r.timing
		fmt.Printf("║  %-16s  %-14s  %-13s  %-15s  %-13s  %-7s║\n",
			r.decision.Phone,
			durStr(t.tSensor, t.tCAMARAStart),
			durStr(t.tCAMARADone, t.tAgentSent),
			durStr(t.tAgentSent, t.tAgentReceived),
			durStr(t.tAgentReceived, t.tSMSSent),
			durStr(t.tSensor, t.tSMSSent),
		)
	}

	fmt.Printf("╚%s╝\n", tdiv)
	fmt.Println()
}

// ─────────────────────────────────────────────
//  MAIN
// ─────────────────────────────────────────────

func main() {
	t0 := time.Now()
	cfg := config.Load()
	log.Printf("[T+%dms] GeoDispatch supervisor starting...", elapsedMS(t0))

	ctx := context.Background()
	
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()
	
	hub := dashboard.NewHub()
	go hub.Run()
	log.Printf("[T+%dms] websocket hub started", elapsedMS(t0))

	mux := http.NewServeMux()
	mux.HandleFunc("/sensor", func(w http.ResponseWriter, r *http.Request) {
		tEvent := time.Now()
		log.Printf("[T+%dms] POST /sensor received", elapsedMS(t0))
		input, err := sensor.Parse(r)
		if err != nil {
			log.Printf("[T+%dms] sensor parse error: %v", elapsedMS(t0), err)
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

	addr := ":" + cfg.ServerPort
	log.Printf("[T+%dms] GeoDispatch supervisor listening on %s", elapsedMS(t0), addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// ─────────────────────────────────────────────
//  PIPELINE
// ─────────────────────────────────────────────

func runPipeline(
	cfg *config.Config,
	db *database.DB,
	hub *dashboard.Hub,
	input *models.SensorInput,
	tEvent time.Time,
	tBoot time.Time,
) {
	banner := strings.Repeat("━", 68)
	fmt.Printf("\n┏%s┓\n", banner)
	fmt.Printf("┃  🌍 GEODISPATCH EVENT STARTED                                      ┃\n")
	fmt.Printf("┃  Event ID    : %-52s┃\n", input.EventID)
	fmt.Printf("┃  Type        : %-52s┃\n", input.DisasterType)
	fmt.Printf("┃  Severity    : %-52s┃\n", fmt.Sprintf("%.1f", input.Severity))
	fmt.Printf("┃  Epicenter   : %-52s┃\n", fmt.Sprintf("%.4f, %.4f", input.Epicenter.Lat, input.Epicenter.Lng))
	fmt.Printf("┃  Radius      : %-52s┃\n", fmt.Sprintf("%.1f km", input.RadiusKm))
	fmt.Printf("┗%s┛\n\n", banner)

	ctx := context.Background()
	hub.BroadcastEventStart(input)

	var (
		nearestShelters []models.Shelter
		networkStatus   models.NetworkStatus
		congestionLevel models.CongestionLevel
		wgAreaCalls     sync.WaitGroup
	)

	wgAreaCalls.Add(3)

	// A — QoS on Demand
	go func() {
		defer wgAreaCalls.Done()
		status, err := camara.RequestQoS(ctx, cfg, input.Epicenter)
		if err != nil {
			log.Printf("[%s] QoS request failed: %v", models.ErrQoSFailed, err)
			hub.BroadcastError(models.ErrorUpdate{
				Code:    models.ErrQoSFailed,
				Message: err.Error(),
				Fatal:   false,
			}, input.EventID)
			networkStatus = models.NetworkStatus{QoSStatus: models.QoSFailed}
			return
		}
		networkStatus = status
	}()

	// B — nearest shelters from DB
	go func() {
		defer wgAreaCalls.Done()
		shelters, err := db.NearestShelters(ctx, input.Epicenter, 3)
		if err != nil {
			log.Printf("[%s] shelter query failed: %v", models.ErrDBError, err)
			hub.BroadcastError(models.ErrorUpdate{
				Code:    models.ErrDBError,
				Message: fmt.Sprintf("shelter query failed: %v", err),
				Fatal:   true,
			}, input.EventID)
			return
		}
		if len(shelters) == 0 {
			log.Printf("[%s] shelter query returned no results", models.ErrDBError)
		}
		nearestShelters = shelters
	}()

	// C — congestion insights
	go func() {
		defer wgAreaCalls.Done()
		level, err := camara.GetCongestion(ctx, cfg, input.Epicenter)
		if err != nil {
			log.Printf("[%s] congestion query failed: %v", models.ErrCAMARATimeout, err)
			hub.BroadcastError(models.ErrorUpdate{
				Code:    models.ErrCAMARATimeout,
				Message: fmt.Sprintf("congestion insight failed: %v", err),
				Phone:   "",
				Fatal:   false,
			}, input.EventID)
			congestionLevel = models.CongestionUnknown
			return
		}
		congestionLevel = level
	}()

	wgAreaCalls.Wait()
	networkStatus.CongestionLevel = congestionLevel

	// D — fetch phones from DB
	phones, err := db.PhonesNearEpicenter(ctx, input.Epicenter, input.RadiusKm)
	if err != nil {
		log.Printf("[%s] phone lookup failed: %v", models.ErrDBError, err)
		hub.BroadcastError(models.ErrorUpdate{
			Code:    models.ErrDBError,
			Message: fmt.Sprintf("phone lookup failed: %v", err),
			Fatal:   true,
		}, input.EventID)
		return
	}
	if len(phones) == 0 {
		log.Printf("[%s] no phones found near epicenter (radius=%.1fkm)", models.ErrDBError, input.RadiusKm)
		return
	}
	log.Printf("[pipeline] %d phones found within %.1fkm of epicenter", len(phones), input.RadiusKm)

	var (
		allDevices []models.TriagedDevice
		wgDevices  sync.WaitGroup
	)

	timingMap := make(map[string]*deviceTiming)
	distMap   := make(map[string]float64)
	var mu sync.Mutex

	const camaraMaxConcurrent = 50
	sem := make(chan struct{}, camaraMaxConcurrent)

	for _, phone := range phones {
		wgDevices.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wgDevices.Done()
			defer func() { <-sem }()

			tCAMARAStart := time.Now()

			var (
				loc   *models.CAMARALocationResponse
				reach *models.CAMARAReachabilityResponse
				wgDev sync.WaitGroup
			)

			wgDev.Add(2)
			go func() {
				defer wgDev.Done()
				l, err := camara.GetLocation(ctx, cfg, p)
				if err != nil {
					log.Printf("[%s] location lookup failed phone=%s: %v", models.ErrCAMARATimeout, p, err)
					hub.BroadcastError(models.ErrorUpdate{
						Code:    models.ErrCAMARATimeout,
						Message: fmt.Sprintf("location lookup failed: %v", err),
						Phone:   p,
						Fatal:   false,
					}, input.EventID)
					return
				}
				loc = l
			}()
			go func() {
				defer wgDev.Done()
				re, err := camara.GetReachability(ctx, cfg, p)
				if err != nil {
					log.Printf("[%s] reachability lookup failed phone=%s: %v", models.ErrCAMARATimeout, p, err)
					hub.BroadcastError(models.ErrorUpdate{
						Code:    models.ErrCAMARATimeout,
						Message: fmt.Sprintf("reachability lookup failed: %v", err),
						Phone:   p,
						Fatal:   false,
					}, input.EventID)
					return
				}
				reach = re
			}()
			wgDev.Wait()

			tCAMARADone := time.Now()

			if loc == nil || reach == nil {
				return
			}

			distKm := zones.Haversine(loc.Area.Center, input.Epicenter)
			zone := zones.Assign(distKm, input.RadiusKm)

			mu.Lock()
			distMap[p] = distKm
			timingMap[p] = &deviceTiming{
				tSensor:      tEvent,
				tCAMARAStart: tCAMARAStart,
				tCAMARADone:  tCAMARADone,
			}
			allDevices = append(allDevices, models.TriagedDevice{
				Phone:              p,
				Latitude:           loc.Area.Center.Lat,
				Longitude:          loc.Area.Center.Lng,
				LocationRadiusM:    loc.Area.Radius,
				LastLocationTime:   loc.LastLocationTime,
				ReachabilityStatus: reach.ReachabilityStatus,
				LastStatusTime:     reach.LastStatusTime,
				Zone:               zone,
				DistanceKm:         distKm,
			})
			mu.Unlock()

			hub.BroadcastDeviceUpdate(models.DeviceUpdate{
				Phone:     p,
				Latitude:  loc.Area.Center.Lat,
				Longitude: loc.Area.Center.Lng,
				Zone:      zone,
				Reachable: reach.ReachabilityStatus != models.NotConnected,
			}, input.EventID)
		}(phone)
	}

	wgDevices.Wait()
	log.Printf("[pipeline] %d/%d devices triaged successfully", len(allDevices), len(phones))

	sort.Slice(allDevices, func(i, j int) bool {
		return allDevices[i].DistanceKm < allDevices[j].DistanceKm
	})

	const batchSize = 20
	batchCh := make(chan []models.TriagedDevice, 20)
	go func() {
		for i := 0; i < len(allDevices); i += batchSize {
			end := i + batchSize
			if end > len(allDevices) {
				end = len(allDevices)
			}
			batchCh <- allDevices[i:end]
		}
		close(batchCh)
	}()

	agentClient := agent.NewClient(cfg.AgentURL)
	batchIdx := 0
	zoneSummary := models.ZoneSummary{}

	for batch := range batchCh {
		tAgentSent := time.Now()
		req := models.AgentRequest{
			EventID:         input.EventID,
			DisasterType:    input.DisasterType,
			Severity:        input.Severity,
			AftershockRisk:  input.AftershockRisk,
			TsunamiRisk:     input.TsunamiRisk,
			Zone:            batch[0].Zone,
			BatchIndex:      batchIdx,
			Devices:         batch,
			NearestShelters: nearestShelters,
			NetworkStatus:   networkStatus,
		}

		resp, err := agentClient.Decide(ctx, req)
		if err != nil {
			log.Printf("[%s] agent decision failed batch=%d: %v", models.ErrAgentError, batchIdx, err)
			hub.BroadcastError(models.ErrorUpdate{
				Code:    models.ErrAgentError,
				Message: fmt.Sprintf("batch %d: %v", batchIdx, err),
				Phone:   "",
				Fatal:   false,
			}, input.EventID)
			batchIdx++
			continue
		}

		tAgentReceived := time.Now()

		for _, dev := range batch {
			mu.Lock()
			if t, ok := timingMap[dev.Phone]; ok {
				t.tAgentSent = tAgentSent
				t.tAgentReceived = tAgentReceived
			}
			mu.Unlock()
		}

		results := make([]deviceResult, len(resp.Decisions))
		var wgDispatch sync.WaitGroup

		for i, decision := range resp.Decisions {
			wgDispatch.Add(1)
			go func(idx int, d models.DeviceDecision) {
				defer wgDispatch.Done()

				if d.Action == models.ActionSMS || d.Action == models.ActionBoth {
					if err := dispatch.SendSMS(ctx, cfg, d.Phone, d.SMSMessage); err != nil {
						log.Printf("[%s] SMS failed phone=%s: %v", models.ErrSMSFailed, d.Phone, err)
						hub.BroadcastError(models.ErrorUpdate{
							Code:    models.ErrSMSFailed,
							Message: fmt.Sprintf("SMS failed: %v", err),
							Phone:   d.Phone,
							Fatal:   false,
						}, input.EventID)
					}
				}

				tSMSSent := time.Now()

				if d.Action == models.ActionRescue || d.Action == models.ActionBoth {
					if db != nil {
						if err := dispatch.FlagRescue(ctx, db, d, input.EventID); err != nil {
							log.Printf("[%s] rescue flag failed phone=%s: %v", models.ErrDBError, d.Phone, err)
							hub.BroadcastError(models.ErrorUpdate{
								Code:    models.ErrDBError,
								Message: fmt.Sprintf("rescue flag failed: %v", err),
								Phone:   d.Phone,
								Fatal:   false,
							}, input.EventID)
						}
					}
				}

				mu.Lock()
				dist := distMap[d.Phone]
				var timing deviceTiming
				if t, ok := timingMap[d.Phone]; ok {
					t.tSMSSent = tSMSSent
					timing = *t
				}
				updateZoneSummary(&zoneSummary, d)
				mu.Unlock()

				results[idx] = deviceResult{
					decision: d,
					distKm:   dist,
					timing:   timing,
				}

				hub.BroadcastDeviceUpdate(models.DeviceUpdate{
					Phone:      d.Phone,
					Zone:       d.ZoneConfirmed,
					SMSSent:    d.Action == models.ActionSMS || d.Action == models.ActionBoth,
					RescueFlag: d.Action == models.ActionRescue || d.Action == models.ActionBoth,
				}, input.EventID)
			}(i, decision)
		}

		wgDispatch.Wait()
		printBatchTable(batchIdx, results, time.Since(tAgentSent))

		if resp.RequestQoS {
			go func() {
				if err := camara.UpgradeQoS(ctx, cfg, input.Epicenter); err != nil {
					log.Printf("[%s] QoS upgrade failed: %v", models.ErrQoSFailed, err)
					hub.BroadcastError(models.ErrorUpdate{
						Code:    models.ErrQoSFailed,
						Message: fmt.Sprintf("QoS upgrade failed: %v", err),
						Fatal:   false,
					}, input.EventID)
				}
			}()
		}

		hub.BroadcastZoneSummary(zoneSummary, input.EventID)
		hub.BroadcastNarrative(resp.Zone, resp.GovNarrative, input.EventID)
		batchIdx++
	}

	total := time.Since(tEvent)
	banner = strings.Repeat("━", 68)
	fmt.Printf("\n┏%s┓\n", banner)
	fmt.Printf("┃  ✅ PIPELINE COMPLETE                                               ┃\n")
	fmt.Printf("┃  Event ID    : %-52s┃\n", input.EventID)
	fmt.Printf("┃  Total time  : %-52s┃\n", total)
	fmt.Printf("┃  Batches     : %-52s┃\n", fmt.Sprintf("%d", batchIdx))
	fmt.Printf("┃  Zone counts : 🔴 %d  🟠 %d  🟢 %d                                  ┃\n",
		zoneSummary.RedTotal, zoneSummary.OrangeTotal, zoneSummary.GreenTotal)
	fmt.Printf("┗%s┛\n\n", banner)
}

// ─────────────────────────────────────────────
//  ZONE SUMMARY
// ─────────────────────────────────────────────

func updateZoneSummary(s *models.ZoneSummary, d models.DeviceDecision) {
	reachable := d.Action == models.ActionSMS || d.Action == models.ActionBoth
	rescue := d.Action == models.ActionRescue || d.Action == models.ActionBoth
	switch d.ZoneConfirmed {
	case models.ZoneRed:
		s.RedTotal++
		if reachable {
			s.RedReachable++
		}
		if rescue {
			s.RedRescue++
		}
	case models.ZoneOrange:
		s.OrangeTotal++
		if reachable {
			s.OrangeReachable++
		}
	case models.ZoneGreen:
		s.GreenTotal++
		if reachable {
			s.GreenReachable++
		}
	}
}