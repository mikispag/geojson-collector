package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"

	"github.com/mikispag/geojson-collector/internal/geo"
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
	mu         sync.Mutex
	dbs        map[string]*sql.DB
	lastAccess map[string]time.Time
}

// NewManager creates a new read-write storage Manager for the given data directory (used by daemon).
func NewManager(dataDir string) (*Manager, error) {
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("creating data directory %s: %w", dataDir, err)
	}
	if err := restrictPermissions(dataDir, 0750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), "-wal"), "-shm")
		if !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".sqlite")); err != nil {
			continue
		}
		if err := restrictPermissions(filepath.Join(dataDir, entry.Name()), 0640); err != nil {
			return nil, err
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
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		return nil, fmt.Errorf("opening data directory %s: %w", dataDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("data directory %s is not a directory", dataDir)
	}
	return &Manager{
		dataDir:    dataDir,
		readOnly:   true,
		dbs:        make(map[string]*sql.DB),
		lastAccess: make(map[string]time.Time),
	}, nil
}

// restrictPermissions tightens existing modes without granting new access.
func restrictPermissions(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() & ^mode == 0 {
		return nil
	}
	if err := os.Chmod(path, info.Mode().Perm()&mode); err != nil {
		return fmt.Errorf("restricting permissions for %s: %w", path, err)
	}
	return nil
}

// getDBForDate returns or opens the SQLite database for a specific UTC date (YYYY-MM-DD).
// The caller must hold m.mu for the entire operation using the returned handle.
func (m *Manager) getDBForDate(ctx context.Context, dateStr string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
			if err := oldestDB.Close(); err != nil {
				return nil, err
			}
			delete(m.dbs, oldestKey)
			delete(m.lastAccess, oldestKey)
		} else {
			break
		}
	}

	dbPath := filepath.Join(m.dataDir, fmt.Sprintf("%s.sqlite", dateStr))

	params := url.Values{}
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "synchronous(FULL)")
	params.Add("_pragma", "foreign_keys(ON)")
	params.Add("_pragma", "temp_store(MEMORY)")
	if m.readOnly {
		params.Set("mode", "ro")
	} else {
		// Precreate with private permissions so SQLite's WAL/SHM inherit them.
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0640)
		if err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		if err := restrictPermissions(dbPath, 0640); err != nil {
			return nil, err
		}
		params.Add("_pragma", "journal_mode(WAL)")
	}
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: params.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if !m.readOnly {
		if _, err := db.ExecContext(ctx, schema); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("executing schema on %s: %w", dbPath, err)
		}
		if err := repairLegacyTelemetry(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("repairing telemetry in %s: %w", dbPath, err)
		}
	}

	m.dbs[dateStr] = db
	m.lastAccess[dateStr] = time.Now()
	return db, nil
}

// repairLegacyTelemetry clears optional infinities accepted by older versions.
func repairLegacyTelemetry(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE locations SET
    altitude = CASE WHEN abs(altitude) > 1.7976931348623157e308 THEN NULL ELSE altitude END,
    desired_accuracy = CASE WHEN abs(desired_accuracy) > 1.7976931348623157e308 THEN NULL ELSE desired_accuracy END,
    deferred = CASE WHEN abs(deferred) > 1.7976931348623157e308 THEN NULL ELSE deferred END
WHERE abs(altitude) > 1.7976931348623157e308
   OR abs(desired_accuracy) > 1.7976931348623157e308
   OR abs(deferred) > 1.7976931348623157e308;
PRAGMA user_version = 1;`); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertLocation inserts a LocationRecord into the SQLite database for its UTC date.
func (m *Manager) InsertLocation(ctx context.Context, loc *models.LocationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readOnly {
		return fmt.Errorf("cannot insert location into read-only manager")
	}
	if err := validateRecord(loc); err != nil {
		return err
	}
	db, err := m.getDBForDate(ctx, loc.Timestamp.UTC().Format("2006-01-02"))
	if err != nil {
		return err
	}
	return insertLocation(ctx, db, loc)
}

// InsertLocationIfUnique checks and inserts one point atomically.
func (m *Manager) InsertLocationIfUnique(ctx context.Context, loc *models.LocationRecord, radiusMeters float64, interval time.Duration) (*geo.DuplicateMatch, error) {
	matches, err := m.InsertLocationsIfUnique(ctx, []*models.LocationRecord{loc}, radiusMeters, interval)
	if err != nil {
		return nil, err
	}
	return matches[0], nil
}

// InsertLocationsIfUnique serializes deduplication and insertion across requests
// sharing this manager. Each UTC day's accepted points commit in one transaction.
// Matches correspond to input records; nil means the point was inserted. An error
// can leave earlier days committed, so callers may retry the original batch.
func (m *Manager) InsertLocationsIfUnique(ctx context.Context, locations []*models.LocationRecord, radiusMeters float64, interval time.Duration) ([]*geo.DuplicateMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readOnly {
		return nil, fmt.Errorf("cannot insert location into read-only manager")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matches := make([]*geo.DuplicateMatch, len(locations))
	// Index staged records by device and time so unordered backfills only search
	// their neighboring windows, instead of scanning the entire accepted batch.
	type pendingKey struct {
		device string
		bucket int64
	}
	width := int64(interval)
	if width <= 0 {
		width = 1
	}
	accepted := make(map[pendingKey][]models.LocationRecord)
	var days []string
	groups := make(map[string][]*models.LocationRecord)
	for i, loc := range locations {
		if err := validateRecord(loc); err != nil {
			return nil, err
		}
		var existing []models.LocationRecord
		start, end := loc.Timestamp.Add(-interval), loc.Timestamp.Add(interval)
		err := m.visitLocations(ctx, start, end, func(rec models.LocationRecord) error {
			existing = append(existing, rec)
			return nil
		})
		if err != nil {
			return nil, err
		}
		match := geo.FindDuplicate(existing, loc, radiusMeters, interval)
		device := geo.DeviceKey(loc)
		if match == nil {
			for bucket, last := start.UnixNano()/width, end.UnixNano()/width; bucket <= last; bucket++ {
				match = geo.FindDuplicate(accepted[pendingKey{device, bucket}], loc, radiusMeters, interval)
				if match != nil || bucket == last {
					break
				}
			}
		}
		if match != nil {
			matches[i] = match
			continue
		}
		key := pendingKey{device, loc.Timestamp.UnixNano() / width}
		accepted[key] = append(accepted[key], *loc)
		day := loc.Timestamp.UTC().Format("2006-01-02")
		if _, ok := groups[day]; !ok {
			days = append(days, day)
		}
		groups[day] = append(groups[day], loc)
	}
	for _, day := range days {
		db, err := m.getDBForDate(ctx, day)
		if err != nil {
			return nil, err
		}
		if err := insertDay(ctx, db, groups[day]); err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func validateRecord(loc *models.LocationRecord) error {
	if err := geo.ValidateFiniteNumbers(loc); err != nil {
		return err
	}
	return validateTimestamp(loc.Timestamp)
}

func insertDay(ctx context.Context, db *sql.DB, locations []*models.LocationRecord) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, loc := range locations {
		if err := insertLocation(ctx, tx, loc); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type locationWriter interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func insertLocation(ctx context.Context, db locationWriter, loc *models.LocationRecord) error {
	dateStr := loc.Timestamp.UTC().Format("2006-01-02")

	var motionJSON sql.NullString
	if len(loc.Motion) > 0 {
		if b, err := json.Marshal(loc.Motion); err == nil {
			motionJSON = sql.NullString{String: string(b), Valid: true}
		}
	}

	var extraJSON sql.NullString
	if len(loc.ExtraProperties) > 0 {
		b, err := json.Marshal(loc.ExtraProperties)
		if err != nil {
			return fmt.Errorf("serializing extra properties: %w", err)
		}
		extraJSON = sql.NullString{String: string(b), Valid: true}
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
	var records []models.LocationRecord
	err := m.VisitLocationsInRange(ctx, start, end, func(rec models.LocationRecord) error {
		records = append(records, rec)
		return nil
	})
	return records, err
}

// VisitLocationsInRange visits records in timestamp order without retaining the
// entire range. The callback must not call back into this manager.
func (m *Manager) VisitLocationsInRange(ctx context.Context, start, end time.Time, visit func(models.LocationRecord) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visitLocations(ctx, start, end, visit)
}

func validateTimestamp(ts time.Time) error {
	if !time.Unix(0, ts.UnixNano()).Equal(ts) {
		return fmt.Errorf("timestamp %v is outside SQLite nanosecond storage range", ts)
	}
	return nil
}

func (m *Manager) visitLocations(ctx context.Context, start, end time.Time, visit func(models.LocationRecord) error) error {
	if start.After(end) {
		start, end = end, start
	}
	if err := validateTimestamp(start); err != nil {
		return err
	}
	if err := validateTimestamp(end); err != nil {
		return err
	}
	startUTC, endUTC := start.UTC(), end.UTC()
	curr := time.Date(startUTC.Year(), startUTC.Month(), startUTC.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(endUTC.Year(), endUTC.Month(), endUTC.Day(), 0, 0, 0, 0, time.UTC)
	for !curr.After(endDay) {
		if err := ctx.Err(); err != nil {
			return err
		}
		dateStr := curr.Format("2006-01-02")
		dbPath := filepath.Join(m.dataDir, dateStr+".sqlite")
		if _, isLoaded := m.dbs[dateStr]; !isLoaded {
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				curr = curr.AddDate(0, 0, 1)
				continue
			} else if err != nil {
				return err
			}
		}
		db, err := m.getDBForDate(ctx, dateStr)
		if err != nil {
			return err
		}
		if err := queryLocations(ctx, db, startUTC.UnixNano(), endUTC.UnixNano(), visit); err != nil {
			var sqliteErr *sqlite.Error
			if m.readOnly && errors.As(err, &sqliteErr) && sqliteErr.Code()&255 == 8 {
				return fmt.Errorf("querying locations from %s: WAL sidecars need directory write access when absent; run export as the service account that owns the data directory: %w", dateStr, err)
			}
			return fmt.Errorf("querying locations from %s: %w", dateStr, err)
		}
		curr = curr.AddDate(0, 0, 1)
	}
	return nil
}

// GetLocationsInRange is an alias for GetLocationsInWindow, returning sorted records.
func (m *Manager) GetLocationsInRange(ctx context.Context, start, end time.Time) ([]models.LocationRecord, error) {
	return m.GetLocationsInWindow(ctx, start, end)
}

func queryLocations(ctx context.Context, db *sql.DB, startNano, endNano int64, visit func(models.LocationRecord) error) error {
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
		return err
	}
	defer rows.Close()

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
			return fmt.Errorf("scanning location row: %w", err)
		}

		var motion []string
		if motionStr.Valid && motionStr.String != "" {
			_ = json.Unmarshal([]byte(motionStr.String), &motion)
		}

		var extraProps map[string]interface{}
		if extraStr.Valid && extraStr.String != "" {
			decoder := json.NewDecoder(strings.NewReader(extraStr.String))
			decoder.UseNumber()
			if err := decoder.Decode(&extraProps); err != nil {
				return fmt.Errorf("decoding extra properties for location %d: %w", id, err)
			}
		}

		var pauses *bool
		if pausesVal.Valid {
			b := pausesVal.Int64 != 0
			pauses = &b
		}

		// Read-only exports also tolerate legacy optional infinities without changing disk.
		for _, value := range []**float64{&alt, &desiredAcc, &defVal} {
			if *value != nil && (math.IsNaN(**value) || math.IsInf(**value, 0)) {
				*value = nil
			}
		}
		ts := time.Unix(0, tsNano).UTC()

		if err := visit(models.LocationRecord{
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
		}); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	return nil
}

// Close closes all open database connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for dateStr, db := range m.dbs {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing database %s: %w", dateStr, err)
		}
	}
	m.dbs = make(map[string]*sql.DB)
	m.lastAccess = make(map[string]time.Time)
	return firstErr
}
