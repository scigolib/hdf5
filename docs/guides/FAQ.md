# Frequently Asked Questions (FAQ)

Answers to common questions about the HDF5 Go library.

---

## Table of Contents

- [General Questions](#general-questions)
- [Features and Capabilities](#features-and-capabilities)
- [Performance](#performance)
- [Compatibility](#compatibility)
- [Development and Contributing](#development-and-contributing)
- [Roadmap and Future](#roadmap-and-future)

---

## General Questions

### What is this library?

A pure Go implementation of the HDF5 file format for reading and writing HDF5 files. It needs no cgo and no C library, so it cross-compiles like any Go program.

### Why use this instead of existing Go HDF5 libraries?

Advantages:
- Pure Go: no cgo, no C dependencies
- Works on any platform Go supports
- A single binary, with no library to install on the target machine
- Reads and writes files that the C library and h5py read

Trade-offs:
- Virtual datasets, parallel I/O and SWMR are not implemented
- SZIP, N-bit and Scale-offset filters are not supported
- Raw I/O speed has not been compared with the C library in a published benchmark; measure your own workload

### Why pure Go? Why not use cgo?

Pure Go:
1. Cross-compilation for any platform from any platform
2. Users do not need the HDF5 C library installed
3. A single static binary
4. No C memory management at the Go/C boundary
5. A plain `go build`

cgo:
- Requires the HDF5 C library to be installed on the build machine
- Makes cross-compilation harder
- Adds call overhead and a second memory model to debug

### Is it ready for my use?

The supported features, the validation results and the limitations are listed in the README "Current Status" section. Decide from those whether your files and workload are covered. Short version:

Read support:
- All datatypes listed in the [Datatypes Guide](DATATYPES.md)
- Compact, contiguous and chunked layouts
- GZIP, Shuffle, Fletcher32, LZF and BZIP2 filters
- Groups in both formats, hard, soft and external links
- Attributes in compact and dense storage
- Files written by HDF5 before 1.8 and by 1.8 and later

Write support:
- Datasets in all three layouts, all datatypes, resizing with unlimited dimensions
- Variable-length strings and ragged arrays
- Groups, attributes (compact and dense, with modification and deletion)
- GZIP, Shuffle, Fletcher32 and LZF filters
- Hard, soft and external links
- User blocks, and deletion of objects as `H5Ldelete` does (v0.15.0)

Quality:
- Test coverage: 82-95% per library package
- Lint: 0 issues with the linters in `.golangci.yml`
- Official HDF5 test suite: 379 of 383 valid files
- Files written by this library are read back by the C library's `h5dump` in CI

### Who is this library for?

Suited for:
- Reading and writing HDF5 data from Go programs (astronomy, climate, physics, instrument data)
- Data analysis tools and services that must not depend on the HDF5 C library
- Docker images and cross-compiled binaries that should contain only the program

Not suited for:
- Applications that need virtual datasets, parallel I/O or SWMR
- Datasets that use SZIP, N-bit, Scale-offset or third-party filters

---

## Features and Capabilities

### Can I read dataset values?

Yes:

- Numeric types (integers and floats of every width): `Read` returns `[]float64`
- Strings, fixed and variable-length: `ReadStrings` returns `[]string`
- Compound types: `ReadCompound` returns one map per element

```go
// Numeric datasets
data, err := ds.Read() // []float64

// String datasets
strings, err := ds.ReadStrings() // []string

// Compound datasets
compounds, err := ds.ReadCompound() // one map from member name to value per element
```

See [Reading Data Guide](READING_DATA.md) for details.

### Can I write HDF5 files?

Yes. A short example (error handling shortened; see the [Quick Start](QUICKSTART.md) for a complete program):

```go
// Create new HDF5 file
fw, err := hdf5.CreateForWrite("output.h5", hdf5.CreateTruncate)
if err != nil {
    log.Fatal(err)
}
defer fw.Close()

// Create groups
if _, err := fw.CreateGroup("/experiments"); err != nil {
    log.Fatal(err)
}

// Write datasets
ds, err := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
if err != nil {
    log.Fatal(err)
}
if err := ds.Write(myFloat64Data); err != nil { // myFloat64Data is a []float64 of 100 values
    log.Fatal(err)
}

// Array of 3x3 float32 elements
if _, err := fw.CreateDataset("/arrays", hdf5.ArrayFloat32, []uint64{10},
    hdf5.WithArrayDims([]uint64{3, 3})); err != nil {
    log.Fatal(err)
}

// Enumeration
if _, err := fw.CreateDataset("/status", hdf5.EnumInt8, []uint64{5},
    hdf5.WithEnumValues([]string{"OK", "ERROR"}, []int64{0, 1})); err != nil {
    log.Fatal(err)
}

// Delete a group and everything only it links to (as H5Ldelete does)
if err := fw.Delete("/experiments"); err != nil {
    log.Fatal(err)
}
```

A file can start with a user block for the application's own header, as `H5Pset_userblock` makes it
in the C library (a MATLAB v7.3 file keeps its MAT header there):

```go
fw, err := hdf5.CreateForWrite("data.mat", hdf5.CreateTruncate, hdf5.WithUserBlock(512))
if err != nil {
    log.Fatal(err)
}
defer fw.Close()
if err := fw.WriteUserBlock(header); err != nil { // up to 512 bytes; the rest stays zero
    log.Fatal(err)
}
```

`OpenForWrite` keeps the user block of an existing file untouched.

Limitations: the filters listed under "What compression formats work?" below. Official HDF5 test suite: 379 of 383 valid files. Files written by this library are read back by the C library's `h5dump` in CI.

See [ROADMAP.md](../../ROADMAP.md) for future plans.

### Can I read attributes?

Yes, including variable-length strings:

```go
// Group attributes
attrs, err := group.Attributes()

// Dataset attributes
attrs, err = dataset.Attributes()

// One attribute by name
units, err := dataset.ReadAttribute("units")

// Access attribute values
for _, attr := range attrs {
    value, err := attr.ReadValue()
    if err != nil {
        log.Printf("Error reading %s: %v", attr.Name, err)
        continue
    }
    fmt.Printf("%s = %v (type: %T)\n", attr.Name, value, value)
}
```

Supported: compact attributes (in the object header) and dense attributes (fractal heap and version 2 B-tree), with every datatype, including variable-length strings.

### What datatypes are supported?

Read and write:

| HDF5 Type | Go Type | Read | Write |
|-----------|---------|------|-------|
| H5T_INTEGER | int8-64, uint8-64 | yes | yes |
| H5T_FLOAT | float32, float64 | yes | yes |
| H5T_STRING | string | yes | yes |
| H5T_ARRAY | fixed arrays | yes | yes |
| H5T_ENUM | named integers | yes | yes |
| H5T_REFERENCE | object/region refs | yes | yes |
| H5T_OPAQUE | binary blobs | yes | yes |
| H5T_COMPOUND | struct-like | yes | yes |
| H5T_VLEN | variable-length | yes | yes |

Not supported: H5T_TIME.

See [Datatypes Guide](DATATYPES.md) for detailed type mapping.

### What compression formats work?

Supported:
- GZIP/Deflate (filter ID 1), read and write
- Shuffle (2) and Fletcher32 (3, verified on read), read and write
- LZF (32000), read and write; BZIP2 (307), read only

Not supported:
- SZIP (filter ID 4, needs libaec)
- N-bit (filter ID 5)
- Scale-offset (filter ID 6)
- Third-party filters such as Blosc, LZ4 and Zstd

A dataset that uses an unsupported filter fails with an error that wraps `hdf5.ErrNotSupported`
(`errors.Is(err, hdf5.ErrNotSupported)`). Earlier versions returned the undecoded bytes as
values, without an error.

Workaround: convert the file with the HDF5 tools:

```bash
h5repack -f GZIP=6 input.h5 output.h5
```

### What HDF5 versions are supported?

Superblock versions:
- Version 0 (HDF5 1.0 - 1.6)
- Version 1: not supported
- Version 2 (HDF5 1.8 and later)
- Version 3 (HDF5 2.0.0, with checksums)

Object header versions 1 (before HDF5 1.8) and 2 (1.8 and later).

File formats: groups as symbol tables and as link messages or dense link storage, and both version 1 and version 2 B-trees.

### Does it support large files?

Yes, with some considerations.

File size: the file is read through `io.ReaderAt` and is not loaded whole. Memory use grows with the number of objects you open, and a dataset is read into memory by `Read`.

Large datasets:
- A chunked dataset of any size can be processed chunk by chunk with `ChunkIterator`, or in parts with `ReadSlice` and `ReadHyperslab`
- `Read()` loads the whole dataset into memory; see "Can I stream large datasets?" below

Practices:
- Process datasets one at a time
- Close files promptly

```go
// Process each dataset as it is found, so it can be garbage collected.
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, err := ds.Read()
        if err != nil {
            log.Printf("%s: %v", path, err)
            return
        }
        processData(data) // processData is your own function
    }
})
```

---

## Performance

### How fast is it compared to the C library?

There is no published comparison. Speed depends on the operation, the dataset size, the layout and the compression, so measure your own workload. The benchmarks in the repository are `release_bench_test.go` and `btree_rebalancing_bench_test.go`:

```bash
go test -run XXX -bench . -benchmem ./...
```

### Is it thread-safe?

Separate `File` values are independent. One `File` used from several goroutines needs the caller's synchronization. There is no SWMR mode and no parallel I/O.

A pattern that needs no locking: open a separate handle per goroutine.

```go
// Each goroutine opens its own handle
func processDataset(filename string, datasetPath string) error {
    file, err := hdf5.Open(filename)
    if err != nil {
        return err
    }
    defer file.Close()

    // Find and process the dataset
    // ...
    return nil
}

// Concurrent processing
var wg sync.WaitGroup
for _, dsPath := range datasetPaths {
    wg.Add(1)
    go func(path string) {
        defer wg.Done()
        if err := processDataset("data.h5", path); err != nil {
            log.Printf("%s: %v", path, err)
        }
    }(dsPath)
}
wg.Wait()
```

### Can I stream large datasets?

Yes, for chunked datasets: `Read()` loads the whole dataset into memory, but
`Dataset.ReadSlice`, `Dataset.ReadHyperslab` and `Dataset.ChunkIterator` read parts of it.
The iterator reads one chunk at a time and follows the `bufio.Scanner` pattern:

```go
iter, err := ds.ChunkIterator() // chunked datasets only
if err != nil {
    return err
}
for iter.Next() {
    chunk, err := iter.Chunk()
    if err != nil {
        return err
    }
    processChunk(chunk)
}
if err := iter.Err(); err != nil {
    return err
}
```

`ChunkIteratorWithContext` also stops when its context is cancelled, inside the index walk too.

To decide whether a whole-dataset read fits in memory, use `Shape()` and `Datatype()` to check
the size before reading:

```go
shape, err := ds.Shape()
if err != nil {
    return err
}
dt, err := ds.Datatype()
if err != nil {
    return err
}

elements := uint64(1)
for _, n := range shape {
    elements *= n
}
storedSize := elements * dt.Size // Bytes of the elements as stored in the file.
resultSize := elements * 8       // Read() returns []float64: 8 bytes per element.

// Only read if size is acceptable
if max(storedSize, resultSize) < threshold {
    data, _ := ds.Read()
    processData(data)
}
```

The result in memory can be larger than the stored data: `Read()` converts every
element to a `float64`, so a `uint8` dataset takes eight times its stored size, and
`ReadCompound()` builds a map per record. Reads of data that the file does not bound
(chunked datasets, unallocated or external storage) are also capped by `File.SetReadLimit`
(4 GiB by default); a read over the limit fails with an error that wraps `hdf5.ErrLimit`
(see [Reading Data](READING_DATA.md#read-limit)).

---

## Compatibility

### Which operating systems are supported?

Any platform Go supports. CI runs the tests on Linux, macOS and Windows, and cross-builds for linux/386, linux/arm and windows/386. Other platforms need only a Go toolchain; they are not tested in CI.

### Can I use it with Docker?

Yes. The library has no C dependencies, so the image needs no HDF5 packages.

Minimal Dockerfile:
```dockerfile
FROM golang:1.25-alpine

WORKDIR /app
COPY . .

RUN go build -o myapp .

CMD ["./myapp"]
```

The module that imports the library lists it in `go.mod` (`go get github.com/scigolib/hdf5`). The image needs no HDF5 C library.

### Does it work with Python h5py-created files?

Yes. Files written by h5py are read by this library, and files written by this library are read by h5py.

Example:
```python
# Create file with Python
import h5py
import numpy as np

with h5py.File('data.h5', 'w') as f:
    f.create_dataset('numbers', data=np.arange(100))
    f.create_dataset('strings', data=['hello', 'world'])
```

```go
// Read with Go
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    fmt.Println(path)
})
```

### Can I read files created by MATLAB, IDL, or other tools?

Usually, if they follow the HDF5 file format. MATLAB v7.3 files have a user block (the 128-byte MAT header) in front of the HDF5 data; the library reads such files, and since v0.15.0 it also writes and modifies files with a user block.

MATLAB example:
```matlab
% Save as HDF5 format
data = rand(100, 100);
save('data.mat', 'data', '-v7.3');  % -v7.3 uses HDF5
```

```go
// Read with Go library
file, err := hdf5.Open("data.mat") // a MATLAB v7.3 .mat file is an HDF5 file
if err != nil {
    log.Fatal(err)
}
defer file.Close()
```

Some tools add their own metadata that this library does not interpret; the datasets are read as stored.

### Can I read NetCDF4 files?

Partially. NetCDF4 is built on HDF5, so the groups, datasets and attributes can be read:

```go
// NetCDF4 files have .nc extension but use HDF5 format
file, err := hdf5.Open("climate.nc")
if err == nil {
    // Can read datasets
    // NetCDF metadata in attributes
}
```

Limitations: NetCDF-specific conventions (dimensions, coordinate variables) are not interpreted; you see the raw HDF5 structure.

---

## Development and Contributing

### How can I contribute?

Bug reports with a sample file, fixes, documentation, and files from your own field that the library should read.

1. Read [CONTRIBUTING.md](../../CONTRIBUTING.md)
2. Check the [open issues](https://github.com/scigolib/hdf5/issues)
3. Ask in the [discussions](https://github.com/scigolib/hdf5/discussions)

### What's the development workflow?

GitHub Flow, described in [CONTRIBUTING.md](../../CONTRIBUTING.md): a branch from `main`, a pull request to `main`.

1. Fork the repository and create a branch from `main`
2. Implement the change with tests
3. Run `golangci-lint run ./...` and `go test ./...`
4. Open a pull request to `main`

### What's the testing strategy?

Test coverage is 82-95% per library package.

Kinds of tests:
1. Unit tests of individual functions
2. Integration tests with real HDF5 files (`testdata/reference`)
3. Round trips: write, read back, compare
4. Interoperability: files written by the library are read by `h5dump` (CI job `c-interop`)
5. Fuzz tests of the parsers, run in CI

Quality checks: golangci-lint, the race detector (`go test -race`), and tests on Windows, Linux and macOS.

### How is the library documented?

1. User guides: `docs/guides/`
2. Architecture: `docs/architecture/`
3. API reference: [pkg.go.dev](https://pkg.go.dev/github.com/scigolib/hdf5)
4. Examples: `examples/`

---

## Roadmap and Future

### What's the current write support status?

See the README "Current Status" section. In short: file creation with superblock versions 0, 2 and 3; datasets in all layouts; resizing; variable-length types; GZIP, Shuffle, Fletcher32 and LZF filters; groups in both formats; attributes in compact and dense storage with modification and deletion; hard, soft and external links; user blocks and recursive group deletion (v0.15.0).

See [ROADMAP.md](../../ROADMAP.md) for what comes next.

### What features are not implemented?

- SWMR (single writer, multiple reader) and parallel I/O
- Virtual datasets
- SZIP, N-bit and Scale-offset filters

### Will v1.0.0 break the API?

The reading API is meant to stay stable, with additions rather than changes, and deprecations announced in advance. v1.0.0 has no date; see [ROADMAP.md](../../ROADMAP.md) for the versioning strategy.

### Is commercial support available?

No. Support is through GitHub issues and discussions.

### How can I stay updated?

- Watch the releases on https://github.com/scigolib/hdf5
- Read [CHANGELOG.md](../../CHANGELOG.md)
- Issues: https://github.com/scigolib/hdf5/issues
- Discussions: https://github.com/scigolib/hdf5/discussions

---

## Still Have Questions?

- [Installation Guide](INSTALLATION.md): setup and verification
- [Quick Start Guide](QUICKSTART.md): first programs
- [Reading Data Guide](READING_DATA.md): reading in detail
- [Datatypes Guide](DATATYPES.md): type conversion details
- [Troubleshooting](TROUBLESHOOTING.md): common issues
- [ROADMAP](../../ROADMAP.md): future plans
- Documentation index: https://github.com/scigolib/hdf5/tree/main/docs

---

*Last Updated: 2026-11-07*
