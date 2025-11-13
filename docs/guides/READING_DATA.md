# Reading Data Guide

> **Comprehensive guide to reading datasets, attributes, and navigating HDF5 files**

---

## 📚 Table of Contents

- [Opening Files](#opening-files)
- [Navigating File Structure](#navigating-file-structure)
- [Reading Datasets](#reading-datasets)
- [Reading Attributes](#reading-attributes)
- [Working with Groups](#working-with-groups)
- [Datatypes and Type Conversion](#datatypes-and-type-conversion)
- [Error Handling](#error-handling)
- [Performance Tips](#performance-tips)

---

## 🗂️ Opening Files

### Basic File Opening

```go
package main

import (
    "fmt"
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    // Open HDF5 file (read-only)
    file, err := hdf5.Open("data.h5")
    if err != nil {
        log.Fatalf("Failed to open file: %v", err)
    }
    defer file.Close()  // Always close files!

    fmt.Printf("File opened successfully\n")
    fmt.Printf("Superblock version: %d\n", file.SuperblockVersion())
}
```

### File Properties

```go
// Get superblock version (0, 2, or 3)
version := file.SuperblockVersion()

// Get root group
root := file.Root()
fmt.Printf("Root group name: %s\n", root.Name())

// Access file reader (advanced)
reader := file.Reader()  // Returns io.ReaderAt
```

### File Closing

```go
// Option 1: Defer (recommended)
file, _ := hdf5.Open("data.h5")
defer file.Close()

// Option 2: Explicit close
file, _ := hdf5.Open("data.h5")
err := file.Close()
if err != nil {
    log.Printf("Close error: %v", err)
}

// Note: It's safe to call Close() multiple times
```

---

## 🚶 Navigating File Structure

### Walking the Entire File

The `Walk()` method traverses all objects in the file:

```go
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
```

**Output example**:
```
📁 Group: / (3 children)
📊 Dataset: /temperature
📁 Group: /experiments/ (2 children)
📊 Dataset: /experiments/trial1
📊 Dataset: /experiments/trial2
```

### Exploring Groups

```go
// Get root group
root := file.Root()

// Iterate through children
for _, child := range root.Children() {
    switch obj := child.(type) {
    case *hdf5.Group:
        fmt.Printf("Group: %s\n", obj.Name())

        // Explore subgroup
        for _, subchild := range obj.Children() {
            fmt.Printf("  - %s\n", subchild.Name())
        }

    case *hdf5.Dataset:
        fmt.Printf("Dataset: %s\n", obj.Name())
    }
}
```

### Finding Specific Objects

```go
// Custom function to find a dataset by name
func findDataset(file *hdf5.File, targetName string) *hdf5.Dataset {
    var result *hdf5.Dataset

    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            if ds.Name() == targetName {
                result = ds
            }
        }
    })

    return result
}

// Usage
dataset := findDataset(file, "temperature")
if dataset != nil {
    fmt.Printf("Found dataset: %s\n", dataset.Name())
}
```

---

## 📊 Reading Datasets

### Numeric Datasets

The library automatically converts numeric data to `float64` for convenience:

```go
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        // Read as float64 (works for int32, int64, float32, float64)
        data, err := ds.Read()
        if err != nil {
            log.Printf("Error reading %s: %v", path, err)
            return
        }

        fmt.Printf("Dataset: %s\n", path)
        fmt.Printf("  Count: %d values\n", len(data))

        // Show first 5 values
        for i := 0; i < 5 && i < len(data); i++ {
            fmt.Printf("  [%d] = %.6f\n", i, data[i])
        }
    }
})
```

**Supported numeric types**:
- `int32` → `float64`
- `int64` → `float64`
- `float32` → `float64`
- `float64` → `float64`

### String Datasets

For string datasets, use `ReadStrings()`:

```go
if ds, ok := obj.(*hdf5.Dataset); ok {
    strings, err := ds.ReadStrings()
    if err != nil {
        log.Printf("Error reading strings: %v", err)
        return
    }

    fmt.Printf("String dataset: %s\n", ds.Name())
    for i, str := range strings {
        fmt.Printf("  [%d] = %q\n", i, str)
    }
}
```

**Supported string types**:
- Fixed-length strings (null-padded)
- Fixed-length strings (space-padded)
- Fixed-length strings (null-terminated)
- Variable-length strings (via Global Heap)

### Compound (Struct) Datasets

For compound types (struct-like data):

```go
if ds, ok := obj.(*hdf5.Dataset); ok {
    compounds, err := ds.ReadCompound()
    if err != nil {
        log.Printf("Error reading compound: %v", err)
        return
    }

    fmt.Printf("Compound dataset: %s\n", ds.Name())
    for i, compound := range compounds {
        fmt.Printf("  Record %d:\n", i)

        // Each compound is a map[string]interface{}
        for fieldName, fieldValue := range compound {
            fmt.Printf("    %s = %v\n", fieldName, fieldValue)
        }
    }
}
```

**Example output**:
```
Compound dataset: /measurements
  Record 0:
    time = 1.5e9
    temperature = 25.3
    humidity = 65.2
    location = "Lab A"
  Record 1:
    time = 1.5e9
    temperature = 26.1
    humidity = 63.8
    location = "Lab B"
```

### Dataset Metadata

Get information about a dataset without reading data:

```go
if ds, ok := obj.(*hdf5.Dataset); ok {
    info, err := ds.Info()
    if err != nil {
        log.Printf("Error getting info: %v", err)
        return
    }

    fmt.Println(info)
}
```

**Example output**:
```
Dataset: temperature
  Datatype: float64
  Layout: Chunked
  Dimensions: [100, 50]
  Chunks: [10, 10]
  Compression: gzip (level 6)
  Total size: 40000 bytes
```

---

## 🏷️ Reading Attributes

Attributes are metadata attached to groups and datasets.

### Reading Group Attributes

```go
// Get root group
root := file.Root()

// Read all attributes
attrs, err := root.Attributes()
if err != nil {
    log.Fatalf("Failed to read attributes: %v", err)
}

fmt.Printf("Group '/' has %d attributes:\n", len(attrs))
for _, attr := range attrs {
    fmt.Printf("  - %s: %v (type: %s)\n",
        attr.Name, attr.Value, attr.Datatype)
}
```

### Reading Dataset Attributes

```go
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        attrs, err := ds.Attributes()
        if err != nil {
            log.Printf("Error reading attributes for %s: %v", path, err)
            return
        }

        if len(attrs) > 0 {
            fmt.Printf("\nDataset: %s\n", path)
            for _, attr := range attrs {
                fmt.Printf("  @%s = %v\n", attr.Name, attr.Value)
            }
        }
    }
})
```

### Attribute Types

Attributes support the same datatypes as datasets:

```go
for _, attr := range attrs {
    fmt.Printf("Attribute: %s\n", attr.Name)

    // Value is interface{}, type depends on HDF5 datatype
    switch v := attr.Value.(type) {
    case int32:
        fmt.Printf("  Type: int32, Value: %d\n", v)

    case int64:
        fmt.Printf("  Type: int64, Value: %d\n", v)

    case float32:
        fmt.Printf("  Type: float32, Value: %.6f\n", v)

    case float64:
        fmt.Printf("  Type: float64, Value: %.6f\n", v)

    case string:
        fmt.Printf("  Type: string, Value: %q\n", v)

    case []int32, []int64, []float32, []float64:
        fmt.Printf("  Type: numeric array, Value: %v\n", v)

    case []string:
        fmt.Printf("  Type: string array, Value: %v\n", v)

    default:
        fmt.Printf("  Type: %T, Value: %v\n", v, v)
    }
}
```

---

## 👥 Working with Groups

### Group Hierarchy

```go
// Get root group
root := file.Root()

// Function to print group hierarchy
func printHierarchy(group *hdf5.Group, indent string) {
    fmt.Printf("%s%s/\n", indent, group.Name())

    for _, child := range group.Children() {
        switch obj := child.(type) {
        case *hdf5.Group:
            printHierarchy(obj, indent+"  ")

        case *hdf5.Dataset:
            fmt.Printf("%s  - %s (dataset)\n", indent, obj.Name())
        }
    }
}

printHierarchy(root, "")
```

**Output**:
```
/
  experiments/
    trial1/
      - data (dataset)
      - parameters (dataset)
    trial2/
      - data (dataset)
  - temperature (dataset)
  - humidity (dataset)
```

### Counting Objects

```go
// Count groups and datasets
var groupCount, datasetCount int

file.Walk(func(path string, obj hdf5.Object) {
    switch obj.(type) {
    case *hdf5.Group:
        groupCount++
    case *hdf5.Dataset:
        datasetCount++
    }
})

fmt.Printf("File contains:\n")
fmt.Printf("  Groups: %d\n", groupCount)
fmt.Printf("  Datasets: %d\n", datasetCount)
```

---

## 🔄 Datatypes and Type Conversion

### Automatic Type Conversion

The library automatically converts HDF5 types to Go types:

| HDF5 Type | Go Type (Read()) | Go Type (Specific) |
|-----------|-----------------|-------------------|
| H5T_STD_I32LE/BE | float64 | int32 |
| H5T_STD_I64LE/BE | float64 | int64 |
| H5T_IEEE_F32LE/BE | float64 | float32 |
| H5T_IEEE_F64LE/BE | float64 | float64 |
| H5T_STRING (fixed) | string | string |
| H5T_STRING (variable) | string | string |
| H5T_COMPOUND | map[string]interface{} | CompoundValue |

### Handling Different Layouts

The library transparently handles all dataset layouts:

**Compact Layout** (data in object header):
```go
// Small datasets stored directly in object header
data, err := ds.Read()  // Works automatically
```

**Contiguous Layout** (sequential storage):
```go
// Data stored in one continuous block
data, err := ds.Read()  // Works automatically
```

**Chunked Layout** (chunked with indexing):
```go
// Data stored in chunks with B-tree index
data, err := ds.Read()  // Works automatically
```

### Compression Support

GZIP/Deflate compression is automatically decompressed:

```go
// Compressed dataset - decompression is automatic
data, err := ds.Read()
if err != nil {
    log.Fatalf("Decompression error: %v", err)
}
// data is already decompressed!
```

---

## ❌ Error Handling

### Common Errors and How to Handle Them

#### File Not Found

```go
file, err := hdf5.Open("missing.h5")
if err != nil {
    if os.IsNotExist(err) {
        fmt.Println("File does not exist")
    } else {
        fmt.Printf("Error opening file: %v\n", err)
    }
    return
}
defer file.Close()
```

#### Unsupported Datatype

```go
data, err := ds.Read()
if err != nil {
    if strings.Contains(err.Error(), "unsupported datatype") {
        fmt.Println("This dataset uses an unsupported datatype")
        fmt.Println("Supported: int32, int64, float32, float64, string, compound")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
    return
}
```

#### Corrupted or Invalid File

```go
file, err := hdf5.Open("data.h5")
if err != nil {
    if strings.Contains(err.Error(), "not an HDF5 file") {
        fmt.Println("File is not a valid HDF5 file")
    } else if strings.Contains(err.Error(), "unsupported superblock version") {
        fmt.Println("File uses unsupported HDF5 format version")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
    return
}
defer file.Close()
```

### Robust Error Handling Pattern

```go
func processHDF5File(filename string) error {
    // Open file
    file, err := hdf5.Open(filename)
    if err != nil {
        return fmt.Errorf("failed to open %s: %w", filename, err)
    }
    defer file.Close()

    // Process datasets
    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            // Try numeric read first
            data, err := ds.Read()
            if err != nil {
                // Try string read
                strings, err := ds.ReadStrings()
                if err != nil {
                    // Try compound read
                    compounds, err := ds.ReadCompound()
                    if err != nil {
                        log.Printf("Could not read %s: %v", path, err)
                        return
                    }
                    processCompounds(path, compounds)
                    return
                }
                processStrings(path, strings)
                return
            }
            processNumeric(path, data)
        }
    })

    return nil
}
```

---

## ⚡ Performance Tips

### 1. Close Files Promptly

```go
// Good: Use defer
file, _ := hdf5.Open("data.h5")
defer file.Close()

// Bad: Manual close may be forgotten
file, _ := hdf5.Open("data.h5")
// ... lots of code ...
file.Close()  // Easy to forget!
```

### 2. Avoid Repeated Walks

```go
// Bad: Multiple walks
file.Walk(func(path string, obj hdf5.Object) {
    // Find groups
})
file.Walk(func(path string, obj hdf5.Object) {
    // Find datasets
})

// Good: Single walk
var groups []*hdf5.Group
var datasets []*hdf5.Dataset

file.Walk(func(path string, obj hdf5.Object) {
    switch v := obj.(type) {
    case *hdf5.Group:
        groups = append(groups, v)
    case *hdf5.Dataset:
        datasets = append(datasets, v)
    }
})
```

### 3. Check Dataset Info Before Reading

```go
// Check size before reading large datasets
info, err := ds.Info()
if err == nil {
    // Parse info to check dataset size
    if strings.Contains(info, "Total size: ") {
        // Decide whether to read based on size
    }
}
```

### 4. Process Data Efficiently

```go
// Good: Process data as you read
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, _ := ds.Read()

        // Process immediately (streaming style)
        sum := 0.0
        for _, v := range data {
            sum += v
        }
        fmt.Printf("%s: average = %.2f\n", path, sum/float64(len(data)))
    }
})

// Bad: Store all data in memory first
type DatasetData struct {
    Path string
    Values []float64
}
var allData []DatasetData  // Can use lots of memory!

file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, _ := ds.Read()
        allData = append(allData, DatasetData{Path: path, Values: data})
    }
})
```

### 5. Reuse File Handles (Concurrency)

**Note**: Current version is not thread-safe. Each goroutine should have its own file handle:

```go
// For concurrent processing, open multiple handles
func processFileConcurrently(filename string) {
    var wg sync.WaitGroup

    // Open once to get object list
    file, _ := hdf5.Open(filename)
    var datasets []string
    file.Walk(func(path string, obj hdf5.Object) {
        if _, ok := obj.(*hdf5.Dataset); ok {
            datasets = append(datasets, path)
        }
    })
    file.Close()

    // Process each dataset in separate goroutine
    for _, dsPath := range datasets {
        wg.Add(1)
        go func(path string) {
            defer wg.Done()

            // Open separate file handle for this goroutine
            f, _ := hdf5.Open(filename)
            defer f.Close()

            // Process this dataset
            // ... (find and read dataset)
        }(dsPath)
    }

    wg.Wait()
}
```

---

## 📖 Complete Examples

### Example 1: Data Analysis

```go
package main

import (
    "fmt"
    "log"
    "math"
    "github.com/scigolib/hdf5"
)

func main() {
    file, err := hdf5.Open("measurements.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            data, err := ds.Read()
            if err != nil {
                return
            }

            // Calculate statistics
            var sum, min, max float64
            min = math.MaxFloat64
            max = -math.MaxFloat64

            for _, v := range data {
                sum += v
                if v < min {
                    min = v
                }
                if v > max {
                    max = v
                }
            }

            avg := sum / float64(len(data))

            // Calculate standard deviation
            var variance float64
            for _, v := range data {
                variance += (v - avg) * (v - avg)
            }
            stddev := math.Sqrt(variance / float64(len(data)))

            fmt.Printf("\nDataset: %s\n", path)
            fmt.Printf("  Count: %d\n", len(data))
            fmt.Printf("  Min: %.6f\n", min)
            fmt.Printf("  Max: %.6f\n", max)
            fmt.Printf("  Mean: %.6f\n", avg)
            fmt.Printf("  Std Dev: %.6f\n", stddev)
        }
    })
}
```

### Example 2: Metadata Extraction

```go
package main

import (
    "fmt"
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    file, err := hdf5.Open("experiment.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Extract file-level attributes
    root := file.Root()
    attrs, err := root.Attributes()
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("=== File Metadata ===")
    for _, attr := range attrs {
        fmt.Printf("%s: %v\n", attr.Name, attr.Value)
    }

    // Extract dataset metadata
    fmt.Println("\n=== Dataset Metadata ===")
    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            fmt.Printf("\n%s:\n", path)

            // Get attributes
            attrs, err := ds.Attributes()
            if err == nil {
                for _, attr := range attrs {
                    fmt.Printf("  @%s = %v\n", attr.Name, attr.Value)
                }
            }

            // Get dataset info
            info, err := ds.Info()
            if err == nil {
                fmt.Printf("  %s\n", info)
            }
        }
    })
}
```

---

## 📚 Next Steps

- **[Datatypes Guide](DATATYPES.md)** - Detailed type mapping and conversion
- **[Troubleshooting](TROUBLESHOOTING.md)** - Solutions to common problems
- **[FAQ](FAQ.md)** - Frequently asked questions
- **[Examples](../../examples/)** - More comprehensive examples

---

*Last Updated: 2025-11-13*
*Version: 0.12.0*
