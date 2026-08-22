package models

import (
	"time"
)

// GeoJSONFeatureCollection represents an RFC 7946 FeatureCollection.
type GeoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []GeoJSONFeature `json:"features"`
}

// GeoJSONFeature represents an RFC 7946 Feature.
type GeoJSONFeature struct {
	Type       string                 `json:"type"`
	Geometry   GeoJSONGeometry        `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

// GeoJSONGeometry represents an RFC 7946 Point geometry.
type GeoJSONGeometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude] or [longitude, latitude, altitude]
}

// RecordToFeature converts an internal LocationRecord into an RFC 7946 GeoJSON Feature
// optimized with rich properties for Timelinize and standard GeoJSON consumers.
func RecordToFeature(rec *LocationRecord) GeoJSONFeature {
	coords := []float64{rec.Longitude, rec.Latitude}
	if rec.Altitude != nil {
		coords = append(coords, *rec.Altitude)
	}

	props := make(map[string]interface{})

	// Copy extra properties first so explicit fields take precedence
	for k, v := range rec.ExtraProperties {
		props[k] = v
	}

	// Standard timestamp in RFC3339 format
	if rec.TimestampISO != "" {
		props["timestamp"] = rec.TimestampISO
	} else {
		props["timestamp"] = rec.Timestamp.UTC().Format(time.RFC3339)
	}

	if rec.Altitude != nil {
		props["altitude"] = *rec.Altitude
	}
	if rec.Speed != nil {
		props["speed"] = *rec.Speed
		// Timelinize recognizes "velocity"
		props["velocity"] = *rec.Speed
	}
	if rec.Course != nil {
		props["course"] = *rec.Course
		// Timelinize recognizes "heading"
		props["heading"] = *rec.Course
	}
	if rec.HorizontalAccuracy != nil {
		props["horizontal_accuracy"] = *rec.HorizontalAccuracy
		// Timelinize recognizes "accuracy"
		props["accuracy"] = *rec.HorizontalAccuracy
	}
	if rec.VerticalAccuracy != nil {
		props["vertical_accuracy"] = *rec.VerticalAccuracy
	}
	if rec.SpeedAccuracy != nil {
		props["speed_accuracy"] = *rec.SpeedAccuracy
	}
	if rec.CourseAccuracy != nil {
		props["course_accuracy"] = *rec.CourseAccuracy
	}
	if len(rec.Motion) > 0 {
		props["motion"] = rec.Motion
	}
	if rec.BatteryState != "" {
		props["battery_state"] = rec.BatteryState
	}
	if rec.BatteryLevel != nil {
		props["battery_level"] = *rec.BatteryLevel
	}
	if rec.WiFi != "" {
		props["wifi"] = rec.WiFi
	}
	if rec.DeviceID != "" {
		props["device_id"] = rec.DeviceID
	}
	if rec.UniqueID != "" {
		props["unique_id"] = rec.UniqueID
	}
	if rec.Pauses != nil {
		props["pauses"] = *rec.Pauses
	}
	if rec.Activity != "" {
		props["activity"] = rec.Activity
	}
	if rec.DesiredAccuracy != nil {
		props["desired_accuracy"] = *rec.DesiredAccuracy
	}
	if rec.Deferred != nil {
		props["deferred"] = *rec.Deferred
	}
	if rec.SignificantChange != "" {
		props["significant_change"] = rec.SignificantChange
	}
	if rec.LocationsInPayload > 0 {
		props["locations_in_payload"] = rec.LocationsInPayload
	}

	return GeoJSONFeature{
		Type: "Feature",
		Geometry: GeoJSONGeometry{
			Type:        "Point",
			Coordinates: coords,
		},
		Properties: props,
	}
}
