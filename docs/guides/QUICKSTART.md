# Quick Start Guide

Get started with the HDF5 Go library in minutes.

---

## 📦 Installation

```bash
go get github.com/scigolib/hdf5
```

**Requirements**:
- Go 1.25 or later
- No CGo dependencies
- No external libraries required

---

## 🚀 Your First HDF5 Program

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
            fmt.Printf("📁 Group: %s (%d children)\n",
                path, len(v.Children()))

        case *hdf5.Dataset:
            fmt.Printf("📊 Dataset: %s\n", path)

        default:
            fmt.Printf("❓ Unknown: %s\n", path)
        }
    })
}
```

**Example output**:
```
📁 Group: / (2 children)
📊 Dataset: /temperature
📁 Group: /experiments/ (3 children)
📊 Dataset: /experiments/trial1
📊 Dataset: /experiments/trial2
📊 Dataset: /experiments/trial3
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

## 📊 Reading Dataset Values

The library can read dataset values for common datatypes:

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

    // Walk and read datasets
    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            // Read dataset data
            data, err := ds.Read()
            if err != nil {
                fmt.Printf("Error reading %s: %v\n", path, err)
                return
            }

            fmt.Printf("Dataset %s:\n", path)

            // Data type depends on HDF5 datatype
            switch v := data.(type) {
            case []int32:
                fmt.Printf("  Type: int32, Count: %d\n", len(v))
                fmt.Printf("  Values: %v\n", v[:min(10, len(v))])

            case []int64:
                fmt.Printf("  Type: int64, Count: %d\n", len(v))
                fmt.Printf("  Values: %v\n", v[:min(10, len(v))])

            case []float32:
                fmt.Printf("  Type: float32, Count: %d\n", len(v))
                fmt.Printf("  Values: %v\n", v[:min(10, len(v))])

            case []float64:
                fmt.Printf("  Type: float64, Count: %d\n", len(v))
                fmt.Printf("  Values: %v\n", v[:min(10, len(v))])

            case []string:
                fmt.Printf("  Type: string, Count: %d\n", len(v))
                fmt.Printf("  Values: %v\n", v[:min(10, len(v))])

            default:
                fmt.Printf("  Type: %T (not yet fully supported)\n", v)
            }
        }
    })
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

---

## 📖 Complete Example

Here's a complete program that analyzes an HDF5 file:

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
    fmt.Printf("=== HDF5 File Analysis ===\n")
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
                fmt.Printf("%s📁 %s/\n", indent, v.Name())
            }
        case *hdf5.Dataset:
            fmt.Printf("%s📊 %s\n", indent, v.Name())
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

## 🧪 Creating Test Files

You can create test HDF5 files directly in pure Go (no Python needed!):

```go
package main

import (
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // Create a new HDF5 file
    fw, err := hdf5.CreateForWrite("test.h5", hdf5.CreateTruncate)
    if err != nil {
        log.Fatal(err)
    }
    defer fw.Close()

    // Create datasets with different types
    dsInt, _ := fw.CreateDataset("/integers", hdf5.Int32, []uint64{100})
    dsFloat, _ := fw.CreateDataset("/floats", hdf5.Float64, []uint64{50})
    dsStr, _ := fw.CreateDataset("/strings", hdf5.StringFixed(10), []uint64{2})

    // Write data
    integers := make([]int32, 100)
    for i := range integers {
        integers[i] = int32(i)
    }
    dsInt.WriteInt32(integers)

    floats := make([]float64, 50)
    for i := range floats {
        floats[i] = float64(i) * 0.5
    }
    dsFloat.Write(floats)

    dsStr.WriteStrings([]string{"hello", "world"})

    // Create groups
    grp, _ := fw.CreateGroup("/experiments")
    grp.CreateDataset("trial1", hdf5.Int32, []uint64{5})
    grp.CreateDataset("trial2", hdf5.Int32, []uint64{5})

    // Nested groups
    subgrp, _ := fw.CreateGroup("/experiments/subgroup")
    dsResult, _ := subgrp.CreateDataset("result", hdf5.Int32, []uint64{1})
    dsResult.WriteInt32([]int32{42})

    log.Println("Created test.h5")
}
```

**Pure Go workflow** - Create files with this library, verify with h5dump or Python h5py!

---

## ❓ Common Questions

### Q: Can I read dataset values?
**A**: **Yes!** The library supports reading:
- ✅ Integers (int32, int64)
- ✅ Floats (float32, float64)
- ✅ Strings (fixed-length and variable-length)
- ✅ Compound types (struct-like data)
- ✅ Compressed datasets (GZIP)
- ✅ Chunked datasets

### Q: Can I write HDF5 files?
**A**: Yes! Write support advancing rapidly - contiguous/chunked datasets, all datatypes, groups, attributes (compact & dense), compression (GZIP/Shuffle), dense storage RMW complete. See [ROADMAP.md](../../ROADMAP.md) for details.

### Q: Does it require CGo?
**A**: **No!** This is a pure Go implementation with zero C dependencies. Works on all Go-supported platforms.

### Q: What HDF5 versions are supported?
**A**: The library supports HDF5 format with superblock v0, v2, v3, and v4 (covering HDF5 1.0 through HDF5 2.0.0+).

### Q: What datatypes are supported?
**A**: Fully supported:
- Fixed-point integers (int8-64, uint8-64)
- Floating-point (float32, float64, FP8, bfloat16)
- Fixed-length and variable-length strings
- Compound types (struct-like)
- Arrays (fixed-size)
- Enums (named integers)
- References (object/region)
- Opaque (binary blobs)

Not supported: H5T_TIME (deprecated in HDF5 spec)

### Q: What compression formats work?
**A**: Currently:
- ✅ GZIP/Deflate (most common)
- ❌ SZIP, LZF, BZIP2 (optional filters, planned for v1.x)

### Q: Is it thread-safe?
**A**: Currently, each `File` instance should be used from a single goroutine. Concurrent file access support is planned for v2.0.

### Q: What about performance?
**A**: The library uses buffer pooling and efficient memory management. Performance is within 2-3x of the C library for most operations.

---

## 📚 Next Steps

- **[Architecture Overview](../architecture/OVERVIEW.md)** - How the library works internally
- **[ROADMAP.md](../../ROADMAP.md)** - Future plans and write support timeline
- **[Examples](../../examples/)** - More comprehensive examples:
  - `01-basic/` - Basic file opening
  - `02-list-objects/` - Listing file structure
  - `03-read-dataset/` - Reading dataset values
  - `04-vlen-strings/` - Variable-length strings
  - `05-comprehensive/` - Complete file analysis

- **[API Reference](https://pkg.go.dev/github.com/scigolib/hdf5)** - Full GoDoc documentation

---

## 🐛 Troubleshooting

### "not an HDF5 file" error
```go
file, err := hdf5.Open("data.h5")
if err != nil {
    // Check error message
    log.Printf("Error: %v", err)
}
```

**Solutions**:
- Verify file exists and is readable
- Check file is valid HDF5 (try with `h5dump -H file.h5` if HDF5 tools installed)
- Ensure file isn't corrupted
- Check file permissions

### "unsupported superblock version" error

**Solution**: Your HDF5 file uses a format version we don't support yet (v1 or v4+). Please file an issue at https://github.com/scigolib/hdf5/issues with:
- HDF5 file (if shareable)
- Output of `h5dump -H yourfile.h5`
- How the file was created (tool/library used)

### "unsupported datatype" error

**Solution**: Your dataset uses a datatype we haven't implemented yet. Currently supported: int32, int64, float32, float64, strings, compounds. Please file an issue with details.

### Reading compressed data fails

**Solution**:
- Check if compression is GZIP (supported)
- Other formats (SZIP, LZF) not yet supported - see [ROADMAP.md](../../ROADMAP.md)

---

## 🚀 Production Readiness

**Current Status: Production-ready for reading and writing HDF5 files**

✅ **Ready for production use** if your files contain:
- Standard datatypes (int, float, string, compound, arrays, enums)
- GZIP compression
- Superblock v0, v2, v3, or v4 (HDF5 2.0.0 compatible)
- Object header v2

**Production Status**:
- ✅ Full read/write support
- ✅ All datatypes including compound
- ✅ Soft/external links supported
- ✅ 98.2% HDF5 test suite pass rate

See [README.md](../../README.md) for full feature list.

---

### 📚 Next Steps

After completing this quick start, explore:

- **[Installation Guide](INSTALLATION.md)** - Detailed installation and platform support
- **[Reading Data](READING_DATA.md)** - Complete guide to datasets, attributes, and datatypes
- **[Datatypes Guide](DATATYPES.md)** - HDF5 to Go type mapping details
- **[Troubleshooting](TROUBLESHOOTING.md)** - Solutions to common problems
- **[FAQ](FAQ.md)** - Frequently asked questions
- **[Examples](../../examples/)** - 5 comprehensive examples with detailed documentation

---

*Last Updated: 2025-11-13*
