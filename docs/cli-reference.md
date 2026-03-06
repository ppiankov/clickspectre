# CLI Reference

## Commands

### `clickspectre analyze`

Analyze ClickHouse usage and generate reports.

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | auto | Config file path (default auto-load: `.clickspectre.yaml`) |
| `--clickhouse-dsn` | (required\*) | ClickHouse connection string |
| `--output` | `./report` | Output directory |
| `--format` | `json` | Output format (`json`, `text`, `sarif`) |
| `--baseline` | `""` | Baseline file path for suppressing known findings |
| `--update-baseline` | `false` | Update baseline with current findings |
| `--lookback` | `30d` | Lookback period (supports: 7d, 30d, 90d, 168h, etc.) |
| `--resolve-k8s` | `false` | Enable Kubernetes IP resolution |
| `--kubeconfig` | `~/.kube/config` | Path to kubeconfig |
| `--concurrency` | `5` | Worker pool size |
| `--batch-size` | `100000` | Query log batch size |
| `--max-rows` | `1000000` | Max rows to process |
| `--query-timeout` | `5m` | ClickHouse query timeout |
| `--k8s-cache-ttl` | `5m` | Kubernetes cache TTL |
| `--k8s-rate-limit` | `10` | K8s API rate limit (RPS) |
| `--detect-unused-tables` | `false` | Detect tables with zero usage |
| `--min-table-size` | `1.0` | Minimum table size in MB for unused table recommendations |
| `--min-query-count` | `0` | Minimum query count to consider a table active |
| `--exclude-table` | `[]` | Exclude table patterns (repeatable, supports glob) |
| `--exclude-database` | `[]` | Exclude database patterns (repeatable, supports glob) |
| `--anomaly-detection` | `true` | Enable anomaly detection |
| `--include-mv-deps` | `true` | Include materialized view deps |
| `--verbose` | `false` | Verbose logging |
| `--dry-run` | `false` | Don't write output |

\* `--clickhouse-dsn` is not required when `clickhouse_url` or `clickhouse_dsn` is set in config file.

### `clickspectre serve`

Serve the generated report via HTTP locally.

```bash
clickspectre serve [directory] [--port 8080]
```

### `clickspectre deploy`

Deploy report to Kubernetes cluster with automatic port-forwarding.

```bash
clickspectre deploy [report-directory] \
  --namespace <namespace> \
  --port <local-port> \
  [--ingress-host <domain>]
```

**Flags:**
- `--kubeconfig` — path to kubeconfig (default: `~/.kube/config`)
- `-n, --namespace` — Kubernetes namespace (default: `default`)
- `-p, --port` — local port for port-forward (default: `8080`)
- `--open` — auto-open browser (default: `true`)
- `--ingress-host` — external domain for Ingress

## Configuration

`clickspectre analyze` auto-loads config from `.clickspectre.yaml` in the current directory, then from home directory.

```yaml
clickhouse_url: clickhouse://readonly:password@host:9000/default
format: text
timeout: 10m
min_query_count: 3
exclude_tables:
  - analytics.tmp_*
exclude_databases:
  - sandbox_*
```

CLI flags override config file values.

## Exit codes

| Code | Meaning | Agent action |
|------|---------|--------------|
| 0 | Success — no findings | No action needed |
| 1 | Internal error | Retry or escalate |
| 2 | Invalid argument | Fix flags and retry |
| 3 | Not found | Check paths |
| 5 | Network error | Check ClickHouse connectivity |
| 6 | Findings detected | Parse JSON output for details |
