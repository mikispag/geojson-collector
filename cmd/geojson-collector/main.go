package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/exporter"
	"github.com/mikispag/geojson-collector/internal/server"
	"github.com/mikispag/geojson-collector/internal/storage"
)

var (
	// Version is populated during build or defaults to dev
	Version = "1.0.0"
)

const rootHelp = `geojson-collector - High-performance Overland iOS location collector and GeoJSON exporter

Usage:
  geojson-collector <command> [arguments]

Commands:
  serve       Run the HTTP webhook daemon to collect Overland location data
  export      Export stored locations as RFC 7946 GeoJSON for a time period
  version     Display version information
  help        Show help for a command

Run 'geojson-collector <command> --help' for command-specific flags and details.
`

const serveHelp = `geojson-collector serve - Start the HTTP location collector daemon

Usage:
  geojson-collector serve [flags]

Flags:
  -c, --config string             Path to configuration JSON file (default "/etc/geojson-collector/config.json")
      --host string               IP address to listen on (default "" for all interfaces)
  -p, --port int                  Port to listen on (default 9696)
  -t, --auth-token string         Bearer token required in HTTP Authorization header
  -d, --data-dir string           Directory where daily SQLite databases are saved (default "/var/lib/geojson-collector")
      --dedup-radius float        Deduplication radius in meters (default 1.0)
      --dedup-interval float      Deduplication time window in seconds (default 60.0)
  -h, --help                      Show this help message

Environment Variables:
  GEOJSON_COLLECTOR_CONFIG        Configuration file path
  GEOJSON_COLLECTOR_HOST          Listen host / IP address
  GEOJSON_COLLECTOR_PORT          Listen port number
  GEOJSON_COLLECTOR_AUTH_TOKEN    Authorization Bearer token
  GEOJSON_COLLECTOR_DATA_DIR      Storage data directory
  GEOJSON_COLLECTOR_DEDUP_RADIUS  Deduplication radius in meters
  GEOJSON_COLLECTOR_DEDUP_INTERVAL Deduplication window in seconds
`

const exportHelp = `geojson-collector export - Export stored locations as RFC 7946 GeoJSON

Usage:
  geojson-collector export --from <start> --to <end> [flags]

Flags:
      --from string               Start date or timestamp (e.g. "2026-08-01" or "2026-08-01T00:00:00Z") [Required]
      --to string                 End date or timestamp (e.g. "2026-08-23" or "2026-08-23T23:59:59Z") [Required]
  -d, --data-dir string           Directory containing daily SQLite databases (default "/var/lib/geojson-collector")
  -c, --config string             Path to config file to read data_dir from (optional)
  -o, --output string             Output file path (default stdout)
      --pretty                    Pretty-print indented JSON output (default false)
  -h, --help                      Show this help message

Time Format Details:
  If a simple date "YYYY-MM-DD" is provided:
    --from starts at the beginning of the day (00:00:00.000000000 UTC).
    --to ends at the last millisecond of the day (23:59:59.999999999 UTC).
  Full ISO8601/RFC3339 timestamps (e.g. "2026-08-23T14:30:00+02:00") are also supported.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(rootHelp)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "serve":
		if err := runServe(args); err != nil {
			log.Fatalf("Error: %v", err)
		}
	case "export":
		if err := runExport(args); err != nil {
			log.Fatalf("Error: %v", err)
		}
	case "version", "--version", "-v":
		fmt.Printf("geojson-collector version %s\n", Version)
	case "help", "--help", "-h":
		if len(args) > 0 {
			switch args[0] {
			case "serve":
				fmt.Print(serveHelp)
			case "export":
				fmt.Print(exportHelp)
			default:
				fmt.Print(rootHelp)
			}
		} else {
			fmt.Print(rootHelp)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		fmt.Print(rootHelp)
		os.Exit(1)
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Print(serveHelp)
	}

	var (
		configPath    string
		host          string
		port          int
		authToken     string
		dataDir       string
		dedupRadius   float64
		dedupInterval float64
	)

	fs.StringVar(&configPath, "config", "", "Path to configuration file")
	fs.StringVar(&configPath, "c", "", "Path to configuration file (shorthand)")
	fs.StringVar(&host, "host", "", "IP address to listen on")
	fs.IntVar(&port, "port", 0, "Port to listen on")
	fs.IntVar(&port, "p", 0, "Port to listen on (shorthand)")
	fs.StringVar(&authToken, "auth-token", "", "Authorization Bearer token")
	fs.StringVar(&authToken, "t", "", "Authorization Bearer token (shorthand)")
	fs.StringVar(&dataDir, "data-dir", "", "Data directory for SQLite files")
	fs.StringVar(&dataDir, "d", "", "Data directory for SQLite files (shorthand)")
	fs.Float64Var(&dedupRadius, "dedup-radius", -1, "Deduplication radius in meters")
	fs.Float64Var(&dedupInterval, "dedup-interval", -1, "Deduplication interval in seconds")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// Apply CLI flag overrides if explicitly passed
	if host != "" {
		cfg.Host = host
	}
	if port > 0 {
		cfg.Port = port
	}
	if authToken != "" {
		cfg.AuthToken = authToken
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	if dedupRadius >= 0 {
		cfg.DedupRadiusMeters = dedupRadius
	}
	if dedupInterval >= 0 {
		cfg.DedupIntervalSeconds = dedupInterval
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	mgr, err := storage.NewManager(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("initializing storage manager: %w", err)
	}

	logger := log.New(os.Stderr, "[geojson-collector] ", log.LstdFlags|log.Lmsgprefix)
	daemon := server.NewDaemon(cfg, mgr, logger)

	return daemon.Run()
}

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Print(exportHelp)
	}

	var (
		fromStr    string
		toStr      string
		dataDir    string
		configPath string
		outputPath string
		pretty     bool
	)

	fs.StringVar(&fromStr, "from", "", "Start date/timestamp")
	fs.StringVar(&toStr, "to", "", "End date/timestamp")
	fs.StringVar(&dataDir, "data-dir", "", "Data directory for SQLite files")
	fs.StringVar(&dataDir, "d", "", "Data directory for SQLite files (shorthand)")
	fs.StringVar(&configPath, "config", "", "Path to configuration file")
	fs.StringVar(&configPath, "c", "", "Path to configuration file (shorthand)")
	fs.StringVar(&outputPath, "output", "", "Output file path (default stdout)")
	fs.StringVar(&outputPath, "o", "", "Output file path (shorthand)")
	fs.BoolVar(&pretty, "pretty", false, "Pretty-print indented JSON")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	if fromStr == "" || toStr == "" {
		fmt.Fprintln(os.Stderr, "Error: both --from and --to flags are required.")
		fmt.Print(exportHelp)
		os.Exit(1)
	}

	fromTime, err := exporter.ParseTimeFlag(fromStr, false)
	if err != nil {
		return fmt.Errorf("invalid --from time: %w", err)
	}

	toTime, err := exporter.ParseTimeFlag(toStr, true)
	if err != nil {
		return fmt.Errorf("invalid --to time: %w", err)
	}

	if dataDir == "" {
		// Try loading from config if specified, or fallback to default
		cfg, _ := config.LoadConfig(configPath)
		if cfg != nil && cfg.DataDir != "" {
			dataDir = cfg.DataDir
		} else {
			dataDir = config.DefaultDataDir
		}
	}

	mgr, err := storage.NewManager(dataDir)
	if err != nil {
		return fmt.Errorf("initializing storage manager: %w", err)
	}
	defer mgr.Close()

	var outWriter io.Writer = os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("creating output file %s: %w", outputPath, err)
		}
		defer f.Close()
		outWriter = f
	}

	ctx := context.Background()
	return exporter.ExportGeoJSON(ctx, mgr, fromTime, toTime, outWriter, pretty)
}
