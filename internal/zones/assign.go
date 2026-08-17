package zones

import "github.com/geodispatch/supervisor/internal/models"

func Assign(distKm, radiusKm float64) models.ZoneType {
	thresholdRed := radiusKm * 0.33
	thresholdOrange := radiusKm * 0.66

	if distKm <= thresholdRed {
		return models.ZoneRed
	} else if distKm <= thresholdOrange {
		return models.ZoneOrange
	}
	return models.ZoneGreen
}