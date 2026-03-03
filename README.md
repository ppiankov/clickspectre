# clickspectre

[![CI](https://github.com/ppiankov/clickspectre/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/clickspectre/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/clickspectre)](https://goreportcard.com/report/github.com/ppiankov/clickspectre)

**clickspectre** — ClickHouse table usage analyzer and cleanup advisor. Part of [SpectreHub](https://github.com/ppiankov/spectrehub).

## What it is

- Analyzes ClickHouse query logs to determine which tables are used and by whom
- Maps service-to-table dependencies from query patterns
- Generates cleanup recommendations for unused tables
- Produces interactive visual reports with bipartite dependency graphs
- Outputs text, JSON, SARIF, and SpectreHub formats

## What it is NOT

- Not a ClickHouse monitoring dashboard
- Not a query optimizer or performance tuner
- Not a migration or schema management tool
- Not a replacement for ClickHouse system tables

## Quick start

### Homebrew

```sh
brew tap ppiankov/tap
brew install clickspectre
```

### From source

```sh
git clone https://github.com/ppiankov/clickspectre.git
cd clickspectre
make build
```

### Usage

```sh
clickspectre audit --dsn "clickhouse://localhost:9000"
```

## CLI commands

| Command | Description |
|---------|-------------|
| `clickspectre audit` | Analyze query logs and report table usage |
| `clickspectre serve` | Start web UI with interactive dependency graphs |
| `clickspectre version` | Print version |

## SpectreHub integration

clickspectre feeds ClickHouse table usage findings into [SpectreHub](https://github.com/ppiankov/spectrehub) for unified visibility across your infrastructure.

```sh
spectrehub collect --tool clickspectre
```

## Safety

clickspectre operates in **read-only mode**. It inspects and reports — never modifies, deletes, or alters your tables.

## License

MIT — see [LICENSE](LICENSE).

---

Built by [Obsta Labs](https://github.com/ppiankov)
