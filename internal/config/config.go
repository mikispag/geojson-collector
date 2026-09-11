package config

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	// DefaultConfigPath is the default location for the configuration file.
	DefaultConfigPath = "/etc/geojson-collector/config.json"

	// DefaultHost listens on all interfaces.
	DefaultHost = ""

	// DefaultPort is the default HTTP listen port.
	DefaultPort = 9696

	// DefaultDataDir is the default directory to store daily SQLite databases.
	DefaultDataDir = "/var/lib/geojson-collector"

	// DefaultDedupRadiusMeters is 1 meter.
	DefaultDedupRadiusMeters = 1.0

	// DefaultDedupIntervalSeconds is 60 seconds (1 minute).
	DefaultDedupIntervalSeconds = 60.0
)

// Config represents the daemon and CLI configuration.
type Config struct {
	Host                 string  `json:"host"`
	Port                 int     `json:"port"`
	AuthToken            string  `json:"auth_token"`
	DataDir              string  `json:"data_dir"`
	DedupRadiusMeters    float64 `json:"dedup_radius_meters"`
	DedupIntervalSeconds float64 `json:"dedup_interval_seconds"`
}

// DefaultConfig returns a Config instance populated with default values.
func DefaultConfig() *Config {
	return &Config{
		Host:                 DefaultHost,
		Port:                 DefaultPort,
		AuthToken:            "",
		DataDir:              DefaultDataDir,
		DedupRadiusMeters:    DefaultDedupRadiusMeters,
		DedupIntervalSeconds: DefaultDedupIntervalSeconds,
	}
}

// LoadConfig loads configuration from a JSON file path if specified or present,
// and applies environment variable overrides.
func LoadConfig(configPath string) (*Config, error) {
	return LoadConfigWithOverrides(configPath, nil)
}

// LoadConfigWithOverrides loads file and environment settings, applies CLI
// overrides, then validates the resulting configuration.
func LoadConfigWithOverrides(configPath string, override func(*Config)) (*Config, error) {
	cfg := DefaultConfig()

	explicit := true
	// If no path was explicitly passed, check environment variable GEOJSON_COLLECTOR_CONFIG
	if configPath == "" {
		configPath = os.Getenv("GEOJSON_COLLECTOR_CONFIG")
	}

	// If still empty, use DefaultConfigPath
	if configPath == "" {
		configPath = DefaultConfigPath
		explicit = false
	}

	// Try reading file if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file %s: %w", configPath, err)
		}
	} else if explicit || !os.IsNotExist(err) {
		// Return error if explicit path was missing/unreadable, or default path had non-ENOENT error
		return nil, fmt.Errorf("reading config file %s: %w", configPath, err)
	}

	// Environment variable overrides
	if host := os.Getenv("GEOJSON_COLLECTOR_HOST"); host != "" {
		cfg.Host = host
	}
	if portStr := os.Getenv("GEOJSON_COLLECTOR_PORT"); portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid GEOJSON_COLLECTOR_PORT: %w", err)
		}
		cfg.Port = p
	}
	if token := os.Getenv("GEOJSON_COLLECTOR_AUTH_TOKEN"); token != "" {
		cfg.AuthToken = token
	}
	if dataDir := os.Getenv("GEOJSON_COLLECTOR_DATA_DIR"); dataDir != "" {
		cfg.DataDir = dataDir
	}
	if radStr := os.Getenv("GEOJSON_COLLECTOR_DEDUP_RADIUS"); radStr != "" {
		r, err := strconv.ParseFloat(radStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GEOJSON_COLLECTOR_DEDUP_RADIUS: %w", err)
		}
		cfg.DedupRadiusMeters = r
	}
	if intStr := os.Getenv("GEOJSON_COLLECTOR_DEDUP_INTERVAL"); intStr != "" {
		i, err := strconv.ParseFloat(intStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GEOJSON_COLLECTOR_DEDUP_INTERVAL: %w", err)
		}
		cfg.DedupIntervalSeconds = i
	}

	if override != nil {
		override(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate verifies that the configuration fields are valid.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port number: %d (must be 1-65535)", c.Port)
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir cannot be empty")
	}
	if math.IsNaN(c.DedupRadiusMeters) || math.IsInf(c.DedupRadiusMeters, 0) || c.DedupRadiusMeters < 0 {
		return fmt.Errorf("dedup_radius_meters must be finite and nonnegative: %v", c.DedupRadiusMeters)
	}
	if math.IsNaN(c.DedupIntervalSeconds) || math.IsInf(c.DedupIntervalSeconds, 0) || c.DedupIntervalSeconds < 0 {
		return fmt.Errorf("dedup_interval_seconds must be finite and nonnegative: %v", c.DedupIntervalSeconds)
	}
	if c.DedupIntervalSeconds > 86400*365 {
		return fmt.Errorf("dedup_interval_seconds exceeds maximum allowed (1 year): %v", c.DedupIntervalSeconds)
	}
	return nil
}

// ListenAddr returns formatted host:port string for http.Server.
func (c *Config) ListenAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// DedupInterval returns the deduplication time window as a time.Duration.
func (c *Config) DedupInterval() time.Duration {
	return time.Duration(c.DedupIntervalSeconds * float64(time.Second))
}
