package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

// ── shared HTTP helpers ───────────────────────────────────────

func rapidAPIHeaders(req *http.Request, cfg *config.Config) {
	req.Header.Set("x-rapidapi-key", cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", cfg.NokiaNacHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

func normalisePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+") {
		return phone
	}
	return "+" + phone
}

// ── Location ─────────────────────────────────────────────────

func GetLocation(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARALocationResponse, error) {
	if cfg.IsReal() {
		return getRealLocation(ctx, cfg, phone)
	}
	return getMockLocation(ctx, cfg, phone)
}

type locationRetrieveRequest struct {
	Device struct {
		PhoneNumber string `json:"phoneNumber"`
	} `json:"device"`
	MaxAge int `json:"maxAge"`
}

func getRealLocation(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARALocationResponse, error) {
	var body locationRetrieveRequest
	body.Device.PhoneNumber = normalisePhone(phone)
	body.MaxAge = cfg.CAMARALocationMaxAgeSec

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal location request: %w", err)
	}

	reqURL := fmt.Sprintf("%s/location-retrieval/v0/retrieve", cfg.NokiaNacBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build retrieve request: %w", err)
	}
	rapidAPIHeaders(req, cfg)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("location retrieve: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("camara 401 – invalid API key: %s", raw)
	case http.StatusNotFound:
		return nil, fmt.Errorf("camara 404 – device not locatable: %s", raw)
	default:
		return nil, fmt.Errorf("camara %d: %s", resp.StatusCode, raw)
	}

	var result models.CAMARALocationResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode location response: %w", err)
	}
	return &result, nil
}

func getMockLocation(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARALocationResponse, error) {
	encodedPhone := url.QueryEscape(phone)
	reqURL := fmt.Sprintf("%s/location?phone=%s", cfg.MockNokiaNacBaseURL, encodedPhone)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build mock request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mock location: %w", err)
	}
	defer resp.Body.Close()

	var result models.CAMARALocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode mock location: %w", err)
	}
	return &result, nil
}