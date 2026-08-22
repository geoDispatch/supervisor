package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"net/http"

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

type congestionFetchRequest struct {
	SubscriptionID string `json:"subscriptionId"`
	Area           struct {
		Center struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"center"`
		Radius int `json:"radius"`
	} `json:"area"`
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
	return fetchCongestion(ctx, cfg, subID, epicenter)
}

func createCongestionSubscription(ctx context.Context, cfg *config.Config, phone string) (string, error) {
	var body congestionSubscriptionRequest
	body.Device.PhoneNumber = phone
	body.Webhook.NotificationURL = cfg.CongestionWebhookURL
	body.Webhook.NotificationAuthToken = cfg.CongestionWebhookToken
	body.SubscriptionExpireTime = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)

	bodyBytes, _ := json.Marshal(body)
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

func fetchCongestion(ctx context.Context, cfg *config.Config, subID string, epicenter models.Coordinates) (models.CongestionLevel, error) {
	var body congestionFetchRequest
	body.SubscriptionID = subID
	body.Area.Center.Latitude = epicenter.Lat
	body.Area.Center.Longitude = epicenter.Lng
	body.Area.Radius = 1000

	bodyBytes, _ := json.Marshal(body)
	reqURL := fmt.Sprintf("%s/congestion-insights/v0/fetch", cfg.NokiaNacBaseURL)

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