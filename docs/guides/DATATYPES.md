# Datatypes Guide

How HDF5 datatypes map to Go types, and how values are converted when read and written.

---

## Table of Contents

- [Overview](#overview)
- [Numeric Types](#numeric-types)
- [String Types](#string-types)
- [Compound Types](#compound-types)
- [Type Conversion Rules](#type-conversion-rules)
- [Best Practices](#best-practices)

---

## Overview

HDF5 uses its own type system that maps to native types in different programming languages. This library provides automatic conversion between HDF5 types and Go types.

### Type Categories

| Category | HDF5 Class | Go Representation | Read | Write |
|----------|------------|-------------------|------|-------|
| **Fixed-point** | H5T_INTEGER | int8-64, uint8-64 | yes | yes |
| **Floating-point** | H5T_FLOAT | float32, float64 | yes | yes |
| **String** | H5T_STRING | string, []string | yes | yes |
| **Compound** | H5T_COMPOUND | map[string]interface{} per element | yes | yes |
| **Array** | H5T_ARRAY | base values, flattened | yes | yes |
| **Enum** | H5T_ENUM | the base integer value | yes | yes |
| **Reference** | H5T_REFERENCE | uint64, [12]byte | yes | yes |
| **Opaque** | H5T_OPAQUE | []byte with tag | yes | yes |
| **Time** | H5T_TIME | - | no | no |

---

## Numeric Types

### Integer Types

#### 32-bit Signed Integer

**HDF5 Types**:
- `H5T_STD_I32LE` (little-endian)
- `H5T_STD_I32BE` (big-endian)
- `H5T_NATIVE_INT` (platform-native, 32-bit)

**Go Type**: `int32`

**Example**:
```go
// HDF5 file contains int32 dataset
data, err := ds.Read()  // Returns []float64

// The original type information stays available
dt, _ := ds.Datatype()
// dt.Class == hdf5.ClassInteger, dt.Size == 4, dt.Signed == true,
// dt.Order == hdf5.OrderLittleEndian (or OrderBigEndian)

// Value conversion: int32 → float64
// Example: 42 (int32) becomes 42.0 (float64)
```

**Range**: -2,147,483,648 to 2,147,483,647

#### 64-bit Signed Integer

**HDF5 Types**:
- `H5T_STD_I64LE` (little-endian)
- `H5T_STD_I64BE` (big-endian)
- `H5T_NATIVE_LLONG` (platform-native, 64-bit)

**Go Type**: `int64`

**Example**:
```go
data, err := ds.Read()  // Returns []float64

// Value conversion: int64 → float64
// Example: 9223372036854775807 (int64) becomes 9.223372036854776e+18 (float64)
```

**Range**: -9,223,372,036,854,775,808 to 9,223,372,036,854,775,807

**Precision Note**: When converting int64 to float64, integers larger than 2^53 (9,007,199,254,740,992) may lose precision due to float64's mantissa limitations.

#### 8-bit Signed Integer

**HDF5 Types**:
- `H5T_STD_I8LE` (little-endian)
- `H5T_STD_I8BE` (big-endian)
- `H5T_NATIVE_INT8` (platform-native)

**Go Type**: `int8`

**Range**: -128 to 127

#### 16-bit Signed Integer

**HDF5 Types**:
- `H5T_STD_I16LE` (little-endian)
- `H5T_STD_I16BE` (big-endian)
- `H5T_NATIVE_INT16` (platform-native)

**Go Type**: `int16`

**Range**: -32,768 to 32,767

#### Unsigned Integers

**Status**: Fully supported (all widths)

**HDF5 Types**:
- `H5T_STD_U8LE/BE` (uint8)
- `H5T_STD_U16LE/BE` (uint16)
- `H5T_STD_U32LE/BE` (uint32)
- `H5T_STD_U64LE/BE` (uint64)

**Go Conversion**: Read as native unsigned integers, converted to float64 by `Dataset.Read()`.

**Precision Note**: uint64 values above 2^53 (9,007,199,254,740,992) may lose precision when converted to float64.

### Floating-Point Types

#### 32-bit Float (Single Precision)

**HDF5 Types**:
- `H5T_IEEE_F32LE` (little-endian)
- `H5T_IEEE_F32BE` (big-endian)
- `H5T_NATIVE_FLOAT` (platform-native)

**Go Type**: `float32`

**Precision**: ~7 decimal digits

**Example**:
```go
data, err := ds.Read()  // Returns []float64

// Value conversion: float32 → float64
// Example: 3.14159265f (float32) becomes 3.1415927410125732 (float64)
```

**Range**: ±1.18e-38 to ±3.40e+38

#### 64-bit Float (Double Precision)

**HDF5 Types**:
- `H5T_IEEE_F64LE` (little-endian)
- `H5T_IEEE_F64BE` (big-endian)
- `H5T_NATIVE_DOUBLE` (platform-native)

**Go Type**: `float64`

**Precision**: ~15 decimal digits

**Example**:
```go
data, err := ds.Read()  // Returns []float64 (native)

// No conversion needed
// Example: 3.141592653589793 (float64) stays exact
```

**Range**: ±2.23e-308 to ±1.80e+308

### Numeric Type Conversion Summary

| HDF5 Type | Size | Go Read Type | Conversion |
|-----------|------|--------------|------------|
| H5T_STD_I8LE/BE | 1 byte | float64 | int8 → float64 |
| H5T_STD_I16LE/BE | 2 bytes | float64 | int16 → float64 |
| H5T_STD_I32LE/BE | 4 bytes | float64 | int32 → float64 |
| H5T_STD_I64LE/BE | 8 bytes | float64 | int64 → float64 |
| H5T_STD_U8LE/BE | 1 byte | float64 | uint8 → float64 |
| H5T_STD_U16LE/BE | 2 bytes | float64 | uint16 → float64 |
| H5T_STD_U32LE/BE | 4 bytes | float64 | uint32 → float64 |
| H5T_STD_U64LE/BE | 8 bytes | float64 | uint64 → float64 * |
| H5T_IEEE_F32LE/BE | 4 bytes | float64 | float32 → float64 |
| H5T_IEEE_F64LE/BE | 8 bytes | float64 | No conversion |

\* uint64 values above 2^53 may lose precision in float64.

---

## String Types

### Fixed-Length Strings

**HDF5 Type**: `H5T_STRING` with fixed size

**Padding Strategies**:
1. **Null-terminated** (C-style): `"hello\0\0\0"`
2. **Null-padded**: `"hello\0\0\0"`
3. **Space-padded**: `"hello   "`

**Go Type**: `string`

**Automatic Handling**: The library automatically strips padding.

**Example**:
```go
// HDF5 file has fixed-length string dataset
strings, err := ds.ReadStrings()  // Returns []string

// Padding is automatically removed:
// HDF5 bytes: "hello\0\0\0" → Go string: "hello"
// HDF5 bytes: "world   "   → Go string: "world"
```

**Python h5py equivalent**:
```python
# Creating fixed-length strings in Python
import h5py
import numpy as np

with h5py.File('strings.h5', 'w') as f:
    # Null-terminated
    dt = h5py.string_dtype(encoding='ascii', length=20)
    f.create_dataset('names', data=[b'Alice', b'Bob'], dtype=dt)
```

### Variable-Length Strings

**HDF5 Type**: `H5T_STRING` with variable size

**Storage**: Global Heap (separate area in HDF5 file)

**Go Type**: `string`

**Example**:
```go
// HDF5 file has variable-length string dataset: ReadStrings reads only
// fixed-length strings; ReadVLenBytes returns each string's bytes.
raw, err := ds.ReadVLenBytes() // [][]byte
if err != nil {
    return err
}
strings := make([]string, len(raw))
for i, b := range raw {
    strings[i] = string(b)
}

// Strings can have different lengths:
// ["short", "a much longer string", "x"]
```

**Python h5py equivalent**:
```python
import h5py

with h5py.File('vlen_strings.h5', 'w') as f:
    # Variable-length strings
    dt = h5py.string_dtype(encoding='utf-8')
    f.create_dataset('messages', data=["Hello", "World!"], dtype=dt)
```

### Character Sets

| Encoding | Status | Notes |
|----------|--------|-------|
| ASCII | supported | Standard ASCII (0-127) |
| UTF-8 | supported | Unicode support |

---

## Compound Types

Compound types are struct-like data with named fields (similar to C structs or Go structs).

### Basic Compound Type

**HDF5 Type**: `H5T_COMPOUND`

**Go Type**: `map[string]interface{}`

**Example HDF5 Structure**:
```
Compound Type:
  - "temperature" : float64
  - "humidity"    : float64
  - "location"    : string (fixed-length, 20 bytes)
```

**Reading Compound Data**:
```go
compounds, err := ds.ReadCompound() // one map from member name to value per element
if err != nil {
    log.Fatal(err)
}

for i, record := range compounds {
    fmt.Printf("Record %d:\n", i)

    // Access fields by name
    temp := record["temperature"].(float64)
    humid := record["humidity"].(float64)
    loc := record["location"].(string)

    fmt.Printf("  Temperature: %.1f°C\n", temp)
    fmt.Printf("  Humidity: %.1f%%\n", humid)
    fmt.Printf("  Location: %s\n", loc)
}
```

`ReadCompound` returns `[]core.CompoundValue`, a map type, so `record["name"]` works as shown.

**Output**:
```
Record 0:
  Temperature: 25.3°C
  Humidity: 65.2%
  Location: Lab A
Record 1:
  Temperature: 26.1°C
  Humidity: 63.8%
  Location: Lab B
```

**Unsigned members**: unsigned 4- and 8-byte members read as `uint32` and `uint64`.
v0.14.2 and earlier returned them as `int32` and `int64`, so values above 2^31 (or 2^63)
read as negative numbers. A type assertion written for the old types now panics: use a type
switch.

### Writing Compound Datasets

Describe the type with `NewCompoundType` (explicit member offsets and total size, like a C
struct with padding) or `NewPackedCompoundType` (members one after another, no gaps), create
the dataset with `FileWriter.CreateCompound` and write one `map[string]any` per element:

```go
package main

import (
    "fmt"
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    // struct { int64 timestamp; uint32 id; double value; char name[16]; }
    // laid out without gaps (H5Tpack).
    ct, err := hdf5.NewPackedCompoundType([]hdf5.CompoundMember{
        {Name: "timestamp", Type: hdf5.Int64},
        {Name: "id", Type: hdf5.Uint32},
        {Name: "value", Type: hdf5.Float64},
        {Name: "name", Type: hdf5.FixedString{Size: 16}},
    })
    if err != nil {
        log.Fatal(err)
    }

    fw, err := hdf5.CreateForWrite("measurements.h5", hdf5.CreateTruncate)
    if err != nil {
        log.Fatal(err)
    }

    ds, err := fw.CreateCompound("/measurements", ct, []uint64{2})
    if err != nil {
        log.Fatal(err)
    }
    err = ds.Write([]map[string]any{
        {"timestamp": 1500000000, "id": 4000000000, "value": 25.3, "name": "Lab A"},
        {"timestamp": 1500000060, "id": 7, "value": 26.1, "name": "Lab B"},
    })
    if err != nil {
        log.Fatal(err)
    }
    if err := fw.Close(); err != nil {
        log.Fatal(err)
    }

    f, err := hdf5.Open("measurements.h5")
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()

    f.Walk(func(path string, obj hdf5.Object) {
        d, ok := obj.(*hdf5.Dataset)
        if !ok {
            return
        }
        rows, err := d.ReadCompound()
        if err != nil {
            log.Fatal(err)
        }
        for _, r := range rows {
            // "id" is an unsigned 4-byte member: it reads as uint32.
            fmt.Printf("%s: id=%d value=%.1f name=%s\n", path, r["id"].(uint32), r["value"], r["name"])
        }
    })
}
```

With explicit offsets, `NewCompoundType(size, members)` takes the size of the whole
compound in bytes and an `Offset` for every member. Gaps and trailing padding are allowed
and are written as zeros:

```go
// struct { int32 id; /* 4 bytes padding */ double value; }
ct, err := hdf5.NewCompoundType(16, []hdf5.CompoundMember{
    {Name: "id", Offset: 0, Type: hdf5.Int32},
    {Name: "value", Offset: 8, Type: hdf5.Float64},
})
```

Rules (all from the C library's `H5Tinsert`): a name is non-empty and unique, members
do not overlap and do not extend past the end, and there are between 1 and 65535 of them.
Member types are the fixed-size scalar `hdf5.Datatype` constants (`Int8` ... `Float64`,
object and region references), `hdf5.FixedString`, `hdf5.ArrayType`, `hdf5.EnumType` and a
nested `*hdf5.CompoundType`. A fixed string holds at most `Size-1` bytes unless its padding
is `PaddingNullPad` or `PaddingSpacePad`. `Write` rejects a value that does not fit its member
instead of truncating it. `DatasetWriter.WriteRaw` takes the packed little-endian bytes of
all elements. The godoc of `CreateCompound` lists the accepted Go value for each member type.

`FileWriter.CreateCompoundDataset` (which took an internal type) is deprecated; use
`CreateCompound`.

### Nested Compound Types

Compound types can contain other compound types:

**HDF5 Structure**:
```
Compound Type "Measurement":
  - "timestamp" : int64
  - "sensor" : Compound {
      - "id" : int32
      - "name" : string
    }
  - "value" : float64
```

**Reading Nested Compounds**:
```go
compounds, err := ds.ReadCompound()

for _, record := range compounds {
    timestamp := record["timestamp"].(int64)
    value := record["value"].(float64)

    // Nested compound
    sensor := record["sensor"].(map[string]interface{})
    sensorID := sensor["id"].(int32)
    sensorName := sensor["name"].(string)

    fmt.Printf("Sensor %d (%s) at %d: %.2f\n",
        sensorID, sensorName, timestamp, value)
}
```

### Compound Type with Arrays

**HDF5 Structure**:
```
Compound Type:
  - "name" : string
  - "scores" : array of 5 × float64
```

**Status**: supported, including array members of compounds.

### Creating Compounds in Python

For testing or reference:

```python
import h5py
import numpy as np

# Define compound datatype
dt = np.dtype([
    ('temperature', 'f8'),      # float64
    ('humidity', 'f8'),         # float64
    ('location', 'S20')         # fixed-length string (20 bytes)
])

# Create data
data = np.array([
    (25.3, 65.2, b'Lab A'),
    (26.1, 63.8, b'Lab B'),
    (24.8, 67.5, b'Lab C')
], dtype=dt)

# Write to HDF5
with h5py.File('compounds.h5', 'w') as f:
    f.create_dataset('measurements', data=data)
```

---

## Type Conversion Rules

### Automatic Conversions

The library performs these conversions automatically:

| From (HDF5) | To (Go) | Information Loss? |
|-------------|---------|-------------------|
| int32 | float64 | No (exact) |
| int64 | float64 | Yes, above 2^53 |
| float32 | float64 | No (promoted) |
| float64 | float64 | No (exact) |
| fixed string | string | No (padding removed) |
| variable string | string | No (exact) |

### Precision Considerations

#### Integer to Float Conversion

**Safe Range** (no precision loss):
- int32: All values (max 2^31 << 2^53)
- int64: -2^53 to 2^53 (±9,007,199,254,740,992)

**Example of Precision Loss**:
```go
// int64 value in HDF5: 9223372036854775807 (2^63 - 1)
// Converted to float64: 9223372036854776000 (rounded)
// Lost precision: ~1000

// If exact 64-bit integers are needed, Dataset.Read is not suitable:
// read hyperslabs with ReadSlice or ReadHyperslab, which return the dataset's own type
```

#### Float32 to Float64 Conversion

Float32 values are promoted to float64 without precision loss (but representation changes):

```go
// float32 in HDF5: 3.14159265f (stored as 0x40490FDB)
// Converted to float64: 3.1415927410125732 (0x400921FB60000000)
//                       ^^^^^^^^ extra precision is not real data

// For display, round appropriately:
fmt.Printf("%.6f\n", value)  // 3.141593 (shows only 6 digits)
```

---

## Best Practices

### 1. Check Dataset Type Before Reading

```go
dt, err := ds.Datatype()
if err != nil {
    return err
}

// Choose appropriate read method
switch dt.Class {
case hdf5.ClassString:
    values, _ := ds.ReadStrings()
    // ...
case hdf5.ClassCompound:
    records, _ := ds.ReadCompound()
    // dt.Members lists each member's name, offset and type
    // ...
default:
    data, _ := ds.Read() // Numeric
    // ...
}
```

### 2. Handle Type Assertions Safely

```go
attrs, err := ds.Attributes()
if err != nil {
    log.Fatal(err)
}
for _, attr := range attrs {
    value, err := attr.ReadValue()
    if err != nil {
        fmt.Printf("error reading %s: %v\n", attr.Name, err)
        continue
    }
    switch v := value.(type) {
    case int32:
        fmt.Printf("int32: %d\n", v)
    case int64:
        fmt.Printf("int64: %d\n", v)
    case float64:
        fmt.Printf("float64: %.6f\n", v)
    case string:
        fmt.Printf("string: %q\n", v)
    default:
        fmt.Printf("unknown type: %T\n", v)
    }
}
```

### 3. Document Precision Requirements

If your application requires exact integer values > 2^53:

```go
// Check if dataset contains large integers
dt, _ := ds.Datatype()
if dt.Class == hdf5.ClassInteger && dt.Size == 8 {
    log.Println("Warning: int64 dataset may lose precision when converted to float64")
    log.Println("Safe range: -2^53 to 2^53 (±9,007,199,254,740,992)")
    // ReadSlice and ReadHyperslab return the dataset's own element type.
}
```

### 4. Use Compound Types for Structured Data

Instead of separate datasets:
```
/measurement_temperature
/measurement_humidity
/measurement_location
```

Use compound types:
```
/measurements (compound with temperature, humidity, location fields)
```

Benefits:
- Keeps related data together
- One dataset and one set of dimensions instead of several
- A record's fields cannot get out of step with each other

### 5. Create Test Files in Go

Test files with one dataset of each kind can be generated with this library:

```go
package main

import (
    "log"

    "github.com/scigolib/hdf5"
)

func main() {
    fw, err := hdf5.CreateForWrite("test_types.h5", hdf5.CreateTruncate)
    if err != nil {
        log.Fatal(err)
    }
    defer func() {
        if err := fw.Close(); err != nil {
            log.Fatal(err)
        }
    }()

    // create is a small helper so the example stays short; real code
    // handles each error where it needs to.
    create := func(name string, dt hdf5.Datatype, dims []uint64, opts ...hdf5.DatasetOption) {
        if _, err := fw.CreateDataset(name, dt, dims, opts...); err != nil {
            log.Fatalf("%s: %v", name, err)
        }
    }

    create("/int32", hdf5.Int32, []uint64{3})
    create("/int64", hdf5.Int64, []uint64{3})
    create("/float32", hdf5.Float32, []uint64{3})
    create("/float64", hdf5.Float64, []uint64{3})

    // Fixed-length strings of 10 bytes
    create("/strings_fixed", hdf5.String, []uint64{2}, hdf5.WithStringSize(10))

    // Arrays: ten elements, each a 3x3 array of float32
    create("/arrays", hdf5.ArrayFloat32, []uint64{10},
        hdf5.WithArrayDims([]uint64{3, 3}))

    // Enums
    create("/status", hdf5.EnumInt8, []uint64{5},
        hdf5.WithEnumValues([]string{"OK", "ERROR"}, []int64{0, 1}))
}
```

Check the result with `h5dump` or h5py.

---

## Next Steps

- [Reading Data Guide](READING_DATA.md): how to use these types in practice
- [Troubleshooting](TROUBLESHOOTING.md): common type-related issues
- [Examples](../../examples/): code examples with different datatypes

---

*Last Updated: 2026-11-07*
