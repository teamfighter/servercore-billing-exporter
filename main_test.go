package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	testTokenValue = "my-token"
	errUnexpected  = "unexpected error: %v"
)

func TestParseConfigMissingToken(t *testing.T) {
	t.Setenv("TOKEN", "")
	_, err := parseConfig()
	if err == nil {
		t.Fatal("expected error when TOKEN is empty")
	}
	if !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("error should mention TOKEN, got: %v", err)
	}
}

func TestParseConfigDefaults(t *testing.T) {
	t.Setenv("TOKEN", testTokenValue)
	t.Setenv("LISTEN_ADDR", "")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf(errUnexpected, err)
	}
	if cfg.Token != testTokenValue {
		t.Errorf("expected token %q, got %s", testTokenValue, cfg.Token)
	}
	if cfg.ListenAddr != ":9876" {
		t.Errorf("expected default listen addr ':9876', got %s", cfg.ListenAddr)
	}
}

func TestParseConfigCustomAddr(t *testing.T) {
	t.Setenv("TOKEN", testTokenValue)
	t.Setenv("LISTEN_ADDR", ":8080")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf(errUnexpected, err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected listen addr ':8080', got %s", cfg.ListenAddr)
	}
}

func TestNewMuxMetricsEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	handler := newMux(registry)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /metrics, got %d", w.Code)
	}
}

func TestNewMuxRootEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	handler := newMux(registry)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "Servercore Billing Exporter") {
		t.Error("root page should contain exporter name")
	}
	if !strings.Contains(string(body), "/metrics") {
		t.Error("root page should link to /metrics")
	}
}

func TestNewMuxHealthEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	handler := newMux(registry)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /health, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "ok" {
		t.Errorf("expected body 'ok', got %s", string(body))
	}
}

func TestNewServer(t *testing.T) {
	handler := http.NewServeMux()
	srv := newServer(":0", handler)

	if srv.Addr != ":0" {
		t.Errorf("expected addr ':0', got %s", srv.Addr)
	}
	if srv.ReadTimeout == 0 {
		t.Error("expected non-zero read timeout")
	}
	if srv.WriteTimeout == 0 {
		t.Error("expected non-zero write timeout")
	}
	if srv.IdleTimeout == 0 {
		t.Error("expected non-zero idle timeout")
	}
}

func TestParseConfigFromEnv(t *testing.T) {
	// Ensure we clean up env after test.
	oldToken := os.Getenv("TOKEN")
	oldAddr := os.Getenv("LISTEN_ADDR")
	defer func() {
		os.Setenv("TOKEN", oldToken)
		os.Setenv("LISTEN_ADDR", oldAddr)
	}()

	os.Setenv("TOKEN", "test-integration-token")
	os.Setenv("LISTEN_ADDR", ":3333")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf(errUnexpected, err)
	}
	if cfg.Token != "test-integration-token" {
		t.Errorf("expected token, got %s", cfg.Token)
	}
	if cfg.ListenAddr != ":3333" {
		t.Errorf("expected :3333, got %s", cfg.ListenAddr)
	}
}
