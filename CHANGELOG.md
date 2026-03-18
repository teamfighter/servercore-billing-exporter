# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/teamfighter/servercore-billing-exporter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/teamfighter/servercore-billing-exporter/releases/tag/v0.1.0
