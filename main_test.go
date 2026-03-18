package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestParseConfig(t *testing.T) {
	// Save original environment and restore after test
	originalToken := os.Getenv("TOKEN")
	originalListenAddr := os.Getenv("LISTEN_ADDR")
	defer func() {
		os.Setenv("TOKEN", originalToken)
		os.Setenv("LISTEN_ADDR", originalListenAddr)
	}()

	tests := []struct {
		name       string
		envToken   string
		envListen  string
		wantToken  string
		wantListen string
		wantErr    bool
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TOKEN", tc.envToken)
			os.Setenv("LISTEN_ADDR", tc.envListen)

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
