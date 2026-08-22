package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"


	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/exporter"
	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/server"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func TestEndToEndCollectorAndExport(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}
	defer mgr.Close()

	cfg := &config.Config{
		Host:                 "127.0.0.1",
		Port:                 9696,
		AuthToken:            "secret-token-e2e",
		DataDir:              tempDir,
		DedupRadiusMeters:    1.0,
		DedupIntervalSeconds: 60.0,
	}

	srv := server.New(cfg, mgr, nil)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// 1. Post Overland Payload
	payload := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [-122.030581, 37.331800, 15.0]
      },
      "properties": {
        "timestamp": "2026-08-23T08:00:00-0700",
        "speed": 4.5,
        "course": 180.0,
        "horizontal_accuracy": 5.0,
        "vertical_accuracy": 3.0,
        "speed_accuracy": 0.2,
        "course_accuracy": 1.0,
        "motion": ["driving"],
        "battery_state": "charging",
        "battery_level": 0.88,
        "device_id": "Miki-iPhone",
        "wifi": "TeslaModel3_WiFi",
        "activity": "automotive_navigation",
        "desired_accuracy": 100,
        "deferred": 1000,
        "significant_change": "disabled",
        "locations_in_payload": 1
      }
    }
  ],
  "current": {"state": "active"},
  "trip": {"distance": 1500}
}`

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("failed creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token-e2e")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http post failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var jsonResp server.JSONResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("decoding response failed: %v", err)
	}
	if jsonResp.Result != "ok" {
		t.Fatalf("expected result 'ok', got '%s'", jsonResp.Result)
	}

	// 2. Export GeoJSON for the day
	exportOutPath := filepath.Join(tempDir, "export.geojson")
	outFile, err := os.Create(exportOutPath)
	if err != nil {
		t.Fatalf("creating export file failed: %v", err)
	}

	fromTime, _ := exporter.ParseTimeFlag("2026-08-23", false)
	toTime, _ := exporter.ParseTimeFlag("2026-08-23", true)

	if err := exporter.ExportGeoJSON(context.Background(), mgr, fromTime, toTime, outFile, true); err != nil {
		outFile.Close()
		t.Fatalf("ExportGeoJSON failed: %v", err)
	}
	outFile.Close()

	// 3. Verify exported GeoJSON structure
	data, err := os.ReadFile(exportOutPath)
	if err != nil {
		t.Fatalf("reading export file failed: %v", err)
	}

	var fc models.GeoJSONFeatureCollection
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("parsing exported GeoJSON failed: %v\nJSON:\n%s", err, string(data))
	}

	if fc.Type != "FeatureCollection" {
		t.Errorf("expected FeatureCollection, got %s", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}

	feat := fc.Features[0]
	if feat.Geometry.Coordinates[0] != -122.030581 || feat.Geometry.Coordinates[1] != 37.331800 || feat.Geometry.Coordinates[2] != 15.0 {
		t.Errorf("unexpected coordinates: %v", feat.Geometry.Coordinates)
	}

	props := feat.Properties
	if props["velocity"] != 4.5 || props["speed"] != 4.5 {
		t.Errorf("unexpected speed/velocity: %v", props["speed"])
	}
	if props["heading"] != 180.0 || props["course"] != 180.0 {
		t.Errorf("unexpected heading/course: %v", props["heading"])
	}
	if props["accuracy"] != 5.0 || props["horizontal_accuracy"] != 5.0 {
		t.Errorf("unexpected accuracy: %v", props["accuracy"])
	}
	if props["battery_level"] != 0.88 {
		t.Errorf("unexpected battery_level: %v", props["battery_level"])
	}
	if props["device_id"] != "Miki-iPhone" {
		t.Errorf("unexpected device_id: %v", props["device_id"])
	}
}
