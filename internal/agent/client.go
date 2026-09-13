// Package agent calls the Python AI agent (POST /decide, GET /health). The
// pipeline consumes *Client through its Decider interface and validates the
// decisions itself (pipeline.ValidateAgentResponse).
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// maxBodyBytes caps the response body read. A larger body is an error,
// never silently truncated JSON.
const maxBodyBytes = 1 << 20

// errSnippetBytes caps the (redacted) body quoted in a non-2xx error.
const errSnippetBytes = 256

// ErrInvalidResponse is wrapped when a 2xx reply is not exactly one
// AgentResponse object (bad JSON, unknown field, trailing data, too large).
// The HTTP exchange worked but the content is unusable.
var ErrInvalidResponse = errors.New("invalid agent response")

// Error is returned by every failed call. Its message never contains a raw
// phone number: upstream text goes through models.RedactPhones.
//
// errors.Is works for ErrInvalidResponse, context.DeadlineExceeded (any
// timeout, including the http.Client one) and context.Canceled.
type Error struct {
	Op     string // "decide" or "health"
	Status int    // HTTP status; 0 when no response was received
	Detail string // already redacted
	cause  error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("agent ")
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

// Client is safe for concurrent use.
type Client struct {
	url       string
	healthURL string // "" when url is not an absolute URL
	http      *http.Client
}

// NewClient builds a client for the agent's /decide URL. timeout bounds
// every HTTP exchange (AGENT_TIMEOUT_SEC); callers may also pass a shorter
// ctx deadline.
func NewClient(decideURL string, timeout time.Duration) *Client {
	return &Client{
		url:       decideURL,
		healthURL: siblingHealthURL(decideURL),
		http:      &http.Client{Timeout: timeout},
	}
}

// Decide POSTs one zone batch and strictly decodes the reply: exactly one
// JSON object, no unknown fields, nothing after it, at most 1 MiB. It does
// not check the decisions against the request — that is the pipeline's job.
func (c *Client) Decide(ctx context.Context, req models.AgentRequest) (*models.AgentResponse, error) {
	const op = "decide"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &Error{Op: op, Detail: "encode request: " + models.RedactPhones(err.Error())}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Op: op, Detail: "invalid AGENT_URL"}
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")

	status, raw, err := c.do(hreq, op)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, &Error{Op: op, Status: status, Detail: snippet(raw)}
	}
	out, err := decodeStrict(raw)
	if err != nil {
		return nil, &Error{Op: op, Status: status, Detail: models.RedactPhones(err.Error()), cause: ErrInvalidResponse}
	}
	return out, nil
}

// Health GETs the sibling /health of the /decide URL (".../decide" →
// ".../health"). Any 2xx is healthy; the body is ignored.
func (c *Client) Health(ctx context.Context) error {
	const op = "health"
	if c.healthURL == "" {
		return &Error{Op: op, Detail: "invalid AGENT_URL"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.healthURL, nil)
	if err != nil {
		return &Error{Op: op, Detail: "invalid AGENT_URL"}
	}
	status, raw, err := c.do(req, op)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return &Error{Op: op, Status: status, Detail: snippet(raw)}
	}
	return nil
}

// do sends req and reads at most maxBodyBytes of the reply, whatever its
// status.
func (c *Client) do(req *http.Request, op string) (int, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, transportError(op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return 0, nil, transportError(op, err)
	}
	if len(raw) > maxBodyBytes {
		return 0, nil, &Error{Op: op, Status: resp.StatusCode,
			Detail: fmt.Sprintf("response body exceeds %d bytes", maxBodyBytes), cause: ErrInvalidResponse}
	}
	return resp.StatusCode, raw, nil
}

// decodeStrict accepts exactly one AgentResponse object. json.Decoder alone
// would accept `null` (leaving a zero response) and ignore trailing data.
func decodeStrict(raw []byte) (*models.AgentResponse, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, errors.New("empty body")
	}
	if trimmed[0] != '{' {
		return nil, errors.New("body is not a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var out models.AgentResponse
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON object")
	}
	return &out, nil
}

// transportError classifies a failure to get or read a response. The URL
// is dropped from *url.Error (it is configuration, not useful in the event
// log).
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

// snippet quotes a non-2xx body: phones redacted BEFORE cutting (a cut could
// leave a partial number too short to be recognised), whitespace collapsed,
// at most errSnippetBytes.
func snippet(raw []byte) string {
	s := strings.Join(strings.Fields(models.RedactPhones(string(raw))), " ")
	if s == "" {
		return "empty body"
	}
	if len(s) > errSnippetBytes {
		cut := errSnippetBytes
		for cut > 0 && s[cut]&0xC0 == 0x80 { // do not split a UTF-8 sequence
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// siblingHealthURL replaces the last path segment of an absolute URL with
// "health" (".../decide" → ".../health"; no path → "/health"), matching
// config.AgentHealthURL. It returns "" for anything that is not absolute.
func siblingHealthURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	dir := path.Dir(strings.TrimSuffix(u.Path, "/"))
	if dir == "." {
		dir = "/"
	}
	u.Path = path.Join(dir, "health")
	u.RawPath = ""
	return u.String()
}
