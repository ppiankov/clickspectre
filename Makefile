.PHONY: build test clean install lint run help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "1.0.0-stage1")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
BINARY := bin/clickspectre

help: ## Show this help message
	@echo "ClickSpectre Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building ClickSpectre..."
	@go build $(LDFLAGS) -o $(BINARY) ./cmd/clickspectre
	@echo "Binary built: $(BINARY)"

test: ## Run tests with race detector
	@echo "Running tests..."
	@go test -v -race ./...

clean: ## Remove binaries and reports
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf report-*/
	@echo "Clean complete"

install: ## Install binary to $GOPATH/bin
	@echo "Installing ClickSpectre..."
	@go install $(LDFLAGS) ./cmd/clickspectre
	@echo "Installed to $(shell go env GOPATH)/bin/clickspectre"

lint: ## Run golangci-lint
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install from: https://golangci-lint.run/usage/install/"; \
	fi

run: build ## Build and run with example flags
	@echo "Running ClickSpectre (dry-run mode)..."
	@$(BINARY) analyze \
		--clickhouse-dsn "clickhouse://localhost:9000/default" \
		--output ./report \
		--lookback 7d \
		--dry-run \
		--verbose

dev: ## Run without building (for development)
	@go run ./cmd/clickspectre $(ARGS)

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...
	@go mod tidy

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

all: clean deps fmt vet test build ## Run all checks and build
