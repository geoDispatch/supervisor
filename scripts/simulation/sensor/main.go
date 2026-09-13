// Command sensor is a DEVELOPMENT TOOL: it POSTs one SensorInput to the
// supervisor's /sensor endpoint, as a seismic sensor would. There is no
// embedded scenario: the location, radius and magnitude must be given.
//
//	go run ./scripts/simulation/sensor -lat 47.4979 -lng 19.0402 -radius 15 \
//	    -severity 6.2 -depth 10.5 -aftershock MEDIUM
//
// It prints the HTTP status and body and exits 1 on any non-2xx reply.
// 202 means the supervisor ACCEPTED the event, not that it finished; watch
// progress with scripts/simulation/wswatch.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// requiredFlags have no sensible default: a made-up location or magnitude
// would silently simulate the wrong incident.
var requiredFlags = []string{"lat", "lng", "radius", "severity", "depth", "aftershock"}

type options struct {
	host    string
	payload models.SensorInput
}

// parseArgs parses the command line into the request to send. now supplies
// the default event id and timestamp.
func parseArgs(args []string, now time.Time, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("sensor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var o options
	p := &o.payload
	var disasterType, aftershock string
	fs.StringVar(&o.host, "host", "http://localhost:8080", "supervisor base URL")
	fs.StringVar(&p.EventID, "event-id", "SIM-"+strings.ToUpper(strconv.FormatInt(now.UnixMilli(), 36)), "event_id (default: SIM-<base36 Unix ms>)")
	fs.StringVar(&disasterType, "type", string(models.Earthquake), "disaster_type: earthquake | flood | heatwave (the supervisor rejects types it does not support)")
	fs.Int64Var(&p.Timestamp, "timestamp", now.UnixMilli(), "sensor timestamp, Unix ms (default: now)")
	fs.Float64Var(&p.Severity, "severity", 0, "severity, 0..10 (Richter magnitude for earthquakes) [required]")
	fs.Float64Var(&p.Epicenter.Lat, "lat", 0, "epicentre latitude, -90..90 [required]")
	fs.Float64Var(&p.Epicenter.Lng, "lng", 0, "epicentre longitude, -180..180 [required]")
	fs.Float64Var(&p.RadiusKm, "radius", 0, "affected radius in km, (0, 500] [required]")
	fs.Float64Var(&p.DepthKm, "depth", 0, "depth in km, 0..800 [required]")
	fs.StringVar(&aftershock, "aftershock", "", "aftershock_risk: LOW | MEDIUM | HIGH [required]")
	fs.BoolVar(&p.TsunamiRisk, "tsunami", false, "tsunami_risk")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var missing []string
	for _, name := range requiredFlags {
		if !set[name] {
			missing = append(missing, "-"+name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("missing required flags: %s", strings.Join(missing, " "))
	}

	p.DisasterType = models.DisasterType(disasterType)
	p.AftershockRisk = models.AftershockRisk(strings.ToUpper(aftershock))
	// Range and enum checks are left to the supervisor on purpose: this tool
	// is also used to see how /sensor rejects bad input.
	return &o, nil
}

// send POSTs the payload and returns the status and raw reply body.
func send(client *http.Client, o *options) (int, []byte, error) {
	body, err := json.Marshal(o.payload)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Post(strings.TrimRight(o.host, "/")+"/sensor", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, reply, err
}

func main() {
	o, err := parseArgs(os.Args[1:], time.Now(), os.Stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "sensor:", err)
		}
		os.Exit(2)
	}

	pretty, _ := json.MarshalIndent(o.payload, "", "  ")
	fmt.Printf("POST %s/sensor\n%s\n", strings.TrimRight(o.host, "/"), pretty)

	status, reply, err := send(&http.Client{Timeout: 15 * time.Second}, o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensor: request failed: %v (is the supervisor running at %s?)\n", err, o.host)
		os.Exit(1)
	}
	fmt.Printf("HTTP %d\n%s\n", status, bytes.TrimSpace(reply))
	if status < 200 || status > 299 {
		os.Exit(1)
	}
	if status == http.StatusAccepted {
		fmt.Println("Accepted: the pipeline is running, not finished. Follow it with scripts/simulation/wswatch.")
	}
}
