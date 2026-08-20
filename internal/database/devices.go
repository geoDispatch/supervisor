package database

import (
	"context"
	"fmt"

	"github.com/geodispatch/supervisor/internal/models"
)

// PhonesNearEpicenter returns all E.164 phone numbers whose last-known
// location falls within `radiusKm` kilometres of the epicenter, ordered
// by ascending distance so the pipeline's min-heap starts with the
// most-critical devices.
//
// Schema assumption (from migrations/001_init.sql):
//
//	CREATE TABLE devices (
//	    id          SERIAL PRIMARY KEY,
//	    phone       TEXT    NOT NULL UNIQUE,   -- E.164 (+212XXXXXXXXX)
//	    location    GEOGRAPHY(POINT, 4326),    -- last known position, nullable
//	    updated_at  TIMESTAMPTZ DEFAULT NOW()
//	);
//	CREATE INDEX devices_location_idx ON devices USING GIST (location);
//
// Devices with a NULL location are excluded — CAMARA will be queried for
// live location during the per-device goroutine stage.
const phonesNearEpicenterSQL = `
SELECT phone
FROM   devices
WHERE  location IS NOT NULL
  AND  ST_DWithin(
           location,
           ST_MakePoint($2, $1)::geography,  -- MakePoint(lng, lat), radius in metres
           $3 * 1000.0
       )
ORDER BY location <-> ST_MakePoint($2, $1)::geography;
`

// PhonesNearEpicenter returns phone numbers of registered devices within
// radiusKm of the given epicenter. It never returns more rows than the
// devices table contains — the caller should handle an empty slice gracefully.
func (db *DB) PhonesNearEpicenter(
	ctx context.Context,
	epicenter models.Coordinates,
	radiusKm float64,
) ([]string, error) {

	rows, err := db.pool.QueryContext(
		ctx,
		phonesNearEpicenterSQL,
		epicenter.Lat, // $1
		epicenter.Lng, // $2
		radiusKm,      // $3  (converted to metres inside SQL)
	)
	if err != nil {
		return nil, fmt.Errorf("database: PhonesNearEpicenter query: %w", err)
	}
	defer rows.Close()

	var phones []string
	for rows.Next() {
		var phone string
		if err := rows.Scan(&phone); err != nil {
			return nil, fmt.Errorf("database: PhonesNearEpicenter scan: %w", err)
		}
		phones = append(phones, phone)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: PhonesNearEpicenter rows: %w", err)
	}

	return phones, nil
}

// UpsertDeviceLocation updates (or inserts) a device row with a fresh
// GPS fix.  Called opportunistically if a future integration feeds
// pre-positioned device locations into the system.
//
// Not used by the core pipeline today, but centralising it here keeps
// all device mutations in one file.
func (db *DB) UpsertDeviceLocation(
	ctx context.Context,
	phone string,
	loc models.Coordinates,
) error {
	const q = `
INSERT INTO devices (phone, location, updated_at)
VALUES ($1, ST_MakePoint($3, $2)::geography, NOW())
ON CONFLICT (phone) DO UPDATE
    SET location   = EXCLUDED.location,
        updated_at = EXCLUDED.updated_at;
`
	if _, err := db.pool.ExecContext(ctx, q, phone, loc.Lat, loc.Lng); err != nil {
		return fmt.Errorf("database: UpsertDeviceLocation: %w", err)
	}
	return nil
}