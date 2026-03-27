// Package exporter implements a Prometheus collector for Servercore billing metrics.
package exporter

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/teamfighter/servercore-billing-exporter/api"
	"github.com/teamfighter/servercore-billing-exporter/openstack"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "sc"

	// kopecksPerUnit converts API monetary values (kopecks) to currency units.
	kopecksPerUnit = 100.0

	// hoursPerDay converts prediction hours to days.
	hoursPerDay = 24.0

	// timeFormat is the layout for Servercore API timestamps.
	timeFormat = "2006-01-02T15:04:05"
)

// TagOverrides maps VM name prefixes to tag key-value overrides.
// Used as a fallback for VMs that have no tags in OpenStack.
type TagOverrides map[string]map[string]string

// LoadTagOverrides reads tag overrides from a JSON file.
// Returns nil map if path is empty (feature disabled).
func LoadTagOverrides(path string) (TagOverrides, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading tag overrides file: %w", err)
	}
	var overrides TagOverrides
	if err := json.Unmarshal(data, &overrides); err != nil {
		return nil, fmt.Errorf("parsing tag overrides JSON: %w", err)
	}
	return overrides, nil
}

// serviceDisplayNames maps Servercore provider_key values to human-readable names.
var serviceDisplayNames = map[string]string{
	"vpc":        "Cloud Compute",
	"dbaas":      "Managed DB",
	"mks":        "Managed K8s",
	"storage":    "Object Storage",
	"cdn":        "CDN",
	"craas":      "Container Registry",
	"serverless": "Serverless",
	"vmware":     "VMware",
	"ones":       "1C",
	"mobfarm":    "Mobile Farm",
	"ses":        "Email (SES)",
}

// humanizeService returns a display name for a Servercore provider_key.
func humanizeService(key string) string {
	if name, ok := serviceDisplayNames[key]; ok {
		return name
	}
	return key
}

// clarifyUnit fixes ambiguous API units. Servercore bills most resources by time,
// returning cumulative quantity (e.g., core-hours) but generic units ("item", "MB").
func clarifyUnit(metric string, unit string) string {
	if strings.HasPrefix(metric, "traffic-req") {
		return "requests"
	}
	if strings.HasPrefix(metric, "traffic-") {
		return unit // "MB" or "GB" for pure volume is correct
	}
	
	// Everything else is a time-billed cumulative quantity.
	if unit == "item" {
		return "item-hours"
	}
	return unit + "-hours"
}

// cacheTTL is the duration historical data is considered fresh.
const cacheTTL = 24 * time.Hour

// Exporter collects Servercore billing metrics and implements prometheus.Collector.
type Exporter struct {
	client     *api.Client
	tagFetcher openstack.TagFetcher

	// Balance metrics.
	balanceByType *prometheus.Desc
	balanceTotal  *prometheus.Desc
	debtTotal     *prometheus.Desc
	debtByService *prometheus.Desc

	// Prediction.
	predictionDays *prometheus.Desc // now with billing_type label

	// Consumption (project-level, backward compatible).
	consumptionCost  *prometheus.Desc
	resourceCost     *prometheus.Desc
	resourceQuantity *prometheus.Desc

	// Historical consumption (monthly aggregated).
	consumptionCostMonthly *prometheus.Desc

	// Per-VM consumption with OpenStack tags.
	vmCost *prometheus.Desc

	// Per-disk/volume cost linked to parent VM.
	diskCost *prometheus.Desc

	// Scrape metadata.
	scrapeSuccess  *prometheus.Desc
	scrapeDuration *prometheus.Desc

	exportedTags  []string
	tagOverrides  TagOverrides
	historyMonths int

	// Cache for historical monthly data.
	historicalMu        sync.Mutex
	historicalCache     []api.ConsumptionItem
	historicalCacheTime time.Time
	nowFunc             func() time.Time // for testing
}

// New creates a new Exporter with the given API client.
// tagFetcher may be nil to disable OpenStack tag enrichment.
// historyMonths controls how many past months of billing data to expose (0 = disabled).
func New(client *api.Client, tagFetcher openstack.TagFetcher, exportedTags []string, tagOverrides TagOverrides, historyMonths int) *Exporter {
	vmCostLabels := append([]string{"project", "service", "metric", "vm_name"}, exportedTags...)
	diskCostLabels := append([]string{"project", "service", "metric", "disk_name", "parent_vm"}, exportedTags...)

	return &Exporter{
		client:        client,
		tagFetcher:    tagFetcher,
		exportedTags:  exportedTags,
		tagOverrides:  tagOverrides,
		historyMonths: historyMonths,
		nowFunc:       time.Now,

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
		consumptionCostMonthly: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "consumption", "cost_monthly"),
			"Historical monthly consumption cost by project, service, and period.",
			[]string{"project", "service", "period"}, nil,
		),
		resourceCost: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "resource", "cost"),
			"Current billing period resource cost by project, service, and metric.",
			[]string{"project", "service", "metric"}, nil,
		),
		resourceQuantity: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "resource", "quantity"),
			"Cumulative resource usage for the current billing period.",
			[]string{"project", "service", "metric", "unit"}, nil,
		),
		vmCost: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "vm", "cost"),
			"Per-VM resource cost with OpenStack tags.",
			vmCostLabels, nil,
		),
		diskCost: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "disk", "cost"),
			"Per-disk/volume cost linked to parent VM.",
			diskCostLabels, nil,
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
	ch <- e.consumptionCostMonthly
	ch <- e.resourceCost
	ch <- e.resourceQuantity
	ch <- e.vmCost
	ch <- e.diskCost
	ch <- e.scrapeSuccess
	ch <- e.scrapeDuration
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	success := 1.0

	// Fetch OpenStack tags (best-effort: failure only logs, doesn't fail the scrape).
	var globalTags map[string]openstack.ServerTags
	if e.tagFetcher != nil {
		var err error
		globalTags, err = e.tagFetcher.FetchAllTags()
		if err != nil {
			log.Printf("WARN: openstack tag enrichment failed: %v", err)
		}
	}

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

	if err := e.collectHistoricalConsumption(ch); err != nil {
		log.Printf("ERROR collecting historical consumption: %v", err)
		success = 0
	}

	if err := e.collectObjectConsumption(ch, globalTags); err != nil {
		log.Printf("ERROR collecting object consumption: %v", err)
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
	ch <- prometheus.MustNewConstMetric(e.balanceTotal, prometheus.GaugeValue, float64(b.FinalSum)/kopecksPerUnit)
	ch <- prometheus.MustNewConstMetric(e.debtTotal, prometheus.GaugeValue, float64(b.DebtSum)/kopecksPerUnit)

	for _, bal := range b.Balances {
		ch <- prometheus.MustNewConstMetric(e.balanceByType, prometheus.GaugeValue, float64(bal.Value)/kopecksPerUnit, bal.BalanceType)
	}

	for _, d := range b.Debt {
		ch <- prometheus.MustNewConstMetric(e.debtByService, prometheus.GaugeValue, float64(d.DebtValue)/kopecksPerUnit, d.ServiceType)
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
			ch <- prometheus.MustNewConstMetric(e.predictionDays, prometheus.GaugeValue, *val/hoursPerDay, billingType)
		}
	}
	return nil
}



// billingDateRange returns the start-of-month and now timestamps for billing queries.
func billingDateRange() (startDate, endDate string) {
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return startOfMonth.Format(timeFormat), now.Format(timeFormat)
}

func (e *Exporter) collectConsumption(ch chan<- prometheus.Metric) error {
	startDate, endDate := billingDateRange()

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
			float64(item.Value)/kopecksPerUnit,
			project, humanizeService(item.ProviderKey),
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
			float64(item.Value)/kopecksPerUnit,
			project, humanizeService(item.ProviderKey), item.Metric.ID,
		)
		ch <- prometheus.MustNewConstMetric(
			e.resourceQuantity, prometheus.GaugeValue,
			item.Metric.Quantity,
			project, humanizeService(item.ProviderKey), item.Metric.ID, clarifyUnit(item.Metric.ID, item.Metric.Unit),
		)
	}

	return nil
}

// collectHistoricalConsumption fetches and caches monthly billing data.
// Past months are cached for cacheTTL (24h); current month is excluded
// (it's already covered by collectConsumption).
func (e *Exporter) collectHistoricalConsumption(ch chan<- prometheus.Metric) error {
	if e.historyMonths <= 0 {
		return nil
	}

	items, err := e.getCachedHistoricalData()
	if err != nil {
		return err
	}

	now := e.nowFunc().UTC()
	currentPeriod := now.Format("2006-01")

	for _, item := range items {
		period := formatPeriod(item.Period)
		if period == currentPeriod {
			continue // skip current month, already in sc_consumption_cost
		}

		project := "unknown"
		if item.Project != nil {
			project = item.Project.Name
		}

		ch <- prometheus.MustNewConstMetric(
			e.consumptionCostMonthly, prometheus.GaugeValue,
			float64(item.Value)/kopecksPerUnit,
			project, humanizeService(item.ProviderKey), period,
		)
	}
	return nil
}

// getCachedHistoricalData returns cached data if fresh, otherwise fetches from API.
func (e *Exporter) getCachedHistoricalData() ([]api.ConsumptionItem, error) {
	e.historicalMu.Lock()
	defer e.historicalMu.Unlock()

	now := e.nowFunc()
	if e.historicalCache != nil && now.Sub(e.historicalCacheTime) < cacheTTL {
		if now.Month() == e.historicalCacheTime.Month() && now.Year() == e.historicalCacheTime.Year() {
			return e.historicalCache, nil
		}
	}

	startDate, endDate := historicalDateRange(now, e.historyMonths)
	resp, err := e.client.FetchConsumptionMonthly(startDate, endDate, "project")
	if err != nil {
		return nil, fmt.Errorf("historical consumption: %w", err)
	}

	e.historicalCache = resp.Data
	e.historicalCacheTime = now
	log.Printf("Refreshed historical billing cache: %d items, range %s..%s", len(resp.Data), startDate, endDate)
	return resp.Data, nil
}

// historicalDateRange computes a date range from N months ago to now.
func historicalDateRange(now time.Time, months int) (startDate, endDate string) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month()-time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	return start.Format(timeFormat), now.Format(timeFormat)
}

// formatPeriod converts an API period string (e.g. "2026-01-01T00:00:00") to "2026-01".
func formatPeriod(period string) string {
	if len(period) >= 7 {
		return period[:7]
	}
	return period
}

// diskObjectTypes are billing object types that represent disk/volume resources.
var diskObjectTypes = map[string]bool{
	"volume_gigabytes_fast":      true,
	"volume_gigabytes_basic":     true,
	"volume_gigabytes_universal": true,
	"volume_backups_gigabytes":   true,
	"snapshot_gigabytes_basic":   true,
	"snapshot_gigabytes_fast":    true,
}

// diskKey uniquely identifies a disk cost series for aggregation.
type diskKey struct {
	project  string
	service  string
	metric   string
	diskName string
	parentVM string
	tagsHash string
}

// collectObjectConsumption fetches per-object billing data.
// Emits sc_vm_cost for VMs and sc_disk_cost for disk/volume objects.
func (e *Exporter) collectObjectConsumption(ch chan<- prometheus.Metric, globalTags map[string]openstack.ServerTags) error {
	startDate, endDate := billingDateRange()

	resp, err := e.client.FetchConsumption(startDate, endDate, "object_metric")
	if err != nil {
		return fmt.Errorf("consumption (object_metric): %w", err)
	}

	// Pre-aggregate disk costs to avoid duplicate series when multiple
	// billing objects share the same name.
	diskAgg := make(map[diskKey]float64)
	vmAgg := make(map[diskKey]float64)

	for _, item := range resp.Data {
		if item.Object == nil || item.Metric == nil {
			continue
		}

		project := "unknown"
		if item.Project != nil {
			project = item.Project.Name
		}

		switch {
		case item.Object.Type == "cloud_vm":
			e.processVMItem(item, project, globalTags, vmAgg)
		case diskObjectTypes[item.Object.Type]:
			e.processDiskItem(item, project, globalTags, diskAgg)
		}
	}

	e.emitAggregatedMetrics(ch, diskAgg, e.diskCost, true)
	e.emitAggregatedMetrics(ch, vmAgg, e.vmCost, false)

	return nil
}

func (e *Exporter) emitAggregatedMetrics(ch chan<- prometheus.Metric, agg map[diskKey]float64, desc *prometheus.Desc, isDisk bool) {
	for key, val := range agg {
		var tags []string
		if key.tagsHash != "" {
			tags = strings.Split(key.tagsHash, "\x00")
		} else {
			tags = make([]string, len(e.exportedTags))
		}
		
		var labelValues []string
		if isDisk {
			labelValues = append([]string{key.project, key.service, key.metric, key.diskName, key.parentVM}, tags...)
		} else {
			labelValues = append([]string{key.project, key.service, key.metric, key.parentVM}, tags...)
		}
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, val, labelValues...)
	}
}

func (e *Exporter) processVMItem(item api.ConsumptionItem, project string, globalTags map[string]openstack.ServerTags, vmAgg map[diskKey]float64) {
	vmName, tagValues := e.resolveVM(item, globalTags)

	key := diskKey{
		project:  project,
		service:  humanizeService(item.ProviderKey),
		metric:   item.Metric.ID,
		parentVM: vmName, // Reusing parentVM as the main VM name
		tagsHash: strings.Join(tagValues, "\x00"),
	}
	vmAgg[key] += float64(item.Value) / kopecksPerUnit
}

func (e *Exporter) processDiskItem(item api.ConsumptionItem, project string, globalTags map[string]openstack.ServerTags, diskAgg map[diskKey]float64) {
	diskName := item.Object.Name
	if diskName == "" {
		diskName = item.Object.ID
	}
	
	parentVM := extractParentVMName(item.Object.ParentName)
	tagValues := e.extractTagValues(parentVM, parentVM, globalTags)

	key := diskKey{
		project:  project,
		service:  humanizeService(item.ProviderKey),
		metric:   item.Metric.ID,
		diskName: diskName,
		parentVM: item.Object.ParentName,
		tagsHash: strings.Join(tagValues, "\x00"),
	}
	diskAgg[key] += float64(item.Value) / kopecksPerUnit
}

// resolveVM extracts the VM display name and OpenStack tags for a billing item.
func (e *Exporter) resolveVM(item api.ConsumptionItem, globalTags map[string]openstack.ServerTags) (vmName string, tagValues []string) {
	vmName = item.Object.Name
	if vmName == "" {
		vmName = item.Object.ID
	}
	tagValues = e.extractTagValues(item.Object.ID, vmName, globalTags)
	return
}

func (e *Exporter) extractTagValues(vmID, vmName string, globalTags map[string]openstack.ServerTags) []string {
	if len(e.exportedTags) == 0 {
		return nil
	}
	tagValues := make([]string, len(e.exportedTags))
	for i := range tagValues {
		tagValues[i] = "Untagged"
	}
	if globalTags != nil {
		if tags, ok := globalTags[vmID]; ok {
			for i, t := range e.exportedTags {
				if tags[t] != "" {
					tagValues[i] = tags[t]
				}
			}
		}
	}
	// Fallback: apply prefix-based overrides for any remaining "Untagged" values.
	e.applyPrefixOverrides(vmName, tagValues)
	return tagValues
}

// applyPrefixOverrides checks if any tag value is still "Untagged" and applies
// prefix-based overrides from the TAG_OVERRIDES_FILE configuration.
func (e *Exporter) applyPrefixOverrides(vmName string, tagValues []string) {
	if len(e.tagOverrides) == 0 {
		return
	}
	for prefix, overrideTags := range e.tagOverrides {
		if strings.HasPrefix(vmName, prefix) {
			for i, t := range e.exportedTags {
				if tagValues[i] == "Untagged" {
					if val, ok := overrideTags[t]; ok {
						tagValues[i] = val
					}
				}
			}
			return
		}
	}
}

// extractParentVMName extracts the VM name from a billing API parent_name.
// Billing API returns disk parents as "disk-for-<VM_NAME>-#N"; this strips
// the "disk-for-" prefix and the "-#N" suffix. If the format doesn't match,
// returns the original string.
func extractParentVMName(parentName string) string {
	name := strings.TrimPrefix(parentName, "disk-for-")
	if idx := strings.LastIndex(name, "-#"); idx >= 0 {
		name = name[:idx]
	}
	return name
}
