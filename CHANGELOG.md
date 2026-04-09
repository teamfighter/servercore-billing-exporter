# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-04-09

### Added

- **Monthly VM cost by team** (`sc_vm_cost_monthly`) — pre-aggregated monthly VM costs by team
  - Labels: `period` (e.g. `2026-03`) plus all `EXPORTED_TAGS` (e.g. `team`, `bo`, `to`)
  - Aggregates raw per-VM billing data in the exporter for low Prometheus cardinality (~300 series from ~22k raw items)
  - Separate 24-hour cache, independent from the project-level historical cache
  - Only `cloud_vm` objects are included (disks excluded to avoid double-counting)
  - Current month is excluded (already covered by `sc_vm_cost`)
  - Ghost VMs (deleted before current month) handled via `TAG_OVERRIDES_FILE` prefix matching
- **12 new unit tests** for the monthly VM cost feature
  - Covers: aggregation, caching, ghost VMs, prefix overrides, nil objects, current month exclusion, API failures
  - Exporter package coverage: 95.7%

### Changed

- **API client timeout** increased from 30s to 120s — the `object_metric` monthly query for 12 months is heavy; since it's cached for 24h this is safe

## [0.3.0] - 2026-03-27

### Added

- **Historical billing observability** — new `sc_consumption_cost_monthly` metric exposes historical billing data
  - Granularity: Aggregated by `project` and `service` for the past N months
  - Evaluated up to `BILLING_HISTORY_MONTHS` configured depth (default: 12)
  - Configurable exclusion of current month to prevent overlap with `sc_consumption_cost`
- **In-memory History Cache** — thread-safe cache for historical metrics
  - 24-hour TTL ensures the API is only queried once per day for immutable historical data, respecting exporter best practices

## [0.2.0] - 2026-03-24

### Added

- **OpenStack tag enrichment** — new `openstack` package fetches VM tags from OpenStack Compute API
  - Multi-project support via INI config file (`OPENSTACK_CONFIG`)
  - Concurrent tag fetching across projects using `errgroup`
  - Tags in `key=value` format are parsed and injected as Prometheus labels
- **Per-VM cost metric** (`sc_vm_cost`) — individual VM costs with dynamic tag labels
  - Labels: `project`, `service`, `metric`, `vm_name`, plus any tags from `EXPORTED_TAGS`
- **Per-disk/volume cost metric** (`sc_disk_cost`) — disk costs linked to parent VM
  - Labels: `project`, `service`, `metric`, `disk_name`, `parent_vm`, plus any tags from `EXPORTED_TAGS`
  - Supported disk types: `volume_gigabytes_fast`, `volume_gigabytes_basic`, `volume_gigabytes_universal`, `volume_backups_gigabytes`, `snapshot_gigabytes_basic`, `snapshot_gigabytes_fast`
- **Tag overrides** (`TAG_OVERRIDES_FILE`) — JSON-based prefix-to-tag mapping for untagged VMs
  - Fallback mechanism: only applied when OpenStack tags are missing
  - Prefix matching for VM name-based team/owner attribution
- **`EXPORTED_TAGS` environment variable** — configurable list of OpenStack tag keys to export as Prometheus labels
- **`scripts/generate-config.sh`** — helper script to convert Servercore OpenStack RC files into `config.ini`
  - Supports `--append` flag for multi-password project batches
  - Accepts `OS_PASSWORD` from environment or interactive prompt
- **`object_metric` consumption API call** — fetches per-object billing data for VM and disk cost breakdown
- **Billing API `object.type` and `object.parent_name` fields** — added to `ConsumptionObject` struct for object classification and disk-to-VM linking

### Changed

- Go version bumped from 1.23 to 1.25 (Dockerfile)
- `exporter.New()` now accepts `tagFetcher`, `exportedTags`, and `tagOverrides` parameters
- `billingDateRange()` extracted as a shared helper function
- Improved metric descriptions for `sc_resource_cost` and `sc_resource_quantity`

## [0.1.0] - 2026-03-18

### Added

- Prometheus exporter for Servercore billing API
- Account balance metrics (`sc_balance_total`, `sc_balance_by_type`)
- Account debt metrics (`sc_debt_total`, `sc_debt_by_service`)
- Billing prediction metric (`sc_prediction_days`)
- Consumption cost metrics by project and service (`sc_consumption_cost`)
- Detailed resource metrics by metric type (`sc_resource_cost`, `sc_resource_quantity`)
- Scrape metadata (`sc_scrape_success`, `sc_scrape_duration_seconds`)
- Health check endpoint (`/health`)
- Multi-stage Docker build
- Docker Compose configuration
- GitHub Actions CI with SonarCloud integration
- GoReleaser-based release workflow (semver tags)
- Comprehensive test suite covering edge-cases (84% total coverage, 100% for exporter module)
- Comprehensive README with deployment examples (Docker, Kubernetes, binary)

[Unreleased]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/teamfighter/servercore-billing-exporter/releases/tag/v0.1.0
