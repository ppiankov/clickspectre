# Contributing to ClickSpectre

We welcome contributions to ClickSpectre! To ensure a smooth collaboration, please follow these guidelines.

## Prerequisites

Before you start, make sure you have the following installed:

*   **Go:** Version 1.25 or later.
*   **ClickHouse:** A running ClickHouse instance is required for integration tests. You can use Docker for easy setup: `docker run -p 8123:8123 -p 9000:9000 --name clickhouse-server -d clickhouse/clickhouse-server`

## Build, Test, and Lint Commands

*   **Build:**
    ```bash
    go build ./cmd/clickspectre
    ```
*   **Test:** Run all unit and integration tests.
    ```bash
    go test -race ./...
    ```
*   **Lint:** Ensure your code adheres to our style guidelines.
    ```bash
    go fmt ./...
    go vet ./...
    ```
    (Note: Additional linters might be configured in CI; ensure your IDE is set up to use `golangci-lint` or similar.)

## Pull Request Conventions

*   **Conventional Commits:** We follow the [Conventional Commits specification](https://www.conventionalcommits.org/en/v1.0.0/) for commit messages. This helps us generate accurate changelogs.
    *   Examples: `feat: add new feature X`, `fix: correct bug Y`, `docs: update README`
*   **Test Coverage:** All new features and bug fixes should be accompanied by appropriate unit and/or integration tests. Aim for high test coverage for your changes.
*   **Descriptive PRs:** Provide a clear and concise description of your changes in the pull request, including the problem it solves and how it was addressed.
*   **Sign Your Commits:** All commits must be signed off (using `git commit -s`).

## Architecture Overview

ClickSpectre is a Go CLI tool designed to analyze ClickHouse query logs and identify potential table cleanup opportunities.

Key components:

*   **`cmd/clickspectre`**: Contains the main CLI application entry points and command definitions.
*   **`internal/collector`**: Responsible for connecting to ClickHouse, querying `system.query_log`, and collecting relevant data.
*   **`internal/analyzer`**: Processes the collected query logs, maps services to tables (often via Kubernetes IP resolution), and identifies usage patterns.
*   **`internal/scorer`**: Implements the logic for scoring table cleanup safety based on usage patterns and other factors.
*   **`internal/reporter`**: Generates various output formats (JSON, HTML, Text, SARIF) for the analysis results.
*   **`internal/k8s`**: Handles Kubernetes-specific logic, suchs as resolving IP addresses to service names.
*   **`pkg/config`**: Manages application configuration, including CLI flags and config file parsing.