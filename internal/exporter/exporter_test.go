package exporter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/exporter"
	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func TestParseTimeFlag(t *testing.T) {
	// Date-only from
	tFrom, err := exporter.ParseTimeFlag("2026-08-23", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedFrom := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if !tFrom.Equal(expectedFrom) {
		t.Errorf("expected from %v, got %v", expectedFrom, tFrom)
	}

	// Date-only to (end of day)
	tTo, err := exporter.ParseTimeFlag("2026-08-23", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedTo := time.Date(2026, 8, 23, 23, 59, 59, 999999999, time.UTC)
	if !tTo.Equal(expectedTo) {
		t.Errorf("expected to %v, got %v", expectedTo, tTo)
	}

	// Full ISO8601 timestamp
	tISO, err := exporter.ParseTimeFlag("2026-08-23T14:30:00Z", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedISO := time.Date(2026, 8, 23, 14, 30, 0, 0, time.UTC)
	if !tISO.Equal(expectedISO) {
		t.Errorf("expected %v, got %v", expectedISO, tISO)
	}

	// Offset timestamp
	tOffset, err := exporter.ParseTimeFlag("2026-08-23T16:30:00+02:00", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tOffset.Equal(expectedISO) {
		t.Errorf("expected %v, got %v", expectedISO, tOffset)
	}

	// Invalid format
	if _, err := exporter.ParseTimeFlag("not-a-date", false); err == nil {
		t.Error("expected error for invalid date, got nil")
	}
}

func TestExportGeoJSON(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create storage manager: %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()

	t1 := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	alt := 412.5
	spd := 15.0
	crs := 270.0
	hAcc := 4.5
	bLevel := 0.77

	rec := &models.LocationRecord{
		Timestamp:          t1,
		TimestampISO:       t1.Format(time.RFC3339),
		Latitude:           47.3769,
		Longitude:          8.5417,
		Altitude:           &alt,
		Speed:              &spd,
		Course:             &crs,
		HorizontalAccuracy: &hAcc,
		Motion:             []string{"cycling"},
		BatteryState:       "unplugged",
		BatteryLevel:       &bLevel,
		WiFi:               "ZRH_WiFi",
		DeviceID:           "device-xyz",
	}

	if err := mgr.InsertLocation(ctx, rec); err != nil {
		t.Fatalf("failed to insert record: %v", err)
	}

	var buf bytes.Buffer
	from := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 23, 23, 59, 59, 0, time.UTC)

	if err := exporter.ExportGeoJSON(ctx, mgr, from, to, &buf, true); err != nil {
		t.Fatalf("ExportGeoJSON failed: %v", err)
	}

	var fc models.GeoJSONFeatureCollection
	if err := json.Unmarshal(buf.Bytes(), &fc); err != nil {
		t.Fatalf("failed to unmarshal output GeoJSON: %v\nJSON:\n%s", err, buf.String())
	}

	if fc.Type != "FeatureCollection" {
		t.Errorf("expected FeatureCollection, got %s", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}

	feat := fc.Features[0]
	if feat.Type != "Feature" {
		t.Errorf("expected Feature, got %s", feat.Type)
	}
	if feat.Geometry.Type != "Point" {
		t.Errorf("expected Point, got %s", feat.Geometry.Type)
	}
	if len(feat.Geometry.Coordinates) != 3 {
		t.Fatalf("expected 3 coordinates [lon, lat, alt], got %v", feat.Geometry.Coordinates)
	}
	if feat.Geometry.Coordinates[0] != 8.5417 || feat.Geometry.Coordinates[1] != 47.3769 || feat.Geometry.Coordinates[2] != 412.5 {
		t.Errorf("unexpected coordinates: %v", feat.Geometry.Coordinates)
	}

	// Verify Timelinize-compatible properties
	props := feat.Properties
	if props["timestamp"] != "2026-08-23T10:00:00Z" {
		t.Errorf("expected timestamp 2026-08-23T10:00:00Z, got %v", props["timestamp"])
	}
	if props["velocity"] != 15.0 {
		t.Errorf("expected velocity 15.0, got %v", props["velocity"])
	}
	if props["speed"] != 15.0 {
		t.Errorf("expected speed 15.0, got %v", props["speed"])
	}
	if props["heading"] != 270.0 {
		t.Errorf("expected heading 270.0, got %v", props["heading"])
	}
	if props["course"] != 270.0 {
		t.Errorf("expected course 270.0, got %v", props["course"])
	}
	if props["accuracy"] != 4.5 {
		t.Errorf("expected accuracy 4.5, got %v", props["accuracy"])
	}
	if props["horizontal_accuracy"] != 4.5 {
		t.Errorf("expected horizontal_accuracy 4.5, got %v", props["horizontal_accuracy"])
	}
	if props["altitude"] != 412.5 {
		t.Errorf("expected altitude 412.5, got %v", props["altitude"])
	}
	if props["wifi"] != "ZRH_WiFi" {
		t.Errorf("expected wifi ZRH_WiFi, got %v", props["wifi"])
	}
}
