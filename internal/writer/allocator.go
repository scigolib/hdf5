package writer

import (
	"fmt"
	"sort"
)

// AllocatedBlock tracks an allocated region of the file.
// Each block represents a contiguous region that has been allocated
// and must not be overwritten or reused (in MVP version).
type AllocatedBlock struct {
	Offset uint64 // Starting address in file
	Size   uint64 // Size of allocated block in bytes
}

// Allocator manages space allocation in the HDF5 file.
// For MVP (v0.11.0-beta), it uses simple end-of-file allocation strategy:
// - New allocations always occur at the end of the file
// - No reuse of freed space (freed space is tracked but not reused)
// - No fragmentation handling
// - Prevents overlapping allocations
//
// Advanced features (deferred to v0.11.0-RC):
// - Free space reuse (best-fit, first-fit strategies)
// - Fragmentation management
// - Free space sections merging
type Allocator struct {
	blocks     []AllocatedBlock // Sorted list of allocated blocks
	nextOffset uint64           // Next available address for allocation
}

// NewAllocator creates a space allocator.
// initialOffset is the starting address for allocations (typically after superblock).
// For a new HDF5 file with superblock v2 (48 bytes), initialOffset would be 48.
func NewAllocator(initialOffset uint64) *Allocator {
	return &Allocator{
		blocks:     make([]AllocatedBlock, 0, 16), // Pre-allocate capacity
		nextOffset: initialOffset,
	}
}

// Allocate reserves a block of space at the end of the file.
// Returns the address where the block was allocated.
// The allocated block is tracked to prevent overlaps.
//
// For MVP:
// - Always allocates at end of file (nextOffset)
// - No alignment requirements (defer to RC)
// - No size limits validation
//
// Example:
//
//	addr, err := allocator.Allocate(1024) // Allocate 1KB
//	if err != nil {
//	    return err
//	}
//	// Use addr to write data
func (a *Allocator) Allocate(size uint64) (uint64, error) {
	if size == 0 {
		return 0, fmt.Errorf("cannot allocate zero bytes")
	}

	// Allocate at current end of file
	addr := a.nextOffset

	// Record the allocation
	block := AllocatedBlock{
		Offset: addr,
		Size:   size,
	}
	a.blocks = append(a.blocks, block)

	// Move next offset to end of this allocation
	a.nextOffset = addr + size

	return addr, nil
}

// IsAllocated checks if an address range overlaps with any allocated blocks.
// Returns true if the range [offset, offset+size) overlaps with existing allocations.
// Useful for validation and debugging.
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
// This is where the next allocation would occur.
func (a *Allocator) EndOfFile() uint64 {
	return a.nextOffset
}

// Blocks returns a copy of all allocated blocks, sorted by offset.
// Useful for debugging and testing.
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
// Returns an error if overlaps are detected.
// This is primarily for debugging and testing.
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
