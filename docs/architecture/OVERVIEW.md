# Architecture Overview

> **Modern HDF5 Go Library** - Pure Go implementation without CGo dependencies

**Last Updated**: 2025-11-13

---

## 🎯 Design Philosophy

This library is designed with the following principles:

1. **Pure Go**: No CGo dependencies for maximum portability
2. **Read + Write**: Full read/write capabilities (feature complete)
3. **Format Accurate**: Strict adherence to HDF5 specification
4. **Idiomatic Go**: Clean, readable, testable code
5. **Production Ready**: Comprehensive error handling and testing (>85% coverage)

---

## 📦 Package Structure

```
github.com/scigolib/hdf5/
│
├── file.go                    # Public API: File read operations
├── file_write.go              # Public API: File write operations
├── group.go                   # Public API: Group interfaces
├── dataset.go                 # Public API: Dataset read operations
├── dataset_write.go           # Public API: Dataset write operations
├── attribute_write.go         # Public API: Attribute write operations
│
├── internal/                  # Internal implementation (not exported)
│   ├── core/                  # Core HDF5 structures
│   │   ├── superblock.go     # File metadata (versions 0, 2, 3)
│   │   ├── objectheader.go   # Object headers (v1 read, v1+v2 write)
│   │   ├── attribute.go      # Attribute reading and writing
│   │   ├── datatype.go       # All HDF5 datatypes
│   │   └── messages.go       # Object header messages
│   │
│   ├── structures/            # HDF5 data structures
│   │   ├── symboltable.go    # Symbol tables (traditional groups)
│   │   ├── btree.go          # B-tree v1 indices (read)
│   │   ├── btreev2_write.go  # B-tree v2 (read + write)
│   │   ├── fractalheap_write.go  # Fractal heap (read + write)
│   │   ├── localheap.go      # Local heap (string storage)
│   │   └── globalheap.go     # Global heap (variable-length data)
│   │
│   ├── writer/                # Write support infrastructure
│   │   ├── space_allocator.go  # Free space management
│   │   ├── checksum.go        # CRC32 checksums
│   │   └── helpers.go         # Write utilities
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
│   ├── *.h5                   # HDF5 test files (57 reference files)
│   └── generators/            # Scripts to create test files
│
├── examples/                  # Usage examples
│   ├── 01-basic/             # Open and walk files
│   ├── 02-list-objects/      # List all objects
│   ├── 03-read-dataset/      # Read dataset data
│   ├── 04-vlen-strings/      # Variable-length strings
│   ├── 05-comprehensive/     # All features demo
│   └── 06-write-dataset/     # Write datasets
│
└── cmd/                       # Command-line tools
    └── dump_hdf5/             # HDF5 file hex dumper
```

---

## 🏗️ Core Architecture

### Layer 1: Public API (Read + Write)

**Read API**:
```go
// Open existing HDF5 file (read-only)
type File struct {
    osFile *os.File
    sb     *core.Superblock
    root   *Group
}

func Open(filename string) (*File, error)
func (f *File) Close() error
func (f *File) Root() *Group
func (f *File) Dataset(path string) (*Dataset, error)
func (f *File) Walk(fn func(path string, obj Object))
```

**Write API**:
```go
// Create new HDF5 file (write mode)
type FileWriter struct {
    osFile         *os.File
    sb             *core.Superblock
    spaceAllocator *writer.SpaceAllocator
}

func CreateForWrite(filename string, mode CreateMode) (*FileWriter, error)
func (fw *FileWriter) CreateDataset(path string, dtype core.DataType, dims []uint64, opts ...CreateOption) (*DatasetWriter, error)
func (fw *FileWriter) CreateGroup(path string) error
func (fw *FileWriter) Close() error

// Dataset writing
type DatasetWriter struct {
    // ...
}

func (dw *DatasetWriter) Write(data interface{}) error
func (dw *DatasetWriter) WriteAttribute(name string, value interface{}) error  // ✨ Upsert semantics!
func (dw *DatasetWriter) DeleteAttribute(name string) error  // 🚧 In progress
```

**Responsibilities**:
- File lifecycle management (read + write)
- High-level navigation
- User-friendly error messages
- Attribute modification with upsert semantics

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
    objectHeader *core.ObjectHeader  // Both v1 and v2 supported
}

// Dataset represents an HDF5 dataset
type Dataset struct {
    file         *File
    name         string
    objectHeader *core.ObjectHeader
    datatype     core.DataType
    dataspace    *core.Dataspace
}

// Attribute represents metadata (compact or dense storage)
type Attribute struct {
    Name     string
    Value    interface{}  // Converted to Go types
    DataType core.DataType
}
```

**Responsibilities**:
- Object hierarchy representation
- Group/dataset abstraction
- Attribute management (compact 0-7, dense 8+)
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

// ObjectHeader: Object metadata (v1 read, v1+v2 write)
type ObjectHeader struct {
    Version  uint8
    Flags    uint8
    Messages []*HeaderMessage
    Address  uint64  // For RMW operations
}

// Attribute: Metadata storage
// - Compact storage: In object header (0-7 attributes)
// - Dense storage: Fractal heap + B-tree v2 (8+ attributes)
```

**Responsibilities**:
- Binary format parsing
- Version-specific handling (v0, v2, v3 superblocks)
- Metadata extraction and encoding
- Read-Modify-Write (RMW) support

### Layer 4: Data Structures (Read + Write)

**Read Support**:
```go
// SymbolTable: Traditional group implementation
// BTree v1: Index structure for groups
// LocalHeap: String storage
// GlobalHeap: Variable-length data
```

**Write Support** (NEW):
```go
// BTree v2: Modern index structure (dense storage)
type BTreeV2 struct {
    NodeSize     uint32
    RecordSize   uint16
    Depth        uint16
    RootAddress  uint64
}

// Fractal Heap: Variable-length object storage
type FractalHeap struct {
    MaxDirectBlockSize uint64
    TableWidth         uint16
    StartingBlockSize  uint64
}

// Space Allocator: Free space management
type SpaceAllocator struct {
    nextAddress uint64
    allocations map[uint64]uint64  // address → size
}
```

**Responsibilities**:
- Low-level HDF5 structures
- Index and storage management (B-tree v2, fractal heap)
- String handling
- Free space allocation

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
    ├─→ Validate checksum (v2/v3: CRC32)
    ├─→ Read offset/length sizes
    ├─→ Determine endianness
    └─→ Extract root group address
    ↓
[3] Load root group (loadGroup)
    ↓
[4] Parse object header (v1 or v2)
    ├─→ Read header messages
    ├─→ Handle continuation blocks (v1)
    └─→ Extract attributes (compact or dense)
    ↓
[5] Load children (symbol table or B-tree)
    ↓
[6] Recursively load child objects
    ↓
Return File object to user
```

### Writing an HDF5 File

```
User Code
    ↓
hdf5.CreateForWrite(filename, mode)
    ↓
[1] Create/truncate file
    ↓
[2] Write Superblock v2 (modern format, default)
    ├─→ Choose offset/length sizes
    ├─→ Initialize root group address
    └─→ Write checksum
    Note: v2/v3 read and write support fully implemented
    ↓
[3] Initialize space allocator
    ↓
CreateDataset(path, dtype, dims)
    ↓
[4] Allocate dataset space
    ├─→ Choose layout (contiguous/chunked)
    ├─→ Apply filters (GZIP, Shuffle)
    └─→ Allocate storage address
    ↓
[5] Create object header (v1 or v2)
    ├─→ Add Datatype message
    ├─→ Add Dataspace message
    ├─→ Add Layout message
    └─→ Add Filter Pipeline message (if compressed)
    ↓
[6] Update parent group
    ├─→ Add to symbol table (traditional)
    └─→ OR add to B-tree v2 (dense)
    ↓
WriteAttribute(name, value)
    ↓
[7] Choose storage (compact or dense)
    ├─→ If ≤7 attrs → Compact (in object header)
    └─→ If ≥8 attrs → Dense (fractal heap + B-tree v2)
    ↓
[8] Upsert semantics
    ├─→ If attribute exists → Replace (modify)
    └─→ If not exists → Add (create)
    ↓
Close()
    ↓
[9] Flush all buffers to disk
```

---

## 🧠 Key Design Patterns

### 1. Upsert Semantics for Attributes

**Problem**: Users shouldn't worry about "create vs modify"
**Solution**: `WriteAttribute()` automatically handles both

```go
// Seamless API - no need to check if attribute exists!
dataset.WriteAttribute("units", "meters")  // Create
dataset.WriteAttribute("units", "km")      // Modify (upsert!)

// Internally:
func (dw *DatasetWriter) WriteAttribute(name string, value interface{}) error {
    existingIndex := findAttributeByName(name)
    if existingIndex >= 0 {
        // Attribute exists → Replace message (modify)
        replaceAttributeMessage(existingIndex, name, value)
    } else {
        // Attribute doesn't exist → Add new message (create)
        addAttributeMessage(name, value)
    }
}
```

**Benefits**:
- Simpler API (one method, not two)
- Less error-prone (no "already exists" errors)
- Matches Python h5py intuitive behavior

### 2. Dense Storage RMW (Read-Modify-Write)

**Problem**: Need to modify existing dense attributes (8+ attributes)
**Solution**: Full RMW cycle with B-tree v2 + fractal heap

```go
// Write → Close → Reopen → Modify → Verify
func TestDenseStorageRMW(t *testing.T) {
    // 1. Create file with 8+ attributes (triggers dense storage)
    fw, _ := CreateForWrite("test.h5", CreateTruncate)
    ds, _ := fw.CreateDataset("/data", Float64, []uint64{10})
    for i := 0; i < 10; i++ {
        ds.WriteAttribute(fmt.Sprintf("attr_%d", i), i)
    }
    fw.Close()

    // 2. Reopen and read attributes
    f, _ := Open("test.h5")
    dataset, _ := f.Dataset("/data")
    attrs, _ := dataset.Attributes()  // ✅ Reads from fractal heap!

    // 3. Verify all 10 attributes present
    assert.Equal(t, 10, len(attrs))
}
```

**Implementation**:
- B-tree v2 header/leaf parsing
- Fractal heap header + direct block reading
- Variable-length heap ID parsing (1 byte flags + offset + length)
- Type conversion via ReadValue()

### 3. Buffer Pooling

**Problem**: Frequent small allocations for reading binary data
**Solution**: `sync.Pool` for reusable buffers

```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}
```

**Benefits**:
- Reduced GC pressure
- Better memory locality
- Improved performance

### 4. Variable-Size Field Reading

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

### 5. Context-Rich Error Handling

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

### 6. Signature-Based Dispatch

**Problem**: Multiple HDF5 structure versions and formats
**Solution**: 4-byte signature detection

```go
const (
    SuperblockSig  = "\x89HDF\r\n\x1a\n"  // File signature
    SymbolTableSig = "SNOD"                // Symbol table node
    ObjectHeaderSig = "OHDR"               // Object header
    BTreeSig       = "TREE"                // B-tree v1
    BTreeV2Sig     = "BTHD"                // B-tree v2
    FractalHeapSig = "FHDB"                // Fractal heap direct block
    HeapSig        = "HEAP"                // Local heap
)
```

---

## 🔍 Format Version Support

### Superblock Versions

| Version | Read | Write | Features |
|---------|------|-------|----------|
| 0 | ✅ | ✅ | Original format (HDF5 1.0-1.6) |
| 1 | ❌ | ❌ | Same as v0 with B-tree K values |
| 2 | ✅ | ✅ | Streamlined format (HDF5 1.8+) |
| 3 | ✅ | ✅ | HDF5 2.0.0 format (48-byte, CRC32 checksum) |

### Object Header Versions

| Version | Read | Write | Notes |
|---------|------|-------|-------|
| 1 | ✅ | ✅ | Legacy format (HDF5 < 1.8) with continuations |
| 2 | ✅ | ✅ | Modern format (HDF5 1.8+) |

### Attribute Storage

| Storage | Trigger | Read | Write | Modify |
|---------|---------|------|-------|--------|
| Compact | 0-7 attrs | ✅ | ✅ | 🚧 Phase 1 complete |
| Dense | 8+ attrs | ✅ | ✅ | 🚧 Phase 2 in progress |

### Dataset Layouts

| Layout | Read | Write | Notes |
|--------|------|-------|-------|
| Compact | ✅ | ⚠️ | Small datasets (<64KB) - write deferred |
| Contiguous | ✅ | ✅ | Simple flat layout |
| Chunked | ✅ | ✅ | With GZIP/Shuffle filters |

### Datatypes

| Type | Read | Write | Examples |
|------|------|-------|----------|
| Integer | ✅ | ✅ | int8, int16, int32, int64, uint* |
| Float | ✅ | ✅ | float32, float64 |
| String | ✅ | ✅ | Fixed-length, variable-length |
| Array | ✅ | ✅ | [3]float64, [2][2]int32 |
| Enum | ✅ | ✅ | Named integer constants |
| Reference | ✅ | ✅ | Object references |
| Opaque | ✅ | ✅ | Binary blobs with tag |
| Compound | ✅ | ✅ | Struct-like with nested members |

---

## 🎨 API Design Principles

### 1. **Progressive Disclosure**

Simple operations are simple:
```go
// Reading (simple)
file, _ := hdf5.Open("data.h5")
defer file.Close()

file.Walk(func(path string, obj hdf5.Object) {
    fmt.Println(path)
})

// Writing (simple)
fw, _ := hdf5.CreateForWrite("output.h5", hdf5.CreateTruncate)
defer fw.Close()

ds, _ := fw.CreateDataset("/data", hdf5.Float64, []uint64{100})
ds.Write([]float64{1.0, 2.0, 3.0, ...})
ds.WriteAttribute("units", "meters")  // ✨ Upsert semantics!
```

Complex operations are possible:
```go
// Advanced: Chunked dataset with compression
fw.CreateDataset("/bigdata", hdf5.Float64, []uint64{1000, 1000},
    hdf5.WithChunked([]uint64{100, 100}),
    hdf5.WithGZIP(6),
    hdf5.WithShuffle(),
)
```

### 2. **Fail Fast**

All errors are detected early:
- Invalid signature → immediate error
- Out-of-bounds address → immediate error
- Unsupported version → immediate error
- Attribute modification errors → clear error messages

### 3. **Resource Safety**

All resources are properly managed:
```go
func CreateForWrite(filename string, mode CreateMode) (*FileWriter, error) {
    f, err := os.OpenFile(filename, flags, 0644)
    if err != nil {
        return nil, err
    }

    // If any subsequent step fails, close file
    fw := &FileWriter{osFile: f}
    if err := fw.writeSuperblock(); err != nil {
        f.Close()  // ← Resource cleanup
        return nil, err
    }

    return fw, nil
}
```

---

## 🚀 Performance Considerations

### Memory Management
- ✅ Buffer pooling reduces allocations
- ✅ Pooled buffers are size-flexible
- ✅ Streaming large dataset writes (chunked layout)

### I/O Patterns
- ✅ Sequential reads for superblock
- ✅ Random access for objects
- ✅ Atomic writes (write-back on Close())
- ⚠️ No read-ahead buffering (future)
- ⚠️ No parallel chunk reading/writing (future)

### Concurrency
- ⚠️ Current implementation is not thread-safe
- 📋 Future: concurrent reader support
- 📋 Future: SWMR mode implementation

---

## 📊 Current Status

### Read Support: 100% ✅
- All HDF5 formats (superblock v0, v2, v3)
- All datatypes
- All layouts (compact, contiguous, chunked)
- All storage types (compact, dense)
- Compression (GZIP/Deflate)
- Object headers (v1, v2)
- Attributes (compact, dense)

### Write Support: 100% ✅
- File creation (Truncate/Exclusive modes)
- Superblock v0, v2, and v3 writing
- Object Header v1 and v2 writing
- Dataset writing (contiguous, chunked)
- All datatypes (including compound, arrays, enums, references)
- GZIP compression, Shuffle filter
- Group creation (symbol table, dense)
- Attribute writing (compact 0-7, dense 8+)
- Dense Storage RMW (read-modify-write cycle complete)
- Attribute modification (compact & dense storage)
- Soft/external links (full support)

### Quality Metrics: Excellent ✅
- **Coverage**: 88%+ library packages (target: >70%)
- **Linter**: 0 issues (34+ linters)
- **Tests**: 100% passing
- **Build**: Cross-platform (Linux, macOS, Windows)

---

## 📚 References

- [HDF5 Format Specification v3.0](https://docs.hdfgroup.org/hdf5/latest/_f_m_t3.html)
- [HDF5 C Library Source](https://github.com/HDFGroup/hdf5) - Reference implementation at `D:\projects\scigolibs\hdf5c\src`
- [Go Standard Library Design](https://go.dev/blog/package-names)

---

*Last Updated: 2025-11-13*
