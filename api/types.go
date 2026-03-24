// Package api provides a client for the Servercore Billing API.
//
// It supports fetching account balance, billing prediction,
// and consumption statistics from the Servercore cloud platform.
package api

// BalanceResponse represents the response from /v3/balances.
type BalanceResponse struct {
	Data struct {
		Billings []Billing `json:"billings"`
	} `json:"data"`
	Settings BalanceSettings `json:"settings"`
}

// BalanceSettings contains account-level billing settings.
type BalanceSettings struct {
	Currency string `json:"currency"`
	Mode     string `json:"mode"`
}

// Billing represents a single billing entry within the balance response.
type Billing struct {
	BillingType string    `json:"billing_type"`
	FinalSum    int64     `json:"final_sum"`
	DebtSum     int64     `json:"debt_sum"`
	Balances    []Balance `json:"balances"`
	Debt        []Debt    `json:"debt"`
}

// Balance represents an individual balance (e.g. main, bonus).
type Balance struct {
	BalanceType string `json:"balance_type"`
	Value       int64  `json:"value"`
}

// Debt represents debt for a particular service.
type Debt struct {
	ServiceType string `json:"service_type"`
	DebtValue   int64  `json:"debt_value"`
}

// PredictionResponse represents the response from /v2/billing/prediction.
type PredictionResponse struct {
	Data PredictionData `json:"data"`
}

// PredictionData contains the prediction values per billing type.
// Values are in days. A nil value means no prediction is available.
type PredictionData struct {
	Primary *float64 `json:"primary"`
	Storage *float64 `json:"storage"`
	Vmware  *float64 `json:"vmware"`
	VPC     *float64 `json:"vpc"`
}

// ConsumptionResponse represents the response from
// /v1/cloud_billing/statistic/consumption.
type ConsumptionResponse struct {
	Status string            `json:"status"`
	Data   []ConsumptionItem `json:"data"`
}

// ConsumptionItem is a single consumption record.
type ConsumptionItem struct {
	AccountID    string              `json:"account_id"`
	ProviderKey  string              `json:"provider_key"`
	Value        int64               `json:"value"`
	Period       string              `json:"period"`
	Project      *ConsumptionProject `json:"project"`
	Metric       *ConsumptionMetric  `json:"metric"`
	Object       *ConsumptionObject  `json:"object"`
	ProvisionEnd string              `json:"provision_end"`
}

// ConsumptionProject identifies the project in a consumption record.
type ConsumptionProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ConsumptionMetric describes the resource metric (e.g. vCPU, RAM).
type ConsumptionMetric struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Region   string  `json:"region"`
}

// ConsumptionObject identifies a specific billing object.
type ConsumptionObject struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParentName string `json:"parent_name"`
}
