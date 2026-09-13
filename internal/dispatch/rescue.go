package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/geodispatch/supervisor/internal/models"
)

// RescueStore persists a rescue flag. *database.DB implements it (and so
// does the pipeline's Store).
type RescueStore interface {
	FlagRescue(ctx context.Context, eventID string, d models.DeviceDecision) error
}

// ErrNoStore is returned when there is no database to record the flag in.
var ErrNoStore = errors.New("dispatch: no rescue store configured")

// FlagRescue records a rescue flag for d by delegating to store. It never
// reports success without a write: a nil store or a decision that did not
// ask for rescue is an error.
func FlagRescue(ctx context.Context, store RescueStore, eventID string, d models.DeviceDecision) error {
	if store == nil {
		return ErrNoStore
	}
	if d.Action != models.ActionRescue && d.Action != models.ActionBoth {
		return fmt.Errorf("dispatch: action %q does not request rescue", d.Action)
	}
	return store.FlagRescue(ctx, eventID, d)
}
