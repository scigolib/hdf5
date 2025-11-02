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

# 6.5. Verify golangci-lint configuration
log_info "Verifying golangci-lint configuration..."
if command -v golangci-lint &> /dev/null; then
    if golangci-lint config verify 2>&1; then
        log_success "golangci-lint config is valid"
    else
        log_error "golangci-lint config is invalid"
        ERRORS=$((ERRORS + 1))
    fi
else
    log_warning "golangci-lint not installed (optional but recommended)"
    log_info "Install: https://golangci-lint.run/welcome/install/"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 7. Run tests with race detector (supports WSL2 fallback)
USE_WSL=0
WSL_DISTRO=""

# Helper function to find WSL distro with Go installed
find_wsl_distro() {
    if ! command -v wsl &> /dev/null; then
        return 1
    fi

    # Try common distros first
    for distro in "Gentoo" "Ubuntu" "Debian" "Alpine"; do
        if wsl -d "$distro" bash -c "command -v go &> /dev/null" 2>/dev/null; then
            echo "$distro"
            return 0
        fi
    done

    return 1
}

if command -v gcc &> /dev/null || command -v clang &> /dev/null; then
    log_info "Running tests with race detector..."
    RACE_FLAG="-race"
    TEST_CMD="go test -race ./... 2>&1"
else
    # Try to find WSL distro with Go
    WSL_DISTRO=$(find_wsl_distro)
    if [ -n "$WSL_DISTRO" ]; then
        log_info "GCC not found locally, but WSL2 ($WSL_DISTRO) detected!"
        log_info "Running tests with race detector via WSL2 $WSL_DISTRO..."
        USE_WSL=1
        RACE_FLAG="-race"

        # Convert Windows path to WSL path (D:\projects\... -> /mnt/d/projects/...)
        CURRENT_DIR=$(pwd)
        if [[ "$CURRENT_DIR" =~ ^/([a-z])/ ]]; then
            # Already in /d/... format (MSYS), convert to /mnt/d/...
            WSL_PATH="/mnt${CURRENT_DIR}"
        else
            # Windows format D:\... convert to /mnt/d/...
            DRIVE_LETTER=$(echo "$CURRENT_DIR" | cut -d: -f1 | tr '[:upper:]' '[:lower:]')
            PATH_WITHOUT_DRIVE=${CURRENT_DIR#*:}
            WSL_PATH="/mnt/$DRIVE_LETTER${PATH_WITHOUT_DRIVE//\\//}"
        fi

        TEST_CMD="wsl -d \"$WSL_DISTRO\" bash -c \"cd \\\"$WSL_PATH\\\" && go test -race ./... 2>&1\""
    else
        log_warning "GCC not found, running tests WITHOUT race detector"
        log_info "Install GCC (mingw-w64) or setup WSL2 with Go for race detection"
        log_info "  Windows: https://www.mingw-w64.org/"
        log_info "  WSL2: https://docs.microsoft.com/en-us/windows/wsl/install"
        WARNINGS=$((WARNINGS + 1))
        RACE_FLAG=""
        TEST_CMD="go test ./... 2>&1"
    fi
fi

log_info "Running tests..."
if [ $USE_WSL -eq 1 ]; then
    # WSL2: Use timeout (3 min) and unbuffered output
    TEST_OUTPUT=$(wsl -d "$WSL_DISTRO" bash -c "cd $WSL_PATH && timeout 180 stdbuf -oL -eL go test -race ./... 2>&1" || true)
    if [ -z "$TEST_OUTPUT" ]; then
        log_error "WSL2 tests timed out or failed to run"
        ERRORS=$((ERRORS + 1))
    fi
else
    TEST_OUTPUT=$(eval "$TEST_CMD")
fi

# Check if race detector failed to build (known issue with some Go versions)
if echo "$TEST_OUTPUT" | grep -q "hole in findfunctab\|build failed.*race"; then
    log_warning "Race detector build failed (known Go runtime issue)"
    log_info "Falling back to tests without race detector..."

    if [ $USE_WSL -eq 1 ]; then
        TEST_OUTPUT=$(wsl -d "$WSL_DISTRO" bash -c "cd \"$WSL_PATH\" && go test ./... 2>&1")
    else
        TEST_OUTPUT=$(go test ./... 2>&1)
    fi

    RACE_FLAG=""
    WARNINGS=$((WARNINGS + 1))
fi

if echo "$TEST_OUTPUT" | grep -q "FAIL"; then
    # Check if failure is only due to performance tests in WSL2 (acceptable)
    if [ $USE_WSL -eq 1 ] && echo "$TEST_OUTPUT" | grep -q "TestMetricsCollector_Performance" && ! echo "$TEST_OUTPUT" | grep -q "race detected"; then
        log_warning "Performance tests failed in WSL2 (acceptable - WSL2 has overhead)"
        echo "$TEST_OUTPUT" | grep -A 5 "FAIL:"
        echo ""
        log_info "No race conditions detected - this is OK for WSL2"
        WARNINGS=$((WARNINGS + 1))
    else
        log_error "Tests failed or race conditions detected"
        echo "$TEST_OUTPUT"
        echo ""
        ERRORS=$((ERRORS + 1))
    fi
elif echo "$TEST_OUTPUT" | grep -q "PASS\|ok"; then
    if [ $USE_WSL -eq 1 ] && [ -n "$RACE_FLAG" ]; then
        log_success "All tests passed with race detector (via WSL2 $WSL_DISTRO)"
    elif [ -n "$RACE_FLAG" ]; then
        log_success "All tests passed with race detector (0 races)"
    else
        log_success "All tests passed (race detector not available)"
    fi
else
    log_error "Unexpected test output"
    echo "$TEST_OUTPUT"
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

# 13. Check ROADMAP.md current status
log_info "Checking ROADMAP.md current version..."
CURRENT_VERSION=$(grep "Current Version" ROADMAP.md | head -1 | sed -n 's/.*Current Version\*\*: \(v[^ |]*\).*/\1/p')
if [ -n "$CURRENT_VERSION" ]; then
    log_success "ROADMAP.md shows current version: $CURRENT_VERSION"
else
    log_warning "Could not detect current version in ROADMAP.md"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Summary
echo "========================================"
echo "  Summary"
echo "========================================"
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    log_success "✅ All checks passed! Ready for release."
    echo ""
    log_info "Next steps for release (see RELEASE_GUIDE.md for details):"
    echo ""
    echo "  1. Create release branch from develop:"
    echo "     git checkout -b release/vX.Y.Z develop"
    echo ""
    echo "  2. Prepare release (ONE commit with ALL changes):"
    echo "     - Update CHANGELOG.md"
    echo "     - Update README.md"
    echo "     - Update ROADMAP.md"
    echo "     - Update all docs/guides/ versions"
    echo "     bash scripts/pre-release-check.sh  # Re-run to verify"
    echo "     git add -A"
    echo "     git commit -m \"chore: prepare vX.Y.Z release\""
    echo ""
    echo "  3. Push release branch, wait for CI:"
    echo "     git push origin release/vX.Y.Z"
    echo "     ⏳ WAIT for CI to be GREEN"
    echo ""
    echo "  4. Merge to main (USE SQUASH for clean history!):"
    echo "     git checkout main"
    echo "     git merge --squash release/vX.Y.Z"
    echo "     git commit -m \"Release vX.Y.Z"
    echo ""
    echo "     <detailed release notes>\""
    echo "     git push origin main"
    echo "     ⏳ WAIT for CI to be GREEN on main!"
    echo ""
    echo "  5. ONLY AFTER CI GREEN - create and push tag:"
    echo "     git tag -a vX.Y.Z -m \"Release vX.Y.Z\""
    echo "     git push origin main --tags  # Tags are PERMANENT!"
    echo ""
    echo "  6. Merge back to develop:"
    echo "     git checkout develop"
    echo "     git merge --no-ff main -m \"Merge release vX.Y.Z back to develop\""
    echo "     git push origin develop"
    echo ""
    echo "  7. Clean up:"
    echo "     git branch -d release/vX.Y.Z"
    echo "     git push origin --delete release/vX.Y.Z"
    echo ""
    echo "  8. Create GitHub release with notes"
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
