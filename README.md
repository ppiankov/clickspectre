# ClickSpectre

**ClickHouse usage analyzer that determines which tables are used, by whom, and which are safe to clean up.**

Part of the Spectre family of infrastructure cleanup tools.

ClickSpectre analyzes ClickHouse query logs to provide actionable insights about table usage, generates cleanup recommendations, and creates beautiful interactive visual reports with bipartite graphs showing service-to-table relationships.

## Why ClickSpectre Exists

ClickHouse is fast, powerful, and absolutely unforgiving when your schema grows faster than your documentation.
Teams end up with:

- Tables nobody remembers creating
- Schemas nobody wants to touch
- "Don't drop this or production dies" tribal knowledge
- Dashboards pointing at tables last queried during the Bronze Age
- Zero clarity about which services depend on what

ClickSpectre exists to answer a simple question:

**"Which ClickHouse tables are actually used, and by whom?"**

It turns vague fears and undocumented assumptions into concrete usage insights, so you can:

- Clean up safely
- Understand dependencies
- Reduce operational risk
- Stop relying on guesswork and hallway conversations

Born entirely out of real operational pain.
Shared so maybe yours hurts less.

## Features

**Core Features:**
- Analyzes ClickHouse `system.query_log` to identify table usage patterns
- Discovers which services/clients use which tables
- Provides safety-scored cleanup recommendations (safe/likely safe/keep)
- **NEW:** Detects tables with zero usage in query logs (never queried)
- **NEW:** Size-based filtering for unused tables (skip tiny tables)
- Generates interactive visual reports with D3.js bipartite graphs
- Optional Kubernetes IP→service resolution
- Concurrent processing with configurable worker pool
- Built-in safety mechanisms to protect ClickHouse and Kubernetes

**Safety First:**
- Read-only queries to ClickHouse
- Query timeouts and pagination
- K8s API rate limiting and caching
- Conservative cleanup recommendations
- Never recommends system tables or recently-written tables

## Readonly User Support

ClickSpectre **works with readonly ClickHouse users**, but with important considerations:

### Using Readonly Users

```bash
# Works with readonly users
clickspectre analyze \
  --clickhouse-dsn "clickhouse://readonly:password@host:9000/database" \
  --lookback 7d \
  --batch-size 1000 \
  --max-rows 50000
```

**Limitations with readonly users:**
- No query timeout protection (can't set `max_execution_time`)
- Queries may run longer than expected
- **Recommendation**: Use smaller batch sizes and shorter lookback periods

### Recommended: Non-Readonly User

For production use, create a dedicated user with SELECT-only permissions:

```sql
-- Create analysis user (not readonly mode, but still safe)
CREATE USER clickspectre IDENTIFIED BY 'your-password';

-- Grant SELECT permission on all databases/tables
GRANT SELECT ON *.* TO clickspectre;

-- This user can:
-- Read all data (SELECT)
-- Modify query settings (timeouts, limits)
-- Cannot drop/delete tables (no DDL permissions)
-- Cannot modify data (no DML permissions)
```

**Benefits:**
- Query timeout protection
- Better performance control
- Still 100% safe (read-only access)

Then use:
```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://clickspectre:password@host:9000/database" \
  --lookback 30d  # Can safely use longer periods
```

---

## Quick Start

### Installation

**Homebrew (macOS/Linux):**

```bash
brew install ppiankov/tap/clickspectre
```

**From Binary (Recommended):**

```bash
# macOS/Linux - Auto-detect platform
curl -L https://github.com/ppiankov/clickspectre/releases/latest/download/clickspectre-$(uname -s)-$(uname -m) -o clickspectre
chmod +x clickspectre
sudo mv clickspectre /usr/local/bin/
```

**Platform-Specific Downloads:**

- **Linux x86_64**: `clickspectre-Linux-x86_64`
- **Linux ARM64**: `clickspectre-Linux-arm64`
- **macOS Intel**: `clickspectre-Darwin-x86_64`
- **macOS Apple Silicon**: `clickspectre-Darwin-arm64`
- **Windows x86_64**: `clickspectre-Windows-x86_64.exe`

**Verify Installation:**

```bash
clickspectre version
```

**Zero-Install (Docker):**

```bash
# Run directly from GHCR
docker run --rm ghcr.io/ppiankov/clickspectre version

# Analyze without installing locally (use `audit` alias if preferred)
docker run --rm \
  -v "$PWD/report:/report" \
  ghcr.io/ppiankov/clickspectre analyze \
  --clickhouse-dsn "clickhouse://user:password@host:9000/default" \
  --output /report \
  --lookback 30d
```

**GitHub Action:**

```yaml
name: ClickSpectre

on:
  workflow_dispatch:

permissions:
  contents: read
  security-events: write

jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: ppiankov/clickspectre-action@v1
        with:
          clickhouse-url: clickhouse://user:password@host:9000/default
          format: sarif
          fail-on: likely
          args: --lookback 30d --detect-unused-tables
```

The action downloads the release binary, runs `clickspectre analyze`, and uploads SARIF to the Security tab when `format: sarif`.

**From Source:**

```bash
# Install via go install
go install github.com/ppiankov/clickspectre/cmd/clickspectre@latest

# Or build locally
git clone https://github.com/ppiankov/clickspectre.git
cd clickspectre
make build
```

### Basic Usage

```bash
# Analyze ClickHouse usage (30-day lookback)
clickspectre analyze \
  --clickhouse-dsn "clickhouse://user:password@host:9000/default" \
  --output ./my-report \
  --lookback 30d  # Supports: 7d, 30d, 90d, or 720h

# View the report locally
clickspectre serve ./my-report

# Open http://localhost:8080 in your browser

# Or deploy to Kubernetes (single command!)
clickspectre deploy ./my-report --namespace monitoring --port 8080
```

### With Kubernetes Resolution

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./report \
  --resolve-k8s \
  --kubeconfig ~/.kube/config \
  --lookback 30d
```

**Important:** If ClickHouse is behind a load balancer, you'll see LB IPs instead of real client IPs. To fix this, enable PROXY protocol on ClickHouse and your load balancer. See **[docs/CLICKHOUSE-REAL-CLIENT-IP.md](docs/CLICKHOUSE-REAL-CLIENT-IP.md)** for complete setup instructions.

### Detecting Unused Tables (Zero Usage)

ClickSpectre can detect tables that have **zero queries** in the lookback period - prime candidates for cleanup:

```bash
# Enable unused table detection (queries system.tables for complete inventory)
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./report \
  --lookback 30d \
  --detect-unused-tables \
  --min-table-size=10.0  # Only show tables >= 10MB
```

**How it works:**
1. Analyzes `system.query_log` to find which tables were queried
2. Queries `system.tables` to get complete table inventory
3. Compares the two to identify tables with **zero usage**
4. Separates replicated vs non-replicated tables (replicated might be intentional idle replicas)
5. Filters by size to focus on tables worth cleaning up

**Benefits:**
- **High-priority cleanup candidates** - Tables that have NEVER been queried
- **Size information** - Focus on large unused tables first
- **Replication awareness** - Flags replicated tables separately (they might be intentional)
- **Safe filtering** - Excludes system tables and applies materialized view dependency checks

**Report output:**
- **Zero Usage - Non-Replicated (High Priority)**: Safe deletion candidates
- **Zero Usage - Replicated (Review Carefully)**: Might be intentional idle replicas
- Shows table size, row count, engine type, and replication status

### Advanced Options

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./report \
  --lookback 90d \
  --concurrency 10 \
  --batch-size 50000 \
  --max-rows 2000000 \
  --query-timeout 10m \
  --resolve-k8s \
  --k8s-rate-limit 20 \
  --baseline ./.clickspectre-baseline.json \
  --update-baseline \
  --anomaly-detection \
  --verbose
```

### Baseline Mode (Suppress Known Findings)

Use a baseline file to suppress findings you've already reviewed:

```bash
# First run: capture current findings in a baseline
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --baseline ./.clickspectre-baseline.json \
  --update-baseline

# Next runs: show only new findings
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --baseline ./.clickspectre-baseline.json
```

### Agent Integration

ClickSpectre is designed for autonomous agent use. Single binary, deterministic output, structured JSON, bounded execution.

```bash
# Agent install (no brew needed)
curl -LO "https://github.com/ppiankov/clickspectre/releases/latest/download/clickspectre_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/').tar.gz"
tar -xzf clickspectre_*.tar.gz && sudo mv clickspectre /usr/local/bin/
```

Agents: read [`SKILL.md`](SKILL.md) for commands, flags, JSON output structure, and exit codes.

Key patterns:
- `clickspectre analyze --clickhouse-dsn <dsn> --format json` — table usage audit with cleanup recommendations
- `clickspectre analyze --clickhouse-dsn <dsn> --format sarif` — findings as SARIF for GitHub Security tab
- Exit code 6 means findings detected (unused tables, cleanup recommendations)

## CLI Commands

### `analyze`

Analyze ClickHouse usage and generate reports.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | auto | Config file path (default auto-load: `.clickspectre.yaml`/`.clickspectre.yml`) |
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
| `--query-timeout` | `5m` | ClickHouse query timeout (e.g., 5m, 10m, 1h) |
| `--k8s-cache-ttl` | `5m` | Kubernetes cache TTL (e.g., 5m, 10m, 1h) |
| `--k8s-rate-limit` | `10` | K8s API rate limit (RPS) |
| `--scoring-algorithm` | `simple` | Scoring algorithm |
| `--anomaly-detection` | `true` | Enable anomaly detection |
| `--include-mv-deps` | `true` | Include materialized view deps |
| `--detect-unused-tables` | `false` | Detect tables with zero usage (queries system.tables) |
| `--min-table-size` | `1.0` | Minimum table size in MB for unused table recommendations |
| `--min-query-count` | `0` | Minimum query count required to consider a table active |
| `--exclude-table` | `[]` | Exclude table patterns (repeatable, supports glob) |
| `--exclude-database` | `[]` | Exclude database patterns (repeatable, supports glob) |
| `--verbose` | `false` | Verbose logging |
| `--dry-run` | `false` | Don't write output |

\* `--clickhouse-dsn` is not required when `clickhouse_url` or `clickhouse_dsn` is set in config file.

### `serve`

Serve the generated report via HTTP locally.

```bash
clickspectre serve [directory] [--port 8080]
```

### `deploy`

Deploy report to Kubernetes cluster with automatic port-forwarding.

```bash
clickspectre deploy [report-directory] \
  --namespace <namespace> \
  --port <local-port> \
  [--ingress-host <domain>]
```

**Flags:**
- `--kubeconfig` - Path to kubeconfig (default: `~/.kube/config`)
- `-n, --namespace` - Kubernetes namespace (default: `default`)
- `-p, --port` - Local port for port-forward (default: `8080`)
- `--open` - Auto-open browser (default: `true`)
- `--ingress-host` - External domain for Ingress
- `--report` - Report directory (default: `./report`)

### `version`

Show version information.

```bash
clickspectre version
```

## Kubernetes IP Resolution

ClickSpectre can resolve client IP addresses to Kubernetes service names for better identification.

### Why K8s Resolution Matters

**Without K8s Resolution:**
```
Service: 10.0.1.100  ← Just an IP
```

**With K8s Resolution:**
```
Service: production/api-server  ← Meaningful name!
Namespace: production
Pod: api-server-xyz
```

### Setup Requirements

#### 1. Ensure Real Client IPs are Logged

If ClickHouse sits behind a load balancer, it sees the LB IP instead of client IPs. You must configure **PROXY protocol** to get real IPs.

**Quick fix for AWS NLB:**
```bash
# Enable proxy protocol on target group
aws elbv2 modify-target-group-attributes \
  --target-group-arn <your-tg-arn> \
  --attributes Key=proxy_protocol_v2.enabled,Value=true
```

**Then enable in ClickHouse** (`/etc/clickhouse-server/config.xml`):
```xml
<clickhouse>
    <tcp_port_proxy>9000</tcp_port_proxy>
</clickhouse>
```

**See complete guide:** [docs/CLICKHOUSE-REAL-CLIENT-IP.md](docs/CLICKHOUSE-REAL-CLIENT-IP.md)

#### 2. Run with K8s Resolution

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --resolve-k8s \
  --kubeconfig ~/.kube/config \
  --verbose
```

**See detailed documentation:** [docs/KUBERNETES-RESOLUTION.md](docs/KUBERNETES-RESOLUTION.md)

---

## Kubernetes Deployment

Deploy ClickSpectre reports to Kubernetes with a **single command**:

### Quick Deploy (Built-in Command)

```bash
# One-command deployment: creates namespace, configmap, deployment, service, and port-forward
clickspectre deploy ./my-report --namespace monitoring --port 8080

# The command automatically:
# - Creates namespace (if it doesn't exist)
# - Creates ConfigMap from report files
# - Deploys nginx pod
# - Creates Service
# - Sets up port-forwarding
# - Opens browser (can disable with --open=false)

# Access at: http://localhost:8080
```

### Custom Domain with Ingress

```bash
# Deploy with external access via Ingress
clickspectre deploy ./my-report \
  --namespace production \
  --ingress-host clickspectre.example.com

# Access at: https://clickspectre.example.com (after DNS configuration)
```

### Advanced Options

```bash
clickspectre deploy --help

Flags:
  --kubeconfig string     Path to kubeconfig (default: ~/.kube/config)
  -n, --namespace string  Kubernetes namespace (default "default")
  -p, --port int          Local port for port-forward (default 8080)
  --open                  Automatically open browser (default true)
  --ingress-host string   Host for Ingress (e.g., clickspectre.example.com)
  --report string         Report directory to deploy (default "./report")
```

### Manual Deployment (Alternative)

If you prefer manual deployment with shell scripts:

**See [k8s/README.md](k8s/README.md) and [k8s/EXAMPLES.md](k8s/EXAMPLES.md)** for:
- Docker image builds
- Custom deployments
- Automatic updates with CronJobs
- CI/CD integration
- Security configurations

## Report Structure

ClickSpectre generates output files based on `--format`:

```
report/
├── report.txt         # Human-readable report (--format text)
├── report.sarif       # SARIF output (--format sarif)
├── report.json        # Structured data (--format json)
├── index.html         # Interactive UI (--format json)
├── app.js             # Application logic (--format json)
├── styles.css         # Styling (--format json)
└── libs/
    └── d3.v7.min.js   # D3.js library (--format json)
```

Default (`--format json`) layout:

```
report/
├── index.html          # Interactive UI
├── app.js             # Application logic
├── styles.css         # Styling
├── report.json        # Structured data
└── libs/
    └── d3.v7.min.js   # D3.js library
```

### Report Contents

- **Overview**: Summary statistics (tables, services, queries, anomalies)
- **Graph**: D3.js bipartite graph visualization (service→table relationships)
- **Tables**: Sortable table list with usage stats and sparklines
- **Services**: List of services and their table usage
- **Cleanup**: Categorized recommendations with new zero-usage detection
  - **Zero Usage - Non-Replicated (High Priority)**: Tables never queried, not replicated
  - **Zero Usage - Replicated (Review Carefully)**: Tables never queried, but replicated
  - **Safe to Drop**: Low activity tables
  - **Likely Safe**: Moderate activity tables
  - **Keep**: Active tables
- **Anomalies**: Detected unusual access patterns

## Architecture

ClickSpectre follows a modular architecture:

- **Collector**: Queries ClickHouse with pagination and worker pool
- **Analyzer**: Processes query logs to build data models
- **Scorer**: Evaluates tables for cleanup safety
- **Reporter**: Generates JSON+HTML, text, or SARIF outputs
- **K8s Resolver**: (Optional) Resolves IPs to Kubernetes services

## Exit Codes

| Code | Meaning | Agent action |
|------|---------|--------------|
| 0 | Success — no findings | No action needed |
| 1 | Internal error | Retry or escalate |
| 2 | Invalid argument | Fix flags and retry |
| 3 | Not found | Check paths |
| 5 | Network error | Check ClickHouse connectivity |
| 6 | Findings detected | Parse JSON output for details |

## Safety Mechanisms

### ClickHouse Protection

- Query timeouts (when using non-readonly users)
- Batch processing (default: 100k rows/batch)
- Max rows limit (default: 1M rows)
- Connection pooling (max 10 connections)
- Self-exclusion (skips system.query_log queries)
- **Note**: Readonly users cannot use query timeouts due to ClickHouse permissions

### Kubernetes Protection

- In-memory cache (5 min TTL)
- Rate limiting (default: 10 RPS)
- Request timeouts (5s per request)
- Graceful fallback to raw IPs
- Optional disable

### Cleanup Recommendations

Conservative scoring that:
- Never recommends system tables
- Never recommends tables with writes in last 7 days
- Never recommends materialized views or MV dependencies
- Flags anomalous tables as "suspect" not "safe"
- **NEW:** Separates zero-usage tables by replication status
- **NEW:** Applies size filtering to focus on meaningful cleanup (configurable via `--min-table-size`)

## Development

```bash
# Build
make build

# Run tests
make test

# Format code
make fmt

# Run linters
make lint

# Clean build artifacts
make clean

# Install to $GOPATH/bin
make install

# Run all checks
make all
```

## Configuration

`clickspectre analyze` auto-loads config from `.clickspectre.yaml` (or `.clickspectre.yml`) in the current directory, then from your home directory.

Example `.clickspectre.yaml`:

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

CLI flags still override config file values.

Example using config + overrides:

```bash
clickspectre analyze \
  --config .clickspectre.yaml \
  --output "./reports/$(date +%Y-%m-%d)" \
  --format sarif \
  --query-timeout 5m
```

## Troubleshooting

### "Failed to connect to ClickHouse"

- Check DSN format: `clickhouse://user:password@host:port/database`
- Verify network connectivity
- Check ClickHouse is running: `clickhouse-client --query "SELECT 1"`

### "Cannot modify 'max_execution_time' setting in readonly mode"

This occurs when using a ClickHouse user in readonly mode.

**Solution 1 (Quick)**: Use smaller dataset
```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://readonly@host:9000/db" \
  --lookback 7d \
  --batch-size 1000 \
  --max-rows 50000
```

**Solution 2 (Recommended)**: Create a non-readonly user
```sql
CREATE USER clickspectre IDENTIFIED BY 'password';
GRANT SELECT ON *.* TO clickspectre;
```
This user can read all data but can't DROP/DELETE tables.

### "Failed to initialize K8s resolver"

- Check kubeconfig path
- Verify cluster connectivity: `kubectl cluster-info`
- Use `--resolve-k8s=false` to disable K8s resolution

### "Query timeout"

- Increase timeout: `--query-timeout 10m`
- Reduce batch size: `--batch-size 50000`
- Reduce max rows: `--max-rows 500000`

## Is it safe to run?

Yes. ClickSpectre is **strictly read-only**. It does not modify anything in ClickHouse or Kubernetes.

### What it does:
- Runs only `SELECT` queries against ClickHouse (`system.query_log`, metadata tables, etc.)
- Reads Kubernetes metadata when the `--kubernetes` option is used
- Builds a static usage report you can review
- Makes **non-destructive recommendations** about unused tables

### What it NEVER does:
- No `DROP TABLE`
- No `ALTER TABLE`
- No `INSERT`, `UPDATE`, or `DELETE`
- No schema changes of any kind
- No writes to your ClickHouse cluster
- No modifications to Kubernetes resources (no deletes, no patches, no apply)

### Kubernetes Safety
If you enable Kubernetes integration (via `--kubernetes` / `--kubeconfig` / in-cluster mode), ClickSpectre **only performs read-only API calls**:

- Reads Pod → IP mappings
- Reads Service metadata
- Reads Namespace information

It **never**:
- Deletes Pods
- Deletes Services
- Mutates anything in the cluster
- Creates or applies resources

ClickSpectre is safe to run in production:
all actions are observational, never destructive.

### Kubernetes namespace creation

When deploying the report to Kubernetes (via `clickspectre deploy`), the tool will:

- Check whether the target namespace exists
- **Create it only if it does not already exist**

This is the only write operation performed on the Kubernetes side, and it is non-destructive.
No other resources are modified unless you explicitly ask ClickSpectre to create them (Ingress, Service, Deployment, etc.).

---

## Roadmap

**Stage 1 (Current)**: MVP snapshot analyzer
- One-shot analysis mode
- Static report generation
- Interactive D3.js visualization
- Kubernetes integration
- Configurable concurrency

**Stage 2 (Planned)**: Daemon mode
- Continuous monitoring
- Incremental updates
- Alert on anomalies
- LLM integration for recommendations
- Multi-cluster support

## Contributing

Contributions welcome! Please open an issue or PR.

## License

MIT License - See LICENSE file for details

## Acknowledgments

- Built with [ClickHouse Go driver](https://github.com/ClickHouse/clickhouse-go)
- Visualization powered by [D3.js](https://d3js.org/)
- CLI framework: [Cobra](https://github.com/spf13/cobra)
- Kubernetes client: [client-go](https://github.com/kubernetes/client-go)

---

## Keywords

ClickHouse, ClickHouse usage analysis, table usage, query log analysis,
orphaned tables, unused tables, data governance, schema cleanup,
data lifecycle, cost optimization, observability, DevOps, SRE, Kubernetes

**ClickSpectre** - Because tribal knowledge shouldn't be your only ClickHouse documentation.
