package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
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

func ms(t time.Time) int64 { return time.Since(t).Milliseconds() }

func since(t time.Time) string { return fmt.Sprintf("%dms", time.Since(t).Milliseconds()) }

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
	log.Printf("[T+%dms] GeoDispatch supervisor starting...", ms(t0))

	var db *database.DB = nil
	log.Printf("[T+%dms] mock database connected (bypassed for testing)", ms(t0))

	hub := dashboard.NewHub()
	go hub.Run()
	log.Printf("[T+%dms] websocket hub started", ms(t0))

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

	addr := ":" + cfg.ServerPort
	log.Printf("[T+%dms] GeoDispatch supervisor listening on %s", ms(t0), addr)
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

	nearestShelters := []models.Shelter{
		{Name: "Casablanca Stadium Shelter", Address: "Stade Mohammed V", Location: models.Coordinates{Lat: 33.5880, Lng: -7.6520}, DistanceKm: 5.2, Capacity: 5000},
		{Name: "Community Center A", Address: "123 Rue de la Paix", Location: models.Coordinates{Lat: 33.5750, Lng: -7.5900}, DistanceKm: 0.5, Capacity: 200},
	}

	var (
		networkStatus models.NetworkStatus
		wgAreaCalls   sync.WaitGroup
	)
	wgAreaCalls.Add(1)
	go func() {
		defer wgAreaCalls.Done()
		var wgA sync.WaitGroup
		wgA.Add(2)
		go func() {
			defer wgA.Done()
			camara.CheckGeofencing(ctx, cfg, input.Epicenter, input.RadiusKm)
		}()
		go func() {
			defer wgA.Done()
			status, err := camara.RequestQoS(ctx, cfg, input.Epicenter)
			if err != nil {
				networkStatus = models.NetworkStatus{QoSStatus: models.QoSFailed}
				return
			}
			networkStatus = status
		}()
		wgA.Wait()
	}()

	phones := []string{
		"+212600000001", "+212600000002", "+212600000003", "+212600000004",
		"+212600000005", "+212600000006", "+212600000007", "+212600000008",
		"+212600000009", "+212600000010", "+212600000011", "+212600000012",
		"+212600000013", "+212600000014", "+212600000015", "+212600000016",
		"+212600000017", "+212600000018", "+212600000019", "+212600000020",
		"+212600000021", "+212600000022", "+212600000023", "+212600000024",
		"+212600000025", "+212600000026", "+212600000027", "+212600000028",
		"+212600000029", "+212600000030", "+212600000031", "+212600000032",
		"+212600000033", "+212600000034", "+212600000035", "+212600000036",
		"+212600000037", "+212600000038", "+212600000039", "+212600000040",
	}

	timingMap := make(map[string]*deviceTiming)
	distMap := make(map[string]float64)
	var mu sync.Mutex // only for map writes from goroutines — not for printing

	heap := zones.NewMinHeap()
	batchCh := make(chan []models.TriagedDevice, 20)
	var wgDevices sync.WaitGroup
	sem := make(chan struct{}, 50)

	go func() {
		const batchSize = 20
		for range heap.Stream() {
			if heap.Len() >= batchSize {
				batch := heap.PopN(batchSize)
				batchCh <- batch
			}
		}
		if heap.Len() > 0 {
			batchCh <- heap.PopAll()
		}
		close(batchCh)
	}()

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
					return
				}
				loc = l
			}()
			go func() {
				defer wgDev.Done()
				re, err := camara.GetReachability(ctx, cfg, p)
				if err != nil {
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
			mu.Unlock()

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

			hub.BroadcastDeviceUpdate(models.DeviceUpdate{
				Phone:     p,
				Latitude:  device.Latitude,
				Longitude: device.Longitude,
				Zone:      zone,
				Reachable: reach.ReachabilityStatus != models.NotConnected,
			}, input.EventID)
		}(phone)
	}

	wgDevices.Wait()
	heap.Close()
	wgAreaCalls.Wait()

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
			log.Printf("[ERROR] AI agent batch %d: %v", batchIdx, err)
			hub.BroadcastError(models.ErrorUpdate{
				Code:    models.ErrAgentError,
				Message: err.Error(),
				Fatal:   false,
			}, input.EventID)
			batchIdx++
			continue
		}

		tAgentReceived := time.Now()

		// stamp agent times on every device in this batch
		for _, dev := range batch {
			mu.Lock()
			if t, ok := timingMap[dev.Phone]; ok {
				t.tAgentSent = tAgentSent
				t.tAgentReceived = tAgentReceived
			}
			mu.Unlock()
		}

		// ── dispatch: SMS + rescue — collect results ──────────────────────
		results := make([]deviceResult, len(resp.Decisions))

		var wgDispatch sync.WaitGroup
		for i, decision := range resp.Decisions {
			wgDispatch.Add(1)
			go func(idx int, d models.DeviceDecision) {
				defer wgDispatch.Done()

				if d.Action == models.ActionSMS || d.Action == models.ActionBoth {
					dispatch.SendSMS(ctx, cfg, d.Phone, d.SMSMessage)
				}

				tSMSSent := time.Now()

				if d.Action == models.ActionRescue || d.Action == models.ActionBoth {
					if db != nil {
						dispatch.FlagRescue(ctx, db, d, input.EventID)
					}
				}

				mu.Lock()
				dist := distMap[d.Phone]
				var timing deviceTiming
				if t, ok := timingMap[d.Phone]; ok {
					t.tSMSSent = tSMSSent
					timing = *t
				}
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

				updateZoneSummary(&zoneSummary, d)
			}(i, decision)
		}
		wgDispatch.Wait()

		// ── print everything sequentially after all goroutines done ───────
		printBatchTable(batchIdx, results, time.Since(tAgentSent))

		if resp.RequestQoS {
			go camara.UpgradeQoS(ctx, cfg, input.Epicenter)
		}

		hub.BroadcastZoneSummary(zoneSummary, input.EventID)
		hub.BroadcastNarrative(resp.Zone, resp.GovNarrative, input.EventID)
		batchIdx++
	}

	// ── pipeline complete banner ──────────────────────────────────────────
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