package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/mikispag/geojson-collector/internal/config"
	"github.com/mikispag/geojson-collector/internal/storage"
)

func TestShutdownExpiryCancelsActiveRequestsAndReturnsError(t *testing.T) {
	mgr, err := storage.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	d := NewDaemon(config.DefaultConfig(), mgr, log.New(io.Discard, "", 0))
	entered, cancelled := make(chan struct{}), make(chan struct{})
	d.httpServer.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	defer d.httpServer.Close()
	go d.httpServer.Serve(ln)
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		if response, err := client.Get("http://" + ln.Addr().String()); err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := d.shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("shutdown error=%v, want deadline exceeded", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Error("expired shutdown left request context active")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Error("expired shutdown left client connection open")
	}
}
