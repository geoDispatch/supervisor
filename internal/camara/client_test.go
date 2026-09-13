package camara

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

// network is a local copy of pipeline.Network (spec §3.6). The pipeline
// package is not imported so this package's tests do not depend on it.
type network interface {
	Location(ctx context.Context, phone string) (*models.CAMARALocationResponse, error)
	Reachability(ctx context.Context, phone string) (*models.CAMARAReachabilityResponse, error)
	RequestQoS(ctx context.Context, epicenter models.Coordinates, phone string) (models.NetworkStatus, error)
	UpgradeQoS(ctx context.Context, epicenter models.Coordinates) error
	Congestion(ctx context.Context, epicenter models.Coordinates, phone string) (models.CongestionLevel, error)
	Source() string
}

var _ network = (*Client)(nil)

const testPhone = "+212600000001"

func mockClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(&config.Config{MockNokiaNacBaseURL: srv.URL, CamaraTimeout: 5 * time.Second})
}

// assertNoPhone fails when err's message contains the raw test phone or its
// bare digits (the mock URL carries the phone percent-encoded).
func assertNoPhone(t *testing.T, err error) {
	t.Helper()
	msg := err.Error()
	if strings.Contains(msg, testPhone) || strings.Contains(msg, testPhone[1:]) {
		t.Fatalf("error leaks the phone number: %q", msg)
	}
}

func TestLocationOK(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/location" || r.URL.Query().Get("phone") != testPhone {
			t.Errorf("unexpected request %s", r.URL)
		}
		io.WriteString(w, `{"lastLocationTime":"2026-08-18T10:00:00Z","area":{"areaType":"CIRCLE","center":{"latitude":33.5731,"longitude":-7.5898},"radius":500}}`)
	})
	loc, err := c.Location(context.Background(), testPhone)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Area.Center.Lat != 33.5731 || loc.Area.Center.Lng != -7.5898 || loc.Area.Radius != 500 {
		t.Fatalf("got %+v", loc)
	}
}

func TestLocationRejectsIncompleteArea(t *testing.T) {
	for name, body := range map[string]string{
		"no center":   `{"area":{"areaType":"CIRCLE","radius":500}}`,
		"null center": `{"area":{"areaType":"CIRCLE","center":null,"radius":500}}`,
		"polygon":     `{"area":{"areaType":"POLYGON","center":{"latitude":1,"longitude":1},"radius":5}}`,
		"bad lat":     `{"area":{"areaType":"CIRCLE","center":{"latitude":91,"longitude":1},"radius":5}}`,
		"neg radius":  `{"area":{"areaType":"CIRCLE","center":{"latitude":1,"longitude":1},"radius":-1}}`,
		"not json":    `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
			if _, err := c.Location(context.Background(), testPhone); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNotFoundWrapsErrNotFound(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"status":404,"code":"NOT_FOUND","message":"device +212600000001 not found"}`)
	})
	_, err := c.Location(context.Background(), testPhone)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Location: want ErrNotFound, got %v", err)
	}
	assertNoPhone(t, err)
	_, err = c.Reachability(context.Background(), testPhone)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reachability: want ErrNotFound, got %v", err)
	}
	assertNoPhone(t, err)
}

func TestServerErrorIsRedacted(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "boom for +212600000001 and 212600000001 "+strings.Repeat("x", 2000))
	})
	_, err := c.Location(context.Background(), testPhone)
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != http.StatusInternalServerError {
		t.Fatalf("want *Error with status 500, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("500 must not wrap ErrNotFound")
	}
	assertNoPhone(t, err)
	if !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom for +212 6** *** 001") {
		t.Fatalf("unexpected message %q", err)
	}
	if len(ce.Detail) > errSnippetBytes+len("…") {
		t.Fatalf("snippet too long: %d bytes", len(ce.Detail))
	}
}

func TestAuthFailureBodyNeverEchoed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-rapidapi-key") != "secret-key" {
			t.Errorf("missing api key header")
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"invalid key secret-key"}`)
	}))
	defer srv.Close()
	c := NewClient(&config.Config{NokiaNacBaseURL: srv.URL, NokiaNacAPIKey: "secret-key", CamaraTimeout: time.Second})
	_, err := c.Location(context.Background(), testPhone)
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("want an error without the key, got %v", err)
	}
}

func TestOversizeBodyRejected(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"lastLocationTime":"`)
		io.WriteString(w, strings.Repeat("a", maxBodyBytes))
		io.WriteString(w, `"}`)
	})
	_, err := c.Location(context.Background(), testPhone)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want body-too-large error, got %v", err)
	}
}

// slowHandler answers only after the client gave up (or 5 s).
func slowHandler(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(5 * time.Second):
	}
}

func TestContextTimeout(t *testing.T) {
	c := mockClient(t, slowHandler)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Reachability(ctx, testPhone)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	var ce *Error
	if !errors.As(err, &ce) || !ce.Timeout() {
		t.Fatalf("want *Error with Timeout() true, got %v", err)
	}
	assertNoPhone(t, err)
}

func TestHTTPClientTimeoutIsDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(slowHandler))
	defer srv.Close()
	c := NewClient(&config.Config{MockNokiaNacBaseURL: srv.URL, CamaraTimeout: 50 * time.Millisecond})
	_, err := c.Location(context.Background(), testPhone)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded from CAMARA_TIMEOUT_MS, got %v", err)
	}
	assertNoPhone(t, err)
}

func TestCanceled(t *testing.T) {
	c := mockClient(t, slowHandler)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Location(ctx, testPhone)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled, got %v", err)
	}
}

func TestReachability(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reachability" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"lastStatusTime":"2026-08-18T10:00:00Z","reachabilityStatus":"CONNECTED_SMS"}`)
	})
	got, err := c.Reachability(context.Background(), testPhone)
	if err != nil || got.ReachabilityStatus != models.ReachableSMS {
		t.Fatalf("got %+v, %v", got, err)
	}

	bad := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"reachabilityStatus":"MAYBE"}`)
	})
	if _, err := bad.Reachability(context.Background(), testPhone); err == nil {
		t.Fatal("unknown reachabilityStatus must be an error")
	}
}

func TestConnectionRefusedIsRedacted(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens there any more
	c := NewClient(&config.Config{MockNokiaNacBaseURL: base, CamaraTimeout: time.Second})
	_, err := c.Location(context.Background(), testPhone)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("transport failure must not be ErrNotFound")
	}
	assertNoPhone(t, err)
}

func TestHealthAndMode(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, `{"status":"ok"}`)
	})
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Mode() != ModeMock || c.Source() != models.NetworkSourceMockCAMARA {
		t.Fatalf("mode %q source %q", c.Mode(), c.Source())
	}

	down := mockClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	if err := down.Health(context.Background()); err == nil {
		t.Fatal("503 must fail the health check")
	}

	nac := NewClient(&config.Config{NokiaNacAPIKey: "k", NokiaNacBaseURL: "http://127.0.0.1:1"})
	if err := nac.Health(context.Background()); err != nil {
		t.Fatalf("real mode is not probed, got %v", err)
	}
	if nac.Mode() != ModeReal || nac.Source() != models.NetworkSourceNokiaNAC {
		t.Fatalf("mode %q source %q", nac.Mode(), nac.Source())
	}
}

// Mock congestion and QoS never touch the network.
func TestMockCongestionAndQoSWithoutNetwork(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network call %s", r.URL)
	})
	ctx := context.Background()
	epi := models.Coordinates{Lat: 47.4979, Lng: 19.0402}

	if err := c.UpgradeQoS(ctx, epi); err == nil {
		t.Fatal("upgrading without a session must fail")
	}
	lvl, err := c.Congestion(ctx, epi, testPhone)
	if err != nil || lvl != models.CongestionHigh {
		t.Fatalf("congestion %q %v", lvl, err)
	}
	st, err := c.RequestQoS(ctx, epi, testPhone)
	if err != nil || st.QoSStatus != models.QoSActive {
		t.Fatalf("qos %+v %v", st, err)
	}
	if err := c.UpgradeQoS(ctx, epi); err != nil {
		t.Fatal(err)
	}
}
