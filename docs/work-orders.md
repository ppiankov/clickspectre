# Work Orders — clickspectre

## Phase 1: Core (v1.0.0) ✅

All Phase 1 work shipped. Current state:

- ✅ ClickHouse query log analysis (system.query_log)
- ✅ Service-to-table mapping via Kubernetes IP resolution
- ✅ Interactive D3.js bipartite graph reports
- ✅ Worker pool for paginated ClickHouse queries
- ✅ Table cleanup safety scoring
- ✅ JSON + HTML reports
- ✅ CI + release workflows (multi-platform binaries)
- ✅ CHANGELOG.md

---

## Phase 2: Hardening

---

## WO-C01: Test coverage push

**Goal:** Coverage is 0% — no test files exist in any package. This is a critical gap for a v1.0.0 tool.

### Packages to test
1. `internal/scorer/` — cleanup safety scoring logic (most testable, pure functions)
2. `internal/analyzer/` — query log processing, service-table mapping
3. `internal/collector/` — pagination logic, query construction (mock ClickHouse)
4. `internal/reporter/` — JSON output structure, HTML template rendering
5. `internal/models/` — data structure validation
6. `internal/k8s/` — IP-to-service resolution (mock k8s client)
7. `pkg/config/` — flag parsing, defaults

### Steps
1. Start with `internal/scorer/scorer_test.go` — pure scoring functions, no dependencies
2. `internal/analyzer/analyzer_test.go` — test with fixture data
3. `internal/reporter/json_test.go` — test JSON output matches expected structure
4. `internal/collector/collector_test.go` — test query construction, pagination (mock responses)
5. `internal/k8s/resolver_test.go` — test IP resolution with mock client
6. `pkg/config/config_test.go` — test flag defaults and validation

### Acceptance
- Coverage > 60% overall
- `make test` passes with -race
- No flaky tests

---

## WO-C02: Structured logging (slog)

**Goal:** Replace ad-hoc print statements with `log/slog`.

### Steps
1. Create `internal/logging/logging.go` — `Init(verbose bool)`
2. Replace fmt.Println/Printf with slog.Debug/Info/Warn
3. `--verbose` maps to LevelDebug, default LevelWarn
4. Structured fields: table count, service count, query count, duration

### Acceptance
- Silent by default
- `make test` passes with -race

---

## WO-C03: Connection resilience

**Goal:** Transient ClickHouse failures retry, auth failures fail fast.

### Steps
1. Create `internal/collector/retry.go` — exponential backoff (max 3 attempts)
2. Classify errors: auth → fail fast, network/timeout → retry
3. `--timeout` caps total query time

### Acceptance
- Network errors retry with backoff
- Auth errors fail immediately
- `make test` passes with -race

---

## WO-C04: Text report format

**Goal:** Add `--format text` alongside existing JSON and HTML.

### Steps
1. Create `internal/reporter/text.go` — plain text table output
2. Group findings by table, show service mappings, safety score
3. Summary section: total tables, unused count, score distribution

### Acceptance
- `clickspectre audit --format text` produces readable output
- Works in pipes (no ANSI when not TTY)
- `make test` passes with -race

---

## WO-C05: SARIF output

**Goal:** `--format sarif` for GitHub Security tab.

### Steps
1. Create `internal/reporter/sarif.go` — SARIF 2.1.0 writer
2. Rule IDs: `clickspectre/ZERO_USAGE`, `clickspectre/LOW_USAGE`, `clickspectre/ANOMALY`
3. Severity mapping

### Acceptance
- Valid SARIF 2.1.0 output
- `make test` passes with -race

---

## WO-C06: Config file (.clickspectre.yaml)

**Goal:** Persistent defaults for ClickHouse connection, thresholds, exclude patterns.

### Steps
1. Create `internal/config/config.go` — YAML loader
2. Fields: clickhouse_url, exclude_tables, exclude_databases, min_query_count, format, timeout
3. CLI flags override config

### Acceptance
- Config file auto-loaded
- `make test` passes with -race

---

## WO-C07: Baseline mode

**Goal:** Suppress known findings on repeat runs.

### Steps
1. Create `internal/baseline/baseline.go` — SHA-256 fingerprints
2. `--baseline` and `--update-baseline` flags

### Acceptance
- Second run shows only new findings
- `make test` passes with -race

---

## Phase 3: Distribution & Adoption

---

## WO-C08: GoReleaser config

**Goal:** Formalize release automation with GoReleaser.

### Steps
1. Create `.goreleaser.yml` — replace manual release.yml builds
2. Multi-platform: linux/darwin/windows, amd64/arm64
3. Checksums, changelog from conventional commits

### Acceptance
- `goreleaser release --snapshot` produces artifacts
- Release workflow uses GoReleaser

---

## WO-C09: Docker image

**Goal:** `docker run clickspectre audit --clickhouse-url ...` — zero install.

### Steps
1. Create `Dockerfile` — multi-stage: Go builder → distroless
2. Multi-arch manifest (amd64, arm64)
3. Push to `ghcr.io/ppiankov/clickspectre`

### Acceptance
- Image < 20MB
- Multi-arch
- `docker run ghcr.io/ppiankov/clickspectre version` works

---

## WO-C10: Homebrew formula

**Goal:** `brew install ppiankov/tap/clickspectre`

### Steps
1. GoReleaser `brews` section → ppiankov/homebrew-tap
2. Formula with test block
3. Auto-updates on release

### Acceptance
- `brew install ppiankov/tap/clickspectre` works

---

## WO-C11: GitHub Action

**Goal:** `uses: ppiankov/clickspectre-action@v1`

### Steps
1. Composite action repo
2. Inputs: `clickhouse-url`, `format`, `fail-on`, `args`
3. Download binary, run, upload SARIF

### Acceptance
- Action works in workflow

---

## WO-C12: First-run experience

**Goal:** Helpful messages for new users.

### Steps
1. Summary header: ClickHouse host, database count, table count
2. No findings: "No issues detected. N tables scanned."
3. Connection banner on verbose
4. Exit code hints

### Acceptance
- First run shows helpful summary
- `make test` passes with -race

---

## WO-C13: Standardized output header

**Goal:** Emit `{"tool": "clickspectre", "version": "...", "timestamp": "..."}` per spectrehub contract.

### Steps
1. Add header fields to JSON report struct
2. Backward compatible with v1.0.0 output

### Acceptance
- JSON includes tool, version, timestamp
- spectrehub parses without errors

---

## WO-C14: CONTRIBUTING.md

**Goal:** Community contribution guidelines.

### Steps
1. Prerequisites (Go 1.25+, ClickHouse for integration tests)
2. Build/test/lint commands
3. PR conventions (conventional commits, test coverage)
4. Architecture overview

### Acceptance
- CONTRIBUTING.md in repo root

---

## WO-C15: SpectreHub `spectre/v1` envelope output

**Goal:** Add `--format spectrehub` flag that outputs the canonical `spectre/v1` JSON envelope for SpectreHub aggregation.

### Details
clickspectre uses an analytical model (tables/services/edges/cleanup_recommendations) rather than findings. Needs: transform cleanup_recommendations and zero-usage tables into standard `findings[]` array with severity classification, and wrap in spectre/v1 envelope.

### Severity Mapping
- Zero-usage non-replicated tables → high (safe to drop)
- Zero-usage replicated tables → medium (needs coordination)
- Low-usage tables (< threshold) → low
- Orphaned services → medium

### Envelope Schema
```json
{
  "schema": "spectre/v1",
  "tool": "clickspectre",
  "version": "1.0.1",
  "timestamp": "2026-02-22T12:00:00Z",
  "target": { "type": "clickhouse", "uri_hash": "sha256:..." },
  "findings": [{ "id": "ZERO_USAGE_TABLE", "severity": "high", "location": "default.old_events", "message": "..." }],
  "summary": { "total": 5, "high": 1, "medium": 2, "low": 1, "info": 1 }
}
```

### Steps
1. Create `internal/reporter/spectrehub.go` with spectre/v1 envelope types and writer
2. Transform tables/recommendations into findings with severity
3. Add `FormatSpectreHub` constant and case in format dispatcher
4. Update `--format` flag help text
5. Create `internal/reporter/spectrehub_test.go`
6. Update SKILL.md and README

### Acceptance
- `clickspectre audit --dsn ... --format spectrehub` outputs valid spectre/v1 JSON
- Cleanup recommendations mapped to findings with severity
- DSN credentials NOT included in target (use uri_hash)
- Tests pass with -race

---

## Non-Goals

- No ClickHouse table creation or deletion
- No automatic cleanup execution
- No web UI beyond static HTML report
- No persistent state
- No real-time monitoring (single-point-in-time audit)
- No MergeTree optimization recommendations
