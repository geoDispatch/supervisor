package pipeline

import (
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

func dev(n int, zone models.ZoneType, dist float64) models.TriagedDevice {
	return models.TriagedDevice{Phone: phoneN(n), Zone: zone, DistanceKm: dist}
}

func TestPartitionBatchesZoneOrderAndSorting(t *testing.T) {
	in := []models.TriagedDevice{
		dev(1, models.ZoneGreen, 12),
		dev(2, models.ZoneRed, 3),
		dev(3, models.ZoneOrange, 7),
		dev(4, models.ZoneRed, 1),
		dev(6, models.ZoneRed, 2), // tie on distance with 5: phone order decides
		dev(5, models.ZoneRed, 2),
		dev(7, models.ZoneGreen, 10),
	}
	orig := append([]models.TriagedDevice(nil), in...)

	got := PartitionBatches(in, 20)

	want := [][]string{
		{phoneN(4), phoneN(5), phoneN(6), phoneN(2)},
		{phoneN(3)},
		{phoneN(7), phoneN(1)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d batches, want %d", len(got), len(want))
	}
	for i, b := range got {
		if len(b) != len(want[i]) {
			t.Fatalf("batch %d has %d devices, want %d", i, len(b), len(want[i]))
		}
		for j, d := range b {
			if d.Phone != want[i][j] {
				t.Errorf("batch %d[%d] = %s, want %s", i, j, d.Phone, want[i][j])
			}
			if d.Zone != b[0].Zone {
				t.Errorf("batch %d mixes zones %s and %s", i, b[0].Zone, d.Zone)
			}
		}
	}
	for i := range in {
		if in[i] != orig[i] {
			t.Fatal("PartitionBatches modified its input")
		}
	}
}

func TestPartitionBatchesSplitsLargeZones(t *testing.T) {
	var in []models.TriagedDevice
	for i := 0; i < 45; i++ {
		in = append(in, dev(i, models.ZoneRed, float64(i)/10))
	}
	in = append(in, dev(100, models.ZoneGreen, 14))

	got := PartitionBatches(in, 20)
	sizes := []int{20, 20, 5, 1}
	if len(got) != len(sizes) {
		t.Fatalf("got %d batches, want %d", len(got), len(sizes))
	}
	for i, b := range got {
		if len(b) != sizes[i] {
			t.Errorf("batch %d has %d devices, want %d", i, len(b), sizes[i])
		}
	}
	if got[3][0].Zone != models.ZoneGreen {
		t.Errorf("last batch zone = %s, want green (never mixed into the red remainder)", got[3][0].Zone)
	}
	// Distance order carries across the red chunks.
	if got[0][19].DistanceKm > got[1][0].DistanceKm || got[1][19].DistanceKm > got[2][0].DistanceKm {
		t.Error("red chunks are not in ascending distance order")
	}
}

func TestPartitionBatchesClampsSize(t *testing.T) {
	var in []models.TriagedDevice
	for i := 0; i < 50; i++ {
		in = append(in, dev(i, models.ZoneOrange, float64(i)))
	}
	for _, tc := range []struct {
		size    int
		batches int
		first   int
	}{
		{0, 50, 1},
		{-3, 50, 1},
		{1, 50, 1},
		{7, 8, 7},
		{20, 3, 20},
		{21, 3, 20},
		{1000, 3, 20},
	} {
		got := PartitionBatches(in, tc.size)
		if len(got) != tc.batches || len(got[0]) != tc.first {
			t.Errorf("size %d: %d batches, first %d; want %d batches, first %d",
				tc.size, len(got), len(got[0]), tc.batches, tc.first)
		}
	}
}

func TestPartitionBatchesEmpty(t *testing.T) {
	if got := PartitionBatches(nil, 20); len(got) != 0 {
		t.Fatalf("got %d batches for no devices", len(got))
	}
}
