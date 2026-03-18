package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// newTestServer creates an httptest.Server that serves JSON fixtures
// based on the request URL path.
func newTestServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check token header.
		if r.Header.Get("X-Token") == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		fixture, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}

		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatalf("reading fixture %s: %v", fixture, err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func TestFetchBalance(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/v3/balances": "../testdata/balances.json",
	})
	defer srv.Close()

	client := NewClient("test-token", srv.URL)
	resp, err := client.FetchBalance()
	if err != nil {
		t.Fatalf("FetchBalance() error: %v", err)
	}

	if resp.Settings.Currency != "rub" {
		t.Errorf("expected currency rub, got %s", resp.Settings.Currency)
	}

	if len(resp.Data.Billings) != 1 {
		t.Fatalf("expected 1 billing entry, got %d", len(resp.Data.Billings))
	}

	b := resp.Data.Billings[0]
	if b.FinalSum != 5000000000 {
		t.Errorf("expected final_sum 5000000000, got %d", b.FinalSum)
	}
	if b.DebtSum != 0 {
		t.Errorf("expected debt_sum 0, got %d", b.DebtSum)
	}
	if len(b.Balances) != 2 {
		t.Errorf("expected 2 balance types, got %d", len(b.Balances))
	}
	if len(b.Debt) != 5 {
		t.Errorf("expected 5 debt entries, got %d", len(b.Debt))
	}
}

func TestFetchPrediction(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/v2/billing/prediction": "../testdata/prediction.json",
	})
	defer srv.Close()

	client := NewClient("test-token", srv.URL)
	resp, err := client.FetchPrediction()
	if err != nil {
		t.Fatalf("FetchPrediction() error: %v", err)
	}

	if resp.Data.Primary != 250 {
		t.Errorf("expected prediction 250, got %f", resp.Data.Primary)
	}
}

func TestFetchConsumption(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/v1/cloud_billing/statistic/consumption": "../testdata/consumption_project.json",
	})
	defer srv.Close()

	client := NewClient("test-token", srv.URL)
	resp, err := client.FetchConsumption("2026-03-01T00:00:00", "2026-03-18T00:00:00", "project")
	if err != nil {
		t.Fatalf("FetchConsumption() error: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}

	if len(resp.Data) != 4 {
		t.Fatalf("expected 4 consumption items, got %d", len(resp.Data))
	}

	// Verify first item (production vpc).
	item := resp.Data[0]
	if item.Project == nil || item.Project.Name != "production" {
		t.Errorf("expected project production, got %+v", item.Project)
	}
	if item.ProviderKey != "vpc" {
		t.Errorf("expected provider_key vpc, got %s", item.ProviderKey)
	}
	if item.Value != 15000000000 {
		t.Errorf("expected value 15000000000, got %d", item.Value)
	}
}

func TestFetchConsumptionWithMetrics(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"/v1/cloud_billing/statistic/consumption": "../testdata/consumption_project_metric.json",
	})
	defer srv.Close()

	client := NewClient("test-token", srv.URL)
	resp, err := client.FetchConsumption("2026-03-17T00:00:00", "2026-03-18T00:00:00", "project_metric")
	if err != nil {
		t.Fatalf("FetchConsumption() error: %v", err)
	}

	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.Data))
	}

	// Verify metric detail for first item (vCPU).
	item := resp.Data[0]
	if item.Metric == nil {
		t.Fatal("expected metric to be non-nil")
	}
	if item.Metric.ID != "compute_cores" {
		t.Errorf("expected metric id compute_cores, got %s", item.Metric.ID)
	}
	if item.Metric.Unit != "item" {
		t.Errorf("expected unit item, got %s", item.Metric.Unit)
	}
	if item.Metric.Quantity != 32000 {
		t.Errorf("expected quantity 32000, got %f", item.Metric.Quantity)
	}
}

func TestClientUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewClient("bad-token", srv.URL)
	_, err := client.FetchBalance()
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestClientNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient("test-token", srv.URL)
	_, err := client.FetchBalance()
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
