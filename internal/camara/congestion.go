package camara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// Placeholder webhook used when CONGESTION_WEBHOOK_URL/TOKEN are unset. The
// Nokia API requires a webhook on every subscription, but the supervisor only
// ever reads the level synchronously, so nothing is delivered to it.
const (
	placeholderWebhookURL   = "http://example.com/notify"
	placeholderWebhookToken = "c8974e592f9fh683d4a3960714"
)

// subscriptionCleanupTimeout bounds the best-effort DELETE of a congestion
// subscription, which also runs after the caller's ctx has ended.
const subscriptionCleanupTimeout = 2 * time.Second

type congestionSubscriptionRequest struct {
	Device struct {
		PhoneNumber string `json:"phoneNumber"`
	} `json:"device"`
	Webhook struct {
		NotificationURL       string `json:"notificationUrl"`
		NotificationAuthToken string `json:"notificationAuthToken"`
	} `json:"webhook"`
	SubscriptionExpireTime string `json:"subscriptionExpireTime"`
}

// Congestion returns the network congestion level around the device.
//
// Mock mode makes no network call and always reports HIGH (the mock CAMARA
// server has no congestion endpoint). Real mode creates a Nokia congestion
// subscription, queries it and deletes it again. On error the level is
// UNKNOWN.
func (c *Client) Congestion(ctx context.Context, epicenter models.Coordinates, phone string) (models.CongestionLevel, error) {
	if !c.cfg.IsReal() {
		return models.CongestionHigh, nil
	}
	subID, err := c.createCongestionSubscription(ctx, phone)
	if err != nil {
		return models.CongestionUnknown, err
	}
	defer c.deleteCongestionSubscription(ctx, subID)
	return c.fetchCongestion(ctx, phone)
}

func (c *Client) congestionBody(phone string) ([]byte, error) {
	var body congestionSubscriptionRequest
	body.Device.PhoneNumber = normalisePhone(phone)
	body.Webhook.NotificationURL = c.cfg.CongestionWebhookURL
	body.Webhook.NotificationAuthToken = c.cfg.CongestionWebhookToken
	if body.Webhook.NotificationURL == "" {
		body.Webhook.NotificationURL = placeholderWebhookURL
	}
	if body.Webhook.NotificationAuthToken == "" {
		body.Webhook.NotificationAuthToken = placeholderWebhookToken
	}
	body.SubscriptionExpireTime = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	return json.Marshal(body)
}

func (c *Client) createCongestionSubscription(ctx context.Context, phone string) (string, error) {
	const op = "congestion"
	b, err := c.congestionBody(phone)
	if err != nil {
		return "", &Error{Op: op, Detail: "encode request: " + err.Error()}
	}
	raw, err := c.postReal(ctx, op, "/congestion-insights/v0/subscriptions", b, http.StatusCreated)
	if err != nil {
		return "", err
	}
	var result struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", decodeError(op, http.StatusCreated, err)
	}
	if result.SubscriptionID == "" {
		return "", decodeError(op, http.StatusCreated, fmt.Errorf("empty subscriptionId"))
	}
	return result.SubscriptionID, nil
}

func (c *Client) fetchCongestion(ctx context.Context, phone string) (models.CongestionLevel, error) {
	const op = "congestion"
	b, err := c.congestionBody(phone)
	if err != nil {
		return models.CongestionUnknown, &Error{Op: op, Detail: "encode request: " + err.Error()}
	}
	raw, err := c.postReal(ctx, op, "/congestion-insights/v0/query", b, http.StatusOK)
	if err != nil {
		return models.CongestionUnknown, err
	}

	// The API answers with either a list of insights (newest first) or a
	// single object.
	var level models.CongestionLevel
	var list []models.CAMARACongestionResponse
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(list) == 0 {
			return models.CongestionUnknown, decodeError(op, http.StatusOK, fmt.Errorf("empty congestion list"))
		}
		level = list[0].Level
	} else {
		var one models.CAMARACongestionResponse
		if err := json.Unmarshal(raw, &one); err != nil {
			return models.CongestionUnknown, decodeError(op, http.StatusOK, err)
		}
		level = one.Level
	}
	// event_context only allows the contract's levels.
	switch level {
	case models.CongestionLow, models.CongestionMedium, models.CongestionHigh, models.CongestionCritical:
		return level, nil
	}
	return models.CongestionUnknown, decodeError(op, http.StatusOK, fmt.Errorf("unknown congestion level %q", level))
}

// deleteCongestionSubscription is best effort: the subscription expires by
// itself after five minutes. It gets its own short deadline so it still runs
// when ctx has already been cancelled.
func (c *Client) deleteCongestionSubscription(ctx context.Context, subID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subscriptionCleanupTimeout)
	defer cancel()
	u := c.cfg.NokiaNacBaseURL + "/congestion-insights/v0/subscriptions/" + url.PathEscape(subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return
	}
	c.rapidAPIHeaders(req)
	_, _ = c.do(req, "congestion cleanup", http.StatusOK, http.StatusAccepted, http.StatusNoContent, http.StatusNotFound)
}
