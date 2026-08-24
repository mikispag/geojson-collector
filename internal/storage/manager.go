package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/mikispag/geojson-collector/internal/models"
)

const schema = `
CREATE TABLE IF NOT EXISTS locations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    timestamp_iso TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    altitude REAL,
    speed REAL,
    course REAL,
    horizontal_accuracy REAL,
    vertical_accuracy REAL,
    speed_accuracy REAL,
    course_accuracy REAL,
    motion TEXT,
    battery_state TEXT,
    battery_level REAL,
    wifi TEXT,
    device_id TEXT,
    unique_id TEXT,
    pauses INTEGER,
    activity TEXT,
    desired_accuracy REAL,
    deferred REAL,
    significant_change TEXT,
    locations_in_payload INTEGER,
    extra_properties TEXT
);
CREATE INDEX IF NOT EXISTS idx_locations_ts ON locations(timestamp);
CREATE INDEX IF NOT EXISTS idx_locations_coords ON locations(latitude, longitude);
`

const (
	maxCachedDBs = 2
)

// Manager coordinates opening, closing, querying, and inserting into daily SQLite databases.
type Manager struct {
	dataDir    string
	readOnly   bool
	mu         sync.RWMutex
	dbs        map[string]*sql.DB
	lastAccess map[string]time.Time
}

// NewManager creates a new read-write storage Manager for the given data directory (used by daemon).
func NewManager(dataDir string) (*Manager, error) {
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("creating data directory %s: %w", dataDir, err)
		}
	}
	return &Manager{
		dataDir:    dataDir,
		readOnly:   false,
		dbs:        make(map[string]*sql.DB),
		lastAccess: make(map[string]time.Time),
	}, nil
}

// NewReadOnlyManager creates a read-only storage Manager for the given data directory (used by exporter).
func NewReadOnlyManager(dataDir string) (*Manager, error) {
	return &Manager{
		dataDir:    dataDir,
		readOnly:   true,
		dbs:        make(map[string]*sql.DB),
		lastAccess: make(map[string]time.Time),
	}, nil
}

// getDBForDate returns or opens the SQLite database for a specific UTC date (YYYY-MM-DD).
// In read-only mode, it opens with mode=ro without executing WAL pragma or DDL.
func (m *Manager) getDBForDate(dateStr string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if db, ok := m.dbs[dateStr]; ok {
		m.lastAccess[dateStr] = time.Now()
		return db, nil
	}

	// Evict least recently accessed database connections if cache exceeds maxCachedDBs
	for len(m.dbs) >= maxCachedDBs {
		var oldestKey string
		var oldestTime time.Time
		for k, t := range m.lastAccess {
			if oldestKey == "" || t.Before(oldestTime) {
				oldestKey = k
				oldestTime = t
			}
		}
		if oldestDB, exists := m.dbs[oldestKey]; exists {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = oldestDB.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE);")
			cancel()
			_ = oldestDB.Close()
			delete(m.dbs, oldestKey)
			delete(m.lastAccess, oldestKey)
		} else {
			break
		}
	}

	dbPath := filepath.Join(m.dataDir, fmt.Sprintf("%s.sqlite", dateStr))

	if m.readOnly {
		dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)", dbPath)
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening read-only sqlite database %s: %w", dbPath, err)
		}
		m.dbs[dateStr] = db
		m.lastAccess[dateStr] = time.Now()
		return db, nil
	}

	// Open with pragmas configured for WAL mode and robust crash tolerance
	// _pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database %s: %w", dbPath, err)
	}

	// Ensure WAL mode and table initialization
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling WAL mode for %s: %w", dbPath, err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous = NORMAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting synchronous=NORMAL for %s: %w", dbPath, err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("executing schema on %s: %w", dbPath, err)
	}

	m.dbs[dateStr] = db
	m.lastAccess[dateStr] = time.Now()
	return db, nil
}

// InsertLocation inserts a LocationRecord into the SQLite database for its UTC date.
func (m *Manager) InsertLocation(ctx context.Context, loc *models.LocationRecord) error {
	if loc == nil {
		return fmt.Errorf("nil location record")
	}
	if m.readOnly {
		return fmt.Errorf("cannot insert location into read-only manager")
	}

	dateStr := loc.Timestamp.UTC().Format("2006-01-02")
	db, err := m.getDBForDate(dateStr)
	if err != nil {
		return err
	}

	var motionJSON sql.NullString
	if len(loc.Motion) > 0 {
		if b, err := json.Marshal(loc.Motion); err == nil {
			motionJSON = sql.NullString{String: string(b), Valid: true}
		}
	}

	var extraJSON sql.NullString
	if len(loc.ExtraProperties) > 0 {
		if b, err := json.Marshal(loc.ExtraProperties); err == nil {
			extraJSON = sql.NullString{String: string(b), Valid: true}
		}
	}

	var pausesVal sql.NullInt64
	if loc.Pauses != nil {
		var v int64
		if *loc.Pauses {
			v = 1
		}
		pausesVal = sql.NullInt64{Int64: v, Valid: true}
	}

	iso := loc.TimestampISO
	if iso == "" {
		iso = loc.Timestamp.UTC().Format(time.RFC3339Nano)
	}

	tsUnixNano := loc.Timestamp.UTC().UnixNano()

	query := `
	INSERT INTO locations (
		timestamp, timestamp_iso, latitude, longitude, altitude, speed, course,
		horizontal_accuracy, vertical_accuracy, speed_accuracy, course_accuracy,
		motion, battery_state, battery_level, wifi, device_id, unique_id,
		pauses, activity, desired_accuracy, deferred, significant_change,
		locations_in_payload, extra_properties
	) VALUES (
		?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?,
		?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?
	)`

	res, err := db.ExecContext(ctx, query,
		tsUnixNano, iso, loc.Latitude, loc.Longitude, loc.Altitude, loc.Speed, loc.Course,
		loc.HorizontalAccuracy, loc.VerticalAccuracy, loc.SpeedAccuracy, loc.CourseAccuracy,
		motionJSON, loc.BatteryState, loc.BatteryLevel, loc.WiFi, loc.DeviceID, loc.UniqueID,
		pausesVal, loc.Activity, loc.DesiredAccuracy, loc.Deferred, loc.SignificantChange,
		loc.LocationsInPayload, extraJSON,
	)
	if err != nil {
		return fmt.Errorf("inserting location into %s.sqlite: %w", dateStr, err)
	}

	id, err := res.LastInsertId()
	if err == nil {
		loc.ID = id
	}

	return nil
}

// GetLocationsInWindow queries all records within [start, end] across all relevant daily databases.
func (m *Manager) GetLocationsInWindow(ctx context.Context, start, end time.Time) ([]models.LocationRecord, error) {
	if start.After(end) {
		start, end = end, start
	}

	startUTC := start.UTC()
	endUTC := end.UTC()

	var records []models.LocationRecord

	// Iterate day by day from start to end
	curr := time.Date(startUTC.Year(), startUTC.Month(), startUTC.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(endUTC.Year(), endUTC.Month(), endUTC.Day(), 0, 0, 0, 0, time.UTC)

	startNano := startUTC.UnixNano()
	endNano := endUTC.UnixNano()

	for !curr.After(endDay) {
		dateStr := curr.Format("2006-01-02")
		dbPath := filepath.Join(m.dataDir, fmt.Sprintf("%s.sqlite", dateStr))

		// Only check if file exists or if we already have it open
		m.mu.RLock()
		_, isLoaded := m.dbs[dateStr]
		m.mu.RUnlock()

		if !isLoaded {
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				curr = curr.AddDate(0, 0, 1)
				continue
			}
		}

		db, err := m.getDBForDate(dateStr)
		if err != nil {
			return nil, err
		}

		dayRecords, err := queryLocations(ctx, db, startNano, endNano)
		if err != nil {
			return nil, fmt.Errorf("querying locations from %s: %w", dateStr, err)
		}

		records = append(records, dayRecords...)
		curr = curr.AddDate(0, 0, 1)
	}

	return records, nil
}

// GetLocationsInRange is an alias for GetLocationsInWindow, returning sorted records.
func (m *Manager) GetLocationsInRange(ctx context.Context, start, end time.Time) ([]models.LocationRecord, error) {
	return m.GetLocationsInWindow(ctx, start, end)
}

func queryLocations(ctx context.Context, db *sql.DB, startNano, endNano int64) ([]models.LocationRecord, error) {
	query := `
	SELECT
		id, timestamp, timestamp_iso, latitude, longitude, altitude, speed, course,
		horizontal_accuracy, vertical_accuracy, speed_accuracy, course_accuracy,
		motion, battery_state, battery_level, wifi, device_id, unique_id,
		pauses, activity, desired_accuracy, deferred, significant_change,
		locations_in_payload, extra_properties
	FROM locations
	WHERE timestamp >= ? AND timestamp <= ?
	ORDER BY timestamp ASC
	`

	rows, err := db.QueryContext(ctx, query, startNano, endNano)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.LocationRecord

	for rows.Next() {
		var (
			id                  int64
			tsNano              int64
			tsISO               string
			lat, lon            float64
			alt, speed, course  *float64
			hAcc, vAcc          *float64
			speedAcc, courseAcc *float64
			motionStr           sql.NullString
			batteryState        string
			batteryLevel        *float64
			wifi, devID, unqID  string
			pausesVal           sql.NullInt64
			activity            string
			desiredAcc, defVal  *float64
			sigChange           string
			locInPayload        int
			extraStr            sql.NullString
		)

		err := rows.Scan(
			&id, &tsNano, &tsISO, &lat, &lon, &alt, &speed, &course,
			&hAcc, &vAcc, &speedAcc, &courseAcc,
			&motionStr, &batteryState, &batteryLevel, &wifi, &devID, &unqID,
			&pausesVal, &activity, &desiredAcc, &defVal, &sigChange,
			&locInPayload, &extraStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning location row: %w", err)
		}

		var motion []string
		if motionStr.Valid && motionStr.String != "" {
			_ = json.Unmarshal([]byte(motionStr.String), &motion)
		}

		var extraProps map[string]interface{}
		if extraStr.Valid && extraStr.String != "" {
			_ = json.Unmarshal([]byte(extraStr.String), &extraProps)
		}

		var pauses *bool
		if pausesVal.Valid {
			b := pausesVal.Int64 != 0
			pauses = &b
		}

		ts := time.Unix(0, tsNano).UTC()

		records = append(records, models.LocationRecord{
			ID:                 id,
			Timestamp:          ts,
			TimestampISO:       tsISO,
			Latitude:           lat,
			Longitude:          lon,
			Altitude:           alt,
			Speed:              speed,
			Course:             course,
			HorizontalAccuracy: hAcc,
			VerticalAccuracy:   vAcc,
			SpeedAccuracy:      speedAcc,
			CourseAccuracy:     courseAcc,
			Motion:             motion,
			BatteryState:       batteryState,
			BatteryLevel:       batteryLevel,
			WiFi:               wifi,
			DeviceID:           devID,
			UniqueID:           unqID,
			Pauses:             pauses,
			Activity:           activity,
			DesiredAccuracy:    desiredAcc,
			Deferred:           defVal,
			SignificantChange:  sigChange,
			LocationsInPayload: locInPayload,
			ExtraProperties:    extraProps,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// Close closes all open database connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for dateStr, db := range m.dbs {
		// Attempt a passive WAL checkpoint before closing to keep files clean
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE);")
		cancel()

		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing database %s: %w", dateStr, err)
		}
	}
	m.dbs = make(map[string]*sql.DB)
	m.lastAccess = make(map[string]time.Time)
	return firstErr
}
