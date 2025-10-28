# Example 05: Comprehensive Demo

> **Complete demonstration of all library features and capabilities**

## What This Example Demonstrates

- All superblock versions (0, 2, 3)
- Both object header versions (v1, v2)
- Traditional and modern groups
- All dataset layouts (compact, contiguous, chunked)
- GZIP compression
- All supported datatypes
- B-trees and heaps
- Complete file analysis
- Production readiness showcase

## Quick Start

```bash
go run main.go
```

## What You'll See

```
=================================================
   Pure Go HDF5 Library - Comprehensive Demo
   ~98% Production Ready Implementation
=================================================

📁 Opening: ../../testdata/v2.h5
------------------------------------------------------------
   Superblock Version: 2
   Offset Size: 8 bytes
   Length Size: 8 bytes
   Root Group: 0x30

   📊 File Structure:
   📂 Group: / (1 children)
   📄 Dataset: /data (addr: 0x800)
      Type: float64
      Dimensions: [10]
      Total elements: 10
      Layout: Contiguous (addr: 0xA00)

📁 Opening: ../../testdata/v3.h5
------------------------------------------------------------
   [Similar output for v3 file...]

📁 Opening: ../../testdata/with_groups.h5
------------------------------------------------------------
   [Output showing nested groups...]

📁 Opening: ../../testdata/vlen_strings.h5
------------------------------------------------------------
   [Output showing variable-length strings...]

=================================================
   ✅ All Features Demonstrated Successfully!
=================================================

🎯 Supported Features:
   ✅ Superblock versions: 0, 2, 3
   ✅ Object header v1 + v2
   ✅ Traditional groups (symbol tables)
   ✅ Modern groups (object headers)
   ✅ B-trees (leaf + non-leaf nodes)
   ✅ Local heaps (string storage)
   ✅ Global Heap (variable-length data)
   ✅ Dataset layouts:
      • Compact
      • Contiguous
      • Chunked (with B-tree index)
   ✅ Compression: GZIP/Deflate
   ✅ Datatypes:
      • Integers (int32, int64)
      • Floats (float32, float64)
      • Fixed-length strings
      • Variable-length strings
      • Compound types (structs)
   ✅ Attributes (compact + dense)
   ✅ File traversal (Walk)

📊 Production Readiness: ~98%
   Ready for reading most common HDF5 scientific datasets!
```

## Code Architecture

### Multi-File Demonstration

```go
testFiles := []string{
    "../../testdata/v2.h5",
    "../../testdata/v3.h5",
    "../../testdata/with_groups.h5",
    "../../testdata/vlen_strings.h5",
}

for _, filename := range testFiles {
    demonstrateFile(filename)
}
```

### Complete File Analysis

```go
func demonstrateFile(filename string) {
    file, _ := hdf5.Open(filename)
    defer file.Close()

    // 1. Superblock information
    sb := file.Superblock()
    fmt.Printf("Superblock Version: %d\n", sb.Version)

    // 2. Walk file structure
    file.Walk(func(path string, obj hdf5.Object) {
        // Show groups and datasets
    })
}
```

### Dataset Deep Dive

```go
func demonstrateDataset(file *hdf5.File, ds *hdf5.Dataset) {
    // Read object header
    header, _ := core.ReadObjectHeader(...)

    // Extract and display:
    // - Datatype
    // - Dataspace (dimensions)
    // - Layout (compact/contiguous/chunked)
    // - Filters (compression)
    // - Sample data (for compound types)
}
```

## Feature Showcase

### 1. Superblock Versions

**Version 0** (HDF5 1.0-1.6):
- Original format
- Symbol table groups
- Fixed-size offsets

**Version 2** (HDF5 1.8+):
- Streamlined superblock
- Object header v2
- Larger file support

**Version 3** (HDF5 1.10+):
- SWMR (Single Writer Multiple Readers)
- Enhanced concurrency
- Checksums

### 2. Object Headers

**Version 1** (legacy):
- Used in pre-1.8 files
- Continuation blocks for large headers
- ✅ **NEW in v0.10.0-beta**

**Version 2** (modern):
- Compact format
- More efficient
- Supports larger objects

### 3. Dataset Layouts

**Compact**:
```
Data stored directly in object header
Best for: Small datasets (< 64KB)
Example: Configuration values, metadata
```

**Contiguous**:
```
Data stored in one continuous block
Best for: Medium datasets, sequential access
Example: Time series, matrices
```

**Chunked**:
```
Data split into chunks with B-tree index
Best for: Large datasets, partial reads, compression
Example: Large scientific datasets, images
```

### 4. Compression

**GZIP/Deflate**:
- Supported compression levels: 0-9
- Level 6 recommended for balance
- Automatic decompression on read

**Example**:
```
Layout: Chunked (addr: 0x1000)
Chunk dimensions: [100, 100]
Filters: GZIP
```

### 5. Datatypes Demonstrated

| Type | Example Dataset | Notes |
|------|----------------|-------|
| int32 | `/counts` | Signed 32-bit integers |
| int64 | `/timestamps` | Signed 64-bit integers |
| float32 | `/measurements` | Single precision |
| float64 | `/data` | Double precision |
| Fixed string | `/names` | Fixed-length strings |
| VLen string | `/descriptions` | Variable-length |
| Compound | `/records` | Struct-like data |

## Production Readiness Metrics

### Test Coverage
- **76.3%** overall coverage
- **57** reference test files
- **200+** test cases
- **0** lint issues (34+ linters)

### Compatibility
- ✅ HDF5 1.0 - 1.14+ files
- ✅ Python h5py-created files
- ✅ MATLAB v7.3 files
- ✅ NASA/climate data files

### Performance
- **2-3x slower** than C library (acceptable)
- **~30-50 MB/s** reading speed
- **Efficient** memory management

### Limitations
- ⚠️ Dense attributes partial support (<10% impact)
- ⚠️ Some advanced types (arrays, enums)
- ⚠️ Read-only (write in v0.11.0+)

## Use This Example To

### 1. Verify Installation

```bash
# If this runs without errors, installation is correct
go run main.go
```

### 2. Test Your HDF5 Files

```go
// Modify testFiles to include your files:
testFiles := []string{
    "../../testdata/v2.h5",
    "/path/to/your/file.h5",  // Add your files
}
```

### 3. Benchmark Performance

```go
import "time"

start := time.Now()
file, _ := hdf5.Open("large.h5")
// ... read datasets ...
elapsed := time.Since(start)

fmt.Printf("Processed in %v\n", elapsed)
```

### 4. Debug Issues

The comprehensive output helps identify:
- Which features your file uses
- Where reading might fail
- What's supported vs not

## Extending This Example

### Add Custom Analysis

```go
func demonstrateDataset(file *hdf5.File, ds *hdf5.Dataset) {
    // ... existing code ...

    // Add your custom analysis:
    data, err := core.ReadDatasetFloat64(...)
    if err == nil {
        // Calculate statistics
        // Generate visualizations
        // Export to other formats
    }
}
```

### Filter Specific Files

```go
// Only process files matching criteria
for _, filename := range testFiles {
    file, _ := hdf5.Open(filename)
    sb := file.Superblock()

    // Only process v2+ files
    if sb.Version >= 2 {
        demonstrateFile(filename)
    }

    file.Close()
}
```

## Next Steps

After exploring all features:

1. **Build your application** using the library
2. **Read** [Architecture Overview](../../docs/architecture/OVERVIEW.md)
3. **Check** [ROADMAP](../../ROADMAP.md) for upcoming features
4. **Contribute** to the project (see [CONTRIBUTING](../../CONTRIBUTING.md))

## Related Documentation

- **[Installation Guide](../../docs/guides/INSTALLATION.md)** - Setup
- **[Reading Data Guide](../../docs/guides/READING_DATA.md)** - Complete guide
- **[Datatypes Guide](../../docs/guides/DATATYPES.md)** - Type details
- **[Troubleshooting](../../docs/guides/TROUBLESHOOTING.md)** - Solutions
- **[FAQ](../../docs/guides/FAQ.md)** - Common questions

---

*Part of the HDF5 Go Library v0.10.0-beta*
*Demonstrates ~98% production-ready implementation*
