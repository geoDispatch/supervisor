// Command wswatch is a DEVELOPMENT TOOL: it connects to the supervisor's
// /ws stream and prints every frame as one JSON line. Phone numbers are
// masked (models.MaskPhone) unless -raw is given.
//
//	go run ./scripts/simulation/wswatch -until-complete EQ-1 -timeout 2m
//
// Exit status: 0 when the -until-complete event's event_complete arrives
// (a replayed one counts: the event already finished), or when -timeout
// passes without -until-complete; 1 on connection errors, on an unexpected
// close, or when -timeout passes before the awaited event_complete.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/geodispatch/supervisor/internal/models"
)

// wsURL turns the -host value into the /ws URL: http→ws, https→wss; a
// ws:// or wss:// URL is used as given; an empty path becomes /ws.
func wsURL(host string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(host))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid -host %q", host)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("invalid -host %q: scheme must be http, https, ws or wss", host)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	return u.String(), nil
}

// formatFrame returns the frame as one compact JSON line. Unless raw, every
// "phone" field is masked and every other string goes through
// models.RedactPhones. Numbers are kept exactly (timestamps are 13 digits
// and must not be mistaken for phones).
func formatFrame(frame []byte, raw bool) ([]byte, error) {
	if raw {
		var b bytes.Buffer
		if err := json.Compact(&b, frame); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}

	var env models.Envelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return nil, err
	}
	if len(env.Payload) > 0 {
		dec := json.NewDecoder(bytes.NewReader(env.Payload))
		dec.UseNumber()
		var payload any
		if err := dec.Decode(&payload); err != nil {
			return nil, err
		}
		masked, err := marshal(mask(payload, ""))
		if err != nil {
			return nil, err
		}
		env.Payload = masked
	}
	return marshal(env)
}

// mask walks decoded JSON. key is the object key v was found under.
func mask(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			t[k] = mask(child, k)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = mask(child, key)
		}
		return t
	case string:
		if key == "phone" && t != "" {
			return models.MaskPhone(t)
		}
		return models.RedactPhones(t)
	}
	return v
}

// marshal is json.Marshal without HTML escaping and without the trailing
// newline.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// completes reports whether frame is the event_complete of eventID.
func completes(frame []byte, eventID string) bool {
	var env struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
	}
	return json.Unmarshal(frame, &env) == nil && env.Type == models.TypeEventComplete && env.EventID == eventID
}

func main() {
	host := flag.String("host", "http://localhost:8080", "supervisor base URL (http, https, ws or wss)")
	raw := flag.Bool("raw", false, "print frames unmodified (phone numbers NOT masked)")
	until := flag.String("until-complete", "", "exit 0 when this event_id's event_complete arrives")
	timeout := flag.Duration("timeout", 0, "stop after this long (0 = no limit)")
	flag.Parse()
	os.Exit(run(*host, *raw, *until, *timeout))
}

func run(host string, raw bool, until string, timeout time.Duration) int {
	target, err := wsURL(host)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wswatch:", err)
		return 2
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.Dial(target, nil)
	if err != nil {
		if resp != nil {
			fmt.Fprintf(os.Stderr, "wswatch: connect %s: HTTP %d\n", target, resp.StatusCode)
		} else {
			fmt.Fprintf(os.Stderr, "wswatch: connect %s: %v\n", target, err)
		}
		return 1
	}
	defer conn.Close()
	fmt.Fprintf(os.Stderr, "wswatch: connected to %s\n", target)

	if timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(timeout))
	}
	for {
		_, frame, err := conn.ReadMessage()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				if until != "" {
					fmt.Fprintf(os.Stderr, "wswatch: timed out after %s before event_complete of %s\n", timeout, until)
					return 1
				}
				return 0
			}
			fmt.Fprintf(os.Stderr, "wswatch: connection closed: %v\n", err)
			return 1
		}
		line, err := formatFrame(frame, raw)
		if err != nil {
			// Never print an unparseable frame in masked mode: it may hold phones.
			fmt.Fprintf(os.Stderr, "wswatch: unparseable frame (%d bytes): %v\n", len(frame), err)
			continue
		}
		fmt.Println(string(line))
		if until != "" && completes(frame, until) {
			return 0
		}
	}
}
