package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production Servercore API base URL.
	DefaultBaseURL = "https://api.servercore.com"

	// DefaultTimeout for HTTP requests.
	DefaultTimeout = 120 * time.Second
)

// AllProviderKeys lists all available Servercore service provider keys.
var AllProviderKeys = []string{
	"vpc", "dbaas", "mks", "storage", "cdn",
	"craas", "serverless", "vmware", "ones", "mobfarm", "ses",
}

// Client is an HTTP client for the Servercore Billing API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Servercore API client.
//
// The token is a static API token (X-Token). An optional baseURL
// can be provided for testing; pass "" to use the default.
func NewClient(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// doRequest performs an authenticated GET request and decodes the JSON response.
func (c *Client) doRequest(path string, result interface{}) error {
	url := c.baseURL + path

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", path, err)
	}
	req.Header.Set("X-Token", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, path, string(body))
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decoding response from %s: %w", path, err)
	}

	return nil
}

// FetchBalance retrieves account balance and debt information.
func (c *Client) FetchBalance() (*BalanceResponse, error) {
	var resp BalanceResponse
	if err := c.doRequest("/v3/balances", &resp); err != nil {
		return nil, fmt.Errorf("fetching balance: %w", err)
	}
	return &resp, nil
}

// FetchPrediction retrieves the billing prediction (days until balance exhaustion).
func (c *Client) FetchPrediction() (*PredictionResponse, error) {
	var resp PredictionResponse
	if err := c.doRequest("/v2/billing/prediction", &resp); err != nil {
		return nil, fmt.Errorf("fetching prediction: %w", err)
	}
	return &resp, nil
}

// FetchConsumption retrieves consumption statistics for the current month.
//
// groupType controls the aggregation level:
//   - "project"        — by project × service
//   - "project_metric" — by project × service × metric (most detailed)
//   - "region_metric"  — by region × metric
func (c *Client) FetchConsumption(startDate, endDate, groupType string) (*ConsumptionResponse, error) {
	path := fmt.Sprintf(
		"/v1/cloud_billing/statistic/consumption?%s&start=%s&end=%s&locale=en&group_type=%s&period_group_type=all",
		buildProviderKeysParam(),
		startDate,
		endDate,
		groupType,
	)

	var resp ConsumptionResponse
	if err := c.doRequest(path, &resp); err != nil {
		return nil, fmt.Errorf("fetching consumption (%s): %w", groupType, err)
	}
	return &resp, nil
}

// FetchConsumptionMonthly retrieves consumption statistics grouped by month.
// Returns per-month breakdown with project × service aggregation.
// Each item's Period field indicates the month (e.g. "2026-01-01T00:00:00").
func (c *Client) FetchConsumptionMonthly(startDate, endDate, groupType string) (*ConsumptionResponse, error) {
	path := fmt.Sprintf(
		"/v1/cloud_billing/statistic/consumption?%s&start=%s&end=%s&locale=en&group_type=%s&period_group_type=month",
		buildProviderKeysParam(),
		startDate,
		endDate,
		groupType,
	)

	var resp ConsumptionResponse
	if err := c.doRequest(path, &resp); err != nil {
		return nil, fmt.Errorf("fetching consumption monthly (%s): %w", groupType, err)
	}
	return &resp, nil
}

// buildProviderKeysParam constructs the repeated provider_keys query parameter.
func buildProviderKeysParam() string {
	var parts []string
	for _, key := range AllProviderKeys {
		parts = append(parts, "provider_keys="+key)
	}
	return strings.Join(parts, "&")
}
