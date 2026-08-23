package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

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

func GetCongestion(ctx context.Context, cfg *config.Config, epicenter models.Coordinates, phone string) (models.CongestionLevel, error) {
	if cfg.IsReal() {
		return getRealCongestion(ctx, cfg, epicenter, phone)
	}
	return getMockCongestion()
}

func getRealCongestion(ctx context.Context, cfg *config.Config, epicenter models.Coordinates, phone string) (models.CongestionLevel, error) {
	subID, err := createCongestionSubscription(ctx, cfg, phone)
	if err != nil {
		return models.CongestionUnknown, err
	}
	defer deleteCongestionSubscription(ctx, cfg, subID)
	return fetchCongestion(ctx, cfg, phone)
}

func newCongestionBody(cfg *config.Config, phone string) congestionSubscriptionRequest {
	var body congestionSubscriptionRequest
	body.Device.PhoneNumber = phone
	body.Webhook.NotificationURL = cfg.CongestionWebhookURL
	body.Webhook.NotificationAuthToken = cfg.CongestionWebhookToken
	if body.Webhook.NotificationURL == "" {
		body.Webhook.NotificationURL = "http://example.com/notify"
	}
	if body.Webhook.NotificationAuthToken == "" {
		body.Webhook.NotificationAuthToken = "c8974e592f9fh683d4a3960714"
	}
	body.SubscriptionExpireTime = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	return body
}

func createCongestionSubscription(ctx context.Context, cfg *config.Config, phone string) (string, error) {
	bodyBytes, _ := json.Marshal(newCongestionBody(cfg, phone))

	reqURL := fmt.Sprintf("%s/congestion-insights/v0/subscriptions", cfg.NokiaNacBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("build create subscription request: %w", err)
	}
	rapidAPIHeaders(req, cfg)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create subscription request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create subscription %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode subscription response: %w", err)
	}
	if result.SubscriptionID == "" {
		return "", fmt.Errorf("empty subscriptionId in response: %s", raw)
	}
	return result.SubscriptionID, nil
}

func fetchCongestion(ctx context.Context, cfg *config.Config, phone string) (models.CongestionLevel, error) {
	bodyBytes, _ := json.Marshal(newCongestionBody(cfg, phone))

	reqURL := fmt.Sprintf("%s/congestion-insights/v0/query", cfg.NokiaNacBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return models.CongestionUnknown, fmt.Errorf("build fetch request: %w", err)
	}
	rapidAPIHeaders(req, cfg)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.CongestionUnknown, fmt.Errorf("fetch congestion request: %w", err)
	}
	if resp.StatusCode == 429 {
		return models.CongestionUnknown, nil
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return models.CongestionUnknown, fmt.Errorf("fetch congestion %d: %s", resp.StatusCode, raw)
	}

	var results []models.CAMARACongestionResponse
	if err := json.Unmarshal(raw, &results); err == nil {
		if len(results) == 0 {
			return models.CongestionUnknown, fmt.Errorf("empty congestion response")
		}
		return results[0].Level, nil
	}

	var result models.CAMARACongestionResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return models.CongestionUnknown, fmt.Errorf("decode congestion: %w", err)
	}
	return result.Level, nil
}

func deleteCongestionSubscription(ctx context.Context, cfg *config.Config, subID string) {
	reqURL := fmt.Sprintf("%s/congestion-insights/v0/subscriptions/%s", cfg.NokiaNacBaseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return
	}
	rapidAPIHeaders(req, cfg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp == nil {
		return
	}
	resp.Body.Close()
}

func getMockCongestion() (models.CongestionLevel, error) {
	return models.CongestionHigh, nil
}