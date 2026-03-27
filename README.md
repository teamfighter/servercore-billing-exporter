# Servercore Billing Exporter

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Prometheus exporter for [Servercore](https://servercore.com) cloud billing data.

Collects account balance, debt, consumption statistics, and billing predictions from the Servercore API and exposes them as Prometheus metrics. Optionally enriches VM-level metrics with OpenStack tags for team/owner attribution.

## Features

- **Account-level metrics** — balance, debt, and balance depletion prediction
- **Consumption breakdown** — monthly costs by project × service
- **Historical consumption** — past month billing costs properly cached (1 day TTL) for longitudinal tracking
- **Per-resource detail** — cost and quantity per metric (vCPU, RAM, disk, traffic, etc.)
- **Per-VM cost attribution** — individual VM costs with custom tag labels from OpenStack
- **Per-disk/volume costs** — disk costs linked to their parent VM
- **Tag enrichment** — automatic label injection from OpenStack VM tags
- **Tag overrides** — prefix-based fallback tags for untagged VMs (via JSON config)
- **Multi-project support** — single exporter instance can query multiple OpenStack projects
- **Config generator script** — converts Servercore OpenStack RC files to INI config

## Metrics

### Account & Billing

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sc_balance_by_type` | Gauge | `type` | Balance by type (main, bonus) |
| `sc_balance_total` | Gauge | — | Total account balance |
| `sc_debt_total` | Gauge | — | Total account debt |
| `sc_debt_by_service` | Gauge | `service` | Debt per service |
| `sc_prediction_days` | Gauge | `billing_type` | Days until balance exhaustion per billing type |

### Consumption (project-level)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sc_consumption_cost` | Gauge | `project`, `service` | Monthly cost for the current metric month by project × service |
| `sc_consumption_cost_monthly` | Gauge | `project`, `service`, `period` | Historical monthly cost by project × service (cached 24h, excludes current active period) |
| `sc_resource_cost` | Gauge | `project`, `service`, `metric` | Cost per resource metric |
| `sc_resource_quantity` | Gauge | `project`, `service`, `metric`, `unit` | Resource quantity (vCPU-hours, GB-hours, etc.) |

### Per-VM / Per-Disk (object-level, requires `OPENSTACK_CONFIG`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sc_vm_cost` | Gauge | `project`, `service`, `metric`, `vm_name`, `<tags>...` | Per-VM resource cost with custom tag labels |
| `sc_disk_cost` | Gauge | `project`, `service`, `metric`, `disk_name`, `parent_vm`, `<tags>...` | Per-disk/volume cost linked to parent VM |

> The `<tags>` labels are dynamically defined by the `EXPORTED_TAGS` environment variable (e.g., `env`, `owner`, `cost_center`).

### Scrape Metadata

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sc_scrape_success` | Gauge | — | 1 if last scrape succeeded, 0 otherwise |
| `sc_scrape_duration_seconds` | Gauge | — | Duration of last API scrape |

## Configuration

| Environment Variable | Required | Default | Description |
|---------------------|----------|---------|-------------|
| `TOKEN` | ✅ | — | Servercore API static token |
| `LISTEN_ADDR` | ❌ | `:9876` | HTTP listen address |
| `BILLING_HISTORY_MONTHS` | ❌ | `12` | Depth in months for fetching historical data (`sc_consumption_cost_monthly`). Set to `0` to disable history scraping |
| `OPENSTACK_CONFIG` | ❌ | — | Path to INI file with OpenStack credentials (enables tag enrichment and per-VM metrics) |
| `EXPORTED_TAGS` | ❌ | — | Comma-separated list of OpenStack tag keys to export as Prometheus labels |
| `TAG_OVERRIDES_FILE` | ❌ | — | Path to JSON file mapping VM name prefixes to tag values (fallback for untagged VMs) |

### Getting a token

1. Go to [Servercore Control Panel](https://my.servercore.com/profile/apikeys)
2. Create a new API key
3. Copy the static token

### OpenStack tag enrichment

When `OPENSTACK_CONFIG` is set, the exporter authenticates against one or more OpenStack projects to read VM tags. Tags in the format `key=value` (e.g., `env=production`, `owner=user@example.com`) are extracted and injected as Prometheus labels into `sc_vm_cost` and `sc_disk_cost` metrics.

#### Step 1: Generate the config file

Download the OpenStack RC files from the Servercore panel for each project you want to enrich. Then use the provided script:

```sh
# Generate config.ini from OpenStack RC files (one password per batch):
OS_PASSWORD='your_openstack_password' ./scripts/generate-config.sh rc-project1.sh rc-project2.sh

# If you have projects with a different password, append with --append:
OS_PASSWORD='another_password' ./scripts/generate-config.sh --append rc-project3.sh
```

This produces a `config.ini` file:

```ini
[project1]
auth_url = https://cloud.servercore.com:5000/v3
project_id = abc123def456
domain_name = users
region_name = ru-1
username = user@example.com
password = your_openstack_password

[project2]
auth_url = https://cloud.servercore.com:5000/v3
project_id = 789xyz012abc
domain_name = users
region_name = ru-1
username = user@example.com
password = your_openstack_password
```

> ⚠️ Keep `config.ini` secure — it contains credentials. Do not commit it to version control.

#### Step 2: Configure tag export

Set `EXPORTED_TAGS` to the OpenStack tag keys you want as Prometheus labels:

```sh
EXPORTED_TAGS=env,owner,cost_center
```

This will add `env`, `owner`, and `cost_center` labels to `sc_vm_cost` and `sc_disk_cost` metrics. VMs without these tags will have the label value `Untagged`.

#### Step 3 (optional): Tag overrides for untagged VMs

If some VMs don't have OpenStack tags but follow a naming convention, you can assign tags based on VM name prefixes. Create a JSON file:

```json
{
  "k8s-prod-node":   {"env": "production", "cost_center": "infrastructure"},
  "k8s-dev-node":    {"env": "development"},
  "dwh-greenplum":   {"owner": "analytics@example.com"},
  "monitoring-node": {"cost_center": "operations"}
}
```

The exporter matches VM names using longest prefix first. Overrides are only applied to tags that are still `Untagged` after OpenStack lookup — explicit OpenStack tags always take precedence.

Set `TAG_OVERRIDES_FILE` to the path of this JSON file.

## Running

### Docker Compose (basic)

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

### Docker Compose (with tag enrichment)

```yaml
services:
  servercore-billing-exporter:
    image: ghcr.io/teamfighter/servercore-billing-exporter:latest
    ports:
      - "9876:9876"
    restart: unless-stopped
    environment:
      TOKEN: your_servercore_api_token
      OPENSTACK_CONFIG: /etc/exporter/config.ini
      EXPORTED_TAGS: env,owner,cost_center
      TAG_OVERRIDES_FILE: /etc/exporter/overrides.json
    volumes:
      - ./config.ini:/etc/exporter/config.ini:ro
      - ./overrides.json:/etc/exporter/overrides.json:ro
```

### Docker

```sh
docker run -d \
  -p 9876:9876 \
  -e TOKEN=your_token \
  ghcr.io/teamfighter/servercore-billing-exporter:latest
```

With tag enrichment:

```sh
docker run -d \
  -p 9876:9876 \
  -e TOKEN=your_token \
  -e OPENSTACK_CONFIG=/etc/exporter/config.ini \
  -e EXPORTED_TAGS=env,owner,cost_center \
  -v $(pwd)/config.ini:/etc/exporter/config.ini:ro \
  ghcr.io/teamfighter/servercore-billing-exporter:latest
```

### Binary

```sh
export TOKEN=your_token
./servercore-billing-exporter

# With tag enrichment:
export OPENSTACK_CONFIG=./config.ini
export EXPORTED_TAGS=env,owner,cost_center
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
            - name: OPENSTACK_CONFIG
              value: /etc/exporter/config.ini
            - name: EXPORTED_TAGS
              value: env,owner,cost_center
          volumeMounts:
            - name: openstack-config
              mountPath: /etc/exporter
              readOnly: true
      volumes:
        - name: openstack-config
          secret:
            secretName: servercore-billing-openstack
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
    scrape_timeout: 2m
    static_configs:
      - targets: ["servercore-billing:9876"]
```

> **Note:** Scrape timeout should be generous (1–2 min) because the Servercore billing API can be slow, especially for accounts with many resources. The default `scrape_interval: 60m` is recommended since billing data is hourly.

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

### Top VM cost alert

```yaml
      - alert: ServercoreHighVMCost
        expr: sum by (vm_name) (sc_vm_cost) > 10000000
        for: 1h
        labels:
          severity: info
        annotations:
          summary: "VM {{ $labels.vm_name }} monthly cost exceeds 10M"
```

## Scripts

### `scripts/generate-config.sh`

Converts Servercore OpenStack RC files into a `config.ini` understood by the exporter.

**Usage:**

```sh
# Convert one or more RC files (prompted for password if OS_PASSWORD is not set):
OS_PASSWORD=secret ./scripts/generate-config.sh rc-file1.sh rc-file2.sh

# Append to an existing config.ini (e.g., for projects with a different password):
OS_PASSWORD=other ./scripts/generate-config.sh --append rc-file3.sh
```

**How it works:**

1. Parses `OS_AUTH_URL`, `OS_PROJECT_ID`, `OS_PROJECT_DOMAIN_NAME`, `OS_REGION_NAME`, `OS_USERNAME` from each RC file
2. Uses the section name derived from the filename (strips `rc-`/`openrc-` prefix and `.sh` extension)
3. Combines them with the provided `OS_PASSWORD` into INI sections
4. Writes to `config.ini` in the current directory

**Example RC file** (`rc-production.sh`):

```sh
export OS_AUTH_URL="https://cloud.servercore.com:5000/v3"
export OS_PROJECT_ID="abc123"
export OS_PROJECT_DOMAIN_NAME="users"
export OS_REGION_NAME="ru-1"
export OS_USERNAME="admin@example.com"
```

**Generated output** (`config.ini`):

```ini
[production]
auth_url = https://cloud.servercore.com:5000/v3
project_id = abc123
domain_name = users
region_name = ru-1
username = admin@example.com
password = secret
```

## Development

```sh
# Run tests
make test

# Build binary
make build

# Run linter
make vet

# Build Docker image
make docker-build
```

## API Endpoints Used

| Endpoint | Description |
|----------|-------------|
| `GET /v3/balances` | Account balance and debt |
| `GET /v2/billing/prediction` | Days until balance exhaustion |
| `GET /v1/cloud_billing/statistic/consumption` | Monthly consumption data (project, project_metric, object_metric aggregations) |

Authentication is done via `X-Token` header with a static API token.

## Acknowledgments

- [selectel-billing-exporter](https://github.com/mxssl/selectel-billing-exporter) by [@mxssl](https://github.com/mxssl) — billing API client and exporter structure, adapted for the Servercore cloud platform.
- [openstack-resource-exporter](https://github.com/avorr/openstack-resource-exporter) by [@avorr](https://github.com/avorr) (Alexander Vorobyev) — OpenStack integration patterns and tag extraction approach.

## License

MIT — see [LICENSE](LICENSE)
