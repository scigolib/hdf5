# Release Guide - HDF5 Go Library

**CRITICAL**: Read this guide BEFORE creating any release!

---

## 🔴 CRITICAL: Backup Before Any Operation

**ALWAYS create a backup before any serious operations!**

```bash
# Create backup BEFORE any git operations with branches/tags
cd /d/projects/scigolibs
cp -r hdf5 hdf5-backup-$(date +%Y%m%d-%H%M%S)

# Or use git bundle (portable backup)
cd hdf5
git bundle create ../hdf5-backup.bundle --all
```

**Dangerous operations (require backup)**:
- `git reset --hard`
- `git branch -D`
- `git tag -d`
- `git push -f`
- `git rebase`
- Any rollback/deletion operations

---

## 🎯 Git Flow Strategy

### Branch Structure

```
main        - Production-ready code ONLY (protected, green CI always)
  ↑
release/*   - Release candidates (RC)
  ↑
develop     - Active development (default branch for PRs)
  ↑
feature/*   - Feature branches
```

### Branch Rules

#### `main` Branch
- ✅ **ALWAYS** production-ready
- ✅ **ALWAYS** green CI (all tests passing)
- ✅ **ONLY** accepts merges from `release/*` branches
- ❌ **NEVER** commit directly to main
- ❌ **NEVER** push without green CI
- ❌ **NEVER** force push
- 🏷️ **Tags created ONLY after CI passes**

#### `develop` Branch
- Default branch for development
- Accepts feature branches
- May contain work-in-progress code
- Should pass tests, but can have warnings
- **Current default branch**

#### `release/*` Branches
- Format: `release/v0.10.0-beta`, `release/v1.0.0`
- Created from `develop`
- Only bug fixes and documentation updates allowed
- No new features
- Merges to both `main` and `develop`

#### `feature/*` Branches
- Format: `feature/dense-attributes`, `feature/write-support`
- Created from `develop`
- Merged back to `develop` with `--no-ff`

---

## 📋 Version Naming

### Semantic Versioning

Format: `MAJOR.MINOR.PATCH[-PRERELEASE]`

Examples:
- `v0.9.0-beta` - Current version (read-only beta)
- `v0.10.0-beta` - Next version (feature-complete read)
- `v0.11.0-beta` - Write support MVP
- `v1.0.0-rc.1` - Release candidate 1
- `v1.0.0` - First stable release (full read/write)
- `v1.1.0` - Minor feature update
- `v1.1.1` - Patch/bugfix

### Version Increment Rules

**MAJOR** (1.0.0 → 2.0.0):
- Breaking API changes
- Major architectural changes
- Requires migration guide
- **NOTE**: For Go, MAJOR v2+ requires new module path (e.g., `/v2`)

**MINOR** (0.9.0 → 0.10.0):
- New features (backward compatible)
- New HDF5 message types supported
- Performance improvements
- New format version support

**PATCH** (0.10.0 → 0.10.1):
- Bug fixes
- Performance improvements
- Documentation updates
- Security patches

**PRERELEASE**:
- `-alpha` - Early testing, unstable API
- `-beta` - Feature complete for milestone, testing phase
- `-rc.N` - Release candidate (N = 1, 2, 3...)

### HDF5 Library Versioning Strategy

**Current Path**: `v0.x.x-beta` until `v1.0.0`

- `v0.9.0-beta`: Basic read support (current)
- `v0.10.0-beta`: Feature-complete read support (in progress)
- `v0.11.0-beta`: Basic write support (MVP)
- `v0.12.0-beta`: Full write support
- `v1.0.0-rc.1`: Release candidate (API stable)
- `v1.0.0`: First stable release

**Rationale**: Avoid `v2.0.0` approach (requires new import path). Use `v0.x.x` beta progression until feature-complete, then `v1.0.0` stable.

---

## ✅ Pre-Release Checklist

**CRITICAL**: Complete ALL items before creating release branch!

### 1. Automated Quality Checks

**Run our pre-release validation script**:

```bash
# ONE COMMAND runs ALL checks (matches CI exactly)
bash scripts/pre-release-check.sh
```

This script validates:
- ✅ Go version (1.25+)
- ✅ Code formatting (gofmt)
- ✅ Static analysis (go vet)
- ✅ All tests passing
- ✅ Race detector
- ✅ Coverage >70% (internal packages)
- ✅ golangci-lint (0 issues required)
- ✅ go.mod integrity
- ✅ Reference tests (57 files)
- ✅ No TODO/FIXME comments (requirement: 0)
- ✅ All documentation present
- ✅ Sprint completion status

**Manual checks** (if script not available):

```bash
# Format code
go fmt ./...

# Verify formatting
if [ -n "$(gofmt -l .)" ]; then
  echo "ERROR: Code not formatted"
  gofmt -l .
  exit 1
fi

# Static analysis
go vet ./...

# Linting (strict)
golangci-lint run --timeout=5m ./...
# Must show: "0 issues."

# All tests
go test ./...
# All must PASS

# Coverage check
go test -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | tail -1
# Minimum: >70% for internal packages

# Race detector
go test -race ./...
```

### 2. Dependencies

```bash
# Verify modules
go mod verify

# Tidy and check diff
go mod tidy
git diff go.mod go.sum
# Should show NO changes

# Check for vulnerabilities
go list -m all | grep -v indirect
# Review all direct dependencies
```

### 3. Documentation

- [ ] README.md updated with latest features
- [ ] CHANGELOG.md entry created for this version
- [ ] All public APIs have godoc comments
- [ ] Examples are up-to-date and tested
- [ ] Migration guide (if breaking changes)
- [ ] ROADMAP.md updated with sprint progress
- [ ] Known limitations documented

### 4. GitHub Actions

- [ ] `.github/workflows/*.yml` exist
- [ ] All workflows tested on `develop`
- [ ] CI passes on latest `develop` commit
- [ ] Coverage badge updated (if changed)

### 5. Project-Specific Checks

**HDF5 Library Requirements**:
- [ ] All v0.10.0-beta sprint tasks complete (6/6)
- [ ] Test coverage >70% for internal packages
- [ ] All HDF5 format features documented
- [ ] C library references cited in code
- [ ] Fractal heap tests passing
- [ ] Attribute reading works (compact + dense infrastructure)
- [ ] Object header v1 & v2 supported
- [ ] No regressions in existing features

---

## 🚀 Release Process

### Step 1: Create Release Branch

```bash
# Ensure you're on develop and up-to-date
git checkout develop
git pull origin develop

# Verify develop is clean
git status
# Should show: "nothing to commit, working tree clean"

# Run ALL pre-release checks (CRITICAL!)
bash scripts/pre-release-check.sh
# Script must exit with: "All checks passed! Ready for release."
# If errors: FIX THEM before proceeding!

# Create release branch (example: v0.10.0-beta)
git checkout -b release/v0.10.0-beta

# Update version in files
# - README.md (version badges)
# - CHANGELOG.md (add version section)
# - ROADMAP.md (update status)

git add .
git commit -m "chore: prepare v0.10.0-beta release"
git push origin release/v0.10.0-beta
```

### Step 2: Wait for CI (CRITICAL!)

```bash
# Go to GitHub Actions and WAIT for green CI
# URL: https://github.com/scigolib/hdf5/actions
```

**⏸️ STOP HERE! Do NOT proceed until CI is GREEN!**

✅ **All checks must pass:**
- Unit tests (Linux, macOS, Windows)
- Linting (golangci-lint)
- Code formatting (gofmt)
- Coverage check (>70%)
- Race detector

❌ **If CI fails:**
1. Fix issues in `release/v0.10.0-beta` branch
2. Commit fixes
3. Push and wait for CI again
4. Repeat until GREEN

### Step 3: Merge to Main (After Green CI)

```bash
# ONLY after CI is green!
git checkout main
git pull origin main

# Merge release branch (--no-ff ensures merge commit)
git merge --no-ff release/v0.10.0-beta -m "Release v0.10.0-beta

Complete v0.10.0-beta implementation:
- Feature-complete read support for HDF5 files
- Compact attribute reading (94.9% coverage)
- Fractal heap infrastructure for dense attributes
- Object header v1 & v2 support with continuation blocks
- Test coverage >70% (76.3% achieved)
- Zero production dependencies (pure Go)
- Comprehensive test suite (45+ tests)
- Production-ready documentation

Sprint Progress: 6/6 tasks complete (100%)
Known Limitations: Dense attributes require B-tree v2 (v0.11.0)"

# Push to main
git push origin main
```

### Step 4: Wait for CI on Main

```bash
# Go to GitHub Actions and verify main branch CI
# https://github.com/scigolib/hdf5/actions

# WAIT for green CI on main branch!
```

**⏸️ STOP! Do NOT create tag until main CI is GREEN!**

### Step 5: Create Tag (After Green CI on Main)

```bash
# ONLY after main CI is green!

# Create annotated tag
git tag -a v0.10.0-beta -m "Release v0.10.0-beta

HDF5 Go Library v0.10.0-beta - Feature-Complete Read Support

Features:
- Complete HDF5 read support (all datatypes, layouts, compression)
- Compact attribute reading (versions 1 & 3)
- Fractal heap infrastructure for dense attributes
- Object header v1 & v2 support with continuation blocks
- All standard datatypes: int32, int64, float32, float64, strings
- Chunked, compact, and contiguous layouts
- GZIP compression support
- Dataset slicing and efficient reading

Performance:
- Zero production dependencies (pure Go)
- Efficient buffer pooling
- Memory-safe operations
- Test coverage: 76.3% overall, 94.9% for attributes

Quality:
- 45+ unit tests passing
- golangci-lint compliant (0 issues)
- Race detector clean
- Production-ready documentation
- C library references throughout

Known Limitations:
- Dense attributes require B-tree v2 iteration (v0.11.0)
- Read-only (write support in v0.11.0+)
- Affects <10% of real-world HDF5 files

API Stability:
- Read API stable and production-ready
- No breaking changes expected for read operations
- Write API will be added in v0.11.0

See CHANGELOG.md for complete details."

# Push tag
git push origin v0.10.0-beta
```

### Step 6: Merge Back to Develop

```bash
# Keep develop in sync
git checkout develop
git merge --no-ff release/v0.10.0-beta -m "Merge release v0.10.0-beta back to develop"
git push origin develop

# Delete release branch (optional, after confirming release is good)
git branch -d release/v0.10.0-beta
git push origin --delete release/v0.10.0-beta
```

### Step 7: Create GitHub Release

1. Go to: https://github.com/scigolib/hdf5/releases/new
2. Select tag: `v0.10.0-beta`
3. Release title: `v0.10.0-beta - Feature-Complete Read Support`
4. Description: Copy from CHANGELOG.md
5. Check "Set as a pre-release" (beta releases)
6. Click "Publish release"

---

## 🔥 Hotfix Process

For critical bugs in production (`main` branch):

```bash
# Create hotfix branch from main
git checkout main
git pull origin main
git checkout -b hotfix/v0.10.1-beta

# Fix the bug
# ... make changes ...

# Test thoroughly
go test ./...
go test -race ./...
golangci-lint run ./...

# Commit
git add .
git commit -m "fix: critical bug in attribute parsing"

# Push and wait for CI
git push origin hotfix/v0.10.1-beta

# WAIT FOR GREEN CI!

# Merge to main
git checkout main
git merge --no-ff hotfix/v0.10.1-beta -m "Hotfix v0.10.1-beta"
git push origin main

# WAIT FOR GREEN CI ON MAIN!

# Create tag
git tag -a v0.10.1-beta -m "Hotfix v0.10.1-beta - Fix critical attribute parsing bug"
git push origin v0.10.1-beta

# Merge back to develop
git checkout develop
git merge --no-ff hotfix/v0.10.1-beta -m "Merge hotfix v0.10.1-beta"
git push origin develop

# Delete hotfix branch
git branch -d hotfix/v0.10.1-beta
git push origin --delete hotfix/v0.10.1-beta
```

---

## 📊 CI Requirements

### Must Pass Before Release

All GitHub Actions workflows must be GREEN:

1. **Unit Tests** (3 platforms)
   - Linux (ubuntu-latest)
   - macOS (macos-latest)
   - Windows (windows-latest)
   - Go versions: 1.23, 1.24, 1.25

2. **Code Quality**
   - go vet (no errors)
   - golangci-lint (34+ linters, 0 issues)
   - gofmt (all files formatted)

3. **Coverage**
   - Overall: ≥70%
   - internal/utils: 100%
   - internal/structures: ≥85%
   - internal/core: ≥70%

4. **Race Detection**
   - go test -race ./... (no data races)

---

## 🚫 NEVER Do This

❌ **NEVER commit directly to main**
```bash
# WRONG!
git checkout main
git commit -m "quick fix"  # ❌ NO!
```

❌ **NEVER push to main without green CI**
```bash
# WRONG!
git push origin main  # ❌ WAIT for CI first!
```

❌ **NEVER create tags before CI passes**
```bash
# WRONG!
git tag v0.10.0-beta  # ❌ WAIT for green CI on main!
git push origin v0.10.0-beta
```

❌ **NEVER force push to main or develop**
```bash
# WRONG!
git push -f origin main  # ❌ NEVER!
```

❌ **NEVER skip lint or format checks**
```bash
# WRONG!
git commit -m "skip CI" --no-verify  # ❌ NO!
```

❌ **NEVER push without running lint locally**
```bash
# WRONG WORKFLOW:
git commit -m "feat: something"
git push  # ❌ Run lint FIRST!

# CORRECT WORKFLOW:
golangci-lint run ./...  # ✅ Check FIRST
go fmt ./...              # ✅ Format FIRST
go test ./...             # ✅ Test FIRST
git commit -m "feat: something"
git push
```

---

## ✅ Always Do This

✅ **ALWAYS run checks before commit**
```bash
# Recommended: Use our pre-release script
bash scripts/pre-release-check.sh

# Or manual workflow:
go fmt ./...
golangci-lint run ./...
go test ./...
git add .
git commit -m "..."
git push
```

✅ **ALWAYS wait for green CI before proceeding**
```bash
# Correct workflow:
git push origin release/v0.10.0-beta
# ⏸️ WAIT for green CI
git checkout main
git merge --no-ff release/v0.10.0-beta
git push origin main
# ⏸️ WAIT for green CI on main
git tag -a v0.10.0-beta -m "..."
git push origin v0.10.0-beta
```

✅ **ALWAYS use annotated tags**
```bash
# Good
git tag -a v0.10.0-beta -m "Release v0.10.0-beta"

# Bad
git tag v0.10.0-beta  # Lightweight tag
```

✅ **ALWAYS update CHANGELOG.md**
- Document all changes
- Include breaking changes
- Add known limitations
- Reference task completion

✅ **ALWAYS test on all platforms locally if possible**
```bash
# At minimum:
go test ./...
go test -race ./...
golangci-lint run ./...
go mod verify
```

✅ **ALWAYS check C library references**
- Cite source files (H5*.c)
- Document format compliance
- Reference HDF5 specification sections

---

## 📝 Release Checklist Template

Copy this for each release:

```markdown
## Release v0.10.0-beta Checklist

### Pre-Release
- [ ] All tests passing locally (`go test ./...`)
- [ ] Race detector clean (`go test -race ./...`)
- [ ] Code formatted (`go fmt ./...`, `gofmt -l .` = empty)
- [ ] Linter clean (`golangci-lint run ./...` = 0 issues)
- [ ] Dependencies verified (`go mod verify`)
- [ ] CHANGELOG.md updated
- [ ] ROADMAP.md updated with sprint status
- [ ] README.md updated (if needed)
- [ ] Version bumped in relevant files
- [ ] Sprint tasks complete (X/6)

### Release Branch
- [ ] Created release/v0.10.0-beta from develop
- [ ] Pushed to GitHub
- [ ] CI GREEN on release branch
- [ ] All checks passed (tests, lint, format, coverage)

### Main Branch
- [ ] Merged release branch to main (`--no-ff`)
- [ ] Pushed to origin
- [ ] CI GREEN on main
- [ ] All checks passed

### Tagging
- [ ] Created annotated tag v0.10.0-beta
- [ ] Tag message includes full changelog
- [ ] Pushed tag to origin
- [ ] GitHub release created (set as pre-release for beta)

### Cleanup
- [ ] Merged back to develop
- [ ] Deleted release branch
- [ ] Verified pkg.go.dev updated
- [ ] Announced release (if applicable)
```

---

## 🎯 Summary: Golden Rules

1. **main = Production ONLY** - Always green CI, always stable
2. **Wait for CI** - NEVER proceed without green CI
3. **Tags LAST** - Only after main CI is green
4. **No Direct Commits** - Use release branches
5. **Annotated Tags** - Always use `git tag -a`
6. **Full Testing** - Run `golangci-lint` + `go test` before commit
7. **Document Everything** - Update CHANGELOG.md, README.md, ROADMAP.md
8. **Git Flow** - develop → release/* → main → tag
9. **Check Lint ALWAYS** - `golangci-lint run ./...` before every push
10. **Pure Go** - Zero production dependencies (test dependencies OK)

---

## 🔧 HDF5-Specific Guidelines

### Before Release

**C Library Compliance**:
- [ ] All implemented features match HDF5 C library behavior
- [ ] Format parsing follows HDF5 specification
- [ ] Test files readable by `h5dump` (if write support)

**Documentation**:
- [ ] All C library references cited (H5*.c files)
- [ ] Format spec sections referenced
- [ ] Known limitations documented
- [ ] Compatibility matrix updated

**Testing**:
- [ ] Test with real HDF5 files (scientific datasets)
- [ ] Compare output with `h5dump`
- [ ] Verify all supported HDF5 versions
- [ ] Test with various datatypes and layouts

---

**Remember**: A release can always wait. A broken production release cannot be undone.

**When in doubt, wait for CI!**

**Always run lint before push!**

---

*Last Updated: 2025-10-29*
*HDF5 Go Library v0.10.0-beta Release Process*
