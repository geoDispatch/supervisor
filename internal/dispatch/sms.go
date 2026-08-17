package dispatch

import (
	"context"
	// "log"
	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/database"
	"github.com/geodispatch/supervisor/internal/models"
)

func SendSMS(ctx context.Context, cfg *config.Config, phone, message string) error {
	// In a real app, this calls Africa's Talking API.
	// For mock testing, we just log it to prove the pipeline reached this step.
	// log.Printf("[DISPATCH] Mock SMS sent to %s: %s", phone, message)
	return nil
}

func FlagRescue(ctx context.Context, db *database.DB, decision models.DeviceDecision, eventID string) error {
	// This is safely bypassed in main.go when db == nil, 
	// but we provide a stub so it compiles.
	return nil
}