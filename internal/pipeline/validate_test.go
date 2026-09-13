package pipeline

import (
	"math"
	"strings"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

// validPair is an orange-zone request of three devices and a reply that
// passes every rule.
func validPair() (models.AgentRequest, *models.AgentResponse) {
	req := models.AgentRequest{
		EventID: "EQ-1",
		Zone:    models.ZoneOrange,
		Devices: []models.TriagedDevice{
			dev(1, models.ZoneOrange, 5),
			dev(2, models.ZoneOrange, 6),
			dev(3, models.ZoneOrange, 7),
		},
	}
	resp := &models.AgentResponse{
		EventID: "EQ-1",
		Zone:    models.ZoneOrange,
		Decisions: []models.DeviceDecision{
			{Phone: phoneN(1), ZoneConfirmed: models.ZoneOrange, Action: models.ActionRescue, RescuePriority: 2, Confidence: 0.9},
			{Phone: phoneN(2), ZoneConfirmed: models.ZoneOrange, Action: models.ActionSMS, SMSMessage: "Go to shelter", Confidence: 0},
			{Phone: phoneN(3), ZoneConfirmed: models.ZoneRed, ZoneEscalated: true, Action: models.ActionBoth, SMSMessage: "Stay put", RescuePriority: 1, Confidence: 1},
		},
	}
	return req, resp
}

func TestValidateAgentResponseAcceptsValid(t *testing.T) {
	req, resp := validPair()
	if err := ValidateAgentResponse(req, resp); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	resp.Decisions[1].Action = models.ActionNone
	resp.Decisions[1].SMSMessage = ""
	if err := ValidateAgentResponse(req, resp); err != nil {
		t.Fatalf("action none rejected: %v", err)
	}
}

func TestValidateAgentResponseRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*models.AgentResponse)
		want   string // substring of the error
	}{
		{"event_id mismatch", func(r *models.AgentResponse) { r.EventID = "EQ-2" }, "event_id"},
		{"zone mismatch", func(r *models.AgentResponse) { r.Zone = models.ZoneRed }, "zone"},
		{"request phone missing (fewer decisions)", func(r *models.AgentResponse) { r.Decisions = r.Decisions[:2] }, "2 decisions for 3 devices"},
		{"more decisions", func(r *models.AgentResponse) {
			r.Decisions = append(r.Decisions, models.DeviceDecision{Phone: phoneN(9), ZoneConfirmed: models.ZoneOrange, Action: models.ActionNone})
		}, "4 decisions for 3 devices"},
		{"unknown phone", func(r *models.AgentResponse) { r.Decisions[0].Phone = phoneN(99) }, "not in the request"},
		{"duplicate phone", func(r *models.AgentResponse) { r.Decisions[2].Phone = phoneN(1) }, "duplicate phone"},
		{"invalid action", func(r *models.AgentResponse) { r.Decisions[0].Action = "evacuate" }, "action"},
		{"empty action", func(r *models.AgentResponse) { r.Decisions[0].Action = "" }, "action"},
		{"invalid zone_confirmed", func(r *models.AgentResponse) { r.Decisions[0].ZoneConfirmed = "yellow" }, "zone_confirmed"},
		{"zone change without escalation", func(r *models.AgentResponse) { r.Decisions[0].ZoneConfirmed = models.ZoneRed }, "without zone_escalated"},
		{"de-escalation", func(r *models.AgentResponse) {
			r.Decisions[2].ZoneConfirmed = models.ZoneGreen
		}, "not more severe"},
		{"escalation to the same zone", func(r *models.AgentResponse) {
			r.Decisions[2].ZoneConfirmed = models.ZoneOrange
		}, "not more severe"},
		{"priority above 10", func(r *models.AgentResponse) { r.Decisions[0].RescuePriority = 11 }, "outside 0..10"},
		{"negative priority", func(r *models.AgentResponse) { r.Decisions[0].RescuePriority = -1 }, "outside 0..10"},
		{"rescue with priority 0", func(r *models.AgentResponse) { r.Decisions[0].RescuePriority = 0 }, "needs a rescue_priority"},
		{"both with priority 0", func(r *models.AgentResponse) { r.Decisions[2].RescuePriority = 0 }, "needs a rescue_priority"},
		{"sms with priority", func(r *models.AgentResponse) { r.Decisions[1].RescuePriority = 3 }, "must have rescue_priority 0"},
		{"none with priority", func(r *models.AgentResponse) {
			r.Decisions[1].Action = models.ActionNone
			r.Decisions[1].RescuePriority = 1
		}, "must have rescue_priority 0"},
		{"sms without message", func(r *models.AgentResponse) { r.Decisions[1].SMSMessage = "  " }, "empty sms_message"},
		{"both without message", func(r *models.AgentResponse) { r.Decisions[2].SMSMessage = "" }, "empty sms_message"},
		{"confidence above 1", func(r *models.AgentResponse) { r.Decisions[0].Confidence = 1.01 }, "confidence"},
		{"negative confidence", func(r *models.AgentResponse) { r.Decisions[0].Confidence = -0.1 }, "confidence"},
		{"NaN confidence", func(r *models.AgentResponse) { r.Decisions[0].Confidence = math.NaN() }, "confidence"},
		{"infinite confidence", func(r *models.AgentResponse) { r.Decisions[0].Confidence = math.Inf(1) }, "confidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, resp := validPair()
			tc.mutate(resp)
			err := ValidateAgentResponse(req, resp)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "+2126") {
				t.Errorf("error %q contains a raw phone", err)
			}
		})
	}
}

func TestValidateAgentResponseRejectsNil(t *testing.T) {
	req, _ := validPair()
	if err := ValidateAgentResponse(req, nil); err == nil {
		t.Fatal("nil response accepted")
	}
}
