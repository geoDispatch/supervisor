package database

import (
	"context"
	"fmt"

	"github.com/geodispatch/supervisor/internal/models"
)

// NearestShelters returns the `limit` shelters closest to `center`,
// ordered by distance ascending.
//
// It uses PostGIS geography types so distances are accurate over large areas
// without a projection step.  The distance_km column is computed server-side
// and populated on each returned Shelter so callers never need to re-derive it.
//
// Schema assumption (from migrations/001_init.sql):
//
//	CREATE TABLE shelters (
//	    id          SERIAL PRIMARY KEY,
//	    name        TEXT    NOT NULL,
//	    address     TEXT    NOT NULL,
//	    capacity    INT     NOT NULL DEFAULT 0,
//	    location    GEOGRAPHY(POINT, 4326) NOT NULL
//	);
//	CREATE INDEX shelters_location_idx ON shelters USING GIST (location);
const nearestSheltersSQL = `
SELECT
    name,
    address,
    capacity,
    ST_Y(location::geometry)                          AS latitude,
    ST_X(location::geometry)                          AS longitude,
    ST_Distance(
        location,
        ST_MakePoint($2, $1)::geography               -- MakePoint(lng, lat)
    ) / 1000.0                                        AS distance_km
FROM shelters
ORDER BY location <-> ST_MakePoint($2, $1)::geography
LIMIT $3;
`

// NearestShelters queries PostGIS for the closest shelters to the given
// epicenter and returns at most `limit` results.
func (db *DB) NearestShelters(
	ctx context.Context,
	center models.Coordinates,
	limit int,
) ([]models.Shelter, error) {

	rows, err := db.pool.QueryContext(
		ctx,
		nearestSheltersSQL,
		center.Lat, // $1
		center.Lng, // $2
		limit,      // $3
	)
	if err != nil {
		return nil, fmt.Errorf("database: NearestShelters query: %w", err)
	}
	defer rows.Close()

	shelters := make([]models.Shelter, 0, limit)
	for rows.Next() {
		var s models.Shelter
		if err := rows.Scan(
			&s.Name,
			&s.Address,
			&s.Capacity,
			&s.Location.Lat,
			&s.Location.Lng,
			&s.DistanceKm,
		); err != nil {
			return nil, fmt.Errorf("database: NearestShelters scan: %w", err)
		}
		shelters = append(shelters, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: NearestShelters rows: %w", err)
	}

	return shelters, nil
}