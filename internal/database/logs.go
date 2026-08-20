package database

import (
	"context"
	"fmt"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// ─────────────────────────────────────────────────────────────────────────────
// SCHEMA ASSUMPTIONS (from migrations/002_events.sql)
//
//  CREATE TABLE events (
//      id              TEXT PRIMARY KEY,           -- SensorInput.EventID
//      disaster_type   TEXT NOT NULL,
//      severity        FLOAT NOT NULL,
//      epicenter_lat   FLOAT NOT NULL,
//      epicenter_lng   FLOAT NOT NULL,
//      radius_km       FLOAT NOT NULL,
//      aftershock_risk TEXT NOT NULL,
//      tsunami_risk    BOOLEAN NOT NULL,
//      created_at      TIMESTAMPTZ DEFAULT NOW()
//  );
//
//  CREATE TABLE device_logs (
//      id              SERIAL PRIMARY KEY,
//      event_id        TEXT NOT NULL REFERENCES events(id),
//      phone           TEXT NOT NULL,
//      zone            TEXT NOT NULL,              -- "red"|"orange"|"green"
//      action          TEXT NOT NULL,              -- ActionType value
//      sms_message     TEXT,
//      shelter_name    TEXT,
//      rescue_priority INT  DEFAULT 0,
//      confidence      FLOAT,
//      zone_escalated  BOOLEAN DEFAULT FALSE,
//      logged_at       TIMESTAMPTZ DEFAULT NOW()
//  );
//
//  CREATE TABLE rescue_flags (
//      id              SERIAL PRIMARY KEY,
//      event_id        TEXT NOT NULL REFERENCES events(id),
//      phone           TEXT NOT NULL,
//      zone            TEXT NOT NULL,
//      rescue_priority INT  DEFAULT 0,
//      flagged_at      TIMESTAMPTZ DEFAULT NOW()
//  );
// ─────────────────────────────────────────────────────────────────────────────

// InsertEvent persists the top-level disaster event metadata.
// Call this once per SensorInput, before any per-device writes.
func (db *DB) InsertEvent(ctx context.Context, input *models.SensorInput) error {
	const q = `
INSERT INTO events (
    id, disaster_type, severity,
    epicenter_lat, epicenter_lng, radius_km,
    aftershock_risk, tsunami_risk, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO NOTHING;
`
	_, err := db.pool.ExecContext(ctx, q,
		input.EventID,
		string(input.DisasterType),
		input.Severity,
		input.Epicenter.Lat,
		input.Epicenter.Lng,
		input.RadiusKm,
		string(input.AftershockRisk),
		input.TsunamiRisk,
		time.UnixMilli(input.Timestamp).UTC(),
	)
	if err != nil {
		return fmt.Errorf("database: InsertEvent %q: %w", input.EventID, err)
	}
	return nil
}

// InsertDeviceLog writes one row per DeviceDecision after the AI agent
// responds.  Call this from the dispatch goroutine alongside SendSMS /
// FlagRescue so all outcomes are durably recorded.
func (db *DB) InsertDeviceLog(
	ctx context.Context,
	eventID string,
	d models.DeviceDecision,
) error {
	const q = `
INSERT INTO device_logs (
    event_id, phone, zone, action,
    sms_message, shelter_name,
    rescue_priority, confidence, zone_escalated,
    logged_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW());
`
	_, err := db.pool.ExecContext(ctx, q,
		eventID,
		d.Phone,
		string(d.ZoneConfirmed),
		string(d.Action),
		d.SMSMessage,
		d.ShelterName,
		d.RescuePriority,
		d.Confidence,
		d.ZoneEscalated,
	)
	if err != nil {
		return fmt.Errorf("database: InsertDeviceLog phone=%s event=%s: %w",
			d.Phone, eventID, err)
	}
	return nil
}

// FlagRescue writes a rescue_flags row for a device that needs physical
// intervention.  This is the function called by dispatch.FlagRescue —
// the dispatch package holds the business logic (priority thresholds,
// duplicate suppression) while this method owns the write.
func (db *DB) FlagRescue(
	ctx context.Context,
	eventID string,
	d models.DeviceDecision,
) error {
	const q = `
INSERT INTO rescue_flags (
    event_id, phone, zone, rescue_priority, flagged_at
) VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT DO NOTHING;
`
	_, err := db.pool.ExecContext(ctx, q,
		eventID,
		d.Phone,
		string(d.ZoneConfirmed),
		d.RescuePriority,
	)
	if err != nil {
		return fmt.Errorf("database: FlagRescue phone=%s event=%s: %w",
			d.Phone, eventID, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// READ HELPERS  (useful for the dashboard /api/events endpoint later)
// ─────────────────────────────────────────────────────────────────────────────

// RescueFlagsForEvent returns every rescue-flagged phone for a given event,
// ordered by priority descending.  Useful for a command-centre view.
func (db *DB) RescueFlagsForEvent(
	ctx context.Context,
	eventID string,
) ([]models.DeviceDecision, error) {
	const q = `
SELECT phone, zone, rescue_priority
FROM   rescue_flags
WHERE  event_id = $1
ORDER  BY rescue_priority DESC, flagged_at ASC;
`
	rows, err := db.pool.QueryContext(ctx, q, eventID)
	if err != nil {
		return nil, fmt.Errorf("database: RescueFlagsForEvent %q: %w", eventID, err)
	}
	defer rows.Close()

	var out []models.DeviceDecision
	for rows.Next() {
		var d models.DeviceDecision
		var zone string
		if err := rows.Scan(&d.Phone, &zone, &d.RescuePriority); err != nil {
			return nil, fmt.Errorf("database: RescueFlagsForEvent scan: %w", err)
		}
		d.ZoneConfirmed = models.ZoneType(zone)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: RescueFlagsForEvent rows: %w", err)
	}
	return out, nil
}