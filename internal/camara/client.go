package camara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

func GetLocation(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARALocationResponse, error) {
	// URL encode the phone number so the '+' doesn't turn into a space!
	encodedPhone := url.QueryEscape(phone)
	reqURL := fmt.Sprintf("%s/location?phone=%s", cfg.NokiaNacBaseURL, encodedPhone)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var res models.CAMARALocationResponse
	return &res, json.NewDecoder(resp.Body).Decode(&res)
}

func GetReachability(ctx context.Context, cfg *config.Config, phone string) (*models.CAMARAReachabilityResponse, error) {
	encodedPhone := url.QueryEscape(phone)
	reqURL := fmt.Sprintf("%s/reachability?phone=%s", cfg.NokiaNacBaseURL, encodedPhone)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var res models.CAMARAReachabilityResponse
	return &res, json.NewDecoder(resp.Body).Decode(&res)
}

func RequestQoS(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) (models.NetworkStatus, error) {
	reqURL := fmt.Sprintf("%s/qos", cfg.NokiaNacBaseURL)
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.NetworkStatus{}, err
	}
	defer resp.Body.Close()
	
	return models.NetworkStatus{QoSStatus: models.QoSActive}, nil
}

func CheckGeofencing(ctx context.Context, cfg *config.Config, epicenter models.Coordinates, radiusKm float64) error {
	return nil
}

func UpgradeQoS(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) error {
	return nil
}