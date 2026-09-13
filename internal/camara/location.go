package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/geodispatch/supervisor/internal/models"
)

type locationRetrieveRequest struct {
	Device struct {
		PhoneNumber string `json:"phoneNumber"`
	} `json:"device"`
	MaxAge int `json:"maxAge"`
}

// Location returns the device's last known position (CAMARA Location
// Retrieval). Real mode: POST /location-retrieval/v0/retrieve. Mock mode:
// GET /location?phone=. A 404 wraps ErrNotFound.
func (c *Client) Location(ctx context.Context, phone string) (*models.CAMARALocationResponse, error) {
	const op = "location"
	var raw []byte
	var err error
	if c.cfg.IsReal() {
		var body locationRetrieveRequest
		body.Device.PhoneNumber = normalisePhone(phone)
		body.MaxAge = c.cfg.CAMARALocationMaxAgeSec
		b, mErr := json.Marshal(body)
		if mErr != nil {
			return nil, &Error{Op: op, Detail: "encode request: " + mErr.Error()}
		}
		raw, err = c.postReal(ctx, op, "/location-retrieval/v0/retrieve", b, http.StatusOK)
	} else {
		raw, err = c.getMock(ctx, op, "/location", phone)
	}
	if err != nil {
		return nil, err
	}
	return parseLocation(raw)
}

// parseLocation decodes and checks a location body. Only CIRCLE areas are
// representable; a missing centre would otherwise decode as (0, 0) and put
// the device in the Gulf of Guinea without anyone noticing.
func parseLocation(raw []byte) (*models.CAMARALocationResponse, error) {
	const op = "location"
	var probe struct {
		Area struct {
			Center json.RawMessage `json:"center"`
		} `json:"area"`
	}
	var out models.CAMARALocationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, decodeError(op, http.StatusOK, err)
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, decodeError(op, http.StatusOK, err)
	}
	a := out.Area
	switch {
	case a.AreaType != "CIRCLE":
		return nil, decodeError(op, http.StatusOK, fmt.Errorf("unsupported area type %q", a.AreaType))
	case len(probe.Area.Center) == 0 || bytes.Equal(probe.Area.Center, []byte("null")):
		return nil, decodeError(op, http.StatusOK, fmt.Errorf("area has no center"))
	case !inRange(a.Center.Lat, -90, 90) || !inRange(a.Center.Lng, -180, 180):
		return nil, decodeError(op, http.StatusOK, fmt.Errorf("center out of range"))
	case !inRange(a.Radius, 0, math.MaxFloat64):
		return nil, decodeError(op, http.StatusOK, fmt.Errorf("negative or invalid area radius"))
	}
	return &out, nil
}

func inRange(v, lo, hi float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi
}
