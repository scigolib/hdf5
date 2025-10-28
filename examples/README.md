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

Each example has its own README.md with detailed explanations, code walkthroughs, and use cases.

### [01-basic](01-basic/) - Basic Usage
**Best starting point for new users!**

Demonstrates:
- Opening HDF5 files
- Reading superblock information
- Walking file structure
- Automatic test file generation with Python

📖 [Full Documentation →](01-basic/README.md)

### [02-list-objects](02-list-objects/) - File Navigation
Demonstrates:
- Traversing groups and datasets
- Reading object hierarchy
- Walking nested structures
- Building object indexes

📖 [Full Documentation →](02-list-objects/README.md)

### [03-read-dataset](03-read-dataset/) - Dataset Reading
Demonstrates:
- Reading numeric datasets (floats, integers)
- Reading matrices (2D data)
- Reading multiple datasets
- Getting dataset metadata
- Data verification

📖 [Full Documentation →](03-read-dataset/README.md)

### [04-vlen-strings](04-vlen-strings/) - Variable-Length Strings
Demonstrates:
- Reading variable-length strings via Global Heap
- Global Heap architecture
- String storage mechanisms
- Compound types with vlen string members

📖 [Full Documentation →](04-vlen-strings/README.md)

### [05-comprehensive](05-comprehensive/) - Full Feature Demo
**Complete demonstration of all library features!**

Demonstrates:
- All superblock versions (0, 2, 3)
- Object headers (v1 + v2)
- All group formats
- All dataset layouts
- GZIP compression
- All datatypes
- Production readiness showcase (~98%)

📖 [Full Documentation →](05-comprehensive/README.md)

## Test Files

Examples expect test HDF5 files in `../../testdata/`:
- `v0.h5` - HDF5 version 0 (earliest format)
- `v2.h5` - HDF5 version 2 (1.8.x format)
- `v3.h5` - HDF5 version 3 (latest format)
- `with_groups.h5` - File with nested groups
- `vlen_strings.h5` - File with variable-length strings

Most examples auto-generate test files if Python with h5py is available.

## Requirements

- Go 1.25 or later
- For test file generation (optional): Python 3 with `h5py` and `numpy`

Install Python dependencies:
```bash
pip install h5py numpy
```

**Note**: Python is optional. You can use your own HDF5 files or download test files.

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

## Documentation

### User Guides
- **[Installation Guide](../docs/guides/INSTALLATION.md)** - Setup and verification
- **[Quick Start Guide](../docs/guides/QUICKSTART.md)** - Get started in 5 minutes
- **[Reading Data Guide](../docs/guides/READING_DATA.md)** - Comprehensive reading guide
- **[Datatypes Guide](../docs/guides/DATATYPES.md)** - Type mapping and conversion
- **[Troubleshooting](../docs/guides/TROUBLESHOOTING.md)** - Common issues and solutions
- **[FAQ](../docs/guides/FAQ.md)** - Frequently asked questions

### Reference
- **[Main README](../README.md)** - Library overview
- **[ROADMAP](../ROADMAP.md)** - Future plans
- **[API Documentation](https://pkg.go.dev/github.com/scigolib/hdf5)** - GoDoc
- **[Architecture Overview](../docs/architecture/OVERVIEW.md)** - How it works

---

*Last Updated: 2025-10-29*
*Version: 0.10.0-beta*
