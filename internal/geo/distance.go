package geo

import (
	"math"
)

const (
	// EarthRadiusMeters is the approximate mean radius of the Earth in meters.
	EarthRadiusMeters = 6371000.0
)

// HaversineDistance calculates the great-circle distance between two points on Earth in meters.
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	if lat1 == lat2 && lon1 == lon2 {
		return 0.0
	}

	phi1 := lat1 * math.Pi / 180.0
	phi2 := lat2 * math.Pi / 180.0
	deltaPhi := (lat2 - lat1) * math.Pi / 180.0
	deltaLambda := (lon2 - lon1) * math.Pi / 180.0

	sinDeltaPhi := math.Sin(deltaPhi / 2.0)
	sinDeltaLambda := math.Sin(deltaLambda / 2.0)

	a := sinDeltaPhi*sinDeltaPhi + math.Cos(phi1)*math.Cos(phi2)*sinDeltaLambda*sinDeltaLambda
	if a > 1.0 {
		a = 1.0
	}
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))

	return EarthRadiusMeters * c
}
