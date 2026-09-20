package database

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenGORM opens a *gorm.DB on the same DATABASE_URL used by the existing
// pool. It is intentionally separate from database.Connect so that the
// pipeline's raw-SQL paths are unaffected. REST handlers use this handle;
// the pipeline keeps its own.
func OpenGORM(dsn string) (*gorm.DB, error) {
	cfg := &gorm.Config{
		// Quiet in production; swap to logger.Info while debugging.
		Logger: logger.Default.LogMode(logger.Silent),
		// Prevents GORM from using plural table names automatically — our
		// tables are already named (devices, shelters, events, …).
		NamingStrategy: nil,
	}

	db, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}

	// Tune the connection pool to sit alongside the existing pgx pool without
	// doubling the connection count.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm underlying db: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}