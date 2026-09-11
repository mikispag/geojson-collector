package config_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
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

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Port != 9696 {
		t.Errorf("expected default port 9696, got %d", cfg.Port)
	}
	if cfg.DataDir != "/var/lib/geojson-collector" {
		t.Errorf("expected default data dir /var/lib/geojson-collector, got %s", cfg.DataDir)
	}
	if cfg.DedupRadiusMeters != 1.0 {
		t.Errorf("expected default dedup radius 1.0, got %f", cfg.DedupRadiusMeters)
	}
	if cfg.DedupInterval() != 60*time.Second {
		t.Errorf("expected default dedup interval 60s, got %v", cfg.DedupInterval())
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should be valid, got err: %v", err)
	}
}

func TestLoadConfigFile(t *testing.T) {
	isolateConfig(t)
	tempDir := t.TempDir()
	confPath := filepath.Join(tempDir, "config.json")

	content := `{
		"host": "127.0.0.1",
		"port": 8080,
		"auth_token": "secret123",
		"data_dir": "/tmp/locations",
		"dedup_radius_meters": 5.0,
		"dedup_interval_seconds": 120.0
	}`

	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := config.LoadConfig(confPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Host != "127.0.0.1" {
		t.Errorf("expected host 127.0.0.1, got %s", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.AuthToken != "secret123" {
		t.Errorf("expected auth_token secret123, got %s", cfg.AuthToken)
	}
	if cfg.DataDir != "/tmp/locations" {
		t.Errorf("expected data_dir /tmp/locations, got %s", cfg.DataDir)
	}
	if cfg.DedupRadiusMeters != 5.0 {
		t.Errorf("expected dedup radius 5.0, got %f", cfg.DedupRadiusMeters)
	}
	if cfg.DedupInterval() != 120*time.Second {
		t.Errorf("expected dedup interval 120s, got %v", cfg.DedupInterval())
	}
}

func TestEnvOverrides(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GEOJSON_COLLECTOR_HOST", "0.0.0.0")
	t.Setenv("GEOJSON_COLLECTOR_PORT", "9999")
	t.Setenv("GEOJSON_COLLECTOR_AUTH_TOKEN", "env-token")
	t.Setenv("GEOJSON_COLLECTOR_DATA_DIR", "/custom/data")

	cfg, err := config.LoadConfig("")
	if err != nil {
		t.Fatalf("failed to load config with env: %v", err)
	}

	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", cfg.Host)
	}
	if cfg.Port != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.Port)
	}
	if cfg.AuthToken != "env-token" {
		t.Errorf("expected token env-token, got %s", cfg.AuthToken)
	}
	if cfg.DataDir != "/custom/data" {
		t.Errorf("expected data dir /custom/data, got %s", cfg.DataDir)
	}
}

func TestLoadConfig_MissingExplicitPath(t *testing.T) {
	isolateConfig(t)
	_, err := config.LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error when explicit config path does not exist, got nil")
	}
}

func TestLoadConfigRejectsInvalidEnvironment(t *testing.T) {
	for key, values := range map[string][]string{
		"PORT":           {"invalid", "0", "-1", "65536"},
		"DEDUP_RADIUS":   {"invalid", "-1", "NaN", "+Inf", "-Inf"},
		"DEDUP_INTERVAL": {"invalid", "-1", "NaN", "+Inf", "-Inf", "31536001"},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				isolateConfig(t)
				t.Setenv("GEOJSON_COLLECTOR_"+key, value)
				if _, err := config.LoadConfig(""); err == nil {
					t.Fatalf("accepted %s=%q", key, value)
				}
			})
		}
	}
}

func TestValidateRejectsNonfiniteValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		cfg := config.DefaultConfig()
		cfg.DedupRadiusMeters = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("accepted radius %v", value)
		}
		cfg = config.DefaultConfig()
		cfg.DedupIntervalSeconds = value
		if err := cfg.Validate(); err == nil {
			t.Errorf("accepted interval %v", value)
		}
	}
}

func TestListenAddr(t *testing.T) {
	for host, want := range map[string]string{
		"": ":9696", "127.0.0.1": "127.0.0.1:9696", "localhost": "localhost:9696",
		"::1": "[::1]:9696", "::": "[::]:9696", "fe80::1%eth0": "[fe80::1%eth0]:9696",
	} {
		cfg := config.DefaultConfig()
		cfg.Host = host
		if got := cfg.ListenAddr(); got != want {
			t.Errorf("host %q: got %q, want %q", host, got, want)
		}
	}
}
