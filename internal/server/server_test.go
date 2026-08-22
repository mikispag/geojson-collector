package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/server"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func setupTestServer(t *testing.T, authToken string) (*server.Server, *storage.Manager, *config.Config) {
	tempDir := t.TempDir()
	mgr, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create storage manager: %v", err)
	}

	cfg := &config.Config{
		Host:                 "127.0.0.1",
		Port:                 9696,
		AuthToken:            authToken,
		DataDir:              tempDir,
		DedupRadiusMeters:    1.0,
		DedupIntervalSeconds: 60.0,
	}

	logger := log.New(io.Discard, "", 0)
	srv := server.New(cfg, mgr, logger)
	return srv, mgr, cfg
}

func TestServer_AuthAndValidPayload(t *testing.T) {
	srv, mgr, _ := setupTestServer(t, "test-secret-token")
	defer mgr.Close()

	handler := srv.Routes()

	payload := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [8.5417, 47.3769, 410.0]
      },
      "properties": {
        "timestamp": "2026-08-23T10:00:00Z",
        "speed": 5.5,
        "course": 120.0,
        "horizontal_accuracy": 10.0,
        "motion": ["walking"],
        "battery_state": "unplugged",
        "battery_level": 0.85,
        "device_id": "iphone-15",
        "wifi": "ZurichWiFi"
      }
    }
  ]
}`

	// 1. Request without auth header -> 401
	reqNoAuth := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString(payload))
	reqNoAuth.Header.Set("Content-Type", "application/json")
	wNoAuth := httptest.NewRecorder()
	handler.ServeHTTP(wNoAuth, reqNoAuth)

	if wNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wNoAuth.Code)
	}

	// 2. Request with invalid token -> 401
	reqBadAuth := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString(payload))
	reqBadAuth.Header.Set("Content-Type", "application/json")
	reqBadAuth.Header.Set("Authorization", "Bearer wrong-token")
	wBadAuth := httptest.NewRecorder()
	handler.ServeHTTP(wBadAuth, reqBadAuth)

	if wBadAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wBadAuth.Code)
	}

	// 3. Request with valid token -> 200
	reqValid := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString(payload))
	reqValid.Header.Set("Content-Type", "application/json")
	reqValid.Header.Set("Authorization", "Bearer test-secret-token")
	wValid := httptest.NewRecorder()
	handler.ServeHTTP(wValid, reqValid)

	if wValid.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", wValid.Code, wValid.Body.String())
	}

	var resp server.JSONResponse
	if err := json.Unmarshal(wValid.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Result != "ok" {
		t.Fatalf("expected result 'ok', got '%s'", resp.Result)
	}

	// Verify point in DB
	ts := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	recs, err := mgr.GetLocationsInWindow(reqValid.Context(), ts.Add(-time.Minute), ts.Add(time.Minute))
	if err != nil {
		t.Fatalf("failed to query storage: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record in storage, got %d", len(recs))
	}
	if recs[0].Latitude != 47.3769 || recs[0].Longitude != 8.5417 {
		t.Errorf("expected lat 47.3769 lon 8.5417, got %f %f", recs[0].Latitude, recs[0].Longitude)
	}
}

func TestServer_Deduplication(t *testing.T) {
	srv, mgr, _ := setupTestServer(t, "")
	defer mgr.Close()

	handler := srv.Routes()

	payload1 := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [8.5417, 47.3769]
      },
      "properties": {
        "timestamp": "2026-08-23T12:00:00Z"
      }
    }
  ]
}`

	req1 := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString(payload1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}

	// Send duplicate point 15 seconds later at same location
	payloadDup := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [8.5417, 47.3769]
      },
      "properties": {
        "timestamp": "2026-08-23T12:00:15Z"
      }
    }
  ]
}`

	reqDup := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString(payloadDup))
	reqDup.Header.Set("Content-Type", "application/json")
	wDup := httptest.NewRecorder()
	handler.ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wDup.Code)
	}

	// Check that only 1 point is stored
	t0 := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	recs, err := mgr.GetLocationsInWindow(reqDup.Context(), t0.Add(-time.Hour), t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("failed to query storage: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record (duplicate ignored), got %d", len(recs))
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	srv, mgr, _ := setupTestServer(t, "")
	defer mgr.Close()

	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewBufferString("{invalid-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}

func TestServer_HealthAndMethodNotAllowed(t *testing.T) {
	srv, mgr, _ := setupTestServer(t, "")
	defer mgr.Close()

	handler := srv.Routes()

	// GET /health
	reqHealth := httptest.NewRequest(http.MethodGet, "/health", nil)
	wHealth := httptest.NewRecorder()
	handler.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 for health check, got %d", wHealth.Code)
	}

	// GET /api
	reqGetAPI := httptest.NewRequest(http.MethodGet, "/api", nil)
	wGetAPI := httptest.NewRecorder()
	handler.ServeHTTP(wGetAPI, reqGetAPI)
	if wGetAPI.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api, got %d", wGetAPI.Code)
	}

	// DELETE /api
	reqDel := httptest.NewRequest(http.MethodDelete, "/api", nil)
	wDel := httptest.NewRecorder()
	handler.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE /api, got %d", wDel.Code)
	}
}
