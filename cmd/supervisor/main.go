// Command supervisor is the GeoDispatch supervisor: POST /sensor starts one
// disaster pipeline at a time and GET /ws streams its v2 frames to the
// dashboard. This file only wires the pieces together; the pipeline lives in
// internal/pipeline and the HTTP handlers in internal/httpapi.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/agent"
	"github.com/geodispatch/supervisor/internal/camara"
	"github.com/geodispatch/supervisor/internal/dashboard"
	"github.com/geodispatch/supervisor/internal/database"
	"github.com/geodispatch/supervisor/internal/dispatch"
	"github.com/geodispatch/supervisor/internal/httpapi"
	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/origin"
	"github.com/geodispatch/supervisor/internal/pipeline"
)

// Shutdown budget. `docker stop` sends SIGKILL 10 s after SIGTERM, so the
// whole sequence (plus the manager's 2 s cancel grace) stays under that.
const (
	httpShutdownTimeout     = 2 * time.Second // in-flight HTTP requests
	pipelineShutdownTimeout = 4 * time.Second // a running pipeline, before it is cancelled
	wsPingInterval          = 30 * time.Second
	readHeaderTimeout       = 10 * time.Second
	idleTimeout             = 120 * time.Second
)

func main() {
	cfg := config.Load()
	policy := origin.NewPolicy(cfg.Env, cfg.AllowedOrigins)
	logStartup(cfg, policy)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		// Fail fast: without the database no event can be accepted, and the
		// container restart policy retries the connection.
		log.Fatalf("[supervisor] database connection failed: %s", models.RedactPhones(err.Error()))
	}

	hub := dashboard.NewHub(dashboard.Options{
		QueueLimit:   cfg.WSClientQueueLimit,
		WriteTimeout: cfg.WSWriteTimeout,
		Heartbeat:    cfg.WSHeartbeat,
		PingInterval: wsPingInterval,
		Origins:      policy,
	})
	network := camara.NewClient(cfg)
	ai := agent.NewClient(cfg.AgentURL, cfg.AgentTimeout)
	sms := dispatch.NewSMS(cfg)

	manager := pipeline.NewManager(pipeline.Config{
		BatchSize:           cfg.AgentBatchSize,
		CamaraConcurrency:   cfg.CamaraConcurrency,
		ReachabilityTimeout: cfg.CamaraReachabilityTimeout,
		AgentTimeout:        cfg.AgentTimeout,
		PipelineTimeout:     cfg.PipelineTimeout,
	}, db, network, ai, sms, hub)

	camaraCheck := httpapi.Check{Name: "camara", Mode: network.Mode(), Probe: network.Health}
	if network.Mode() == camara.ModeReal {
		camaraCheck.Probe = nil // every Nokia call is billed and rate-limited: reported as not checked
	}
	api := httpapi.New(httpapi.Options{
		Incidents:    manager,
		Origins:      policy,
		MaxBodyBytes: cfg.SensorMaxBodyBytes,
		Checks: []httpapi.Check{
			{Name: "database", Probe: db.HealthCheck},
			{Name: "agent", Probe: ai.Health},
			camaraCheck,
		},
		WSStats: func() httpapi.WSStats {
			s := hub.Stats()
			return httpapi.WSStats{
				Clients:               s.Clients,
				FramesPublished:       s.FramesPublished,
				SlowClientDisconnects: s.SlowClientDisconnects,
				MaxQueueDepth:         s.MaxQueueDepth,
			}
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWS) // the hub applies the origin policy to the upgrade
	api.Register(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Printf("[supervisor] listening on %s", srv.Addr)

	exitCode := 0
	select {
	case err := <-serveErr:
		log.Printf("[supervisor] server failed: %v", err)
		exitCode = 1
	case <-ctx.Done():
		log.Printf("[supervisor] shutdown signal received")
	}
	stop() // a second signal terminates immediately

	shutdown(srv, manager, hub, db)
	os.Exit(exitCode)
}

// shutdown stops accepting HTTP requests, gives a running pipeline a grace
// period (then cancels it, so its event still ends with event_complete),
// closes every WebSocket client and finally the database.
func shutdown(srv *http.Server, manager *pipeline.Manager, hub *dashboard.Hub, db *database.DB) {
	httpCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(httpCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[supervisor] HTTP shutdown: %v", err)
	}

	pipeCtx, cancelPipe := context.WithTimeout(context.Background(), pipelineShutdownTimeout)
	defer cancelPipe()
	if err := manager.Shutdown(pipeCtx); err != nil {
		log.Printf("[supervisor] pipeline shutdown: %v", err)
	}

	hub.Close()
	if err := db.Close(); err != nil {
		log.Printf("[supervisor] database close: %v", err)
	}
	log.Printf("[supervisor] stopped")
}

// logStartup prints the effective configuration without secrets or phones.
func logStartup(cfg *config.Config, policy origin.Policy) {
	log.Printf("[supervisor] GeoDispatch supervisor starting: contract v%d, environment %s", models.ContractVersion, policy.Env())
	if policy.AllowsAll() {
		log.Printf("[supervisor] WARNING: every browser origin is allowed (ALLOWED_ORIGINS=*, development only)")
	} else {
		log.Printf("[supervisor] allowed browser origins: %v (plus same-origin and clients without Origin)", policy.Origins())
	}
	log.Printf("[supervisor] network source (declared by configuration, unverified): %s", cfg.NetworkSource())
	if cfg.SMSConfigured() {
		log.Printf("[supervisor] SMS gateway: configured")
	} else {
		log.Printf("[supervisor] SMS gateway: not configured — no SMS is ever sent or reported as sent")
	}
	log.Printf("[supervisor] AI batch size %d, CAMARA concurrency %d, agent timeout %s, pipeline timeout %s",
		cfg.AgentBatchSize, cfg.CamaraConcurrency, cfg.AgentTimeout, cfg.PipelineTimeout)
}
