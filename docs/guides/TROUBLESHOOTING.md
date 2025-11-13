# Troubleshooting Guide

> **Solutions to common problems when using the HDF5 Go library**

---

## 📚 Table of Contents

- [File Opening Issues](#file-opening-issues)
- [Reading Data Issues](#reading-data-issues)
- [Type and Conversion Issues](#type-and-conversion-issues)
- [Performance Issues](#performance-issues)
- [Installation Issues](#installation-issues)
- [Debugging Tips](#debugging-tips)
- [Getting Help](#getting-help)

---

## 📂 File Opening Issues

### Problem: "not an HDF5 file" Error

**Error Message**:
```
Error: not an HDF5 file
```

**Possible Causes**:
1. File is not actually an HDF5 file
2. File is corrupted
3. File path is incorrect
4. File has wrong format/encoding

**Solutions**:

#### 1. Verify File is HDF5

```bash
# Using h5dump (if HDF5 tools installed)
h5dump -H yourfile.h5

# Using file command (Unix/Linux/macOS)
file yourfile.h5
# Should output: "HDF5 data file"

# Check file signature (first 8 bytes)
xxd -l 8 yourfile.h5
# Should show: 89 48 44 46 0d 0a 1a 0a  (.HDF....)
```

#### 2. Check File Path

```go
import "os"

filename := "data.h5"

// Check if file exists
if _, err := os.Stat(filename); os.IsNotExist(err) {
    fmt.Printf("File does not exist: %s\n", filename)
    return
}

// Check if readable
file, err := os.Open(filename)
if err != nil {
    fmt.Printf("Cannot open file: %v\n", err)
    return
}
file.Close()

// Now try HDF5 open
h5file, err := hdf5.Open(filename)
if err != nil {
    fmt.Printf("Not an HDF5 file: %v\n", err)
    return
}
defer h5file.Close()
```

#### 3. Verify File Not Corrupted

```go
// Try opening with detailed error handling
file, err := hdf5.Open("data.h5")
if err != nil {
    if strings.Contains(err.Error(), "not an HDF5 file") {
        fmt.Println("File signature is invalid")
        fmt.Println("Possible causes:")
        fmt.Println("  - File is corrupted")
        fmt.Println("  - File was truncated during transfer")
        fmt.Println("  - File is actually a different format (NetCDF, etc.)")
    }
    return
}
defer file.Close()
```

### Problem: "unsupported superblock version" Error

**Error Message**:
```
Error: unsupported superblock version: 1
```

**Cause**: File uses superblock version 1 (rare, used in very old HDF5 files).

**Supported Versions**: 0, 2, 3, 4 (HDF5 2.0.0)

**Solution**:

```bash
# Convert file using h5repack (if HDF5 tools installed)
h5repack -f SHUF -f GZIP=6 oldfile.h5 newfile.h5

# This creates a new file with modern format
```

Or open an issue at https://github.com/scigolib/hdf5/issues with:
- Your HDF5 file (if shareable)
- Output of `h5dump -H yourfile.h5`
- How the file was created

### Problem: "root group address beyond file size" Error

**Error Message**:
```
Error: root group address 12345678 beyond file size 1000000
```

**Cause**: File is corrupted or truncated.

**Solution**:

```bash
# Check actual file size
ls -lh yourfile.h5

# Compare with expected size
# If file is unexpectedly small, it may have been truncated during download/copy

# Try re-downloading or re-copying the file
```

### Problem: File Opens But No Data Visible

**Symptoms**: `file.Walk()` shows no objects or very few objects.

**Possible Causes**:
1. Empty file
2. Objects we don't support yet (external links, virtual datasets)
3. Corrupt file structure

**Debugging**:

```go
file, _ := hdf5.Open("data.h5")
defer file.Close()

// Check root group
root := file.Root()
fmt.Printf("Root group: %s\n", root.Name())
fmt.Printf("Children count: %d\n", len(root.Children()))

// List all children
for i, child := range root.Children() {
    fmt.Printf("  [%d] %s (type: %T)\n", i, child.Name(), child)
}

// Compare with h5ls output
// Run: h5ls yourfile.h5
```

---

## 📊 Reading Data Issues

### Problem: "unsupported datatype" Error

**Error Message**:
```
Error: unsupported datatype: H5T_ARRAY
Error: unsupported datatype class: 10
```

**Cause**: Dataset uses a datatype not yet implemented.

**Supported Types**: int32, int64, float32, float64, strings, compounds

**Unsupported**: arrays, enums, references, opaque, time

**Solution**:

Check datatype before reading:

```go
info, err := ds.Info()
if err == nil {
    fmt.Println(info)  // Shows datatype information
}

// Try different read methods
data, err := ds.Read()  // For numeric types
if err != nil {
    strings, err := ds.ReadStrings()  // For string types
    if err != nil {
        compounds, err := ds.ReadCompound()  // For compound types
        if err != nil {
            log.Printf("Dataset uses unsupported datatype: %v", err)
        }
    }
}
```

**Workaround**: Convert file using Python:

```python
import h5py
import numpy as np

# Read with h5py (supports all types)
with h5py.File('original.h5', 'r') as f_in:
    with h5py.File('converted.h5', 'w') as f_out:
        # Convert enum to int
        data = f_in['enum_dataset'][:]
        f_out.create_dataset('enum_dataset', data=data.astype('i4'))

        # Convert array to flattened
        arr = f_in['array_dataset'][:]  # Shape: (100, 3)
        for i in range(3):
            f_out.create_dataset(f'array_{i}', data=arr[:, i])
```

### Problem: Empty or Nil Data Returned

**Symptoms**: `Read()` returns empty slice or nil.

**Possible Causes**:
1. Dataset is actually empty
2. Unsupported layout (e.g., external storage)
3. Compression format not supported

**Debugging**:

```go
// Check dataset size
info, err := ds.Info()
if err == nil {
    fmt.Println(info)  // Shows dimensions and size
}

// Try reading
data, err := ds.Read()
if err != nil {
    fmt.Printf("Error: %v\n", err)
} else if len(data) == 0 {
    fmt.Println("Dataset is empty")
} else {
    fmt.Printf("Read %d values\n", len(data))
}
```

### Problem: String Decoding Issues

**Symptoms**: Strings contain garbage characters or are truncated.

**Causes**:
1. Encoding mismatch (ASCII vs UTF-8)
2. Fixed-length strings with padding issues
3. Non-standard string format

**Solution**:

```go
strings, err := ds.ReadStrings()
if err != nil {
    log.Printf("String read error: %v", err)
    return
}

// Check for encoding issues
for i, str := range strings {
    // Trim any null bytes or spaces
    clean := strings.TrimRight(str, "\x00 ")

    // Check for non-printable characters
    if !utf8.ValidString(clean) {
        fmt.Printf("String %d has encoding issues\n", i)
    }

    fmt.Printf("[%d] %q\n", i, clean)
}
```

### Problem: Compression Error

**Error Message**:
```
Error: gzip: invalid header
Error: unsupported filter: 6
```

**Cause**: Unsupported compression format.

**Supported**: GZIP/Deflate (filter ID 1)

**Unsupported**: SZIP (2), LZF (32000), BZIP2 (307)

**Solution**:

Convert file to use GZIP:

```bash
# Using h5repack
h5repack -f GZIP=6 original.h5 gzip_compressed.h5
```

Or in Python:

```python
import h5py

with h5py.File('original.h5', 'r') as f_in:
    with h5py.File('gzip_compressed.h5', 'w') as f_out:
        for name in f_in:
            data = f_in[name][:]
            f_out.create_dataset(name, data=data,
                                 compression='gzip',
                                 compression_opts=6)
```

---

## 🔄 Type and Conversion Issues

### Problem: Precision Loss with Large Integers

**Symptom**: int64 values > 2^53 lose precision when read.

**Cause**: Automatic conversion to float64.

**Example**:
```go
// HDF5 int64: 9223372036854775807
// Read as float64: 9223372036854776000  (rounded!)
```

**Solution**:

Check if values are in safe range:

```go
const MaxSafeInt = 9007199254740992  // 2^53

data, _ := ds.Read()
for i, v := range data {
    if math.Abs(v) > MaxSafeInt {
        log.Printf("Warning: value at [%d] may have lost precision\n", i)
        log.Printf("  Value: %.0f\n", v)
        log.Printf("  Safe range: ±%.0f\n", MaxSafeInt)
    }
}
```

**Future**: Direct int64 reading will be available in v1.0.0.

### Problem: Type Assertion Panic

**Error**:
```
panic: interface conversion: interface {} is float64, not int32
```

**Cause**: Incorrect type assertion on attribute value or compound field.

**Solution**:

Always use safe type assertion:

```go
// Bad: Direct assertion (can panic)
value := attr.Value.(int32)

// Good: Safe assertion with check
if value, ok := attr.Value.(int32); ok {
    fmt.Printf("int32: %d\n", value)
} else {
    fmt.Printf("Not int32, actual type: %T\n", attr.Value)
}

// Best: Use type switch
switch v := attr.Value.(type) {
case int32:
    fmt.Printf("int32: %d\n", v)
case int64:
    fmt.Printf("int64: %d\n", v)
case float64:
    fmt.Printf("float64: %.6f\n", v)
case string:
    fmt.Printf("string: %q\n", v)
default:
    fmt.Printf("unknown: %T = %v\n", v, v)
}
```

---

## ⚡ Performance Issues

### Problem: Slow File Opening

**Symptom**: `Open()` takes several seconds for large files.

**Cause**: Large group structures require traversing B-trees and loading many objects.

**Solutions**:

```go
// 1. Profile to find bottleneck
import "runtime/pprof"

f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()

file, _ := hdf5.Open("largefile.h5")
defer file.Close()

// 2. Avoid unnecessary Walk() calls
// Bad: Multiple walks
file.Walk(countGroups)
file.Walk(countDatasets)

// Good: Single walk
var groups, datasets int
file.Walk(func(path string, obj hdf5.Object) {
    switch obj.(type) {
    case *hdf5.Group:
        groups++
    case *hdf5.Dataset:
        datasets++
    }
})
```

### Problem: High Memory Usage

**Symptom**: Program uses excessive memory when reading large datasets.

**Causes**:
1. Reading entire dataset into memory
2. Keeping multiple file handles open
3. Not releasing buffers

**Solutions**:

```go
// 1. Process data incrementally
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, _ := ds.Read()

        // Process immediately, don't store
        processData(data)

        // data will be garbage collected after this function
    }
})

// 2. Close files promptly
file, _ := hdf5.Open("data.h5")
// ... use file ...
file.Close()  // Don't wait for defer if processing many files

// 3. Force garbage collection if needed (rare)
runtime.GC()
```

### Problem: Slow Dataset Reading

**Symptom**: Reading individual datasets is slow.

**Possible Causes**:
1. Chunked dataset with many small chunks
2. High compression level
3. Disk I/O bottleneck

**Debugging**:

```go
import "time"

start := time.Now()
data, err := ds.Read()
elapsed := time.Since(start)

fmt.Printf("Read %d values in %v\n", len(data), elapsed)
if elapsed > time.Second {
    info, _ := ds.Info()
    fmt.Println("Dataset info:", info)
    // Check if heavily compressed or chunked
}
```

**Solutions**: File optimization (using h5repack):

```bash
# Optimize chunk size and compression
h5repack -l CHUNK=1000x1000 -f GZIP=3 input.h5 output.h5

# Use less compression (faster decompression)
h5repack -f GZIP=3 input.h5 output.h5  # Level 3 instead of 9
```

---

## 💻 Installation Issues

### Problem: "command not found: go"

**Cause**: Go is not installed or not in PATH.

**Solution**:

```bash
# Install Go from https://golang.org/dl/

# Verify installation
go version  # Should show Go 1.25+

# Check PATH
echo $PATH  # Should include Go bin directory
```

### Problem: "go get" Fails

**Error**:
```
go: github.com/scigolib/hdf5: unknown revision
```

**Solutions**:

```bash
# 1. Check internet connection
ping github.com

# 2. Set up proxy if behind firewall
export GOPROXY=https://proxy.golang.org,direct

# 3. Clear module cache
go clean -modcache

# 4. Try again
go get github.com/scigolib/hdf5@latest
```

### Problem: Import Error in IDE

**Symptom**: IDE shows "package not found" for hdf5 import.

**Solution**:

```bash
# 1. Ensure module is downloaded
go get github.com/scigolib/hdf5

# 2. Run go mod tidy
go mod tidy

# 3. Restart IDE / reload workspace
# VS Code: Reload Window
# GoLand: Invalidate Caches and Restart
```

---

## 🔍 Debugging Tips

### Enable Verbose Error Messages

```go
// Wrap operations with detailed error context
file, err := hdf5.Open("data.h5")
if err != nil {
    fmt.Printf("Failed to open file: %v\n", err)

    // Print full error chain (if using errors.Wrap)
    fmt.Printf("Full error: %+v\n", err)
    return
}
defer file.Close()
```

### Inspect File with h5dump

```bash
# View file structure
h5dump -H file.h5

# View specific dataset
h5dump -d /path/to/dataset file.h5

# View attributes
h5dump -A file.h5
```

### Compare with Python h5py

```python
import h5py

with h5py.File('data.h5', 'r') as f:
    # Print all objects
    f.visititems(print)

    # Check specific dataset
    ds = f['/temperature']
    print(f"Shape: {ds.shape}")
    print(f"Dtype: {ds.dtype}")
    print(f"Data: {ds[:]}")
```

### Add Logging

```go
import "log"

// Set up logging
log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

// Log operations
log.Println("Opening file...")
file, err := hdf5.Open("data.h5")
if err != nil {
    log.Fatalf("Open failed: %v", err)
}
defer file.Close()
log.Println("File opened successfully")

// Log walk operations
file.Walk(func(path string, obj hdf5.Object) {
    log.Printf("Visiting: %s (type: %T)", path, obj)
})
```

### Create Minimal Reproduction

```go
// Minimal example to report bug
package main

import (
    "fmt"
    "log"
    "github.com/scigolib/hdf5"
)

func main() {
    file, err := hdf5.Open("problem.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Minimal code that reproduces issue
    file.Walk(func(path string, obj hdf5.Object) {
        if ds, ok := obj.(*hdf5.Dataset); ok {
            _, err := ds.Read()
            if err != nil {
                fmt.Printf("Error reading %s: %v\n", path, err)
            }
        }
    })
}
```

---

## 📞 Getting Help

### Before Asking for Help

1. **Check this guide** for common issues
2. **Search existing issues**: https://github.com/scigolib/hdf5/issues
3. **Read the documentation**: https://github.com/scigolib/hdf5/tree/main/docs
4. **Try the examples**: https://github.com/scigolib/hdf5/tree/main/examples

### When Reporting Issues

Include:

1. **Go version**: Output of `go version`
2. **Library version**: `go list -m github.com/scigolib/hdf5`
3. **Operating system**: Windows/Linux/macOS
4. **Error message**: Full error output
5. **Minimal code**: Smallest code that reproduces issue
6. **HDF5 file**: If possible, share problematic file or `h5dump -H` output
7. **What you tried**: Solutions attempted

**Good issue report template**:

**Go version**: go1.25.1 linux/amd64
**Library version**: github.com/scigolib/hdf5 vX.Y.Z (use `go list -m github.com/scigolib/hdf5`)
**OS**: Ubuntu 22.04

**Problem**: Cannot read string dataset

**Error message**:
```
Error: unsupported datatype class: 3
```

**Code**:
```go
file, _ := hdf5.Open("strings.h5")
defer file.Close()

// Trying to read strings dataset
// ...
```

**HDF5 file info**:
```
$ h5dump -H strings.h5
HDF5 "strings.h5" {
GROUP "/" {
   DATASET "names" {
      DATATYPE  H5T_STRING {
         STRSIZE H5T_VARIABLE;
      }
      ...
   }
}
}
```

**What I tried**:
- ReadStrings() method
- Read() method
- Checked file with Python h5py (works fine there)


### Where to Get Help

- **GitHub Issues**: https://github.com/scigolib/hdf5/issues (bugs, feature requests)
- **GitHub Discussions**: https://github.com/scigolib/hdf5/discussions (questions, ideas)
- **Documentation**: https://github.com/scigolib/hdf5/tree/main/docs

---

## 📚 Related Guides

- **[Installation Guide](INSTALLATION.md)** - Setup and verification
- **[Reading Data Guide](READING_DATA.md)** - Comprehensive reading guide
- **[Datatypes Guide](DATATYPES.md)** - Type conversion details
- **[FAQ](FAQ.md)** - Frequently asked questions

---

*Last Updated: 2025-11-13*
