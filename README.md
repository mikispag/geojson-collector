# geojson-collector

[![CI](https://github.com/mikispag/geojson-collector/actions/workflows/ci.yml/badge.svg)](https://github.com/mikispag/geojson-collector/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mikispag/geojson-collector)](https://goreportcard.com/report/github.com/mikispag/geojson-collector)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mikispag/geojson-collector)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GeoJSON](https://img.shields.io/badge/GeoJSON-RFC%207946-brightgreen.svg)](https://datatracker.ietf.org/doc/html/rfc7946)

A lightweight, robust, and zero-CGO Go daemon and CLI tool to collect real-time GPS location updates from the [iOS Overland](https://overland.p3k.app/) app into daily UTC-partitioned SQLite databases with WAL mode durability, sub-meter deduplication, and export to [Timelinize](https://timelinize.com/)-compatible RFC 7946 GeoJSON.

---

## 🌟 Key Features

* **Overland Webhook Server:** Collects batched location updates via HTTP `POST /api` with Bearer token authentication.
* **Daily Partitioned Storage:** Automatically stores records into UTC daily SQLite databases (`/var/lib/geojson-collector/YYYY-MM-DD.sqlite`).
* **Sudden Termination & Crash Resilience:** Runs SQLite with Write-Ahead Logging (`WAL`), `synchronous = NORMAL`, and `busy_timeout = 5000` to guarantee high throughput and zero corruption on abrupt restarts or power loss.
* **Intelligent Deduplication:** Prevents duplicate stationary points within a configurable radius (default `1.0m`) and time delta (default `60s`), evaluating across UTC midnight boundaries seamlessly.
* **Sanity & Telemetry Filtering:** Rejects corrupted GPS coordinates, unphysical speeds (e.g. supersonic glitches > Mach 3), clock-drift future timestamps, or malformed data while logging warnings to `stderr`.
* **Timelinize-Ready RFC 7946 GeoJSON Exporter:** CLI `export` subcommand extracts time intervals into standard GeoJSON FeatureCollections, mapped with well-known keys (`velocity`, `heading`, `accuracy`, `altitude`, `timestamp`, `motion`, `battery_level`, `wifi`, etc.) directly consumable by the Timelinize GeoJSON importer.
* **Zero CGO:** Built on pure Go (`modernc.org/sqlite`) for single-binary portability across Linux (amd64, arm64, riscv64), macOS, and BSD.

---

## 🏗 Architecture & Data Flow

```mermaid
flowchart TD
    subgraph Client ["iOS Device"]
        A[iOS Overland App]
    end

    subgraph Server ["geojson-collector Daemon"]
        B["POST /api (HTTP Bearer Auth)"]
        C{Bearer Token Valid?}
        D[Sanity Checks & Telemetry Validation]
        E[Spatial & Temporal Deduplication Engine]
        F[(Daily UTC SQLite Databases: YYYY-MM-DD.sqlite)]
    end

    subgraph Exporter ["CLI Exporter & Consumers"]
        G["geojson-collector export --from ... --to ..."]
        H[RFC 7946 GeoJSON FeatureCollection]
        I[Timelinize Timeline & Maps]
    end

    A -->|POST JSON batch| B
    B --> C
    C -->|No / 401| Z1[Reject Request]
    C -->|Yes / 200| D
    D -->|Invalid / Glitch| Z2[Warn on stderr & Skip Point]
    D -->|Valid| E
    E -->|Duplicate <= 1m / 60s| Z3[Warn on stderr & Ignore]
    E -->|Unique Location| F
    F --> G
    G --> H
    H -->|tlz import| I
```

---

## 📦 Installation

### From Source

Ensure Go 1.24+ is installed:

```bash
git clone https://github.com/mikispag/geojson-collector.git
cd geojson-collector
go build -o geojson-collector ./cmd/geojson-collector
sudo install -m 755 geojson-collector /usr/local/bin/
```

### Via `go install`

```bash
go install github.com/mikispag/geojson-collector/cmd/geojson-collector@latest
```

---

## ⚙️ Configuration

By default, `geojson-collector` reads `/etc/geojson-collector/config.json`.

### Configuration File Example

```json
{
  "host": "0.0.0.0",
  "port": 9696,
  "auth_token": "your_secure_random_token_here",
  "data_dir": "/var/lib/geojson-collector",
  "dedup_radius_meters": 1.0,
  "dedup_interval_seconds": 60.0
}
```

### Configuration Options

| Option | Type | Default | Description | Environment Variable |
|---|---|---|---|---|
| `host` | `string` | `""` (all IPs) | IP / Host interface to bind on | `GEOJSON_COLLECTOR_HOST` |
| `port` | `int` | `9696` | TCP port for HTTP server | `GEOJSON_COLLECTOR_PORT` |
| `auth_token` | `string` | `""` | Bearer token checked on `/api` | `GEOJSON_COLLECTOR_AUTH_TOKEN` |
| `data_dir` | `string` | `/var/lib/geojson-collector` | Directory for daily SQLite files | `GEOJSON_COLLECTOR_DATA_DIR` |
| `dedup_radius_meters` | `float` | `1.0` | Duplicate spatial radius threshold (meters) | `GEOJSON_COLLECTOR_DEDUP_RADIUS` |
| `dedup_interval_seconds` | `float` | `60.0` | Duplicate temporal window threshold (seconds) | `GEOJSON_COLLECTOR_DEDUP_INTERVAL` |

---

## 📱 Configuring the iOS Overland App

1. Download and install [Overland](https://overland.p3k.app/) from the iOS App Store.
2. Open Overland and navigate to **Settings** (`⚙️` icon in top right).
3. Configure the receiver:
   * **Endpoint URL:** `https://your-server-domain.com/api` (or `http://<IP>:9696/api` if testing locally)
   * **Access Token:** Enter the secret configured in `auth_token`
   * **Send in Batches:** Checked (recommended: e.g. 50-100 points)
   * **Include Tracking Stats:** Checked (sends battery state, wifi SSID, motion activity, and accuracy stats)
4. Tap **Save** and verify the status indicator turns green.

```
POST /api HTTP/1.1
Host: your-domain.com
Authorization: Bearer your_secure_random_token_here
Content-Type: application/json

{
  "locations": [
    {
      "type": "Feature",
      "geometry": {
        "type": "Point",
        "coordinates": [-122.030581, 37.331800]
      },
      "properties": {
        "timestamp": "2026-08-23T08:00:00-0700",
        "altitude": 15,
        "speed": 4.5,
        "course": 180,
        "horizontal_accuracy": 5,
        "vertical_accuracy": 3,
        "motion": ["driving"],
        "battery_state": "charging",
        "battery_level": 0.88,
        "wifi": "TeslaModel3_WiFi",
        "device_id": "Miki-iPhone"
      }
    }
  ]
}
```

---

## 🚀 CLI Usage

### 1. Starting the Server

```bash
# Using default /etc/geojson-collector/config.json
geojson-collector serve

# Or with custom flags
geojson-collector serve --port 9696 --auth-token "mytoken" --data-dir /var/lib/geojson-collector
```

### 2. Exporting GeoJSON

The `export` subcommand extracts points for any arbitrary time range into standard RFC 7946 GeoJSON.

#### Date-only Range (`YYYY-MM-DD`):
When date-only format is specified:
* `--from` automatically starts at `00:00:00.000000000 UTC` of the start day.
* `--to` automatically covers up to `23:59:59.999999999 UTC` of the end day.

```bash
# Export all points from August 1 to August 23, 2026
geojson-collector export \
  --from 2026-08-01 \
  --to 2026-08-23 \
  --data-dir /var/lib/geojson-collector \
  --output august_track.geojson \
  --pretty
```

#### Precise ISO8601 Timestamp Range:
```bash
geojson-collector export \
  --from "2026-08-23T08:00:00Z" \
  --to "2026-08-23T18:00:00Z" \
  --output afternoon_drive.geojson
```

---

## 🗺 Timelinize Integration

The exported GeoJSON is fully compatible with [Timelinize](https://github.com/timelinize/timelinize).

`geojson-collector` ensures that all extracted properties are preserved and well-known fields are aligned with Timelinize expectations:
* `timestamp` (ISO8601) $\rightarrow$ item `Timestamp`
* `altitude` $\rightarrow$ item `Location.Altitude`
* `speed` & `velocity` $\rightarrow$ item metadata `Velocity`
* `course` & `heading` $\rightarrow$ item metadata `Heading`
* `horizontal_accuracy` & `accuracy` $\rightarrow$ item `Location.Uncertainty`
* `motion`, `battery_state`, `battery_level`, `wifi`, `device_id`, etc. $\rightarrow$ item `Metadata`

Import into Timelinize:

```bash
tlz import --data-source geojson --file /path/to/exported.geojson
```

---

## 🗄 SQLite Storage Schema & WAL Mode

Each day's data is stored in `/var/lib/geojson-collector/YYYY-MM-DD.sqlite`.

```sql
CREATE TABLE IF NOT EXISTS locations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,          -- Unix timestamp in nanoseconds (UTC)
    timestamp_iso TEXT NOT NULL,         -- Original ISO8601 / RFC3339 string
    latitude REAL NOT NULL,              -- Decimal latitude (-90.0 to 90.0)
    longitude REAL NOT NULL,             -- Decimal longitude (-180.0 to 180.0)
    altitude REAL,                       -- Altitude in meters
    speed REAL,                          -- Speed in m/s
    course REAL,                         -- Course / heading in degrees (0-360)
    horizontal_accuracy REAL,            -- Horizontal accuracy in meters
    vertical_accuracy REAL,              -- Vertical accuracy in meters
    speed_accuracy REAL,                 -- Speed accuracy in m/s
    course_accuracy REAL,                -- Course accuracy in degrees
    motion TEXT,                         -- JSON array: ["driving", "stationary"]
    battery_state TEXT,                  -- "charging", "full", "unplugged", "unknown"
    battery_level REAL,                  -- Battery fraction (0.0 to 1.0)
    wifi TEXT,                           -- Connected WiFi SSID
    device_id TEXT,                      -- Configured Device ID
    unique_id TEXT,                      -- Apple Unique ID
    pauses INTEGER,                      -- Boolean 0 / 1 / NULL
    activity TEXT,                       -- "automotive_navigation", "fitness", etc.
    desired_accuracy REAL,               -- Desired accuracy in meters
    deferred REAL,                       -- Deferred distance in meters
    significant_change TEXT,             -- "disabled", "enabled", "exclusive"
    locations_in_payload INTEGER,        -- Batch count
    extra_properties TEXT                -- JSON object for custom metadata
);

CREATE INDEX IF NOT EXISTS idx_locations_ts ON locations(timestamp);
CREATE INDEX IF NOT EXISTS idx_locations_coords ON locations(latitude, longitude);
```

### Crash Durability Pragmas
On each opened connection, the daemon enforces:
```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA temp_store = MEMORY;
```

---

## 🐧 Systemd Service Setup

`geojson-collector` uses `systemd-sysusers` for declarative system user provisioning and `systemd`'s `StateDirectory` / `ConfigurationDirectory` directives to automatically manage `/var/lib/geojson-collector` and `/etc/geojson-collector` ownership and permissions.

### 1. Declarative System User (`systemd-sysusers`)

Deploy [`geojson-collector.sysusers`](geojson-collector.sysusers) to create the dedicated system user and group:

```bash
# On Arch Linux and Debian/Ubuntu:
sudo cp geojson-collector.sysusers /usr/lib/sysusers.d/geojson-collector.conf
sudo systemd-sysusers
```

### 2. Configuration & Binary Installation

```bash
# Install binary
sudo install -m 755 geojson-collector /usr/local/bin/

# Install configuration (owned by geojson-collector)
sudo mkdir -p /etc/geojson-collector
sudo install -m 600 -o geojson-collector -g geojson-collector config.example.json /etc/geojson-collector/config.json

# Install and start systemd service
sudo cp geojson-collector.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now geojson-collector
```

`/var/lib/geojson-collector` is provisioned automatically with `0750` permissions by `systemd` via `StateDirectory=geojson-collector` when the service starts.

### 3. Check Status & Logs

```bash
sudo systemctl status geojson-collector
sudo journalctl -u geojson-collector -f
```

## 🔒 Reverse Proxy & HTTPS (Caddy)

`geojson-collector` runs as a plain HTTP daemon and is designed to sit behind a reverse proxy (such as Caddy or Nginx) to handle TLS/HTTPS termination, certificate lifecycle (ACME/Let's Encrypt), and public exposure.

### Caddyfile Snippet

```caddy
# Subdomain configuration
location.example.com {
    reverse_proxy 127.0.0.1:9696
}
```

---

## 📡 API Reference

### `POST /api`
Receives batches of location points in the format sent by Overland.

* **Headers:**
  * `Authorization: Bearer <auth_token>`
  * `Content-Type: application/json`
* **Success Response (200 OK):**
  ```json
  {
    "result": "ok"
  }
  ```
* **Error Responses:**
  * `401 Unauthorized`: `{"result": "unauthorized"}`
  * `400 Bad Request`: `{"result": "invalid json payload: ..."}`
  * `500 Internal Server Error`: `{"result": "database error ..."}`

### `GET /health` or `GET /api`
Health check endpoint returning `200 OK` `{"result": "ok"}`.

---

## 🧪 Testing

Run all unit, integration, and race-detection tests:

```bash
go test -v -race ./...
```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).
