package camara

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// qosSession is the QoS on Demand session held for one epicentre.
type qosSession struct {
	sessionID string // Nokia session id; "" in mock mode or when Nokia answered 409
	expiresAt time.Time
	profile   string
	phone     string
}

func epicenterKey(c models.Coordinates) string {
	return fmt.Sprintf("%.4f,%.4f", c.Lat, c.Lng)
}

type qodDevice struct {
	PhoneNumber string         `json:"phoneNumber"`
	IPv4Address qodIPv4Address `json:"ipv4Address"`
}

type qodIPv4Address struct {
	PublicAddress  string `json:"publicAddress"`
	PrivateAddress string `json:"privateAddress"`
	PublicPort     int    `json:"publicPort"`
}

type qodAppServer struct {
	Ipv4Address string `json:"ipv4Address"`
}

type qodCreateRequest struct {
	Device            qodDevice    `json:"device"`
	ApplicationServer qodAppServer `json:"applicationServer"`
	QosProfile        string       `json:"qosProfile"`
	Duration          int          `json:"duration"`
}

type qodCreateResponse struct {
	SessionID string    `json:"sessionId"`
	QosStatus string    `json:"qosStatus"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type qodExtendRequest struct {
	RequestedAdditionalDuration int `json:"requestedAdditionalDuration"`
}

// RequestQoS opens a QoS on Demand session (profile QOS_PROFILE_INITIAL) for
// the epicentre, anchored on phone. Mock mode makes no network call and
// only records the session. On error the returned status is "failed".
func (c *Client) RequestQoS(ctx context.Context, epicenter models.Coordinates, phone string) (models.NetworkStatus, error) {
	if !c.cfg.IsReal() {
		c.storeSession(epicenter, &qosSession{
			sessionID: "mock-session-" + epicenterKey(epicenter),
			expiresAt: time.Now().Add(2 * time.Hour),
			profile:   c.cfg.QoSProfileInitial,
		})
		return models.NetworkStatus{QoSStatus: models.QoSActive}, nil
	}
	if err := c.createRealSession(ctx, epicenter, phone, c.cfg.QoSProfileInitial); err != nil {
		return models.NetworkStatus{QoSStatus: models.QoSFailed}, err
	}
	return models.NetworkStatus{QoSStatus: models.QoSActive}, nil
}

// UpgradeQoS is called when the agent asks for more network priority.
// Mock mode switches the recorded session to QOS_PROFILE_UPGRADE without a
// network call. Real mode extends the Nokia session by
// QOS_EXTEND_DURATION_SEC (QoD v0 cannot change the profile of a live
// session); if Nokia no longer knows the session it is re-created with
// QOS_PROFILE_UPGRADE. Without a session to upgrade it returns an error.
func (c *Client) UpgradeQoS(ctx context.Context, epicenter models.Coordinates) error {
	const op = "qos upgrade"
	key := epicenterKey(epicenter)

	c.qosMu.Lock()
	s, ok := c.qosSessions[key]
	var sess qosSession
	if ok {
		sess = *s
	}
	c.qosMu.Unlock()

	if !c.cfg.IsReal() {
		if !ok {
			return &Error{Op: op, Detail: "no QoS session for this epicentre"}
		}
		c.storeSession(epicenter, &qosSession{
			sessionID: sess.sessionID,
			expiresAt: time.Now().Add(3 * time.Hour),
			profile:   c.cfg.QoSProfileUpgrade,
		})
		return nil
	}

	if !ok || sess.sessionID == "" {
		return &Error{Op: op, Detail: "no Nokia QoS session id to extend for this epicentre"}
	}

	b, err := json.Marshal(qodExtendRequest{RequestedAdditionalDuration: c.cfg.QoSExtendDuration})
	if err != nil {
		return &Error{Op: op, Detail: "encode request: " + err.Error()}
	}
	raw, err := c.postReal(ctx, op, "/qod/v0/sessions/"+url.PathEscape(sess.sessionID)+"/extend", b, http.StatusOK)
	var ce *Error
	switch {
	case err == nil:
		var updated qodCreateResponse
		if err := json.Unmarshal(raw, &updated); err != nil {
			return decodeError(op, http.StatusOK, err)
		}
		sess.expiresAt = updated.ExpiresAt
		c.storeSession(epicenter, &sess)
		return nil
	case errors.As(err, &ce) && (ce.Status == http.StatusNotFound || ce.Status == http.StatusGone):
		// The session expired upstream: open a new one with the upgrade profile.
		c.qosMu.Lock()
		delete(c.qosSessions, key)
		c.qosMu.Unlock()
		return c.createRealSession(ctx, epicenter, sess.phone, c.cfg.QoSProfileUpgrade)
	default:
		return err
	}
}

// createRealSession POSTs /qod/v0/sessions and records the session. 409
// means Nokia already holds a session for the device: QoS is active, but
// its id is unknown, so it cannot be extended later (same for a 201 whose
// body does not decode).
func (c *Client) createRealSession(ctx context.Context, epicenter models.Coordinates, phone, profile string) error {
	const op = "qos"
	body := qodCreateRequest{
		Device: qodDevice{
			PhoneNumber: normalisePhone(phone),
			// RFC 5737 documentation addresses: the device's IP is not known
			// to the supervisor, and the API requires the field.
			IPv4Address: qodIPv4Address{
				PublicAddress:  "233.252.0.2",
				PrivateAddress: "192.0.2.25",
				PublicPort:     80,
			},
		},
		ApplicationServer: qodAppServer{Ipv4Address: c.cfg.SupervisorPublicSubnet},
		QosProfile:        profile,
		Duration:          c.cfg.QoSSessionDuration,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return &Error{Op: op, Detail: "encode request: " + err.Error()}
	}
	raw, err := c.postReal(ctx, op, "/qod/v0/sessions", b, http.StatusCreated, http.StatusConflict)
	if err != nil {
		return err
	}
	sess := &qosSession{profile: profile, phone: phone}
	var created qodCreateResponse
	if json.Unmarshal(raw, &created) == nil {
		sess.sessionID = created.SessionID
		sess.expiresAt = created.ExpiresAt
	}
	c.storeSession(epicenter, sess)
	return nil
}

func (c *Client) storeSession(epicenter models.Coordinates, s *qosSession) {
	c.qosMu.Lock()
	c.qosSessions[epicenterKey(epicenter)] = s
	c.qosMu.Unlock()
}
