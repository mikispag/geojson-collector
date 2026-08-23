package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
)

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
	os.Setenv("GEOJSON_COLLECTOR_HOST", "0.0.0.0")
	os.Setenv("GEOJSON_COLLECTOR_PORT", "9999")
	os.Setenv("GEOJSON_COLLECTOR_AUTH_TOKEN", "env-token")
	os.Setenv("GEOJSON_COLLECTOR_DATA_DIR", "/custom/data")
	defer func() {
		os.Unsetenv("GEOJSON_COLLECTOR_HOST")
		os.Unsetenv("GEOJSON_COLLECTOR_PORT")
		os.Unsetenv("GEOJSON_COLLECTOR_AUTH_TOKEN")
		os.Unsetenv("GEOJSON_COLLECTOR_DATA_DIR")
	}()

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
	_, err := config.LoadConfig("/non-existent/explicit/config.json")
	if err == nil {
		t.Fatal("expected error when explicit config path does not exist, got nil")
	}
}
