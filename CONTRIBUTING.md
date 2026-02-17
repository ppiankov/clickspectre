# Contributing to ClickSpectre

Thanks for helping improve ClickSpectre. Keep changes focused, testable, and documented.

## Prerequisites

- Go `1.25+` (see `go.mod` for the exact toolchain version).
- A ClickHouse instance for integration testing and end-to-end validation.
- `golangci-lint` installed locally to run `make lint`.

## Build, Test, and Lint

Run these from the repo root:

- Build binary: `make build`
- Run tests (with race detector): `make test`
- Run linter: `make lint`
- Generate coverage profile: `go test ./... -coverprofile=coverage.out`

Recommended before opening a PR:

- Format code: `make fmt`
- Static checks: `make vet`
- Full local quality pass: `make fmt && make vet && make test && make lint`

## Pull Request Conventions

- Use Conventional Commits, for example:
  - `feat: add baseline update flag`
  - `fix: handle nil reporter output`
  - `docs: clarify clickhouse dsn examples`
- Add or update tests for any behavior change.
- Maintain or improve coverage for touched code paths.
- Keep PRs scoped to one change area when possible.
- In the PR description, include:
  - what changed
  - why it changed
  - how you validated it (commands and/or manual checks)

## Architecture Overview

ClickSpectre pipeline:

`ClickHouse -> collector -> analyzer -> scorer -> reporter -> outputs`

Primary modules:

- `cmd/clickspectre`: CLI entrypoints and command wiring.
- `internal/collector`: ClickHouse query execution, pagination, and data collection.
- `internal/analyzer`: usage analysis and service-to-table mapping.
- `internal/scorer`: cleanup-safety scoring logic.
- `internal/reporter`: JSON and SARIF report generation plus static web assets.
- `internal/k8s`: Kubernetes IP-to-workload resolution helpers.
- `internal/baseline`: finding fingerprint generation and baseline filtering.
- `internal/models`: shared domain models.
- `pkg/config`: config/flag parsing and validation.
- `web`: static report assets.
