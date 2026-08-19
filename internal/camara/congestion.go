package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

type congestionRequest struct {
	Area struct {
		Center struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"center"`
		Radius int `json:"radius"`
	} `json:"area"`
}

func GetCongestion(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) (models.CongestionLevel, error) {
	if cfg.IsReal() {
		return getRealCongestion(ctx, cfg, epicenter)
	}
	return getMockCongestion()
}

func getRealCongestion(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) (models.CongestionLevel, error) {
	token, err := fetchClientCredentialsToken(ctx, cfg)
	if err != nil {
		return models.CongestionUnknown, fmt.Errorf("congestion auth: %w", err)
	}

	var body congestionRequest
	body.Area.Center.Latitude = epicenter.Lat
	body.Area.Center.Longitude = epicenter.Lng
	body.Area.Radius = 1000

	bodyBytes, _ := json.Marshal(body)

	reqURL := fmt.Sprintf("%s/congestion-insights/v0/retrieve", cfg.NokiaNacBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return models.CongestionUnknown, fmt.Errorf("build congestion request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-rapidapi-key", cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", cfg.NokiaNacHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.CongestionUnknown, fmt.Errorf("congestion request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return models.CongestionUnknown, fmt.Errorf("congestion %d: %s", resp.StatusCode, raw)
	}

	var result models.CAMARACongestionResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return models.CongestionUnknown, fmt.Errorf("decode congestion: %w", err)
	}

	return result.Level, nil
}

func getMockCongestion() (models.CongestionLevel, error) {
	return models.CongestionHigh, nil
}