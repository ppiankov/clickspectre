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

## Commands

### clickspectre analyze

Analyze ClickHouse usage and generate report. Alias: `audit`.

**Flags:**
- `--clickhouse-dsn` — ClickHouse DSN (required)
- `--format json` — output format: json, text, sarif (default: json)
- `--output` — output directory (default: ./report)
- `--lookback` — lookback period, e.g. 7d, 30d, 90d (default: 30d)
- `--resolve-k8s` — enable Kubernetes IP resolution
- `--kubeconfig` — path to kubeconfig (default: ~/.kube/config)
- `--batch-size` — query log batch size (default: 100000)
- `--max-rows` — max query log rows to process (default: 1000000)
- `--query-timeout` — query timeout (default: 5m)
- `--detect-unused-tables` — detect tables with zero usage in query logs
- `--min-table-size` — minimum table size in MB for unused table recommendations (default: 1)
- `--exclude-table` — exclude table pattern (repeatable, supports glob)
- `--exclude-database` — exclude database pattern (repeatable, supports glob)
- `--baseline` — path to baseline file for suppressing known findings
- `--update-baseline` — update baseline with current findings
- `--dry-run` — dry run mode (don't write output)
- `--config` — path to config file (default: auto-load .clickspectre.yaml)
- `--concurrency` — worker pool size (default: 5)
- `--anomaly-detection` — enable anomaly detection (default: true)
- `--verbose` — verbose logging

**JSON output:**
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
      "query_count": 1250
    }
  ],
  "services": [
    {
      "name": "analytics-service",
      "ip": "10.0.1.5",
      "k8s_pod": "analytics-service-abc123",
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
    "safe_to_drop": ["default.tmp_import"],
    "keep": ["default.events", "default.users"]
  }
}
```

**Exit codes:**
- 0: success (no findings)
- 1: internal error
- 2: invalid arguments
- 3: not found (DSN unreachable)
- 5: network error
- 6: findings detected

### clickspectre serve

Serve generated report locally.

**Flags:**
- `--port` — port to serve on (default: 8080)
- `--dir` — directory to serve (default: ./report)

### clickspectre deploy

Deploy report to Kubernetes.

**Flags:**
- `--kubeconfig` — path to kubeconfig
- `--namespace` — Kubernetes namespace (default: default)
- `--port` — local port for port-forward (default: 8080)
- `--ingress-host` — host for Ingress
- `--report` — report directory to deploy (default: ./report)

### clickspectre version

Print version info.

### clickspectre init

Not implemented. Reads `.clickspectre.yaml` from cwd or home directory if present.

## Config file

Create `.clickspectre.yaml` to avoid repeating flags:

```yaml
clickhouse_dsn: "clickhouse://reader:secret@clickhouse:9000/default"
lookback: "30d"
resolve_k8s: true
format: "json"
output: "./report"
detect_unused_tables: true
exclude_database:
  - "system"
  - "INFORMATION_SCHEMA"
```

## What this does NOT do

- No table creation or deletion — read-only analysis
- No automatic cleanup — recommendations only, humans decide
- No persistent state — every run is a fresh analysis
- No real-time monitoring — point-in-time snapshots
- No MergeTree optimization — reports usage, not tuning

## Parsing examples

```bash
# Basic analysis
clickspectre analyze --clickhouse-dsn "clickhouse://user:pass@host:9000/default" --format json --output ./report

# Parse cleanup recommendations
jq '.cleanup_recommendations.safe_to_drop' ./report/report.json

# Unused table detection with filtering
clickspectre analyze --clickhouse-dsn "clickhouse://..." --detect-unused-tables --min-table-size 100 --exclude-database "system" --format json

# Baseline diffing
clickspectre analyze --clickhouse-dsn "..." --format json --update-baseline --baseline baseline.json
clickspectre analyze --clickhouse-dsn "..." --format json --baseline baseline.json
```
