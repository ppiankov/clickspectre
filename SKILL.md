---
name: clickspectre
description: "ClickHouse usage analyzer — identifies unused tables, service dependencies, and cleanup recommendations"
user-invocable: false
metadata: {"requires":{"bins":["clickspectre"]}}
---

# clickspectre — ClickHouse Usage Analyzer

You have access to `clickspectre`, a ClickHouse usage analyzer that identifies unused tables, service-to-table dependencies, and generates cleanup recommendations.

## Install

```bash
brew install ppiankov/tap/clickspectre
```

Or download binary:

```bash
curl -LO https://github.com/ppiankov/clickspectre/releases/latest/download/clickspectre_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz
tar -xzf clickspectre_*.tar.gz
sudo mv clickspectre /usr/local/bin/
```

## Commands

| Command | What it does |
|---------|-------------|
| `clickspectre analyze --clickhouse-dsn <dsn>` | Analyze ClickHouse usage and generate report |
| `clickspectre analyze --format json` | JSON output for parsing |
| `clickspectre analyze --format sarif` | SARIF output for CI integration |
| `clickspectre serve` | Serve generated report locally |
| `clickspectre deploy` | Deploy report to Kubernetes |
| `clickspectre version` | Print version info |

`analyze` has alias `audit`.

## Key Flags

### analyze / audit

| Flag | Description |
|------|-------------|
| `--clickhouse-dsn` | ClickHouse DSN (required) |
| `--format` | Output format: json, text, sarif (default: json) |
| `--output` | Output directory (default: ./report) |
| `--lookback` | Lookback period, e.g. 7d, 30d, 90d (default: 30d) |
| `--resolve-k8s` | Enable Kubernetes IP resolution |
| `--kubeconfig` | Path to kubeconfig (default: ~/.kube/config) |
| `--batch-size` | Query log batch size (default: 100000) |
| `--max-rows` | Max query log rows to process (default: 1000000) |
| `--query-timeout` | Query timeout, e.g. 5m, 10m (default: 5m) |
| `--detect-unused-tables` | Detect tables with zero usage in query logs |
| `--min-table-size` | Minimum table size in MB for unused table recommendations (default: 1) |
| `--exclude-table` | Exclude table pattern (repeatable, supports glob) |
| `--exclude-database` | Exclude database pattern (repeatable, supports glob) |
| `--baseline` | Path to baseline file for suppressing known findings |
| `--update-baseline` | Update baseline with current findings |
| `--dry-run` | Dry run mode (don't write output) |
| `--config` | Path to config file (default: auto-load .clickspectre.yaml) |
| `--concurrency` | Worker pool size (default: 5) |
| `--min-query-count` | Minimum query count to consider a table active |
| `--anomaly-detection` | Enable anomaly detection (default: true) |
| `--include-mv-deps` | Include materialized view dependencies (default: true) |

### serve

| Flag | Description |
|------|-------------|
| `--port` | Port to serve on (default: 8080) |
| `--dir` | Directory to serve (default: ./report) |

### deploy

| Flag | Description |
|------|-------------|
| `--kubeconfig` | Path to kubeconfig (default: ~/.kube/config) |
| `-n`, `--namespace` | Kubernetes namespace (default: default) |
| `-p`, `--port` | Local port for port-forward (default: 8080) |
| `--open` | Automatically open browser (default: true) |
| `--ingress-host` | Host for Ingress (e.g. clickspectre.example.com) |
| `--report` | Report directory to deploy (default: ./report) |

### Global

| Flag | Description |
|------|-------------|
| `--verbose` | Verbose logging |

## Exit Codes

| Code | Meaning | Agent action |
|------|---------|--------------|
| `0` | Success (no findings) | Continue |
| `1` | Internal error | Fail job |
| `2` | Invalid arguments | Fix command and retry |
| `3` | Not found (DSN unreachable) | Check connectivity |
| `5` | Network error | Retry with backoff |
| `6` | Findings detected | Parse JSON and report |

## JSON Output Structure

### analyze --format json

```json
{
  "tool": "clickspectre",
  "version": "1.0.0",
  "timestamp": "2026-02-22T10:30:00Z",
  "metadata": {
    "clickhouse_dsn": "clickhouse://...",
    "lookback": "30d",
    "resolve_k8s": true
  },
  "tables": [
    {
      "database": "default",
      "table": "events",
      "engine": "MergeTree",
      "total_rows": 50000000,
      "total_bytes": 2147483648,
      "query_count": 1250,
      "read_count": 1100,
      "write_count": 150
    }
  ],
  "services": [
    {
      "name": "analytics-service",
      "ip": "10.0.1.5",
      "k8s_pod": "analytics-service-abc123",
      "k8s_namespace": "production",
      "query_count": 500
    }
  ],
  "edges": [
    {
      "service": "analytics-service",
      "table": "default.events",
      "query_count": 450,
      "query_type": "SELECT"
    }
  ],
  "anomalies": [
    {
      "type": "usage_spike",
      "table": "default.events",
      "description": "Query count 5x above baseline",
      "severity": "warning"
    }
  ],
  "cleanup_recommendations": {
    "zero_usage_non_replicated": ["default.tmp_import"],
    "zero_usage_replicated": ["default.old_events_replica"],
    "safe_to_drop": ["default.tmp_import"],
    "likely_safe": ["default.old_events_replica"],
    "keep": ["default.events", "default.users"]
  }
}
```

## What clickspectre Does NOT Do

- No table creation or deletion — read-only analysis
- No automatic cleanup — recommendations only, humans decide
- No persistent state — every run is a fresh analysis
- No real-time monitoring — point-in-time snapshots
- No MergeTree optimization — reports usage, not tuning

## Agent Usage Patterns

### Basic analysis

```bash
clickspectre analyze --clickhouse-dsn "clickhouse://user:pass@host:9000/default" --format json --output ./report
if [ $? -eq 6 ]; then
  # Findings detected — parse cleanup recommendations
  jq '.cleanup_recommendations.safe_to_drop' ./report/report.json
fi
```

### Analysis with Kubernetes resolution

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://user:pass@host:9000/default" \
  --resolve-k8s \
  --kubeconfig ~/.kube/config \
  --format json \
  --output ./report
```

### Unused table detection

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://user:pass@host:9000/default" \
  --detect-unused-tables \
  --min-table-size 100 \
  --exclude-database "system" \
  --exclude-table "_temporary_*" \
  --format json
```

### Baseline diffing (suppress known findings)

```bash
# First run — create baseline
clickspectre analyze --clickhouse-dsn "..." --format json --update-baseline --baseline baseline.json

# Subsequent runs — only new findings trigger exit code 6
clickspectre analyze --clickhouse-dsn "..." --format json --baseline baseline.json
```

### Serve report locally

```bash
clickspectre serve --dir ./report --port 9090
```

### Deploy report to Kubernetes

```bash
clickspectre deploy ./report --namespace monitoring --ingress-host clickspectre.example.com
```

### Config file

Create `.clickspectre.yaml` in cwd or home directory to avoid repeating flags:

```yaml
clickhouse_dsn: "clickhouse://reader:secret@clickhouse:9000/default"
lookback: "30d"
resolve_k8s: true
kubeconfig: "~/.kube/config"
format: "json"
output: "./report"
detect_unused_tables: true
min_table_size: 10
exclude_database:
  - "system"
  - "INFORMATION_SCHEMA"
exclude_table:
  - "_temporary_*"
  - ".inner.*"
```

Then run without flags:

```bash
clickspectre analyze  # uses config defaults
```
