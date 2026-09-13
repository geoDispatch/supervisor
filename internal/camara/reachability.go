package camara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/geodispatch/supervisor/internal/models"
)

type reachabilityRequest struct {
	Device struct {
		PhoneNumber string `json:"phoneNumber"`
	} `json:"device"`
}

// Reachability returns the device's connectivity (CAMARA Device
// Reachability Status). Real mode: POST
// /device-status/device-reachability-status/v1/retrieve. Mock mode:
// GET /reachability?phone=. A 404 wraps ErrNotFound.
func (c *Client) Reachability(ctx context.Context, phone string) (*models.CAMARAReachabilityResponse, error) {
	const op = "reachability"
	var raw []byte
	var err error
	if c.cfg.IsReal() {
		var body reachabilityRequest
		body.Device.PhoneNumber = normalisePhone(phone)
		b, mErr := json.Marshal(body)
		if mErr != nil {
			return nil, &Error{Op: op, Detail: "encode request: " + mErr.Error()}
		}
		raw, err = c.postReal(ctx, op, "/device-status/device-reachability-status/v1/retrieve", b, http.StatusOK)
	} else {
		raw, err = c.getMock(ctx, op, "/reachability", phone)
	}
	if err != nil {
		return nil, err
	}

	var out models.CAMARAReachabilityResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, decodeError(op, http.StatusOK, err)
	}
	// The status feeds device_update.reachable; an unknown or missing value
	// must not silently read as "reachable".
	switch out.ReachabilityStatus {
	case models.ReachableData, models.ReachableSMS, models.NotConnected:
	default:
		return nil, decodeError(op, http.StatusOK, fmt.Errorf("unknown reachabilityStatus %q", out.ReachabilityStatus))
	}
	return &out, nil
}
