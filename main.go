// Servercore Billing Exporter
//
// A Prometheus exporter for Servercore (servercore.com) billing data.
// Exposes account balance, debt, consumption, and prediction metrics.
//
// Configuration via environment variables:
//   - TOKEN: Servercore API static token (required)
//   - LISTEN_ADDR: HTTP listen address (default ":9876")
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/teamfighter/servercore-billing-exporter/api"
	"github.com/teamfighter/servercore-billing-exporter/exporter"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// config holds application configuration parsed from environment variables.
type config struct {
	Token      string
	ListenAddr string
}

// parseConfig reads configuration from environment variables.
// Returns an error if required variables are missing.
func parseConfig() (*config, error) {
	token := os.Getenv("TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TOKEN environment variable is required (Servercore API static token)")
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":9876"
	}

	return &config{
		Token:      token,
		ListenAddr: listenAddr,
	}, nil
}

// newMux creates the HTTP handler with /metrics, /, and /health endpoints.
func newMux(registry *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorLog: log.Default(),
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Servercore Billing Exporter</title></head>
<body>
<h1>Servercore Billing Exporter</h1>
<p><a href="/metrics">Metrics</a></p>
</body></html>`))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return mux
}

// newServer creates and configures the HTTP server.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Servercore Billing Exporter starting...")

	cfg, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}

	client := api.NewClient(cfg.Token, "")
	exp := exporter.New(client)

	registry := prometheus.NewRegistry()
	registry.MustRegister(exp)

	mux := newMux(registry)
	srv := newServer(cfg.ListenAddr, mux)

	go func() {
		log.Printf("Listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}
	log.Println("Exporter stopped")
}
