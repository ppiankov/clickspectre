# Changelog

All notable changes to ClickSpectre will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-03-26

### Added
- `query` command — ad-hoc query_log power tool with flexible filtering
- `top` command — live running queries view for incident response
- `slow` command — slow query digest and performance analysis
- `who` command — which services use a given table, with `--stdin` support
- `ls` command — list ClickHouse databases and tables
- `grants` command — user permissions audit
- `explain` command — structured table intelligence for agents
- `snapshot` command — save cluster state for offline analysis
- `diff` command — compare two analysis reports
- `watch` command — continuous table drift detection
- `doctor` command — connectivity and config diagnostics
- `init` command — guided config setup for first-run experience
- `ci-init` command — generate CI pipeline snippets
- MCP server mode for agent integration (8 tools exposed via `mcp` command)
- Multi-node ClickHouse support with query_id deduplication
- Incremental query_log collection with watermark file
- Per-user query activity analysis (`--by-user` flag)
- Policy-as-code enforcement for table hygiene (`--policy` flag)
- Built-in secret redaction for query text
- `--stdin` support for Unix composition on `who` and `query` commands
- `--quiet` flag for machine consumption
- `--output -` for stdout JSON piping
- `--format json` on `version` command

### Changed
- Serve/deploy logging migrated to slog
- README updated with all 18 commands and CLI reference

### Fixed
- `spectre/v1` schema compliance — removed invalid `additionalProperties`
- URL-aware DSN masking preserving hostname
- DSN format validation in PreRunE
- Version injection via ldflags (replaced hardcoded value)

## [1.0.2] - 2026-02-23

### Added
- SpectreHub `spectre/v1` envelope output format (`--format spectrehub`)
- `HashDSN()` function strips credentials before hashing ClickHouse DSN

## [1.0.1] - 2026-02-22

### Added
- Structured exit codes (0=success, 1=internal, 2=invalid args, 3=not found, 5=network, 6=findings)
- SKILL.md for agent integration
- Agent integration section in README
- Trivy security scanning in CI
- Baseline mode for suppressing known findings
- SARIF output format
- Config file support (.clickspectre.yaml)
- CONTRIBUTING.md guidelines

### Changed
- Simplified verbose logging to use slog level directly
- Release workflow separated into test and release jobs
- Archive naming aligned with spectre family convention
- GoReleaser Homebrew tap integration with auto-push

### Fixed
- Homebrew tap token name aligned with spectre family (HOMEBREW_TAP_TOKEN)

## [1.0.0] - 2026-02-02

### Added
- Initial stable release of ClickSpectre
- ClickHouse query log analysis for table usage patterns
- Service-to-table relationship mapping
- Safety-scored cleanup recommendations (safe/likely safe/keep)
- Zero-usage table detection with configurable size filtering
- Interactive D3.js visual reports with bipartite graphs
- Optional Kubernetes IP-to-service resolution
- Kubernetes deployment command for single-command report deployment
- Read-only safety guarantees for ClickHouse and Kubernetes
- Concurrent processing with configurable worker pools
- Support for readonly ClickHouse users (with limitations)
- Materialized view dependency tracking
- Anomaly detection for unusual access patterns
- CI/CD automation with GitHub Actions
- Automated multi-platform releases (Linux, macOS, Windows)
- Automated testing and linting on every push/PR

### Documentation
- Aligned README with Spectre family philosophy
- Problem-first documentation approach
- Comprehensive usage examples and troubleshooting
- ClickHouse real client IP configuration guide
- Kubernetes resolution setup documentation

### Infrastructure
- GitHub Actions CI workflow (test, lint, build)
- GitHub Actions release workflow (multi-platform binaries)
- Automated binary builds for Linux (amd64, arm64), macOS (Intel, Apple Silicon), Windows
- SHA256 checksum generation for all releases
- Auto-generated release notes

[1.1.0]: https://github.com/ppiankov/clickspectre/releases/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/ppiankov/clickspectre/releases/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/ppiankov/clickspectre/releases/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ppiankov/clickspectre/releases/tag/v1.0.0
