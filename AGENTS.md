# AGENTS.md: scigolib/hdf5

> Pure Go reader and writer for HDF5 files. No cgo, no HDF5 installation. For code that imports
> `github.com/scigolib/hdf5`.

This file is for AI coding agents and tools that write Go code against this library. Every
identifier below was checked with `go doc` at v0.15.0, and the code blocks compile against it.

## What it is and when to use it

HDF5 is a binary container for scientific data: n-dimensional arrays (datasets) in a tree of groups,
with attributes (small named values) on both. It is the format behind h5py and PyTables output,
MATLAB v7.3 `.mat` files, NetCDF-4, and much instrument and simulation data.

Use this library when a Go program must read such a file, or write one that the HDF5 C library and
tools (`h5dump`, `h5py`) read back, and you do not want cgo or a system HDF5. Writing is verified
against `h5dump` in CI.

Do not use it for: SZIP, N-bit or scale-offset compressed datasets (see Limitations), SWMR or
parallel I/O, or the family and multi file drivers.

Import path `github.com/scigolib/hdf5`, package name `hdf5`. Go 1.25 or later.

## Install

```bash
go get github.com/scigolib/hdf5
```

## How the API is shaped

- **Reading**: `hdf5.Open(path)` returns a `*hdf5.File`. There is no lookup by path: `file.Walk` calls
  your function for every group and dataset, with the object's absolute path, and the object is a
  `*hdf5.Group` or a `*hdf5.Dataset`. `file.Root()` and `Group.Children()` walk the tree by hand.
- **Writing**: `hdf5.CreateForWrite(path, hdf5.CreateTruncate)` creates a file and
  `hdf5.OpenForWrite(path, hdf5.OpenReadWrite)` modifies an existing one. Both return a
  `*hdf5.FileWriter`. Objects are named by absolute path (`"/run1/temperature"`).
- **Always** close what you open. Check the error of `FileWriter.Close()`: a written file is only
  complete after it.
- Numeric data read with `Dataset.Read()` is **always `[]float64`**, flattened in row-major (C) order.
  The dataset's `Shape()` gives the dimensions.

## Recipes

Each block below is a complete function that compiles. They assume these imports:

```go
import (
    "errors"
    "fmt"
    "log"

    "github.com/scigolib/hdf5"
)
```

### Open a file and list its objects

```go
func listObjects(path string) error {
    file, err := hdf5.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()

    file.Walk(func(p string, obj hdf5.Object) {
        switch v := obj.(type) {
        case *hdf5.Group:
            fmt.Printf("group   %s (%d children)\n", p, len(v.Children()))
        case *hdf5.Dataset:
            shape, err := v.Shape()
            if err != nil {
                fmt.Printf("dataset %s: %v\n", p, err)
                return
            }
            fmt.Printf("dataset %s shape %v\n", p, shape)
        }
    })
    return nil
}
```

Group paths from `Walk` may end in a slash (`/run1/`); dataset paths do not (`/run1/temperature`).

### Find one dataset by path

```go
func findDataset(file *hdf5.File, path string) *hdf5.Dataset {
    var found *hdf5.Dataset
    file.Walk(func(p string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok && p == path {
            found = ds
        }
    })
    return found // nil when there is no such dataset
}
```

### Read a numeric dataset and its shape

```go
func readFloats(ds *hdf5.Dataset) ([]float64, []uint64, error) {
    shape, err := ds.Shape()
    if err != nil {
        return nil, nil, err
    }
    values, err := ds.Read() // all numeric types are converted to float64
    if err != nil {
        return nil, nil, err
    }
    // For shape [2 3], values[i*3+j] is element (i, j).
    return values, shape, nil
}
```

`Datatype()` describes the stored type (class, size, signedness, members of a compound), and `Layout()`
the storage (contiguous, chunked, compact) and filters.

### Read attributes

```go
func readAttributes(ds *hdf5.Dataset) error {
    names, err := ds.ListAttributes()
    if err != nil {
        return err
    }
    for _, name := range names {
        value, err := ds.ReadAttribute(name)
        if err != nil {
            return err
        }
        fmt.Printf("%s = %v (%T)\n", name, value, value)
    }
    return nil
}
```

`ReadAttribute` returns a Go scalar (`int32`, `float64`, ...), a slice (`[]float64`, `[]string`) or a
`string`, by the attribute's stored type; switch on it. Groups have `Attributes()` as well. Use
`ListAttributes` and `ReadAttribute` on datasets.

### Read strings and compound records

```go
func readTexts(ds *hdf5.Dataset) error {
    texts, err := ds.ReadStrings() // fixed-length string datasets
    if err != nil {
        return err
    }
    fmt.Println(texts)
    return nil
}

func readRecords(ds *hdf5.Dataset) error {
    records, err := ds.ReadCompound() // one map per element
    if err != nil {
        return err
    }
    for _, rec := range records {
        fmt.Println(rec["id"], rec["label"]) // member name -> value (int32, float64, string, ...)
    }
    return nil
}
```

Variable-length strings are not read by `ReadStrings`: `ReadVLenBytes` returns one `[]byte` per
element (`string(b)` gives the Go string), and it reads any other variable-length data the same way.
`ReadStrings` and `ReadCompound` return an error on a dataset of another type; use `Datatype()` to
choose.

### Read part of a large dataset

```go
func partial(ds *hdf5.Dataset) error {
    // A 2x3 block starting at (0, 0). The result is []float64.
    block, err := ds.ReadSlice([]uint64{0, 0}, []uint64{2, 3})
    if err != nil {
        return err
    }
    fmt.Printf("%T\n", block)

    // Every second column of two rows: start, count, stride and block as in H5Sselect_hyperslab.
    sel := &hdf5.HyperslabSelection{
        Start:  []uint64{0, 0},
        Count:  []uint64{2, 2},
        Stride: []uint64{1, 2},
    }
    picked, err := ds.ReadHyperslab(sel)
    if err != nil {
        return err
    }
    fmt.Printf("%T\n", picked)
    return nil
}

func sumByChunk(ds *hdf5.Dataset) (float64, error) {
    it, err := ds.ChunkIterator() // chunked datasets only; one chunk in memory at a time
    if err != nil {
        return 0, err
    }
    var sum float64
    for it.Next() {
        chunk, err := it.Chunk()
        if err != nil {
            return 0, err
        }
        if values, ok := chunk.([]float64); ok {
            for _, v := range values {
                sum += v
            }
        }
    }
    return sum, it.Err()
}
```

`ReadSlice` and `ReadHyperslab` read only the chunks the selection needs. Start, count, stride and
block slices must have one entry per dimension.

### Create a file and write datasets, groups and attributes

```go
func writeFile(path string) (err error) {
    fw, err := hdf5.CreateForWrite(path, hdf5.CreateTruncate)
    if err != nil {
        return err
    }
    defer func() {
        if cerr := fw.Close(); err == nil {
            err = cerr // the file is complete only after Close
        }
    }()

    if _, err := fw.CreateGroup("/run1"); err != nil {
        return err
    }

    // A 2x3 chunked, shuffled and compressed dataset. Data is flattened row-major.
    ds, err := fw.CreateDataset("/run1/temperature", hdf5.Float64, []uint64{2, 3},
        hdf5.WithChunkDims([]uint64{1, 3}),
        hdf5.WithShuffle(),
        hdf5.WithGZIPCompression(6),
    )
    if err != nil {
        return err
    }
    if err := ds.Write([]float64{1, 2, 3, 4, 5, 6}); err != nil {
        return err
    }
    if err := ds.WriteAttribute("units", "K"); err != nil {
        return err
    }
    if err := ds.WriteAttribute("calibration", []float64{1, 0}); err != nil {
        return err
    }

    // Fixed-length strings need their size.
    names, err := fw.CreateDataset("/run1/names", hdf5.String, []uint64{2}, hdf5.WithStringSize(8))
    if err != nil {
        return err
    }
    return names.Write([]string{"alpha", "beta"})
}
```

`Write` takes a slice that matches the dataset's type: `[]int8` to `[]int64`, `[]uint8` to
`[]uint64`, `[]float32`, `[]float64`, `[]string`, or `[]map[string]any` for compounds. The data length
must equal the product of the dimensions. Filters (`WithGZIPCompression`, `WithShuffle`,
`WithFletcher32`) and `WithMaxDims` need `WithChunkDims`. `WriteAttribute` takes a scalar, a slice or a
string, and replaces an attribute that already has that name.

A compound dataset:

```go
func writeRecords(fw *hdf5.FileWriter) error {
    ct, err := hdf5.NewPackedCompoundType([]hdf5.CompoundMember{
        {Name: "id", Type: hdf5.Int32},
        {Name: "temperature", Type: hdf5.Float64},
        {Name: "label", Type: hdf5.FixedString{Size: 16}},
    })
    if err != nil {
        return err
    }
    ds, err := fw.CreateCompound("/samples", ct, []uint64{2})
    if err != nil {
        return err
    }
    return ds.Write([]map[string]any{
        {"id": 1, "temperature": 20.5, "label": "first"},
        {"id": 2, "temperature": 21.0, "label": "second"},
    })
}
```

### Modify an existing file

```go
func modify(path string) (err error) {
    fw, err := hdf5.OpenForWrite(path, hdf5.OpenReadWrite)
    if err != nil {
        return err
    }
    defer func() {
        if cerr := fw.Close(); err == nil {
            err = cerr // the file is complete only after Close
        }
    }()

    counts, err := fw.CreateDataset("/counts", hdf5.Int32, []uint64{3})
    if err != nil {
        return err
    }
    if err := counts.Write([]int32{1, 2, 3}); err != nil {
        return err
    }

    existing, err := fw.OpenDataset("/run1/temperature")
    if err != nil {
        return err
    }
    if err := existing.WriteAttribute("source", "sensor-7"); err != nil {
        return err
    }

    // Delete a dataset, as H5Ldelete does. Deleting a group also deletes everything
    // only that group links to, so check that a group is empty first if you need to keep its children.
    return fw.Delete("/run1/names")
}
```

`OpenForWrite` modifies an existing file in place. It returns an error that wraps `ErrNeedsRepack` for
a file it cannot make valid (see the error table below).
`fw.CreateSoftLink`, `CreateHardLink` and `CreateExternalLink` add links. Do not use a
`DatasetWriter` or `GroupWriter` after the object it names was deleted.

### Write a user block (MATLAB v7.3 header)

```go
func matlabHeader(path string, header []byte) (err error) {
    fw, err := hdf5.CreateForWrite(path, hdf5.CreateTruncate, hdf5.WithUserBlock(512))
    if err != nil {
        return err
    }
    defer func() {
        if cerr := fw.Close(); err == nil {
            err = cerr // the file is complete only after Close
        }
    }()
    return fw.WriteUserBlock(header) // up to 512 bytes; the rest of the block stays zero
}
```

The user block size is 0 or a power of two of at least 512. The HDF5 data starts after the block.
Reading a file with a user block needs nothing special; `OpenForWrite` never touches the block.

### Handle errors and untrusted files

```go
func openUntrusted(path string) error {
    file, err := hdf5.Open(path)
    switch {
    case errors.Is(err, hdf5.ErrCorrupt):
        return fmt.Errorf("damaged file: %w", err)
    case errors.Is(err, hdf5.ErrNotSupported):
        return fmt.Errorf("valid HDF5 this library cannot read: %w", err)
    case err != nil:
        return err
    }
    defer file.Close()

    file.SetReadLimit(256 << 20) // at most 256 MiB per dataset read (default 4 GiB)
    for _, w := range file.Warnings() {
        log.Println(w) // the reader tolerated this; the message says how to fix the file
    }

    file.Walk(func(p string, obj hdf5.Object) {
        ds, ok := obj.(*hdf5.Dataset)
        if !ok {
            return
        }
        _, err := ds.Read()
        switch {
        case errors.Is(err, hdf5.ErrLimit):
            log.Printf("%s: result larger than the read limit", p)
        case errors.Is(err, hdf5.ErrNoData):
            log.Printf("%s: no stored data", p)
        case errors.Is(err, hdf5.ErrNotSupported):
            log.Printf("%s: %v", p, err)
        case err != nil:
            log.Printf("%s: %v", p, err)
        }
    })
    return nil
}
```

| Error (test with `errors.Is`) | Meaning |
|---|---|
| `hdf5.ErrCorrupt` | The file's metadata is invalid or inconsistent. Do not retry. |
| `hdf5.ErrNotSupported` | Valid HDF5 that this library does not handle (see Limitations). |
| `hdf5.ErrLimit` | The read would allocate more than `File.ReadLimit()`. The file may be valid; raise the limit with `SetReadLimit` if you accept the memory. |
| `hdf5.ErrNoData` | The dataset's storage was never written and it has no defined fill value, so there is nothing to return. The file is not corrupt. |
| `hdf5.ErrNeedsRepack` | `OpenForWrite` refused a file it cannot make valid in place (typically written by v0.13.9 or earlier of this library, which the C library cannot open either). The file reads with `Open`; write its contents into a new file. |

With files from outside, call `SetReadLimit` before reading, check the file size yourself first, and
read large datasets with `ReadSlice` or `ChunkIterator`. Dataset and group names are arbitrary
strings from the file; never use one as a file system path without sanitizing it. See
[SECURITY.md](SECURITY.md).

## Type mapping

| HDF5 | Read with | Go value | Write with (`CreateDataset` type) |
|---|---|---|---|
| integers 8 to 64 bits, signed and unsigned; float32, float64 | `Read`, `ReadSlice`, `ReadHyperslab` | `[]float64` (uint64 and int64 above 2^53 lose precision) | `hdf5.Int8` to `hdf5.Int64`, `hdf5.Uint8` to `hdf5.Uint64`, `hdf5.Float32`, `hdf5.Float64` with a slice of the same Go type |
| fixed-length string | `ReadStrings` | `[]string` | `hdf5.String` with `WithStringSize`, `[]string` |
| compound | `ReadCompound` | `[]map[string]interface{}` | `CreateCompound` with `NewPackedCompoundType`, `[]map[string]any` |
| variable-length data | `ReadVLenBytes` | `[][]byte` | `hdf5.VLen*` types, see the datatypes guide |
| array of a numeric type | `Read` | flattened `[]float64`, array dimensions innermost | `hdf5.Array*` with `WithArrayDims` |
| enum, opaque, references, FP8, bfloat16 | `Datatype()` describes them | see [docs/guides/DATATYPES.md](docs/guides/DATATYPES.md) | `hdf5.Enum*` with `WithEnumValues`, `hdf5.Opaque` with `WithOpaqueTag` |

Attributes map to Go scalars, slices and strings, as `ReadAttribute` and `WriteAttribute` show above.

## Limitations and common mistakes

- `Read`, `ReadSlice` and `ReadHyperslab` return `[]float64` for every numeric type (checked at
  v0.15.0 for int8, uint16, uint64 and float32). Convert back with a cast; 64-bit integers above 2^53
  lose precision, and there is no integer-typed read.
- `Read` on a string or compound dataset fails with an error that wraps `ErrNotSupported`. Use
  `ReadStrings` or `ReadCompound`.
- Filters without a decoder here: SZIP, N-bit, Scale-offset and third-party filters. A dataset that
  uses one fails with an error that wraps `ErrNotSupported`; the undecoded bytes are never returned
  as values.
- `Open` refuses a file whose groups form a hard-link cycle (a link back to an ancestor).
- No SWMR mode and no parallel I/O. Separate `File` values are independent; one `File` used from
  several goroutines needs your own synchronization.
- `WithSmartRebalancing` is accepted and has no effect. The B-tree rebalancing options
  (`WithBTreeRebalancing`, `WithLazyRebalancing`, `WithIncrementalRebalancing`) only matter for
  files with many deleted attributes; leave them out otherwise.
- `FileWriter.Delete` of a group deletes the group and everything only it links to.
- A written file is complete only after `FileWriter.Close()` returns; check its error.
- External raw data files (datasets with external storage) are refused unless you call
  `File.SetExternalFilePrefix(dir)`.
- Do not invent API. If a function is not in `go doc github.com/scigolib/hdf5`, it does not exist.
  There is no `File.OpenDataset`, no `Dataset.ReadInt64` and no `hdf5.Open` option list.

## Contributing to this repository

Only if you are changing the library itself: read [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow
and the quality gates, and [docs/architecture/OVERVIEW.md](docs/architecture/OVERVIEW.md) for how the
code is laid out. The short form:

```bash
go build ./...
go test ./...
golangci-lint run ./...
```

## Links

- Repository: https://github.com/scigolib/hdf5
- API reference: https://pkg.go.dev/github.com/scigolib/hdf5
- [README.md](README.md), [Quick Start](docs/guides/QUICKSTART.md),
  [Reading Data](docs/guides/READING_DATA.md), [Datatypes](docs/guides/DATATYPES.md),
  [Troubleshooting](docs/guides/TROUBLESHOOTING.md), [FAQ](docs/guides/FAQ.md)
- [CHANGELOG.md](CHANGELOG.md), [SECURITY.md](SECURITY.md)
- Examples: [examples/](examples/)
