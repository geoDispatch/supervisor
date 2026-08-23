package camara

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

type qodSession struct {
	sessionID string
	expiresAt time.Time
	profile   string
	phone     string
}

var (
	qodMu       sync.Mutex
	qodSessions = make(map[string]*qodSession)
)

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

type qodCreateRequest struct {
	Device            qodDevice    `json:"device"`
	ApplicationServer qodAppServer `json:"applicationServer"`
	QosProfile        string       `json:"qosProfile"`
	Duration          int          `json:"duration"`
}

type qodAppServer struct {
	Ipv4Address string `json:"ipv4Address"`
}

type qodCreateResponse struct {
	SessionID string    `json:"sessionId"`
	QosStatus string    `json:"qosStatus"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type qodExtendRequest struct {
	RequestedAdditionalDuration int `json:"requestedAdditionalDuration"`
}

func RequestQoS(ctx context.Context, cfg *config.Config, epicenter models.Coordinates, phone string) (models.NetworkStatus, error) {
	if cfg.IsReal() {
		return requestQoSReal(ctx, cfg, epicenter, phone)
	}
	return requestQoSMock(epicenter, cfg)
}

func UpgradeQoS(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) error {
	if cfg.IsReal() {
		upgradeQoSReal(ctx, cfg, epicenter)
		return nil
	}
	upgradeQoSMock(epicenter, cfg)
	return nil
}

func requestQoSReal(ctx context.Context, cfg *config.Config, epicenter models.Coordinates, phone string) (models.NetworkStatus, error) {
	body := qodCreateRequest{
		Device: qodDevice{
			PhoneNumber: phone,
			IPv4Address: qodIPv4Address{
				PublicAddress:  "233.252.0.2",
				PrivateAddress: "192.0.2.25",
				PublicPort:     80,
			},
		},
		ApplicationServer: qodAppServer{Ipv4Address: cfg.SupervisorPublicSubnet},
		QosProfile:        cfg.QoSProfileInitial,
		Duration:          cfg.QoSSessionDuration,
	}
	bodyBytes, _ := json.Marshal(body)
	reqURL := fmt.Sprintf("%s/qod/v0/sessions", cfg.NokiaNacBaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return models.NetworkStatus{QoSStatus: models.QoSFailed}, fmt.Errorf("build qos request: %w", err)
	}
	rapidAPIHeaders(req, cfg)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.NetworkStatus{QoSStatus: models.QoSFailed}, fmt.Errorf("qos session create: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return models.NetworkStatus{QoSStatus: models.QoSFailed}, fmt.Errorf("qos create %d: %s", resp.StatusCode, raw)
	}

	var qodResp qodCreateResponse
	if err := json.Unmarshal(raw, &qodResp); err != nil {
		return models.NetworkStatus{QoSStatus: models.QoSFailed}, fmt.Errorf("decode qos response: %w", err)
	}

	qodMu.Lock()
	qodSessions[epicenterKey(epicenter)] = &qodSession{
		sessionID: qodResp.SessionID,
		expiresAt: qodResp.ExpiresAt,
		profile:   cfg.QoSProfileInitial,
		phone:     phone,
	}
	qodMu.Unlock()

	return models.NetworkStatus{QoSStatus: models.QoSActive}, nil
}

func upgradeQoSReal(ctx context.Context, cfg *config.Config, epicenter models.Coordinates) {
	key := epicenterKey(epicenter)
	qodMu.Lock()
	session, ok := qodSessions[key]
	qodMu.Unlock()

	if !ok || session.sessionID == "" {
		upgradedCfg := *cfg
		upgradedCfg.QoSProfileInitial = cfg.QoSProfileUpgrade
		if _, err := requestQoSReal(ctx, &upgradedCfg, epicenter, ""); err != nil {
			fmt.Printf("[QoS] upgrade fallback create failed: %v\n", err)
		}
		return
	}

	extendURL := fmt.Sprintf("%s/qod/v0/sessions/%s/extend", cfg.NokiaNacBaseURL, session.sessionID)
	extBody, _ := json.Marshal(qodExtendRequest{RequestedAdditionalDuration: cfg.QoSExtendDuration})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, extendURL, bytes.NewReader(extBody))
	if err != nil {
		fmt.Printf("[QoS] build extend request: %v\n", err)
		return
	}
	rapidAPIHeaders(req, cfg)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("[QoS] extend request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		var updated qodCreateResponse
		if err := json.Unmarshal(raw, &updated); err == nil {
			qodMu.Lock()
			session.expiresAt = updated.ExpiresAt
			qodMu.Unlock()
			fmt.Printf("[QoS] session %s extended until %s\n", session.sessionID, session.expiresAt.Format(time.RFC3339))
		}
	case http.StatusNotFound, http.StatusGone:
		qodMu.Lock()
		phone := session.phone
		delete(qodSessions, key)
		qodMu.Unlock()
		upgradedCfg := *cfg
		upgradedCfg.QoSProfileInitial = cfg.QoSProfileUpgrade
		if _, err := requestQoSReal(ctx, &upgradedCfg, epicenter, phone); err != nil {
			fmt.Printf("[QoS] re-create after expiry failed: %v\n", err)
		}
	default:
		fmt.Printf("[QoS] extend %d: %s\n", resp.StatusCode, raw)
	}
}

func requestQoSMock(epicenter models.Coordinates, cfg *config.Config) (models.NetworkStatus, error) {
	key := epicenterKey(epicenter)
	qodMu.Lock()
	qodSessions[key] = &qodSession{
		sessionID: "mock-session-" + key,
		expiresAt: time.Now().Add(2 * time.Hour),
		profile:   cfg.QoSProfileInitial,
	}
	qodMu.Unlock()
	fmt.Printf("[QoS MOCK] session created for %s profile=%s\n", key, cfg.QoSProfileInitial)
	return models.NetworkStatus{QoSStatus: models.QoSActive}, nil
}

func upgradeQoSMock(epicenter models.Coordinates, cfg *config.Config) {
	key := epicenterKey(epicenter)
	qodMu.Lock()
	if s, ok := qodSessions[key]; ok {
		s.profile = cfg.QoSProfileUpgrade
		s.expiresAt = time.Now().Add(3 * time.Hour)
		fmt.Printf("[QoS MOCK] session %s upgraded to %s\n", s.sessionID, cfg.QoSProfileUpgrade)
	}
	qodMu.Unlock()
}