package models

import (
	"encoding/json"
	"fmt"
	"math"
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

// UnmarshalJSON distinguishes an absent coordinate from a numeric zero.
func (g *Geometry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type        string     `json:"type"`
		Coordinates []*float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Coordinates) < 2 || raw.Coordinates[0] == nil || raw.Coordinates[1] == nil {
		return fmt.Errorf("invalid coordinates: longitude and latitude must be numbers")
	}
	coordinates := make([]float64, 0, len(raw.Coordinates))
	for i, value := range raw.Coordinates {
		if value == nil {
			if i == 2 && len(raw.Coordinates) == 3 {
				break // Optional altitude may be unknown.
			}
			return fmt.Errorf("invalid null coordinate at index %d", i)
		}
		coordinates = append(coordinates, *value)
	}
	g.Type, g.Coordinates = raw.Type, coordinates
	return nil
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
	for _, coordinate := range f.Geometry.Coordinates {
		if math.IsNaN(coordinate) || math.IsInf(coordinate, 0) {
			return nil, fmt.Errorf("invalid nonfinite coordinate: %v", coordinate)
		}
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
		key := strings.ToLower(k)
		if _, exists := normProps[key]; exists {
			return nil, fmt.Errorf("duplicate property after case normalization: %q", key)
		}
		normProps[key] = v
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

	var propertyAlt, batteryLevelPtr, desiredAccPtr, deferredPtr *float64
	var speedPtr, coursePtr, hAccPtr, vAccPtr, speedAccPtr, courseAccPtr *float64
	for _, field := range []struct {
		target **float64
		keys   []string
	}{
		{&propertyAlt, []string{"altitude", "elevation", "height"}},
		{&speedPtr, []string{"speed", "velocity"}},
		{&coursePtr, []string{"course", "heading", "bearing", "direction"}},
		{&hAccPtr, []string{"horizontal_accuracy", "accuracy", "h_accuracy"}},
		{&vAccPtr, []string{"vertical_accuracy", "v_accuracy"}},
		{&speedAccPtr, []string{"speed_accuracy"}},
		{&courseAccPtr, []string{"course_accuracy"}},
		{&batteryLevelPtr, []string{"battery_level"}},
		{&desiredAccPtr, []string{"desired_accuracy"}},
		{&deferredPtr, []string{"deferred"}},
	} {
		value, err := getOptionalFloat(normProps, field.keys...)
		if err != nil {
			return nil, err
		}
		*field.target = value
	}
	if batteryLevelPtr != nil && *batteryLevelPtr == -1 {
		batteryLevelPtr = nil // UIDevice reports -1 when the battery level is unknown.
	}
	if alt == nil {
		alt = propertyAlt
	}

	motion := getStringSlice(normProps, "motion")
	batteryState := getString(normProps, "battery_state")
	wifi := getString(normProps, "wifi")
	deviceID := getString(normProps, "device_id")
	uniqueID := getString(normProps, "unique_id")
	activity := getString(normProps, "activity")
	sigChange := getString(normProps, "significant_change")

	var pausesPtr *bool
	if p, ok := getBool(normProps, "pauses"); ok {
		pausesPtr = &p
	}

	locInPayload, err := getInt(normProps, "locations_in_payload")
	if err != nil {
		return nil, err
	}

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
	var number string
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
				if !time.Unix(0, t.UnixNano()).Equal(t) {
					return time.Time{}, "", fmt.Errorf("timestamp outside nanosecond storage range: %q", v)
				}
				return t, v, nil
			}
		}
		number = v
	case json.Number:
		number = v.String()
	case float64:
		number = strconv.FormatFloat(v, 'g', -1, 64)
	case int64:
		number = strconv.FormatInt(v, 10)
	case int:
		number = strconv.Itoa(v)
	default:
		return time.Time{}, "", fmt.Errorf("unsupported timestamp type: %T", val)
	}
	t, err := parseUnixTime(number)
	return t, t.Format(time.RFC3339Nano), err
}

func parseUnixTime(raw string) (time.Time, error) {
	negative, digits, integerDigits, err := decimalParts(raw)
	if err != nil {
		return time.Time{}, err
	}
	scale := 9 // Values below 10^10 are seconds; larger values are milliseconds.
	if !negative && integerDigits > 10 {
		scale = 6
	}
	nanoseconds, err := decimalInteger(negative, digits, integerDigits+scale)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp outside nanosecond storage range: %q", raw)
	}
	return time.Unix(0, nanoseconds).UTC(), nil
}

func decimalParts(raw string) (negative bool, digits string, integerDigits int, err error) {
	// Parse the decimal digits directly: converting through float64 loses
	// nanoseconds in current Unix timestamps, including fractional milliseconds.
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return false, "", 0, fmt.Errorf("invalid decimal number %q", raw)
	}
	negative = strings.HasPrefix(raw, "-")
	digits = strings.TrimPrefix(strings.TrimPrefix(raw, "-"), "+")
	exponent := 0
	if i := strings.IndexAny(digits, "eE"); i >= 0 {
		exponent, err = strconv.Atoi(digits[i+1:])
		if err != nil || exponent > len(raw)+20 || exponent < -len(raw)-20 {
			return false, "", 0, fmt.Errorf("decimal exponent out of range: %q", raw)
		}
		digits = digits[:i]
	}
	fractionDigits := 0
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		fractionDigits = len(digits) - i - 1
		digits = digits[:i] + digits[i+1:]
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return false, "", 0, fmt.Errorf("invalid decimal number %q", raw)
		}
	}
	digits = strings.TrimLeft(digits, "0")
	return negative, digits, len(digits) + exponent - fractionDigits, nil
}

// decimalInteger truncates a decimal to the requested number of integer digits.
func decimalInteger(negative bool, digits string, length int) (int64, error) {
	if digits == "" || length <= 0 {
		return 0, nil
	}
	if length > 19 {
		return 0, fmt.Errorf("decimal number outside int64 range")
	}
	if length > len(digits) {
		digits += strings.Repeat("0", length-len(digits))
	} else {
		digits = digits[:length]
	}
	if negative {
		digits = "-" + digits
	}
	return strconv.ParseInt(digits, 10, 64)
}

func getProp(m map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, k := range keys {
		if v, ok := m[strings.ToLower(k)]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func getOptionalFloat(m map[string]interface{}, keys ...string) (*float64, error) {
	var result *float64
	for _, key := range keys {
		val, ok := getProp(m, key)
		if !ok {
			continue
		}
		var number float64
		switch v := val.(type) {
		case float64:
			number = v
		case float32:
			number = float64(v)
		case int:
			number = float64(v)
		case int64:
			number = float64(v)
		case json.Number:
			parsed, err := v.Float64()
			if err != nil {
				return nil, fmt.Errorf("invalid %s: %w", key, err)
			}
			number = parsed
		case string:
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid %s: %w", key, err)
			}
			number = parsed
		default:
			return nil, fmt.Errorf("invalid %s: expected number, got %T", key, val)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("invalid %s: nonfinite number %v", key, number)
		}
		// Validate every supplied alias, keeping the first non-null value.
		if result == nil {
			result = &number
		}
	}
	return result, nil
}

func getInt(m map[string]interface{}, keys ...string) (int, error) {
	val, ok := getProp(m, keys...)
	if !ok {
		return 0, nil
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		if int64(int(v)) == v {
			return int(v), nil
		}
	case float64:
		limit := math.Ldexp(1, strconv.IntSize-1)
		if !math.IsNaN(v) && v >= -limit && v < limit && math.Trunc(v) == v {
			return int(v), nil
		}
	case json.Number:
		negative, digits, integerDigits, err := decimalParts(v.String())
		if err == nil && (integerDigits >= len(digits) || strings.Trim(digits[max(integerDigits, 0):], "0") == "") {
			if i, err := decimalInteger(negative, digits, integerDigits); err == nil && int64(int(i)) == i {
				return int(i), nil
			}
		}
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i, nil
		}
	}
	return 0, fmt.Errorf("invalid %s: expected integer, got %v", keys[0], val)
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
	case json.Number:
		if number, err := v.Float64(); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
			return number != 0, true
		}
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
