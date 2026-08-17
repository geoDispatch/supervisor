package zones

import (
	"math"
	"github.com/geodispatch/supervisor/internal/models"
)

func Haversine(p1, p2 models.Coordinates) float64 {
	const R = 6371.0 // Earth radius in km
	dLat := (p2.Lat - p1.Lat) * math.Pi / 180.0
	dLon := (p2.Lng - p1.Lng) * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(p1.Lat*math.Pi/180.0)*math.Cos(p2.Lat*math.Pi/180.0)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}