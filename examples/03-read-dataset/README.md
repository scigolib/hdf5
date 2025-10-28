# Example 03: Read Datasets

> **Read numeric dataset values and verify correctness**

## What This Example Demonstrates

- Reading float64 datasets
- Reading 2D matrices (flattened to 1D)
- Reading multiple datasets from one file
- Verifying data correctness
- Getting dataset metadata

## Quick Start

```bash
go run main.go
```

## What You'll See

```
=== Reading simple_float64.h5 ===
Dataset info: Dataset: data
  Datatype: float64
  Layout: Compact
  Dimensions: [5]
  Total size: 40 bytes

Values: [1 2 3 4 5]
Expected: [1.0, 2.0, 3.0, 4.0, 5.0]
✓ All values match!

=== Reading matrix_2x3.h5 ===
Matrix (2x3): [1 2 3 4 5 6]
Expected: [1.0, 2.0, 3.0, 4.0, 5.0, 6.0]
✓ Matrix read successfully!

=== Reading multiple_datasets.h5 ===
Found 3 datasets
vector_x: [1 2 3 4 5]
vector_y: [10 20 30 40 50]
scalar_c: [42]
✓ All datasets read successfully!
```

## Code Walkthrough

### Finding a Dataset

```go
var dataset *hdf5.Dataset

file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok && ds.Name() == "data" {
        dataset = ds
    }
})
```

### Getting Dataset Info

```go
info, err := dataset.Info()
if err != nil {
    log.Fatalf("Failed to get info: %v", err)
}

fmt.Printf("Dataset info: %s\n", info)
```

**Info Output**:
- Datatype (int32, float64, etc.)
- Layout (compact, contiguous, chunked)
- Dimensions
- Total size in bytes

### Reading Values

```go
values, err := dataset.Read()
if err != nil {
    log.Fatalf("Failed to read: %v", err)
}

// values is []float64
fmt.Printf("Values: %v\n", values)
```

### Verifying Data

```go
expected := []float64{1.0, 2.0, 3.0, 4.0, 5.0}

if len(values) != len(expected) {
    log.Fatalf("Wrong count: got %d, expected %d",
        len(values), len(expected))
}

for i, v := range values {
    if v != expected[i] {
        log.Fatalf("Mismatch at [%d]: got %f, expected %f",
            i, v, expected[i])
    }
}
```

## Supported Data Types

| HDF5 Type | Go Read Type | Example |
|-----------|--------------|---------|
| int32 | float64 | `[1, 2, 3]` |
| int64 | float64 | `[100, 200, 300]` |
| float32 | float64 | `[1.1, 2.2, 3.3]` |
| float64 | float64 | `[1.23, 4.56, 7.89]` |

All numeric types are converted to `float64` for convenience.

## Common Use Cases

### 1. Statistical Analysis

```go
data, _ := ds.Read()

// Calculate statistics
var sum float64
for _, v := range data {
    sum += v
}
avg := sum / float64(len(data))

fmt.Printf("Mean: %.2f\n", avg)
```

### 2. Read and Process Immediately

```go
file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        data, err := ds.Read()
        if err == nil {
            // Process immediately (memory efficient)
            processData(data)
        }
    }
})
```

### 3. Collect All Datasets

```go
datasets := make(map[string]*hdf5.Dataset)

file.Walk(func(path string, obj hdf5.Object) {
    if ds, ok := obj.(*hdf5.Dataset); ok {
        datasets[ds.Name()] = ds
    }
})

// Access by name
if ds, ok := datasets["temperature"]; ok {
    data, _ := ds.Read()
    fmt.Printf("Temperature: %v\n", data)
}
```

## Troubleshooting

### "unsupported datatype" Error

**Cause**: Dataset uses unsupported type (array, enum, etc.).

**Solution**: Use `ReadStrings()` or `ReadCompound()` if appropriate.

### Empty Data Returned

**Cause**: Dataset might be empty or use unsupported layout.

**Solution**: Check with `Info()`:
```go
info, _ := ds.Info()
fmt.Println(info)  // Check dimensions and size
```

### Reading Large Datasets

**Note**: Entire dataset loads into memory. For multi-GB datasets:

```go
// Check size first
info, _ := ds.Info()
fmt.Println(info)  // See "Total size"

// Only read if acceptable
if /* size is ok */ {
    data, _ := ds.Read()
}
```

## Next Steps

- **[Example 04](../04-vlen-strings/)** - Variable-length strings
- **[Example 05](../05-comprehensive/)** - All features
- **[Datatypes Guide](../../docs/guides/DATATYPES.md)** - Type details

---

*Part of the HDF5 Go Library v0.10.0-beta*
