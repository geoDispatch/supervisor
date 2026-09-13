package pipeline

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/geodispatch/supervisor/internal/models"
)

// ValidateAgentResponse checks an AI reply against the request it answers
// (spec §3.6). Any violation rejects the whole batch: a partially trusted
// reply could pair a decision with the wrong device. The error names the
// first violation; phones in it are masked.
func ValidateAgentResponse(req models.AgentRequest, resp *models.AgentResponse) error {
	if resp == nil {
		return errors.New("empty response")
	}
	if resp.EventID != req.EventID {
		return fmt.Errorf("event_id %q does not match the request", models.RedactPhones(resp.EventID))
	}
	if resp.Zone != req.Zone {
		return fmt.Errorf("zone %q does not match the request zone %q", resp.Zone, req.Zone)
	}
	if len(resp.Decisions) != len(req.Devices) {
		return fmt.Errorf("%d decisions for %d devices", len(resp.Decisions), len(req.Devices))
	}

	devZone := make(map[string]models.ZoneType, len(req.Devices))
	for _, d := range req.Devices {
		devZone[d.Phone] = d.Zone
	}
	seen := make(map[string]bool, len(resp.Decisions))
	for i, d := range resp.Decisions {
		where := fmt.Sprintf("decision %d (%s)", i, models.MaskPhone(d.Phone))
		zone, known := devZone[d.Phone]
		switch {
		case !known:
			return fmt.Errorf("%s: phone is not in the request", where)
		case seen[d.Phone]:
			return fmt.Errorf("%s: duplicate phone", where)
		}
		seen[d.Phone] = true
		if err := validateDecision(d, zone); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}
	// Equal counts, no unknown and no duplicate phones already imply every
	// request phone is answered; this keeps the rule explicit.
	for _, d := range req.Devices {
		if !seen[d.Phone] {
			return fmt.Errorf("no decision for %s", models.MaskPhone(d.Phone))
		}
	}
	return nil
}

func validateDecision(d models.DeviceDecision, devZone models.ZoneType) error {
	if !models.ValidAction(d.Action) {
		return fmt.Errorf("action %q is not one of sms, rescue_flag, both, none", d.Action)
	}
	if !models.ValidZone(d.ZoneConfirmed) {
		return fmt.Errorf("zone_confirmed %q is not a valid zone", d.ZoneConfirmed)
	}
	if !d.ZoneEscalated && d.ZoneConfirmed != devZone {
		return fmt.Errorf("zone_confirmed %q differs from the device zone %q without zone_escalated", d.ZoneConfirmed, devZone)
	}
	if d.ZoneEscalated && models.ZoneRank(d.ZoneConfirmed) <= models.ZoneRank(devZone) {
		return fmt.Errorf("escalation to %q is not more severe than the device zone %q", d.ZoneConfirmed, devZone)
	}
	if d.RescuePriority < 0 || d.RescuePriority > 10 {
		return fmt.Errorf("rescue_priority %d is outside 0..10", d.RescuePriority)
	}
	rescue := wantsRescue(d.Action)
	if rescue && d.RescuePriority == 0 {
		return fmt.Errorf("action %q needs a rescue_priority of 1..10", d.Action)
	}
	if !rescue && d.RescuePriority > 0 {
		return fmt.Errorf("action %q must have rescue_priority 0", d.Action)
	}
	if wantsSMS(d.Action) && strings.TrimSpace(d.SMSMessage) == "" {
		return fmt.Errorf("action %q has an empty sms_message", d.Action)
	}
	if math.IsNaN(d.Confidence) || math.IsInf(d.Confidence, 0) || d.Confidence < 0 || d.Confidence > 1 {
		return errors.New("confidence is outside 0..1")
	}
	return nil
}

func wantsSMS(a models.ActionType) bool {
	return a == models.ActionSMS || a == models.ActionBoth
}

func wantsRescue(a models.ActionType) bool {
	return a == models.ActionRescue || a == models.ActionBoth
}
