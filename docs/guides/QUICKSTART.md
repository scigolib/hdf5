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

If you don't have HDF5 files to test with, you can create them using Python:

```python
# create_test.py
import h5py
import numpy as np

# Create a new HDF5 file
with h5py.File('test.h5', 'w') as f:
    # Create datasets with different types
    f.create_dataset('integers', data=np.arange(100, dtype=np.int32))
    f.create_dataset('floats', data=np.random.rand(50).astype(np.float64))
    f.create_dataset('strings', data=np.array([b'hello', b'world']))

    # Create a group
    grp = f.create_group('experiments')
    grp.create_dataset('trial1', data=np.array([1, 2, 3, 4, 5], dtype=np.int32))
    grp.create_dataset('trial2', data=np.array([6, 7, 8, 9, 10], dtype=np.int32))

    # Nested groups
    subgrp = grp.create_group('subgroup')
    subgrp.create_dataset('result', data=np.array([42], dtype=np.int32))

    # Chunked dataset with compression
    f.create_dataset('compressed',
                     data=np.random.rand(1000, 1000),
                     chunks=(100, 100),
                     compression='gzip',
                     compression_opts=6)

print("Created test.h5")
```

```bash
# Install h5py
pip install h5py numpy

# Run script
python create_test.py
```

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
**A**: Not yet in v0.9.0-beta. Write support is planned for v2.0 (4-5 months). See [ROADMAP.md](../../ROADMAP.md) for details.

### Q: Does it require CGo?
**A**: **No!** This is a pure Go implementation with zero C dependencies. Works on all Go-supported platforms.

### Q: What HDF5 versions are supported?
**A**: The library supports HDF5 format with superblock v0, v2, and v3 (covering HDF5 1.0 through 1.14+).

### Q: What datatypes are supported?
**A**: Currently supported:
- Fixed-point integers (int32, int64)
- Floating-point (float32, float64)
- Fixed-length strings
- Variable-length strings
- Compound types (struct-like)

Not yet supported: Arrays, enums, references, opaque, time types

### Q: What compression formats work?
**A**: Currently:
- ✅ GZIP/Deflate (most common)
- ❌ SZIP, LZF, BZIP2 (planned for v1.2)

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

**Current Status: ~98% ready for reading common HDF5 files**

✅ **Ready for production use** if your files contain:
- Standard datatypes (int, float, string, compound)
- GZIP compression
- Superblock v0, v2, or v3
- Object header v2

⚠️ **Beta limitations**:
- No write support yet (v2.0)
- Limited attribute reading
- Object header v1 not fully supported
- Some advanced datatypes missing

See [README.md](../../README.md) for full feature list.

---

*Last Updated: 2025-10-17*
*Version: 0.9.0-beta*
