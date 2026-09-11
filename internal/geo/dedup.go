package geo

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
)

// DuplicateMatch contains details about a detected duplicate point.
type DuplicateMatch struct {
	MatchedRecord models.LocationRecord
	DistanceM     float64
	TimeDiff      time.Duration
}

// DeviceKey prefers Overland's stable unique_id, falling back to device_id.
// Unidentified records share a key, separate from all identified devices.
func DeviceKey(rec *models.LocationRecord) string {
	if rec.UniqueID != "" {
		return "unique:" + rec.UniqueID
	}
	if rec.DeviceID != "" {
		return "device:" + rec.DeviceID
	}
	return ""
}

func isEvent(rec *models.LocationRecord) bool {
	for key, value := range rec.ExtraProperties {
		if value == nil {
			continue
		}
		if strings.EqualFold(key, "action") || strings.EqualFold(key, "event") {
			return true
		}
		if strings.EqualFold(key, "type") {
			kind, ok := value.(string)
			if !ok || !strings.EqualFold(kind, "location") {
				return true
			}
		}
	}
	return false
}

func eventPayload(rec models.LocationRecord) []byte {
	// Database IDs and timestamp spelling do not change event identity.
	rec.ID = 0
	rec.Timestamp = rec.Timestamp.UTC()
	rec.TimestampISO = ""
	// SQLite REAL values lose the sign of zero. Match their persisted form so
	// an unchanged event remains a duplicate after the database is reopened.
	if rec.Latitude == 0 {
		rec.Latitude = 0
	}
	if rec.Longitude == 0 {
		rec.Longitude = 0
	}
	zero := 0.0
	for _, field := range []**float64{
		&rec.Altitude, &rec.Speed, &rec.Course, &rec.HorizontalAccuracy,
		&rec.VerticalAccuracy, &rec.SpeedAccuracy, &rec.CourseAccuracy,
		&rec.BatteryLevel, &rec.DesiredAccuracy, &rec.Deferred,
	} {
		if *field != nil && **field == 0 {
			*field = &zero
		}
	}
	payload, _ := json.Marshal(rec)
	return payload
}

// FindDuplicate thins ordinary samples from the same device within the distance
// and time window. Events are retained unless their complete payload is repeated.
func FindDuplicate(existingRecords []models.LocationRecord, candidate *models.LocationRecord, radiusMeters float64, interval time.Duration) *DuplicateMatch {
	if candidate == nil || len(existingRecords) == 0 {
		return nil
	}

	candTime := candidate.Timestamp
	device := DeviceKey(candidate)
	candidateEvent := isEvent(candidate)
	var payload []byte
	if candidateEvent {
		payload = eventPayload(*candidate)
	}

	for _, rec := range existingRecords {
		if device != DeviceKey(&rec) {
			continue
		}
		if candidateEvent || isEvent(&rec) {
			if candidateEvent && isEvent(&rec) && len(payload) > 0 && bytes.Equal(payload, eventPayload(rec)) {
				return &DuplicateMatch{MatchedRecord: rec}
			}
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
