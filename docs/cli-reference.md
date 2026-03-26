# CLI Reference

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--verbose` | `false` | Debug logging |
| `-q, --quiet` | `false` | Suppress non-error output |

## Commands

### `clickspectre query`

Fast, targeted queries against system.query_log.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN (comma-separated for multi-node) |
| `--table` | | Filter by table name |
| `--user` | | Filter by user |
| `--ip` | | Filter by client IP |
| `--by` | `table` | Group by: user, table, ip |
| `--lookback` | `24h` | Time window |
| `--top` | `20` | Max results |
| `--min-read-rows` | `0` | Filter by minimum read rows |
| `--show-queries` | `false` | Show sample SQL (auto-redacted) |
| `--format` | `text` | Output format (text, json) |

### `clickspectre who <table>`

Reverse dependency lookup — which services/users access a table.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--by` | `ip` | Group by: ip, user |
| `--lookback` | `7d` | Time window |
| `--top` | `20` | Max results |
| `--stdin` | `false` | Read table names from stdin |
| `--format` | `text` | Output format (text, json) |

### `clickspectre ls [database]`

List databases or tables within a database.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--sort` | `name` | Sort by: name, size, rows |
| `--format` | `text` | Output format (text, json) |

### `clickspectre top`

Show running ClickHouse queries (htop for ClickHouse).

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--watch` | `false` | Continuously refresh |
| `--interval` | `2s` | Refresh interval for --watch |
| `--min-elapsed` | `0` | Filter by elapsed seconds |
| `--user` | | Filter by user |
| `--top` | `20` | Max results |
| `--format` | `text` | Output format (text, json) |

### `clickspectre slow`

Slow query digest with duration percentiles.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--lookback` | `24h` | Time window |
| `--min-duration` | | Only patterns slower than this (e.g., 1s) |
| `--sort` | `duration` | Sort by: duration, count, read_rows |
| `--show-example` | `false` | Show one example query per pattern |
| `--top` | `20` | Max patterns |
| `--format` | `text` | Output format (text, json) |

### `clickspectre explain <table>`

Structured table intelligence summary.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--lookback` | `30d` | Analysis window |
| `--format` | `text` | Output format (text, json) |

### `clickspectre grants [user]`

User permissions audit from system.users and system.grants.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `--unused` | `false` | Only show users with grants but zero queries |
| `--lookback` | `30d` | Lookback for --unused |
| `--format` | `text` | Output format (text, json) |

### `clickspectre analyze`

Full table usage analysis with scoring and recommendations.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required\*) | ClickHouse DSN (comma-separated for multi-node) |
| `--config` | auto | Config file path |
| `--output` | `./report` | Output directory (use `-` for stdout) |
| `--format` | `json` | Output format (json, text, sarif, spectrehub) |
| `--lookback` | `30d` | Lookback period |
| `--by-user` | `false` | Include per-user activity analysis |
| `--policy` | | Policy file for enforcement |
| `--baseline` | | Baseline file for suppressing known findings |
| `--update-baseline` | `false` | Update baseline with current findings |
| `--incremental` | `false` | Only fetch entries newer than last run |
| `--watermark-file` | auto | Watermark file path |
| `--reset-watermark` | `false` | Force full rescan |
| `--resolve-k8s` | `false` | Enable Kubernetes IP resolution |
| `--kubeconfig` | `~/.kube/config` | Path to kubeconfig |
| `--concurrency` | `5` | Worker pool size |
| `--batch-size` | `100000` | Query log batch size |
| `--max-rows` | `1000000` | Max rows to process |
| `--query-timeout` | `5m` | ClickHouse query timeout |
| `--detect-unused-tables` | `false` | Detect tables with zero usage |
| `--min-table-size` | `1.0` | Min table size in MB for recommendations |
| `--min-query-count` | `0` | Min queries to consider active |
| `--exclude-table` | `[]` | Exclude table patterns (glob, repeatable) |
| `--exclude-database` | `[]` | Exclude database patterns (glob, repeatable) |
| `--anomaly-detection` | `true` | Enable anomaly detection |
| `--verbose` | `false` | Debug logging |
| `--dry-run` | `false` | Don't write output |

\* Not required when `clickhouse_dsn` is set in config file.

### `clickspectre diff <old> <new>`

Compare two analysis reports.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format (text, json) |

### `clickspectre watch`

Run analyze on a schedule and report table drift.

| Flag | Default | Description |
|------|---------|-------------|
| `--interval` | `24h` | Run frequency (minimum 1h) |
| `--state-file` | auto | Watch state file path |
| `--once` | `false` | Run once and exit (CI-friendly) |

### `clickspectre snapshot`

Save cluster state for offline analysis.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |
| `-o` | `snapshot.json` | Output file (use `-` for stdout) |
| `--lookback` | `30d` | Activity lookback |

### `clickspectre doctor`

Check connectivity and configuration health.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | | ClickHouse DSN |
| `--config` | | Config file path |
| `--format` | `text` | Output format (text, json) |

### `clickspectre init`

Create config file with defaults.

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | `false` | Overwrite existing config |
| `--with-policy` | `false` | Also generate policy template |

### `clickspectre ci-init`

Generate CI pipeline snippet.

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `gitlab` | CI platform (gitlab, github) |
| `--stage` | `validate` | CI stage name |

### `clickspectre mcp`

Start MCP server for agent integration.

| Flag | Default | Description |
|------|---------|-------------|
| `--clickhouse-dsn` | (required) | ClickHouse DSN |

### `clickspectre serve`

Serve report via HTTP locally.

```bash
clickspectre serve [directory] [--port 8080]
```

### `clickspectre deploy`

Deploy report to Kubernetes with port-forwarding.

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `~/.kube/config` | Path to kubeconfig |
| `-n, --namespace` | `default` | Kubernetes namespace |
| `-p, --port` | `8080` | Local port |
| `--open` | `true` | Auto-open browser |
| `--ingress-host` | | External domain for Ingress |

### `clickspectre version`

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | | Output format (json) |

## Configuration

`clickspectre analyze` auto-loads `.clickspectre.yaml` from the current directory.

```yaml
clickhouse_dsn: clickhouse://readonly:password@host:9000/default
format: text
query_timeout: 10m
min_query_count: 3
exclude_tables:
  - analytics.tmp_*
exclude_databases:
  - sandbox_*
```

CLI flags override config file values. Generate with `clickspectre init`.

## Policy

Table hygiene rules in `.clickspectre-policy.yaml`:

```yaml
max_zero_usage_days: 90
require_replication: true
max_table_size_gb: 500
min_query_count_per_30d: 10
```

Use with `clickspectre analyze --policy .clickspectre-policy.yaml`. Generate with `clickspectre init --with-policy`.

## Exit codes

| Code | Meaning | Agent action |
|------|---------|--------------|
| 0 | Success — no findings | No action needed |
| 1 | Internal error | Retry or escalate |
| 2 | Invalid argument | Fix flags and retry |
| 3 | Not found | Check paths |
| 5 | Network error | Check ClickHouse connectivity |
| 6 | Findings detected | Parse JSON output for details |
