package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

func GetReachability(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARAReachabilityResponse, error) {
	if cfg.IsReal() {
		return getRealReachability(ctx, cfg, phone)
	}
	return getMockReachability(ctx, cfg, phone)
}

type reachabilityRequest struct {
	Device struct {
		PhoneNumber string `json:"phoneNumber"`
	} `json:"device"`
}

func getRealReachability(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARAReachabilityResponse, error) {
	token, err := fetchClientCredentialsToken(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("reachability auth: %w", err)
	}

	var body reachabilityRequest
	body.Device.PhoneNumber = normalisePhone(phone)
	bodyBytes, _ := json.Marshal(body)

	reqURL := fmt.Sprintf("%s/device-status/v0/retrieve", cfg.NokiaNacBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build reachability request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-rapidapi-key", cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", cfg.NokiaNacHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reachability request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		locTokenCache.mu.Lock()
		locTokenCache.token = ""
		locTokenCache.mu.Unlock()
		return nil, fmt.Errorf("reachability 401 – token invalidated: %s", raw)
	case http.StatusNotFound:
		return nil, fmt.Errorf("reachability 404 – device unknown: %s", raw)
	default:
		return nil, fmt.Errorf("reachability %d: %s", resp.StatusCode, raw)
	}

	var result models.CAMARAReachabilityResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode reachability: %w", err)
	}
	return &result, nil
}

func getMockReachability(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARAReachabilityResponse, error) {
	encodedPhone := url.QueryEscape(phone)
	reqURL := fmt.Sprintf("%s/reachability?phone=%s", cfg.MockNokiaNacBaseURL, encodedPhone)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build mock reachability request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mock reachability: %w", err)
	}
	defer resp.Body.Close()

	var result models.CAMARAReachabilityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode mock reachability: %w", err)
	}
	return &result, nil
}