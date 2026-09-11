package geo_test

import (
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/geo"
	"github.com/mikispag/geojson-collector/internal/models"
)

func TestDuplicateDeviceIdentity(t *testing.T) {
	base := models.LocationRecord{Timestamp: time.Unix(1750000000, 0), Latitude: 47, Longitude: 8}
	for _, tc := range []struct {
		name, oldDevice, oldUnique, device, unique string
		duplicate                                  bool
	}{
		{"different unique IDs", "", "phone-a", "", "phone-b", false},
		{"same unique ID renamed", "old name", "phone-a", "new name", "phone-a", true},
		{"shared label", "phone", "phone-a", "phone", "phone-b", false},
		{"legacy same device", "phone", "", "phone", "", true},
		{"unidentified devices", "", "", "", "", true},
		{"known and unidentified", "phone", "", "", "", false},
		{"different identity namespaces", "phone", "", "", "phone", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, candidate := base, base
			old.DeviceID, old.UniqueID = tc.oldDevice, tc.oldUnique
			candidate.DeviceID, candidate.UniqueID = tc.device, tc.unique
			match := geo.FindDuplicate([]models.LocationRecord{old}, &candidate, 1, time.Minute)
			if (match != nil) != tc.duplicate {
				t.Fatalf("duplicate=%v, want %v", match != nil, tc.duplicate)
			}
		})
	}
}

func TestEventsArePreservedAndRetriesDeduplicated(t *testing.T) {
	point := models.LocationRecord{Timestamp: time.Unix(1750000000, 0), Latitude: 47, Longitude: 8}
	trip := point
	trip.ExtraProperties = map[string]interface{}{"type": "trip", "duration": 10}
	different := trip
	different.ExtraProperties = map[string]interface{}{"type": "trip", "duration": 20}
	later := trip
	later.Timestamp = later.Timestamp.Add(time.Second)
	action := point
	action.ExtraProperties = map[string]interface{}{"action": "visit"}
	for _, tc := range []struct {
		name           string
		old, candidate models.LocationRecord
		duplicate      bool
	}{
		{"point then trip", point, trip, false},
		{"trip then point", trip, point, false},
		{"different trip details", trip, different, false},
		{"different event time", trip, later, false},
		{"trip retry", trip, trip, true},
		{"action versus sample", point, action, false},
		{"action retry", action, action, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			match := geo.FindDuplicate([]models.LocationRecord{tc.old}, &tc.candidate, 1, time.Minute)
			if (match != nil) != tc.duplicate {
				t.Fatalf("duplicate=%v, want %v", match != nil, tc.duplicate)
			}
		})
	}
}
