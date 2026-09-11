package storage

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/geo"
	"github.com/mikispag/geojson-collector/internal/models"
)

func TestBatchDedupMatchesSequentialDecisions(t *testing.T) {
	for _, interval := range []time.Duration{0, time.Nanosecond, time.Minute} {
		t.Run(interval.String(), func(t *testing.T) {
			m, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			base := time.Date(2026, 8, 1, 23, 59, 59, 0, time.UTC)
			rng := rand.New(rand.NewSource(7))
			var input []*models.LocationRecord
			var accepted []models.LocationRecord
			var want []bool
			step := interval
			if step == 0 {
				step = time.Nanosecond
			}
			for i := 0; i < 300; i++ {
				rec := &models.LocationRecord{Timestamp: base.Add(time.Duration(rng.Intn(180)-90) * step), Latitude: 47 + float64(rng.Intn(3))/100, Longitude: 8, UniqueID: []string{"", "a", "b"}[rng.Intn(3)]}
				input = append(input, rec)
				duplicate := geo.FindDuplicate(accepted, rec, 1, interval) != nil
				want = append(want, duplicate)
				if !duplicate {
					accepted = append(accepted, *rec)
				}
			}
			matches, err := m.InsertLocationsIfUnique(context.Background(), input, 1, interval)
			if err != nil {
				t.Fatal(err)
			}
			for i, match := range matches {
				if (match != nil) != want[i] {
					t.Fatalf("record %d duplicate=%v, want %v", i, match != nil, want[i])
				}
			}
			records, err := m.GetLocationsInRange(context.Background(), base.Add(-24*time.Hour), base.Add(24*time.Hour))
			if err != nil || len(records) != len(accepted) {
				t.Fatalf("stored=%d, want %d, err=%v", len(records), len(accepted), err)
			}
			// A retry must use the same device rules for persisted records.
			matches, err = m.InsertLocationsIfUnique(context.Background(), input, 1, interval)
			if err != nil {
				t.Fatal(err)
			}
			for i, match := range matches {
				if match == nil {
					t.Fatalf("retry inserted record %d", i)
				}
			}
		})
	}
}

func TestEventRetryAfterReopen(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ts := time.Unix(1750000000, 123)
	negativeZero := math.Copysign(0, -1)
	point := &models.LocationRecord{Timestamp: ts, Latitude: negativeZero, Longitude: negativeZero}
	trip := *point
	trip.Altitude = &negativeZero
	trip.ExtraProperties = map[string]interface{}{"type": "trip", "duration": json.Number("10.0"), "counter": json.Number("9007199254740993")}
	input := []*models.LocationRecord{point, &trip}
	matches, err := m.InsertLocationsIfUnique(context.Background(), input, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if matches[0] != nil || matches[1] != nil {
		t.Fatal("trip and sample must both be retained")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	matches, err = m.InsertLocationsIfUnique(context.Background(), input, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if matches[0] == nil || matches[1] == nil {
		t.Fatal("retry must deduplicate both persisted records")
	}
	records, err := m.GetLocationsInRange(context.Background(), ts, ts)
	if err != nil || len(records) != 2 {
		t.Fatalf("stored=%d, err=%v", len(records), err)
	}
}

func TestReadOnlyWALMissingSidecarsExplainsPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Unix(1750000000, 0)
	if err := m.InsertLocation(context.Background(), &models.LocationRecord{Timestamp: ts, Latitude: 47, Longitude: 8}); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, ts.UTC().Format("2006-01-02")+".sqlite"), 0440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0550); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0750) })
	m, err = NewReadOnlyManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.GetLocationsInRange(context.Background(), ts, ts)
	if err == nil || !strings.Contains(err.Error(), "service account") {
		t.Fatalf("expected actionable WAL permission error, got %v", err)
	}
}
