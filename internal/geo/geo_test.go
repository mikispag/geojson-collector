package geo_test

import (
	"math"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/geo"
	"github.com/mikispag/geojson-collector/internal/models"
)

func TestHaversineDistance(t *testing.T) {
	// Distance between Zurich (47.3769, 8.5417) and Bern (46.9480, 7.4474) is ~95.6 km
	zurichLat, zurichLon := 47.3769, 8.5417
	bernLat, bernLon := 46.9480, 7.4474

	dist := geo.HaversineDistance(zurichLat, zurichLon, bernLat, bernLon)
	if dist < 95000 || dist > 97000 {
		t.Errorf("expected ~95.6 km, got %.2f m", dist)
	}

	// Same point distance should be exactly 0
	zeroDist := geo.HaversineDistance(zurichLat, zurichLon, zurichLat, zurichLon)
	if zeroDist != 0.0 {
		t.Errorf("expected 0, got %f", zeroDist)
	}

	// Small distance (approx 1 meter in latitude)
	// 1 degree latitude ~ 111,139 meters => 1m ~ 0.00000899 degrees
	lat2 := zurichLat + 0.00000899
	oneMeterDist := geo.HaversineDistance(zurichLat, zurichLon, lat2, zurichLon)
	if math.Abs(oneMeterDist-1.0) > 0.05 {
		t.Errorf("expected ~1.0m, got %.3fm", oneMeterDist)
	}
}

func TestValidateLocation(t *testing.T) {
	now := time.Now().UTC()

	speed := 25.0
	course := 90.0
	battery := 0.8

	validLoc := &models.LocationRecord{
		Timestamp:    now,
		Latitude:     47.3769,
		Longitude:    8.5417,
		Speed:        &speed,
		Course:       &course,
		BatteryLevel: &battery,
	}

	if err := geo.ValidateLocation(validLoc, now); err != nil {
		t.Errorf("expected valid location, got err: %v", err)
	}

	// Test invalid latitude
	badLat := *validLoc
	badLat.Latitude = 95.0
	if err := geo.ValidateLocation(&badLat, now); err == nil {
		t.Error("expected error for lat > 90, got nil")
	}

	// Test invalid longitude
	badLon := *validLoc
	badLon.Longitude = -185.0
	if err := geo.ValidateLocation(&badLon, now); err == nil {
		t.Error("expected error for lon < -180, got nil")
	}

	// Test far future timestamp
	futureLoc := *validLoc
	futureLoc.Timestamp = now.Add(48 * time.Hour)
	if err := geo.ValidateLocation(&futureLoc, now); err == nil {
		t.Error("expected error for far future timestamp, got nil")
	}

	// Test ancient timestamp
	pastLoc := *validLoc
	pastLoc.Timestamp = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := geo.ValidateLocation(&pastLoc, now); err == nil {
		t.Error("expected error for ancient timestamp, got nil")
	}

	// Test impossible speed (> Mach 3)
	badSpeed := *validLoc
	hyperSpeed := 2000.0
	badSpeed.Speed = &hyperSpeed
	if err := geo.ValidateLocation(&badSpeed, now); err == nil {
		t.Error("expected error for supersonic speed, got nil")
	}

	// Test invalid battery
	badBattery := *validLoc
	overBattery := 1.5
	badBattery.BatteryLevel = &overBattery
	if err := geo.ValidateLocation(&badBattery, now); err == nil {
		t.Error("expected error for battery > 1.0, got nil")
	}
}

func TestFindDuplicate(t *testing.T) {
	t0 := time.Now().UTC()

	existing := []models.LocationRecord{
		{
			Timestamp: t0,
			Latitude:  47.376900,
			Longitude: 8.541700,
			DeviceID:  "device-A",
		},
	}

	// Duplicate: same location 10 seconds later, same device
	cand1 := &models.LocationRecord{
		Timestamp: t0.Add(10 * time.Second),
		Latitude:  47.376900,
		Longitude: 8.541700,
		DeviceID:  "device-A",
	}

	dup := geo.FindDuplicate(existing, cand1, 1.0, 1*time.Minute)
	if dup == nil {
		t.Fatal("expected duplicate to be found, got nil")
	}
	if dup.TimeDiff != 10*time.Second {
		t.Errorf("expected time diff 10s, got %v", dup.TimeDiff)
	}

	// Non-duplicate: same location 10 seconds later, but DIFFERENT device
	candDiffDevice := &models.LocationRecord{
		Timestamp: t0.Add(10 * time.Second),
		Latitude:  47.376900,
		Longitude: 8.541700,
		DeviceID:  "device-B",
	}
	if d := geo.FindDuplicate(existing, candDiffDevice, 1.0, 1*time.Minute); d != nil {
		t.Errorf("expected different device not to be deduplicated, got %+v", d)
	}

	// Non-duplicate: moved 50 meters away
	cand2 := &models.LocationRecord{
		Timestamp: t0.Add(10 * time.Second),
		Latitude:  47.377300, // ~44m away
		Longitude: 8.541700,
		DeviceID:  "device-A",
	}
	if d := geo.FindDuplicate(existing, cand2, 1.0, 1*time.Minute); d != nil {
		t.Errorf("expected no duplicate for 44m move, got %+v", d)
	}

	// Non-duplicate: same location but 5 minutes later (> 1 minute interval)
	cand3 := &models.LocationRecord{
		Timestamp: t0.Add(5 * time.Minute),
		Latitude:  47.376900,
		Longitude: 8.541700,
		DeviceID:  "device-A",
	}
	if d := geo.FindDuplicate(existing, cand3, 1.0, 1*time.Minute); d != nil {
		t.Errorf("expected no duplicate for 5 min dt, got %+v", d)
	}
}
