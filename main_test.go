package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestParseConfig(t *testing.T) {
	// Save original environment and restore after test
	originalToken := os.Getenv("TOKEN")
	originalListenAddr := os.Getenv("LISTEN_ADDR")
	originalExportedTags := os.Getenv("EXPORTED_TAGS")
	originalOSConfig := os.Getenv("OPENSTACK_CONFIG")
	originalOverrides := os.Getenv("TAG_OVERRIDES_FILE")
	defer func() {
		os.Setenv("TOKEN", originalToken)
		os.Setenv("LISTEN_ADDR", originalListenAddr)
		os.Setenv("EXPORTED_TAGS", originalExportedTags)
		os.Setenv("OPENSTACK_CONFIG", originalOSConfig)
		os.Setenv("TAG_OVERRIDES_FILE", originalOverrides)
	}()

	tests := []struct {
		name             string
		envToken         string
		envListen        string
		envExportedTags  string
		envOSConfig      string
		envOverrides     string
		wantToken        string
		wantListen       string
		wantExportedTags []string
		wantOSConfig     string
		wantOverrides    string
		wantErr          bool
	}{
		{
			name:       "Missing TOKEN",
			envToken:   "",
			envListen:  "",
			wantErr:    true,
		},
		{
			name:       "Valid TOKEN, Default ListenAddr",
			envToken:   "test-token",
			envListen:  "",
			wantToken:  "test-token",
			wantListen: ":9876",
			wantErr:    false,
		},
		{
			name:       "Valid TOKEN, Custom ListenAddr",
			envToken:   "test-token",
			envListen:  ":8080",
			wantToken:  "test-token",
			wantListen: ":8080",
			wantErr:    false,
		},
		{
			name:             "EXPORTED_TAGS parsed correctly",
			envToken:         "test-token",
			envExportedTags:  "env,owner,cost_center",
			wantToken:        "test-token",
			wantListen:       ":9876",
			wantExportedTags: []string{"env", "owner", "cost_center"},
		},
		{
			name:             "EXPORTED_TAGS with spaces trimmed",
			envToken:         "test-token",
			envExportedTags:  " team , bo , to ",
			wantToken:        "test-token",
			wantListen:       ":9876",
			wantExportedTags: []string{"team", "bo", "to"},
		},
		{
			name:             "EXPORTED_TAGS with empty items filtered",
			envToken:         "test-token",
			envExportedTags:  "team,,bo,,,to,",
			wantToken:        "test-token",
			wantListen:       ":9876",
			wantExportedTags: []string{"team", "bo", "to"},
		},
		{
			name:             "EXPORTED_TAGS empty string gives nil",
			envToken:         "test-token",
			envExportedTags:  "",
			wantToken:        "test-token",
			wantListen:       ":9876",
			wantExportedTags: nil,
		},
		{
			name:         "OPENSTACK_CONFIG is passed through",
			envToken:     "test-token",
			envOSConfig:  "/etc/exporter/config.ini",
			wantToken:    "test-token",
			wantListen:   ":9876",
			wantOSConfig: "/etc/exporter/config.ini",
		},
		{
			name:          "TAG_OVERRIDES_FILE is passed through",
			envToken:      "test-token",
			envOverrides:  "/etc/exporter/overrides.json",
			wantToken:     "test-token",
			wantListen:    ":9876",
			wantOverrides: "/etc/exporter/overrides.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TOKEN", tc.envToken)
			os.Setenv("LISTEN_ADDR", tc.envListen)
			os.Setenv("EXPORTED_TAGS", tc.envExportedTags)
			os.Setenv("OPENSTACK_CONFIG", tc.envOSConfig)
			os.Setenv("TAG_OVERRIDES_FILE", tc.envOverrides)

			cfg, err := parseConfig()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.Token != tc.wantToken {
				t.Errorf("expected Token %q, got %q", tc.wantToken, cfg.Token)
			}
			if cfg.ListenAddr != tc.wantListen {
				t.Errorf("expected ListenAddr %q, got %q", tc.wantListen, cfg.ListenAddr)
			}
			if tc.wantExportedTags == nil {
				if cfg.ExportedTags != nil {
					t.Errorf("expected nil ExportedTags, got %v", cfg.ExportedTags)
				}
			} else {
				if len(cfg.ExportedTags) != len(tc.wantExportedTags) {
					t.Fatalf("expected %d exported tags, got %d: %v", len(tc.wantExportedTags), len(cfg.ExportedTags), cfg.ExportedTags)
				}
				for i, want := range tc.wantExportedTags {
					if cfg.ExportedTags[i] != want {
						t.Errorf("ExportedTags[%d] = %q, want %q", i, cfg.ExportedTags[i], want)
					}
				}
			}
			if cfg.OpenStackConf != tc.wantOSConfig {
				t.Errorf("expected OpenStackConf %q, got %q", tc.wantOSConfig, cfg.OpenStackConf)
			}
			if cfg.TagOverridesFile != tc.wantOverrides {
				t.Errorf("expected TagOverridesFile %q, got %q", tc.wantOverrides, cfg.TagOverridesFile)
			}
		})
	}
}

func TestNewMux(t *testing.T) {
	registry := prometheus.NewRegistry()
	mux := newMux(registry)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Health Check",
			method:         "GET",
			path:           "/health",
			expectedStatus: http.StatusOK,
			expectedBody:   "ok",
		},
		{
			name:           "Root Path",
			method:         "GET",
			path:           "/",
			expectedStatus: http.StatusOK,
			expectedBody:   "<html>",
		},
		{
			name:           "Metrics Path",
			method:         "GET",
			path:           "/metrics",
			expectedStatus: http.StatusOK,
			expectedBody:   "", // promhttp output, not strictly checking body content
		},
		{
			name:           "Not Found",
			method:         "GET",
			path:           "/not-found",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tc.expectedStatus)
			}

			if tc.expectedBody != "" && tc.path != "/metrics" {
				// Simple substring check for root path to ensure HTML is rendered
				if tc.path == "/" {
					if !strings.Contains(rr.Body.String(), tc.expectedBody) {
						t.Errorf("handler returned unexpected body: got %v want it to contain %v",
							rr.Body.String(), tc.expectedBody)
					}
				} else {
					if rr.Body.String() != tc.expectedBody {
						t.Errorf("handler returned unexpected body: got %v want %v",
							rr.Body.String(), tc.expectedBody)
					}
				}
			}
		})
	}
}

func TestNewServer(t *testing.T) {
	handler := http.NewServeMux()
	srv := newServer(":1234", handler)

	if srv.Addr != ":1234" {
		t.Errorf("expected Addr %q, got %q", ":1234", srv.Addr)
	}
	if srv.Handler == nil {
		t.Errorf("expected non-nil Handler")
	}
}

func TestMainFunc(t *testing.T) {
	// Set required env vars
	originalToken := os.Getenv("TOKEN")
	originalListenAddr := os.Getenv("LISTEN_ADDR")
	defer func() {
		os.Setenv("TOKEN", originalToken)
		os.Setenv("LISTEN_ADDR", originalListenAddr)
	}()

	os.Setenv("TOKEN", "dummy-test-token")
	os.Setenv("LISTEN_ADDR", "localhost:0")

	// Run main in a goroutine
	done := make(chan struct{})
	go func() {
		main()
		close(done)
	}()

	// Give it a moment to start up and set up the signal listener
	time.Sleep(200 * time.Millisecond)

	// Send an interrupt signal to ourselves
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	// Wait for main to finish gracefully
	select {
	case <-done:
		// test passed
	case <-time.After(3 * time.Second):
		t.Fatal("main did not shut down gracefully in time")
	}
}

func TestParseConfigBillingHistoryMonths(t *testing.T) {
	originalToken := os.Getenv("TOKEN")
	originalHist := os.Getenv("BILLING_HISTORY_MONTHS")
	defer func() {
		os.Setenv("TOKEN", originalToken)
		os.Setenv("BILLING_HISTORY_MONTHS", originalHist)
	}()

	os.Setenv("TOKEN", "test-token")

	// Default = 12
	os.Setenv("BILLING_HISTORY_MONTHS", "")
	cfg, err := parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BillingHistMonths != 12 {
		t.Errorf("default BillingHistMonths = %d, want 12", cfg.BillingHistMonths)
	}

	// Custom value
	os.Setenv("BILLING_HISTORY_MONTHS", "24")
	cfg, err = parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BillingHistMonths != 24 {
		t.Errorf("BillingHistMonths = %d, want 24", cfg.BillingHistMonths)
	}

	// Disabled
	os.Setenv("BILLING_HISTORY_MONTHS", "0")
	cfg, err = parseConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BillingHistMonths != 0 {
		t.Errorf("BillingHistMonths = %d, want 0", cfg.BillingHistMonths)
	}

	// Invalid
	os.Setenv("BILLING_HISTORY_MONTHS", "abc")
	_, err = parseConfig()
	if err == nil {
		t.Fatal("expected error for invalid BILLING_HISTORY_MONTHS, got nil")
	}
	if !strings.Contains(err.Error(), "BILLING_HISTORY_MONTHS") {
		t.Errorf("error should mention BILLING_HISTORY_MONTHS: %v", err)
	}
}
