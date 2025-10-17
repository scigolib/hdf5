# Architecture Overview

> **Modern HDF5 Go Library** - Pure Go implementation without CGo dependencies

---

## 🎯 Design Philosophy

This library is designed with the following principles:

1. **Pure Go**: No CGo dependencies for maximum portability
2. **Memory Efficient**: Buffer pooling and minimal allocations
3. **Format Accurate**: Strict adherence to HDF5 specification
4. **Idiomatic Go**: Clean, readable, testable code
5. **Production Ready**: Comprehensive error handling and testing

---

## 📦 Package Structure

```
github.com/scigolib/hdf5/
│
├── file.go                    # Public API: File operations
├── group.go                   # Public API: Group/Dataset/Object interfaces
│
├── internal/                  # Internal implementation (not exported)
│   ├── core/                  # Core HDF5 structures
│   │   ├── superblock.go     # File metadata (versions 0, 2, 3)
│   │   ├── objectheader.go   # Object headers (version 2)
│   │   └── linkinfo.go       # Link information messages
│   │
│   ├── structures/            # HDF5 data structures
│   │   ├── symboltable.go    # Symbol tables (traditional groups)
│   │   ├── btree.go          # B-tree indices
│   │   └── localheap.go      # Local heap (string storage)
│   │
│   ├── utils/                 # Internal utilities
│   │   ├── bufferpool.go     # Memory-efficient buffer pooling
│   │   ├── endian.go         # Endianness-aware reading
│   │   └── errors.go         # Error context wrapping
│   │
│   └── testing/               # Test utilities
│       └── mock_reader.go    # Mock io.ReaderAt for tests
│
├── testdata/                  # Test fixtures
│   ├── *.h5                   # HDF5 test files
│   └── generators/            # Scripts to create test files
│
├── examples/                  # Usage examples
│   └── ...
│
└── cmd/                       # Command-line tools
    └── dump_hdf5/             # HDF5 file hex dumper
```

---

## 🏗️ Core Architecture

### Layer 1: Public API

```go
// High-level API for users
type File struct {
    osFile *os.File
    sb     *core.Superblock
    root   *Group
}

func Open(filename string) (*File, error)
func (f *File) Close() error
func (f *File) Root() *Group
func (f *File) Walk(fn func(path string, obj Object))
```

**Responsibilities**:
- File lifecycle management
- High-level navigation
- User-friendly error messages

### Layer 2: Object Model

```go
// Object interface for all HDF5 objects
type Object interface {
    Name() string
}

// Group represents an HDF5 group
type Group struct {
    file        *File
    name        string
    children    []Object
    symbolTable *structures.SymbolTable
    localHeap   *structures.LocalHeap
}

// Dataset represents an HDF5 dataset (metadata only currently)
type Dataset struct {
    file *File
    name string
}
```

**Responsibilities**:
- Object hierarchy representation
- Group/dataset abstraction
- Child object management

### Layer 3: HDF5 Core Structures

```go
// Superblock: File metadata
type Superblock struct {
    Version        uint8
    OffsetSize     uint8
    LengthSize     uint8
    BaseAddress    uint64
    RootGroup      uint64
    Endianness     binary.ByteOrder
    SuperExtension uint64
    DriverInfo     uint64
}

// ObjectHeader: Object metadata
type ObjectHeader struct {
    Version  uint8
    Flags    uint8
    Type     ObjectType
    Messages []*HeaderMessage
    Name     string
}
```

**Responsibilities**:
- Binary format parsing
- Version-specific handling
- Metadata extraction

### Layer 4: Data Structures

```go
// SymbolTable: Traditional group implementation
type SymbolTable struct {
    Version      uint8
    EntryCount   uint16
    BTreeAddress uint64
    HeapAddress  uint64
}

// LocalHeap: String storage
type LocalHeap struct {
    Data       []byte
    FreeList   uint64
    HeaderSize uint64
}

// BTree: Index structure (partial implementation)
```

**Responsibilities**:
- Low-level HDF5 structures
- Index and storage management
- String handling

---

## 🔄 Data Flow

### Reading an HDF5 File

```
User Code
    ↓
hdf5.Open(filename)
    ↓
[1] File signature validation
    ↓
[2] Superblock parsing (core.ReadSuperblock)
    ├─→ Determine version (0, 2, or 3)
    ├─→ Read offset/length sizes
    ├─→ Determine endianness
    └─→ Extract root group address
    ↓
[3] Load root group (loadGroup)
    ↓
[4] Detect group format (signature-based)
    ├─→ "SNOD" → Traditional (loadTraditionalGroup)
    │   ├─→ Parse symbol table
    │   ├─→ Load local heap
    │   └─→ Read symbol table entries
    │
    └─→ "OHDR" → Modern (loadModernGroup)
        ├─→ Parse object header
        ├─→ Process header messages
        └─→ Load children via symbol table or B-tree
    ↓
[5] Recursively load child objects
    ↓
Return File object to user
```

### Group Loading Strategy

The library uses **signature-based dispatch** to handle different group formats:

```go
func loadGroup(file *File, address uint64) (*Group, error) {
    sig := readSignature(file.osFile, address)

    switch sig {
    case "SNOD":  // Traditional format (HDF5 < 1.8)
        return loadTraditionalGroup(file, address)

    case "OHDR":  // Modern format (HDF5 >= 1.8)
        return loadModernGroup(file, address)

    default:
        return nil, fmt.Errorf("unknown signature: %s", sig)
    }
}
```

---

## 🧠 Key Design Patterns

### 1. Buffer Pooling

**Problem**: Frequent small allocations for reading binary data
**Solution**: `sync.Pool` for reusable buffers

```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func GetBuffer(size int) []byte {
    // Reuse or allocate
}

func ReleaseBuffer(buf []byte) {
    // Return to pool
}
```

**Benefits**:
- Reduced GC pressure
- Better memory locality
- Improved performance

### 2. Variable-Size Field Reading

**Problem**: HDF5 uses variable-sized fields (1, 2, 4, or 8 bytes)
**Solution**: Size-aware reading helper

```go
func readValue(offset int, size uint8) (uint64, error) {
    data := buf[offset : offset+int(size)]
    switch size {
    case 1: return uint64(data[0]), nil
    case 2: return uint64(endianness.Uint16(data)), nil
    case 4: return uint64(endianness.Uint32(data)), nil
    case 8: return endianness.Uint64(data), nil
    }
}
```

### 3. Context-Rich Error Handling

**Problem**: Deep call stacks make debugging difficult
**Solution**: Error wrapping with context

```go
func WrapError(context string, err error) error {
    return fmt.Errorf("%s: %w", context, err)
}

// Usage:
if err := readData(); err != nil {
    return utils.WrapError("superblock read failed", err)
}
// Error: "superblock read failed: root group load failed: invalid signature"
```

### 4. Signature-Based Dispatch

**Problem**: Multiple HDF5 structure versions and formats
**Solution**: 4-byte signature detection

```go
const (
    SuperblockSig  = "\x89HDF\r\n\x1a\n"  // File signature
    SymbolTableSig = "SNOD"                // Symbol table node
    ObjectHeaderSig = "OHDR"               // Object header
    BTreeSig       = "BTRE"                // B-tree
    HeapSig        = "HEAP"                // Local heap
)
```

---

## 🔍 Format Version Support

### Superblock Versions

| Version | Status | Features |
|---------|--------|----------|
| 0 | ✅ Supported | Original format (HDF5 1.0-1.6) |
| 1 | ❌ Not needed | Same as v0 with B-tree K values |
| 2 | ✅ Supported | Streamlined format (HDF5 1.8+) |
| 3 | ✅ Supported | SWMR support (HDF5 1.10+) |

### Object Header Versions

| Version | Status | Notes |
|---------|--------|-------|
| 1 | ❌ Not yet | Legacy format |
| 2 | ✅ Supported | Modern format (HDF5 1.8+) |

### Group Formats

| Format | Signature | Status | Notes |
|--------|-----------|--------|-------|
| Traditional | SNOD | ✅ Supported | Symbol table based |
| Modern | OHDR | ✅ Supported | Object header based |

---

## 🎨 API Design Principles

### 1. **Progressive Disclosure**

Simple operations are simple:
```go
file, _ := hdf5.Open("data.h5")
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    fmt.Println(path)
})
```

Complex operations are possible:
```go
// Future API for dataset reading
data, _ := file.Root().Dataset("mydata").Read()
```

### 2. **Fail Fast**

All errors are detected early:
- Invalid signature → immediate error
- Out-of-bounds address → immediate error
- Unsupported version → immediate error

### 3. **Resource Safety**

All resources are properly managed:
```go
func Open(filename string) (*File, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, err
    }

    // If any subsequent step fails, close file
    if !isHDF5File(f) {
        f.Close()  // ← Resource cleanup
        return nil, errors.New("not an HDF5 file")
    }

    // ...
}
```

---

## 🚀 Performance Considerations

### Memory Management
- ✅ Buffer pooling reduces allocations
- ✅ Pooled buffers are size-flexible
- ⚠️ Large file support needs streaming (future)

### I/O Patterns
- ✅ Sequential reads for superblock
- ✅ Random access for objects
- ⚠️ No read-ahead buffering (future)
- ⚠️ No parallel chunk reading (future)

### Concurrency
- ⚠️ Current implementation is not thread-safe
- 📋 Future: concurrent reader support
- 📋 Future: SWMR mode implementation

---

## 📚 References

- [HDF5 Format Specification v3.0](https://docs.hdfgroup.org/documentation/hdf5/latest/_f_m_t3.html)
- [HDF5 C Library Source](https://github.com/HDFGroup/hdf5)
- [Go Standard Library Design](https://go.dev/blog/package-names)

---

*Last Updated: 2025-10-17*
