package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OverlandPayload represents the root JSON payload sent by the iOS Overland app.
type OverlandPayload struct {
	Locations []LocationFeature `json:"locations"`
	Current   *json.RawMessage  `json:"current,omitempty"`
	Trip      *json.RawMessage  `json:"trip,omitempty"`
}

// LocationFeature represents a single GeoJSON Feature sent in the Overland payload.
type LocationFeature struct {
	Type       string                 `json:"type"`
	Geometry   Geometry               `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

// Geometry represents a GeoJSON geometry object (Point).
type Geometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude] or [longitude, latitude, altitude]
}

// LocationRecord represents our internal strongly-typed location model.
type LocationRecord struct {
	ID                 int64                  `json:"id,omitempty"`
	Timestamp          time.Time              `json:"timestamp"`
	TimestampISO       string                 `json:"timestamp_iso"`
	Latitude           float64                `json:"latitude"`
	Longitude          float64                `json:"longitude"`
	Altitude           *float64               `json:"altitude,omitempty"`
	Speed              *float64               `json:"speed,omitempty"`
	Course             *float64               `json:"course,omitempty"`
	HorizontalAccuracy *float64               `json:"horizontal_accuracy,omitempty"`
	VerticalAccuracy   *float64               `json:"vertical_accuracy,omitempty"`
	SpeedAccuracy      *float64               `json:"speed_accuracy,omitempty"`
	CourseAccuracy     *float64               `json:"course_accuracy,omitempty"`
	Motion             []string               `json:"motion,omitempty"`
	BatteryState       string                 `json:"battery_state,omitempty"`
	BatteryLevel       *float64               `json:"battery_level,omitempty"`
	WiFi               string                 `json:"wifi,omitempty"`
	DeviceID           string                 `json:"device_id,omitempty"`
	UniqueID           string                 `json:"unique_id,omitempty"`
	Pauses             *bool                  `json:"pauses,omitempty"`
	Activity           string                 `json:"activity,omitempty"`
	DesiredAccuracy    *float64               `json:"desired_accuracy,omitempty"`
	Deferred           *float64               `json:"deferred,omitempty"`
	SignificantChange  string                 `json:"significant_change,omitempty"`
	LocationsInPayload int                    `json:"locations_in_payload,omitempty"`
	ExtraProperties    map[string]interface{} `json:"extra_properties,omitempty"`
}

// FeatureToRecord converts a GeoJSON LocationFeature from Overland into a LocationRecord.
func FeatureToRecord(f *LocationFeature) (*LocationRecord, error) {
	if f == nil {
		return nil, fmt.Errorf("nil feature")
	}

	if len(f.Geometry.Coordinates) < 2 {
		return nil, fmt.Errorf("invalid coordinates: expected at least [lon, lat], got %v", f.Geometry.Coordinates)
	}

	lon := f.Geometry.Coordinates[0]
	lat := f.Geometry.Coordinates[1]

	var alt *float64
	if len(f.Geometry.Coordinates) >= 3 {
		v := f.Geometry.Coordinates[2]
		alt = &v
	}

	props := f.Properties
	if props == nil {
		props = make(map[string]interface{})
	}

	// Normalized property lookup map
	normProps := make(map[string]interface{}, len(props))
	extraProps := make(map[string]interface{})
	for k, v := range props {
		normProps[strings.ToLower(k)] = v
	}

	// Parse Timestamp
	var ts time.Time
	var tsISO string

	if val, ok := getProp(normProps, "timestamp", "time", "time_long", "datetime", "date_time"); ok {
		parsed, rawISO, err := parseTimestamp(val)
		if err != nil {
			return nil, fmt.Errorf("parsing timestamp: %w", err)
		}
		ts = parsed.UTC()
		tsISO = rawISO
	} else {
		return nil, fmt.Errorf("missing timestamp property")
	}

	// Parse Altitude from properties if not in coordinates
	if alt == nil {
		if v, ok := getFloat(normProps, "altitude", "elevation", "height"); ok {
			alt = &v
		}
	}

	speed, _ := getFloat(normProps, "speed", "velocity")
	course, _ := getFloat(normProps, "course", "heading", "bearing", "direction")
	hAcc, _ := getFloat(normProps, "horizontal_accuracy", "accuracy", "h_accuracy")
	vAcc, _ := getFloat(normProps, "vertical_accuracy", "v_accuracy")
	speedAcc, _ := getFloat(normProps, "speed_accuracy")
	courseAcc, _ := getFloat(normProps, "course_accuracy")

	var speedPtr, coursePtr, hAccPtr, vAccPtr, speedAccPtr, courseAccPtr *float64
	if hasProp(normProps, "speed", "velocity") {
		speedPtr = &speed
	}
	if hasProp(normProps, "course", "heading", "bearing", "direction") {
		coursePtr = &course
	}
	if hasProp(normProps, "horizontal_accuracy", "accuracy", "h_accuracy") {
		hAccPtr = &hAcc
	}
	if hasProp(normProps, "vertical_accuracy", "v_accuracy") {
		vAccPtr = &vAcc
	}
	if hasProp(normProps, "speed_accuracy") {
		speedAccPtr = &speedAcc
	}
	if hasProp(normProps, "course_accuracy") {
		courseAccPtr = &courseAcc
	}

	motion := getStringSlice(normProps, "motion")
	batteryState := getString(normProps, "battery_state")
	wifi := getString(normProps, "wifi")
	deviceID := getString(normProps, "device_id")
	uniqueID := getString(normProps, "unique_id")
	activity := getString(normProps, "activity")
	sigChange := getString(normProps, "significant_change")

	var batteryLevelPtr, desiredAccPtr, deferredPtr *float64
	if bLevel, ok := getFloat(normProps, "battery_level"); ok {
		batteryLevelPtr = &bLevel
	}
	if dAcc, ok := getFloat(normProps, "desired_accuracy"); ok {
		desiredAccPtr = &dAcc
	}
	if def, ok := getFloat(normProps, "deferred"); ok {
		deferredPtr = &def
	}

	var pausesPtr *bool
	if p, ok := getBool(normProps, "pauses"); ok {
		pausesPtr = &p
	}

	locInPayload := getInt(normProps, "locations_in_payload")

	// Collect extra unknown properties
	knownKeys := map[string]bool{
		"timestamp":            true,
		"time":                 true,
		"time_long":            true,
		"datetime":             true,
		"date_time":            true,
		"altitude":             true,
		"elevation":            true,
		"height":               true,
		"speed":                true,
		"velocity":             true,
		"course":               true,
		"heading":              true,
		"bearing":              true,
		"direction":            true,
		"horizontal_accuracy":  true,
		"accuracy":             true,
		"h_accuracy":           true,
		"vertical_accuracy":    true,
		"v_accuracy":           true,
		"speed_accuracy":       true,
		"course_accuracy":      true,
		"motion":               true,
		"battery_state":        true,
		"battery_level":        true,
		"wifi":                 true,
		"device_id":            true,
		"unique_id":            true,
		"pauses":               true,
		"activity":             true,
		"desired_accuracy":     true,
		"deferred":             true,
		"significant_change":   true,
		"locations_in_payload": true,
	}

	for k, v := range props {
		if !knownKeys[strings.ToLower(k)] {
			extraProps[k] = v
		}
	}

	return &LocationRecord{
		Timestamp:          ts,
		TimestampISO:       tsISO,
		Latitude:           lat,
		Longitude:          lon,
		Altitude:           alt,
		Speed:              speedPtr,
		Course:             coursePtr,
		HorizontalAccuracy: hAccPtr,
		VerticalAccuracy:   vAccPtr,
		SpeedAccuracy:      speedAccPtr,
		CourseAccuracy:     courseAccPtr,
		Motion:             motion,
		BatteryState:       batteryState,
		BatteryLevel:       batteryLevelPtr,
		WiFi:               wifi,
		DeviceID:           deviceID,
		UniqueID:           uniqueID,
		Pauses:             pausesPtr,
		Activity:           activity,
		DesiredAccuracy:    desiredAccPtr,
		Deferred:           deferredPtr,
		SignificantChange:  sigChange,
		LocationsInPayload: locInPayload,
		ExtraProperties:    extraProps,
	}, nil
}

func parseTimestamp(val interface{}) (time.Time, string, error) {
	switch v := val.(type) {
	case string:
		formats := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05-0700",
			"2006-01-02T15:04:05.999999999-0700",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			time.RFC822,
			time.RFC822Z,
			time.RFC1123,
			time.RFC1123Z,
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, v, nil
			}
		}
		if intVal, err := strconv.ParseInt(v, 10, 64); err == nil {
			t := parseUnixTime(intVal)
			return t, t.Format(time.RFC3339), nil
		}
		return time.Time{}, "", fmt.Errorf("cannot parse string timestamp %q", v)
	case float64:
		t := parseUnixTime(int64(v))
		return t, t.Format(time.RFC3339), nil
	case int64:
		t := parseUnixTime(v)
		return t, t.Format(time.RFC3339), nil
	case int:
		t := parseUnixTime(int64(v))
		return t, t.Format(time.RFC3339), nil
	default:
		return time.Time{}, "", fmt.Errorf("unsupported timestamp type: %T", val)
	}
}

func parseUnixTime(val int64) time.Time {
	const year2286Sec = 10000000000
	if val < year2286Sec {
		return time.Unix(val, 0).UTC()
	}
	return time.UnixMilli(val).UTC()
}

func getProp(m map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, k := range keys {
		if v, ok := m[strings.ToLower(k)]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func hasProp(m map[string]interface{}, keys ...string) bool {
	_, ok := getProp(m, keys...)
	return ok
}

func getFloat(m map[string]interface{}, keys ...string) (float64, bool) {
	val, ok := getProp(m, keys...)
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func getInt(m map[string]interface{}, keys ...string) int {
	val, ok := getProp(m, keys...)
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

func getString(m map[string]interface{}, keys ...string) string {
	val, ok := getProp(m, keys...)
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getBool(m map[string]interface{}, keys ...string) (bool, bool) {
	val, ok := getProp(m, keys...)
	if !ok {
		return false, false
	}
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b, true
		}
	case int:
		return v != 0, true
	case float64:
		return v != 0, true
	}
	return false, false
}

func getStringSlice(m map[string]interface{}, keys ...string) []string {
	val, ok := getProp(m, keys...)
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []interface{}:
		res := make([]string, 0, len(v))
		for _, elem := range v {
			if s, ok := elem.(string); ok {
				res = append(res, s)
			}
		}
		return res
	case []string:
		return v
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return nil
}
