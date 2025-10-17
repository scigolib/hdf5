# HDF5 Go Library Examples

This directory contains standalone examples demonstrating the HDF5 Go library features.

## Running Examples

Each example is in its own directory with a `main.go` file. Run them using `go run`:

```bash
# Basic usage example
go run examples/01-basic/main.go

# List objects in HDF5 file
go run examples/02-list-objects/main.go

# Read dataset values
go run examples/03-read-dataset/main.go

# Read variable-length strings
go run examples/04-vlen-strings/main.go

# Comprehensive feature demonstration
go run examples/05-comprehensive/main.go
```

Or build and run:

```bash
cd examples/01-basic
go build
./01-basic   # or 01-basic.exe on Windows
```

## Examples Overview

### 01-basic - Basic Usage
**File**: `01-basic/main.go`

Demonstrates:
- Opening HDF5 files
- Reading superblock information
- Walking file structure
- Automatic test file generation with Python

This is the best starting point for new users.

### 02-list-objects - File Navigation
**File**: `02-list-objects/main.go`

Demonstrates:
- Traversing groups and datasets
- Reading object hierarchy
- Walking nested structures

### 03-read-dataset - Dataset Reading
**File**: `03-read-dataset/main.go`

Demonstrates:
- Reading numeric datasets (floats, integers)
- Reading string datasets
- Reading compound datasets (structs)
- Handling different data layouts

### 04-vlen-strings - Variable-Length Strings
**File**: `04-vlen-strings/main.go`

Demonstrates:
- Reading variable-length strings via Global Heap
- Compound types with vlen string members
- Global Heap functionality

### 05-comprehensive - Full Feature Demo
**File**: `05-comprehensive/main.go`

Demonstrates ALL library features:
- Superblock versions (0, 2, 3)
- Object headers
- Groups (traditional + modern)
- Datasets (compact, contiguous, chunked)
- Compression (GZIP)
- Datatypes (numeric, strings, compounds)
- B-trees and heaps

## Test Files

Examples expect test HDF5 files in `../../testdata/`:
- `v0.h5` - HDF5 version 0 (earliest format)
- `v2.h5` - HDF5 version 2 (1.8.x format)
- `v3.h5` - HDF5 version 3 (latest format)
- `with_groups.h5` - File with nested groups
- `vlen_strings.h5` - File with variable-length strings

Most examples auto-generate test files if Python with h5py is available.

## Requirements

- Go 1.24 or later
- For test file generation: Python 3 with `h5py` and `numpy`

Install Python dependencies:
```bash
pip install h5py numpy
```

## API Usage Patterns

### Opening Files
```go
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatal(err)
}
defer file.Close()
```

### Walking File Structure
```go
file.Walk(func(path string, obj hdf5.Object) {
    switch v := obj.(type) {
    case *hdf5.Group:
        fmt.Printf("Group: %s\n", path)
    case *hdf5.Dataset:
        fmt.Printf("Dataset: %s\n", path)
    }
})
```

### Reading Datasets
```go
// Numeric data
values, err := dataset.Read()

// String data
strings, err := dataset.ReadStrings()

// Compound data (structs)
records, err := dataset.ReadCompound()
```

## Building All Examples

From repository root:
```bash
make examples
```

This builds all examples to verify they compile correctly.

## See Also

- [Main README](../README.md) - Library overview
- [CLAUDE.md](../.claude/CLAUDE.md) - Development documentation
- [API Documentation](https://pkg.go.dev/github.com/scigolib/hdf5)
