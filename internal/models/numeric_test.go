package models_test

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

func TestNumericTimestampsKeepNanoseconds(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  string
	}{
		{json.Number("1750000000.123456789"), "2025-06-15T15:06:40.123456789Z"},
		{json.Number("1750000000123.456789"), "2025-06-15T15:06:40.123456789Z"},
		{json.Number("1.750000000123456789e9"), "2025-06-15T15:06:40.123456789Z"},
		{1750000000.125, "2025-06-15T15:06:40.125Z"},
		{"1750000000.125", "2025-06-15T15:06:40.125Z"},
		{json.Number("-0.125"), "1969-12-31T23:59:59.875Z"},
		{json.Number("-9223372036.854775808"), "1677-09-21T00:12:43.145224192Z"},
		{json.Number("9223372036854.775807"), "2262-04-11T23:47:16.854775807Z"},
	} {
		rec, err := models.FeatureToRecord(numericFeature("timestamp", tc.value))
		if err != nil {
			t.Errorf("%v: %v", tc.value, err)
			continue
		}
		if got := rec.Timestamp.Format(time.RFC3339Nano); got != tc.want {
			t.Errorf("%v: got %s, want %s", tc.value, got, tc.want)
		}
	}
	for _, value := range []interface{}{json.Number("9223372036854.775808"), json.Number("-9223372036.854775809"), json.Number("9999999999.999999999"), json.Number("1e999"), 1e30, "9999-01-01T00:00:00Z"} {
		if _, err := models.FeatureToRecord(numericFeature("timestamp", value)); err == nil {
			t.Errorf("accepted out-of-range timestamp %v", value)
		}
	}
}

func TestGeometryNullCoordinates(t *testing.T) {
	for _, coordinates := range []string{"[null,47]", "[8,null]", "null", "[8]"} {
		var feature models.LocationFeature
		err := json.Unmarshal([]byte(`{"geometry":{"type":"Point","coordinates":`+coordinates+`},"properties":{"timestamp":"2025-01-01T00:00:00Z"}}`), &feature)
		if err == nil {
			_, err = models.FeatureToRecord(&feature)
		}
		if err == nil {
			t.Errorf("accepted invalid coordinates %s", coordinates)
		}
	}
	var feature models.LocationFeature
	if err := json.Unmarshal([]byte(`{"geometry":{"type":"Point","coordinates":[8,47,null]},"properties":{"timestamp":"2025-01-01T00:00:00Z"}}`), &feature); err != nil {
		t.Fatal(err)
	}
	rec, err := models.FeatureToRecord(&feature)
	if err != nil || rec.Altitude != nil {
		t.Fatalf("null altitude: record=%+v error=%v", rec, err)
	}
}

func TestFeatureNumbersFromDecoder(t *testing.T) {
	var feature models.LocationFeature
	dec := json.NewDecoder(strings.NewReader(`{"geometry":{"type":"Point","coordinates":[8,47]},"properties":{"timestamp":1750000000.125,"speed":5.5,"locations_in_payload":2,"pauses":1,"battery_level":-1}}`))
	dec.UseNumber()
	if err := dec.Decode(&feature); err != nil {
		t.Fatal(err)
	}
	rec, err := models.FeatureToRecord(&feature)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Speed == nil || *rec.Speed != 5.5 || rec.LocationsInPayload != 2 || rec.Pauses == nil || !*rec.Pauses || rec.BatteryLevel != nil {
		t.Fatalf("numeric properties changed: %+v", rec)
	}
}

func TestFeatureRejectsCaseInsensitivePropertyCollisions(t *testing.T) {
	for i := 0; i < 100; i++ {
		feature := numericFeature("speed", 1.0)
		feature.Properties["SPEED"] = "NaN"
		if _, err := models.FeatureToRecord(feature); err == nil {
			t.Fatal("accepted conflicting normalized property keys")
		}
	}
}

func numericFeature(key string, value interface{}) *models.LocationFeature {
	return &models.LocationFeature{
		Type:       "Feature",
		Geometry:   models.Geometry{Type: "Point", Coordinates: []float64{8.5417, 47.3769}},
		Properties: map[string]interface{}{"timestamp": "2025-01-02T03:04:05.123456789Z", key: value},
	}
}

func TestFeatureToRecordRejectsMalformedNumbers(t *testing.T) {
	keys := []string{
		"altitude", "elevation", "height", "speed", "velocity", "course", "heading", "bearing", "direction",
		"horizontal_accuracy", "accuracy", "h_accuracy", "vertical_accuracy", "v_accuracy", "speed_accuracy",
		"course_accuracy", "battery_level", "desired_accuracy", "deferred", "locations_in_payload",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			for _, value := range []interface{}{"invalid", false, []interface{}{1}, "Inf", "-Inf", "NaN", math.Inf(1), math.Inf(-1), math.NaN()} {
				if _, err := models.FeatureToRecord(numericFeature(key, value)); err == nil {
					t.Errorf("accepted malformed %s=%v", key, value)
				}
			}
		})
	}
	for _, key := range []string{"altitude", "velocity"} {
		feature := numericFeature(key, "invalid")
		feature.Geometry.Coordinates = append(feature.Geometry.Coordinates, 10)
		feature.Properties["speed"] = 1.0
		if _, err := models.FeatureToRecord(feature); err == nil {
			t.Errorf("accepted malformed %s shadowed by another field", key)
		}
	}
	for _, value := range []interface{}{1.5, "1.5", 1e30} {
		if _, err := models.FeatureToRecord(numericFeature("locations_in_payload", value)); err == nil {
			t.Errorf("accepted invalid integer %v", value)
		}
	}
	for _, value := range []json.Number{"1.00000000000000001", "9007199254740992.5", "1e-999999999"} {
		if _, err := models.FeatureToRecord(numericFeature("locations_in_payload", value)); err == nil {
			t.Errorf("accepted fractional JSON integer %s", value)
		}
	}
	for _, value := range []json.Number{"2.0", "2e0", "200e-2"} {
		rec, err := models.FeatureToRecord(numericFeature("locations_in_payload", value))
		if err != nil || rec.LocationsInPayload != 2 {
			t.Errorf("integral JSON number %s: record=%+v error=%v", value, rec, err)
		}
	}
	if strconv.IntSize == 64 {
		for _, value := range []json.Number{"9007199254740993", "9007199254740993.0", "9.007199254740993e15"} {
			rec, err := models.FeatureToRecord(numericFeature("locations_in_payload", value))
			if err != nil || int64(rec.LocationsInPayload) != 9007199254740993 {
				t.Errorf("large integral JSON number %s: record=%+v error=%v", value, rec, err)
			}
		}
	}
}

func TestFeatureToRecordOptionalNumbers(t *testing.T) {
	fields := []struct {
		key string
		get func(*models.LocationRecord) *float64
	}{
		{"altitude", func(r *models.LocationRecord) *float64 { return r.Altitude }},
		{"speed", func(r *models.LocationRecord) *float64 { return r.Speed }},
		{"course", func(r *models.LocationRecord) *float64 { return r.Course }},
		{"horizontal_accuracy", func(r *models.LocationRecord) *float64 { return r.HorizontalAccuracy }},
		{"vertical_accuracy", func(r *models.LocationRecord) *float64 { return r.VerticalAccuracy }},
		{"speed_accuracy", func(r *models.LocationRecord) *float64 { return r.SpeedAccuracy }},
		{"course_accuracy", func(r *models.LocationRecord) *float64 { return r.CourseAccuracy }},
		{"battery_level", func(r *models.LocationRecord) *float64 { return r.BatteryLevel }},
		{"desired_accuracy", func(r *models.LocationRecord) *float64 { return r.DesiredAccuracy }},
		{"deferred", func(r *models.LocationRecord) *float64 { return r.Deferred }},
	}
	for _, field := range fields {
		t.Run(field.key, func(t *testing.T) {
			for _, value := range []interface{}{nil, 0.0, "0", -1.0, "-1", "12.5"} {
				rec, err := models.FeatureToRecord(numericFeature(field.key, value))
				if err != nil {
					t.Fatalf("%v: %v", value, err)
				}
				got := field.get(rec)
				if field.key == "battery_level" && (value == -1.0 || value == "-1") {
					if got != nil {
						t.Errorf("unknown battery level: got %v, want nil", got)
					}
					continue
				}
				if value == nil {
					if got != nil {
						t.Errorf("null became %v", *got)
					}
					continue
				}
				want := 0.0
				if value == -1.0 || value == "-1" {
					want = -1
				}
				if value == "12.5" {
					want = 12.5
				}
				if got == nil || *got != want {
					t.Errorf("%v: got %v, want %v", value, got, want)
				}
				if _, err := json.Marshal(models.RecordToFeature(rec)); err != nil {
					t.Errorf("accepted record cannot serialize: %v", err)
				}
			}
		})
	}
	feature := numericFeature("speed", nil)
	feature.Properties["velocity"] = "2.5"
	rec, err := models.FeatureToRecord(feature)
	if err != nil || rec.Speed == nil || *rec.Speed != 2.5 {
		t.Fatalf("null canonical key prevented alias fallback: record=%+v error=%v", rec, err)
	}
}

func TestRecordToFeaturePreservesTimestampPrecision(t *testing.T) {
	rec, err := models.FeatureToRecord(numericFeature("speed", 0.0))
	if err != nil {
		t.Fatal(err)
	}
	feature := models.RecordToFeature(rec)
	got, err := time.Parse(time.RFC3339Nano, feature.Properties["timestamp"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(rec.Timestamp) {
		t.Errorf("timestamp changed from %s to %s", rec.Timestamp.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano))
	}
}
