# CLAUDE.md - HDF5 Go Library

> **Project-specific instructions for Claude Code**
> **Version**: 1.0.0
> **Last Updated**: 2025-10-17

---

## 📋 Project Status

**Current Version**: v0.9.0-beta
**Status**: Production-ready for reading (~98%)
**Test Coverage**: 76.3%
**Quality**: ⭐⭐⭐⭐⭐ (0 linter issues)

---

## 🚨 CRITICAL: Git Workflow Rules

### ⛔ Main Branch Protection

**WE HAVE EXITED RAPID DEVELOPMENT MODE!**

From now on, **STRICT GIT-FLOW ENFORCEMENT**:

```
❌ NEVER commit directly to main
❌ NEVER push to main without PR
❌ NEVER skip pre-release checks
✅ ALWAYS work in feature branches
✅ ALWAYS use git-flow workflow
✅ ALWAYS run pre-release-check.sh before merge
```

### 📐 Git-Flow Workflow (MANDATORY)

```bash
# Branch Structure
main                 # Production-ready code (PROTECTED)
  └─ develop         # Integration branch for next release
       ├─ feature/*  # New features (branch from develop)
       ├─ bugfix/*   # Bug fixes (branch from develop)
       └─ hotfix/*   # Critical fixes (branch from main)
```

### 🔧 Working with Features

```bash
# 1. Start new feature
git checkout develop
git pull origin develop
git checkout -b feature/my-feature

# 2. Work on feature
git add .
git commit -m "feat: description"

# 3. Before merge - RUN PRE-RELEASE CHECK
bash scripts/pre-release-check.sh

# 4. If checks pass - merge to develop
git checkout develop
git merge --no-ff feature/my-feature
git push origin develop

# 5. Delete feature branch
git branch -d feature/my-feature
```

### 🔥 Hotfix (Production Emergency)

```bash
# 1. Branch from main
git checkout main
git checkout -b hotfix/critical-bug

# 2. Fix and test
git commit -m "fix: critical production bug"

# 3. Merge to BOTH main AND develop
git checkout main
git merge --no-ff hotfix/critical-bug
git tag -a v0.9.1 -m "Hotfix v0.9.1"

git checkout develop
git merge --no-ff hotfix/critical-bug

# 4. Push everything
git push origin main develop --tags
```

---

## 🧪 Pre-Release Check Script

### Location
`scripts/pre-release-check.sh`

### Purpose
Automated quality gate that **MUST** pass before any merge to main.

### What It Checks

1. **Go Version** - Go 1.25+
2. **Git Status** - Clean working directory
3. **Code Formatting** - gofmt compliance
4. **Go Vet** - Static analysis
5. **Build** - All packages compile
6. **CLI Tools** - dump_hdf5 builds
7. **go.mod** - Dependencies up to date
8. **Tests with Race Detector** - All pass
9. **Test Coverage** - Target 70%+ (warns if below)
10. **golangci-lint** - 0 issues (34+ linters)
11. **TODO/FIXME** - Count and warn
12. **Documentation** - All critical files present
13. **CHANGELOG** - Version present
14. **Sensitive Data** - Pattern detection
15. **Testdata** - HDF5 files present
16. **GitHub Actions** - Workflow configured
17. **.gitignore** - Required patterns

### Usage

```bash
# Run before any merge to develop or main
bash scripts/pre-release-check.sh

# Exit codes:
# 0 = All checks passed (ready for release)
# 0 = Checks passed with warnings (review before release)
# 1 = Checks failed (fix errors before release)
```

### When to Run

- ✅ **ALWAYS** before merging feature to develop
- ✅ **ALWAYS** before merging develop to main
- ✅ **ALWAYS** before creating release tag
- ✅ **ALWAYS** before pushing hotfix
- ✅ After making significant changes
- ✅ Before creating PR

### Fixing Issues

```bash
# Formatting
make fmt

# Linting
make lint

# Tests
make test

# All-in-one
make pre-commit
```

---

## 🤖 Specialized AI Agents

### go-senior-architect

**Purpose**: Professional Go development with senior-level expertise.

**Capabilities**:
- API design (internal/wrapper)
- Professional test writing
- DDD implementation with rich domain models
- HDF5 integration
- Architectural decisions

**When to Use**:
- Complex domain logic implementation
- API wrapper design
- Test suite creation (like we did for internal/structures)
- Architectural reviews
- Code quality improvements

**Example Usage**:
```bash
# In Claude Code
Task: go-senior-architect
Prompt: "Improve test coverage for internal/core from 22% to 70%"
```

**Recent Success**:
- Improved test coverage from 5% to 76.3%
- Created 2,845 lines of professional tests
- 169 test cases for internal/structures
- 100% coverage for internal/utils

---

## 📁 Project Structure

```
hdf5/
├── .github/
│   ├── CODEOWNERS          # Code ownership (@kolkov)
│   └── workflows/
│       └── test.yml        # CI/CD (Linux, macOS, Windows)
├── .claude/
│   └── CLAUDE.md           # This file
├── cmd/
│   └── dump_hdf5/          # CLI utility
├── docs/
│   ├── architecture/
│   │   └── OVERVIEW.md
│   ├── dev/
│   │   └── USING_C_REFERENCE.md
│   └── guides/
│       └── QUICKSTART.md
├── examples/               # Usage examples
├── internal/
│   ├── core/              # HDF5 core structures (22% coverage)
│   ├── structures/        # B-tree, symbol table, heaps (95.6% coverage)
│   ├── testing/           # Test utilities
│   └── utils/             # Utilities (100% coverage)
├── scripts/
│   └── pre-release-check.sh  # Quality gate script
├── testdata/              # HDF5 test files (20 files)
├── file.go                # Public API - File operations
├── group.go               # Public API - Groups & Datasets
├── CHANGELOG.md           # Version history
├── CODE_OF_CONDUCT.md     # Community guidelines
├── CONTRIBUTING.md        # Contribution guide
├── LICENSE                # MIT License
├── Makefile               # Build tasks
├── README.md              # Project overview
├── ROADMAP.md             # Development roadmap
└── SECURITY.md            # Security policy
```

---

## 🎯 Development Priorities (from ROADMAP.md)

### Current Focus (v1.0 - Next 1-2 Months)

1. **Full Attribute Reading** - Reference: HDF5 C library H5A*.c files
2. **Object Header v1 Support** - Reference: H5Oold.c
3. **Bug Fixes and Edge Cases** - Production hardening
4. **Documentation Completion** - API reference, guides

### Quality Targets

- ✅ Test Coverage: 70%+ (achieved: 76.3%)
- ✅ Linter Issues: 0 (achieved)
- ✅ CI/CD: All platforms green (achieved)
- ⏳ Attribute Reading: In progress
- ⏳ Object Header v1: Planned

---

## 🔧 Development Commands

### Building

```bash
# Build all packages
make build

# Build CLI tool
make build-cmd

# Build everything
make all
```

### Testing

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Check coverage
make test-coverage

# Run specific package
go test ./internal/structures/...
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run all quality checks
make pre-commit

# Pre-release validation
bash scripts/pre-release-check.sh
```

### Release Process

```bash
# 1. Feature complete on develop
git checkout develop

# 2. Run pre-release check
bash scripts/pre-release-check.sh

# 3. If green, create release branch
git checkout -b release/v1.0.0

# 4. Update CHANGELOG.md, version numbers

# 5. Merge to main
git checkout main
git merge --no-ff release/v1.0.0
git tag -a v1.0.0 -m "Release v1.0.0"

# 6. Merge back to develop
git checkout develop
git merge --no-ff release/v1.0.0

# 7. Push
git push origin main develop --tags
```

---

## 📊 Test Coverage Details

| Package | Coverage | Quality |
|---------|----------|---------|
| Root | 57.1% | Good |
| internal/core | 22.1% | Needs improvement |
| internal/structures | 95.6% | Excellent |
| internal/utils | 100.0% | Perfect |
| **Overall** | **76.3%** | **Excellent** |

### Testing Philosophy

- **Table-driven tests** - Go best practice
- **testify/require** - Clear assertions
- **Mock implementations** - Isolated unit tests
- **Real file testing** - Integration with testdata/*.h5
- **Race detector** - Concurrent safety
- **Error scenarios** - Comprehensive error paths

---

## 🔒 Security Considerations

See [SECURITY.md](../SECURITY.md) for detailed security policy.

**Key Risks**:
- Binary parsing vulnerabilities (buffer/integer overflow)
- Compression bombs (GZIP)
- Resource exhaustion (deeply nested structures)
- Malicious HDF5 files

**Mitigations**:
- Bounds checking on all reads
- Size validation before allocation
- Decompression ratio limits
- Recursion depth limits

---

## 🌟 Code Quality Standards

### Linting

- **34+ linters** enabled via golangci-lint
- **0 issues** required for merge to main
- **Custom rules** for HDF5-specific patterns

### Go Version

- **Minimum**: Go 1.25+
- **Pure Go**: No CGo dependencies
- **Cross-platform**: Linux, macOS, Windows

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add compound datatype support
fix: correct endianness handling
docs: update README with examples
test: add tests for B-tree parsing
refactor: simplify datatype conversion
chore: update dependencies
```

---

## 📚 Helpful Commands

```bash
# Find TODOs
grep -r "TODO" --include="*.go" . | grep -v testdata

# Check import cycles
go list -f '{{.ImportPath}} {{.Imports}}' ./... | grep cycle

# Benchmark specific function
go test -bench=BenchmarkReadDataset -benchmem

# Profile CPU
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof

# Profile memory
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

---

## 🚀 Next Steps

1. **Complete v1.0 Features**
   - Full attribute reading
   - Object header v1 support

2. **Improve internal/core Coverage**
   - Target: 70%+ (currently 22.1%)
   - Use go-senior-architect agent

3. **Production Hardening**
   - Fuzz testing
   - Edge case coverage
   - Security audit

4. **v2.0 Planning**
   - Write support design
   - C library integration for testing
   - Performance benchmarking

---

## 📖 Documentation

- **Architecture**: [docs/architecture/OVERVIEW.md](../docs/architecture/OVERVIEW.md)
- **Quick Start**: [docs/guides/QUICKSTART.md](../docs/guides/QUICKSTART.md)
- **Contributing**: [CONTRIBUTING.md](../CONTRIBUTING.md)
- **Roadmap**: [ROADMAP.md](../ROADMAP.md)
- **Security**: [SECURITY.md](../SECURITY.md)
- **API Reference**: https://pkg.go.dev/github.com/scigolib/hdf5

---

## 🤝 Code Ownership

All code owned by **@kolkov** (see [.github/CODEOWNERS](../.github/CODEOWNERS)).

Automatic review requests on PR for:
- Public API changes (file.go, group.go)
- Core implementation (internal/core, internal/structures)
- CI/CD configuration (.github/workflows)
- Documentation updates
- Release management (CHANGELOG, ROADMAP)

---

**Remember**: Quality over speed. We're building production-grade software! 🎯

*Last updated: 2025-10-17*
