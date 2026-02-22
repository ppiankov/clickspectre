# Changelog

All notable changes to ClickSpectre will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[1.0.1]: https://github.com/ppiankov/clickspectre/releases/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ppiankov/clickspectre/releases/tag/v1.0.0
