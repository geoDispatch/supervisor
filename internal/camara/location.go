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
	"sync"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

type tokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

var locTokenCache tokenCache

func fetchClientCredentialsToken(ctx context.Context, cfg *config.Config) (string, error) {
	locTokenCache.mu.Lock()
	defer locTokenCache.mu.Unlock()

	if locTokenCache.token != "" && time.Now().Before(locTokenCache.expiresAt.Add(-30*time.Second)) {
		return locTokenCache.token, nil
	}

	tokenURL := fmt.Sprintf("%s/oauth2/v1/auth/clientcredentials", cfg.NokiaNacBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("x-rapidapi-key", cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", cfg.NokiaNacHost)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response")
	}

	ttl := 3600
	if tokenResp.ExpiresIn > 0 {
		ttl = tokenResp.ExpiresIn
	}
	locTokenCache.token = tokenResp.AccessToken
	locTokenCache.expiresAt = time.Now().Add(time.Duration(ttl) * time.Second)

	return locTokenCache.token, nil
}

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
	token, err := fetchClientCredentialsToken(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("camara auth: %w", err)
	}

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
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-rapidapi-key", cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", cfg.NokiaNacHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("location retrieve: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		locTokenCache.mu.Lock()
		locTokenCache.token = ""
		locTokenCache.mu.Unlock()
		return nil, fmt.Errorf("camara 401 – token invalidated, retry: %s", raw)
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

func normalisePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+") {
		return phone
	}
	return "+" + phone
}