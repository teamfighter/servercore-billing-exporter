// Servercore Billing Exporter
//
// A Prometheus exporter for Servercore (servercore.com) billing data.
// Exposes account balance, debt, consumption, and prediction metrics.
//
// Configuration via environment variables:
//   - TOKEN: Servercore API static token (required)
//   - LISTEN_ADDR: HTTP listen address (default ":9876")
//   - OPENSTACK_CONFIG: path to config.ini to enable VM tag enrichment (optional)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/teamfighter/servercore-billing-exporter/api"
	"github.com/teamfighter/servercore-billing-exporter/exporter"
	"github.com/teamfighter/servercore-billing-exporter/openstack"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// config holds application configuration parsed from environment variables.
type config struct {
	Token              string
	ListenAddr         string
	OpenStackConf      string
	ExportedTags       []string
	TagOverridesFile   string
	BillingHistMonths  int
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

	var exportedTags []string
	if tagsEnv := os.Getenv("EXPORTED_TAGS"); tagsEnv != "" {
		for _, t := range strings.Split(tagsEnv, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				exportedTags = append(exportedTags, t)
			}
		}
	}

	histMonths := 12 // default
	if v := os.Getenv("BILLING_HISTORY_MONTHS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("BILLING_HISTORY_MONTHS must be an integer: %w", err)
		}
		histMonths = n
	}

	return &config{
		Token:              token,
		ListenAddr:         listenAddr,
		OpenStackConf:      os.Getenv("OPENSTACK_CONFIG"),
		ExportedTags:       exportedTags,
		TagOverridesFile:   os.Getenv("TAG_OVERRIDES_FILE"),
		BillingHistMonths:  histMonths,
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

	var tagFetcher openstack.TagFetcher
	if cfg.OpenStackConf != "" {
		osConfs, err := openstack.LoadConfig(cfg.OpenStackConf)
		if err != nil {
			log.Fatalf("Failed to load openstack config: %v", err)
		}
		tagFetcher = &openstack.LiveFetcher{Configs: osConfs}
		log.Printf("Loaded %d OpenStack environments for tag enrichment", len(osConfs))
	} else {
		log.Printf("OPENSTACK_CONFIG not set, tag enrichment disabled")
	}

	tagOverrides, err := exporter.LoadTagOverrides(cfg.TagOverridesFile)
	if err != nil {
		log.Fatalf("Failed to load tag overrides: %v", err)
	}
	if tagOverrides != nil {
		log.Printf("Loaded %d prefix tag overrides", len(tagOverrides))
	}

	exp := exporter.New(client, tagFetcher, cfg.ExportedTags, tagOverrides, cfg.BillingHistMonths)
	log.Printf("Historical billing: %d months", cfg.BillingHistMonths)

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
