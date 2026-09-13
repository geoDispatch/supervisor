// Package sensor decodes and validates POST /sensor (contract v2, spec §2.1
// steps 1 and 3–6). The origin check (step 2) belongs to the HTTP layer and
// the single-incident rules (step 7) to the pipeline manager.
package sensor

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/geodispatch/supervisor/internal/models"
)

// Field limits. GET /capabilities publishes them and
// contracts/examples/sensor_input.json mirrors them.
const (
	EventIDMaxLength = 64
	SeverityMin      = 0.0
	SeverityMax      = 10.0
	RadiusKmMax      = 500.0 // the minimum is exclusive: radius_km > 0
	DepthKmMin       = 0.0
	DepthKmMax       = 800.0

	// DefaultMaxBodyBytes applies when Decode is given a limit ≤ 0.
	DefaultMaxBodyBytes int64 = 16384
)

// Reason strings clients match on (the dashboard shows them inline).
const (
	ReasonRequired     = "required"
	ReasonUnknownField = "unknown field"
	ReasonUnsupported  = "unsupported: not implemented by this supervisor"
)

var eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

// Problem is a rejected request: the HTTP status and the JSON body to send.
type Problem struct {
	Status int
	Body   map[string]any
}

// Decode checks the method, media type, size and JSON shape of r and
// validates every SensorInput field. It returns the input, or the Problem to
// reply with. It writes nothing except the Allow header of a 405 (and the
// connection-close hint http.MaxBytesReader sets on an oversized body).
func Decode(w http.ResponseWriter, r *http.Request, maxBytes int64) (*models.SensorInput, *Problem) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		return nil, &Problem{http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"}}
	}

	// Parameters such as charset are fine; the media type itself must match.
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return nil, &Problem{http.StatusUnsupportedMediaType, map[string]any{
			"error":  "unsupported_media_type",
			"detail": "Content-Type must be application/json",
		}}
	}

	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &Problem{http.StatusRequestEntityTooLarge, map[string]any{
				"error":       "payload_too_large",
				"limit_bytes": maxBytes,
			}}
		}
		return nil, invalidJSON("the request body could not be read")
	}

	obj, detail := parseObject(body)
	if detail != "" {
		return nil, invalidJSON(detail)
	}
	in, fields := validate(obj)
	if len(fields) > 0 {
		return nil, &Problem{http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation_failed",
			"fields": fields,
		}}
	}
	return in, nil
}

func invalidJSON(detail string) *Problem {
	return &Problem{http.StatusBadRequest, map[string]any{"error": "invalid_json", "detail": detail}}
}

// parseObject accepts exactly one JSON object followed by nothing but
// whitespace, with no duplicate keys at any depth (encoding/json would
// silently keep the last one, so the payload would be ambiguous). It returns
// the top-level members or a detail for the 400 reply.
func parseObject(body []byte) (map[string]json.RawMessage, string) {
	dec := json.NewDecoder(bytes.NewReader(body))
	var doc json.RawMessage
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "the request body is empty"
		}
		return nil, "malformed JSON: " + err.Error()
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, "unexpected content after the JSON object"
	}
	if doc[0] != '{' { // RawMessage from Decode has no leading whitespace
		return nil, "the body must be a single JSON object"
	}
	if key, dup := duplicateKey(doc); dup {
		return nil, "duplicate key " + strconv.Quote(key)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil {
		return nil, "malformed JSON: " + err.Error()
	}
	return obj, ""
}

// duplicateKey walks a well-formed JSON document and reports the first key
// repeated inside one object.
func duplicateKey(doc []byte) (string, bool) {
	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]struct{}
	}
	var stack []*frame
	valueDone := func() { // a complete value was read inside the top frame
		if n := len(stack); n > 0 && stack[n-1].object {
			stack[n-1].expectKey = true
		}
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false // io.EOF: the document was already validated
		}
		var top *frame
		if n := len(stack); n > 0 {
			top = stack[n-1]
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:len(stack)-1]
			valueDone()
			continue
		}
		if top != nil && top.object && top.expectKey {
			key := tok.(string) // the decoder guarantees a string key here
			if _, seen := top.keys[key]; seen {
				return key, true
			}
			top.keys[key] = struct{}{}
			top.expectKey = false
			continue
		}
		switch tok {
		case json.Delim('{'):
			stack = append(stack, &frame{object: true, expectKey: true, keys: map[string]struct{}{}})
		case json.Delim('['):
			stack = append(stack, &frame{})
		default:
			valueDone()
		}
	}
}

// validate checks every field of the decoded object and returns the input,
// or a map of JSON path → reason. Every field is checked so one reply lists
// every problem.
func validate(obj map[string]json.RawMessage) (*models.SensorInput, map[string]string) {
	fields := map[string]string{}
	in := &models.SensorInput{}

	known := map[string]bool{
		"event_id": true, "disaster_type": true, "timestamp": true, "severity": true,
		"epicenter": true, "radius_km": true, "depth_km": true,
		"aftershock_risk": true, "tsunami_risk": true,
	}
	for k := range obj {
		if !known[k] {
			fields[k] = ReasonUnknownField
		}
	}

	// field runs check on obj[name], or records "required" when it is absent.
	field := func(name string, check func(raw json.RawMessage) string) {
		raw, ok := obj[name]
		if !ok {
			fields[name] = ReasonRequired
			return
		}
		if reason := check(raw); reason != "" {
			fields[name] = reason
		}
	}

	field("event_id", func(raw json.RawMessage) string {
		s, ok := asString(raw)
		switch {
		case !ok:
			return "must be a string"
		case len(s) == 0 || len(s) > EventIDMaxLength:
			return "must be 1.." + strconv.Itoa(EventIDMaxLength) + " characters"
		case !eventIDPattern.MatchString(s):
			return "must start with a letter or digit and contain only letters, digits, '.', '_', ':' or '-'"
		}
		in.EventID = s
		return ""
	})

	field("disaster_type", func(raw json.RawMessage) string {
		s, ok := asString(raw)
		if !ok {
			return "must be a string"
		}
		t := models.DisasterType(s)
		if _, known := models.SupportedDisasterTypes[t]; !known {
			return "must be one of earthquake, flood, heatwave"
		}
		if models.DisasterCapability(t) != models.CapabilityOperational {
			return ReasonUnsupported
		}
		in.DisasterType = t
		return ""
	})

	field("timestamp", func(raw json.RawMessage) string {
		v, ok := asInteger(raw)
		switch {
		case !ok:
			return "must be an integer (Unix milliseconds)"
		case v <= 0:
			return "must be greater than 0 (Unix milliseconds)"
		}
		in.Timestamp = v
		return ""
	})

	field("severity", rangeCheck(&in.Severity, SeverityMin, SeverityMax, "must be between 0 and 10"))
	field("depth_km", rangeCheck(&in.DepthKm, DepthKmMin, DepthKmMax, "must be between 0 and 800"))
	field("radius_km", func(raw json.RawMessage) string {
		v, reason := asFinite(raw)
		switch {
		case reason != "":
			return reason
		case v <= 0 || v > RadiusKmMax:
			return "must be greater than 0 and at most 500"
		}
		in.RadiusKm = v
		return ""
	})

	field("epicenter", func(raw json.RawMessage) string {
		var epi map[string]json.RawMessage
		if raw[0] != '{' || json.Unmarshal(raw, &epi) != nil {
			return "must be an object"
		}
		for k := range epi {
			if k != "latitude" && k != "longitude" {
				fields["epicenter."+k] = ReasonUnknownField
			}
		}
		for _, c := range []struct {
			name     string
			dst      *float64
			min, max float64
			reason   string
		}{
			{"latitude", &in.Epicenter.Lat, -90, 90, "must be between -90 and 90"},
			{"longitude", &in.Epicenter.Lng, -180, 180, "must be between -180 and 180"},
		} {
			v, ok := epi[c.name]
			if !ok {
				fields["epicenter."+c.name] = ReasonRequired
				continue
			}
			if reason := rangeCheck(c.dst, c.min, c.max, c.reason)(v); reason != "" {
				fields["epicenter."+c.name] = reason
			}
		}
		return ""
	})

	field("aftershock_risk", func(raw json.RawMessage) string {
		s, ok := asString(raw)
		if !ok {
			return "must be a string"
		}
		switch r := models.AftershockRisk(s); r {
		case models.AftershockLow, models.AftershockMedium, models.AftershockHigh:
			in.AftershockRisk = r
			return ""
		}
		return "must be one of LOW, MEDIUM, HIGH"
	})

	field("tsunami_risk", func(raw json.RawMessage) string {
		switch string(raw) {
		case "true":
			in.TsunamiRisk = true
		case "false":
			in.TsunamiRisk = false
		default:
			return "must be a boolean"
		}
		return ""
	})

	if len(fields) > 0 {
		return nil, fields
	}
	return in, nil
}

// rangeCheck returns a check for a finite number in [min, max] stored in dst.
func rangeCheck(dst *float64, min, max float64, reason string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		v, bad := asFinite(raw)
		switch {
		case bad != "":
			return bad
		case v < min || v > max:
			return reason
		}
		*dst = v
		return ""
	}
}

// asString decodes a JSON string (null and other types are rejected).
func asString(raw json.RawMessage) (string, bool) {
	if raw[0] != '"' {
		return "", false
	}
	var s string
	return s, json.Unmarshal(raw, &s) == nil
}

// isNumber reports whether raw is a JSON number literal. The members come
// from a validated document, so the first byte decides.
func isNumber(raw json.RawMessage) bool {
	return raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')
}

// asFinite decodes a JSON number that fits a finite float64.
func asFinite(raw json.RawMessage) (float64, string) {
	if !isNumber(raw) {
		return 0, "must be a number"
	}
	v, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, "must be a finite number"
	}
	return v, ""
}

// asInteger decodes a JSON number with an integral value. As in JSON Schema,
// 1757699999000.0 and 1.757699999e12 are integers too, as long as the value
// is exact (|v| ≤ 2^53).
func asInteger(raw json.RawMessage) (int64, bool) {
	if !isNumber(raw) {
		return 0, false
	}
	s := string(raw)
	if !strings.ContainsAny(s, ".eE") {
		v, err := strconv.ParseInt(s, 10, 64)
		return v, err == nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}
