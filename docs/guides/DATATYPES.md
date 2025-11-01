# Datatypes Guide

> **Complete guide to HDF5 datatype mapping and Go type conversion**

---

## 📚 Table of Contents

- [Overview](#overview)
- [Numeric Types](#numeric-types)
- [String Types](#string-types)
- [Compound Types](#compound-types)
- [Type Conversion Rules](#type-conversion-rules)
- [Unsupported Types](#unsupported-types)
- [Best Practices](#best-practices)

---

## 🎯 Overview

HDF5 uses its own type system that maps to native types in different programming languages. This library provides automatic conversion between HDF5 types and Go types.

### Type Categories

| Category | HDF5 Class | Go Representation | Read | Write |
|----------|------------|-------------------|------|-------|
| **Fixed-point** | H5T_INTEGER | int8-64, uint8-64 | ✅ | ✅ |
| **Floating-point** | H5T_FLOAT | float32, float64 | ✅ | ✅ |
| **String** | H5T_STRING | string, []string | ✅ | ✅ |
| **Compound** | H5T_COMPOUND | map[string]interface{} | ✅ | ❌ Planned |
| **Array** | H5T_ARRAY | [N]T (fixed arrays) | ✅ | ✅ |
| **Enum** | H5T_ENUM | Named integer constants | ✅ | ✅ |
| **Reference** | H5T_REFERENCE | uint64, [12]byte | ✅ | ✅ |
| **Opaque** | H5T_OPAQUE | []byte with tag | ✅ | ✅ |
| **Time** | H5T_TIME | - | ❌ | ❌ Deprecated |

---

## 🔢 Numeric Types

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

// Or preserve original type information
info, _ := ds.Info()
// info shows: "Datatype: int32"

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

#### Unsigned Integers

**Status**: Partially supported (converted to signed)

**HDF5 Types**:
- `H5T_STD_U32LE`, `H5T_STD_U32BE`
- `H5T_STD_U64LE`, `H5T_STD_U64BE`

**Go Conversion**: Read as native unsigned integers (uint8/uint16/uint32/uint64).

**Note**: All unsigned types (Uint8, Uint16, Uint32, Uint64) are fully supported for both reading and writing.

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
| H5T_STD_I32LE/BE | 4 bytes | float64 | int32 → float64 |
| H5T_STD_I64LE/BE | 8 bytes | float64 | int64 → float64 |
| H5T_IEEE_F32LE/BE | 4 bytes | float64 | float32 → float64 |
| H5T_IEEE_F64LE/BE | 8 bytes | float64 | No conversion |

---

## 📝 String Types

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
// HDF5 file has variable-length string dataset
strings, err := ds.ReadStrings()  // Returns []string

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
| ASCII | ✅ Full | Standard ASCII (0-127) |
| UTF-8 | ✅ Full | Unicode support |

---

## 🏗️ Compound Types

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
compounds, err := ds.ReadCompound()  // Returns []map[string]interface{}

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

**Status**: Array fields not yet supported (planned for future release).

**Workaround**: Flatten arrays into separate fields:
```
- "name" : string
- "score_0" : float64
- "score_1" : float64
- "score_2" : float64
...
```

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

## 🔄 Type Conversion Rules

### Automatic Conversions

The library performs these conversions automatically:

| From (HDF5) | To (Go) | Information Loss? |
|-------------|---------|-------------------|
| int32 | float64 | ✅ No (exact) |
| int64 | float64 | ⚠️ Yes (> 2^53) |
| float32 | float64 | ✅ No (promoted) |
| float64 | float64 | ✅ No (exact) |
| fixed string | string | ✅ No (padding removed) |
| variable string | string | ✅ No (exact) |

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

// For most scientific data, this is acceptable
// If exact large integers needed, wait for v1.0.0 (direct int64 support)
```

#### Float32 to Float64 Conversion

Float32 values are promoted to float64 without precision loss (but representation changes):

```go
// float32 in HDF5: 3.14159265f (stored as 0x40490FDB)
// Converted to float64: 3.1415927410125732 (0x400921FB60000000)
//                       ^^^^^^^^ extra precision is not real data!

// For display, round appropriately:
fmt.Printf("%.6f\n", value)  // 3.141593 (shows only 6 digits)
```

---

## ❌ Not Yet Supported

### Compound Datatype (Write Only)

**HDF5 Type**: `H5T_COMPOUND`

**Status**:
- ✅ Reading: Fully supported
- ❌ Writing: Planned for v0.12.0-rc.1

**Example** (Reading works):
```go
// Read compound data
data, err := ds.ReadCompound()
// data is map[string]interface{} with field names as keys
```

**Workaround for Writing**: Use multiple separate datasets (one per field) until compound write is implemented.

---

## ✅ Best Practices

### 1. Check Dataset Type Before Reading

```go
info, err := ds.Info()
if err == nil {
    fmt.Println(info)  // Shows datatype

    // Choose appropriate read method
    if strings.Contains(info, "string") {
        strings, _ := ds.ReadStrings()
        // ...
    } else if strings.Contains(info, "compound") {
        compounds, _ := ds.ReadCompound()
        // ...
    } else {
        data, _ := ds.Read()  // Numeric
        // ...
    }
}
```

### 2. Handle Type Assertions Safely

```go
for _, attr := range attrs {
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
        fmt.Printf("unknown type: %T\n", v)
    }
}
```

### 3. Document Precision Requirements

If your application requires exact integer values > 2^53:

```go
// Check if dataset contains large integers
info, _ := ds.Info()
if strings.Contains(info, "int64") {
    log.Println("Warning: int64 dataset may lose precision when converted to float64")
    log.Println("Safe range: -2^53 to 2^53 (±9,007,199,254,740,992)")
    log.Println("For exact int64 values, wait for v1.0.0")
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
- More efficient storage
- Easier to maintain consistency

### 5. Test with Python h5py

Generate test files using Python for verification:

```python
import h5py
import numpy as np

with h5py.File('test_types.h5', 'w') as f:
    # Test all supported types
    f.create_dataset('int32', data=np.array([1, 2, 3], dtype='i4'))
    f.create_dataset('int64', data=np.array([1, 2, 3], dtype='i8'))
    f.create_dataset('float32', data=np.array([1.1, 2.2, 3.3], dtype='f4'))
    f.create_dataset('float64', data=np.array([1.1, 2.2, 3.3], dtype='f8'))

    # Fixed-length strings
    dt = h5py.string_dtype(encoding='utf-8', length=10)
    f.create_dataset('strings_fixed', data=[b'hello', b'world'], dtype=dt)

    # Variable-length strings
    dt = h5py.string_dtype(encoding='utf-8')
    f.create_dataset('strings_vlen', data=['hello', 'world'], dtype=dt)

    # Compound type
    dt = np.dtype([('x', 'f8'), ('y', 'f8'), ('name', 'S20')])
    data = np.array([(1.0, 2.0, b'point1'), (3.0, 4.0, b'point2')], dtype=dt)
    f.create_dataset('compound', data=data)
```

Then read with Go and verify values match!

---

## 📚 Next Steps

- **[Reading Data Guide](READING_DATA.md)** - How to use these types in practice
- **[Troubleshooting](TROUBLESHOOTING.md)** - Common type-related issues
- **[Examples](../../examples/)** - Code examples with different datatypes

---

*Last Updated: 2025-11-01*
*Version: 0.11.3-beta*
