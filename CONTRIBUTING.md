# Contributing to scigolib/hdf5

Contributions are welcome: bug reports with a sample file, fixes, documentation, and files from your
own field that the library should read. This document describes how changes get into `main`.

The standard a change is held to — correctness against the C library, code, tests, architecture, review
and release — is [docs/QUALITY.md](docs/QUALITY.md).

To report a security vulnerability, do not open an issue; follow [SECURITY.md](SECURITY.md). By taking
part you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Search the [issues](https://github.com/scigolib/hdf5/issues) and
  [discussions](https://github.com/scigolib/hdf5/discussions) first.
- For a bug, an issue with a small file that shows it (or the code that writes it) is the most useful
  thing you can send. For a larger change, open an issue or discussion first so the approach is agreed
  before you write the code.
- Read the [architecture overview](docs/architecture/OVERVIEW.md) to see where the code lives.

## Development setup

- Go 1.25 or later (the `go` line of `go.mod`). The library has no dependencies outside the standard
  library; tests use `github.com/stretchr/testify`.
- [golangci-lint](https://golangci-lint.run/) for linting; `make install-lint` installs it if it is
  missing.
- A C compiler (cgo) only if you want to run the race detector (`go test -race`). On Windows use WSL
  or a Linux machine for that.
- Optional: the HDF5 command-line tools (`h5dump`, `h5ls`, `h5stat`, `h5repack`, `h5copy`, `h5diff`,
  `h5debug`, `h5unjam`; on Debian and Ubuntu the package `hdf5-tools`). The C interoperability tests
  look each tool up in `PATH` and skip themselves when it is missing, so the suite passes without
  them. CI runs these tests with the tools installed, so a change to what the library writes should be
  checked with them before you push.

```bash
git clone https://github.com/scigolib/hdf5.git
cd hdf5
go build ./...
go test ./...
```

## Workflow

The project uses GitHub Flow.

- `main` is the only long-lived branch and is protected by a repository ruleset: no direct pushes, no
  force pushes, no deletion. Every change arrives through a pull request.
- Work on a branch from `main`, named for its kind: `feature/<topic>`, `fix/<topic>`, `docs/<topic>`.
  Contributors work from a fork.
- Keep a pull request to one logical change. Unrelated fixes go in separate pull requests.
- Push the branch and open a pull request against `main`. If review asks for changes, push more
  commits to the same branch; the pull request updates itself.
- A pull request needs an approving review from someone other than its author, and the required checks
  must pass. A maintainer merges once both hold and the review comments are resolved. The merge method
  (rebase, squash or merge commit) is chosen from the pull request's commits: a clean series of
  commits is kept as it is, a work log of fixups is squashed. You do not need to rewrite your history
  for this.
- Releases are tagged by maintainers from `main`; see [CHANGELOG.md](CHANGELOG.md).

### Continuous integration

`.github/workflows/test.yml` runs on every pull request to `main` and on every push to `main`:

| Job | What it checks |
|-----|----------------|
| Unit Tests (Linux, macOS, Windows; Go 1.25, 1.26, 1.27) | `go vet` (Linux), then `go test -short -race` with coverage |
| Lint | `golangci-lint` with the repository's `.golangci.yml` |
| Code Formatting | `gofmt -l .` prints nothing |
| Cross-Build 32-bit | the module builds for linux/386, linux/arm (GOARM=6) and windows/386, and `go vet` passes on linux/386 |
| C Library Interop | `go test -run TestCInterop .` with the HDF5 tools installed |
| Fuzz | 60 seconds each of `FuzzOpenFile` and `FuzzParseAttributeMessage`; informational, does not block merging |
| Govulncheck | known vulnerabilities in the dependencies and the standard library; informational, does not block merging |
| Required checks | the gate the ruleset requires: fails unless every required job passed |

The branch ruleset requires one check, `Required checks`: a gate that fails unless every required job
passed — the Unit Tests jobs, Lint, Code Formatting, Cross-Build 32-bit and C Library Interop — plus one
approving review on the pull request.

A change that passes `go test ./...` on one platform can still fail on another, on a 32-bit target or
on a different Go version. If a CI job fails on your pull request, the log shows which test; fix it on
the same branch.

## Quality gates to run locally

Before you push, from the repository root:

```bash
gofmt -l .                  # must print nothing
go vet ./...
go test ./...
golangci-lint run ./...     # must report 0 issues
```

Also useful:

```bash
go test -race ./...         # needs cgo (Linux, macOS, or WSL); CI runs it with -short
go test -run TestName ./... # one test
go test -coverprofile=tmp/coverage.out ./... && go tool cover -html=tmp/coverage.out   # tmp/ is ignored by git
GOOS=linux GOARCH=386 go build ./...   # 32-bit build, as CI does
```

`make` wraps the same steps: `make test`, `make test-race`, `make lint`, `make fmt` (runs
`gofmt -w -s` and `go mod tidy`), `make fmt-check`, `make benchmark` and `make ci` (format check,
tests, lint). Put generated files (coverage, profiles, test binaries) outside the source tree or under
a directory that is ignored by git, and do not commit them.

The linter configuration is `.golangci.yml`: it enables complexity, bug-detection (`staticcheck`,
`gosec`, `errcheck`, `errorlint`, `nilerr`), style and performance linters. Fix what it reports instead
of silencing it; a `//nolint` comment needs a reason and a narrow scope.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`, with the
types `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test` and `chore`. The first line says what
changed; the body says why, and for a bug fix what the failure was.

```
fix: write v2 B-tree leaves as whole nodes, as the C library does
feat: add user block support (WithUserBlock, WriteUserBlock)
docs: check the guides against the code
test: cover a truncated fractal heap block
```

Make each commit one finished change: a fix and its test go together. Do not commit credentials,
generated files or personal paths.

## Tests

Every change ends with tests. The test suite is the main safeguard against a library that parses
untrusted binary files, so tests must be able to fail.

- **A bug fix starts with a test that fails before the fix.** Write the test, run it against the
  current code and see it fail for the reason the bug describes, then fix the code. Say in the pull
  request how you saw it fail.
- **Never weaken a test to make it pass.** If a test fails, either the code is wrong or the intended
  behaviour changed. In the second case update the assertion deliberately and explain it in the pull
  request.
- **Corruption and security tests must not exhaust memory.** Prove the check, not the crash: claim a
  few megabytes against a few dozen bytes of input and assert the error. A test that needs a
  multi-gigabyte allocation to fail can take the test machine down. Where a damaged field has to be
  written into a file, patch the bytes of a small valid file.
- **Files the library writes are verified with the C tools.** For a change to the write path, add a
  `TestCInterop*` test (see `interop_c_test.go`): write the file, run `h5dump` (or `h5ls`, `h5stat`,
  `h5repack`) on it and compare the output. A file only this library can read is not a success.
- **Round trips.** Write, read back and compare. Include the edge cases of the format: empty
  datasets, one element, chunk edges, the largest and smallest values of a type.
- Use `testify/require`, table-driven tests where cases share a shape, and `t.TempDir()` for files a
  test creates. Test both success and error paths. Tests of the public API live in package `hdf5`
  next to the code; tests of internals live beside their package.
- Fuzz targets (`func FuzzXxx(f *testing.F)`) exist for the parsers. When a parser changes, run its
  target locally for a while: `go test -run=^$ -fuzz=FuzzOpenFile -fuzztime=60s .`. A crasher goes
  into the regression inputs under `testdata/fuzz` together with the fix.
- Test files that are checked in live in `testdata/`. Keep them small. `testdata/reference/` and the
  top-level `*.h5` files are fixtures for reading; `testdata/hdf5_official/` holds the HDF Group's
  own test files; `testdata/c-library-corpus/` holds files grouped by feature. If you add a file,
  say in the pull request what tool created it and with which options.
- Coverage is a signal, not a target: the library packages sit at 82 to 95% (see the README), and a
  new change should not lower that. A high percentage never replaces a missing edge case.

## The C library is the reference

The format is defined by the [HDF5 File Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html),
and the behaviour that files in the wild depend on is defined by the
[HDF5 C library](https://github.com/HDFGroup/hdf5).

- Check how the C library does a thing before you write it; do not guess a layout, a size or an
  order of fields. Where the specification and the C library differ, follow the specification and
  say so in a comment.
- Cite the C source in the comment that explains format behaviour: the function and the file, for
  example `H5B2__remove_internal`, `H5B2internal.c`, or `H5Ocache.c:1207`. A reader can then check the
  claim, and a later change can tell a deliberate difference from a mistake.
- Where this library deliberately differs from C (for example, refusing a corrupt file the C
  library would read), write the reason next to the code.
- [UPSTREAM.md](UPSTREAM.md) records the C library commit and the format specification version the
  code was last compared against.

## Code

- Follow Go conventions; run `gofmt`. Keep functions small and focused.
- **Bounds first.** Every size, count, offset and address read from a file is untrusted. Check it
  against the file and against a sane limit before you allocate or slice, and return an error that
  wraps `ErrCorrupt` (damage), `ErrNotSupported` (valid HDF5 this library cannot handle) or
  `ErrLimit`. Use the helpers in `internal/utils` and `core.ReadBlock`. A panic or a fatal allocation
  on a crafted file is a security bug.
- Explicit byte order. Use `encoding/binary` with an explicit order, never the host's.
- Wrap errors with context using `%w`, and test them with `errors.Is`. Do not ignore errors, including
  the error from `Close` on a file that was written.
- Public API: exported identifiers have doc comments that start with the name. Changing a signature or
  a documented behaviour needs an issue first.
- Exported symbols are the contract. Anything that does not need to be public goes under `internal/`.

## Documentation

Update the docs in the same pull request as the code:

- **README.md** holds the overview and the status. Do not state a number (test counts, coverage,
  speed) without the command that produces it.
- **CHANGELOG.md** follows [Keep a Changelog](https://keepachangelog.com/): add your entry under
  `[Unreleased]` in **Added**, **Changed**, **Fixed** or **Security**. A change in behaviour that
  existing code can notice needs an **Upgrading:** note that says what to check.
- **docs/guides/** are the user guides (quick start, reading data, datatypes, troubleshooting, FAQ,
  performance); **docs/architecture/** describes how the library is built. If you change what a
  guide says, change the guide. Every code example in the docs must compile.
- **Doc comments** in the code are the API reference on
  [pkg.go.dev](https://pkg.go.dev/github.com/scigolib/hdf5).
- [ROADMAP.md](ROADMAP.md) and [UPSTREAM.md](UPSTREAM.md) are maintained by the maintainers.
- [AGENTS.md](AGENTS.md) and [llms.txt](llms.txt) describe the public API for programs that write
  code against the library; update them when an exported function is added, removed or changed.

## Project layout

```
hdf5/
├── *.go                  public API (package hdf5) and its tests
├── internal/
│   ├── core/             parsing and encoding of HDF5 structures
│   ├── structures/       B-trees, heaps, symbol tables, links
│   ├── writer/           write path, allocator, filters
│   ├── rebalancing/      B-tree rebalancing support
│   ├── testing/          mock readers
│   └── utils/            bounds checks, byte order, errors
├── cmd/dump_hdf5/        hex dumper of HDF5 files
├── examples/             runnable programs
├── testdata/             fixtures
├── docs/                 guides and architecture
└── .github/workflows/    CI
```

[docs/architecture/OVERVIEW.md](docs/architecture/OVERVIEW.md) describes each part in more detail.

## Getting help

- Ask in [discussions](https://github.com/scigolib/hdf5/discussions) or the
  [issue tracker](https://github.com/scigolib/hdf5/issues).
- Read the guides in [docs/guides/](docs/guides/).

## License

By contributing, you agree that your contributions are licensed under the MIT License.
