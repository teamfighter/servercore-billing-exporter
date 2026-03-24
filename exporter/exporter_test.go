package exporter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/teamfighter/servercore-billing-exporter/api"
	"github.com/teamfighter/servercore-billing-exporter/openstack"

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

	ch := make(chan *prometheus.Desc, 20)
	exp.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}

	// We expect exactly 12 metric descriptors.
	if len(descs) != 12 {
		t.Errorf("expected 12 descriptors, got %d", len(descs))
	}
}

func TestExporterCollect(t *testing.T) {
	srv := testAPIServer(t)
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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
	exp := New(client, nil, []string{"tag1", "tag2", "tag3"}, nil)

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

// --- Mock TagFetcher for testing ---

type mockTagFetcher struct {
	tags map[string]openstack.ServerTags
	err  error
}

func (m *mockTagFetcher) FetchAllTags() (map[string]openstack.ServerTags, error) {
	return m.tags, m.err
}

func TestExporterVMCostWithTags(t *testing.T) {
	// Server returns object_metric data with cloud_vm objects matching mock tags.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"final_sum":100,"debt_sum":0,"balances":[],"debt":[]}]},"settings":{"currency":"rub"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":100}}`))
		case pathConsumption:
			gt := r.URL.Query().Get("group_type")
			if gt == "object_metric" {
				w.Write([]byte(`{"status":"success","data":[{"account_id":"1","provider_key":"vpc","value":5000,"period":"2026-03","project":{"id":"p1","name":"prod"},"metric":{"id":"compute_cores","name":"vCPU","quantity":4,"unit":"item"},"object":{"id":"server-aaa","name":"web1","type":"cloud_vm"}}]}`))
				return
			}
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	fetcher := &mockTagFetcher{
		tags: map[string]openstack.ServerTags{
			"server-aaa": {"tag2": "owner@example.com", "tag3": "lead@example.com", "tag1": "Platform"},
		},
	}
	exp := New(client, fetcher, []string{"tag1", "tag2", "tag3"}, nil)

	expected := strings.NewReader(`
		# HELP sc_vm_cost Per-VM resource cost with OpenStack tags.
		# TYPE sc_vm_cost gauge
		sc_vm_cost{metric="compute_cores",project="prod",service="Cloud Compute",tag1="Platform",tag2="owner@example.com",tag3="lead@example.com",vm_name="web1"} 50
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_vm_cost"); err != nil {
		t.Errorf("vm_cost with tags mismatch: %v", err)
	}
}

func TestExporterVMCostNoMatch(t *testing.T) {
	// Server returns cloud_vm object with ID that does NOT match any tag.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"final_sum":100,"debt_sum":0,"balances":[],"debt":[]}]},"settings":{"currency":"rub"}}`))
		case pathPrediction:
			w.Write([]byte(`{"status":"success","data":{"primary":100}}`))
		case pathConsumption:
			gt := r.URL.Query().Get("group_type")
			if gt == "object_metric" {
				w.Write([]byte(`{"status":"success","data":[{"account_id":"1","provider_key":"vpc","value":1000,"period":"2026-03","project":{"id":"p1","name":"prod"},"metric":{"id":"compute_ram","name":"RAM","quantity":8,"unit":"GB"},"object":{"id":"server-unknown","name":"mystery","type":"cloud_vm"}}]}`))
				return
			}
			w.Write([]byte(`{"status":"success","data":[]}`))
		default:
			http.Error(w, errNotFound, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := api.NewClient(testToken, srv.URL)
	fetcher := &mockTagFetcher{
		tags: map[string]openstack.ServerTags{
			"server-aaa": {"tag2": "owner@example.com", "tag3": "lead@example.com", "tag1": "Platform"},
		},
	}
	exp := New(client, fetcher, []string{"tag1", "tag2", "tag3"}, nil)

	// Tags should be empty because server-unknown is not in the tags map.
	expected := strings.NewReader(`
		# HELP sc_vm_cost Per-VM resource cost with OpenStack tags.
		# TYPE sc_vm_cost gauge
		sc_vm_cost{metric="compute_ram",project="prod",service="Cloud Compute",tag1="Untagged",tag2="Untagged",tag3="Untagged",vm_name="mystery"} 10
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_vm_cost"); err != nil {
		t.Errorf("vm_cost with unmatched tags mismatch: %v", err)
	}
}

func TestExporterTagFetcherError(t *testing.T) {
	// TagFetcher returns an error — scrape should still succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerCType, headerJSON)
		switch r.URL.Path {
		case pathBalances:
			w.Write([]byte(`{"data":{"billings":[{"final_sum":100,"debt_sum":0,"balances":[],"debt":[]}]},"settings":{"currency":"rub"}}`))
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
	fetcher := &mockTagFetcher{err: fmt.Errorf("keystone unreachable")}
	exp := New(client, fetcher, []string{"tag1", "tag2", "tag3"}, nil)

	// scrape_success should be 1 despite tag fetcher error.
	expected := strings.NewReader(`
		# HELP sc_scrape_success Whether the last scrape was successful (1 = success, 0 = failure).
		# TYPE sc_scrape_success gauge
		sc_scrape_success 1
	`)

	if err := testutil.CollectAndCompare(exp, expected, "sc_scrape_success"); err != nil {
		t.Errorf("scrape should succeed even when tag fetcher fails: %v", err)
	}
}
func TestExtractParentVMName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"disk-for-myvm-1-#1", "myvm-1"},
		{"disk-for-hello-#99", "hello"},
		{"nodisk", "nodisk"},
		{"disk-for--#", ""},
	}
	for _, tc := range tests {
		if got := extractParentVMName(tc.in); got != tc.want {
			t.Errorf("extractParentVMName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProcessDiskItem(t *testing.T) {
	exp := New(nil, nil, []string{"tag1", "tag2"}, nil)
	diskAgg := make(map[diskKey]float64)

	item := api.ConsumptionItem{
		ProviderKey: "vpc",
		Metric:      &api.ConsumptionMetric{ID: "volume_standard"},
		Object: &api.ConsumptionObject{
			Type:       "volume_fast",
			ID:         "disk-123",
			Name:       "my-disk",
			ParentName: "disk-for-myvm-#1",
		},
		Value: 500, // 5 units
	}

	globalTags := map[string]openstack.ServerTags{
		"myvm": {"tag1": "backend", "tag2": "owner@"},
	}

	exp.processDiskItem(item, "my-proj", globalTags, diskAgg)

	if len(diskAgg) != 1 {
		t.Fatalf("expected 1 aggregated disk, got %d", len(diskAgg))
	}

	for key, val := range diskAgg {
		if key.diskName != "my-disk" || key.parentVM != "disk-for-myvm-#1" {
			t.Errorf("unexpected key: %+v", key)
		}
		if key.tagsHash != "backend\x00owner@" {
			t.Errorf("unexpected tagsHash: %q", key.tagsHash)
		}
		if val != 5.0 {
			t.Errorf("expected value 5.0, got %v", val)
		}
	}
}

func TestApplyPrefixOverrides(t *testing.T) {
	t.Run("prefix match applies override", func(t *testing.T) {
		overrides := TagOverrides{
			"k8s-prod-node": {"team": "Platform", "bo": "ops@example.com"},
		}
		exp := New(nil, nil, []string{"team", "bo"}, overrides)
		tagValues := []string{"Untagged", "Untagged"}

		exp.applyPrefixOverrides("k8s-prod-node-abc123", tagValues)

		if tagValues[0] != "Platform" {
			t.Errorf("expected team=Platform, got %q", tagValues[0])
		}
		if tagValues[1] != "ops@example.com" {
			t.Errorf("expected bo=ops@example.com, got %q", tagValues[1])
		}
	})

	t.Run("no prefix match leaves Untagged", func(t *testing.T) {
		overrides := TagOverrides{
			"k8s-prod": {"team": "Platform"},
		}
		exp := New(nil, nil, []string{"team"}, overrides)
		tagValues := []string{"Untagged"}

		exp.applyPrefixOverrides("web-server-01", tagValues)

		if tagValues[0] != "Untagged" {
			t.Errorf("expected Untagged, got %q", tagValues[0])
		}
	})

	t.Run("empty overrides is no-op", func(t *testing.T) {
		exp := New(nil, nil, []string{"team"}, nil)
		tagValues := []string{"Untagged"}

		exp.applyPrefixOverrides("anything", tagValues)

		if tagValues[0] != "Untagged" {
			t.Errorf("expected Untagged, got %q", tagValues[0])
		}
	})

	t.Run("override does not overwrite existing tags", func(t *testing.T) {
		overrides := TagOverrides{
			"k8s-prod": {"team": "Platform", "bo": "override@example.com"},
		}
		exp := New(nil, nil, []string{"team", "bo"}, overrides)
		tagValues := []string{"ExistingTeam", "Untagged"}

		exp.applyPrefixOverrides("k8s-prod-node-01", tagValues)

		if tagValues[0] != "ExistingTeam" {
			t.Errorf("expected ExistingTeam to be preserved, got %q", tagValues[0])
		}
		if tagValues[1] != "override@example.com" {
			t.Errorf("expected override for bo, got %q", tagValues[1])
		}
	})

	t.Run("override key not in exportedTags is ignored", func(t *testing.T) {
		overrides := TagOverrides{
			"web-": {"team": "Backend", "nonexistent": "ignored"},
		}
		exp := New(nil, nil, []string{"team"}, overrides)
		tagValues := []string{"Untagged"}

		exp.applyPrefixOverrides("web-server-01", tagValues)

		if tagValues[0] != "Backend" {
			t.Errorf("expected Backend, got %q", tagValues[0])
		}
	})

	t.Run("exact VM name matches prefix", func(t *testing.T) {
		overrides := TagOverrides{
			"exact-name": {"team": "Exact"},
		}
		exp := New(nil, nil, []string{"team"}, overrides)
		tagValues := []string{"Untagged"}

		exp.applyPrefixOverrides("exact-name", tagValues)

		if tagValues[0] != "Exact" {
			t.Errorf("expected Exact, got %q", tagValues[0])
		}
	})
}

func TestLoadTagOverrides(t *testing.T) {
	t.Run("empty path returns nil", func(t *testing.T) {
		overrides, err := LoadTagOverrides("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if overrides != nil {
			t.Errorf("expected nil overrides for empty path, got %v", overrides)
		}
	})

	t.Run("valid JSON file", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/overrides.json"
		content := `{
			"k8s-prod-node": {"team": "Platform"},
			"web-": {"team": "Backend", "bo": "web-owner@example.com"}
		}`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		overrides, err := LoadTagOverrides(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if overrides["k8s-prod-node"]["team"] != "Platform" {
			t.Errorf("expected team=Platform, got %q", overrides["k8s-prod-node"]["team"])
		}
		if overrides["web-"]["bo"] != "web-owner@example.com" {
			t.Errorf("expected bo=web-owner@example.com, got %q", overrides["web-"]["bo"])
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := LoadTagOverrides("/nonexistent/path/overrides.json")
		if err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/bad.json"
		if err := os.WriteFile(path, []byte(`{not valid json}`), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := LoadTagOverrides(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})

	t.Run("empty JSON object is valid", func(t *testing.T) {
		dir := t.TempDir()
		path := dir + "/empty.json"
		if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		overrides, err := LoadTagOverrides(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(overrides) != 0 {
			t.Errorf("expected empty overrides, got %d entries", len(overrides))
		}
	})
}

