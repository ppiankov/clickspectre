# ClickSpectre

[![CI](https://github.com/ppiankov/clickspectre/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/clickspectre/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/clickspectre)](https://goreportcard.com/report/github.com/ppiankov/clickspectre)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

ClickHouse table usage analyzer and cleanup advisor. Part of [SpectreHub](https://github.com/ppiankov/spectrehub).

Analyzes ClickHouse query logs to determine which tables are used, by whom, and which are safe to clean up. Generates interactive visual reports with D3.js bipartite graphs showing service-to-table relationships.

## Why this exists

ClickHouse is fast, powerful, and absolutely unforgiving when your schema grows faster than your documentation. Teams end up with:

- Tables nobody remembers creating
- Schemas nobody wants to touch
- "Don't drop this or production dies" tribal knowledge
- Dashboards pointing at tables last queried during the Bronze Age
- Zero clarity about which services depend on what

ClickSpectre answers one question: **"Which ClickHouse tables are actually used, and by whom?"**

Born entirely out of real operational pain. Shared so maybe yours hurts less.

## What it is

- Analyzes ClickHouse `system.query_log` to identify table usage patterns
- Maps service-to-table dependencies from query patterns
- Generates safety-scored cleanup recommendations (safe/likely safe/keep)
- Detects tables with zero usage in query logs
- Produces text, JSON, SARIF, and interactive HTML reports
- Optional Kubernetes IP-to-service resolution

## What it is NOT

- Not a ClickHouse monitoring dashboard
- Not a query optimizer or performance tuner
- Not a migration or schema management tool
- Not a replacement for ClickHouse system tables

## Quick start

```bash
# Install
brew install ppiankov/tap/clickspectre

# Analyze ClickHouse usage (30-day lookback)
clickspectre analyze \
  --clickhouse-dsn "clickhouse://user:password@host:9000/default" \
  --output ./report \
  --lookback 30d

# View the report locally
clickspectre serve ./report
# Open http://localhost:8080

# Or deploy to Kubernetes
clickspectre deploy ./report --namespace monitoring --port 8080
```

## CLI commands

| Command | Description |
|---------|-------------|
| `clickspectre analyze` | Analyze query logs and report table usage |
| `clickspectre serve` | Start web UI with interactive dependency graphs |
| `clickspectre deploy` | Deploy report to Kubernetes with port-forwarding |
| `clickspectre version` | Print version |

See [CLI Reference](docs/cli-reference.md) for all flags, configuration, and exit codes.

## Agent integration

Single binary, deterministic output, structured JSON, bounded execution.

Agents: read [`SKILL.md`](SKILL.md) for commands, flags, JSON output, and exit codes.

Key patterns:
- `clickspectre analyze --clickhouse-dsn <dsn> --format json` — table usage audit
- `clickspectre analyze --clickhouse-dsn <dsn> --format sarif` — findings for GitHub Security tab
- Exit code 6 means findings detected (unused tables, cleanup recommendations)

## SpectreHub integration

```sh
spectrehub collect --tool clickspectre
```

## Safety

clickspectre operates in **read-only mode** — never modifies, deletes, or alters your tables. See [Security & Safety](docs/security.md) for the full safety model.

## Documentation

| Document | Contents |
|----------|----------|
| [Architecture](docs/architecture.md) | Module design, report structure, K8s deployment |
| [CLI Reference](docs/cli-reference.md) | All flags, configuration, exit codes |
| [Security & Safety](docs/security.md) | Read-only guarantees, ClickHouse/K8s protection |
| [Known Limitations](docs/known-limitations.md) | Constraints, readonly users, troubleshooting |
| [ClickHouse Real Client IP](docs/CLICKHOUSE-REAL-CLIENT-IP.md) | PROXY protocol setup for load balancers |
| [Kubernetes Resolution](docs/KUBERNETES-RESOLUTION.md) | IP-to-service name resolution |

## License

MIT — see [LICENSE](LICENSE).

---

Built by [Obsta Labs](https://obstalabs.dev)
