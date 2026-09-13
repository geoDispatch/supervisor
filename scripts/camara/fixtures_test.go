package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/camara"
)

// seedRow matches one active VALUES row of seed_devices.sql:
// ('+212600000001', ST_MakePoint(<lng>, <lat>)::geography)
var seedRow = regexp.MustCompile(`^\('(\+\d+)',\s*ST_MakePoint\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*\)::geography\)[,;]?$`)

type point struct{ lat, lng float64 }

// expectedMockDevices: the 40 named Casablanca and 40 Budapest fixtures, plus
// every generated demo-area subscriber (Casablanca's start after the named 40).
func expectedMockDevices() int {
	n := 80
	for _, area := range demoAreas {
		n += area.deviceCount()
	}
	return n
}

// seedDevices parses the active VALUES rows of one seed file.
func seedDevices(t *testing.T, path string) map[string]point {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	out := map[string]point{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue // comments never count: only rows Postgres will insert
		}
		m := seedRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lng, err1 := strconv.ParseFloat(m[2], 64)
		lat, err2 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("bad coordinates in %q", line)
		}
		if _, dup := out[m[1]]; dup {
			t.Fatalf("seed has %s twice", m[1])
		}
		out[m[1]] = point{lat: lat, lng: lng}
	}
	return out
}

func TestFixturesMatchSeed(t *testing.T) {
	devs, err := loadAllFixtures()
	if err != nil {
		t.Fatal(err)
	}
	base := seedDevices(t, "../seed/seed_devices.sql")
	if len(base) != 300 || len(devs) != expectedMockDevices() {
		t.Fatalf("want 300 active Casablanca seed devices and %d mock devices, seed=%d fixtures=%d",
			expectedMockDevices(), len(base), len(devs))
	}

	// Postgres runs seed_devices.sql, then seed_demo_areas.sql over it.
	seed := base
	for phone, p := range seedDevices(t, "../seed/seed_demo_areas.sql") {
		seed[phone] = p
	}
	for phone, p := range seed {
		d, ok := devs[phone]
		if !ok {
			t.Errorf("%s is seeded but has no fixture", phone)
			continue
		}
		// The device lookup and the network put every subscriber in one place.
		c := d.location.Area.Center
		if math.Abs(c.Lat-p.lat) > 1e-9 || math.Abs(c.Lng-p.lng) > 1e-9 {
			t.Errorf("%s: seeded at %.5f,%.5f but the mock reports %.5f,%.5f", phone, p.lat, p.lng, c.Lat, c.Lng)
		}
	}
	for _, phone := range []string{"+212600000001", "+212600000300", "+36719991001", "+36719991040", "+212601000001", "+212602000420"} {
		if _, ok := devs[phone]; !ok {
			t.Errorf("missing %s", phone)
		}
	}
}

func TestLoadFixturesRejectsBadData(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field": `{"geographies":[{"name":"x","observed_at":"2026-08-18T10:00:00Z","devices":[{"phone":"+212600000001","latitude":1,"longitude":1,"accuracy_m":1,"reachability":"CONNECTED_DATA","zone":"red"}]}]}`,
		"bad phone":     `{"geographies":[{"name":"x","observed_at":"2026-08-18T10:00:00Z","devices":[{"phone":"0600","latitude":1,"longitude":1,"accuracy_m":1,"reachability":"CONNECTED_DATA"}]}]}`,
		"bad status":    `{"geographies":[{"name":"x","observed_at":"2026-08-18T10:00:00Z","devices":[{"phone":"+212600000001","latitude":1,"longitude":1,"accuracy_m":1,"reachability":"UP"}]}]}`,
		"bad time":      `{"geographies":[{"name":"x","observed_at":"yesterday","devices":[]}]}`,
		"empty":         `{"geographies":[]}`,
	} {
		if _, err := loadFixtures([]byte(raw)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	devs, err := loadAllFixtures()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newHandler(devs))
	t.Cleanup(srv.Close)
	return srv
}

func TestUnknownPhoneIs404(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/location?phone=%2B212699999999", "/reachability?phone=%2B212699999999", "/location/+212699999999"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got["status"] != float64(404) || got["code"] != "NOT_FOUND" || got["message"] != "device not found" || len(got) != 3 {
			t.Fatalf("%s: body %s", path, body)
		}
	}

	resp, err := http.Get(srv.URL + "/location")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing phone: status %d", resp.StatusCode)
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Status  string `json:"status"`
		Devices int    `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || got.Status != "ok" || got.Devices != expectedMockDevices() {
		t.Fatalf("status %d body %+v", resp.StatusCode, got)
	}
}

// The supervisor's own CAMARA client reads the mock correctly.
func TestSupervisorClientAgainstMock(t *testing.T) {
	srv := newTestServer(t)
	c := camara.NewClient(&config.Config{MockNokiaNacBaseURL: srv.URL, CamaraTimeout: 5 * time.Second})
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	loc, err := c.Location(ctx, "+36719991001")
	if err != nil {
		t.Fatal(err)
	}
	if loc.Area.Center.Lat != 47.4979 || loc.Area.Center.Lng != 19.0402 || loc.Area.AreaType != "CIRCLE" {
		t.Fatalf("location %+v", loc)
	}
	reach, err := c.Reachability(ctx, "+212600000004")
	if err != nil || reach.ReachabilityStatus != "CONNECTED_SMS" {
		t.Fatalf("reachability %+v %v", reach, err)
	}
	if _, err := c.Location(ctx, "+212699999999"); !errors.Is(err, camara.ErrNotFound) {
		t.Fatalf("unknown phone: want ErrNotFound, got %v", err)
	}
}
