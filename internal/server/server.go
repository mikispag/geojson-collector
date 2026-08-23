package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/storage"
)

// Daemon coordinates the HTTP server lifecycle, logging, and graceful shutdown.
type Daemon struct {
	cfg        *config.Config
	storage    *storage.Manager
	httpServer *http.Server
	logger     *log.Logger
}

// NewDaemon initializes a new daemon instance.
func NewDaemon(cfg *config.Config, mgr *storage.Manager, logger *log.Logger) *Daemon {
	if logger == nil {
		logger = log.New(os.Stderr, "[geojson-collector] ", log.LstdFlags|log.Lmsgprefix)
	}

	srv := New(cfg, mgr, logger)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Daemon{
		cfg:        cfg,
		storage:    mgr,
		httpServer: httpServer,
		logger:     logger,
	}
}

// Run starts the HTTP server and blocks until SIGINT/SIGTERM is received.
func (d *Daemon) Run() error {
	ln, err := net.Listen("tcp", d.cfg.ListenAddr())
	if err != nil {
		return fmt.Errorf("listening on %s: %w", d.cfg.ListenAddr(), err)
	}

	d.logger.Printf("Starting geojson-collector server on %s (data_dir: %s)", d.cfg.ListenAddr(), d.cfg.DataDir)
	if d.cfg.AuthToken != "" {
		d.logger.Printf("Authorization Bearer token is enabled")
	} else {
		d.logger.Printf("[WARN] No auth_token configured! Server is unauthenticated.")
	}

	errChan := make(chan error, 1)
	go func() {
		if err := d.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Listen for shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errChan:
		return fmt.Errorf("http server error: %w", err)
	case sig := <-sigChan:
		d.logger.Printf("Received signal %s, initiating graceful shutdown...", sig)
	}

	// Graceful shutdown with 10s timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := d.httpServer.Shutdown(shutdownCtx); err != nil {
		d.logger.Printf("[ERROR] HTTP server shutdown error: %v", err)
	}

	if err := d.storage.Close(); err != nil {
		d.logger.Printf("[ERROR] closing storage databases: %v", err)
	}

	d.logger.Printf("Shutdown complete.")
	return nil
}
