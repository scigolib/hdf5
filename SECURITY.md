# Security Policy

## Supported Versions

Fixes go into the latest release only. The library is pre-1.0, and older versions are not patched.

| Version | Supported |
| ------- | --------- |
| 0.15.x (latest: v0.15.0) | yes |
| earlier than 0.15 | no, upgrade to the latest release |

Versions v0.9.0-beta through v0.13.9 are retracted in `go.mod`: they wrote files that no HDF5 tool
can open. Do not use them.

## Reporting a Vulnerability

Please do not open a public GitHub issue for a security vulnerability.

Report it privately through GitHub:
https://github.com/scigolib/hdf5/security/advisories/new

The report goes to the maintainers only. Include:

- what the problem is and what it allows (a crash, a hang, unbounded memory use, wrong data, a file
  that is read outside its bounds);
- a file that triggers it, or the steps to build one; a small file is best;
- the versions you tested;
- a fix, if you have one.

We acknowledge a report within a few days, tell you what we found, and agree a disclosure date with
you. A fix is released first and the advisory published after it, with credit to the reporter unless
you prefer not to be named.

Bugs that are not security problems (a file that fails to open, wrong values) belong in the
[issue tracker](https://github.com/scigolib/hdf5/issues).

## Threat Model

An HDF5 file is a graph of structures that point at each other, and every size, count and address in
it is data the file's author chose. A hostile or damaged file can claim a result of any size, point a
structure at itself, or declare more entries than it holds. Go cannot recover from a failed
allocation: the process ends. So the library treats the file as untrusted input and aims for one
outcome on any input: an error, never a panic, a hang or a fatal allocation.

## How the Library Handles Untrusted Files

These are the checks in the code today.

- **Sizes and addresses are checked against the file** before memory is allocated. Metadata blocks
  (object header chunks, heaps, global heap collections, chunks) are limited to 1 GiB, an attribute
  to 64 MiB and a string element to 16 MiB (`internal/utils/overflow.go`). A larger claim, or one
  that reaches past the end of the file, is an error, not an allocation.
- **Reads are capped.** Some results are not bounded by the file: a chunked dataset can be larger than
  its file, because chunks never written read as the fill value. The memory one dataset read may
  allocate for such data is capped at 4 GiB (512 MiB on 32-bit platforms) by default. Change it with
  `File.SetReadLimit`; a read over the cap fails with an error that wraps `ErrLimit`. The same cap
  covers the shared-message metadata kept per file. `File.ReadLimit` returns the current value.
- **Decompression is bounded by the chunk.** A filter pipeline may not produce more bytes than the
  chunk size the dataset layout declares, so a small compressed chunk cannot expand without limit.
  Output past the bound is an error that wraps `ErrCorrupt`.
- **Hyperslab selections are bounded.** A selection of more than 1 billion elements is refused;
  `ChunkIterator` reads a large dataset one chunk at a time.
- **Cycles are refused.** A group that links back to an ancestor, or a structure that points at
  itself, makes `Open` fail with an error instead of recursing without end. Groups nested deeper
  than 4096 levels are refused when a file is opened for writing.
- **External raw data files are off by default.** A dataset can name files outside the HDF5 file as
  its storage. Reading them is refused unless the program calls `File.SetExternalFilePrefix`, and
  then only names that stay inside that directory are opened.
- **Unsupported is told from damaged.** Valid HDF5 that the library cannot read (for example the SZIP
  filter) wraps `ErrNotSupported`; invalid metadata wraps `ErrCorrupt`. `File.Warnings` lists what
  the reader tolerated.

Reading never writes to the file and never runs code from it.

### What the library does not do

- It does not limit the file size or the time a read takes. A file can be large, and a read can be
  slow. Programs that accept uploads should check the size before `Open` and run the read under a
  deadline or in a separate process.
- It does not validate the meaning of names. Group and dataset names are arbitrary strings from the
  file (they may contain `..` or `/`). The library never uses them as file system paths, apart from
  the opt-in external storage above. A program that does must sanitize them itself.
- One `File` is not safe for use from several goroutines without the caller's synchronization.

### Recommended use with untrusted files

```go
file, err := hdf5.Open(path)
if err != nil {
    // errors.Is(err, hdf5.ErrCorrupt): damaged or hostile file
    // errors.Is(err, hdf5.ErrNotSupported): valid, but not readable here
    return err
}
defer file.Close()

file.SetReadLimit(256 << 20) // at most 256 MiB for one dataset read
```

Check every error. A parsing failure on a file from an unknown source is the expected result of a
hostile file, not a reason to retry.

## Testing

- **Fuzzing.** Go native fuzz targets exist for the file reader (`FuzzOpenFile`) and for the parsers
  of datatype, dataspace, attribute, fill value and shared messages, chunk indexes, B-trees, global
  and fractal heaps, external file lists and the Fletcher32 filter (`go test -run=^$ -fuzz=FuzzX`;
  list them with `grep -rn "func Fuzz" --include=*_test.go .`). CI runs two of them for 60 seconds on
  every push (`FuzzOpenFile`, `FuzzParseAttributeMessage`), as a job that does not block merging.
  Crashers found by fuzzing are kept as regression inputs under `testdata/fuzz`.
- **Corrupted files.** The test suite includes corruption corpora and per-structure tests that damage
  one field at a time (sizes, counts, addresses, checksums) and expect an error.
- **Official test files.** The HDF Group's test files are read in CI.
- **Interoperability.** Files this library writes are read back by the C library's `h5dump` in CI
  (the `c-interop` job).
- **Static analysis.** `golangci-lint` runs on every push with the configuration in `.golangci.yml`,
  which includes `gosec`, `errcheck` and `errorlint`.

`govulncheck` and automated dependency updates are not part of CI today. The library has no
dependencies outside the Go standard library; `github.com/stretchr/testify` is used by tests only.

## Disclosure History

Published security advisories are listed at
https://github.com/scigolib/hdf5/security/advisories. Fixes are also recorded under **Security** in
[CHANGELOG.md](CHANGELOG.md).

| Advisory | Severity | Summary | Affected | Fixed in | Published |
| -------- | -------- | ------- | -------- | -------- | --------- |
| [GHSA-8p88-h28q-w6m9](https://github.com/scigolib/hdf5/security/advisories/GHSA-8p88-h28q-w6m9) | high | A corrupt HDF5 file can request a multi-terabyte allocation and abort the Go process | before 0.14.2 | 0.14.2 | 2026-10-21 |
| [GHSA-8p77-6v32-rwf3](https://github.com/scigolib/hdf5/security/advisories/GHSA-8p77-6v32-rwf3) | high | Crafted HDF5 files can exhaust memory or hang the reading process (incomplete fix for GHSA-8p88-h28q-w6m9) | before 0.14.3 | 0.14.3 | 2026-11-07 |

Neither advisory has a CVE identifier.

## Bug Bounty

There is no bug bounty program. A reporter is credited in the advisory and in the CHANGELOG entry for
the fix.
