package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	pool *sql.DB
}

// Returns a ready-to-use *DB or a descriptive error.
func Connect(ctx context.Context, databaseURL string) (*DB, error) {
	pool, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: open driver: %w", err)
	}

	// Pool tuning — these are sane defaults for a supervisor process
	// that handles bursts (disaster events) with inter-batch idle periods.
	pool.SetMaxOpenConns(25)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(5 * time.Minute)
	pool.SetConnMaxIdleTime(2 * time.Minute)

	// Verify the connection before handing the pool to the caller.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = pool.PingContext(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping failed (is DATABASE_URL correct?): %w", err)
	}

	log.Printf("[database] connected to Postgres")
	return &DB{pool: pool}, nil
}

func (db *DB) Close() error {
	return db.pool.Close()
}

// HealthCheck performs a lightweight round-trip to confirm the pool is alive.
// The /health HTTP handler can call this to expose DB liveness.
func (db *DB) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return db.pool.PingContext(ctx)
}