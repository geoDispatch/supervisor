package zones

import (
	"math"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

func TestAssignBoundaries(t *testing.T) {
	const r = 15.0
	above := func(x float64) float64 { return math.Nextafter(x, math.Inf(1)) }

	cases := []struct {
		name   string
		dist   float64
		radius float64
		want   models.ZoneType
	}{
		{"epicentre", 0, r, models.ZoneRed},
		{"red edge is red", r * RedRatio, r, models.ZoneRed},
		{"just above red edge", above(r * RedRatio), r, models.ZoneOrange},
		{"orange edge is orange", r * OrangeRatio, r, models.ZoneOrange},
		{"just above orange edge", above(r * OrangeRatio), r, models.ZoneGreen},
		{"radius is green", r, r, models.ZoneGreen},
		{"beyond radius is green", r * 2, r, models.ZoneGreen},
		{"radius 0, at epicentre", 0, 0, models.ZoneRed},
		{"radius 0, any distance", 0.001, 0, models.ZoneGreen},
	}
	for _, c := range cases {
		if got := Assign(c.dist, c.radius); got != c.want {
			t.Errorf("%s: Assign(%v, %v) = %q, want %q", c.name, c.dist, c.radius, got, c.want)
		}
	}
}

func TestBandsMatchConstants(t *testing.T) {
	b := Bands()
	if b.Red != RedRatio || b.Orange != OrangeRatio || b.Green != GreenRatio {
		t.Fatalf("Bands() = %+v, want %v/%v/%v", b, RedRatio, OrangeRatio, GreenRatio)
	}
	if !(0 < b.Red && b.Red < b.Orange && b.Orange < b.Green) {
		t.Fatalf("bands must increase: %+v", b)
	}
	// The contract publishes these exact values (spec §1.2).
	if b != (models.ZoneBands{Red: 0.33, Orange: 0.66, Green: 1.0}) {
		t.Fatalf("bands drifted from the contract: %+v", b)
	}
}

func TestHaversineKnownDistance(t *testing.T) {
	// Casablanca → Rabat is about 87 km as the crow flies.
	d := Haversine(models.Coordinates{Lat: 33.5731, Lng: -7.5898}, models.Coordinates{Lat: 34.0209, Lng: -6.8416})
	if d < 84 || d > 90 {
		t.Fatalf("Haversine Casablanca-Rabat = %.2f km, want ~87", d)
	}
	if Haversine(models.Coordinates{Lat: 1, Lng: 2}, models.Coordinates{Lat: 1, Lng: 2}) != 0 {
		t.Fatal("distance from a point to itself must be 0")
	}
}
