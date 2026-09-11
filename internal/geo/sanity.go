package geo

import (
	"fmt"
	"math"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

const (
	// MaxRealisticSpeedMetersPerSec is Mach 3 (~1020 m/s or ~3670 km/h) to filter corrupted telemetry.
	MaxRealisticSpeedMetersPerSec = 1000.0

	// MaxFutureAllowed is the maximum amount of clock drift into the future accepted.
	MaxFutureAllowed = 24 * time.Hour
)

var (
	// MinAllowedTimestamp is year 2000 UTC.
	MinAllowedTimestamp = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
)

// ValidateFiniteNumbers rejects numeric telemetry that cannot be represented in JSON.
func ValidateFiniteNumbers(loc *models.LocationRecord) error {
	if loc == nil {
		return fmt.Errorf("nil location record")
	}
	for _, field := range []struct {
		name  string
		value *float64
	}{
		{"latitude", &loc.Latitude},
		{"longitude", &loc.Longitude},
		{"altitude", loc.Altitude},
		{"speed", loc.Speed},
		{"course", loc.Course},
		{"horizontal_accuracy", loc.HorizontalAccuracy},
		{"vertical_accuracy", loc.VerticalAccuracy},
		{"speed_accuracy", loc.SpeedAccuracy},
		{"course_accuracy", loc.CourseAccuracy},
		{"battery_level", loc.BatteryLevel},
		{"desired_accuracy", loc.DesiredAccuracy},
		{"deferred", loc.Deferred},
	} {
		if field.value != nil && (math.IsNaN(*field.value) || math.IsInf(*field.value, 0)) {
			return fmt.Errorf("invalid %s: nonfinite number %v", field.name, *field.value)
		}
	}
	return nil
}

// ValidateLocation performs sanity checks on a LocationRecord.
// Returns an error if the data is malformed or physically impossible.
func ValidateLocation(loc *models.LocationRecord, now time.Time) error {
	if err := ValidateFiniteNumbers(loc); err != nil {
		return err
	}

	// Check coordinates
	if loc.Latitude < -90.0 || loc.Latitude > 90.0 {
		return fmt.Errorf("invalid latitude: %v (must be between -90 and 90)", loc.Latitude)
	}
	if loc.Longitude < -180.0 || loc.Longitude > 180.0 {
		return fmt.Errorf("invalid longitude: %v (must be between -180 and 180)", loc.Longitude)
	}

	// Check timestamp
	if loc.Timestamp.IsZero() {
		return fmt.Errorf("empty or zero timestamp")
	}
	if loc.Timestamp.Before(MinAllowedTimestamp) {
		return fmt.Errorf("timestamp %v is unreasonably far in the past (before 2000-01-01)", loc.Timestamp)
	}
	if !now.IsZero() && loc.Timestamp.After(now.Add(MaxFutureAllowed)) {
		return fmt.Errorf("timestamp %v is unreasonably in the future (current time: %v)", loc.Timestamp, now)
	}

	// Check speed
	if loc.Speed != nil {
		s := *loc.Speed
		if s < 0 && s != -1 {
			return fmt.Errorf("negative speed %v", s)
		}
		if s > MaxRealisticSpeedMetersPerSec {
			return fmt.Errorf("unrealistically high speed: %v m/s (> %v m/s)", s, MaxRealisticSpeedMetersPerSec)
		}
	}

	// Check course
	if loc.Course != nil {
		c := *loc.Course
		if c < 0 && c != -1 {
			return fmt.Errorf("negative course %v", c)
		}
		if c > 360.0 {
			return fmt.Errorf("course %v exceeds 360 degrees", c)
		}
	}

	// Check accuracies
	if loc.HorizontalAccuracy != nil {
		h := *loc.HorizontalAccuracy
		if h < 0 && h != -1 {
			return fmt.Errorf("invalid horizontal_accuracy: %v", h)
		}
	}
	if loc.VerticalAccuracy != nil {
		v := *loc.VerticalAccuracy
		if v < 0 && v != -1 {
			return fmt.Errorf("invalid vertical_accuracy: %v", v)
		}
	}
	if loc.SpeedAccuracy != nil {
		sa := *loc.SpeedAccuracy
		if sa < 0 && sa != -1 {
			return fmt.Errorf("invalid speed_accuracy: %v", sa)
		}
	}
	if loc.CourseAccuracy != nil {
		ca := *loc.CourseAccuracy
		if ca < 0 && ca != -1 {
			return fmt.Errorf("invalid course_accuracy: %v", ca)
		}
	}

	// Check battery level
	if loc.BatteryLevel != nil {
		b := *loc.BatteryLevel
		if b == -1 {
			loc.BatteryLevel = nil
		} else if b < 0.0 || b > 1.0 {
			return fmt.Errorf("invalid battery_level: %v (must be between 0.0 and 1.0)", b)
		}
	}

	return nil
}
