package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/geodispatch/supervisor/config"
	"github.com/geodispatch/supervisor/internal/models"
)

// messenger is a local copy of pipeline.Messenger (spec §3.6); the pipeline
// package is not imported.
type messenger interface {
	Send(ctx context.Context, phone, message string) models.SMSStatus
	Configured() bool
}

var _ messenger = (*SMS)(nil)

func TestSMSNotConfiguredNeverSent(t *testing.T) {
	for _, gw := range []string{"", "africastalking"} {
		s := NewSMS(&config.Config{SMSGateway: gw})
		if s.Configured() {
			t.Fatalf("SMS_GATEWAY=%q: Configured() must be false", gw)
		}
		for _, msg := range []string{"", "Evacuate now"} {
			if got := s.Send(context.Background(), "+212600000001", msg); got != models.SMSStatusNotConfigured {
				t.Fatalf("SMS_GATEWAY=%q: Send = %q, want not_configured", gw, got)
			}
		}
	}
}

type fakeStore struct {
	calls int
	err   error
}

func (f *fakeStore) FlagRescue(ctx context.Context, eventID string, d models.DeviceDecision) error {
	f.calls++
	return f.err
}

func TestFlagRescueDelegates(t *testing.T) {
	ctx := context.Background()
	d := models.DeviceDecision{Phone: "+212600000001", Action: models.ActionBoth, RescuePriority: 1}

	if err := FlagRescue(ctx, nil, "EQ-1", d); !errors.Is(err, ErrNoStore) {
		t.Fatalf("nil store: got %v", err)
	}

	ok := &fakeStore{}
	if err := FlagRescue(ctx, ok, "EQ-1", d); err != nil || ok.calls != 1 {
		t.Fatalf("got err=%v calls=%d", err, ok.calls)
	}

	boom := errors.New("db down")
	failing := &fakeStore{err: boom}
	if err := FlagRescue(ctx, failing, "EQ-1", d); !errors.Is(err, boom) {
		t.Fatalf("store error must be returned, got %v", err)
	}

	none := &fakeStore{}
	d.Action = models.ActionSMS
	if err := FlagRescue(ctx, none, "EQ-1", d); err == nil || none.calls != 0 {
		t.Fatalf("non-rescue action: err=%v calls=%d", err, none.calls)
	}
}
