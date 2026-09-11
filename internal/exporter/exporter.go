package exporter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/storage"
)

// ParseTimeFlag parses either a "YYYY-MM-DD" date or a full RFC3339 / ISO8601 timestamp string.
// If isEnd is true and the input is a date (YYYY-MM-DD), it sets the time to the end of the day (23:59:59.999999999 UTC).
// If isEnd is false and the input is a date (YYYY-MM-DD), it sets the time to the start of the day (00:00:00 UTC).
func ParseTimeFlag(input string, isEnd bool) (time.Time, error) {
	if input == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// Try date-only format YYYY-MM-DD
	if len(input) == 10 {
		if t, err := time.Parse("2006-01-02", input); err == nil {
			if isEnd {
				return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC), nil
			}
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}

	// Try standard ISO / RFC formats
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05.999999999-0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}

	for _, f := range formats {
		if t, err := time.Parse(f, input); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time %q: expected YYYY-MM-DD or RFC3339/ISO8601 format", input)
}

// ExportGeoJSON queries locations between from and to (inclusive) and writes an RFC 7946 FeatureCollection to w.
func ExportGeoJSON(ctx context.Context, mgr *storage.Manager, from, to time.Time, w io.Writer, pretty bool) error {
	if from.After(to) {
		return fmt.Errorf("--from (%v) cannot be after --to (%v)", from, to)
	}

	// Bound memory to the writer buffer and one feature, even for multi-year exports.
	out := bufio.NewWriter(w)
	header, separator, footer := `{"type":"FeatureCollection","features":[`, ",", "]}\n"
	if pretty {
		header = "{\n  \"type\": \"FeatureCollection\",\n  \"features\": ["
		separator = ",\n"
		footer = "\n  ]\n}\n"
	}
	if _, err := out.WriteString(header); err != nil {
		return fmt.Errorf("writing GeoJSON header: %w", err)
	}
	first := true
	err := mgr.VisitLocationsInRange(ctx, from, to, func(rec models.LocationRecord) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		feature := models.RecordToFeature(&rec)
		var data []byte
		var err error
		if pretty {
			data, err = json.MarshalIndent(feature, "    ", "  ")
		} else {
			data, err = json.Marshal(feature)
		}
		if err != nil {
			return fmt.Errorf("serializing GeoJSON feature: %w", err)
		}
		if !first {
			if _, err := out.WriteString(separator); err != nil {
				return err
			}
		} else if pretty {
			if _, err := out.WriteString("\n"); err != nil {
				return err
			}
		}
		first = false
		if pretty {
			if _, err := out.WriteString("    "); err != nil {
				return err
			}
		}
		_, err = out.Write(data)
		return err
	})
	if err != nil {
		return fmt.Errorf("exporting locations: %w", err)
	}
	if _, err := out.WriteString(footer); err != nil {
		return fmt.Errorf("writing GeoJSON footer: %w", err)
	}
	if err := out.Flush(); err != nil {
		return fmt.Errorf("writing GeoJSON output: %w", err)
	}
	return nil
}
