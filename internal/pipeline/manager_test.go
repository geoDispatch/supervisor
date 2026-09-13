package pipeline

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/geodispatch/supervisor/internal/models"
)

// blockedHarness keeps every accepted pipeline in PhonesNearEpicenter until
// release is called, so "running" is a stable state in the test.
func blockedHarness() (*harness, func()) {
	h := newHarness()
	h.store.phonesGate = make(chan struct{})
	var once sync.Once
	return h, func() { once.Do(func() { close(h.store.phonesGate) }) }
}

func TestSubmitConcurrentDifferentEvents(t *testing.T) {
	h, release := blockedHarness()
	defer release()
	m := h.manager()

	ids := []string{"EQ-A", "EQ-B"}
	results := make([]SubmitResult, 2)
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-barrier
			results[i] = m.Submit(context.Background(), testInput(ids[i]))
		}(i)
	}
	close(barrier)
	wg.Wait()

	var winner, loser int
	switch {
	case results[0].Status == http.StatusAccepted && results[1].Status == http.StatusConflict:
		winner, loser = 0, 1
	case results[1].Status == http.StatusAccepted && results[0].Status == http.StatusConflict:
		winner, loser = 1, 0
	default:
		t.Fatalf("want exactly one 202 and one 409, got %d and %d", results[0].Status, results[1].Status)
	}
	if got := results[winner].Body; got["status"] != "accepted" || got["event_id"] != ids[winner] || got["contract_version"] != models.ContractVersion {
		t.Errorf("202 body = %v", got)
	}
	if got := results[loser].Body; got["error"] != "pipeline_busy" || got["active_event_id"] != ids[winner] {
		t.Errorf("409 body = %v, want pipeline_busy naming %s", got, ids[winner])
	}
	if n := len(h.store.inserted); n != 1 {
		t.Errorf("InsertEvent succeeded %d times, want 1", n)
	}

	release()
	m.Wait()
	if id, lc, active := m.Status(); id != ids[winner] || active || !lc.Terminal() {
		t.Errorf("Status after run = %q %s %v", id, lc, active)
	}
}

func TestSubmitDuplicateWhileRunningAndAfterCompletion(t *testing.T) {
	h, release := blockedHarness()
	defer release()
	m := h.manager()

	if r := m.Submit(context.Background(), testInput("EQ-1")); r.Status != http.StatusAccepted {
		t.Fatalf("first submit = %d", r.Status)
	}
	r := m.Submit(context.Background(), testInput("EQ-1"))
	if r.Status != http.StatusOK || r.Body["status"] != "duplicate" || r.Body["event_id"] != "EQ-1" || r.Body["lifecycle"] != "running" {
		t.Fatalf("duplicate while running = %d %v", r.Status, r.Body)
	}

	release()
	m.Wait()
	r = m.Submit(context.Background(), testInput("EQ-1"))
	if r.Status != http.StatusOK || r.Body["status"] != "duplicate" || r.Body["lifecycle"] != string(models.LifecycleNoDevices) {
		t.Fatalf("duplicate after completion = %d %v", r.Status, r.Body)
	}
	if n := len(h.pub.ofType(models.TypeEventStart)); n != 1 {
		t.Errorf("%d event_start frames, want 1 (duplicates start nothing)", n)
	}
	if n := len(h.store.inserted); n != 1 {
		t.Errorf("InsertEvent called for duplicates: %d inserts", n)
	}
}

func TestSubmitSameIDDifferentPayload(t *testing.T) {
	h, release := blockedHarness()
	defer release()
	m := h.manager()
	m.Submit(context.Background(), testInput("EQ-1"))

	changed := testInput("EQ-1")
	changed.Severity = 7.1
	for _, phase := range []string{"running", "finished"} {
		r := m.Submit(context.Background(), changed)
		if r.Status != http.StatusConflict || r.Body["error"] != "event_id_conflict" || r.Body["event_id"] != "EQ-1" ||
			r.Body["detail"] != "event_id already used with a different payload" {
			t.Fatalf("%s: got %d %v", phase, r.Status, r.Body)
		}
		release()
		m.Wait()
	}
}

func TestSubmitDifferentEventAfterCompletionStarts(t *testing.T) {
	h := newHarness()
	m := h.manager()
	m.Submit(context.Background(), testInput("EQ-1"))
	m.Wait()
	if r := m.Submit(context.Background(), testInput("EQ-2")); r.Status != http.StatusAccepted {
		t.Fatalf("second event = %d %v", r.Status, r.Body)
	}
	m.Wait()
	if id, _, _ := m.Status(); id != "EQ-2" {
		t.Errorf("held event = %q, want EQ-2", id)
	}
}

func TestSubmitInsertEventOutcomes(t *testing.T) {
	t.Run("row exists", func(t *testing.T) {
		h := newHarness()
		h.store.notInserted = true
		m := h.manager()
		r := m.Submit(context.Background(), testInput("EQ-OLD"))
		if r.Status != http.StatusConflict || r.Body["error"] != "event_id_conflict" || r.Body["detail"] != "event_id already used" {
			t.Fatalf("got %d %v", r.Status, r.Body)
		}
		if len(h.pub.all()) != 0 {
			t.Error("frames published for a rejected event")
		}
		if id, lc, _ := m.Status(); id != "" || lc != models.LifecycleIdle {
			t.Errorf("Status = %q %s, want idle", id, lc)
		}
	})
	t.Run("database error", func(t *testing.T) {
		h := newHarness()
		h.store.insertErr = errors.New("connection refused")
		m := h.manager()
		r := m.Submit(context.Background(), testInput("EQ-1"))
		if r.Status != http.StatusServiceUnavailable || r.Body["error"] != "database_unavailable" {
			t.Fatalf("got %d %v", r.Status, r.Body)
		}
		if len(h.pub.all()) != 0 {
			t.Error("frames published for a rejected event")
		}
	})
}

func TestStatusIdle(t *testing.T) {
	m := newHarness().manager()
	if id, lc, active := m.Status(); id != "" || lc != models.LifecycleIdle || active {
		t.Fatalf("Status = %q %s %v", id, lc, active)
	}
}

func TestShutdownRefusesSubmits(t *testing.T) {
	h := newHarness()
	m := h.manager()
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := m.Submit(context.Background(), testInput("EQ-1")); r.Status != http.StatusServiceUnavailable || r.Body["error"] != "shutting_down" {
		t.Fatalf("got %d %v", r.Status, r.Body)
	}
}

func TestShutdownCancelsRunningPipeline(t *testing.T) {
	h, release := blockedHarness()
	defer release()
	m := h.manager()
	m.Submit(context.Background(), testInput("EQ-1"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want the context deadline (pipeline cancelled)", err)
	}
	c := h.pub.complete(t, "EQ-1")
	if c.Status != models.LifecycleFailed || c.FatalError == nil || c.FatalError.Code != models.ErrInternalError {
		t.Fatalf("event_complete = %+v", c)
	}
	errs := h.pub.errors()
	if len(errs) != 1 || !errs[0].Fatal || errs[0].Stage != models.ErrorStagePipeline {
		t.Fatalf("errors = %+v, want one fatal pipeline error", errs)
	}
}
