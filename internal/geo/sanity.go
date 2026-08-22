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

// ValidateLocation performs sanity checks on a LocationRecord.
// Returns an error if the data is malformed or physically impossible.
func ValidateLocation(loc *models.LocationRecord, now time.Time) error {
	if loc == nil {
		return fmt.Errorf("nil location record")
	}

	// Check coordinates
	if math.IsNaN(loc.Latitude) || math.IsInf(loc.Latitude, 0) || loc.Latitude < -90.0 || loc.Latitude > 90.0 {
		return fmt.Errorf("invalid latitude: %v (must be between -90 and 90)", loc.Latitude)
	}
	if math.IsNaN(loc.Longitude) || math.IsInf(loc.Longitude, 0) || loc.Longitude < -180.0 || loc.Longitude > 180.0 {
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
		if math.IsNaN(s) || math.IsInf(s, 0) {
			return fmt.Errorf("speed is NaN or Inf")
		}
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
		if math.IsNaN(c) || math.IsInf(c, 0) {
			return fmt.Errorf("course is NaN or Inf")
		}
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
		if math.IsNaN(h) || math.IsInf(h, 0) || (h < 0 && h != -1) {
			return fmt.Errorf("invalid horizontal_accuracy: %v", h)
		}
	}
	if loc.VerticalAccuracy != nil {
		v := *loc.VerticalAccuracy
		if math.IsNaN(v) || math.IsInf(v, 0) || (v < 0 && v != -1) {
			return fmt.Errorf("invalid vertical_accuracy: %v", v)
		}
	}
	if loc.SpeedAccuracy != nil {
		sa := *loc.SpeedAccuracy
		if math.IsNaN(sa) || math.IsInf(sa, 0) || (sa < 0 && sa != -1) {
			return fmt.Errorf("invalid speed_accuracy: %v", sa)
		}
	}
	if loc.CourseAccuracy != nil {
		ca := *loc.CourseAccuracy
		if math.IsNaN(ca) || math.IsInf(ca, 0) || (ca < 0 && ca != -1) {
			return fmt.Errorf("invalid course_accuracy: %v", ca)
		}
	}

	// Check battery level
	if loc.BatteryLevel != nil {
		b := *loc.BatteryLevel
		if math.IsNaN(b) || math.IsInf(b, 0) || b < 0.0 || b > 1.0 {
			return fmt.Errorf("invalid battery_level: %v (must be between 0.0 and 1.0)", b)
		}
	}

	return nil
}
