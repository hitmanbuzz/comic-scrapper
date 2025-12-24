.PHONY: help fmt lint test build clean install-tools pre-commit all run

# Go binary tools path
GOBIN := $(shell go env GOPATH)/bin
GOIMPORTS := $(GOBIN)/goimports
GOLANGCI_LINT := $(GOBIN)/golangci-lint

# Default target - show help
help:
	@echo "Makefile Commands:"
	@echo ""
	@echo "  make fmt           - Format code with goimports"
	@echo "  make lint          - Run golangci-lint"
	@echo "  make lint-fix      - Run golangci-lint with auto-fix"
	@echo "  make test          - Run tests"
	@echo "  make build         - Build the project"
	@echo "  make run           - Run the application"
	@echo "  make clean         - Remove build artifacts"
	@echo "  make install-tools - Install dev dependencies (goimports, golangci-lint)"
	@echo "  make pre-commit    - Set up pre-commit hooks"
	@echo "  make all           - Format, lint, and test"
	@echo ""

# Format code with goimports
fmt:
	@echo "Formatting code..."
	@$(GOIMPORTS) -w .
	@echo "Code formatted"

# Run linter
lint:
	@echo "Running linters..."
	@$(GOLANGCI_LINT) run
	@echo "Linting complete"

# Run linter with auto-fix
lint-fix:
	@echo "Running linters with auto-fix..."
	@$(GOLANGCI_LINT) run --fix
	@echo "Linting and fixes complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete"

# Build the project
build:
	@echo "Building all main go files..."
	@go build -o ./build/series ./cmd/series/main.go
	@go build -o ./build/filter ./cmd/filter/main.go
	@go build -o ./build/scrape ./cmd/scrape_source_series/main.go
	@go build -o ./build/download ./cmd/download_series/main.go
	@go build -o ./build/metadata ./cmd/metadata_gen/main.go
	@echo "Build complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f comicrawl coverage.out
	@echo "Clean complete"

# Run the application
series:
	@echo "Running series scrape..."
	@go run ./cmd/series/main.go

filter:
	@echo "Running filtering series..."
	@go run ./cmd/filter/main.go

scrape:
	@echo "Running scraping sources series..."
	@go run ./cmd/scrape_source_series/main.go

download:
	@echo "Running downloading series..."
	@go run ./cmd/download_series/main.go

metadata:
	@echo "Running metadata process..."
	@go run ./cmd/metadata_gen/main.go

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed"

# Set up pre-commit hooks
pre-commit:
	@echo "Setting up pre-commit hooks..."
	@cp -f scripts/pre-commit.sh .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed"

# Run all quality checks
all: fmt lint test
	@echo "All checks passed"
