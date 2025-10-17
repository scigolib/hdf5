# HDF5 Go Library Makefile
# Pure Go HDF5 file format reader

PROJECT = hdf5
VERSION ?= v0.9.0-beta

# Default target
.DEFAULT_GOAL := test

# Build library (check compilation)
build:
	@echo "Building $(PROJECT) library..."
	GO111MODULE=on go build ./...

# Run all tests
test:
	@echo "Running tests..."
	go test -v -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	go test -v -race -coverprofile=coverage.out ./...

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run --timeout=5m

# Run linter and save results
lint-report:
	@echo "Running linter with report..."
	golangci-lint run --timeout=5m > lint_results.log 2>&1 || true
	@echo "Linter results saved to lint_results.log"
	@cat lint_results.log

# Format code
fmt:
	@echo "Formatting code..."
	gofmt -w -s .
	go mod tidy

# Check code formatting (CI-friendly, no changes)
fmt-check:
	@echo "Checking code formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "ERROR: The following files are not formatted:"; \
		gofmt -l .; \
		echo ""; \
		echo "Run 'make fmt' to fix formatting issues."; \
		exit 1; \
	fi
	@echo "All files are properly formatted ✓"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html
	rm -f lint_results.log
	rm -f *.exe

# Build examples
examples:
	@echo "Building examples..."
	@for example in examples/*.go; do \
		if [ -f "$$example" ]; then \
			echo "Building $$example..."; \
			go build -o /dev/null "$$example" || exit 1; \
		fi; \
	done
	@echo "All examples built successfully!"

# Run example
run-example:
	@echo "Running basic example..."
	go run examples/example.go

# Run comprehensive demo
run-demo:
	@echo "Running comprehensive demo..."
	go run examples/comprehensive_demo.go

# Development workflow
dev: fmt lint test
	@echo "Development checks complete!"

# CI/CD checks (includes formatting check)
ci: fmt-check test lint
	@echo "CI checks passed!"

# Pre-commit checks
pre-commit: fmt lint test
	@echo "Pre-commit checks complete!"

# Install golangci-lint (if not installed)
install-lint:
	@echo "Installing golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
	else \
		echo "golangci-lint is already installed"; \
	fi

# Help
help:
	@echo "HDF5 Go Library Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build         - Build library (check compilation)"
	@echo "  make test          - Run tests"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make test-race     - Run tests with race detector"
	@echo "  make benchmark     - Run benchmarks"
	@echo "  make lint          - Run linter"
	@echo "  make lint-report   - Run linter and save to file"
	@echo "  make fmt           - Format code"
	@echo "  make fmt-check     - Check code formatting (CI)"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make examples      - Build all examples"
	@echo "  make run-example   - Run basic example"
	@echo "  make run-demo      - Run comprehensive demo"
	@echo "  make dev           - Full development workflow"
	@echo "  make ci            - CI/CD checks"
	@echo "  make pre-commit    - Pre-commit checks"
	@echo "  make install-lint  - Install golangci-lint"
	@echo ""
	@echo "Version: $(VERSION)"

.PHONY: build test test-coverage test-race benchmark lint lint-report fmt fmt-check clean \
	examples run-example run-demo dev ci pre-commit install-lint help
