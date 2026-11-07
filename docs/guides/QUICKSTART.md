# Quick Start Guide

Small programs that read and write HDF5 files with this library.

---

## Installation

```bash
go get github.com/scigolib/hdf5
```

Requirements:
- Go 1.25 or later
- No cgo and no external libraries

---

## Your First HDF5 Program

### 1. Reading an HDF5 File

```go
package main

import (
    "fmt"
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    // Open an HDF5 file
    file, err := hdf5.Open("data.h5")
    if err != nil {
        log.Fatalf("Failed to open file: %v", err)
    }
    defer file.Close()

    // Print file information
    fmt.Printf("HDF5 file opened successfully\n")
    fmt.Printf("Superblock version: %d\n", file.SuperblockVersion())
}
```

### 2. Walking the File Structure

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

    // Walk through all objects in the file
    file.Walk(func(path string, obj hdf5.Object) {
        switch v := obj.(type) {
        case *hdf5.Group:
            fmt.Printf("group:   %s (%d children)\n",
                path, len(v.Children()))

        case *hdf5.Dataset:
            fmt.Printf("dataset: %s\n", path)

        default:
            fmt.Printf("other:   %s\n", path)
        }
    })
}
```

Output for a file with a root dataset and one group of three datasets (the exact path strings
depend on the file):
```
group:   / (2 children)
dataset: /temperature
group:   /experiments (3 children)
dataset: /experiments/trial1
dataset: /experiments/trial2
dataset: /experiments/trial3
```

### 3. Exploring Groups

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

    // Get the root group
    root := file.Root()
    fmt.Printf("Root group: %s\n", root.Name())

    // Iterate through children
    for _, child := range root.Children() {
        fmt.Printf("  - %s\n", child.Name())
    }
}
```

---

## Reading Dataset Values

`Dataset.Read` returns numeric data as `[]float64`: every integer and floating-point value is
converted. Strings are read with `ReadStrings` and compound data with `ReadCompound`. The dataset's
datatype class tells which one to call:

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

    file.Walk(func(path string, obj hdf5.Object) {
        ds, ok := obj.(*hdf5.Dataset)
        if !ok {
            return
        }
        dt, err := ds.Datatype()
        if err != nil {
            log.Printf("%s: %v", path, err)
            return
        }

        switch dt.Class {
        case hdf5.ClassInteger, hdf5.ClassFloat:
            values, err := ds.Read()
            if err != nil {
                log.Printf("%s: %v", path, err)
                return
            }
            fmt.Printf("%s: %d numeric values, first: %v\n", path, len(values), values[:min(10, len(values))])

        case hdf5.ClassString:
            values, err := ds.ReadStrings()
            if err != nil {
                log.Printf("%s: %v", path, err)
                return
            }
            fmt.Printf("%s: %d strings, first: %q\n", path, len(values), values[:min(10, len(values))])

        default:
            fmt.Printf("%s: datatype class %s, see the Reading Data guide\n", path, dt.Class)
        }
    })
}
```

`min` is a built-in function since Go 1.21.

---

## Complete Example

A program that counts the groups and datasets of a file and prints its structure:

```go
package main

import (
    "flag"
    "fmt"
    "log"
    "os"

    "github.com/scigolib/hdf5"
)

func main() {
    // Parse command-line arguments
    filename := flag.String("file", "", "HDF5 file to analyze")
    flag.Parse()

    if *filename == "" {
        fmt.Println("Usage: analyze -file <filename.h5>")
        os.Exit(1)
    }

    // Open file
    file, err := hdf5.Open(*filename)
    if err != nil {
        log.Fatalf("Error opening file: %v", err)
    }
    defer file.Close()

    // Print file info
    fmt.Printf("HDF5 file analysis\n")
    fmt.Printf("File: %s\n", *filename)
    fmt.Printf("Superblock version: %d\n\n", file.SuperblockVersion())

    // Statistics
    var (
        groupCount   int
        datasetCount int
    )

    // Analyze structure
    file.Walk(func(path string, obj hdf5.Object) {
        switch obj.(type) {
        case *hdf5.Group:
            groupCount++
        case *hdf5.Dataset:
            datasetCount++
        }
    })

    fmt.Printf("Statistics:\n")
    fmt.Printf("  Groups:   %d\n", groupCount)
    fmt.Printf("  Datasets: %d\n\n", datasetCount)

    // Print structure
    fmt.Printf("Structure:\n")
    file.Walk(func(path string, obj hdf5.Object) {
        indent := ""
        depth := 0
        for _, c := range path {
            if c == '/' {
                depth++
            }
        }
        for i := 0; i < depth-1; i++ {
            indent += "  "
        }

        switch v := obj.(type) {
        case *hdf5.Group:
            if path != "/" {
                fmt.Printf("%s%s/\n", indent, v.Name())
            }
        case *hdf5.Dataset:
            fmt.Printf("%s%s\n", indent, v.Name())
        }
    })
}
```

**Usage**:
```bash
go build -o analyze
./analyze -file mydata.h5
```

---

## Creating Test Files

Test files can be created in Go, without h5py:

```go
package main

import (
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    fw, err := hdf5.CreateForWrite("test.h5", hdf5.CreateTruncate)
    if err != nil {
        log.Fatal(err)
    }
    defer func() {
        if err := fw.Close(); err != nil {
            log.Fatal(err)
        }
    }()

    // Datasets of different types. A fixed-length string dataset needs its string size.
    dsInt, err := fw.CreateDataset("/integers", hdf5.Int32, []uint64{100})
    if err != nil {
        log.Fatal(err)
    }
    dsFloat, err := fw.CreateDataset("/floats", hdf5.Float64, []uint64{50})
    if err != nil {
        log.Fatal(err)
    }
    dsStr, err := fw.CreateDataset("/strings", hdf5.String, []uint64{2}, hdf5.WithStringSize(10))
    if err != nil {
        log.Fatal(err)
    }

    integers := make([]int32, 100)
    for i := range integers {
        integers[i] = int32(i)
    }
    if err := dsInt.Write(integers); err != nil {
        log.Fatal(err)
    }

    floats := make([]float64, 50)
    for i := range floats {
        floats[i] = float64(i) * 0.5
    }
    if err := dsFloat.Write(floats); err != nil {
        log.Fatal(err)
    }

    if err := dsStr.Write([]string{"hello", "world"}); err != nil {
        log.Fatal(err)
    }

    // Groups, including nested ones, are created from the file; a dataset in a
    // group is addressed by its full path.
    if _, err := fw.CreateGroup("/experiments"); err != nil {
        log.Fatal(err)
    }
    if _, err := fw.CreateDataset("/experiments/trial1", hdf5.Int32, []uint64{5}); err != nil {
        log.Fatal(err)
    }
    if _, err := fw.CreateGroup("/experiments/subgroup"); err != nil {
        log.Fatal(err)
    }
    dsResult, err := fw.CreateDataset("/experiments/subgroup/result", hdf5.Int32, []uint64{1})
    if err != nil {
        log.Fatal(err)
    }
    if err := dsResult.Write([]int32{42}); err != nil {
        log.Fatal(err)
    }

    log.Println("Created test.h5")
}
```

Check the result with `h5dump test.h5` or h5py.

---

## Common Questions

### Can I read dataset values?
Yes. The library reads:
- integers and floating-point numbers (`Read` returns `[]float64`)
- fixed-length strings (`ReadStrings`) and variable-length strings (`ReadVLenBytes`, one `[]byte`
  per string)
- compound types (`ReadCompound`)
- chunked, compact and contiguous datasets, with GZIP, Shuffle, Fletcher32, LZF and BZIP2 filters

### Can I write HDF5 files?
Yes: contiguous, chunked and compact datasets, groups, attributes (compact and dense storage),
GZIP, Shuffle, Fletcher32 and LZF filters, links, user blocks, and modification of existing files.
See the README "Current Status" section and [ROADMAP.md](../../ROADMAP.md).

### Does it require cgo?
No. The library is pure Go and works on every platform Go supports.

### What HDF5 versions are supported?
Superblock versions 0, 2 and 3, which covers files written by HDF5 1.0 through 2.0.0.

### What datatypes are supported?
- Fixed-point integers (8 to 64 bits, signed and unsigned)
- Floating point (float32, float64, FP8, bfloat16)
- Fixed-length and variable-length strings
- Compound types
- Arrays
- Enums
- References (object and region)
- Opaque

H5T_TIME is not supported.

### What compression formats work?
- Supported: GZIP (deflate), Shuffle, Fletcher32, LZF, BZIP2
- Not supported: SZIP (4), N-bit (5), Scale-offset (6) and third-party filters such as Blosc, LZ4 and Zstd

A dataset that uses an unsupported filter fails with an error that wraps `hdf5.ErrNotSupported`;
earlier versions returned the undecoded bytes as values.

### Is it thread-safe?
Separate `File` values are independent. One `File` used from several goroutines needs the caller's
synchronization. There is no SWMR (single writer, multiple readers) mode and no parallel I/O.

### What about performance?
The library pools buffers and reads only the chunks a selection needs. It has not been compared
with the C library in a published benchmark. Measure your own workload; the benchmarks in the
repository are `release_bench_test.go` and `btree_rebalancing_bench_test.go`.

---

## Next Steps

- [Architecture Overview](../architecture/OVERVIEW.md): how the library is built
- [ROADMAP.md](../../ROADMAP.md): what comes next
- [Examples](../../examples/):
  - `01-basic/`: opening a file
  - `02-list-objects/`: listing the file structure
  - `03-read-dataset/`: reading dataset values
  - `04-vlen-strings/`: variable-length strings
  - `05-comprehensive/`: a complete file analysis
  - `06-write-dataset/`: writing datasets
  - `07-rebalancing/`: B-tree rebalancing strategies
- [API reference](https://pkg.go.dev/github.com/scigolib/hdf5): GoDoc

---

## Troubleshooting

### "not an HDF5 file" error
```go
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Printf("Error: %v", err)
}
```

Check that:
- the file exists and is readable
- it is valid HDF5 (`h5dump -H file.h5`, if the HDF5 tools are installed)
- it is not truncated or corrupted

### "unsupported superblock version" error

The file uses a superblock version this library does not read (it reads 0, 2 and 3). Open an issue
at https://github.com/scigolib/hdf5/issues with the file (if it can be shared), the output of
`h5dump -H yourfile.h5` and the tool that created it.

### "unsupported datatype" error

The dataset uses a datatype the library does not read. `errors.Is(err, hdf5.ErrNotSupported)`
tells unsupported features from damaged files. The supported datatypes are listed above and in
[DATATYPES.md](DATATYPES.md). Open an issue with the output of `h5dump -H` for the dataset.

### Reading compressed data fails

**Solution**:
- Check which filters the dataset uses: `layout, _ := ds.Layout()`, then `layout.Filters`
- SZIP, N-bit, Scale-offset and third-party filters are not supported; the error wraps
  `hdf5.ErrNotSupported` (see [Troubleshooting](TROUBLESHOOTING.md#problem-compression-error))

---

## Status

The supported features, the validation against the official HDF5 test suite (379 of 383 valid
files) and the limitations are listed in the README "Current Status" section:
[README.md](../../README.md).

---

## Further Reading

- [Installation Guide](INSTALLATION.md): installation and platform support
- [Reading Data](READING_DATA.md): datasets, attributes and partial reads
- [Datatypes Guide](DATATYPES.md): how HDF5 types map to Go types
- [Troubleshooting](TROUBLESHOOTING.md): common problems
- [FAQ](FAQ.md): frequently asked questions
- [Examples](../../examples/): seven runnable programs

---

*Last Updated: 2026-11-07*
