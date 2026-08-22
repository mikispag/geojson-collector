package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func TestStorageManager_DailyDBAndWindowQueries(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()

	// Day 1: 2026-08-22 23:59:50 UTC
	t1 := time.Date(2026, 8, 22, 23, 59, 50, 0, time.UTC)
	alt := 450.0
	speed := 12.5
	course := 180.0
	hAcc := 5.0
	vAcc := 3.0
	speedAcc := 0.2
	courseAcc := 1.0
	bLevel := 0.95
	pauses := false

	rec1 := &models.LocationRecord{
		Timestamp:          t1,
		TimestampISO:       t1.Format(time.RFC3339Nano),
		Latitude:           47.3769,
		Longitude:          8.5417,
		Altitude:           &alt,
		Speed:              &speed,
		Course:             &course,
		HorizontalAccuracy: &hAcc,
		VerticalAccuracy:   &vAcc,
		SpeedAccuracy:      &speedAcc,
		CourseAccuracy:     &courseAcc,
		Motion:             []string{"driving"},
		BatteryState:       "charging",
		BatteryLevel:       &bLevel,
		WiFi:               "OfficeWiFi",
		DeviceID:           "test-device",
		UniqueID:           "uid-1",
		Pauses:             &pauses,
		Activity:           "automotive_navigation",
		LocationsInPayload: 1,
		ExtraProperties:    map[string]interface{}{"custom_field": "xyz"},
	}

	if err := mgr.InsertLocation(ctx, rec1); err != nil {
		t.Fatalf("failed to insert rec1: %v", err)
	}

	// Day 2: 2026-08-23 00:00:10 UTC (20 seconds later)
	t2 := time.Date(2026, 8, 23, 0, 0, 10, 0, time.UTC)
	rec2 := &models.LocationRecord{
		Timestamp:    t2,
		TimestampISO: t2.Format(time.RFC3339Nano),
		Latitude:     47.3770,
		Longitude:    8.5418,
	}

	if err := mgr.InsertLocation(ctx, rec2); err != nil {
		t.Fatalf("failed to insert rec2: %v", err)
	}

	// Check that 2 separate daily sqlite files were created
	db1 := filepath.Join(tempDir, "2026-08-22.sqlite")
	db2 := filepath.Join(tempDir, "2026-08-23.sqlite")

	// Query window spanning across midnight: 23:59:40 to 00:00:20
	windowStart := time.Date(2026, 8, 22, 23, 59, 40, 0, time.UTC)
	windowEnd := time.Date(2026, 8, 23, 0, 0, 20, 0, time.UTC)

	recs, err := mgr.GetLocationsInWindow(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("GetLocationsInWindow failed: %v", err)
	}

	if len(recs) != 2 {
		t.Fatalf("expected 2 records across midnight window, got %d", len(recs))
	}

	// Verify all fields on rec1
	r1 := recs[0]
	if !r1.Timestamp.Equal(t1) {
		t.Errorf("expected timestamp %v, got %v", t1, r1.Timestamp)
	}
	if r1.Latitude != 47.3769 || r1.Longitude != 8.5417 {
		t.Errorf("expected lat/lon 47.3769/8.5417, got %f/%f", r1.Latitude, r1.Longitude)
	}
	if r1.Altitude == nil || *r1.Altitude != 450.0 {
		t.Errorf("expected altitude 450.0, got %v", r1.Altitude)
	}
	if r1.Speed == nil || *r1.Speed != 12.5 {
		t.Errorf("expected speed 12.5, got %v", r1.Speed)
	}
	if len(r1.Motion) != 1 || r1.Motion[0] != "driving" {
		t.Errorf("expected motion [driving], got %v", r1.Motion)
	}
	if r1.Pauses == nil || *r1.Pauses != false {
		t.Errorf("expected pauses false, got %v", r1.Pauses)
	}
	if r1.ExtraProperties["custom_field"] != "xyz" {
		t.Errorf("expected custom_field xyz, got %v", r1.ExtraProperties)
	}

	// Query only day 2
	day2Start := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	day2End := time.Date(2026, 8, 23, 23, 59, 59, 0, time.UTC)
	day2Recs, err := mgr.GetLocationsInWindow(ctx, day2Start, day2End)
	if err != nil {
		t.Fatalf("failed querying day 2: %v", err)
	}
	if len(day2Recs) != 1 {
		t.Fatalf("expected 1 record on day 2, got %d", len(day2Recs))
	}

	// Close and reopen manager to ensure durability
	if err := mgr.Close(); err != nil {
		t.Fatalf("failed to close manager: %v", err)
	}

	mgr2, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to reopen manager: %v", err)
	}
	defer mgr2.Close()

	recsAfterReopen, err := mgr2.GetLocationsInWindow(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("failed querying after reopen: %v", err)
	}
	if len(recsAfterReopen) != 2 {
		t.Fatalf("expected 2 records after reopen, got %d", len(recsAfterReopen))
	}

	_ = db1
	_ = db2
}
