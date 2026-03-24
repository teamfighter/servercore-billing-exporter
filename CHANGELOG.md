# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/teamfighter/servercore-billing-exporter/releases/tag/v0.1.0
