package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/geodispatch/supervisor/internal/models"
)

// store is a local copy of pipeline.Store (spec §3.6); the pipeline package
// is not imported.
type store interface {
	PhonesNearEpicenter(ctx context.Context, epicenter models.Coordinates, radiusKm float64) ([]string, error)
	NearestShelters(ctx context.Context, center models.Coordinates, limit int) ([]models.Shelter, error)
	InsertEvent(ctx context.Context, in *models.SensorInput) (inserted bool, err error)
	InsertDeviceLog(ctx context.Context, eventID string, d models.DeviceDecision) error
	FlagRescue(ctx context.Context, eventID string, d models.DeviceDecision) error
}

var _ store = (*DB)(nil)

// ── a minimal database/sql driver that records Exec calls ────────────────

type execCall struct {
	query string
	args  []driver.NamedValue
}

type fakeDriver struct {
	mu       sync.Mutex
	calls    []execCall
	affected int64
	err      error
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d: d}, nil }

type fakeConn struct{ d *fakeDriver }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	c.d.calls = append(c.d.calls, execCall{query: query, args: args})
	if c.d.err != nil {
		return nil, c.d.err
	}
	return driver.RowsAffected(c.d.affected), nil
}

func newFakeDB(t *testing.T, fd *fakeDriver) *DB {
	t.Helper()
	pool := sql.OpenDB(connector{fd})
	t.Cleanup(func() { pool.Close() })
	return &DB{pool: pool}
}

type connector struct{ d *fakeDriver }

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.d.Open("") }
func (c connector) Driver() driver.Driver                        { return c.d }

// ── tests ─────────────────────────────────────────────────────────────────

func testInput() *models.SensorInput {
	return &models.SensorInput{
		EventID: "EQ-1", DisasterType: models.Earthquake, Timestamp: 1757699999000,
		Severity: 6.8, Epicenter: models.Coordinates{Lat: 33.5731, Lng: -7.5898},
		RadiusKm: 15, DepthKm: 10.5, AftershockRisk: models.AftershockHigh,
	}
}

func TestInsertEventReportsInserted(t *testing.T) {
	ctx := context.Background()

	fresh := &fakeDriver{affected: 1}
	inserted, err := newFakeDB(t, fresh).InsertEvent(ctx, testInput())
	if err != nil || !inserted {
		t.Fatalf("new id: inserted=%v err=%v", inserted, err)
	}
	if q := fresh.calls[0].query; !strings.Contains(q, "ON CONFLICT (id) DO NOTHING") {
		t.Fatalf("query lacks ON CONFLICT (id) DO NOTHING:\n%s", q)
	}

	used := &fakeDriver{affected: 0}
	inserted, err = newFakeDB(t, used).InsertEvent(ctx, testInput())
	if err != nil || inserted {
		t.Fatalf("existing id: inserted=%v err=%v", inserted, err)
	}

	boom := errors.New("connection refused")
	inserted, err = newFakeDB(t, &fakeDriver{err: boom}).InsertEvent(ctx, testInput())
	if !errors.Is(err, boom) || inserted {
		t.Fatalf("db error: inserted=%v err=%v", inserted, err)
	}
}

func TestInsertDeviceLogWritesNullShelter(t *testing.T) {
	fd := &fakeDriver{affected: 1}
	d := models.DeviceDecision{
		Phone: "+212600000001", ZoneConfirmed: models.ZoneRed, Action: models.ActionBoth,
		SMSMessage: "Evacuate", RescuePriority: 1, Confidence: 0.9,
	}
	if err := newFakeDB(t, fd).InsertDeviceLog(context.Background(), "EQ-1", d); err != nil {
		t.Fatal(err)
	}
	call := fd.calls[0]
	if !strings.Contains(call.query, "shelter_name") || !strings.Contains(call.query, "$5, NULL, $6") {
		t.Fatalf("shelter_name must be inserted as NULL:\n%s", call.query)
	}
	if len(call.args) != 8 {
		t.Fatalf("want 8 args, got %d", len(call.args))
	}
}

func TestWriteErrorsMaskPhones(t *testing.T) {
	fd := &fakeDriver{err: errors.New("pq: relation does not exist")}
	db := newFakeDB(t, fd)
	d := models.DeviceDecision{Phone: "+212600000001", Action: models.ActionRescue, RescuePriority: 1}
	for name, err := range map[string]error{
		"InsertDeviceLog": db.InsertDeviceLog(context.Background(), "EQ-1", d),
		"FlagRescue":      db.FlagRescue(context.Background(), "EQ-1", d),
	} {
		if err == nil || strings.Contains(err.Error(), "212600000001") {
			t.Fatalf("%s: want an error without the raw phone, got %v", name, err)
		}
	}
}

func TestNilDBDoesNotPanic(t *testing.T) {
	var db *DB
	ctx := context.Background()
	if _, err := db.InsertEvent(ctx, testInput()); !errors.Is(err, errNoDB) {
		t.Fatalf("InsertEvent: %v", err)
	}
	if err := db.FlagRescue(ctx, "EQ-1", models.DeviceDecision{}); !errors.Is(err, errNoDB) {
		t.Fatalf("FlagRescue: %v", err)
	}
	if _, err := db.PhonesNearEpicenter(ctx, models.Coordinates{}, 1); !errors.Is(err, errNoDB) {
		t.Fatalf("PhonesNearEpicenter: %v", err)
	}
	if err := db.HealthCheck(ctx); !errors.Is(err, errNoDB) {
		t.Fatalf("HealthCheck: %v", err)
	}
}
