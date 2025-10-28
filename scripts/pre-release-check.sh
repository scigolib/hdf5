#!/usr/bin/env bash
# Pre-Release Validation Script for HDF5 Go Library
# This script runs all quality checks before creating a release
# EXACTLY matches CI checks + additional validations

set -e  # Exit on first error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Header
echo ""
echo "========================================"
echo "  HDF5 Go Library - Pre-Release Check"
echo "========================================"
echo ""

# Track overall status
ERRORS=0
WARNINGS=0

# 1. Check Go version
log_info "Checking Go version..."
GO_VERSION=$(go version | awk '{print $3}')
REQUIRED_VERSION="go1.25"
if [[ "$GO_VERSION" < "$REQUIRED_VERSION" ]]; then
    log_error "Go version $REQUIRED_VERSION+ required, found $GO_VERSION"
    ERRORS=$((ERRORS + 1))
else
    log_success "Go version: $GO_VERSION"
fi
echo ""

# 2. Check git status
log_info "Checking git status..."
if git diff-index --quiet HEAD --; then
    log_success "Working directory is clean"
else
    log_warning "Uncommitted changes detected"
    git status --short
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 3. Code formatting check (EXACT CI command)
log_info "Checking code formatting (gofmt -l .)..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    log_error "The following files need formatting:"
    echo "$UNFORMATTED"
    echo ""
    log_info "Run: go fmt ./..."
    ERRORS=$((ERRORS + 1))
else
    log_success "All files are properly formatted"
fi
echo ""

# 4. Go vet
log_info "Running go vet..."
if go vet ./... 2>&1; then
    log_success "go vet passed"
else
    log_error "go vet failed"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 5. Build all packages
log_info "Building all packages..."
if go build ./... 2>&1; then
    log_success "Build successful"
else
    log_error "Build failed"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 6. go.mod validation
log_info "Validating go.mod..."
go mod verify
if [ $? -eq 0 ]; then
    log_success "go.mod verified"
else
    log_error "go.mod verification failed"
    ERRORS=$((ERRORS + 1))
fi

# Check if go.mod needs tidying
go mod tidy
if git diff --quiet go.mod go.sum; then
    log_success "go.mod is tidy"
else
    log_warning "go.mod needs tidying (run 'go mod tidy')"
    git diff go.mod go.sum
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 7. Run tests (with race detector if GCC available)
if command -v gcc &> /dev/null; then
    log_info "Running tests with race detector..."
    RACE_FLAG="-race"
else
    log_warning "GCC not found, running tests without race detector"
    log_info "Install GCC (mingw-w64) for race detection on Windows"
    WARNINGS=$((WARNINGS + 1))
    log_info "Running tests..."
    RACE_FLAG=""
fi

if go test $RACE_FLAG ./... 2>&1; then
    if [ -n "$RACE_FLAG" ]; then
        log_success "All tests passed with race detector"
    else
        log_success "All tests passed"
    fi
else
    log_error "Tests failed"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 8. Test coverage check
log_info "Checking test coverage..."
COVERAGE=$(go test -cover ./internal/... 2>&1 | grep "coverage:" | tail -1 | awk '{print $5}' | sed 's/%//')
if [ -n "$COVERAGE" ]; then
    echo "  • internal/ coverage: ${COVERAGE}%"
    if awk -v cov="$COVERAGE" 'BEGIN {exit !(cov >= 70.0)}'; then
        log_success "Coverage meets requirement (>70%)"
    else
        log_error "Coverage below 70% (${COVERAGE}%)"
        ERRORS=$((ERRORS + 1))
    fi
else
    log_warning "Could not determine coverage"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 9. Reference tests check
log_info "Checking reference tests..."
REFERENCE_FILES=$(find testdata/reference -name "*.h5" 2>/dev/null | wc -l)
if [ "$REFERENCE_FILES" -ge 57 ]; then
    log_success "Found $REFERENCE_FILES reference test files"
else
    log_warning "Expected 57 reference files, found $REFERENCE_FILES"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 10. golangci-lint (same as CI)
log_info "Running golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    if golangci-lint run --timeout=5m ./... 2>&1 | tail -5 | grep -q "0 issues"; then
        log_success "golangci-lint passed with 0 issues"
    else
        log_error "Linter found issues"
        golangci-lint run --timeout=5m ./... 2>&1 | tail -10
        ERRORS=$((ERRORS + 1))
    fi
else
    log_error "golangci-lint not installed"
    log_info "Install: https://golangci-lint.run/welcome/install/"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 11. Check for TODO/FIXME comments (HDF5 requirement: 0 TODOs)
log_info "Checking for TODO/FIXME comments..."
TODO_COUNT=$(grep -r "TODO\|FIXME" --include="*.go" --exclude-dir=vendor . 2>/dev/null | wc -l)
if [ "$TODO_COUNT" -gt 0 ]; then
    log_error "Found $TODO_COUNT TODO/FIXME comments (requirement: 0)"
    grep -r "TODO\|FIXME" --include="*.go" --exclude-dir=vendor . 2>/dev/null | head -5
    ERRORS=$((ERRORS + 1))
else
    log_success "No TODO/FIXME comments found"
fi
echo ""

# 12. Check critical documentation files
log_info "Checking documentation..."
DOCS_MISSING=0
REQUIRED_DOCS="README.md CHANGELOG.md ROADMAP.md RELEASE_GUIDE.md"
REQUIRED_GUIDES="docs/guides/INSTALLATION.md docs/guides/READING_DATA.md docs/guides/DATATYPES.md docs/guides/TROUBLESHOOTING.md docs/guides/FAQ.md"

for doc in $REQUIRED_DOCS $REQUIRED_GUIDES; do
    if [ ! -f "$doc" ]; then
        log_error "Missing: $doc"
        DOCS_MISSING=1
        ERRORS=$((ERRORS + 1))
    fi
done

if [ $DOCS_MISSING -eq 0 ]; then
    log_success "All critical documentation files present"
fi
echo ""

# 13. Check sprint completion (v0.10.0-beta specific)
log_info "Checking sprint tasks completion..."
if grep -q "100% - 6/6 tasks" ROADMAP.md; then
    log_success "Sprint v0.10.0-beta: 100% complete (6/6 tasks)"
else
    log_warning "Sprint tasks may not be complete"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Summary
echo "========================================"
echo "  Summary"
echo "========================================"
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    log_success "All checks passed! Ready for release."
    echo ""
    log_info "Next steps (from RELEASE_GUIDE.md):"
    echo "  1. Create release branch: git checkout -b release/v0.10.0-beta"
    echo "  2. Update CHANGELOG.md with version details"
    echo "  3. Commit: git commit -m 'chore: prepare v0.10.0-beta release'"
    echo "  4. Push: git push origin release/v0.10.0-beta"
    echo "  5. Wait for CI (5-10 min) ⏰"
    echo "  6. Merge to main (only after green CI)"
    echo "  7. Wait for CI on main ⏰"
    echo "  8. Create tag (only after green CI): git tag -a v0.10.0-beta"
    echo "  9. Push tag: git push origin v0.10.0-beta"
    echo " 10. Create GitHub release"
    echo ""
    exit 0
elif [ $ERRORS -eq 0 ]; then
    log_warning "Checks completed with $WARNINGS warning(s)"
    echo ""
    log_info "Review warnings above before proceeding with release"
    echo ""
    exit 0
else
    log_error "Checks failed with $ERRORS error(s) and $WARNINGS warning(s)"
    echo ""
    log_error "Fix errors before creating release"
    echo ""
    exit 1
fi
