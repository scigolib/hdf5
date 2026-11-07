# HDF5 Go Library

A pure Go implementation of the HDF5 file format: read and write HDF5 files without the C library or cgo.

[![Release](https://img.shields.io/github/v/release/scigolib/hdf5?include_prereleases&style=flat-square&logo=github&color=blue&label=version)](https://github.com/scigolib/hdf5/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/scigolib/hdf5?style=flat-square&logo=go)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/scigolib/hdf5?style=flat-square)](https://goreportcard.com/report/github.com/scigolib/hdf5)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue?style=flat-square&logo=go)](https://pkg.go.dev/github.com/scigolib/hdf5)
[![CI](https://img.shields.io/github/actions/workflow/status/scigolib/hdf5/test.yml?branch=main&style=flat-square&logo=github&label=tests)](https://github.com/scigolib/hdf5/actions)
[![codecov](https://codecov.io/gh/scigolib/hdf5/graph/badge.svg)](https://codecov.io/gh/scigolib/hdf5)
[![License](https://img.shields.io/github/license/scigolib/hdf5?style=flat-square&color=blue)](https://github.com/scigolib/hdf5/blob/main/LICENSE)
[![Stars](https://img.shields.io/github/stars/scigolib/hdf5?style=flat-square&logo=github)](https://github.com/scigolib/hdf5/stargazers)
[![Discussions](https://img.shields.io/github/discussions/scigolib/hdf5?style=flat-square&logo=github&label=discussions)](https://github.com/scigolib/hdf5/discussions)

HDF5 is the file format behind much of scientific data: h5py and PyTables output, MATLAB v7.3 `.mat`
files, NetCDF-4, instrument and simulation data. This library lets a Go program read such files and
write new ones that the HDF5 C library and the tools built on it (h5dump, h5py) read back. A Go binary
built with it needs no C toolchain and no HDF5 installation, on any platform Go supports.

---

## Features

- **Pure Go**: no cgo and no C dependencies; cross-compiles like any Go program.
- **HDF5 2.0.0 file format**: superblock versions 0, 2 and 3 with checksum validation, read and write.
- **Datasets**: compact, contiguous and chunked layouts; GZIP, Shuffle, Fletcher32 and LZF filters;
  partial reads by hyperslab and chunk-by-chunk iteration for data larger than memory.
- **Datatypes**: integers, floating point (including FP8 and bfloat16), fixed and variable-length
  strings, arrays, enums, compounds, references, opaque.
- **Groups, attributes and links**: both group formats, compact and dense attribute storage, hard,
  soft and external links.
- **Modifying existing files**: add, change and delete datasets, groups and attributes in place.
- **Checked against the C library**: the official HDF5 test files are read in CI, and files this
  library writes are read back by `h5dump`.
- **Hardened for untrusted files**: sizes and addresses read from a file are checked against the file
  before memory is allocated; the memory one read may use is capped (`File.SetReadLimit`).

---

## Quick Start

### Installation

```bash
go get github.com/scigolib/hdf5
```

### Read a file

```go
package main

import (
    "fmt"
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    file, err := hdf5.Open("data.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // List every group and dataset, and read the datasets' values.
    file.Walk(func(path string, obj hdf5.Object) {
        switch v := obj.(type) {
        case *hdf5.Group:
            fmt.Printf("group   %s (%d children)\n", path, len(v.Children()))
        case *hdf5.Dataset:
            values, err := v.Read() // numeric data as []float64
            if err != nil {
                log.Printf("%s: %v", path, err)
                return
            }
            units, _ := v.ReadAttribute("units") // nil error only if the attribute exists
            fmt.Printf("dataset %s: %d values, units %v\n", path, len(values), units)
        }
    })
}
```

### Write a file

```go
fw, err := hdf5.CreateForWrite("results.h5", hdf5.CreateTruncate)
if err != nil {
    log.Fatal(err)
}
defer fw.Close()

if _, err := fw.CreateGroup("/run1"); err != nil {
    log.Fatal(err)
}
ds, err := fw.CreateDataset("/run1/temperature", hdf5.Float64, []uint64{4})
if err != nil {
    log.Fatal(err)
}
if err := ds.Write([]float64{293.1, 293.4, 293.9, 294.2}); err != nil {
    log.Fatal(err)
}
if err := ds.WriteAttribute("units", "K"); err != nil {
    log.Fatal(err)
}
```

`hdf5.OpenForWrite` opens an existing file to add, change or delete objects. More in
[examples](examples/) and the [Quick Start guide](docs/guides/QUICKSTART.md).

---

## Documentation

### Getting started
- [Installation](docs/guides/INSTALLATION.md): install and verify the library
- [Quick Start](docs/guides/QUICKSTART.md): first programs that read and write files
- [Reading Data](docs/guides/READING_DATA.md): datasets, attributes, partial reads

### Reference
- [Datatypes](docs/guides/DATATYPES.md): how HDF5 types map to Go types
- [Troubleshooting](docs/guides/TROUBLESHOOTING.md): common errors and what they mean
- [FAQ](docs/guides/FAQ.md)
- [API reference](https://pkg.go.dev/github.com/scigolib/hdf5) on pkg.go.dev

### Advanced
- [Architecture overview](docs/architecture/OVERVIEW.md): how the library is built
- [Performance tuning](docs/guides/PERFORMANCE_TUNING.md): B-tree rebalancing strategies
- [Rebalancing API](docs/guides/REBALANCING_API.md): the rebalancing options in detail
- [Examples](examples/): seven runnable programs

---

## Performance Tuning

When many attributes are deleted, the B-trees that index them can become sparse: space is wasted and
lookups slow down. The library offers four rebalancing strategies.

### 1. Immediate (default) and none

By default every delete rebalances, as in the C library. `WithBTreeRebalancing(false)` skips
rebalancing: deletes are faster, but the B-tree may become sparse.

The HDF5 C library merges and redistributes version 2 B-tree nodes on every delete
(`H5B2__remove_internal`, `H5B2internal.c`); only its version 1 B-tree never rebalances (`H5B.c`).
The strategies below act on the version 2 B-trees of dense attribute storage. With no options,
this library does the same as C and rebalances on every delete; the options choose when
rebalancing happens, or turn it off.

```go
// No options = rebalance on every delete (the C library's behavior for version 2 B-trees)
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)

// No rebalancing at all
fw, err = hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithBTreeRebalancing(false),
)
```

Use "none" for append-only workloads and small files.

### 2. Lazy rebalancing

Rebalances in batches, once a threshold is reached.

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(
        hdf5.LazyThreshold(0.05),         // Trigger at 5% underflow
        hdf5.LazyMaxDelay(5*time.Minute), // Force rebalance after 5 min
    ),
)
```

Use it for batch deletions. Overhead is low, with occasional pauses while a batch is rebalanced.

### 3. Incremental rebalancing

Rebalances in a background goroutine within a time budget.

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithLazyRebalancing(), // required by incremental mode
    hdf5.WithIncrementalRebalancing(
        hdf5.IncrementalBudget(100*time.Millisecond),
        hdf5.IncrementalInterval(5*time.Second),
    ),
)
defer fw.Close() // stops the background goroutine
```

Use it for large files and long-running processes that delete continuously.

### 4. Smart rebalancing

Watches the workload and chooses one of the modes above.

```go
fw, err := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate,
    hdf5.WithSmartRebalancing(
        hdf5.SmartAutoDetect(true),
        hdf5.SmartAutoSwitch(true),
    ),
)
```

Use it when the workload is not known in advance.

### Comparison

| Mode | Deletion cost | Pauses | Use case |
|------|---------------|--------|----------|
| None (`WithBTreeRebalancing(false)`) | lowest (no rebalancing) | none | append-only, small files |
| Immediate (default) | highest for batch deletions | none | few deletions |
| Lazy | low, batched | short, per batch | batch deletions |
| Incremental | low, in the background | none | large files, continuous deletes |
| Smart | that of the chosen mode | varies | unknown workloads |

Measure your own workload with the benchmarks in `btree_rebalancing_bench_test.go`. Details: the
[performance tuning guide](docs/guides/PERFORMANCE_TUNING.md), the
[rebalancing API reference](docs/guides/REBALANCING_API.md) and the
[rebalancing examples](examples/07-rebalancing/).

---

## Current Status

v0.15.0. Library packages have 82–95% test coverage.

### Reading
- **File structure**:
  - Superblocks v0, v2 and v3 with checksum validation (CRC32)
  - Object headers v1 (HDF5 before 1.8) and v2 (HDF5 1.8 and later), with continuation chunks
  - Groups in both formats: symbol tables, and link messages or dense link storage
  - Version 1 and version 2 B-trees, local heaps, the global heap, fractal heaps
  - Files with a user block in front of the HDF5 data (e.g. MATLAB v7.3)

- **Datasets**:
  - Compact, contiguous and chunked layouts (every chunk index type)
  - External raw data files, opt-in with `File.SetExternalFilePrefix`
  - Filters: GZIP (deflate), Shuffle, Fletcher32, LZF (h5py and PyTables), BZIP2
  - Partial reads: hyperslab selection with stride and block; only the chunks a selection needs are read
  - `ChunkIterator` for datasets larger than memory

- **Datatypes** (read and write):
  - Integers (8 to 64 bits, signed and unsigned), float32 and float64
  - FP8 (E4M3, E5M2) and bfloat16
  - Strings: fixed-length (null-terminated, null-padded, space-padded) and variable-length
  - Arrays, enums, compounds (nested), object and region references, opaque

- **Attributes**: compact and dense storage, on groups and datasets (`Attributes`, `ReadAttribute`)

- **Navigation**: the whole tree with `File.Walk`

- **Errors**: callers can tell damage from unsupported features with `errors.Is`: `ErrCorrupt`,
  `ErrNotSupported`, `ErrLimit`, `ErrNoData`, `ErrNeedsRepack`; `File.Warnings` reports what the reader
  tolerated

- **Security**: sizes, counts and addresses read from the file are checked against the file before
  memory is allocated, and one read is capped by `File.SetReadLimit`. Reported vulnerabilities and
  their fixes are published as [security advisories](https://github.com/scigolib/hdf5/security/advisories);
  see [SECURITY.md](SECURITY.md) to report one

### Writing

**Datasets and attributes**:
- Create datasets in every layout (contiguous, chunked, compact) and write every datatype above
- Resize datasets with unlimited dimensions
- Variable-length strings and ragged arrays
- Filters: GZIP, Shuffle, Fletcher32, LZF
- Write, modify and delete attributes (compact and dense storage)
- Delete objects as `H5Ldelete` does: a group goes with everything only it links to, and an object
  also linked from elsewhere keeps its data (v0.15.0)

**File layout**:
- Superblock version 0 (most compatible), 2 (default) or 3 (`WithSuperblockVersion`)
- User blocks (`WithUserBlock`, `WriteUserBlock`), as `H5Pset_userblock` writes them: room for the
  application's own header in front of the HDF5 data, such as the MAT header of a MATLAB v7.3 file
  (v0.15.0)
- `OpenForWrite` modifies existing files, including files with a user block, and never touches the
  block (v0.15.0)

**Links**:
- Hard, soft and external links
- User-defined links are read and kept unchanged when a file is modified

### Validation

- Official HDF5 test suite: 379 of 383 valid files (the other 4 need the family or multi file driver, or
  are truncated and refused by the C library as well; 50 files of the suite are deliberately invalid or need
  unsupported drivers and are skipped)
- Files written by this library are read back by the C library's `h5dump` in CI (`c-interop` job)

### Limitations

- Filters without a decoder here: SZIP (4, needs libaec), N-bit (5), Scale-offset (6) and third-party
  filters. A dataset that uses one fails with an error that wraps `ErrNotSupported`; earlier versions
  returned the undecoded bytes as values
- A file whose groups form a hard-link cycle (a link back to an ancestor group) is refused by `Open`;
  planned for v0.15.x
- No SWMR (single writer, multiple readers) mode and no parallel I/O. Separate `File` values are
  independent; one `File` used from several goroutines needs the caller's synchronization

See [ROADMAP.md](ROADMAP.md) for what comes next.

---

## Development

### Requirements
- Go 1.25 or later
- The library has no dependencies outside the Go standard library; tests use testify, and the C
  interoperability tests use the HDF5 command-line tools when they are installed

### Building and testing

```bash
git clone https://github.com/scigolib/hdf5.git
cd hdf5

go build ./...                # library, examples and tools
go test ./...                 # all tests
go test -race ./...           # race detector (needs cgo, e.g. on Linux or WSL)
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Contributing

Contributions are welcome: bug reports with a sample file, fixes, documentation, and files from your
own field that the library should read. Before a pull request:

1. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and the code standards
2. Check the [open issues](https://github.com/scigolib/hdf5/issues)
3. Look at the [architecture overview](docs/architecture/OVERVIEW.md)

---

## Other Go HDF5 Libraries

| | This library | [gonum/hdf5](https://github.com/gonum/hdf5) | [go-hdf5/hdf5](https://github.com/go-hdf5/hdf5) |
|---|---|---|---|
| Implementation | pure Go, read and write | cgo bindings to the HDF5 C library | pure Go |
| Needs the C library installed | no | yes | no |
| Repository | active | last change July 2021 | archived (last change 2018) |

---

## HDF5 Resources

- [HDF5 File Format Specification](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
- [HDF5 C library](https://github.com/HDFGroup/hdf5)
- [The HDF Group](https://www.hdfgroup.org/)

---

## License

MIT License; see [LICENSE](LICENSE).

---

## Acknowledgments

- The HDF Group, for the HDF5 format, its specification and the C library this implementation is
  checked against
- gonum/hdf5, which came first
- Everyone who reported issues and contributed code

**Professor Ancha Baranova**: this project would not have been possible without her help and support.

---

## Support

- [Documentation](docs/): guides and architecture
- [Issue tracker](https://github.com/scigolib/hdf5/issues): bugs and feature requests
- [Discussions](https://github.com/scigolib/hdf5/discussions): questions and announcements
- [HDF Group forum thread](https://forum.hdfgroup.org/t/pure-go-hdf5-library-production-release-with-hdf5-2-0-0-compatibility/13584)
  about this library

---

## Star History

<a href="https://starhistory.io">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.starhistory.io/png?repos=scigolib/hdf5&style=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.starhistory.io/png?repos=scigolib/hdf5&style=professional" />
   <img alt="Star History Chart" src="https://api.starhistory.io/png?repos=scigolib/hdf5" width="800" />
 </picture>
</a>
