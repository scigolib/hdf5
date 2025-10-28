# Example 01: Basic Usage

> **Learn the fundamentals of opening HDF5 files and walking the file structure**

## What This Example Demonstrates

- Opening HDF5 files
- Reading superblock information
- Walking through file structure using `Walk()`
- Identifying groups and datasets
- Automatic test file generation with Python (optional)

## Quick Start

```bash
# From this directory
go run main.go

# Or from repository root
go run examples/01-basic/main.go
```

## What You'll See

The example will:
1. Generate test HDF5 files (if Python/h5py available)
2. Open each test file
3. Display superblock version
4. Walk through all objects (groups and datasets)
5. Show object hierarchy

**Example Output**:
```
Creating test HDF5 files...
Created: testdata/v0.h5
Created: testdata/v2.h5
Created: testdata/v3.h5
Created: testdata/with_groups.h5

===== Testing file: testdata/v0.h5 =====
File opened successfully. Superblock version: 0
File structure:
[Group] / (1 children)
[Dataset] /test

===== Testing file: testdata/v2.h5 =====
File opened successfully. Superblock version: 2
File structure:
[Group] / (1 children)
[Dataset] /data

===== Testing file: testdata/with_groups.h5 =====
File opened successfully. Superblock version: 2
File structure:
[Group] / (2 children)
[Dataset] /dataset1
[Group] /subgroup/ (2 children)
  [Dataset] /subgroup/dataset2
  [Group] /subgroup/nested_group/ (1 children)
    [Dataset] /subgroup/nested_group/nested_data
```

## Code Walkthrough

### Opening a File

```go
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatalf("Failed to open file: %v", err)
}
defer file.Close()  // Always close!
```

### Getting File Information

```go
// Superblock version (0, 2, or 3)
version := file.SuperblockVersion()
fmt.Println("Superblock version:", version)
```

### Walking File Structure

```go
file.Walk(func(path string, obj hdf5.Object) {
    switch v := obj.(type) {
    case *hdf5.Group:
        fmt.Printf("[Group] %s (%d children)\n",
            path, len(v.Children()))

    case *hdf5.Dataset:
        fmt.Printf("[Dataset] %s\n", path)

    default:
        fmt.Printf("[Unknown] %s\n", path)
    }
})
```

## Key Concepts

### HDF5 Object Types

The library provides two main object types:

| Type | Description | Example Path |
|------|-------------|--------------|
| `*hdf5.Group` | Container for other objects | `/`, `/experiments/` |
| `*hdf5.Dataset` | Array data storage | `/temperature`, `/data` |

### File Structure

HDF5 files are organized hierarchically:

```
/ (root group)
├── dataset1
├── subgroup/
│   ├── dataset2
│   └── nested_group/
│       └── nested_data
└── dataset3
```

### Superblock Versions

- **Version 0**: Original HDF5 format (1998-2006)
- **Version 2**: Modern format (HDF5 1.8+, 2008+)
- **Version 3**: Latest format with SWMR support (HDF5 1.10+, 2016+)

## Test File Generation

The example includes automatic test file generation:

```go
func createTestFiles() error {
    // Checks for Python with h5py
    if !checkPythonDependencies() {
        log.Printf("Python dependencies missing...")
        return nil
    }

    // Generates Python script
    // Creates test HDF5 files
    // ...
}
```

**Requirements**:
- Python 3
- h5py: `pip install h5py`
- numpy: `pip install numpy`

If Python is not available, you can provide your own HDF5 files.

## Common Use Cases

### 1. Inspect Unknown HDF5 File

```go
file, _ := hdf5.Open("unknown.h5")
defer file.Close()

var groupCount, datasetCount int

file.Walk(func(path string, obj hdf5.Object) {
    switch obj.(type) {
    case *hdf5.Group:
        groupCount++
    case *hdf5.Dataset:
        datasetCount++
    }
})

fmt.Printf("File contains: %d groups, %d datasets\n",
    groupCount, datasetCount)
```

### 2. Find Specific Object

```go
func findDataset(file *hdf5.File, name string) *hdf5.Dataset {
    var result *hdf5.Dataset

    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            if ds.Name() == name {
                result = ds
            }
        }
    })

    return result
}
```

### 3. Print File Tree

```go
func printTree(file *hdf5.File) {
    file.Walk(func(path string, obj hdf5.Object) {
        // Calculate indent based on depth
        depth := strings.Count(path, "/") - 1
        indent := strings.Repeat("  ", depth)

        switch v := obj.(type) {
        case *hdf5.Group:
            if path != "/" {
                fmt.Printf("%s📁 %s/\n", indent, v.Name())
            }
        case *hdf5.Dataset:
            fmt.Printf("%s📄 %s\n", indent, v.Name())
        }
    })
}
```

## Troubleshooting

### "Failed to open file" Error

**Cause**: File doesn't exist or path is incorrect.

**Solution**:
```go
import "os"

// Check if file exists
if _, err := os.Stat("data.h5"); os.IsNotExist(err) {
    fmt.Println("File does not exist")
}
```

### "not an HDF5 file" Error

**Cause**: File is not in HDF5 format.

**Solution**: Verify with `h5dump -H file.h5` (if HDF5 tools installed).

### Python Dependencies Missing

**Cause**: Python or h5py not installed.

**Solution**:
```bash
# Install Python dependencies
pip install h5py numpy

# Or skip test generation and use your own HDF5 files
```

## Next Steps

After understanding basic usage:

1. **[Example 02](../02-list-objects/)** - Advanced file navigation
2. **[Example 03](../03-read-dataset/)** - Reading dataset values
3. **[Reading Data Guide](../../docs/guides/READING_DATA.md)** - Comprehensive guide

## Related Documentation

- **[Installation Guide](../../docs/guides/INSTALLATION.md)** - Setup instructions
- **[Quick Start Guide](../../docs/guides/QUICKSTART.md)** - 5-minute intro
- **[API Reference](https://pkg.go.dev/github.com/scigolib/hdf5)** - GoDoc

---

*Part of the HDF5 Go Library v0.10.0-beta*
