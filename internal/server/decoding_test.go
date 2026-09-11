package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerSkipsMalformedFeatureShapes(t *testing.T) {
	for _, malformed := range []string{
		`"not a feature"`, `[]`, `null`,
		`{"type":"Feature","geometry":{"type":"Point","coordinates":[8,"bad"]},"properties":{"timestamp":"2026-08-23T12:00:00Z"}}`,
		`{"type":"Feature","geometry":{"type":"Point","coordinates":[8,47]},"properties":"bad"}`,
		`{"type":"Feature","geometry":{"type":"Point","coordinates":[8,47]},"properties":{"timestamp":"2026-08-23T12:00:00Z","speed":1e999}}`,
	} {
		t.Run(malformed, func(t *testing.T) {
			srv, mgr, _ := setupTestServer(t, "")
			defer mgr.Close()
			feature := `{"type":"Feature","geometry":{"type":"Point","coordinates":[8.5417,47.3769]},"properties":{"timestamp":"2026-08-23T12:%02d:00Z","speed":5.5}}`
			payload := `{"locations":[` + fmt.Sprintf(feature, 0) + "," + malformed + "," + fmt.Sprintf(feature, 2) + "]}"
			req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(payload))
			response := httptest.NewRecorder()
			srv.Routes().ServeHTTP(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("malformed feature rejected valid neighbors: status=%d body=%s", response.Code, response.Body.String())
			}
			start := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
			records, err := mgr.GetLocationsInRange(context.Background(), start, start.Add(2*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 2 {
				t.Fatalf("stored %d records, want both valid neighbors", len(records))
			}
			for _, rec := range records {
				if rec.Speed == nil || *rec.Speed != 5.5 {
					t.Errorf("numeric speed did not survive decoding: %v", rec.Speed)
				}
			}
		})
	}
}

func TestServerRejectsMalformedRoot(t *testing.T) {
	for _, payload := range []string{`null`, `[]`, `"not an object"`, `{"locations":[`, `{"locations":{}}`, `{"locations":[]} {}`} {
		t.Run(payload, func(t *testing.T) {
			srv, mgr, _ := setupTestServer(t, "")
			defer mgr.Close()
			response := httptest.NewRecorder()
			srv.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(payload)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("malformed root got status %d, want 400", response.Code)
			}
		})
	}
}

func TestServerPreservesNumericTimestampPrecision(t *testing.T) {
	for _, timestamp := range []string{"1750000000.123456789", "1750000000123.456789"} {
		t.Run(timestamp, func(t *testing.T) {
			srv, mgr, _ := setupTestServer(t, "")
			defer mgr.Close()
			payload := `{"locations":[{"type":"Feature","geometry":{"type":"Point","coordinates":[8,47]},"properties":{"timestamp":` + timestamp + `,"pauses":1,"battery_level":-1}}]}`
			response := httptest.NewRecorder()
			srv.Routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(payload)))
			if response.Code != http.StatusOK {
				t.Fatalf("numeric feature failed: status=%d body=%s", response.Code, response.Body.String())
			}
			want := time.Unix(1750000000, 123456789)
			records, err := mgr.GetLocationsInRange(context.Background(), want.Add(-time.Second), want.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 {
				t.Fatalf("stored %d records, want 1", len(records))
			}
			record := records[0]
			if !record.Timestamp.Equal(want) {
				t.Errorf("timestamp = %s, want %s", record.Timestamp.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
			}
			if record.Pauses == nil || !*record.Pauses {
				t.Errorf("numeric boolean was lost: pauses=%v", record.Pauses)
			}
			if record.BatteryLevel != nil {
				t.Errorf("unknown battery level should be absent: %v", *record.BatteryLevel)
			}
		})
	}
}
