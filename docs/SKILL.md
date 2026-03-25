# clickspectre

ClickHouse table usage analyzer and cleanup advisor.

## Install

```
brew install ppiankov/tap/clickspectre
```

Or via Go:

```
go install github.com/ppiankov/clickspectre/cmd/clickspectre@latest
```

## Commands

### clickspectre analyze

Analyze ClickHouse query logs to determine table usage, service dependencies, and cleanup recommendations. Alias: `audit`.

**Required flags:**
- `--clickhouse-dsn` — ClickHouse connection string (`clickhouse://user:pass@host:9000/db`)

**Output flags:**
- `--format json` — structured JSON report (default)
- `--format text` — human-readable text report
- `--format sarif` — SARIF 2.1.0 for CI/GitHub Security tab
- `--format spectrehub` — SpectreHub spectre/v1 envelope for cross-tool aggregation
- `--output ./report` — output directory (default: `./report`); use `--output -` for stdout

**Analysis flags:**
- `--lookback 30d` — how far back to scan query_log (default: 30d)
- `--query-timeout 5m` — per-query timeout (default: 5m)
- `--batch-size 100000` — query log batch size (default: 100000)
- `--max-rows 1000000` — max query log rows (default: 1000000)
- `--min-query-count 0` — minimum queries to consider a table active
- `--min-table-size 1` — minimum table size in MB for recommendations (default: 1)
- `--exclude-table pattern` — glob pattern to exclude tables (repeatable)
- `--exclude-database pattern` — glob pattern to exclude databases (repeatable)
- `--anomaly-detection` — enable anomaly detection (default: true)
- `--detect-unused-tables` — detect tables with zero usage
- `--include-mv-deps` — include materialized view dependencies (default: true)
- `--scoring-algorithm simple` — scoring algorithm (default: simple)
- `--concurrency 5` — worker pool size (default: 5)

**Kubernetes flags:**
- `--resolve-k8s` — resolve client IPs to K8s service names
- `--kubeconfig path` — path to kubeconfig
- `--k8s-cache-ttl 5m` — K8s cache TTL (default: 5m)
- `--k8s-rate-limit 10` — K8s API rate limit (default: 10 req/s)

**Baseline flags:**
- `--baseline path` — suppress known findings from a previous run
- `--update-baseline` — merge current findings into baseline file

**Other:**
- `--config path` — config file path (default: auto-load `.clickspectre.yaml`)
- `--dry-run` — show what would be analyzed without writing output
- `--verbose` — debug logging
- `-q, --quiet` — suppress non-error output (for agent piping)

**JSON output (--format json):**
```json
{
  "tool": "clickspectre",
  "version": "1.0.2",
  "timestamp": "2026-03-25T12:00:00Z",
  "metadata": {
    "generated_at": "2026-03-25T12:00:00Z",
    "lookback_days": 30,
    "clickhouse_host": "host:9000",
    "total_queries_analyzed": 142831,
    "analysis_duration": "12.3s",
    "k8s_resolution_enabled": false
  },
  "tables": [
    {
      "name": "events",
      "database": "default",
      "engine": "MergeTree",
      "is_replicated": false,
      "size_mb": 1024.5,
      "rows": 5000000
    }
  ],
  "services": [],
  "edges": [],
  "anomalies": [],
  "cleanup_recommendations": {
    "zero_usage_non_replicated": [],
    "zero_usage_replicated": [],
    "safe_to_drop": [],
    "likely_safe": [],
    "keep": []
  }
}
```

**SpectreHub output (--format spectrehub):**
```json
{
  "schema": "spectre/v1",
  "tool": "clickspectre",
  "version": "1.0.2",
  "timestamp": "2026-03-25T12:00:00Z",
  "target": { "type": "clickhouse", "uri_hash": "sha256:abc123..." },
  "findings": [
    {
      "id": "ZERO_USAGE_TABLE",
      "severity": "high",
      "location": "default.old_events",
      "message": "Table has zero queries in 30d lookback"
    }
  ],
  "summary": { "total": 1, "high": 1, "medium": 0, "low": 0, "info": 0 }
}
```

**Exit codes:**
- 0: analysis complete, no findings
- 1: internal error
- 2: invalid arguments (bad flags, invalid format, bad duration)
- 3: file or path not found (missing DSN, output dir, report file)
- 5: network error (connection refused, timeout, unreachable)
- 6: analysis complete, findings detected (unused tables, anomalies)

### clickspectre serve

Start a local HTTP server to view the generated report.

**Flags:**
- `--dir ./report` — directory to serve (default: `./report`)
- `--port 8080` — port to serve on (default: 8080)

**Exit codes:**
- 0: server stopped cleanly
- 1: internal error (port in use, directory not found)

### clickspectre deploy

Deploy report to Kubernetes as an nginx pod with port-forwarding.

**Flags:**
- `--report ./report` — report directory (default: `./report`)
- `--kubeconfig path` — path to kubeconfig
- `-n, --namespace default` — Kubernetes namespace (default: `default`)
- `-p, --port 8080` — local port for port-forward (default: 8080)
- `--open` — open browser automatically (default: true)
- `--ingress-host host` — host for Ingress resource

**Exit codes:**
- 0: deployed successfully
- 1: internal error
- 3: report directory not found
- 5: cluster unreachable

### clickspectre init

Create a `.clickspectre.yaml` config file with commented defaults.

**Flags:**
- `--force` — overwrite existing config file

**Exit codes:**
- 0: config created
- 1: config already exists (without --force) or write error

### clickspectre version

Show version, commit, Go version, and platform.

**Flags:**
- `--format json` — output as JSON for agent consumption

**Exit codes:**
- 0: always

## Handoffs

- Output: JSON report with tables/services/edges/recommendations. Next: `clickspectre serve` to view interactively, or feed to SpectreHub for aggregation.
- Output: SARIF. Next: upload to GitHub Security tab or CI security gates.
- Output: spectre/v1 envelope. Next: SpectreHub for cross-scanner aggregation.
- Refused questions: how to fix findings, whether to drop tables, risk acceptance decisions, ClickHouse optimization advice.

## What this does NOT do

- Does not modify, delete, or alter ClickHouse tables or schema — analysis is strictly read-only
- Does not store findings or manage a findings database — each run produces a fresh report
- Does not replace ClickHouse monitoring tools — this is a point-in-time usage audit, not continuous monitoring
- Does not make cleanup decisions — it presents evidence and scores safety, humans decide

## Failure Modes

- Authentication failure: exits 5. Distrust: all output fields. Safe fallback: report auth failure, do not cache results.
- Network timeout: exits 5. Distrust: completeness of table inventory and query counts. Safe fallback: partial results with warning, note incomplete scan.
- Invalid DSN: exits 2. Distrust: nothing ran. Safe fallback: check DSN format and retry.
- Pagination limit reached (--max-rows): exits 0 or 6 normally but results may be incomplete. Distrust: query count accuracy for tables near the threshold. Safe fallback: increase --max-rows or narrow --lookback.
- ClickHouse version incompatibility: system.query_log schema varies across CH versions. Exits 1 with scan error if expected columns are missing.

## Parsing examples

```bash
# Extract cleanup recommendations
clickspectre analyze --clickhouse-dsn $DSN --format json
cat ./report/report.json | jq '.cleanup_recommendations.safe_to_drop'

# Count findings
cat ./report/report.json | jq '.cleanup_recommendations | (.zero_usage_non_replicated | length) + (.zero_usage_replicated | length)'

# SpectreHub pipeline
clickspectre analyze --clickhouse-dsn $DSN --format spectrehub
cat ./report/report.spectrehub.json | jq '.findings[] | select(.severity == "high")'

# SARIF for CI
clickspectre analyze --clickhouse-dsn $DSN --format sarif
```

---

This tool follows the [Agent-Native CLI Convention](https://ancc.dev). Validate with: `ancc validate .`
