// Package camara talks to the network: Nokia Network as Code (CAMARA APIs)
// when NOKIA_NAC_API_KEY is set, otherwise the development mock in
// scripts/camara. The pipeline consumes *Client through its Network
// interface.
package camara

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

// Modes reported by Mode (and by the /health camara check).
const (
	ModeMock = "mock"
	ModeReal = "real"
)

// maxBodyBytes caps every response body read. A larger body is an error,
// never silently truncated JSON.
const maxBodyBytes = 1 << 20

// errSnippetBytes caps the (redacted) upstream body quoted in an error.
const errSnippetBytes = 256

// ErrNotFound is wrapped by every error caused by an HTTP 404 (the device is
// unknown to the network or the mock has no fixture for it).
var ErrNotFound = errors.New("device not found")

// Error is returned by every Client call that fails. Its message never
// contains a raw phone number or an unredacted upstream body.
//
// errors.Is works for ErrNotFound (HTTP 404), context.DeadlineExceeded (any
// timeout, including the http.Client one) and context.Canceled.
type Error struct {
	Op     string // "location", "reachability", "congestion", "qos", "qos upgrade", "health"
	Status int    // HTTP status; 0 when no response was received
	Detail string // already redacted
	cause  error  // ErrNotFound, context.DeadlineExceeded, context.Canceled or nil
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("camara ")
	b.WriteString(e.Op)
	b.WriteString(": ")
	if e.Status != 0 {
		fmt.Fprintf(&b, "HTTP %d: ", e.Status)
	}
	b.WriteString(e.Detail)
	return b.String()
}

func (e *Error) Unwrap() error { return e.cause }

// Timeout reports whether the call failed because a deadline passed.
func (e *Error) Timeout() bool { return errors.Is(e.cause, context.DeadlineExceeded) }

// Client is safe for concurrent use. Build it once with NewClient.
type Client struct {
	cfg  *config.Config
	http *http.Client // ONE client, Timeout = CAMARA_TIMEOUT_MS

	// QoS sessions per epicentre. Real mode keeps the Nokia session id so
	// UpgradeQoS can extend it; mock mode only records the profile.
	qosMu       sync.Mutex
	qosSessions map[string]*qosSession
}

// NewClient builds the client for cfg. cfg must not be mutated afterwards.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg:         cfg,
		http:        &http.Client{Timeout: cfg.CamaraTimeout},
		qosSessions: make(map[string]*qosSession),
	}
}

// Mode is "real" (Nokia NaC) or "mock" (scripts/camara).
func (c *Client) Mode() string {
	if c.cfg.IsReal() {
		return ModeReal
	}
	return ModeMock
}

// Source is the network source this configuration DECLARES ("nokia_nac" or
// "mock_camara"). Nothing verifies it.
func (c *Client) Source() string {
	return c.cfg.NetworkSource()
}

// Health probes GET <MOCK_NOKIA_NAC_BASE_URL>/health in mock mode (2xx = ok).
// Real mode is deliberately not probed — every Nokia call is billed and
// rate-limited — so Health returns nil there; callers report it as
// "checked": false using Mode.
func (c *Client) Health(ctx context.Context) error {
	if c.cfg.IsReal() {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.MockNokiaNacBaseURL+"/health", nil)
	if err != nil {
		return &Error{Op: "health", Detail: "invalid MOCK_NOKIA_NAC_BASE_URL"}
	}
	_, err = c.do(req, "health", http.StatusOK)
	return err
}

// do sends req and returns the body of a response whose status is one of
// want. Every failure is an *Error: 404 wraps ErrNotFound, other statuses
// quote at most errSnippetBytes of the redacted body.
func (c *Client) do(req *http.Request, op string, want ...int) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(op, err)
	}
	defer resp.Body.Close()

	raw, err := readLimited(resp.Body)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			return nil, &Error{Op: op, Status: resp.StatusCode, Detail: err.Error()}
		}
		return nil, transportError(op, err)
	}

	for _, w := range want {
		if resp.StatusCode == w {
			return raw, nil
		}
	}
	e := &Error{Op: op, Status: resp.StatusCode, Detail: snippet(raw)}
	switch resp.StatusCode {
	case http.StatusNotFound:
		e.Detail, e.cause = ErrNotFound.Error(), ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		// Never echo an auth failure body: it may repeat the key.
		e.Detail = "not authorised (check NOKIA_NAC_API_KEY)"
	case http.StatusTooManyRequests:
		e.Detail = "rate limited"
	}
	return nil, e
}

var errBodyTooLarge = fmt.Errorf("response body exceeds %d bytes", maxBodyBytes)

// readLimited reads at most maxBodyBytes and fails if there is more.
func readLimited(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBodyBytes {
		return nil, errBodyTooLarge
	}
	return raw, nil
}

// transportError classifies a failure to get (or read) a response. The URL
// is dropped from *url.Error — mock URLs carry the phone in the query.
func transportError(op string, err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return &Error{Op: op, Detail: "timed out", cause: context.DeadlineExceeded}
	case errors.Is(err, context.Canceled):
		return &Error{Op: op, Detail: "canceled", cause: context.Canceled}
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	return &Error{Op: op, Detail: "request failed: " + models.RedactPhones(err.Error())}
}

// snippet is the upstream body for an error message: phones redacted
// BEFORE cutting (a cut could leave a partial number too short to be
// recognised), whitespace collapsed, at most errSnippetBytes.
func snippet(raw []byte) string {
	s := strings.Join(strings.Fields(models.RedactPhones(string(raw))), " ")
	if s == "" {
		return "empty body"
	}
	if len(s) > errSnippetBytes {
		cut := errSnippetBytes
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// decodeError reports a 2xx body that is not the expected JSON. The decoder
// message names types and offsets, never values, but is redacted anyway.
func decodeError(op string, status int, err error) error {
	return &Error{Op: op, Status: status, Detail: "invalid response: " + models.RedactPhones(err.Error())}
}

// rapidAPIHeaders sets the Nokia NaC (RapidAPI) headers on a real-mode request.
func (c *Client) rapidAPIHeaders(req *http.Request) {
	req.Header.Set("x-rapidapi-key", c.cfg.NokiaNacAPIKey)
	req.Header.Set("x-rapidapi-host", c.cfg.NokiaNacHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// postReal POSTs body (already JSON) to the Nokia NaC path.
func (c *Client) postReal(ctx context.Context, op, path string, body []byte, want ...int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.NokiaNacBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Op: op, Detail: "invalid NOKIA_NAC_BASE_URL"}
	}
	c.rapidAPIHeaders(req)
	return c.do(req, op, want...)
}

// getMock GETs <MOCK_NOKIA_NAC_BASE_URL><path>?phone=<phone>.
func (c *Client) getMock(ctx context.Context, op, path, phone string) ([]byte, error) {
	u := c.cfg.MockNokiaNacBaseURL + path + "?phone=" + url.QueryEscape(phone)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{Op: op, Detail: "invalid MOCK_NOKIA_NAC_BASE_URL"}
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, op, http.StatusOK)
}

func normalisePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+") {
		return phone
	}
	return "+" + phone
}
