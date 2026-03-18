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

const (
	testToken              = "test-token"
	headerCType            = "Content-Type"
	headerJSON             = "application/json"
	pathBalances           = "/v3/balances"
	pathPrediction         = "/v2/billing/prediction"
	pathConsumption        = "/v1/cloud_billing/statistic/consumption"
	fixtureBalances        = "../testdata/balances.json"
	fixturePrediction      = "../testdata/prediction.json"
	fixtureConsProject     = "../testdata/consumption_project.json"
	fixtureConsProjMetric  = "../testdata/consumption_project_metric.json"
	errNotFound            = "not found"
)

// testAPIServer creates a mock API server that serves fixtures for all endpoints.
func testAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)

		var fixture string
		switch r.URL.Path {
		case pathBalances:
			fixture = fixtureBalances
		case pathPrediction:
			fixture = fixturePrediction
		case pathConsumption:
			groupType := r.URL.Query().Get("group_type")
			switch groupType {
			case "project":
				fixture = fixtureConsProject
			case "project_metric":
				fixture = fixtureConsProjMetric
			default:
				fixture = fixtureConsProject
			}
		default:
			http.Error(w, `{"error":"`+errNotFound+`"}`, http.StatusNotFound)
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

	client := api.NewClient(testToken, srv.URL)
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
	// prediction_days: 2 (primary=250, vpc=180; storage=null, vmware=null)
	// consumption_cost: 4 (from consumption_project.json)
	// resource_cost: 3 (from consumption_project_metric.json)
	// resource_quantity: 3 (from consumption_project_metric.json)
	// scrape_success: 1
	// scrape_duration: 1
	// Total: 23
	expected := 23
	if len(metrics) != expected {
		t.Errorf("expected %d metrics, got %d", expected, len(metrics))
	}
}

func TestExporterScrapeSuccess(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
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

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_balance_total Total account balance in account currency.
		# TYPE sc_balance_total gauge
		sc_balance_total 5e+07
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_balance_total"); err != nil {
		t.Errorf("sc_balance_total mismatch: %v", err)
	}
}

func TestExporterPrediction(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_prediction_days Estimated number of days until the balance is exhausted.
		# TYPE sc_prediction_days gauge
		sc_prediction_days{billing_type="primary"} 10.416666666666666
		sc_prediction_days{billing_type="vpc"} 7.5
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_prediction_days"); err != nil {
		t.Errorf("sc_prediction_days mismatch: %v", err)
	}
}

func TestExporterPredictionAllNull(t *testing.T) {
	// Server returns all-null predictions.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"billing_type":"primary","final_sum":100,"debt_sum":0,"balances":[{"balance_type":"main","value":100}],"debt":[]}]},"settings":{"currency":"rub","mode":"prepaid"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":null,"storage":null,"vmware":null,"vpc":null}}`))
		case pathConsumption:
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// When all predictions are null, no sc_prediction_days metrics should be emitted.
	ch := make(chan prometheus.Metric, 50)
	exp.Collect(ch)
	close(ch)

	for m := range ch {
		desc := m.Desc().String()
		if strings.Contains(desc, "prediction_days") {
			t.Error("expected no prediction_days metrics when all values are null")
		}
	}
}

func TestExporterAPIFailure(t *testing.T) {
	// Server that always returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
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

func TestExporterPartialAPIFailure(t *testing.T) {
	// Server where balance works but prediction and consumption fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			data, _ := os.ReadFile(fixtureBalances)
			w.Write(data)
		default:
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// scrape_success should be 0 because prediction and consumption failed.
	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 0
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("sc_scrape_success should be 0 on partial failure: %v", err)
	}
}

func TestExporterEmptyBillings(t *testing.T) {
	// Server returns empty billings array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[]},"settings":{"currency":"rub","mode":"prepaid"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":100}}`))
		case pathConsumption:
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// Empty billings triggers an error, so scrape_success = 0.
	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 0
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("sc_scrape_success should be 0 when billings is empty: %v", err)
	}
}

func TestExporterEmptyConsumption(t *testing.T) {
	// Server returns valid balance, prediction, but empty consumption.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			data, _ := os.ReadFile(fixtureBalances)
			w.Write(data)
		case pathPrediction:
			data, _ := os.ReadFile(fixturePrediction)
			w.Write(data)
		case pathConsumption:
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// scrape should succeed, just no consumption metrics.
	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 1
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("sc_scrape_success should be 1 with empty consumption: %v", err)
	}
}

func TestExporterConsumptionNilProject(t *testing.T) {
	// Server returns consumption items with nil project (should use "unknown").
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			data, _ := os.ReadFile(fixtureBalances)
			w.Write(data)
		case pathPrediction:
			data, _ := os.ReadFile(fixturePrediction)
			w.Write(data)
		case pathConsumption:
			w.Write([]byte(`{"status":"success","data":[{"account_id":"1","provider_key":"vpc","value":100,"period":"2026-03","project":null}]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	expected := strings.NewReader(`
		# HELP sc_consumption_cost Current month consumption cost by project and service in account currency.
		# TYPE sc_consumption_cost gauge
		sc_consumption_cost{project="unknown",service="Cloud Compute"} 1
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_consumption_cost"); err != nil {
		t.Errorf("consumption with nil project should use 'unknown': %v", err)
	}
}

func TestHumanizeService(t *testing.T) {
	if got := humanizeService("vpc"); got != "Cloud Compute" {
		t.Errorf("expected 'Cloud Compute', got %q", got)
	}
	if got := humanizeService("unknown_service_123"); got != "unknown_service_123" {
		t.Errorf("expected 'unknown_service_123', got %q", got)
	}
}

func TestClarifyUnit(t *testing.T) {
	tests := []struct {
		metric string
		unit   string
		want   string
	}{
		{"traffic-req-1", "item", "requests"},
		{"traffic-out", "GB", "GB"},
		{"ram", "MB", "MB-hours"},
		{"ip", "item", "item-hours"},
	}

	for _, tc := range tests {
		t.Run(tc.metric+"_"+tc.unit, func(t *testing.T) {
			got := clarifyUnit(tc.metric, tc.unit)
			if got != tc.want {
				t.Errorf("clarifyUnit(%q, %q) = %q; want %q", tc.metric, tc.unit, got, tc.want)
			}
		})
	}
}

func TestExporterConsumptionProjectMetricFailure(t *testing.T) {
	// Server where project succeeds but project_metric fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"final_sum":100,"debt_sum":0,"balances":[],"debt":[]}]},"settings":{"currency":"rub"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":100}}`))
		case pathConsumption:
			if r.URL.Query().Get("group_type") == "project_metric" {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// Since consumption/project_metric failed, scrape_success should be 0.
	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 0
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("sc_scrape_success should be 0 on partial consumption failure: %v", err)
	}
}

func TestExporterConsumptionNilMetric(t *testing.T) {
	// Server returns valid metric response but Metric object is nil
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"final_sum":100,"debt_sum":0,"balances":[],"debt":[]}]},"settings":{"currency":"rub"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":100}}`))
		case pathConsumption:
			if r.URL.Query().Get("group_type") == "project_metric" {
				w.Write([]byte(`{"status":"success","data":[{"account_id":"1","provider_key":"vpc","value":100,"period":"2026-03","project":null,"metric":null}]}`))
				return
			}
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client)

	// It should succeed, but metric should be skipped -> no resourceCost emitted
	ch := make(chan prometheus.Metric, 50)
	exp.Collect(ch)
	close(ch)

	for m := range ch {
		if strings.Contains(m.Desc().String(), "resource_cost") {
			t.Error("expected no resource_cost when metric is nil")
		}
	}
}
