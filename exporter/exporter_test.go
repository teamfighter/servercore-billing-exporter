package exporter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/teamfighter/servercore-billing-exporter/api"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// testAPIServer creates a mock API server that serves fixtures for all endpoints.
func testAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var fixture string
		switch r.URL.Path {
		case "/v3/balances":
			fixture = "../testdata/balances.json"
		case "/v2/billing/prediction":
			fixture = "../testdata/prediction.json"
		case "/v1/cloud_billing/statistic/consumption":
			groupType := r.URL.Query().Get("group_type")
			switch groupType {
			case "project":
				fixture = "../testdata/consumption_project.json"
			case "project_metric":
				fixture = "../testdata/consumption_project_metric.json"
			default:
				fixture = "../testdata/consumption_project.json"
			}
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}

		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatalf("reading fixture %s: %v", fixture, err)
		}
		w.Write(data)
	}))
}

func TestExporterDescribe(t *testing.T) {
	client := api.NewClient("test", "http://localhost")
	exp := New(client)

	ch := make(chan *prometheus.Desc, 20)
	exp.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}

	// We expect exactly 10 metric descriptors.
	if len(descs) != 10 {
		t.Errorf("expected 10 descriptors, got %d", len(descs))
	}
}

func TestExporterCollect(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient("test-token", srv.URL)
	exp := New(client)

	// Collect all metrics.
	ch := make(chan prometheus.Metric, 50)
	exp.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	// Count expected metrics:
	// balance_by_type: 2 (main, bonus)
	// balance_total: 1
	// debt_total: 1
	// debt_by_service: 5 (vpc, dbaas, mks, storage, cdn)
	// prediction_days: 1
	// consumption_cost: 4 (from consumption_project.json: production×vpc, production×mks, production×storage, infrastructure×vpc)
	// resource_cost: 3 (from consumption_project_metric.json)
	// resource_quantity: 3 (from consumption_project_metric.json)
	// scrape_success: 1
	// scrape_duration: 1
	// Total: 22
	expected := 22
	if len(metrics) != expected {
		t.Errorf("expected %d metrics, got %d", expected, len(metrics))
	}
}

func TestExporterScrapeSuccess(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient("test-token", srv.URL)
	exp := New(client)

	// Register and check that scrape_success = 1.
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(exp)

	// Gather will call Collect.
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	found := false
	for _, fam := range families {
		if fam.GetName() == "sc_scrape_success" {
			found = true
			if len(fam.GetMetric()) != 1 {
				t.Fatalf("expected 1 scrape_success metric, got %d", len(fam.GetMetric()))
			}
			val := fam.GetMetric()[0].GetGauge().GetValue()
			if val != 1 {
				t.Errorf("expected scrape_success=1, got %f", val)
			}
		}
	}
	if !found {
		t.Error("sc_scrape_success metric not found")
	}
}

func TestExporterBalanceTotal(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient("test-token", srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_balance_total Total account balance in account currency.
		# TYPE sc_balance_total gauge
		sc_balance_total 5e+09
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_balance_total"); err != nil {
		t.Errorf("sc_balance_total mismatch: %v", err)
	}
}

func TestExporterPrediction(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient("test-token", srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_prediction_days Estimated number of days until the balance is exhausted.
		# TYPE sc_prediction_days gauge
		sc_prediction_days 250
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_prediction_days"); err != nil {
		t.Errorf("sc_prediction_days mismatch: %v", err)
	}
}

func TestExporterAPIFailure(t *testing.T) {
	// Server that always returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := api.NewClient("test-token", srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 0
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("sc_scrape_success should be 0 on API failure: %v", err)
	}
}
