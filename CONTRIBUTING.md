# Contributing to ClickSpectre

Keep changes focused, deterministic, and easy to review.

## Prerequisites

- Go 1.25+
- ClickHouse available for integration tests
- `golangci-lint` installed if you plan to run `make lint`

## Build and Verification

Run from the repo root:

```bash
make build
make test
make lint
```

Notes:

- `make test` runs `go test -v -race ./...`
- Some tests exercise ClickHouse paths; use a local or reachable ClickHouse instance for integration coverage

## Pull Requests

- Use conventional commits: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`
- Add or update tests for every code change
- Keep test coverage at least as strong as the touched area; do not merge untested behavior
- Describe the user-visible change, risk, and verification in the PR
- Keep PRs small enough to review in one pass

## Architecture Overview

- `internal/collector`: reads ClickHouse metadata and query-log data, handles retries and batching
- `internal/analyzer`: turns collected data into table usage, service mappings, and relationship edges
- `internal/scorer`: assigns cleanup safety scores and recommendations
- `internal/reporter`: renders JSON, HTML, text, SARIF, and SpectreHub outputs
- `internal/k8s`: resolves client IPs to Kubernetes services with caching and rate limiting
- `pkg/config`: owns CLI/config-file parsing, defaults, exclude rules, and duration handling

## Development Notes

- Prefer root-cause fixes over local patches
- Preserve read-only and safety guarantees around ClickHouse and Kubernetes access
- Do not broaden scope beyond the task you are changing
