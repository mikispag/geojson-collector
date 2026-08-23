package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

func TestOverlandPayloadParsing(t *testing.T) {
	rawJSON := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [
          -122.030581, 
          37.331800
        ]
      },
      "properties": {
        "timestamp": "2015-10-01T08:00:00-0700",
        "altitude": 15,
        "speed": 4,
        "course": 180,
        "horizontal_accuracy": 30,
        "vertical_accuracy": -1,
        "speed_accuracy": 0.5,
        "course_accuracy": 2.0,
        "motion": ["driving","stationary"],
        "pauses": false,
        "activity": "other_navigation",
        "desired_accuracy": 100,
        "deferred": 1000,
        "significant_change": "disabled",
        "locations_in_payload": 1,
        "battery_state": "charging",
        "battery_level": 0.80,
        "device_id": "iphone-12",
        "unique_id": "apple-uuid-1234",
        "wifi": "HomeNet",
        "custom_meta": "extra_val"
      }
    }
  ],
  "current": {"state": "active"},
  "trip": {"distance": 500}
}`

	var payload models.OverlandPayload
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		t.Fatalf("failed to unmarshal OverlandPayload: %v", err)
	}

	if len(payload.Locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(payload.Locations))
	}

	rec, err := models.FeatureToRecord(&payload.Locations[0])
	if err != nil {
		t.Fatalf("FeatureToRecord failed: %v", err)
	}

	if rec.Latitude != 37.331800 {
		t.Errorf("expected lat 37.331800, got %f", rec.Latitude)
	}
	if rec.Longitude != -122.030581 {
		t.Errorf("expected lon -122.030581, got %f", rec.Longitude)
	}
	if rec.Altitude == nil || *rec.Altitude != 15 {
		t.Errorf("expected altitude 15, got %v", rec.Altitude)
	}
	if rec.Speed == nil || *rec.Speed != 4 {
		t.Errorf("expected speed 4, got %v", rec.Speed)
	}
	if rec.Course == nil || *rec.Course != 180 {
		t.Errorf("expected course 180, got %v", rec.Course)
	}
	if rec.HorizontalAccuracy == nil || *rec.HorizontalAccuracy != 30 {
		t.Errorf("expected horizontal_accuracy 30, got %v", rec.HorizontalAccuracy)
	}
	if rec.BatteryLevel == nil || *rec.BatteryLevel != 0.80 {
		t.Errorf("expected battery_level 0.80, got %v", rec.BatteryLevel)
	}
	if rec.BatteryState != "charging" {
		t.Errorf("expected battery_state charging, got %s", rec.BatteryState)
	}
	if rec.WiFi != "HomeNet" {
		t.Errorf("expected wifi HomeNet, got %s", rec.WiFi)
	}
	if rec.DeviceID != "iphone-12" {
		t.Errorf("expected device_id iphone-12, got %s", rec.DeviceID)
	}
	if rec.UniqueID != "apple-uuid-1234" {
		t.Errorf("expected unique_id apple-uuid-1234, got %s", rec.UniqueID)
	}
	if rec.Pauses == nil || *rec.Pauses != false {
		t.Errorf("expected pauses false, got %v", rec.Pauses)
	}
	if len(rec.Motion) != 2 || rec.Motion[0] != "driving" || rec.Motion[1] != "stationary" {
		t.Errorf("expected motion [driving stationary], got %v", rec.Motion)
	}
	if rec.ExtraProperties["custom_meta"] != "extra_val" {
		t.Errorf("expected extra_properties custom_meta, got %v", rec.ExtraProperties)
	}

	expectedTime, _ := time.Parse(time.RFC3339, "2015-10-01T15:00:00Z")
	if !rec.Timestamp.Equal(expectedTime) {
		t.Errorf("expected timestamp %v, got %v", expectedTime, rec.Timestamp)
	}

	// Test RecordToFeature
	feat := models.RecordToFeature(rec)
	if feat.Type != "Feature" {
		t.Errorf("expected feat type Feature, got %s", feat.Type)
	}
	if feat.Geometry.Type != "Point" {
		t.Errorf("expected geometry type Point, got %s", feat.Geometry.Type)
	}
	if len(feat.Geometry.Coordinates) != 3 {
		t.Errorf("expected 3 coordinates [lon, lat, alt], got %v", feat.Geometry.Coordinates)
	}
	if feat.Properties["velocity"] != 4.0 {
		t.Errorf("expected velocity 4, got %v", feat.Properties["velocity"])
	}
	if feat.Properties["heading"] != 180.0 {
		t.Errorf("expected heading 180, got %v", feat.Properties["heading"])
	}
	if feat.Properties["accuracy"] != 30.0 {
		t.Errorf("expected accuracy 30, got %v", feat.Properties["accuracy"])
	}
	if feat.Properties["timestamp"] != "2015-10-01T15:00:00Z" {
		t.Errorf("expected timestamp 2015-10-01T15:00:00Z, got %v", feat.Properties["timestamp"])
	}
	if feat.Properties["timestamp_iso"] != "2015-10-01T08:00:00-0700" {
		t.Errorf("expected timestamp_iso 2015-10-01T08:00:00-0700, got %v", feat.Properties["timestamp_iso"])
	}
	if feat.Properties["custom_meta"] != "extra_val" {
		t.Errorf("expected extra custom_meta preserved, got %v", feat.Properties["custom_meta"])
	}
}
