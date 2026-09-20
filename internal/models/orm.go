package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a registered operator account.
// Table: users
type User struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Email     string    `gorm:"uniqueIndex;not null"                           json:"email"`
	Password  string    `gorm:"not null"                                       json:"-"` // bcrypt — never serialised
	CreatedAt time.Time `gorm:"not null;default:now()"                         json:"created_at"`
}

// APIKey represents a machine-to-machine access key belonging to a User.
// The raw key is shown once on creation; only KeyHash is persisted.
// Table: api_keys
type APIKey struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"                       json:"user_id"`
	KeyHash   string    `gorm:"not null"                                       json:"-"` // bcrypt — never serialised
	Label     string    `gorm:"not null;default:''"                            json:"label"`
	CreatedAt time.Time `gorm:"not null;default:now()"                         json:"created_at"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// ---------------------------------------------------------------------------
// Domain model GORM views (001_init.sql + 002_events.sql)
//
// Pipeline code keeps using the plain structs in models.go.
// Only REST handlers use these tagged versions.
// PostGIS GEOGRAPHY columns are omitted — use ST_X/ST_Y in raw queries.
// ---------------------------------------------------------------------------

// DeviceRow — table: devices (001_init.sql)
//
//	id         SERIAL PRIMARY KEY
//	phone      TEXT NOT NULL UNIQUE
//	location   GEOGRAPHY(POINT, 4326)   ← omitted, not readable by GORM
//	updated_at TIMESTAMPTZ
type DeviceRow struct {
	ID        int       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Phone     string    `gorm:"column:phone;uniqueIndex;not null"  json:"phone"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"   json:"updated_at"`
}

func (DeviceRow) TableName() string { return "devices" }

// ShelterRow — table: shelters (001_init.sql)
//
//	id       SERIAL PRIMARY KEY
//	name     TEXT NOT NULL
//	address  TEXT NOT NULL
//	capacity INT  NOT NULL DEFAULT 0
//	location GEOGRAPHY(POINT, 4326) NOT NULL  ← omitted, not readable by GORM
type ShelterRow struct {
	ID       int    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name     string `gorm:"column:name;not null"               json:"name"`
	Address  string `gorm:"column:address;not null"            json:"address"`
	Capacity int    `gorm:"column:capacity;not null"           json:"capacity"`
}

func (ShelterRow) TableName() string { return "shelters" }

// EventRow — table: events (002_events.sql)
//
//	id              TEXT PRIMARY KEY       ← comes from USGS / sensor source
//	disaster_type   TEXT NOT NULL
//	severity        FLOAT NOT NULL
//	epicenter_lat   FLOAT NOT NULL
//	epicenter_lng   FLOAT NOT NULL
//	radius_km       FLOAT NOT NULL
//	aftershock_risk TEXT NOT NULL
//	tsunami_risk    BOOLEAN NOT NULL
//	created_at      TIMESTAMPTZ
type EventRow struct {
	ID             string    `gorm:"column:id;primaryKey"          json:"id"`
	DisasterType   string    `gorm:"column:disaster_type;not null" json:"disaster_type"`
	Severity       float64   `gorm:"column:severity;not null"      json:"severity"`
	EpicenterLat   float64   `gorm:"column:epicenter_lat;not null" json:"epicenter_lat"`
	EpicenterLng   float64   `gorm:"column:epicenter_lng;not null" json:"epicenter_lng"`
	RadiusKm       float64   `gorm:"column:radius_km;not null"     json:"radius_km"`
	AftershockRisk string    `gorm:"column:aftershock_risk;not null" json:"aftershock_risk"`
	TsunamiRisk    bool      `gorm:"column:tsunami_risk;not null"  json:"tsunami_risk"`
	CreatedAt      time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (EventRow) TableName() string { return "events" }

// DeviceLogRow — table: device_logs (002_events.sql)
//
//	id              SERIAL PRIMARY KEY
//	event_id        TEXT NOT NULL REFERENCES events(id)
//	phone           TEXT NOT NULL
//	zone            TEXT NOT NULL     "red" | "orange" | "green"
//	action          TEXT NOT NULL
//	sms_message     TEXT
//	shelter_name    TEXT
//	rescue_priority INT  DEFAULT 0
//	confidence      FLOAT
//	zone_escalated  BOOLEAN DEFAULT FALSE
//	logged_at       TIMESTAMPTZ
type DeviceLogRow struct {
	ID             int       `gorm:"column:id;primaryKey;autoIncrement"  json:"id"`
	EventID        string    `gorm:"column:event_id;not null;index"      json:"event_id"`
	Phone          string    `gorm:"column:phone;not null;index"         json:"phone"`
	Zone           string    `gorm:"column:zone;not null"                json:"zone"`
	Action         string    `gorm:"column:action;not null"              json:"action"`
	SMSMessage     *string   `gorm:"column:sms_message"                  json:"sms_message,omitempty"`
	ShelterName    *string   `gorm:"column:shelter_name"                 json:"shelter_name,omitempty"`
	RescuePriority int       `gorm:"column:rescue_priority;default:0"    json:"rescue_priority"`
	Confidence     *float64  `gorm:"column:confidence"                   json:"confidence,omitempty"`
	ZoneEscalated  bool      `gorm:"column:zone_escalated;default:false" json:"zone_escalated"`
	LoggedAt       time.Time `gorm:"column:logged_at;default:now()"      json:"logged_at"`
}

func (DeviceLogRow) TableName() string { return "device_logs" }

// RescueFlagRow — table: rescue_flags (002_events.sql)
//
//	id              SERIAL PRIMARY KEY
//	event_id        TEXT NOT NULL REFERENCES events(id)
//	phone           TEXT NOT NULL
//	zone            TEXT NOT NULL
//	rescue_priority INT  DEFAULT 0
//	flagged_at      TIMESTAMPTZ
//	UNIQUE (event_id, phone)
type RescueFlagRow struct {
	ID             int       `gorm:"column:id;primaryKey;autoIncrement"        json:"id"`
	EventID        string    `gorm:"column:event_id;not null;index:idx_rf_evt_pri,priority:1" json:"event_id"`
	Phone          string    `gorm:"column:phone;not null"                     json:"phone"`
	Zone           string    `gorm:"column:zone;not null"                      json:"zone"`
	RescuePriority int       `gorm:"column:rescue_priority;default:0;index:idx_rf_evt_pri,sort:desc,priority:2" json:"rescue_priority"`
	FlaggedAt      time.Time `gorm:"column:flagged_at;default:now()"           json:"flagged_at"`
}

func (RescueFlagRow) TableName() string { return "rescue_flags" }