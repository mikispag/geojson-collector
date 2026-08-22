package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/geo"
	"github.com/mikispag/geojson-collector/internal/models"
	"github.com/mikispag/geojson-collector/internal/storage"
)

// Server encapsulates the HTTP handler, configuration, and storage.
type Server struct {
	cfg     *config.Config
	storage *storage.Manager
	logger  *log.Logger
}

// New creates a new Server instance.
func New(cfg *config.Config, mgr *storage.Manager, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		cfg:     cfg,
		storage: mgr,
		logger:  logger,
	}
}

// JSONResponse is the standard response structure.
type JSONResponse struct {
	Result string `json:"result"`
}

func (s *Server) writeJSON(w http.ResponseWriter, statusCode int, result string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(JSONResponse{Result: result})
}

// AuthMiddleware validates the Authorization Bearer token if configured.
func (s *Server) checkAuth(r *http.Request) bool {
	if s.cfg.AuthToken == "" {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}

	providedToken := strings.TrimSpace(parts[1])
	expectedToken := s.cfg.AuthToken

	return subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken)) == 1
}

// HandleAPI handles POST /api Overland webhook requests as well as health checks.
func (s *Server) HandleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeJSON(w, http.StatusOK, "ok")
		return
	}

	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Validate authorization
	if !s.checkAuth(r) {
		s.logger.Printf("[WARN] unauthorized request from %s", r.RemoteAddr)
		s.writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Limit request body to 10MB to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Printf("[WARN] error reading request body: %v", err)
		s.writeJSON(w, http.StatusBadRequest, fmt.Sprintf("error reading body: %v", err))
		return
	}

	var payload models.OverlandPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		s.logger.Printf("[WARN] invalid JSON payload received: %v", err)
		s.writeJSON(w, http.StatusBadRequest, fmt.Sprintf("invalid json payload: %v", err))
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	dedupInterval := s.cfg.DedupInterval()
	dedupRadius := s.cfg.DedupRadiusMeters

	storedCount := 0
	for i := range payload.Locations {
		feat := &payload.Locations[i]
		rec, err := models.FeatureToRecord(feat)
		if err != nil {
			s.logger.Printf("[WARN] skipping malformed location feature #%d: %v", i, err)
			continue
		}

		// Sanity check
		if err := geo.ValidateLocation(rec, now); err != nil {
			s.logger.Printf("[WARN] skipping impossible location data (ts: %s, lat: %f, lon: %f): %v",
				rec.Timestamp.Format(time.RFC3339), rec.Latitude, rec.Longitude, err)
			continue
		}

		// Deduplication check
		windowStart := rec.Timestamp.Add(-dedupInterval)
		windowEnd := rec.Timestamp.Add(dedupInterval)

		existing, err := s.storage.GetLocationsInWindow(ctx, windowStart, windowEnd)
		if err != nil {
			s.logger.Printf("[ERROR] checking deduplication window: %v", err)
			s.writeJSON(w, http.StatusInternalServerError, "database error during deduplication")
			return
		}

		if dup := geo.FindDuplicate(existing, rec, dedupRadius, dedupInterval); dup != nil {
			s.logger.Printf("[WARN] duplicate point ignored: dist=%.2fm (threshold=%.2fm), dt=%v, ts=%s (%.6f, %.6f)",
				dup.DistanceM, dedupRadius, dup.TimeDiff, rec.Timestamp.Format(time.RFC3339), rec.Latitude, rec.Longitude)
			continue
		}

		// Insert into daily SQLite database
		if err := s.storage.InsertLocation(ctx, rec); err != nil {
			s.logger.Printf("[ERROR] failed to store location: %v", err)
			s.writeJSON(w, http.StatusInternalServerError, "database error inserting location")
			return
		}

		storedCount++
	}

	s.writeJSON(w, http.StatusOK, "ok")
}

// Routes returns the configured http.Handler with middleware.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.HandleAPI)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, http.StatusOK, "ok")
	})

	return s.recoveryMiddleware(s.loggingMiddleware(mux))
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		_ = duration // available for extended logging if desired
	})
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Printf("[ERROR] panic recovered: %v", rec)
				s.writeJSON(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
