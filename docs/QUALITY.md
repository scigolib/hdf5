# Engineering standards

The bar this library is held to, and how each part of it is checked. It is written down so that a user
can judge what "tested" and "reviewed" mean here, and so that a contributor knows what a change has to
pass before it merges.

The library parses and writes binary files that may come from anywhere. That sets the priorities:
**security > working > documented > clean code > performance**.

## What "correct" means

- The [HDF5 File Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
  decides what a file must contain. Where the specification leaves room, the
  [HDF5 C library](https://github.com/HDFGroup/hdf5) decides how files in the wild behave. A deliberate
  difference from the C library is commented next to the code and explained.
- Files this library writes must open in the C tools. A change to the write path adds a
  `TestCInterop*` test that runs `h5dump` or `h5ls` on the result and compares it. A file only this
  library can read is not a success.
- The HDF Group's own test files are part of the suite: 379 of the 383 valid files pass; the other four
  need the family or multi-file driver (see the README).
- Round trips: write, read back and compare, including empty datasets, a single element, chunk edges and
  the extremes of each type.

## Code

- `gofmt -l .` prints nothing, `go vet ./...` is clean, and `golangci-lint run ./...` reports 0 issues
  under the repository's configuration: complexity, bug detection (`staticcheck`, `gosec`, `errcheck`,
  `errorlint`, `nilerr` and others), style and performance linters.
- No swallowed errors. Every error is handled, returned with `%w`, or logged with a reason; an ignore
  that is deliberate is narrow and says why in a comment.
- **Bounds first.** Every size, count, address and offset read from a file is treated as hostile: it is
  checked against the file size and a limit before it becomes an allocation, a slice or a loop bound. A
  panic or a fatal allocation on a crafted file is a security bug.
- Explicit byte order in every binary read and write; never the host's.
- The library itself is pure Go: no cgo and no dependencies outside the standard library. Tests use
  `testify`; the C interoperability tests use the HDF5 command-line tools when they are installed.
- Exported identifiers have doc comments that start with the name. Anything that does not need to be
  public lives under `internal/`.

## Tests

- Every change ships with tests. A bug fix starts with a test that fails for the reason the bug
  describes, and the pull request says how it was seen to fail.
- Tests are never weakened to go green. A test that cannot fail proves nothing.
- Every library package stays above **70% coverage**; it currently sits at 82–95% (README). Coverage is
  a signal, not a target: a missing edge case is not made up for by a percentage.
- The parsers have fuzz targets (`FuzzOpenFile`, `FuzzParseAttributeMessage`); a crasher becomes a
  regression input under `testdata/fuzz` together with the fix.
- Damage and security tests use small inputs that cannot exhaust the memory of the test machine: prove
  the check, not the crash.
- CI runs on Linux, macOS and Windows with Go 1.25 (the `go.mod` minimum), 1.26 and 1.27; with the race
  detector, 32-bit cross-builds, the linter, formatting, the C-interoperability job and 60-second fuzz
  runs. The full job list is in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Architecture

- Layered: public API → object model → core structures → data structures. `internal/` is not part of
  the contract and can change without notice.
- Design decisions are recorded and reviewed before large changes; the public description is
  [docs/architecture/OVERVIEW.md](architecture/OVERVIEW.md).
- No global mutable state and no `init()` doing work. A panic on anything a file can contain is a bug,
  not a failure mode.

## Review and release

- Every pull request needs an approving review from someone other than its author, and all required
  checks must be green. Contributors work from a fork; a review is a real read of the diff, not a
  rubber stamp.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/); `main`'s history is one
  commit per finished change, not a work log.
- `CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/) and is updated in the same
  pull request as the change. Behaviour a user can notice gets an **Upgrading** note that says what to
  check.
- Releases are tagged from `main` after CI on the exact commit is green. Tags are permanent in the Go
  module proxy and cannot be moved.
- Before v1.0 the API can still change, with a CHANGELOG entry and, where possible, a deprecation one
  minor release ahead. [ROADMAP.md](../ROADMAP.md) describes the v1.0 long-term-support release: API
  stability, three or more years of support, strict semantic versioning and security updates.
- Security reports follow [SECURITY.md](../SECURITY.md): a private advisory, a fix, a release, then a
  published advisory.

## How the bar is enforced

| Standard | Checked by |
|---|---|
| Formatting, lint, vet | the `Code Formatting` and `Lint` CI jobs; `go vet` in the Linux unit job |
| Tests, race, three operating systems, three Go versions | the `Unit Tests - <os> - Go <version>` jobs |
| 32-bit portability | the `Cross-Build 32-bit` job |
| Files open in the C tools | the `C Library Interop` job and the `TestCInterop*` tests |
| Parser robustness | the fuzz targets, the corruption tests and the read limit |
| All of the above in one context | the `Required checks` gate, the single check the branch ruleset requires |
| Review | the branch ruleset: one approving review, review threads resolved |
| Release hygiene | the release checklist: CHANGELOG before the tag, CI green on the exact commit |

Numbers here are kept in step with the README and CONTRIBUTING. Where they disagree, the CI
configuration and the README's "Current Status" section are the source of truth.
