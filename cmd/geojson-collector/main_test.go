package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/server"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func isolateConfig(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"CONFIG", "HOST", "PORT", "AUTH_TOKEN", "DATA_DIR", "DEDUP_RADIUS", "DEDUP_INTERVAL"} {
		t.Setenv("GEOJSON_COLLECTOR_"+key, "")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEOJSON_COLLECTOR_CONFIG", path)
	return path
}

func TestEndToEndCollectorAndExport(t *testing.T) {
	isolateConfig(t)
	tempDir := t.TempDir()
	mgr, err := storage.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}
	defer mgr.Close()

	cfg := &config.Config{
		Host:                 "127.0.0.1",
		Port:                 9696,
		AuthToken:            "secret-token-e2e",
		DataDir:              tempDir,
		DedupRadiusMeters:    1.0,
		DedupIntervalSeconds: 60.0,
	}

	srv := server.New(cfg, mgr, nil)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	// 1. Post Overland Payload
	payload := `{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [-122.030581, 37.331800, 15.0]
      },
      "properties": {
        "timestamp": "2026-08-23T08:00:00-0700",
        "speed": 4.5,
        "course": 180.0,
        "horizontal_accuracy": 5.0,
        "vertical_accuracy": 3.0,
        "speed_accuracy": 0.2,
        "course_accuracy": 1.0,
        "motion": ["driving"],
        "battery_state": "charging",
        "battery_level": 0.88,
        "device_id": "Miki-iPhone",
        "wifi": "TeslaModel3_WiFi",
        "activity": "automotive_navigation",
        "desired_accuracy": 100,
        "deferred": 1000,
        "significant_change": "disabled",
        "locations_in_payload": 1
      }
    }
  ],
  "current": {"state": "active"},
  "trip": {"distance": 1500}
}`

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("failed creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret-token-e2e")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http post failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var jsonResp server.JSONResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("decoding response failed: %v", err)
	}
	if jsonResp.Result != "ok" {
		t.Fatalf("expected result 'ok', got '%s'", jsonResp.Result)
	}

	// 2. Export GeoJSON for the day
	exportOutPath := filepath.Join(tempDir, "export.geojson")
	if err := os.WriteFile(exportOutPath, []byte("previous export"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", tempDir, "--output", exportOutPath, "--pretty"}); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}
	info, err := os.Stat(exportOutPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("export permissions = %04o, want 0600", got)
	}

	// 3. Verify exported GeoJSON structure
	data, err := os.ReadFile(exportOutPath)
	if err != nil {
		t.Fatalf("reading export file failed: %v", err)
	}

	var fc models.GeoJSONFeatureCollection
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("parsing exported GeoJSON failed: %v\nJSON:\n%s", err, string(data))
	}

	if fc.Type != "FeatureCollection" {
		t.Errorf("expected FeatureCollection, got %s", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(fc.Features))
	}

	feat := fc.Features[0]
	if feat.Geometry.Coordinates[0] != -122.030581 || feat.Geometry.Coordinates[1] != 37.331800 || feat.Geometry.Coordinates[2] != 15.0 {
		t.Errorf("unexpected coordinates: %v", feat.Geometry.Coordinates)
	}

	props := feat.Properties
	if props["velocity"] != 4.5 || props["speed"] != 4.5 {
		t.Errorf("unexpected speed/velocity: %v", props["speed"])
	}
	if props["heading"] != 180.0 || props["course"] != 180.0 {
		t.Errorf("unexpected heading/course: %v", props["heading"])
	}
	if props["accuracy"] != 5.0 || props["horizontal_accuracy"] != 5.0 {
		t.Errorf("unexpected accuracy: %v", props["accuracy"])
	}
	if props["battery_level"] != 0.88 {
		t.Errorf("unexpected battery_level: %v", props["battery_level"])
	}
	if props["device_id"] != "Miki-iPhone" {
		t.Errorf("unexpected device_id: %v", props["device_id"])
	}
}

func TestRunExportPreservesExistingOutput(t *testing.T) {
	for _, failure := range []string{"reversed range", "corrupt database"} {
		t.Run(failure, func(t *testing.T) {
			isolateConfig(t)
			dataDir := t.TempDir()
			outputDir := t.TempDir()
			output := filepath.Join(outputDir, "locations.geojson")
			previous := []byte("previous export\n")
			if err := os.WriteFile(output, previous, 0600); err != nil {
				t.Fatal(err)
			}
			from, to := "2026-08-23", "2026-08-23"
			if failure == "reversed range" {
				from = "2026-08-24"
			} else if err := os.WriteFile(filepath.Join(dataDir, "2026-08-23.sqlite"), []byte("corrupt database"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := runExport([]string{"--from", from, "--to", to, "--data-dir", dataDir, "--output", output}); err == nil {
				t.Fatal("expected export failure")
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, previous) {
				t.Errorf("previous export changed to %q", got)
			}
			entries, err := os.ReadDir(outputDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("temporary output leaked: %v", entries)
			}
		})
	}
}

func TestRunExportRejectsMissingEnvironmentConfig(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(strconv.FormatBool(override), func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("GEOJSON_COLLECTOR_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
			args := []string{"--from", "2026-08-23", "--to", "2026-08-23", "--output", filepath.Join(t.TempDir(), "output.json")}
			if override {
				args = append(args, "--data-dir", t.TempDir())
			}
			if err := runExport(args); err == nil || !strings.Contains(err.Error(), "loading configuration") {
				t.Fatalf("expected config loading error, got %v", err)
			}
		})
	}
}

func TestRunExportExplicitDataDirBypassesImplicitConfig(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GEOJSON_COLLECTOR_CONFIG", "")
	output := filepath.Join(t.TempDir(), "output.json")
	if err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", t.TempDir(), "--output", output}); err != nil {
		t.Fatalf("export with explicit data directory failed: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var fc struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatal(err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 0 {
		t.Fatalf("unexpected empty export: %s", data)
	}
	if err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", "", "--output", output}); err == nil || !strings.Contains(err.Error(), "data_dir cannot be empty") {
		t.Fatalf("expected invalid explicit empty data directory, got %v", err)
	}
}

func TestRunExportExplicitDataDirHonorsExplicitConfig(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GEOJSON_COLLECTOR_CONFIG", "")
	missing := filepath.Join(t.TempDir(), "missing.json")
	err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", t.TempDir(), "--config", missing, "--output", filepath.Join(t.TempDir(), "output.json")})
	if err == nil || !strings.Contains(err.Error(), "loading configuration") {
		t.Fatalf("expected explicit configuration loading error, got %v", err)
	}
}

func TestRunExportProtectsStoragePaths(t *testing.T) {
	for _, name := range []string{"2030-01-01.sqlite", "2030-01-01.sqlite-wal", "2030-01-01.sqlite-shm", "2030-01-01.sqlite-journal"} {
		for _, alias := range []string{"direct", "symlinked parent", "hardlink", "symlink", "future", "future symlinked parent"} {
			t.Run(name+"/"+alias, func(t *testing.T) {
				isolateConfig(t)
				dataDir := t.TempDir()
				source := filepath.Join(dataDir, name)
				output := source
				original := []byte("protected source contents")
				future := strings.HasPrefix(alias, "future")
				if !future {
					if err := os.WriteFile(source, original, 0600); err != nil {
						t.Fatal(err)
					}
				}
				switch alias {
				case "symlinked parent", "future symlinked parent":
					link := filepath.Join(t.TempDir(), "data")
					if err := os.Symlink(dataDir, link); err != nil {
						t.Fatal(err)
					}
					output = filepath.Join(link, name)
				case "hardlink", "symlink":
					output = filepath.Join(t.TempDir(), "track.geojson")
					link := os.Link
					if alias == "symlink" {
						link = os.Symlink
					}
					if err := link(source, output); err != nil {
						t.Fatal(err)
					}
				}
				err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", dataDir, "--output", output})
				if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
					t.Fatalf("expected source protection error, got %v", err)
				}
				got, err := os.ReadFile(source)
				if future {
					if !os.IsNotExist(err) {
						t.Fatalf("future database path was created: %v", err)
					}
				} else if err != nil || !bytes.Equal(got, original) {
					t.Fatalf("source changed: %q, %v", got, err)
				}
			})
		}
	}
}

func TestRunExportProtectsLoadedConfig(t *testing.T) {
	for _, alias := range []string{"environment", "flag", "symlinked parent", "hardlink", "symlink"} {
		t.Run(alias, func(t *testing.T) {
			path := isolateConfig(t)
			output := path
			args := []string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", t.TempDir()}
			switch alias {
			case "flag":
				t.Setenv("GEOJSON_COLLECTOR_CONFIG", "")
				args = append(args, "--config", path)
			case "symlinked parent":
				link := filepath.Join(t.TempDir(), "config")
				if err := os.Symlink(filepath.Dir(path), link); err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(link, filepath.Base(path))
			case "hardlink", "symlink":
				output = filepath.Join(t.TempDir(), "track.geojson")
				link := os.Link
				if alias == "symlink" {
					link = os.Symlink
				}
				if err := link(path, output); err != nil {
					t.Fatal(err)
				}
			}
			err := runExport(append(args, "--output", output))
			if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
				t.Fatalf("expected configuration protection error, got %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "{}" {
				t.Fatalf("configuration changed: %q, %v", got, err)
			}
		})
	}
}

func TestRunExportAllowsUnrelatedOutputPaths(t *testing.T) {
	for _, name := range []string{"track.geojson", "notes.sqlite", "2026-08-23.sqlite.backup", "2030-01-01.sqlite"} {
		t.Run(name, func(t *testing.T) {
			isolateConfig(t)
			dataDir := t.TempDir()
			output := filepath.Join(dataDir, name)
			if name == "2030-01-01.sqlite" {
				output = filepath.Join(t.TempDir(), name)
			}
			if err := runExport([]string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", dataDir, "--output", output}); err != nil {
				t.Fatalf("unrelated output rejected: %v", err)
			}
		})
	}
}

func TestRunExportProtectsOnlySelectedConfiguration(t *testing.T) {
	environmentConfig := isolateConfig(t)
	selectedConfig := filepath.Join(t.TempDir(), "selected.json")
	if err := os.WriteFile(selectedConfig, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--from", "2026-08-23", "--to", "2026-08-23", "--data-dir", t.TempDir(), "--config", selectedConfig}
	if err := runExport(append(args, "--output", selectedConfig)); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("selected configuration was not protected: %v", err)
	}
	if err := runExport(append(args, "--output", environmentConfig)); err != nil {
		t.Fatalf("unused environment configuration blocked output: %v", err)
	}
}

func TestRunServeValidatesExplicitCLIValues(t *testing.T) {
	for _, args := range [][]string{
		{"--port", "0"}, {"--port", "-1"}, {"--dedup-radius", "-1"},
		{"--dedup-interval", "-1"}, {"--dedup-radius", "+Inf"}, {"--dedup-interval", "NaN"}, {"--data-dir", ""},
	} {
		t.Run(strings.Join(args, "="), func(t *testing.T) {
			path := isolateConfig(t)
			cfg := config.DefaultConfig()
			cfg.Host = "invalid host"
			cfg.DataDir = t.TempDir()
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := runServe(args); err == nil || !strings.Contains(err.Error(), "invalid configuration") {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestRunServeAppliesOverridesBeforeValidation(t *testing.T) {
	for _, environment := range []bool{false, true} {
		t.Run(strconv.FormatBool(environment), func(t *testing.T) {
			path := isolateConfig(t)
			if err := os.WriteFile(path, []byte(`{"port": 0, "dedup_radius_meters": -1, "data_dir": ""}`), 0600); err != nil {
				t.Fatal(err)
			}
			if environment {
				t.Setenv("GEOJSON_COLLECTOR_PORT", "-1")
				t.Setenv("GEOJSON_COLLECTOR_DEDUP_RADIUS", "+Inf")
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			err = runServe([]string{"--host", "127.0.0.1", "--port", port, "--dedup-radius", "0", "--data-dir", t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), "address already in use") {
				t.Fatalf("expected bind error after merged config passed validation, got %v", err)
			}
		})
	}
}
