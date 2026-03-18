# Servercore Billing Exporter

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Prometheus exporter for [Servercore](https://servercore.com) cloud billing data.

Collects account balance, debt, consumption statistics, and billing predictions from the Servercore API and exposes them as Prometheus metrics.

## Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sc_balance_by_type` | Gauge | `type` | Balance by type (main, bonus) |
| `sc_balance_total` | Gauge | — | Total account balance |
| `sc_debt_total` | Gauge | — | Total account debt |
| `sc_debt_by_service` | Gauge | `service` | Debt per service (vpc, dbaas, mks, ...) |
| `sc_prediction_days` | Gauge | — | Days until balance exhaustion |
| `sc_consumption_cost` | Gauge | `project`, `service` | Monthly cost by project × service |
| `sc_resource_cost` | Gauge | `project`, `service`, `metric` | Cost per resource metric |
| `sc_resource_quantity` | Gauge | `project`, `service`, `metric`, `unit` | Resource quantity (vCPU, RAM, disk, etc.) |
| `sc_scrape_success` | Gauge | — | 1 if last scrape succeeded, 0 otherwise |
| `sc_scrape_duration_seconds` | Gauge | — | Duration of last API scrape |

## Configuration

| Environment Variable | Required | Default | Description |
|---------------------|----------|---------|-------------|
| `TOKEN` | ✅ | — | Servercore API static token |
| `LISTEN_ADDR` | ❌ | `:9876` | HTTP listen address |

### Getting a token

1. Go to [Servercore Control Panel](https://my.servercore.com/profile/apikeys)
2. Create a new API key
3. Copy the static token

## Running

### Docker Compose

```yaml
services:
  servercore-billing-exporter:
    image: ghcr.io/teamfighter/servercore-billing-exporter:latest
    ports:
      - "9876:9876"
    restart: unless-stopped
    environment:
      TOKEN: your_servercore_api_token
```

```sh
docker compose up -d
curl http://localhost:9876/metrics
```

### Docker

```sh
docker run -d \
  -p 9876:9876 \
  -e TOKEN=your_token \
  ghcr.io/teamfighter/servercore-billing-exporter:latest
```

### Binary

```sh
export TOKEN=your_token
./servercore-billing-exporter
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: servercore-billing
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app: servercore-billing
  template:
    metadata:
      labels:
        app: servercore-billing
    spec:
      containers:
        - name: exporter
          image: ghcr.io/teamfighter/servercore-billing-exporter:latest
          ports:
            - containerPort: 9876
          env:
            - name: TOKEN
              valueFrom:
                secretKeyRef:
                  name: servercore-billing
                  key: token
---
apiVersion: v1
kind: Service
metadata:
  name: servercore-billing
  namespace: monitoring
spec:
  ports:
    - name: metrics
      port: 9876
      targetPort: 9876
  selector:
    app: servercore-billing
```

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: servercore_billing
    scrape_interval: 60m
    static_configs:
      - targets: ["servercore-billing:9876"]
```

## Alert Examples

### Low balance alert

```yaml
groups:
  - name: servercore_billing
    rules:
      - alert: ServercoreLowBalance
        expr: sc_prediction_days < 30
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Servercore balance will run out in {{ $value }} days"
          description: "Account balance is predicted to be exhausted within 30 days."

      - alert: ServercoreDebt
        expr: sc_debt_total > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Servercore account has debt: {{ $value }}"
```

## Development

```sh
# Run tests
make test

# Build binary
make build

# Run linter
make vet
```

## API Endpoints Used

| Endpoint | Description |
|----------|-------------|
| `GET /v3/balances` | Account balance and debt |
| `GET /v2/billing/prediction` | Days until balance exhaustion |
| `GET /v1/cloud_billing/statistic/consumption` | Monthly consumption data |

Authentication is done via `X-Token` header with a static API token.

## License

MIT — see [LICENSE](LICENSE)
