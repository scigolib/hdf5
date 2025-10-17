# Contributing to HDF5 Go Library

Thank you for considering contributing to the HDF5 Go Library! This document outlines the development workflow and guidelines.

## Git Workflow (Git-Flow)

This project uses Git-Flow branching model for development.

### Branch Structure

```
main                 # Production-ready code (tagged releases)
  └─ develop         # Integration branch for next release
       ├─ feature/*  # New features
       ├─ bugfix/*   # Bug fixes
       └─ hotfix/*   # Critical fixes from main

legacy               # Historical development (all commits from original development)
```

### Branch Purposes

- **main**: Production-ready code. Only releases are merged here.
- **develop**: Active development branch. All features merge here first.
- **feature/\***: New features. Branch from `develop`, merge back to `develop`.
- **bugfix/\***: Bug fixes. Branch from `develop`, merge back to `develop`.
- **hotfix/\***: Critical production fixes. Branch from `main`, merge to both `main` and `develop`.
- **legacy**: Historical branch preserving full development history (read-only).

### Workflow Commands

#### Starting a New Feature

```bash
# Create feature branch from develop
git checkout develop
git pull origin develop
git checkout -b feature/my-new-feature

# Work on your feature...
git add .
git commit -m "feat: add my new feature"

# When done, merge back to develop
git checkout develop
git merge --no-ff feature/my-new-feature
git branch -d feature/my-new-feature
git push origin develop
```

#### Fixing a Bug

```bash
# Create bugfix branch from develop
git checkout develop
git pull origin develop
git checkout -b bugfix/fix-issue-123

# Fix the bug...
git add .
git commit -m "fix: resolve issue #123"

# Merge back to develop
git checkout develop
git merge --no-ff bugfix/fix-issue-123
git branch -d bugfix/fix-issue-123
git push origin develop
```

#### Creating a Release

```bash
# Create release branch from develop
git checkout develop
git pull origin develop
git checkout -b release/v1.0.0

# Update version numbers, CHANGELOG, etc.
git add .
git commit -m "chore: prepare release v1.0.0"

# Merge to main and tag
git checkout main
git merge --no-ff release/v1.0.0
git tag -a v1.0.0 -m "Release v1.0.0"

# Merge back to develop
git checkout develop
git merge --no-ff release/v1.0.0

# Delete release branch
git branch -d release/v1.0.0

# Push everything
git push origin main develop --tags
```

#### Hotfix (Critical Production Bug)

```bash
# Create hotfix branch from main
git checkout main
git pull origin main
git checkout -b hotfix/critical-bug

# Fix the bug...
git add .
git commit -m "fix: critical production bug"

# Merge to main and tag
git checkout main
git merge --no-ff hotfix/critical-bug
git tag -a v1.0.1 -m "Hotfix v1.0.1"

# Merge to develop
git checkout develop
git merge --no-ff hotfix/critical-bug

# Delete hotfix branch
git branch -d hotfix/critical-bug

# Push everything
git push origin main develop --tags
```

## Commit Message Guidelines

Follow [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

### Types

- **feat**: New feature
- **fix**: Bug fix
- **docs**: Documentation changes
- **style**: Code style changes (formatting, etc.)
- **refactor**: Code refactoring
- **test**: Adding or updating tests
- **chore**: Maintenance tasks (build, dependencies, etc.)
- **perf**: Performance improvements

### Examples

```bash
feat: add support for compound datatypes
fix: correct endianness handling in superblock
docs: update README with new examples
refactor: simplify datatype parsing logic
test: add tests for chunked dataset reading
chore: update golangci-lint configuration
```

## Code Quality Standards

### Before Committing

1. **Format code**:
   ```bash
   make fmt
   ```

2. **Run linter**:
   ```bash
   make lint
   ```

3. **Run tests**:
   ```bash
   make test
   ```

4. **All-in-one**:
   ```bash
   make pre-commit
   ```

### Pull Request Requirements

- [ ] Code is formatted (`make fmt`)
- [ ] Linter passes (`make lint`)
- [ ] All tests pass (`make test`)
- [ ] New code has tests (minimum 70% coverage)
- [ ] Documentation updated (if applicable)
- [ ] Commit messages follow conventions
- [ ] No sensitive data (credentials, tokens, etc.)

## Development Setup

### Prerequisites

- Go 1.25 or later
- golangci-lint
- Python 3 with h5py and numpy (for test file generation)

### Install Dependencies

```bash
# Install golangci-lint
make install-lint

# Install Python dependencies (optional, for test files)
pip install h5py numpy
```

### Running Tests

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run with race detector
make test-race

# Run benchmarks
make benchmark
```

### Running Linter

```bash
# Run linter
make lint

# Save linter report
make lint-report
```

## Project Structure

```
hdf5/
├── .golangci.yml         # Linter configuration
├── Makefile              # Development commands
├── cmd/                  # Command-line utilities
├── docs/                 # Documentation
├── examples/             # Usage examples
├── internal/             # Internal implementation
│   ├── core/            # Core HDF5 structures
│   ├── structures/      # HDF5 data structures
│   ├── testing/         # Test utilities
│   └── utils/           # Utility functions
├── testdata/            # Test HDF5 files
├── file.go              # Public API - File operations
├── group.go             # Public API - Groups & Datasets
└── README.md            # Main documentation
```

## Adding New Features

1. Check if issue exists, if not create one
2. Discuss approach in the issue
3. Create feature branch from `develop`
4. Implement feature with tests
5. Update documentation
6. Run quality checks (`make pre-commit`)
7. Create pull request to `develop`
8. Wait for code review
9. Address feedback
10. Merge when approved

## Code Style Guidelines

### General Principles

- Follow Go conventions and idioms
- Write self-documenting code
- Add comments for complex logic
- Keep functions small and focused
- Use meaningful variable names

### Naming Conventions

- **Public types/functions**: `PascalCase` (e.g., `ReadSuperblock`)
- **Private types/functions**: `camelCase` (e.g., `readSignature`)
- **Constants**: `PascalCase` with context prefix (e.g., `Version0`)
- **Test functions**: `Test*` (e.g., `TestReadSuperblock`)

### Error Handling

- Always check and handle errors
- Use `utils.WrapError(context, err)` to add context
- Never ignore errors
- Validate inputs

### Testing

- Use `testify/require` for assertions
- Test both success and error cases
- Use table-driven tests when appropriate
- Mock external dependencies

## Getting Help

- Check existing issues and discussions
- Read documentation in `docs/`
- Review architecture documentation in `docs/architecture/`
- Ask questions in GitHub Issues

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

---

**Thank you for contributing to the HDF5 Go Library!** 🎉
