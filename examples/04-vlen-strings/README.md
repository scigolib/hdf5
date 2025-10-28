# Example 04: Variable-Length Strings

> **Demonstrates Global Heap and variable-length string support**

## What This Example Demonstrates

- Opening files with variable-length strings
- Global Heap functionality
- String storage architecture
- Address tracking

## Quick Start

```bash
go run main.go
```

## What You'll See

```
Opening HDF5 file with variable-length strings: ../testdata/vlen_strings.h5

File opened successfully. Superblock version: 2
Offset size: 8 bytes
Length size: 8 bytes

=== Dataset: /vlen_strings ===
Address: 0x800
✓ Global Heap support implemented!
  - ParseGlobalHeapReference: Extracts heap address + object index
  - ReadGlobalHeapCollection: Loads heap collection from file
  - GetObject: Retrieves string data from heap

Variable-length string support is ready! 🎯
```

## What is Global Heap?

**Global Heap** is HDF5's storage mechanism for variable-length data:

- **Variable-length strings**: Different string lengths
- **Variable-length arrays**: Different array sizes
- **Object references**: Pointers to other objects

### Storage Architecture

```
Dataset → Global Heap Reference → Global Heap → String Data
```

**Example**:
```
Dataset contains:
  Reference 1: {heap_addr: 0x1000, object_index: 0}
  Reference 2: {heap_addr: 0x1000, object_index: 1}

Global Heap at 0x1000:
  Object 0: "Hello World"
  Object 1: "Variable Length String"
```

## Global Heap Functions

### 1. Parse Reference

```go
// Extract heap address and object index from reference bytes
heapAddr, objIndex := ParseGlobalHeapReference(refBytes, superblock)
```

### 2. Load Heap Collection

```go
// Read entire Global Heap collection from file
collection := ReadGlobalHeapCollection(file, heapAddr, superblock)
```

### 3. Get Object

```go
// Retrieve specific object data
stringData := collection.GetObject(objIndex)
```

## Use Cases

### Reading Variable-Length Strings

```go
// When reading datasets with vlen strings:
strings, err := ds.ReadStrings()
if err != nil {
    log.Fatal(err)
}

// strings is []string with different lengths:
// ["short", "a much longer string", "x"]
```

### Compound Types with VLen Strings

```go
// Compound type with vlen string field:
// {
//   "id": int32,
//   "name": variable-length string
// }

compounds, err := ds.ReadCompound()
// Each compound contains string field resolved via Global Heap
```

## Fixed vs Variable-Length Strings

| Type | Storage | Example |
|------|---------|---------|
| **Fixed** | In dataset directly | All strings padded to 20 bytes |
| **Variable** | Global Heap references | Each string has natural length |

**Python h5py Example**:

```python
import h5py

with h5py.File('strings.h5', 'w') as f:
    # Fixed-length (20 bytes each)
    dt = h5py.string_dtype(encoding='utf-8', length=20)
    f.create_dataset('fixed', data=['hello', 'world'], dtype=dt)

    # Variable-length (via Global Heap)
    dt = h5py.string_dtype(encoding='utf-8')
    f.create_dataset('variable', data=['short', 'much longer string'], dtype=dt)
```

## Technical Details

### Global Heap Collection Structure

```
+------------------------+
| Signature: "GCOL"      |
+------------------------+
| Version                |
+------------------------+
| Collection size        |
+------------------------+
| Object 0 offset        |
| Object 0 size          |
| Object 0 data          |
+------------------------+
| Object 1 offset        |
| Object 1 size          |
| Object 1 data          |
+------------------------+
| ...                    |
+------------------------+
```

### Reference Format

8 or 16 bytes depending on offset size:
```
Bytes 0-7/15: Global Heap address
Bytes 8-11/16-19: Object index
```

## Troubleshooting

### "unsupported datatype: vlen string" Error

**Cause**: Very old implementation or custom vlen format.

**Solution**: File an issue with your HDF5 file for investigation.

### String Appears Garbled

**Cause**: Encoding mismatch (ASCII vs UTF-8).

**Solution**: Verify encoding with h5dump:
```bash
h5dump -d /dataset file.h5
```

## Next Steps

- **[Example 05](../05-comprehensive/)** - Complete feature demo
- **[Datatypes Guide](../../docs/guides/DATATYPES.md)** - String type details
- **[Reading Data Guide](../../docs/guides/READING_DATA.md)** - String reading

---

*Part of the HDF5 Go Library v0.10.0-beta*
