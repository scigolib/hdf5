// Package writer provides HDF5 file writing infrastructure.
//
// The Allocator manages free space allocation in HDF5 files.
// For v0.11.0-beta MVP, it uses a simple end-of-file allocation strategy
// with no freed space reuse.
//
// See ALLOCATOR_DESIGN.md for comprehensive design documentation.
package writer

import (
	"fmt"
	"sort"
)

// AllocatedBlock tracks an allocated region of the file.
//
// Each block represents a contiguous region that has been allocated
// and must not be overwritten or reused (in MVP version).
//
// Blocks are tracked to prevent overlapping allocations and to
// validate allocator integrity during testing.
type AllocatedBlock struct {
	Offset uint64 // Starting address in file
	Size   uint64 // Size of allocated block in bytes
}

// FreeBlock represents a freed region of the file available for reuse.
type FreeBlock struct {
	Offset uint64
	Size   uint64
}

// Allocator manages space allocation in HDF5 files.
//
// Strategy:
//   - End-of-file allocation: New allocations occur at end of file
//   - Free space reuse: Freed blocks are tracked and reused (best-fit)
//   - Adjacent coalescing: Adjacent free blocks are merged automatically
//   - EOF shrink: Freeing the last block shrinks the file
//   - Overlap prevention: All allocations tracked
//
// Thread Safety:
//   - NOT thread-safe: Use external synchronization if needed
//   - Designed for single-threaded FileWriter
//
// Performance:
//   - Allocate: O(n) where n = number of free blocks (best-fit search)
//   - Free: O(n) where n = number of free blocks (coalesce scan)
//   - IsAllocated: O(n) - linear scan over blocks
//   - Blocks: O(n log n) - copy and sort
//   - ValidateNoOverlaps: O(n log n) - sort and scan
//
// Reference: H5MF.c - H5MF_xfree(), H5MF__add_sect(), H5MF_try_shrink().
//
// See ALLOCATOR_DESIGN.md for detailed design documentation.
type Allocator struct {
	blocks     []AllocatedBlock // All allocated blocks
	freeList   []FreeBlock      // Free blocks sorted by offset
	nextOffset uint64           // Next available address (end-of-file)
}

// NewAllocator creates a space allocator.
//
// The allocator tracks all allocations and manages free space in the HDF5 file.
// It uses end-of-file allocation strategy (no freed space reuse in MVP).
//
// Parameters:
//   - initialOffset: Starting address for allocations (typically after superblock)
//   - For superblock v2 (48 bytes): initialOffset = 48
//   - For superblock v0 (variable size): initialOffset = superblock_size + driver_info_size
//
// Returns:
//   - *Allocator ready to allocate space
//
// Example:
//
//	alloc := NewAllocator(48) // Start after superblock v2
//	addr, err := alloc.Allocate(1024)
//	if err != nil {
//	    return err
//	}
func NewAllocator(initialOffset uint64) *Allocator {
	return &Allocator{
		blocks:     make([]AllocatedBlock, 0, 16), // Pre-allocate capacity for 16 blocks
		nextOffset: initialOffset,
	}
}

// Allocate reserves a block of space at the end of the file.
//
// The block is allocated at the current end-of-file address and tracked
// to prevent overlapping allocations. This is the primary method for
// obtaining space for HDF5 objects (datasets, groups, attributes, metadata).
//
// Strategy:
//   - Allocates at current end-of-file (sequential allocation)
//   - Updates end-of-file pointer to addr + size
//   - Tracks allocation in internal block list
//   - No alignment enforcement (deferred to RC)
//   - No size limit validation (OS will reject impossible sizes)
//
// Parameters:
//   - size: Number of bytes to allocate (must be > 0)
//
// Returns:
//   - address: File offset where block is allocated
//   - error: Non-nil if allocation fails
//
// Errors:
//   - "cannot allocate zero bytes": Size must be greater than 0
//
// Thread Safety:
//   - NOT thread-safe: Do not call concurrently
//
// Example:
//
//	addr, err := allocator.Allocate(1024) // Allocate 1KB
//	if err != nil {
//	    return err
//	}
//	// Use addr to write data to file
//	file.WriteAt(data, int64(addr))
func (a *Allocator) Allocate(size uint64) (uint64, error) {
	if size == 0 {
		return 0, fmt.Errorf("cannot allocate zero bytes")
	}

	// Try to reuse freed space (best-fit strategy).
	// Best-fit minimizes wasted space by choosing the smallest free block
	// that can satisfy the request.
	bestIdx := -1
	var bestSize uint64
	for i, fb := range a.freeList {
		if fb.Size >= size {
			if bestIdx == -1 || fb.Size < bestSize {
				bestIdx = i
				bestSize = fb.Size
			}
		}
	}

	if bestIdx >= 0 {
		fb := a.freeList[bestIdx]
		addr := fb.Offset

		if fb.Size == size {
			// Exact fit: remove from free list.
			a.freeList = append(a.freeList[:bestIdx], a.freeList[bestIdx+1:]...)
		} else {
			// Partial fit: shrink free block.
			a.freeList[bestIdx].Offset += size
			a.freeList[bestIdx].Size -= size
		}

		// Record the allocation.
		a.blocks = append(a.blocks, AllocatedBlock{Offset: addr, Size: size})
		return addr, nil
	}

	// No suitable free block found: allocate at end of file.
	addr := a.nextOffset

	// Record the allocation.
	a.blocks = append(a.blocks, AllocatedBlock{Offset: addr, Size: size})

	// Move next offset to end of this allocation.
	a.nextOffset = addr + size

	return addr, nil
}

// IsAllocated checks if an address range overlaps with any allocated blocks.
//
// This method is useful for validation and debugging to ensure no
// overlapping writes occur. It performs a linear scan over all allocated blocks.
//
// Overlap Detection Logic:
//   - Two ranges [a1,a2) and [b1,b2) overlap if: a1 < b2 && b1 < a2
//   - Adjacent blocks (touching boundaries) do NOT overlap
//   - Zero-size ranges never overlap (returns false)
//
// Parameters:
//   - offset: Starting address of range to check
//   - size: Size of range to check
//
// Returns:
//   - true: Range overlaps with at least one allocated block
//   - false: Range is free (or size is 0)
//
// Performance:
//   - Time: O(n) where n is number of allocated blocks
//   - Space: O(1) - no allocations
//
// Use Cases:
//   - Validation before writing to file
//   - Debugging overlap issues
//   - Testing allocation correctness
//
// Example:
//
//	if alloc.IsAllocated(1000, 100) {
//	    fmt.Println("Warning: Range [1000, 1100) already allocated!")
//	}
func (a *Allocator) IsAllocated(offset, size uint64) bool {
	if size == 0 {
		return false
	}

	rangeEnd := offset + size

	for _, block := range a.blocks {
		blockEnd := block.Offset + block.Size

		// Check for overlap:
		// Two ranges [a1,a2) and [b1,b2) overlap if: a1 < b2 && b1 < a2
		if offset < blockEnd && block.Offset < rangeEnd {
			return true
		}
	}

	return false
}

// EndOfFile returns the current end-of-file address.
//
// This is where the next allocation would occur. It represents the
// total file size including all allocated blocks.
//
// Returns:
//   - uint64: Current end-of-file address (next allocation address)
//
// Performance:
//   - Time: O(1) - constant time
//   - Space: O(1) - no allocations
//
// Use Cases:
//   - Determine total file size
//   - Verify space usage
//   - Track file growth
//
// Example:
//
//	eof := alloc.EndOfFile()
//	fmt.Printf("File size: %d bytes\n", eof)
func (a *Allocator) EndOfFile() uint64 {
	return a.nextOffset
}

// Blocks returns a copy of all allocated blocks, sorted by offset.
//
// The returned slice is a copy, so modifications do not affect the
// allocator's internal state. Blocks are sorted by offset in ascending
// order for consistent iteration and display.
//
// Returns:
//   - []AllocatedBlock: Copy of all allocated blocks, sorted by offset
//
// Performance:
//   - Time: O(n log n) where n is number of blocks (due to sorting)
//   - Space: O(n) - allocates copy of blocks
//
// Use Cases:
//   - Debugging allocation patterns
//   - Testing allocator state
//   - Visualizing file layout
//   - Calculating total allocated space
//
// Example:
//
//	blocks := alloc.Blocks()
//	for _, block := range blocks {
//	    fmt.Printf("Block: [%d, %d) size=%d\n",
//	        block.Offset, block.Offset+block.Size, block.Size)
//	}
//
//	// Calculate total allocated space
//	var total uint64
//	for _, block := range blocks {
//	    total += block.Size
//	}
func (a *Allocator) Blocks() []AllocatedBlock {
	// Make a copy to prevent external modification
	blocks := make([]AllocatedBlock, len(a.blocks))
	copy(blocks, a.blocks)

	// Sort by offset for consistent output
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Offset < blocks[j].Offset
	})

	return blocks
}

// ValidateNoOverlaps checks that no allocated blocks overlap.
//
// This method is primarily for debugging and testing to ensure the
// allocator maintains correct state. In a correctly functioning allocator
// with end-of-file allocation, overlaps should NEVER occur.
//
// Detection Logic:
//   - Sorts blocks by offset
//   - Checks that each block ends before the next block starts
//   - Adjacent blocks (touching boundaries) are NOT considered overlapping
//
// Returns:
//   - nil: No overlaps detected (allocator state is valid)
//   - error: Overlap detected (indicates allocator bug)
//
// Performance:
//   - Time: O(n log n) where n is number of blocks (due to sorting)
//   - Space: O(n) - allocates sorted copy of blocks
//
// Use Cases:
//   - Debugging allocator implementation
//   - Pre-release validation
//   - Testing allocation correctness
//   - Detecting memory corruption
//
// Example:
//
//	if err := alloc.ValidateNoOverlaps(); err != nil {
//	    panic(fmt.Sprintf("BUG: Allocator corrupted: %v", err))
//	}
func (a *Allocator) ValidateNoOverlaps() error {
	blocks := a.Blocks() // Get sorted blocks

	for i := 0; i < len(blocks)-1; i++ {
		current := blocks[i]
		next := blocks[i+1]

		currentEnd := current.Offset + current.Size

		// Check if current block extends into next block
		if currentEnd > next.Offset {
			return fmt.Errorf("overlap detected: block at %d (size %d) overlaps block at %d",
				current.Offset, current.Size, next.Offset)
		}
	}

	return nil
}

// Free returns a previously allocated block to the free list for reuse.
//
// The freed block is added to the free list, and adjacent free blocks
// are coalesced into larger blocks to reduce fragmentation. If the freed
// block is at the end of the file, the EOF pointer is shrunk instead of
// adding to the free list (per H5MF_try_shrink pattern).
//
// Parameters:
//   - offset: Starting address of the block to free
//   - size: Size of the block to free (must be > 0)
//
// Returns:
//   - error: Non-nil if the free operation fails
//
// Errors:
//   - "cannot free zero bytes": Size must be greater than 0
//
// Thread Safety:
//   - NOT thread-safe: Do not call concurrently
//
// Reference: H5MF.c - H5MF_xfree(), H5MF__add_sect(), H5MF_try_shrink().
//
// Example:
//
//	addr, _ := alloc.Allocate(1024)
//	alloc.Free(addr, 1024) // Return block to free list
func (a *Allocator) Free(offset, size uint64) error {
	if size == 0 {
		return fmt.Errorf("cannot free zero bytes")
	}

	// Remove from allocated blocks list.
	removed := false
	for i, b := range a.blocks {
		if b.Offset == offset && b.Size == size {
			a.blocks = append(a.blocks[:i], a.blocks[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		// Allow freeing sub-blocks that aren't exactly tracked (e.g., chunk data
		// within a larger allocation). Still add to free list for future reuse.
		_ = removed
	}

	// EOF optimization: if freed block is at the end of file, shrink EOF
	// instead of adding to the free list. Per C reference H5MF_try_shrink().
	if offset+size == a.nextOffset {
		a.nextOffset = offset

		// Check if any free blocks at new tail can also be absorbed.
		a.shrinkTrailingFreeBlocks()
		return nil
	}

	// Add to free list and coalesce with adjacent blocks.
	a.addToFreeList(offset, size)
	return nil
}

// addToFreeList inserts a free block into the sorted free list and coalesces
// adjacent blocks. The free list is kept sorted by offset.
func (a *Allocator) addToFreeList(offset, size uint64) {
	newBlock := FreeBlock{Offset: offset, Size: size}

	// Find insertion point (maintain sorted order by offset).
	insertIdx := sort.Search(len(a.freeList), func(i int) bool {
		return a.freeList[i].Offset >= offset
	})

	// Insert the new block.
	a.freeList = append(a.freeList, FreeBlock{})
	copy(a.freeList[insertIdx+1:], a.freeList[insertIdx:])
	a.freeList[insertIdx] = newBlock

	// Coalesce with the next block if adjacent.
	if insertIdx+1 < len(a.freeList) {
		next := a.freeList[insertIdx+1]
		if a.freeList[insertIdx].Offset+a.freeList[insertIdx].Size == next.Offset {
			a.freeList[insertIdx].Size += next.Size
			a.freeList = append(a.freeList[:insertIdx+1], a.freeList[insertIdx+2:]...)
		}
	}

	// Coalesce with the previous block if adjacent.
	if insertIdx > 0 {
		prev := a.freeList[insertIdx-1]
		if prev.Offset+prev.Size == a.freeList[insertIdx].Offset {
			a.freeList[insertIdx-1].Size += a.freeList[insertIdx].Size
			a.freeList = append(a.freeList[:insertIdx], a.freeList[insertIdx+1:]...)
		}
	}
}

// shrinkTrailingFreeBlocks absorbs any free blocks that are now at the tail
// of the file after an EOF shrink. This can happen when freeing a block
// before the last block, and the last block was already freed.
func (a *Allocator) shrinkTrailingFreeBlocks() {
	for len(a.freeList) > 0 {
		last := a.freeList[len(a.freeList)-1]
		if last.Offset+last.Size == a.nextOffset {
			// This free block is at the tail; absorb it.
			a.nextOffset = last.Offset
			a.freeList = a.freeList[:len(a.freeList)-1]
		} else {
			break
		}
	}
}

// FreeBlocks returns a copy of all free blocks, sorted by offset.
// This is primarily for testing and debugging.
func (a *Allocator) FreeBlocks() []FreeBlock {
	result := make([]FreeBlock, len(a.freeList))
	copy(result, a.freeList)
	return result
}
