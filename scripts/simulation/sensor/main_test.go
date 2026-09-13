package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

var fullArgs = []string{
	"-host", "http://sup:8080", "-event-id", "EQ-TEST-9", "-type", "earthquake",
	"-timestamp", "1757699999000", "-severity", "6.8", "-lat", "47.4979", "-lng", "19.0402",
	"-radius", "15", "-depth", "10.5", "-aftershock", "high", "-tsunami",
}

func TestRequiredFlags(t *testing.T) {
	now := time.UnixMilli(1757700000000)
	for _, name := range requiredFlags {
		t.Run(name, func(t *testing.T) {
			var args []string
			for i := 0; i < len(fullArgs); i++ {
				if fullArgs[i] == "-"+name {
					i++ // drop the flag and its value
					continue
				}
				args = append(args, fullArgs[i])
			}
			_, err := parseArgs(args, now, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "-"+name) {
				t.Fatalf("want a missing -%s error, got %v", name, err)
			}
		})
	}

	_, err := parseArgs(nil, now, io.Discard)
	if err == nil {
		t.Fatal("no flags must fail")
	}
	for _, name := range requiredFlags {
		if !strings.Contains(err.Error(), "-"+name) {
			t.Fatalf("error %q does not list -%s", err, name)
		}
	}
}

func TestEveryFieldSet(t *testing.T) {
	o, err := parseArgs(fullArgs, time.Now(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := models.SensorInput{
		EventID: "EQ-TEST-9", DisasterType: models.Earthquake, Timestamp: 1757699999000,
		Severity: 6.8, Epicenter: models.Coordinates{Lat: 47.4979, Lng: 19.0402},
		RadiusKm: 15, DepthKm: 10.5, AftershockRisk: models.AftershockHigh, TsunamiRisk: true,
	}
	if o.payload != want {
		t.Fatalf("payload %+v\nwant    %+v", o.payload, want)
	}
	if o.host != "http://sup:8080" {
		t.Fatalf("host %q", o.host)
	}

	// Every SensorInput field is a flag: no field is left at a zero value
	// the user could not choose.
	v := reflect.ValueOf(o.payload)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Errorf("field %s was not set from the flags", v.Type().Field(i).Name)
		}
	}
}

func TestDefaults(t *testing.T) {
	now := time.UnixMilli(1757700000000)
	o, err := parseArgs([]string{"-severity", "5", "-lat", "1", "-lng", "2", "-radius", "3", "-depth", "0", "-aftershock", "LOW"}, now, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	p := o.payload
	if want := "SIM-" + strings.ToUpper(strconv.FormatInt(now.UnixMilli(), 36)); p.EventID != want {
		t.Fatalf("event id %q", p.EventID)
	}
	if p.Timestamp != now.UnixMilli() || p.DisasterType != models.Earthquake || p.TsunamiRisk {
		t.Fatalf("defaults %+v", p)
	}
}

func TestSendPostsTheFullSensorInput(t *testing.T) {
	var got map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sensor" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("got %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"accepted"}`))
	}))
	defer srv.Close()

	o, err := parseArgs(fullArgs, time.Now(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	o.host = srv.URL + "/"
	status, body, err := send(srv.Client(), o)
	if err != nil || status != http.StatusAccepted || string(body) != `{"status":"accepted"}` {
		t.Fatalf("status %d body %s err %v", status, body, err)
	}

	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	wantKeys := []string{"aftershock_risk", "depth_km", "disaster_type", "epicenter", "event_id", "radius_km", "severity", "timestamp", "tsunami_risk"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("posted keys %v, want %v", keys, wantKeys)
	}
}
