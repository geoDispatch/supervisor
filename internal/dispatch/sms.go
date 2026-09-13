// Package dispatch carries out the agent's decisions: SMS through a gateway
// (none is supported yet) and rescue flags through the database.
package dispatch

import (
	"context"
	"log"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

// SMS is the pipeline's Messenger. No gateway is implemented: an empty
// SMS_GATEWAY (the only supported value today) means SMS is not configured,
// and Send reports not_configured — it never claims a message was sent.
type SMS struct {
	configured bool
}

// NewSMS builds the Messenger for cfg. A non-empty SMS_GATEWAY names a
// gateway this build cannot drive, so it is logged and treated as not
// configured rather than reported as working.
func NewSMS(cfg *config.Config) *SMS {
	if cfg.SMSGateway != "" {
		log.Printf("[dispatch] WARNING: SMS_GATEWAY=%q is not supported by this supervisor; SMS is NOT configured and nothing will be sent", cfg.SMSGateway)
	}
	return &SMS{configured: false}
}

// Configured reports whether a working SMS gateway is available. Always
// false today.
func (s *SMS) Configured() bool { return s.configured }

// Send delivers message to phone. Without a gateway nothing is sent and the
// result is not_configured.
func (s *SMS) Send(ctx context.Context, phone, message string) models.SMSStatus {
	if !s.configured {
		return models.SMSStatusNotConfigured
	}
	// Unreachable until a gateway is implemented: nothing can be sent, so
	// never report "sent".
	return models.SMSStatusFailed
}
