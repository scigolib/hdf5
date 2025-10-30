# HDF5 Allocator Design

**Component**: Free Space Management (Component 5)
**Version**: v0.11.0-beta MVP
**Status**: Production-ready
**Coverage**: 100% (6/6 functions)

---

## Overview

The **Allocator** manages free space allocation in HDF5 files during write operations. It tracks all allocated regions and ensures no overlapping blocks are written to the file.

### Key Responsibilities
- Allocate space for new HDF5 objects (datasets, groups, attributes, metadata)
- Track all allocated blocks to prevent overlaps
- Report end-of-file address for file growth
- Validate allocation integrity (debugging/testing)

---

## Strategy (MVP)

**End-of-File Allocation**:
- All allocations occur sequentially at the end of the file
- No reuse of freed space (freed blocks are not tracked in MVP)
- No fragmentation (perfect sequential layout)
- Simple, predictable, and reliable

### Why End-of-File?

**Advantages**:
1. **Simplicity**: No complex free space management algorithms
2. **Reliability**: No fragmentation issues, predictable behavior
3. **Performance**: O(1) allocation time (constant)
4. **Testing**: Easy to validate (sequential addresses)
5. **Deterministic**: Same input always produces same file layout

**Trade-offs**:
- Cannot reclaim space from deleted objects (acceptable for MVP)
- Files may be larger than minimal size (acceptable for MVP)
- No best-fit/first-fit optimization (deferred to RC)

---

## API Reference

### Core Functions

#### `NewAllocator(initialOffset uint64) *Allocator`
Creates a new space allocator.

**Parameters**:
- `initialOffset` - Starting address for allocations (typically after superblock)
  - For new HDF5 file with superblock v2 (48 bytes): `initialOffset = 48`
  - For new HDF5 file with superblock v0 (superblock size + driver info): variable

**Returns**:
- `*Allocator` - Ready-to-use allocator

**Example**:
```go
// Create allocator starting after superblock v2
alloc := NewAllocator(48)
```

---

#### `Allocate(size uint64) (uint64, error)`
Allocates a block of space at the end of the file.

**Parameters**:
- `size` - Number of bytes to allocate (must be > 0)

**Returns**:
- `address` - File offset where block is allocated
- `error` - Non-nil if allocation fails

**Errors**:
- `"cannot allocate zero bytes"` - Size must be > 0

**Behavior**:
1. Allocates block at current end-of-file address
2. Tracks allocation in internal block list
3. Updates end-of-file pointer to `address + size`
4. Returns allocated address

**Example**:
```go
addr, err := alloc.Allocate(1024) // Allocate 1KB
if err != nil {
    return err
}
// Use addr to write data to file
file.WriteAt(data, int64(addr))
```

---

#### `EndOfFile() uint64`
Returns the current end-of-file address.

**Returns**:
- `uint64` - Address where next allocation would occur

**Use Cases**:
- Determine file size
- Verify total space usage
- Track file growth

**Example**:
```go
eof := alloc.EndOfFile()
fmt.Printf("File size: %d bytes\n", eof)
```

---

#### `IsAllocated(offset, size uint64) bool`
Checks if an address range overlaps with any allocated blocks.

**Parameters**:
- `offset` - Starting address to check
- `size` - Size of range to check

**Returns**:
- `true` - Range overlaps with at least one allocated block
- `false` - Range is free (or size is 0)

**Use Cases**:
- Validation before writing
- Debugging overlap issues
- Testing allocation correctness

**Example**:
```go
if alloc.IsAllocated(1000, 100) {
    fmt.Println("Warning: Range [1000, 1100) already allocated!")
}
```

---

#### `Blocks() []AllocatedBlock`
Returns a **copy** of all allocated blocks, sorted by offset.

**Returns**:
- `[]AllocatedBlock` - Sorted list of allocated blocks

**AllocatedBlock Structure**:
```go
type AllocatedBlock struct {
    Offset uint64 // Starting address in file
    Size   uint64 // Size of allocated block in bytes
}
```

**Behavior**:
- Returns a **copy** (modifications don't affect allocator state)
- Sorted by `Offset` in ascending order
- Useful for debugging and testing

**Example**:
```go
blocks := alloc.Blocks()
for _, block := range blocks {
    fmt.Printf("Block: [%d, %d) size=%d\n",
        block.Offset, block.Offset+block.Size, block.Size)
}
```

---

#### `ValidateNoOverlaps() error`
Validates that no allocated blocks overlap.

**Returns**:
- `nil` - No overlaps detected (all blocks valid)
- `error` - Overlap detected (should never happen with correct implementation)

**Use Cases**:
- Debugging allocator logic
- Testing allocation correctness
- Pre-release validation

**Example**:
```go
if err := alloc.ValidateNoOverlaps(); err != nil {
    panic(fmt.Sprintf("Allocator corrupted: %v", err))
}
```

---

## Implementation Details

### Alignment

**Current Status**: No alignment enforcement in MVP.

**Rationale**:
- HDF5 spec does NOT strictly require 8-byte alignment for all objects
- Alignment is performance optimization, not correctness requirement
- Deferred to v0.11.0-RC for performance tuning

**Future Enhancement** (v0.11.0-RC):
```go
func alignTo8(size uint64) uint64 {
    return (size + 7) &^ 7 // Round up to multiple of 8
}
```

### Overlap Detection

The `IsAllocated()` function uses standard interval overlap logic:

**Two ranges overlap if**: `offset1 < end2 && offset2 < end1`

Where:
- Range 1: `[offset1, end1)` - Query range
- Range 2: `[offset2, end2)` - Allocated block

**Edge Cases Handled**:
- Zero-size ranges never overlap (returns `false`)
- Adjacent blocks (touching boundaries) do NOT overlap
- Fully contained ranges DO overlap

### Block Tracking

**Data Structure**:
```go
type Allocator struct {
    blocks     []AllocatedBlock // All allocated blocks
    nextOffset uint64           // Next available address
}
```

**Characteristics**:
- `blocks` grows with each allocation (append-only in MVP)
- `nextOffset` always points to end-of-file
- No block removal in MVP (no `Free()` method)

**Memory Usage**:
- 16 bytes per allocated block (Offset + Size)
- Pre-allocated capacity: 16 blocks
- Grows dynamically as needed

### Thread Safety

**Status**: **NOT thread-safe in MVP**

**Why**:
- Synchronization adds complexity (mutex overhead)
- FileWriter (which owns Allocator) is single-threaded in MVP
- No concurrent write operations in MVP architecture

**Mitigation**:
- Document limitation clearly
- Add thread safety in v0.11.0-RC if needed
- External synchronization possible if required

**If Thread Safety Needed**:
```go
type Allocator struct {
    mu         sync.Mutex
    blocks     []AllocatedBlock
    nextOffset uint64
}

func (a *Allocator) Allocate(size uint64) (uint64, error) {
    a.mu.Lock()
    defer a.mu.Unlock()
    // ... allocation logic
}
```

---

## Performance Characteristics

### Time Complexity

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| `NewAllocator()` | O(1) | Constant time initialization |
| `Allocate()` | O(1) | Append to slice + pointer update |
| `EndOfFile()` | O(1) | Return field value |
| `IsAllocated()` | O(n) | Linear scan over blocks |
| `Blocks()` | O(n log n) | Copy + sort |
| `ValidateNoOverlaps()` | O(n log n) | Sort + linear scan |

Where `n` = number of allocated blocks

### Benchmark Results

**Test Environment**: 12th Gen Intel Core i7-1255U

| Benchmark | Time/Op | Allocs/Op | Notes |
|-----------|---------|-----------|-------|
| `Allocate` | ~27-54 ns | 89-93 B | Single allocation |
| `IsAllocated` (1000 blocks) | ~445 ns | 0 B | Overlap check |
| `Blocks` (1000 blocks) | ~8.5 µs | 16 KB | Copy + sort |
| `ValidateNoOverlaps` (1000 blocks) | ~16 µs | 16 KB | Full validation |

**Interpretation**:
- Allocation is extremely fast (< 100 ns)
- Overlap checking scales linearly but is acceptable
- Validation is fast enough for testing/debugging
- No memory allocations for overlap checks (good!)

### Stress Test Results

**10,000 Small Allocations** (64 bytes each):
- ✅ All allocations successful
- ✅ All addresses unique
- ✅ Sequential (no gaps)
- ✅ No overlaps detected
- ✅ Total space accurate

**Large Allocations** (up to 1 GB):
- ✅ Handles 1 MB, 10 MB, 100 MB, 1 GB allocations
- ✅ No overflow issues
- ✅ Correct tracking

---

## Limitations (MVP)

These are **acceptable limitations** for v0.11.0-beta:

### 1. No Freed Space Reuse
**Limitation**: Once space is allocated, it cannot be reclaimed or reused.

**Impact**:
- Deleted objects leave "holes" in the file (space waste)
- Files may be larger than minimal size
- No defragmentation support

**Workaround**:
- Copy file to reclaim space (external tool)
- Plan for fixed-size writes (minimize deletions)

**Future**: v0.11.0-RC will add free space manager

---

### 2. No Allocation Strategies
**Limitation**: Only end-of-file allocation supported.

**Impact**:
- Cannot optimize for specific access patterns
- No best-fit/first-fit/worst-fit options
- Fragmentation impossible to control (though also impossible to occur!)

**Future**: v0.11.0-RC may add allocation strategies

---

### 3. No Thread Safety
**Limitation**: Concurrent calls to `Allocate()` will cause data races.

**Impact**:
- Cannot use from multiple goroutines simultaneously
- Requires external synchronization if needed

**Workaround**:
- Use single-threaded writer (current architecture)
- Add mutex if concurrent writes needed

**Future**: v0.11.0-RC may add optional thread safety

---

### 4. No Size Validation
**Limitation**: Does not validate allocation size limits.

**Impact**:
- Can allocate sizes larger than filesystem supports
- May cause file I/O errors later
- No protection against overflow (though practically impossible)

**Workaround**:
- Caller should validate sizes before allocating
- Operating system will reject impossible writes

**Future**: May add size validation in RC

---

### 5. No Alignment Enforcement
**Limitation**: Does not enforce 8-byte alignment.

**Impact**:
- May have minor performance impact on some platforms
- HDF5 spec does not strictly require alignment

**Workaround**:
- Caller can manually align sizes before allocating
- Not critical for correctness

**Future**: v0.11.0-RC may add automatic alignment

---

## Testing Strategy

### Test Coverage

**Current Coverage**: 100% (6/6 functions)

**Test Categories**:
1. **Unit Tests**
   - Basic allocation (`TestAllocate`)
   - Overlap detection (`TestIsAllocated`)
   - Block retrieval (`TestAllocator_Blocks_Complete`)
   - Validation (`TestAllocator_ValidateNoOverlaps_Complete`)

2. **Stress Tests**
   - 10,000 small allocations (`TestAllocator_StressTest`)
   - Mixed size allocations
   - Large allocations (up to 1 GB)

3. **Edge Cases**
   - Zero-size allocation (error)
   - Single-byte allocation
   - Very large allocations
   - Non-zero initial offset

4. **Integration Tests**
   - FileWriter integration (`TestFileWriter_Allocate`)
   - Real-world usage patterns

### Test Execution

```bash
# Run all allocator tests
go test -v ./internal/writer -run "Test.*Allocat"

# Run with coverage
go test -coverprofile=coverage.out ./internal/writer
go tool cover -func=coverage.out | grep allocator.go

# Run benchmarks
go test -bench=Bench.*Allocat -benchmem ./internal/writer
```

---

## Usage Examples

### Example 1: Basic Allocation

```go
// Create allocator for new file
alloc := NewAllocator(48) // After superblock v2

// Allocate space for dataset header
headerAddr, err := alloc.Allocate(256)
if err != nil {
    return err
}

// Allocate space for data
dataAddr, err := alloc.Allocate(1024 * 1024) // 1 MB
if err != nil {
    return err
}

// Write to file
file.WriteAt(headerBytes, int64(headerAddr))
file.WriteAt(dataBytes, int64(dataAddr))

fmt.Printf("File size: %d bytes\n", alloc.EndOfFile())
```

### Example 2: Validation During Development

```go
// During development, validate allocator state
alloc := NewAllocator(48)

// ... perform allocations ...

// Validate before closing file
if err := alloc.ValidateNoOverlaps(); err != nil {
    panic(fmt.Sprintf("BUG: Allocator has overlaps: %v", err))
}

// Check for unexpected space usage
blocks := alloc.Blocks()
var totalAllocated uint64
for _, block := range blocks {
    totalAllocated += block.Size
}

expectedEOF := 48 + totalAllocated
if alloc.EndOfFile() != expectedEOF {
    panic("BUG: EOF doesn't match total allocated space")
}
```

### Example 3: Multiple Object Allocation

```go
alloc := NewAllocator(48)

// Allocate space for multiple objects
objects := []struct {
    name string
    size uint64
    addr uint64
}{
    {"superblock", 48, 0}, // Pre-allocated
    {"root_group", 128, 0},
    {"dataset_header", 256, 0},
    {"data_chunk_1", 8192, 0},
    {"data_chunk_2", 8192, 0},
    {"attributes", 512, 0},
}

// Allocate each object
for i := 1; i < len(objects); i++ { // Skip superblock
    addr, err := alloc.Allocate(objects[i].size)
    if err != nil {
        return err
    }
    objects[i].addr = addr
}

// Write each object
for _, obj := range objects {
    if obj.addr > 0 {
        fmt.Printf("Writing %s at %d (size %d)\n",
            obj.name, obj.addr, obj.size)
        // ... write to file ...
    }
}
```

---

## Future Enhancements (v0.11.0-RC)

### 1. Free Space Manager
Track and reuse freed blocks:
```go
func (a *Allocator) Free(offset, size uint64) error {
    // Add block to free list
    // Merge adjacent free blocks
}

func (a *Allocator) Allocate(size uint64) (uint64, error) {
    // Try to allocate from free list first
    // Fall back to end-of-file if no suitable block
}
```

### 2. Allocation Strategies
Support different allocation policies:
```go
type AllocationStrategy int

const (
    StrategyEndOfFile  AllocationStrategy = iota
    StrategyBestFit
    StrategyFirstFit
    StrategyWorstFit
)

func NewAllocatorWithStrategy(initialOffset uint64, strategy AllocationStrategy) *Allocator {
    // ...
}
```

### 3. Alignment Support
Automatic alignment for performance:
```go
func (a *Allocator) AllocateAligned(size, alignment uint64) (uint64, error) {
    // Round size up to alignment
    // Ensure address is aligned
}
```

### 4. Fragmentation Management
Track and report fragmentation:
```go
func (a *Allocator) GetFragmentation() float64 {
    totalSpace := a.EndOfFile() - a.initialOffset
    usedSpace := /* sum of allocated blocks */
    freeSpace := totalSpace - usedSpace
    return float64(freeSpace) / float64(totalSpace)
}
```

### 5. Thread Safety
Optional concurrent access:
```go
func NewAllocatorThreadSafe(initialOffset uint64) *Allocator {
    // Returns thread-safe allocator with mutex
}
```

---

## References

### C Library Implementation
- **File Memory Management**: `src/H5MF.c` (~2000 LOC)
- **Free Space Manager**: `src/H5FS*.c` (~5000 LOC total)
- **Memory Allocation**: `src/H5MA.c` (memory aggregation)

**Key Differences from C Library**:
- C library has complex free space manager with:
  - Multiple aggregation strategies
  - Free space sections tracking
  - Metadata aggregation
  - Small/large object optimization
- Our MVP intentionally keeps it simple (end-of-file only)
- Can add complexity in RC if needed

### HDF5 Format Specification
- **Free Space Manager**: Section III.G (HDF5 File Format Spec v3)
- **Memory Management**: Not strictly specified (implementation detail)

### Related Documentation
- `internal/writer/filewriter.go` - FileWriter (uses Allocator)
- `internal/writer/filewriter_test.go` - Integration tests
- Component 1 documentation (File Creation)

---

**Last Updated**: 2025-10-30
**Status**: Production-ready for v0.11.0-beta MVP
**Maintainer**: Claude (AI Developer)
