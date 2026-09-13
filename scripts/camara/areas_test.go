package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
	"github.com/geodispatch/supervisor/internal/zones"
)

var update = flag.Bool("update", false, "rewrite scripts/seed/seed_demo_areas.sql from areas.go")

const demoSeedPath = "../seed/seed_demo_areas.sql"

// namedFixtures is fixtures.json's explicit devices, keyed by phone.
func namedFixtures(t *testing.T) map[string]fixtureDevice {
	t.Helper()
	var doc fixtureDocument
	if err := json.NewDecoder(bytes.NewReader(fixturesJSON)).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]fixtureDevice{}
	for _, g := range doc.Geographies {
		for _, d := range g.Devices {
			out[d.Phone] = d
		}
	}
	return out
}

func TestDemoSeedIsCurrent(t *testing.T) {
	got := renderDemoSeed(namedFixtures(t))
	if *update {
		if err := os.WriteFile(demoSeedPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(demoSeedPath)
	if err != nil {
		t.Fatalf("read %s: %v (generate it: go test ./scripts/camara -run TestDemoSeedIsCurrent -update)", demoSeedPath, err)
	}
	if string(want) != got {
		t.Fatalf("%s is out of date with areas.go: go test ./scripts/camara -run TestDemoSeedIsCurrent -update", demoSeedPath)
	}
}

func TestDemoAreasAreDeterministic(t *testing.T) {
	for _, area := range demoAreas {
		if !reflect.DeepEqual(area.generate(), area.generate()) {
			t.Errorf("%s: two generations differ", area.name)
		}
	}
}

func TestDemoAreaPhones(t *testing.T) {
	seen := map[string]string{}
	for _, area := range demoAreas {
		devs := area.generate()
		if len(devs) != area.deviceCount() {
			t.Fatalf("%s: %d devices, want %d", area.name, len(devs), area.deviceCount())
		}
		for i, d := range devs {
			if !e164Pattern.MatchString(d.phone) {
				t.Errorf("%s: %q is not E.164", area.name, d.phone)
			}
			if want := fmt.Sprintf(area.phoneFmt, area.firstIndex+i); d.phone != want {
				t.Errorf("%s: device %d is %s, want %s", area.name, i, d.phone, want)
			}
			if other, dup := seen[d.phone]; dup {
				t.Errorf("%s is in both %s and %s", d.phone, other, area.name)
			}
			seen[d.phone] = area.name
		}
	}
	// Casablanca's districts fill exactly the seeded phones after the named 40.
	devs := casablancaArea.generate()
	if devs[0].phone != "+212600000041" || devs[len(devs)-1].phone != "+212600000300" {
		t.Fatalf("casablanca covers %s..%s, want +212600000041..+212600000300", devs[0].phone, devs[len(devs)-1].phone)
	}
}

// Every subscriber stays within the clipped scatter of its own town.
func TestDemoAreasStayNearTheirTowns(t *testing.T) {
	for _, area := range demoAreas {
		centre := map[string]locality{}
		for _, l := range area.localities {
			centre[l.name] = l
		}
		for _, d := range area.generate() {
			l := centre[d.locality]
			km := zones.Haversine(models.Coordinates{Lat: d.lat, Lng: d.lng}, models.Coordinates{Lat: l.lat, Lng: l.lng})
			if limit := scatterClip*l.spreadKm + 0.01; km > limit {
				t.Errorf("%s: %s is %.2f km from %s, limit %.2f km", area.name, d.phone, km, l.name, limit)
			}
		}
	}
}

// The launcher preset for each area reaches people in all three zones, and
// its three nearest shelters are the area's own.
func TestDemoAreaPresetsCoverEveryZone(t *testing.T) {
	for _, area := range demoAreas {
		count := map[models.ZoneType]int{}
		for _, d := range area.generate() {
			km := zones.Haversine(models.Coordinates{Lat: d.lat, Lng: d.lng}, area.epicentre)
			if km <= area.radiusKm {
				count[zones.Assign(km, area.radiusKm)]++
			}
		}
		for _, z := range []models.ZoneType{models.ZoneRed, models.ZoneOrange, models.ZoneGreen} {
			if count[z] < 20 {
				t.Errorf("%s preset: only %d devices in the %s zone (%v)", area.name, count[z], z, count)
			}
		}
		for _, s := range area.shelters {
			if km := zones.Haversine(models.Coordinates{Lat: s.lat, Lng: s.lng}, area.epicentre); km > area.radiusKm {
				t.Errorf("%s: shelter %q is %.1f km out, beyond the %.0f km preset radius", area.name, s.name, km, area.radiusKm)
			}
		}
	}
}
