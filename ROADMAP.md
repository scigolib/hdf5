# Roadmap

> **Last updated**: 2026-11-14 · **Current release**: v0.15.0 (2026-11-07) · **Next**: the approved
> batch listed under "What's next"

A pure Go implementation of the HDF5 file format — reading and writing, no cgo and no C library.
The HDF5 C library is the reference for format behaviour; the user-facing description is in
[README.md](README.md), the per-release history in [CHANGELOG.md](CHANGELOG.md).

---

## What the library does today

Verified against the code on 2026-11-14.

### Reading

- Superblocks v0, v2 and v3, with checksum validation; object headers v1 and v2 with continuation
  chunks
- Groups in both formats — symbol tables, link messages and dense link storage; B-trees v1 and v2;
  local, global and fractal heaps; files with a user block in front of the HDF5 data
- Datasets in compact, contiguous and chunked layouts, with every chunk index type (v1 B-tree,
  single chunk, fixed array, extensible array, v2 B-tree)
- External raw data files, opt-in with `File.SetExternalFilePrefix`
- Filters: GZIP, Shuffle, Fletcher32, LZF, BZIP2
- Datatypes: integers, floating point (including FP8 and bfloat16), fixed and variable-length
  strings, arrays, enums, compounds, references, opaque
- Attributes in compact and dense storage; hyperslab selection; chunk-by-chunk iteration for data
  larger than memory

### Writing

- Files with superblock version 0, 2 or 3 and an optional user block; `OpenForWrite` modifies
  existing files in place
- Datasets in contiguous and chunked layouts (compact-layout datasets are read, not written), with
  resizing and unlimited dimensions
- The datatypes above on write, including variable-length strings and ragged arrays
- Groups: symbol-table groups and dense groups (`CreateDenseGroup`); hard, soft and external links;
  user-defined links are kept unchanged
- Attributes in compact and dense storage — write, modify, delete; objects deleted as `H5Ldelete`
  deletes them, a group with everything only it links to
- Filters: GZIP, Shuffle, Fletcher32, LZF; BZIP2 is decompression-only
- Smart B-tree rebalancing — none, lazy or incremental — chosen through the writer options

### Known gaps

Each is an explicit error, never silent corruption:

- SZIP, N-bit and scale-offset filters have no decoder; BZIP2 has no encoder
- Virtual datasets; SWMR; parallel I/O; the family and multi file drivers
- Compact-layout datasets are not written
- `Open` refuses a file whose groups form a hard-link cycle
- `ReadCompound` does not yet read a compound with a variable-length member
- A new file's root group gets a one-leaf B-tree where the C library writes an empty one — the
  files are valid, just not byte-identical there

### Validation

- Official HDF5 test suite: 379 of 383 valid files
- Files the writer produces are read back by the C library's `h5dump` in CI (c-interop job); the 386
  cross-build, two fuzz targets and `govulncheck` run on every push
- 82–95% test coverage per library package; `golangci-lint` clean

---

## Release history

| Version | Date | What it brought |
| --- | --- | --- |
| v0.10.0-beta | 2025-10-29 | Read support complete |
| v0.12.0 | 2025-11-13 | Write support: compounds, soft and external links, official-suite validation |
| v0.13.0 | 2025-11-13 | HDF5 2.0.0 formats (superblocks v0, v2, v3), FP8 and bfloat16, security hardening |
| v0.13.1 – v0.13.6 | 2026-01 – 2026-02 | Superblock, attribute, checksum and filter fixes |
| v0.13.7 – v0.13.20 | 2026-02 – 2026-06 | Writer interoperability fixes, each recorded in CHANGELOG.md |
| v0.14.0 | 2026-06-25 | Object deletion, dense links |
| v0.14.1 | 2026-09-02 | Write-compatibility fixes |
| v0.14.2 | 2026-10-21 | Security: metadata-sized allocations bounded |
| v0.14.3 | 2026-11-06 | Reader and writer aligned with the C library; sentinel errors, read limit |
| v0.15.0 | 2026-11-07 | User blocks, recursive group delete, user-defined links in OpenForWrite |

---

## Security

- A file is untrusted input: every size, count and address is checked against the file before it
  becomes an allocation, a slice or a loop bound, and a read can be capped (`File.SetReadLimit`).
- Two advisories are published: GHSA-8p88-h28q-w6m9 (fixed in 0.14.2) and GHSA-8p77-6v32-rwf3
  (0.14.3). Neither has a CVE identifier. How to report: [SECURITY.md](SECURITY.md).

---

## What's next

**The approved batch** — the next release brings the changes reviewed in PRs #87–#93:

- `OpenReader(io.ReaderAt, size)` for reading from memory, a network stream or an archive
- The upstream sync to HDF5 2.2.0: the reference pin, format-spec notes and the checks that follow
  from it
- Stricter validation of variable-length datatypes
- Fixes: truncated Symbol Table messages, `ReadSlice` on a scalar dataspace, unsigned attribute
  types read back signed, and the chunked rewrite that freed old chunks

**Then, by the needs of the users we know of:**

- The chunk B-tree sentinel key must carry the element size — C parity for readers that scan for
  the last chunk (libmysofa does)
- Dimension scales (H5DS) as a public API — required for NetCDF-4 and the CF conventions
- Hard-link cycles: open and walk such a file instead of refusing it
- A new file's root group created the way the C library creates it
- `ReadCompound` with a variable-length member
- HDF5 2.2.0 superblock validation and test fixtures

Out of scope for now: SWMR, parallel I/O, virtual datasets, SZIP.

---

## v1.0.0 LTS

Ships when the public API has been unchanged for a full minor cycle and the gaps above that affect
readers are closed; no date.

- No breaking API change in v1.x
- Semantic versioning strictly followed
- Security fixes and bug fixes for the support window
- All the formats listed above, read and write

---

## How development works

- The C library is the reference for format behaviour; a difference from it is our bug until
  proven otherwise, and a regression test must fail on the code before a fix.
- Files the writer produces are checked with the C library's tools; the official test suite, the
  386 cross-build, the fuzz targets and `govulncheck` run in CI.
- Semantic versioning: fixes are patches, features are minors. A breaking change is a new major.

---

## Links

- [README.md](README.md) — what the library does and how to use it
- [CHANGELOG.md](CHANGELOG.md) — release history
- [CONTRIBUTING.md](CONTRIBUTING.md) — how to contribute
- [SECURITY.md](SECURITY.md) — how to report a vulnerability
