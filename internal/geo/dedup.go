package geo

import (
	"math"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

// DuplicateMatch contains details about a detected duplicate point.
type DuplicateMatch struct {
	MatchedRecord models.LocationRecord
	DistanceM     float64
	TimeDiff      time.Duration
}

// FindDuplicate checks if candidate is within radiusMeters of any point in existingRecords
// within the time delta window. Returns a pointer to DuplicateMatch if a duplicate is found.
func FindDuplicate(existingRecords []models.LocationRecord, candidate *models.LocationRecord, radiusMeters float64, interval time.Duration) *DuplicateMatch {
	if candidate == nil || len(existingRecords) == 0 {
		return nil
	}

	candTime := candidate.Timestamp

	for _, rec := range existingRecords {
		// Only deduplicate points from the same device if device_id is specified
		if candidate.DeviceID != "" && rec.DeviceID != "" && candidate.DeviceID != rec.DeviceID {
			continue
		}

		// Calculate time difference
		var dt time.Duration
		if rec.Timestamp.After(candTime) {
			dt = rec.Timestamp.Sub(candTime)
		} else {
			dt = candTime.Sub(rec.Timestamp)
		}

		if dt > interval {
			continue
		}

		// Calculate spatial distance
		dist := HaversineDistance(candidate.Latitude, candidate.Longitude, rec.Latitude, rec.Longitude)

		// Fast path for floating point rounding or equal points
		if dist <= radiusMeters || math.Abs(dist-radiusMeters) < 1e-6 {
			return &DuplicateMatch{
				MatchedRecord: rec,
				DistanceM:     dist,
				TimeDiff:      dt,
			}
		}
	}

	return nil
}
