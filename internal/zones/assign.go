package zones

import "github.com/geodispatch/supervisor/internal/models"

// Zone bands: the OUTER edge of each band as a fraction of the event radius.
// They are published in event_start.zone_bands and /capabilities so the
// dashboard draws its rings from the same numbers Go uses to assign zones.
const (
	RedRatio    = 0.33
	OrangeRatio = 0.66
	GreenRatio  = 1.0
)

// Assign returns the zone of a device distKm from the epicentre of an event
// with radius radiusKm. Band edges are inclusive (exactly RedRatio×R is red).
// Devices beyond the radius are green: the device lookup is already limited
// to the radius, so this only absorbs haversine vs PostGIS rounding.
func Assign(distKm, radiusKm float64) models.ZoneType {
	switch {
	case distKm <= radiusKm*RedRatio:
		return models.ZoneRed
	case distKm <= radiusKm*OrangeRatio:
		return models.ZoneOrange
	}
	return models.ZoneGreen
}

// Bands returns the band ratios in their wire form.
func Bands() models.ZoneBands {
	return models.ZoneBands{Red: RedRatio, Orange: OrangeRatio, Green: GreenRatio}
}
