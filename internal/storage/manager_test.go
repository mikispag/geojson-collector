package storage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

func TestDatabaseSettingsAndPaths(t *testing.T) {
	for _, name := range []string{"data", "data?archive", "data#archive", "data%20archive"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			m, err := NewManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
			if err := m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts, Latitude: 47, Longitude: 8}); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{dir, filepath.Join(dir, "2026-08-01.sqlite"), filepath.Join(dir, "2026-08-01.sqlite-wal"), filepath.Join(dir, "2026-08-01.sqlite-shm")} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm()&0007 != 0 {
					t.Errorf("%s grants access to other users: %o", path, info.Mode().Perm())
				}
			}
			db := m.dbs["2026-08-01"]
			if got := db.Stats().MaxOpenConnections; got != 1 {
				t.Errorf("max connections = %d, want 1", got)
			}
			// Force two fresh connections to verify pragmas apply to every connection.
			db.SetMaxIdleConns(0)
			for i := 0; i < 2; i++ {
				var synchronous int
				if err := db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
					t.Fatal(err)
				}
				if synchronous != 2 {
					t.Errorf("synchronous = %d, want FULL (2)", synchronous)
				}
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			read, err := NewReadOnlyManager(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			records, err := read.GetLocationsInRange(context.Background(), ts, ts)
			if err != nil || len(records) != 1 {
				t.Fatalf("records=%d, err=%v", len(records), err)
			}
		})
	}
}

type blockingMetadata struct {
	entered chan struct{}
	resume  chan struct{}
}

func (b blockingMetadata) MarshalJSON() ([]byte, error) {
	close(b.entered)
	<-b.resume
	return []byte(`"value"`), nil
}

func TestConcurrentInsertSurvivesCacheEviction(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	b := blockingMetadata{make(chan struct{}), make(chan struct{})}
	first := make(chan error, 1)
	go func() {
		first <- m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts, Latitude: 47, Longitude: 8, ExtraProperties: map[string]interface{}{"test": b}})
	}()
	<-b.entered
	others := make(chan error, 1)
	go func() {
		for day := 1; day <= 2; day++ {
			if err := m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts.AddDate(0, 0, day), Latitude: 47, Longitude: 8}); err != nil {
				others <- err
				return
			}
		}
		others <- nil
	}()
	// The old cache could evict the first database while its caller marshaled metadata.
	var othersErr error
	completed := false
	select {
	case othersErr = <-others:
		completed = true
	case <-time.After(250 * time.Millisecond):
	}
	close(b.resume)
	if err := <-first; err != nil {
		t.Error(err)
	}
	if !completed {
		othersErr = <-others
	}
	if othersErr != nil {
		t.Error(othersErr)
	}
	records, err := m.GetLocationsInRange(context.Background(), ts, ts.AddDate(0, 0, 2))
	if err != nil || len(records) != 3 {
		t.Fatalf("records=%d, err=%v", len(records), err)
	}
}

func TestRejectUnserializableRecords(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	inf := math.Inf(1)
	for _, record := range []*models.LocationRecord{
		{Timestamp: ts, Latitude: 47, Longitude: 8, Altitude: &inf},
		{Timestamp: ts, Latitude: 47, Longitude: 8, ExtraProperties: map[string]interface{}{"invalid": inf}},
	} {
		if err := m.InsertLocation(context.Background(), record); err == nil {
			t.Error("accepted unserializable record")
		}
	}
}

func TestRepairExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-08-01.sqlite")
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts, Latitude: 47, Longitude: 8}); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE locations SET altitude=?, desired_accuracy=?, deferred=?; PRAGMA user_version=0", math.Inf(1), math.Inf(-1), math.Inf(1)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	read, err := NewReadOnlyManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := read.GetLocationsInRange(context.Background(), ts, ts)
	if err != nil || len(legacy) != 1 {
		t.Fatalf("read-only records=%d, err=%v", len(legacy), err)
	}
	if legacy[0].Altitude != nil || legacy[0].DesiredAccuracy != nil || legacy[0].Deferred != nil {
		t.Error("read-only query retained legacy infinities")
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0007 != 0 {
		t.Errorf("existing database remains accessible: %o", info.Mode().Perm())
	}
	records, err := m.GetLocationsInRange(context.Background(), ts, ts)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d, err=%v", len(records), err)
	}
	if records[0].Altitude != nil || records[0].DesiredAccuracy != nil || records[0].Deferred != nil {
		t.Error("legacy nonfinite optional telemetry was not cleared")
	}
	var cleared bool
	if err := m.dbs["2026-08-01"].QueryRow("SELECT altitude IS NULL AND desired_accuracy IS NULL AND deferred IS NULL FROM locations").Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Error("legacy nonfinite fields were not repaired on disk")
	}
}

func TestVisitLocationsCancellationAndErrors(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ts := time.Date(2026, 8, 1, 12, 0, 0, 123456789, time.UTC)
	for day := 0; day < 3; day++ {
		if err := m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts.AddDate(0, 0, day), Latitude: 47, Longitude: 8}); err != nil {
			t.Fatal(err)
		}
	}
	stop := errors.New("stop visiting")
	visited := 0
	err = m.VisitLocationsInRange(context.Background(), ts, ts.AddDate(0, 0, 2), func(rec models.LocationRecord) error {
		visited++
		if !rec.Timestamp.Equal(ts) {
			t.Errorf("timestamp=%s, want %s", rec.Timestamp, ts)
		}
		return stop
	})
	if !errors.Is(err, stop) || visited != 1 {
		t.Fatalf("visited=%d, err=%v", visited, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.VisitLocationsInRange(ctx, ts, ts, func(models.LocationRecord) error { t.Fatal("visited after cancellation"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if _, err := m.GetLocationsInRange(context.Background(), ts, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("accepted range that overflows nanoseconds")
	}
	if len(m.dbs) > maxCachedDBs {
		t.Errorf("cached %d databases", len(m.dbs))
	}
}

func TestReadOnlyManagerRequiresDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), file} {
		if m, err := NewReadOnlyManager(path); err == nil {
			m.Close()
			t.Errorf("accepted invalid data directory %s", path)
		}
	}
}

func TestBatchRollsBackFailedDayAndCanRetry(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	locations := []*models.LocationRecord{
		{Timestamp: ts, Latitude: 47, Longitude: 8},
		{Timestamp: ts.AddDate(0, 0, 1), Latitude: 47, Longitude: 8},
		{Timestamp: ts.AddDate(0, 0, 1).Add(time.Hour), Latitude: 47, Longitude: 8, ExtraProperties: map[string]interface{}{"invalid": math.Inf(1)}},
	}
	if _, err := m.InsertLocationsIfUnique(context.Background(), locations, 1, time.Minute); err == nil {
		t.Fatal("accepted unserializable metadata")
	}
	stored, err := m.GetLocationsInRange(context.Background(), ts, ts.AddDate(0, 0, 2))
	if err != nil || len(stored) != 1 {
		t.Fatalf("earlier day should remain committed, failing day rolled back: records=%d err=%v", len(stored), err)
	}
	locations[2].ExtraProperties = nil
	matches, err := m.InsertLocationsIfUnique(context.Background(), locations, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if matches[0] == nil || matches[1] != nil || matches[2] != nil {
		t.Fatalf("unexpected retry duplicate matches: %v", matches)
	}
	stored, err = m.GetLocationsInRange(context.Background(), ts, ts.AddDate(0, 0, 2))
	if err != nil || len(stored) != 3 {
		t.Fatalf("records=%d err=%v", len(stored), err)
	}
}
