package pipeline

import (
	"sort"

	"github.com/geodispatch/supervisor/internal/models"
)

// zoneOrder is the order batches are sent to the AI: most severe first.
var zoneOrder = []models.ZoneType{models.ZoneRed, models.ZoneOrange, models.ZoneGreen}

// PartitionBatches groups devs by their Go zone (red, then orange, then
// green), sorts each zone by distance_km ascending (ties by phone, so the
// order is deterministic) and splits it into chunks of at most size devices
// (clamped to 1..MaxBatchSize). A batch never mixes zones: the agent answers
// for exactly one zone per request. devs is not modified.
//
// Devices whose zone is not red/orange/green are not batched; zones.Assign
// never produces one.
func PartitionBatches(devs []models.TriagedDevice, size int) [][]models.TriagedDevice {
	size = clampBatchSize(size)
	byZone := make(map[models.ZoneType][]models.TriagedDevice, len(zoneOrder))
	for _, d := range devs {
		byZone[d.Zone] = append(byZone[d.Zone], d)
	}

	var batches [][]models.TriagedDevice
	for _, z := range zoneOrder {
		group := byZone[z]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].DistanceKm != group[j].DistanceKm {
				return group[i].DistanceKm < group[j].DistanceKm
			}
			return group[i].Phone < group[j].Phone
		})
		for start := 0; start < len(group); start += size {
			end := min(start+size, len(group))
			// Full slice expression: appending to one batch can never
			// overwrite the next one.
			batches = append(batches, group[start:end:end])
		}
	}
	return batches
}
