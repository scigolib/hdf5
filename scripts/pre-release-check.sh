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
echo "  HDF5 Go - Pre-Release Validation"
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
    log_error "Go version $REQUIRED_VERSION or higher required (found $GO_VERSION)"
    ERRORS=$((ERRORS + 1))
else
    log_success "Go version: $GO_VERSION"
fi
echo ""

# 2. Check git status (early check)
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
UNFORMATTED=$(gofmt -l . | grep -v "^testdata/" || true)
if [ -n "$UNFORMATTED" ]; then
    log_error "The following files need formatting:"
    echo "$UNFORMATTED"
    echo ""
    log_info "Run: make fmt"
    ERRORS=$((ERRORS + 1))
else
    log_success "All files are properly formatted"
fi
echo ""

# 4. Go vet
log_info "Running go vet..."
if go vet ./... 2>&1 | grep -E "^#|FAIL" > /dev/null; then
    log_error "go vet failed"
    go vet ./... 2>&1 | head -10
    ERRORS=$((ERRORS + 1))
else
    log_success "go vet passed"
fi
echo ""

# 5. Build all packages
log_info "Building all packages..."
if go build ./... 2>&1 | grep -E "FAIL|ERROR"; then
    log_error "Build failed"
    ERRORS=$((ERRORS + 1))
else
    log_success "Build successful"
fi
echo ""

# 6. Build cmd utilities
log_info "Building command-line utilities..."
if go build -o /tmp/dump_hdf5 ./cmd/dump_hdf5 2>&1; then
    log_success "dump_hdf5 built successfully"
else
    log_error "Failed to build dump_hdf5"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 7. go.mod validation
log_info "Validating go.mod..."
go mod tidy
if git diff --exit-code go.mod go.sum > /dev/null 2>&1; then
    log_success "go.mod and go.sum are up to date"
else
    log_warning "go.mod or go.sum has changes after 'go mod tidy'"
    log_info "Run: go mod tidy"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 8. Run tests with race detector
log_info "Running tests with race detector..."
if go test -race -v ./... 2>&1 | tee /tmp/test-output.txt | grep -q "FAIL"; then
    log_error "Tests failed"
    grep "FAIL" /tmp/test-output.txt | head -5
    ERRORS=$((ERRORS + 1))
else
    log_success "All tests passed with race detector"
fi
echo ""

# 9. Test coverage check
log_info "Checking test coverage..."
COVERAGE=$(go test -cover ./... 2>&1 | grep "coverage:" | awk '{sum+=$5; count++} END {if (count > 0) print sum/count; else print 0}' | sed 's/%//')
if [ -n "$COVERAGE" ]; then
    echo "  Overall coverage: ${COVERAGE}%"
    # Check if coverage is above 70%
    if awk -v cov="$COVERAGE" 'BEGIN {exit !(cov >= 70.0)}'; then
        log_success "Coverage meets requirements (>70%)"
    else
        log_warning "Coverage below 70% (${COVERAGE}%)"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    log_warning "Could not determine test coverage"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 10. golangci-lint (same as CI)
log_info "Running golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    LINT_OUTPUT=$(golangci-lint run --timeout=5m ./... 2>&1)
    if echo "$LINT_OUTPUT" | grep -q "0 issues"; then
        log_success "golangci-lint passed with no issues"
    else
        log_error "golangci-lint found issues"
        echo "$LINT_OUTPUT" | tail -20
        ERRORS=$((ERRORS + 1))
    fi
else
    log_error "golangci-lint not installed"
    log_info "Install: https://golangci-lint.run/welcome/install/"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 11. Check for TODO/FIXME comments
log_info "Checking for TODO/FIXME comments..."
TODO_COUNT=$(grep -r "TODO\|FIXME" --include="*.go" --exclude-dir=vendor --exclude-dir=testdata . 2>/dev/null | wc -l)
if [ "$TODO_COUNT" -gt 0 ]; then
    log_warning "Found $TODO_COUNT TODO/FIXME comments"
    grep -r "TODO\|FIXME" --include="*.go" --exclude-dir=vendor --exclude-dir=testdata . 2>/dev/null | head -5
    WARNINGS=$((WARNINGS + 1))
else
    log_success "No TODO/FIXME comments found"
fi
echo ""

# 12. Check critical documentation files
log_info "Checking documentation..."
DOCS_MISSING=0
CRITICAL_DOCS="README.md CHANGELOG.md ROADMAP.md CONTRIBUTING.md LICENSE SECURITY.md CODE_OF_CONDUCT.md"
for doc in $CRITICAL_DOCS; do
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

# 13. Check CHANGELOG for version
log_info "Checking CHANGELOG.md..."
if head -20 CHANGELOG.md | grep -q "## \[0.9.0-beta\]"; then
    log_success "CHANGELOG.md contains current version"
else
    log_warning "CHANGELOG.md may need updating for release"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 14. Check for sensitive data patterns
log_info "Checking for sensitive data patterns..."
SENSITIVE_FOUND=0
SENSITIVE_PATTERNS="password|secret|token|api[_-]?key|private[_-]?key"
if grep -rE "$SENSITIVE_PATTERNS" --include="*.go" --exclude-dir=vendor --exclude-dir=testdata . 2>/dev/null | grep -v "// " | grep -v "const" | head -5; then
    log_warning "Found potential sensitive data patterns (review manually)"
    WARNINGS=$((WARNINGS + 1))
    SENSITIVE_FOUND=1
fi
if [ $SENSITIVE_FOUND -eq 0 ]; then
    log_success "No obvious sensitive data patterns found"
fi
echo ""

# 15. Verify testdata files exist
log_info "Checking testdata files..."
TESTDATA_COUNT=$(find testdata -name "*.h5" 2>/dev/null | wc -l)
if [ "$TESTDATA_COUNT" -gt 0 ]; then
    log_success "Found $TESTDATA_COUNT HDF5 test files"
else
    log_warning "No HDF5 test files found in testdata/"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 16. Check GitHub Actions workflow
log_info "Checking GitHub Actions workflow..."
if [ -f ".github/workflows/test.yml" ]; then
    log_success "GitHub Actions workflow present"
else
    log_error "Missing .github/workflows/test.yml"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 17. Check .gitignore
log_info "Checking .gitignore..."
REQUIRED_IGNORES=".claude scripts"
MISSING_IGNORES=0
for pattern in $REQUIRED_IGNORES; do
    if ! grep -q "$pattern" .gitignore 2>/dev/null; then
        log_warning ".gitignore missing pattern: $pattern"
        MISSING_IGNORES=1
        WARNINGS=$((WARNINGS + 1))
    fi
done
if [ $MISSING_IGNORES -eq 0 ]; then
    log_success ".gitignore contains required patterns"
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
    log_info "Next steps:"
    echo "  1. Create release branch (if needed)"
    echo "  2. Update CHANGELOG.md with final notes"
    echo "  3. Create and push git tag"
    echo "  4. Wait for CI to pass (3-5 min) ⏰"
    echo "  5. Create GitHub release with notes ✅"
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
