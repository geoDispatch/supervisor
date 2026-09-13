package main

import (
	"fmt"
	"math"
	"math/rand"
	"strings"

	"github.com/geodispatch/supervisor/internal/models"
)

// Demo areas: DEVELOPMENT FIXTURES for the places a demo launches an event.
//
// Subscribers are scattered around real towns and villages — the centres are
// OpenStreetMap place coordinates — because people live in settlements, not
// on a uniform disc. A uniform disc around a coastal epicentre puts a third of
// Casablanca in the Atlantic.
//
// Every device is generated deterministically from its area's seed. The mock
// serves them from memory, and scripts/seed/seed_demo_areas.sql puts the same
// phones at the same coordinates in Postgres, so the device lookup and the
// network agree. TestDemoSeedIsCurrent keeps that file equal to this one:
//
//	go test ./scripts/camara -run TestDemoSeedIsCurrent -update
//
// The shelters are real public places at approximate coordinates, marked
// "(fixture)": none of them is a designated emergency shelter.

type locality struct {
	name     string
	lat, lng float64
	devices  int
	spreadKm float64 // standard deviation of the scatter, per axis
	// Shares of NOT_CONNECTED and CONNECTED_SMS; the rest is CONNECTED_DATA.
	notConnected, smsOnly float64
}

type areaShelter struct {
	name, address string
	capacity      int
	lat, lng      float64
}

type demoArea struct {
	name       string
	phoneFmt   string // fmt verb for the device index
	firstIndex int
	seed       int64
	observedAt string
	accuracyM  float64
	// The event the dashboard's launcher preset sends for this area. Kept here
	// so a test can check it reaches devices in every zone.
	epicentre  models.Coordinates
	radiusKm   float64
	localities []locality
	shelters   []areaShelter
}

type areaDevice struct {
	phone        string
	lat, lng     float64
	reachability models.ReachabilityStatus
	locality     string
}

// Casablanca's 300 seeded subscribers: +212600000001-040 are the named
// fixtures in fixtures.json, and these districts hold +212600000041-300.
var casablancaArea = demoArea{
	name:       "casablanca",
	phoneFmt:   "+21260000%04d",
	firstIndex: 41,
	seed:       2026,
	observedAt: "2026-09-13T10:00:00Z",
	accuracyM:  500,
	epicentre:  models.Coordinates{Lat: 33.5731, Lng: -7.5898},
	radiusKm:   15,
	localities: []locality{
		{"Derb Sultan", 33.5721, -7.5958, 15, 0.45, 0.35, 0.25},
		{"Mers Sultan", 33.5698, -7.6088, 15, 0.45, 0.35, 0.25},
		{"Ben M'Sick", 33.5600, -7.5750, 15, 0.55, 0.35, 0.25},
		{"Hay Mohammadi", 33.5896, -7.5713, 18, 0.55, 0.35, 0.25},
		{"Belvédère", 33.5930, -7.5870, 8, 0.35, 0.35, 0.25},
		{"Maârif", 33.5708, -7.6283, 18, 0.55, 0.35, 0.25},
		{"Aïn Chock", 33.5371, -7.5982, 14, 0.60, 0.35, 0.25},
		{"Sidi Othmane", 33.5560, -7.5480, 14, 0.55, 0.35, 0.25},
		{"Oasis", 33.5547, -7.6314, 10, 0.45, 0.35, 0.25},
		{"Californie", 33.5412, -7.6260, 10, 0.50, 0.35, 0.25},
		{"Moulay Rachid", 33.5649, -7.5354, 16, 0.60, 0.15, 0.45},
		{"Aïn Sebaâ", 33.5960, -7.5283, 10, 0.45, 0.15, 0.45},
		{"Lahraouiyine", 33.5425, -7.5299, 12, 0.60, 0.15, 0.45},
		{"Sidi Maârouf", 33.5200, -7.6400, 10, 0.55, 0.15, 0.45},
		{"Sidi Moumen", 33.5844, -7.5072, 14, 0.60, 0.15, 0.45},
		{"Lissasfa", 33.5315, -7.6724, 10, 0.55, 0.15, 0.45},
		{"Hay Hassani", 33.5420, -7.6880, 14, 0.50, 0.15, 0.45},
		{"Anfa", 33.5760, -7.6880, 6, 0.40, 0.15, 0.45},
		{"Sidi Bernoussi", 33.6120, -7.4980, 8, 0.45, 0.15, 0.45},
		{"Tit Mellil", 33.5514, -7.4836, 13, 0.60, 0.05, 0.10},
		{"Bouskoura", 33.4600, -7.6507, 10, 0.60, 0.05, 0.10},
	},
}

// Al Haouz: the High Atlas villages around the 8 September 2023 epicentre
// (USGS 31.058 N, 8.385 W). Village networks fared worst, hence the higher
// NOT_CONNECTED shares close in.
var alHaouzArea = demoArea{
	name:       "al-haouz",
	phoneFmt:   "+212601%06d",
	firstIndex: 1,
	seed:       20230908,
	observedAt: "2026-09-13T10:00:00Z",
	accuracyM:  900,
	epicentre:  models.Coordinates{Lat: 31.058, Lng: -8.385},
	radiusKm:   50,
	localities: []locality{
		{"Ighil", 31.0059, -8.3556, 35, 0.70, 0.45, 0.25},
		{"Azgour", 31.1221, -8.3624, 25, 0.60, 0.45, 0.25},
		{"Adassil", 31.1109, -8.4949, 30, 0.70, 0.45, 0.25},
		{"Anougal", 31.1197, -8.2734, 25, 0.60, 0.45, 0.25},
		{"Talat N'Yaaqoub", 30.9905, -8.1849, 45, 0.80, 0.25, 0.35},
		{"Ijoukak", 30.9993, -8.1586, 25, 0.60, 0.25, 0.35},
		{"Amizmiz", 31.2171, -8.2333, 90, 1.10, 0.25, 0.35},
		{"Ouirgane", 31.1757, -8.0789, 30, 0.70, 0.25, 0.35},
		{"Imlil", 31.1368, -7.9208, 35, 0.70, 0.10, 0.30},
		{"Asni", 31.2496, -7.9800, 60, 1.00, 0.10, 0.30},
		{"Moulay Brahim", 31.2858, -7.9655, 50, 0.90, 0.10, 0.30},
	},
	// At the edge of each town on the side away from the epicentre, the way
	// the 2023 camps were set up — and clear of the town's own subscribers.
	shelters: []areaShelter{
		{"Centre d'accueil de Talat N'Yaaqoub (fixture)", "Talat N'Yaaqoub, Al Haouz", 600, 30.9845, -8.1689},
		{"Centre d'accueil d'Amizmiz (fixture)", "Amizmiz, Al Haouz", 1200, 31.2291, -8.2193},
		{"Centre d'accueil d'Ouirgane (fixture)", "Ouirgane, Al Haouz", 400, 31.1807, -8.0619},
		{"Centre d'accueil d'Asni (fixture)", "Asni, Al Haouz", 800, 31.2596, -7.9670},
	},
}

// Agadir and the Inezgane / Aït Melloul conurbation. Coastal districts keep a
// tight scatter so none of it reaches the beach.
var agadirArea = demoArea{
	name:       "agadir",
	phoneFmt:   "+212602%06d",
	firstIndex: 1,
	seed:       19600229,
	observedAt: "2026-09-13T10:00:00Z",
	accuracyM:  500,
	epicentre:  models.Coordinates{Lat: 30.4205, Lng: -9.5839},
	radiusKm:   20,
	localities: []locality{
		{"Agadir centre", 30.4205, -9.5839, 70, 0.55, 0.30, 0.25},
		{"Talborjt", 30.4237, -9.5870, 35, 0.35, 0.30, 0.25},
		{"Bensergao", 30.3887, -9.5650, 35, 0.50, 0.30, 0.25},
		{"Hay Dakhla", 30.4112, -9.5568, 35, 0.55, 0.30, 0.25},
		{"Hay Mohammadi", 30.4320, -9.5520, 35, 0.55, 0.30, 0.25},
		{"Dcheira El Jihadia", 30.3753, -9.5285, 40, 0.70, 0.15, 0.35},
		{"Inezgane", 30.3563, -9.5459, 50, 0.70, 0.15, 0.35},
		{"Aït Melloul", 30.3400, -9.5000, 45, 0.80, 0.15, 0.35},
		{"Drarga", 30.3820, -9.4764, 25, 0.60, 0.15, 0.35},
		{"Temsia", 30.3638, -9.4170, 25, 0.60, 0.07, 0.20},
		{"Lqliâa", 30.2986, -9.4620, 25, 0.70, 0.07, 0.20},
	},
	shelters: []areaShelter{
		{"Université Ibn Zohr (fixture)", "Hay Dakhla, Agadir", 3000, 30.4048, -9.5777},
		{"Parc Hay El Mohammadi (fixture)", "Hay Mohammadi, Agadir", 1500, 30.4387, -9.5581},
		{"Stade Adrar (fixture)", "Stade Adrar, Agadir", 5000, 30.4275, -9.5402},
		{"Centre d'accueil d'Inezgane (fixture)", "Inezgane", 1200, 30.3470, -9.5370},
	},
}

// Casablanca's districts replace the positions of subscribers the seed already
// holds; Al Haouz and Agadir add subscribers and shelters of their own.
var demoAreas = []demoArea{casablancaArea, alHaouzArea, agadirArea}

// scatterClip bounds the scatter at 2.5 standard deviations, so no tail puts a
// village's resident in the next valley — or in the sea.
const scatterClip = 2.5

func (a demoArea) deviceCount() int {
	n := 0
	for _, l := range a.localities {
		n += l.devices
	}
	return n
}

func (a demoArea) generate() []areaDevice {
	rng := rand.New(rand.NewSource(a.seed))
	out := make([]areaDevice, 0, a.deviceCount())
	index := a.firstIndex
	for _, l := range a.localities {
		for k := 0; k < l.devices; k++ {
			lat, lng := scatter(rng, l.lat, l.lng, l.spreadKm)
			out = append(out, areaDevice{
				phone:        fmt.Sprintf(a.phoneFmt, index),
				lat:          lat,
				lng:          lng,
				reachability: pickReachability(rng, l),
				locality:     l.name,
			})
			index++
		}
	}
	return out
}

func scatter(rng *rand.Rand, lat, lng, spreadKm float64) (float64, float64) {
	for {
		dx, dy := rng.NormFloat64(), rng.NormFloat64()
		if math.Hypot(dx, dy) > scatterClip {
			continue
		}
		dLat := dy * spreadKm / 111.32
		dLng := dx * spreadKm / (111.32 * math.Cos(lat*math.Pi/180))
		return round5(lat + dLat), round5(lng + dLng)
	}
}

func pickReachability(rng *rand.Rand, l locality) models.ReachabilityStatus {
	u := rng.Float64()
	switch {
	case u < l.notConnected:
		return models.NotConnected
	case u < l.notConnected+l.smsOnly:
		return models.ReachableSMS
	default:
		return models.ReachableData
	}
}

// round5 keeps coordinates to 1e-5 degrees (about a metre), the precision the
// seed file writes, so the mock and Postgres hold the same numbers.
func round5(v float64) float64 {
	return math.Round(v*1e5) / 1e5
}

// addDemoAreas puts every generated subscriber in the mock. A phone that
// fixtures.json names explicitly keeps its reviewed values.
func addDemoAreas(devices map[string]deviceFixture) {
	for _, area := range demoAreas {
		for _, d := range area.generate() {
			if _, named := devices[d.phone]; named {
				continue
			}
			devices[d.phone] = makeFixture(d.lat, d.lng, area.accuracyM, area.observedAt, d.reachability)
		}
	}
}

// renderDemoSeed writes scripts/seed/seed_demo_areas.sql: every demo-area
// subscriber (Casablanca's named fixtures included) at the position the mock
// reports, and the areas' shelters. Upserts, so it also repairs a volume that
// was seeded from an older seed_devices.sql.
func renderDemoSeed(named map[string]fixtureDevice) string {
	var b strings.Builder
	b.WriteString(`-- GENERATED from scripts/camara/areas.go — do not edit by hand. Regenerate with:
--   go test ./scripts/camara -run TestDemoSeedIsCurrent -update
--
-- DEVELOPMENT FIXTURE — synthetic subscribers around real towns and villages,
-- and shelters at real public places marked "(fixture)". Not real people and
-- not designated emergency shelters.
--
-- Runs after seed_devices.sql (004). It moves Casablanca's 300 subscribers to
-- the positions the mock CAMARA reports, so the device lookup and the network
-- agree, and adds the Al Haouz and Agadir areas. Safe to run again on an
-- existing volume:
--   docker exec -i geodispatch_postgres_dev psql -U geodispatch -d geodispatch < scripts/seed/seed_demo_areas.sql
`)
	for _, area := range demoAreas {
		fmt.Fprintf(&b, "\n-- %s: %d generated subscribers", area.name, area.deviceCount())
		rows := area.generate()
		if area.firstIndex > 1 {
			// Casablanca: the named fixtures come first, at their reviewed positions.
			var head []areaDevice
			for i := 1; i < area.firstIndex; i++ {
				phone := fmt.Sprintf(area.phoneFmt, i)
				if d, ok := named[phone]; ok {
					head = append(head, areaDevice{phone: phone, lat: d.Latitude, lng: d.Longitude})
				}
			}
			fmt.Fprintf(&b, " after %d named fixtures", len(head))
			rows = append(head, rows...)
		}
		b.WriteString("\nINSERT INTO devices (phone, location) VALUES\n")
		for i, d := range rows {
			sep := ","
			if i == len(rows)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "('%s', ST_MakePoint(%.5f, %.5f)::geography)%s\n", d.phone, d.lng, d.lat, sep)
		}
		b.WriteString("ON CONFLICT (phone) DO UPDATE SET location = EXCLUDED.location, updated_at = NOW();\n")

		// shelters.name is not unique, so an upsert is an UPDATE of the row by
		// that name, then an INSERT only if there was none.
		for _, s := range area.shelters {
			name := sqlString(s.name)
			fmt.Fprintf(&b, "\nUPDATE shelters SET address = %s, capacity = %d, location = ST_MakePoint(%.4f, %.4f)::geography WHERE name = %s;\n"+
				"INSERT INTO shelters (name, address, capacity, location)\n"+
				"SELECT %s, %s, %d, ST_MakePoint(%.4f, %.4f)::geography\n"+
				"WHERE NOT EXISTS (SELECT 1 FROM shelters WHERE name = %s);\n",
				sqlString(s.address), s.capacity, s.lng, s.lat, name,
				name, sqlString(s.address), s.capacity, s.lng, s.lat, name)
		}
	}
	return b.String()
}

func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
