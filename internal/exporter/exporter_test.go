package exporter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func exportTestRecords(t *testing.T, perDay int) (*storage.Manager, time.Time, time.Time) {
	t.Helper()
	mgr, err := storage.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })
	start := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	// Insert in reverse order to exercise ordering within and across partitions.
	for day := 2; day >= 0; day-- {
		for i := perDay - 1; i >= 0; i-- {
			ts := start.AddDate(0, 0, day).Add(time.Duration(i) * time.Second)
			err := mgr.InsertLocation(context.Background(), &models.LocationRecord{
				Timestamp:    ts,
				TimestampISO: ts.Format(time.RFC3339),
				Latitude:     47.3769,
				Longitude:    8.5417,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return mgr, start, start.AddDate(0, 0, 3).Add(-time.Nanosecond)
}

func TestExportGeoJSONCollections(t *testing.T) {
	mgr, start, end := exportTestRecords(t, 2)
	for _, tc := range []struct {
		name   string
		pretty bool
		empty  bool
	}{
		{name: "compact"},
		{name: "pretty", pretty: true},
		{name: "empty compact", empty: true},
		{name: "empty pretty", pretty: true, empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to, expected := start, end, 6
			if tc.empty {
				from, to, expected = start.AddDate(0, 0, 3), end.AddDate(0, 0, 3), 0
			}
			var buf bytes.Buffer
			if err := exporter.ExportGeoJSON(context.Background(), mgr, from, to, &buf, tc.pretty); err != nil {
				t.Fatal(err)
			}
			var fc models.GeoJSONFeatureCollection
			if err := json.Unmarshal(buf.Bytes(), &fc); err != nil {
				t.Fatalf("invalid GeoJSON: %v", err)
			}
			if fc.Type != "FeatureCollection" || fc.Features == nil || len(fc.Features) != expected {
				t.Fatalf("unexpected collection: type=%s features=%v", fc.Type, fc.Features)
			}
			for i, feature := range fc.Features {
				expectedTime := start.AddDate(0, 0, i/2).Add(time.Duration(i%2) * time.Second)
				if feature.Type != "Feature" || feature.Geometry.Type != "Point" || feature.Properties["timestamp"] != expectedTime.Format(time.RFC3339) {
					t.Errorf("unexpected feature #%d: %+v", i, feature)
				}
			}
			if tc.pretty && !bytes.Contains(buf.Bytes(), []byte("\n  \"features\":")) {
				t.Error("pretty output is not indented")
			}
			if !tc.pretty && bytes.Count(buf.Bytes(), []byte("\n")) != 1 {
				t.Error("compact output must have only its trailing newline")
			}
		})
	}
}

type exportWriterFunc func([]byte) (int, error)

func (f exportWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestExportGeoJSONStreamsOutput(t *testing.T) {
	mgr, start, end := exportTestRecords(t, 32)
	for _, pretty := range []bool{false, true} {
		var buf bytes.Buffer
		writes := 0
		writer := exportWriterFunc(func(p []byte) (int, error) {
			writes++
			if len(p) > 4096 {
				return 0, errors.New("output was not streamed in bounded writes")
			}
			return buf.Write(p)
		})
		if err := exporter.ExportGeoJSON(context.Background(), mgr, start, end, writer, pretty); err != nil {
			t.Fatalf("pretty=%v: %v", pretty, err)
		}
		var fc models.GeoJSONFeatureCollection
		if err := json.Unmarshal(buf.Bytes(), &fc); err != nil || len(fc.Features) != 96 || writes < 2 {
			t.Fatalf("pretty=%v: features=%d writes=%d error=%v", pretty, len(fc.Features), writes, err)
		}
	}
}

func TestExportGeoJSONOutputFailures(t *testing.T) {
	mgr, start, end := exportTestRecords(t, 32)
	t.Run("short write", func(t *testing.T) {
		writer := exportWriterFunc(func(p []byte) (int, error) { return len(p) - 1, nil })
		err := exporter.ExportGeoJSON(context.Background(), mgr, start, end, writer, false)
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("expected io.ErrShortWrite, got %v", err)
		}
	})
	t.Run("writer error", func(t *testing.T) {
		writeErr := errors.New("output unavailable")
		writes := 0
		writer := exportWriterFunc(func(p []byte) (int, error) {
			writes++
			return 0, writeErr
		})
		err := exporter.ExportGeoJSON(context.Background(), mgr, start, end, writer, false)
		if !errors.Is(err, writeErr) || writes != 1 {
			t.Fatalf("expected writer error without further writes, got error=%v writes=%d", err, writes)
		}
	})
	t.Run("canceled during output", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		writes := 0
		writer := exportWriterFunc(func(p []byte) (int, error) {
			writes++
			cancel()
			return len(p), nil
		})
		err := exporter.ExportGeoJSON(ctx, mgr, start, end, writer, false)
		if !errors.Is(err, context.Canceled) || writes != 1 {
			t.Fatalf("expected cancellation without further writes, got error=%v writes=%d", err, writes)
		}
	})
}
