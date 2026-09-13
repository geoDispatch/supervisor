package main

import (
	"strings"
	"testing"
)

const deviceFrame = `{"v":2,"type":"device_update","event_id":"EQ-1","seq":7,"timestamp":1757700000123,"replay":false,
"payload":{"phone":"+212600000001","latitude":33.5731,"longitude":-7.5898,"location_accuracy_m":500,"zone":"red","distance_km":0,
"reachable":false,"reachability_status":"NOT_CONNECTED","reachability_assumed":false,"stage":"decided","action":"rescue_flag",
"zone_escalated":false,"escalated_zone":null,"rescue_priority":1,"confidence":0.99,"sms_status":"not_requested","sms_sent":false,
"rescue_flag":true,"rescue_status":"recorded"}}`

func TestFormatFrameMasksPhones(t *testing.T) {
	line, err := formatFrame([]byte(deviceFrame), false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(line)
	if strings.Contains(s, "212600000001") {
		t.Fatalf("phone not masked: %s", s)
	}
	for _, want := range []string{`"phone":"+212 6** *** 001"`, `"timestamp":1757700000123`, `"seq":7`, `"escalated_zone":null`, `"confidence":0.99`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, "\n") {
		t.Fatal("frame must be one line")
	}

	raw, err := formatFrame([]byte(deviceFrame), true)
	if err != nil || !strings.Contains(string(raw), `"phone":"+212600000001"`) || strings.Contains(string(raw), "\n") {
		t.Fatalf("raw mode: %s %v", raw, err)
	}
}

func TestFormatFrameRedactsFreeText(t *testing.T) {
	frame := `{"v":2,"type":"error","event_id":"EQ-1","seq":9,"timestamp":1757700000999,"replay":true,
"payload":{"code":"SMS_FAILED","message":"gateway said +36719991001 <busy>","phone":"","fatal":false,"stage":"dispatch"}}`
	line, err := formatFrame([]byte(frame), false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(line)
	if strings.Contains(s, "36719991001") || !strings.Contains(s, "+36 7** *** 001 <busy>") || !strings.Contains(s, `"phone":""`) {
		t.Fatalf("got %s", s)
	}
	if _, err := formatFrame([]byte(`not json +212600000001`), false); err == nil {
		t.Fatal("unparseable frames must be an error, never echoed")
	}
}

func TestCompletes(t *testing.T) {
	done := `{"v":2,"type":"event_complete","event_id":"EQ-1","seq":50,"timestamp":1,"replay":true,"payload":{}}`
	if !completes([]byte(done), "EQ-1") {
		t.Fatal("event_complete of EQ-1 (even replayed) completes")
	}
	if completes([]byte(done), "EQ-2") || completes([]byte(deviceFrame), "EQ-1") {
		t.Fatal("other events / types must not complete")
	}
}

func TestWSURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:8080":       "ws://localhost:8080/ws",
		"https://sup.example/":        "wss://sup.example/ws",
		"ws://localhost:3000/ws":      "ws://localhost:3000/ws",
		"wss://h/custom/ws":           "wss://h/custom/ws",
		"http://localhost:8080?x=1":   "ws://localhost:8080/ws?x=1",
		"ftp://nope":                  "",
		"localhost:8080-without-http": "",
	} {
		got, err := wsURL(in)
		if want == "" {
			if err == nil {
				t.Errorf("wsURL(%q) = %q, want an error", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("wsURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
