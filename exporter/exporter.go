// Package exporter implements a Prometheus collector for Servercore billing metrics.
package exporter

import (
	"fmt"
	"log"
	"time"

	"github.com/teamfighter/servercore-billing-exporter/api"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "sc"

// Exporter collects Servercore billing metrics and implements prometheus.Collector.
type Exporter struct {
	client *api.Client

	// Balance metrics.
	balanceByType *prometheus.Desc
	balanceTotal  *prometheus.Desc
	debtTotal     *prometheus.Desc
	debtByService *prometheus.Desc

	// Prediction.
	predictionDays *prometheus.Desc // now with billing_type label

	// Consumption.
	consumptionCost     *prometheus.Desc
	resourceCost        *prometheus.Desc
	resourceQuantity    *prometheus.Desc

	// Scrape metadata.
	scrapeSuccess  *prometheus.Desc
	scrapeDuration *prometheus.Desc
}

// New creates a new Exporter with the given API client.
func New(client *api.Client) *Exporter {
	return &Exporter{
		client: client,

		balanceByType: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "balance", "by_type"),
			"Account balance by type (e.g. main, bonus) in account currency.",
			[]string{"type"}, nil,
		),
		balanceTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "balance", "total"),
			"Total account balance in account currency.",
			nil, nil,
		),
		debtTotal: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "debt", "total"),
			"Total account debt in account currency.",
			nil, nil,
		),
		debtByService: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "debt", "by_service"),
			"Debt amount per service in account currency.",
			[]string{"service"}, nil,
		),
		predictionDays: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "prediction", "days"),
			"Estimated number of days until the balance is exhausted.",
			[]string{"billing_type"}, nil,
		),
		consumptionCost: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "consumption", "cost"),
			"Current month consumption cost by project and service in account currency.",
			[]string{"project", "service"}, nil,
		),
		resourceCost: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "resource", "cost"),
			"Current month resource cost by project, service and metric in account currency.",
			[]string{"project", "service", "metric"}, nil,
		),
		resourceQuantity: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "resource", "quantity"),
			"Current month resource quantity by project, service, metric and unit.",
			[]string{"project", "service", "metric", "unit"}, nil,
		),
		scrapeSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "success"),
			"Whether the last scrape was successful (1 = success, 0 = failure).",
			nil, nil,
		),
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "scrape", "duration_seconds"),
			"Duration of the last scrape in seconds.",
			nil, nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.balanceByType
	ch <- e.balanceTotal
	ch <- e.debtTotal
	ch <- e.debtByService
	ch <- e.predictionDays
	ch <- e.consumptionCost
	ch <- e.resourceCost
	ch <- e.resourceQuantity
	ch <- e.scrapeSuccess
	ch <- e.scrapeDuration
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	success := 1.0

	if err := e.collectBalance(ch); err != nil {
		log.Printf("ERROR collecting balance: %v", err)
		success = 0
	}

	if err := e.collectPrediction(ch); err != nil {
		log.Printf("ERROR collecting prediction: %v", err)
		success = 0
	}

	if err := e.collectConsumption(ch); err != nil {
		log.Printf("ERROR collecting consumption: %v", err)
		success = 0
	}

	ch <- prometheus.MustNewConstMetric(e.scrapeSuccess, prometheus.GaugeValue, success)
	ch <- prometheus.MustNewConstMetric(e.scrapeDuration, prometheus.GaugeValue, time.Since(start).Seconds())
}

func (e *Exporter) collectBalance(ch chan<- prometheus.Metric) error {
	resp, err := e.client.FetchBalance()
	if err != nil {
		return err
	}

	if len(resp.Data.Billings) == 0 {
		return fmt.Errorf("no billing data returned")
	}

	b := resp.Data.Billings[0]
	ch <- prometheus.MustNewConstMetric(e.balanceTotal, prometheus.GaugeValue, float64(b.FinalSum))
	ch <- prometheus.MustNewConstMetric(e.debtTotal, prometheus.GaugeValue, float64(b.DebtSum))

	for _, bal := range b.Balances {
		ch <- prometheus.MustNewConstMetric(e.balanceByType, prometheus.GaugeValue, float64(bal.Value), bal.BalanceType)
	}

	for _, d := range b.Debt {
		ch <- prometheus.MustNewConstMetric(e.debtByService, prometheus.GaugeValue, float64(d.DebtValue), d.ServiceType)
	}

	return nil
}

func (e *Exporter) collectPrediction(ch chan<- prometheus.Metric) error {
	resp, err := e.client.FetchPrediction()
	if err != nil {
		return err
	}

	// Emit a metric for each non-nil billing type prediction.
	predictions := map[string]*float64{
		"primary": resp.Data.Primary,
		"storage": resp.Data.Storage,
		"vmware":  resp.Data.Vmware,
		"vpc":     resp.Data.VPC,
	}
	for billingType, val := range predictions {
		if val != nil {
			ch <- prometheus.MustNewConstMetric(e.predictionDays, prometheus.GaugeValue, *val, billingType)
		}
	}
	return nil
}

func (e *Exporter) collectConsumption(ch chan<- prometheus.Metric) error {
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	startDate := startOfMonth.Format("2006-01-02T15:04:05")
	endDate := now.Format("2006-01-02T15:04:05")

	// Fetch aggregated consumption by project × service.
	projResp, err := e.client.FetchConsumption(startDate, endDate, "project")
	if err != nil {
		return fmt.Errorf("consumption (project): %w", err)
	}

	for _, item := range projResp.Data {
		project := "unknown"
		if item.Project != nil {
			project = item.Project.Name
		}
		ch <- prometheus.MustNewConstMetric(
			e.consumptionCost, prometheus.GaugeValue,
			float64(item.Value),
			project, item.ProviderKey,
		)
	}

	// Fetch detailed consumption by project × service × metric.
	metricResp, err := e.client.FetchConsumption(startDate, endDate, "project_metric")
	if err != nil {
		return fmt.Errorf("consumption (project_metric): %w", err)
	}

	for _, item := range metricResp.Data {
		project := "unknown"
		if item.Project != nil {
			project = item.Project.Name
		}
		if item.Metric == nil {
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			e.resourceCost, prometheus.GaugeValue,
			float64(item.Value),
			project, item.ProviderKey, item.Metric.ID,
		)
		ch <- prometheus.MustNewConstMetric(
			e.resourceQuantity, prometheus.GaugeValue,
			item.Metric.Quantity,
			project, item.ProviderKey, item.Metric.ID, item.Metric.Unit,
		)
	}

	return nil
}
