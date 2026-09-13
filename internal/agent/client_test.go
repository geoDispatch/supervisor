package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// decider is a local copy of pipeline.Decider (spec §3.6); the pipeline
// package is not imported.
type decider interface {
	Decide(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error)
}

var _ decider = (*Client)(nil)

const testPhone = "+212600000001"

const validResponse = `{"event_id":"EQ-1","zone":"red","decisions":[{"phone":"+212600000001","zone_confirmed":"red","zone_escalated":false,"action":"rescue_flag","sms_message":"","rescue_priority":1,"confidence":0.9,"reasoning":"r"}],"gov_narrative":"n","request_qos":true,"confidence":0.9}`

func testRequest() models.AgentRequest {
	return models.AgentRequest{
		EventID: "EQ-1", DisasterType: models.Earthquake, Severity: 6.8,
		AftershockRisk: models.AftershockHigh, Zone: models.ZoneRed,
		Devices:       []models.TriagedDevice{{Phone: testPhone, Zone: models.ZoneRed, ReachabilityStatus: models.NotConnected}},
		NetworkStatus: models.NetworkStatus{CongestionLevel: models.CongestionHigh, QoSStatus: models.QoSActive},
	}
}

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/decide", 5*time.Second)
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}
}

func TestDecideOK(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/decide" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type %q", ct)
		}
		var got map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		// nil slices must go out as arrays (the agent requires them).
		if string(got["nearest_shelters"]) != "[]" {
			t.Errorf("nearest_shelters = %s", got["nearest_shelters"])
		}
		reply(http.StatusOK, validResponse)(w, r)
	})
	resp, err := c.Decide(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if resp.EventID != "EQ-1" || len(resp.Decisions) != 1 || resp.Decisions[0].Action != models.ActionRescue {
		t.Fatalf("got %+v", resp)
	}
}

func TestDecideNon2xxIsRedactedSnippet(t *testing.T) {
	body := `{"detail":"device +212600000001 failed"}` + strings.Repeat("y", 1000)
	c := serve(t, reply(http.StatusBadGateway, body))
	_, err := c.Decide(context.Background(), testRequest())
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadGateway {
		t.Fatalf("want *Error with status 502, got %v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, testPhone) || strings.Contains(msg, testPhone[1:]) {
		t.Fatalf("phone leaked: %q", msg)
	}
	if !strings.Contains(msg, "HTTP 502") || !strings.Contains(msg, "+212 6** *** 001") {
		t.Fatalf("unexpected message %q", msg)
	}
	if len(ae.Detail) > errSnippetBytes+len("…") {
		t.Fatalf("snippet is %d bytes", len(ae.Detail))
	}
	if errors.Is(err, ErrInvalidResponse) {
		t.Fatal("an HTTP error is not an invalid response")
	}
}

func TestDecideStrictDecode(t *testing.T) {
	cases := map[string]string{
		"unknown field":   strings.Replace(validResponse, `"confidence":0.9}`, `"confidence":0.9,"shelter_name":"x"}`, 1),
		"nested unknown":  strings.Replace(validResponse, `"reasoning":"r"`, `"reasoning":"r","shelter_name":"x"`, 1),
		"trailing object": validResponse + `{}`,
		"trailing junk":   validResponse + ` x`,
		"null":            `null`,
		"array":           `[` + validResponse + `]`,
		"empty":           ``,
		"wrong type":      strings.Replace(validResponse, `"request_qos":true`, `"request_qos":"yes"`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := serve(t, reply(http.StatusOK, body))
			_, err := c.Decide(context.Background(), testRequest())
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("want ErrInvalidResponse, got %v", err)
			}
		})
	}

	// Trailing whitespace is fine.
	c := serve(t, reply(http.StatusOK, validResponse+"\n  "))
	if _, err := c.Decide(context.Background(), testRequest()); err != nil {
		t.Fatalf("trailing whitespace: %v", err)
	}
}

func TestDecideBodyLimit(t *testing.T) {
	big := strings.Replace(validResponse, `"gov_narrative":"n"`, `"gov_narrative":"`+strings.Repeat("n", maxBodyBytes)+`"`, 1)
	c := serve(t, reply(http.StatusOK, big))
	_, err := c.Decide(context.Background(), testRequest())
	if !errors.Is(err, ErrInvalidResponse) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want body-too-large, got %v", err)
	}
}

// slow answers only after the client gave up (or 5 s). The body is drained
// first: the server notices a closed connection only once it has read the
// request body.
func slow(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-time.After(5 * time.Second):
	}
}

func TestDecideContextTimeout(t *testing.T) {
	c := serve(t, slow)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Decide(ctx, testRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	var ae *Error
	if !errors.As(err, &ae) || !ae.Timeout() {
		t.Fatalf("want Timeout() true, got %v", err)
	}
}

func TestDecideClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(slow))
	defer srv.Close()
	c := NewClient(srv.URL+"/decide", 50*time.Millisecond)
	if _, err := c.Decide(context.Background(), testRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded from AGENT_TIMEOUT_SEC, got %v", err)
	}
}

func TestHealth(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		io.WriteString(w, "ok")
	})
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}

	down := serve(t, reply(http.StatusServiceUnavailable, `{"status":"loading"}`))
	if err := down.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want a 503 error, got %v", err)
	}

	if err := NewClient("not a url", time.Second).Health(context.Background()); err == nil {
		t.Fatal("invalid AGENT_URL must fail")
	}
}

func TestSiblingHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://agent:5000/decide":        "http://agent:5000/health",
		"http://agent:5000/decide/":       "http://agent:5000/health",
		"http://agent:5000":               "http://agent:5000/health",
		"https://h/api/v1/decide?x=1":     "https://h/api/v1/health?x=1",
		"/decide":                         "",
		"mock_agent:5000/decide-no-proto": "",
	} {
		if got := siblingHealthURL(in); got != want {
			t.Errorf("siblingHealthURL(%q) = %q, want %q", in, got, want)
		}
	}
}
