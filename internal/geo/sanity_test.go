package geo_test

import (
	"math"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/geo"
	"github.com/mikispag/geojson-collector/internal/models"
)

func TestValidateLocationRejectsEveryNonfiniteNumber(t *testing.T) {
	fields := []struct {
		name string
		set  func(*models.LocationRecord, float64)
	}{
		{"latitude", func(r *models.LocationRecord, v float64) { r.Latitude = v }},
		{"longitude", func(r *models.LocationRecord, v float64) { r.Longitude = v }},
		{"altitude", func(r *models.LocationRecord, v float64) { r.Altitude = &v }},
		{"speed", func(r *models.LocationRecord, v float64) { r.Speed = &v }},
		{"course", func(r *models.LocationRecord, v float64) { r.Course = &v }},
		{"horizontal_accuracy", func(r *models.LocationRecord, v float64) { r.HorizontalAccuracy = &v }},
		{"vertical_accuracy", func(r *models.LocationRecord, v float64) { r.VerticalAccuracy = &v }},
		{"speed_accuracy", func(r *models.LocationRecord, v float64) { r.SpeedAccuracy = &v }},
		{"course_accuracy", func(r *models.LocationRecord, v float64) { r.CourseAccuracy = &v }},
		{"battery_level", func(r *models.LocationRecord, v float64) { r.BatteryLevel = &v }},
		{"desired_accuracy", func(r *models.LocationRecord, v float64) { r.DesiredAccuracy = &v }},
		{"deferred", func(r *models.LocationRecord, v float64) { r.Deferred = &v }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
				rec := &models.LocationRecord{Timestamp: time.Now().UTC(), Latitude: 47.3769, Longitude: 8.5417}
				field.set(rec, value)
				if err := geo.ValidateLocation(rec, time.Now().UTC()); err == nil {
					t.Errorf("accepted %s=%v", field.name, value)
				}
			}
		})
	}
}

func TestValidateLocationPreservesNegativeSentinels(t *testing.T) {
	unknown, altitude, desiredAccuracy := -1.0, -50.0, -2.0
	rec := &models.LocationRecord{
		Timestamp: time.Now().UTC(), Latitude: 47.3769, Longitude: 8.5417,
		Altitude: &altitude, Speed: &unknown, Course: &unknown, HorizontalAccuracy: &unknown,
		VerticalAccuracy: &unknown, SpeedAccuracy: &unknown, CourseAccuracy: &unknown,
		DesiredAccuracy: &desiredAccuracy, Deferred: &unknown,
		BatteryLevel: &unknown,
	}
	if err := geo.ValidateLocation(rec, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if rec.BatteryLevel != nil {
		t.Fatal("unknown battery level was not normalized")
	}
}
