# HDF5 Go Library Makefile
# Pure Go HDF5 file format reader and writer.
#
# The checks CI runs are in .github/workflows/test.yml; `make ci` runs the
# same gates locally (formatting, tests, lint). Outputs go to tmp/, which
# is ignored by git.

PROJECT = hdf5

# Default target
.DEFAULT_GOAL := test

# Build library, examples and tools (check compilation)
build:
	@echo "Building $(PROJECT)..."
	go build ./...

# Run all tests
test:
	@echo "Running tests..."
	@mkdir -p tmp
	go test -coverprofile=tmp/coverage.out ./...

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "Coverage report: tmp/coverage.html"

# Run tests with race detector (needs cgo: Linux, macOS, or WSL on Windows)
test-race:
	@echo "Running tests with race detector..."
	go test -race ./...

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	go test -run '^$$' -bench=. -benchmem ./...

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run --timeout=5m ./...

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
	@echo "All files are formatted."

# Remove generated outputs
clean:
	@echo "Cleaning..."
	rm -f tmp/coverage.out tmp/coverage.html

# Build every example program
examples:
	@echo "Building examples..."
	go build ./examples/...

# Development workflow
dev: fmt lint test
	@echo "Development checks complete."

# The gates CI runs (formatting, tests, lint)
ci: fmt-check test lint
	@echo "CI checks passed."

# Pre-commit checks
pre-commit: fmt lint test
	@echo "Pre-commit checks complete."

# Install golangci-lint v2 (CI uses the latest release)
install-lint:
	@echo "Installing golangci-lint..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# Help
help:
	@echo "HDF5 Go Library Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build         - Build library, examples and tools"
	@echo "  make test          - Run tests (coverage profile in tmp/)"
	@echo "  make test-coverage - Run tests and write tmp/coverage.html"
	@echo "  make test-race     - Run tests with the race detector (needs cgo)"
	@echo "  make benchmark     - Run benchmarks"
	@echo "  make lint          - Run golangci-lint"
	@echo "  make fmt           - Format code and tidy go.mod"
	@echo "  make fmt-check     - Check code formatting"
	@echo "  make clean         - Remove generated outputs"
	@echo "  make examples      - Build all examples"
	@echo "  make dev           - fmt, lint, test"
	@echo "  make ci            - The gates CI runs"
	@echo "  make pre-commit    - fmt, lint, test"
	@echo "  make install-lint  - Install golangci-lint"

.PHONY: build test test-coverage test-race benchmark lint fmt fmt-check clean \
	examples dev ci pre-commit install-lint help
