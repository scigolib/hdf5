// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// Fractal heap write constants.
const (
	HeapSignature        = "FRHP" // Heap header signature
	DirectBlockSignature = "FHDB" // Direct block signature

	DefaultHeapIDLength         = 8      // 8-byte object IDs (H5O_FHEAP_ID_LEN, H5Oprivate.h:64)
	DefaultMaxManagedObjectSize = 65536  // 64KB
	DefaultStartingBlockSize    = 524288 // 512KB starting block
	// DefaultTableWidth is the doubling-table width of the C library's dense
	// attribute and link heaps (H5O_FHEAP_MAN_WIDTH, H5Oprivate.h:57;
	// H5G_FHEAP_MAN_WIDTH, H5Gdense.c:43).
	DefaultTableWidth = 4

	// DenseHeapBlockSize is the single direct block of the dense attribute
	// and link heaps this writer creates. It is the C library's maximum
	// direct block size for those heaps (H5O_FHEAP_MAN_MAX_DIRECT_SIZE,
	// H5Oprivate.h:59; H5G_FHEAP_MAN_MAX_DIRECT_SIZE, H5Gdense.c:45), so the
	// heap length field is 2 bytes and a link heap ID fits the 7 bytes of a
	// link name record.
	DenseHeapBlockSize = 64 * 1024

	// heapFlagChecksumDirectBlocks: direct blocks carry a checksum in their
	// prefix (H5HF_HDR_FLAGS_CHECKSUM_DBLOCKS, H5HFpkg.h:61). The C library
	// sets it for every dense attribute and link heap
	// (H5O_FHEAP_CHECKSUM_DBLOCKS, H5Oprivate.h:62; H5Gdense.c:48).
	heapFlagChecksumDirectBlocks = 0x02
	heapChecksumSize             = 4 // H5HF_SIZEOF_CHKSUM, H5HFpkg.h:39

	// heapStartRootRows: H5O_FHEAP_MAN_START_ROOT_ROWS (H5Oprivate.h:61),
	// H5G_FHEAP_MAN_START_ROOT_ROWS (H5Gdense.c:47).
	heapStartRootRows = 1

	// attrHeapMaxIndex and linkHeapMaxIndex are the heap address-space bits
	// (H5O_FHEAP_MAN_MAX_INDEX, H5Oprivate.h:60; H5G_FHEAP_MAN_MAX_INDEX,
	// H5Gdense.c:46). They set the heap offset size: 5 and 4 bytes.
	attrHeapMaxIndex = 40
	linkHeapMaxIndex = 32

	// linkHeapIDLength is the heap ID length of a link heap
	// (H5G_DENSE_FHEAP_ID_LEN, H5Gpkg.h:40): 1 + 4 + 2 as H5HFhdr.c:416-419
	// computes it for the cparams of H5Gdense.c.
	linkHeapIDLength = 7

	// heapUndefAddress is HADDR_UNDEF. The C library writes it for the
	// huge-object B-tree and the free-space manager until they exist
	// (H5HFhdr.c:357, 360).
	heapUndefAddress = ^uint64(0)
)

// Fractal heap error definitions.
var (
	ErrHeapFull        = errors.New("fractal heap is full")
	ErrObjectTooLarge  = errors.New("object size exceeds maximum managed size")
	ErrEmptyObject     = errors.New("cannot insert empty object")
	ErrInvalidObjectID = errors.New("invalid object ID")
	ErrObjectNotFound  = errors.New("object not found in heap")
)

// WritableFractalHeap represents a fractal heap that supports writing.
// This implementation supports both direct and indirect block structures.
//
// Structure:
// - Small heaps (single direct block): Use DirectBlock directly
// - Large heaps (multiple blocks): Use RootIndirectBlock with child blocks
//
// Transition: When direct block fills up, heap automatically transitions to
// indirect block structure, moving existing data to first child block.
//
// Limitations:
// - Single-level indirect blocks (no multiply-indirect yet)
// - No filters/compression (MVP)
// - No huge objects (MVP)
// - No tiny object optimization (MVP)
//
// Reference: H5HF.c, H5HFhdr.c, H5HFdblock.c, H5HFiblock.c.
type WritableFractalHeap struct {
	Header      *WritableHeapHeader
	DirectBlock *WritableDirectBlock // Used when root is direct block (small heaps)

	// Indirect block support (for large heaps)
	RootIndirectBlock  *WritableIndirectBlock          // Non-nil when root is indirect block
	MaxDirectBlockSize uint64                          // When to transition to indirect
	DirectBlocks       map[uint64]*WritableDirectBlock // Child blocks (key = block offset)

	// Addresses loaded from file (for RMW scenarios)
	loadedHeaderAddress      uint64
	loadedDirectBlockAddress uint64

	// sizeofAddr is the file address size the direct block prefix holds
	// (H5HF_MAN_ABS_DIRECT_OVERHEAD, H5HFpkg.h:102-109). Object offsets
	// include that prefix, so it must be known before the first insert:
	// SetAddressSize, or LoadFromFile from the superblock.
	sizeofAddr uint8

	// adopted is false between LoadFromFile and AdoptObjects: the heap
	// keeps no record of where its free space starts (this writer drops the
	// C free-space manager), so inserts wait for the live objects.
	adopted bool

	// legacy marks a heap written by hdf5-go v0.14.2 or earlier, whose
	// object offsets start after the direct block prefix; see
	// isLegacyHeapHeader. AdoptObjects rewrites it in the C layout.
	legacy bool

	// src is the heap LoadFromFile opened with the core reader, through
	// which AdoptObjects reads and checks the live objects; nil otherwise.
	src *core.FractalHeap
}

// WritableHeapHeader represents a fractal heap header for writing.
// Simplified from FractalHeapHeader for MVP write support.
type WritableHeapHeader struct {
	// Core fields
	Version         uint8
	HeapIDLength    uint16 // Typically 8 bytes
	IOFiltersLength uint16 // 0 for MVP
	Flags           uint8  // Bit flags

	// Object limits
	MaxManagedObjectSize uint32 // Typically 64KB

	// Huge object support (MVP: all zeros)
	NextHugeObjectID    uint64
	HugeObjectBTreeAddr uint64

	// Free space management
	FreeSpace          uint64 // Free space in managed blocks
	FreeSectionAddress uint64 // 0 for MVP (no free space manager)

	// Heap statistics
	ManagedSpaceSize      uint64 // Total managed space
	AllocatedManagedSpace uint64 // Total allocated
	ManagedSpaceOffset    uint64 // Next insert position (iterator offset)
	NumManagedObjects     uint64 // Object count

	// Huge/Tiny object stats (MVP: all zeros)
	SizeHugeObjects uint64
	NumHugeObjects  uint64
	SizeTinyObjects uint64
	NumTinyObjects  uint64

	// Doubling table parameters
	TableWidth         uint16 // Width of doubling table (MVP: 2)
	StartingBlockSize  uint64 // Size of first direct block
	MaxDirectBlockSize uint64 // Same as starting for MVP
	MaxHeapSize        uint16 // Max rows (MVP: 16 to allow offset encoding)
	StartingNumRows    uint16 // For indirect blocks (MVP: 0)
	RootBlockAddress   uint64 // Address of direct block
	CurrentNumRows     uint16 // For indirect blocks (MVP: 0)

	// Computed values (not stored, but needed for encoding)
	HeapOffsetSize uint8 // Size of heap offsets (bytes)
	HeapLengthSize uint8 // Size of heap lengths (bytes)
}

// WritableDirectBlock represents a direct block for writing.
type WritableDirectBlock struct {
	Version           uint8
	HeapHeaderAddress uint64
	BlockOffset       uint64 // Offset within heap (0 for first block)
	Size              uint64 // Total block size
	Objects           []byte // Raw object data
	FreeOffset        uint64 // Offset of next free space
	ChecksumEnabled   bool   // Whether to add checksum
}

// NewWritableFractalHeap creates a fractal heap for writing with one direct
// block of blockSize bytes (a power of two), 8-byte heap IDs and the link
// heap's 32-bit address space, which keeps an 8-byte ID valid for any block
// size up to 16 MiB. The dense attribute and link writers use
// NewAttributeFractalHeap and NewLinkFractalHeap, which match the C library's
// heaps exactly.
//
// Parameters:
// - blockSize: size of the direct block (use DefaultStartingBlockSize)
//
// Returns:
// - *WritableFractalHeap: heap structure ready for object insertion.
func NewWritableFractalHeap(blockSize uint64) *WritableFractalHeap {
	return newFractalHeap(blockSize, linkHeapMaxIndex, DefaultHeapIDLength)
}

// NewAttributeFractalHeap creates the fractal heap of dense attribute storage
// as H5A__dense_create (H5Adense.c:182-205) does, with one direct block of
// DenseHeapBlockSize as root: 8-byte heap IDs, a 40-bit address space,
// checksummed direct blocks.
func NewAttributeFractalHeap() *WritableFractalHeap {
	return newFractalHeap(DenseHeapBlockSize, attrHeapMaxIndex, DefaultHeapIDLength)
}

// NewLinkFractalHeap creates the fractal heap of dense link storage as
// H5G__dense_create (H5Gdense.c:262-285) does, with one direct block of
// DenseHeapBlockSize as root: 7-byte heap IDs (H5G_DENSE_FHEAP_ID_LEN), a
// 32-bit address space, checksummed direct blocks.
func NewLinkFractalHeap() *WritableFractalHeap {
	return newFractalHeap(DenseHeapBlockSize, linkHeapMaxIndex, linkHeapIDLength)
}

// newFractalHeap creates a heap whose root is one direct block of blockSize,
// that block also being the maximum direct block size. The header fields are
// those H5HF__hdr_create (H5HFhdr.c:310-460) and H5HF__man_dblock_new
// (H5HFdblock.c:347-366) leave for an empty heap with a root direct block:
// no free-space manager and no huge-object B-tree yet (HADDR_UNDEF), the
// iterator offset 0 (the root block does not advance it), managed and
// allocated space the block size, and the block's free space after its
// prefix. validateHeapParams checks the result before the heap is written.
func newFractalHeap(blockSize uint64, maxIndex, idLen uint16) *WritableFractalHeap {
	maxMan := uint32(DefaultMaxManagedObjectSize)
	header := &WritableHeapHeader{
		Version:         0,
		HeapIDLength:    idLen,
		IOFiltersLength: 0,
		Flags:           heapFlagChecksumDirectBlocks,

		MaxManagedObjectSize: maxMan,
		NextHugeObjectID:     0,
		HugeObjectBTreeAddr:  heapUndefAddress,

		FreeSectionAddress: heapUndefAddress,

		ManagedSpaceSize:      blockSize,
		AllocatedManagedSpace: blockSize,
		ManagedSpaceOffset:    0,
		NumManagedObjects:     0,

		TableWidth:         DefaultTableWidth,
		StartingBlockSize:  blockSize,
		MaxDirectBlockSize: blockSize,
		MaxHeapSize:        maxIndex,
		StartingNumRows:    heapStartRootRows,
		RootBlockAddress:   0, // Will be set when written
		CurrentNumRows:     0, // 0 = direct block at root

		// H5HF_SIZEOF_OFFSET_BITS, H5HFhdr.c:194. maxIndex is one of this
		// package's heap constants; validateHeapParams checks it.
		HeapOffsetSize: uint8((maxIndex + 7) / 8), //nolint:gosec // G115: at most 64 bits, so at most 8
		HeapLengthSize: heapLengthSize(blockSize, maxMan),
	}

	fh := &WritableFractalHeap{
		Header: header,
		DirectBlock: &WritableDirectBlock{
			Version:         0,
			BlockOffset:     0,
			Size:            blockSize,
			ChecksumEnabled: true,
		},
		RootIndirectBlock:  nil,
		MaxDirectBlockSize: blockSize,
		DirectBlocks:       make(map[uint64]*WritableDirectBlock),
		sizeofAddr:         8,
		adopted:            true,
	}
	fh.resetDirectBlock()
	return fh
}

// heapLengthSize is the size of the length field of a managed heap ID:
// MIN(max_dir_blk_off_size, H5VM_limit_enc_size(max_man_size))
// (H5HFhdr.c:199-200), the first being H5HF_SIZEOF_OFFSET_LEN of the maximum
// direct block size (H5HFdtable.c:96, H5HFpkg.h:134) and the second
// log2(max_man_size)/8 + 1 (H5VMprivate.h:448-451).
func heapLengthSize(maxDirect uint64, maxMan uint32) uint8 {
	// log2Floor of a uint64 is 0..63, so both widths are at most 8.
	dirOff := uint8((log2Floor(maxDirect) + 7) / 8) //nolint:gosec // G115: 0..63 bits
	enc := uint8(log2Floor(uint64(maxMan))/8 + 1)   //nolint:gosec // G115: 0..63 bits
	return min(dirOff, enc)
}

// log2Floor is H5VM_log2_gen: the index of the highest set bit, 0 for 0.
func log2Floor(v uint64) int {
	if v == 0 {
		return 0
	}
	return bits.Len64(v) - 1
}

// directBlockOverhead is H5HF_MAN_ABS_DIRECT_OVERHEAD (H5HFpkg.h:102-109):
// signature, version, heap header address, block offset and, when direct
// blocks are checksummed, the checksum. A managed object's heap offset is
// relative to the start of its block, so no object lies below it
// (H5HFman.c:388-391).
func (fh *WritableFractalHeap) directBlockOverhead() uint64 {
	n := uint64(4+1) + uint64(fh.sizeofAddr) + uint64(fh.Header.HeapOffsetSize)
	if fh.Header.Flags&heapFlagChecksumDirectBlocks != 0 {
		n += heapChecksumSize
	}
	return n
}

// resetDirectBlock empties the root direct block: its image holds the
// prefix (filled in when written) and the free space starts after it, as
// H5HF__man_dblock_create leaves a new block (H5HFdblock.c:136-173).
func (fh *WritableFractalHeap) resetDirectBlock() {
	ovh := fh.directBlockOverhead()
	fh.DirectBlock.Objects = make([]byte, ovh, fh.DirectBlock.Size)
	fh.DirectBlock.FreeOffset = ovh
	fh.Header.FreeSpace = fh.DirectBlock.Size - ovh
	fh.Header.NumManagedObjects = 0
}

// SetAddressSize sets the file address size (the superblock's OffsetSize)
// of an empty heap; the direct block prefix and so every object offset
// depend on it. A heap holding objects keeps its size and reports a
// mismatch.
func (fh *WritableFractalHeap) SetAddressSize(sizeofAddr uint8) error {
	if sizeofAddr == fh.sizeofAddr {
		return nil
	}
	if fh.Header.NumManagedObjects != 0 || !fh.adopted || fh.RootIndirectBlock != nil {
		return fmt.Errorf("fractal heap holds objects laid out for %d-byte addresses, not %d",
			fh.sizeofAddr, sizeofAddr)
	}
	fh.sizeofAddr = sizeofAddr
	fh.resetDirectBlock()
	return nil
}

// validateHeapParams checks what H5HF__hdr_create requires of the creation
// parameters (H5HFhdr.c:310-345, 416-446) and that the single root direct
// block this writer supports spans the doubling table's first row
// (H5HFdtable.c:91-94: max_index must cover start_bits + log2(width)).
func (fh *WritableFractalHeap) validateHeapParams(sb *core.Superblock) error {
	h := fh.Header
	switch {
	case h.TableWidth == 0 || h.TableWidth&(h.TableWidth-1) != 0:
		return fmt.Errorf("fractal heap width %d is not a power of two", h.TableWidth)
	case h.StartingBlockSize == 0 || h.StartingBlockSize&(h.StartingBlockSize-1) != 0:
		return fmt.Errorf("fractal heap block size %d is not a power of two", h.StartingBlockSize)
	case h.MaxDirectBlockSize < h.StartingBlockSize || h.MaxDirectBlockSize&(h.MaxDirectBlockSize-1) != 0:
		return fmt.Errorf("fractal heap max direct block size %d invalid for block size %d",
			h.MaxDirectBlockSize, h.StartingBlockSize)
	case h.MaxDirectBlockSize < uint64(h.MaxManagedObjectSize):
		return fmt.Errorf("fractal heap max direct block size %d below max managed size %d",
			h.MaxDirectBlockSize, h.MaxManagedObjectSize)
	case int(h.MaxHeapSize) < log2Floor(h.StartingBlockSize)+log2Floor(uint64(h.TableWidth)):
		return fmt.Errorf("fractal heap address space of %d bits cannot hold a first row of %d-byte blocks",
			h.MaxHeapSize, h.StartingBlockSize)
	case int(h.MaxHeapSize) > 8*int(sb.LengthSize):
		return fmt.Errorf("fractal heap address space of %d bits too large for %d-byte lengths",
			h.MaxHeapSize, sb.LengthSize)
	case int(h.HeapIDLength) < 1+int(h.HeapOffsetSize)+int(h.HeapLengthSize):
		return fmt.Errorf("fractal heap ID of %d bytes cannot hold a %d-byte offset and a %d-byte length",
			h.HeapIDLength, h.HeapOffsetSize, h.HeapLengthSize)
	case sb.OffsetSize != fh.sizeofAddr:
		return fmt.Errorf("fractal heap laid out for %d-byte addresses, file uses %d",
			fh.sizeofAddr, sb.OffsetSize)
	}
	return nil
}

// transitionToIndirectRoot transitions the heap from direct block root to indirect block root.
//
// This is called when the single direct block becomes full and we need to add more blocks.
// The process:
// 1. Create a root indirect block (1 row initially, will grow)
// 2. Move existing direct block to first child position
// 3. Update heap header to point to indirect block as root
// 4. Prepare for allocating additional child blocks
//
// Reference: H5HFiblock.c - H5HF__man_iblock_create().
//
// MVP Limitation: Single-level indirect only (max_direct_rows = nrows).
func (fh *WritableFractalHeap) transitionToIndirectRoot() error {
	if fh.RootIndirectBlock != nil {
		return fmt.Errorf("heap already has indirect root")
	}

	if fh.DirectBlock == nil {
		return fmt.Errorf("no direct block to transition")
	}

	// Calculate indirect block parameters
	// Start with 1 row (holds 2 direct blocks with table width=2)
	numRows := uint16(1)
	maxDirectRows := numRows // All rows are direct blocks (MVP: no multiply-indirect)

	// Create root indirect block
	// Block offset = 0 (root starts at beginning of heap address space)
	indirectBlock := NewWritableIndirectBlock(
		0, // Heap header address will be set during write
		0, // Block offset = 0 for root
		numRows,
		fh.Header.TableWidth,
		maxDirectRows,
	)

	// Move existing direct block to first child position (row 0, col 0)
	// The existing block already has offset 0, which is correct
	entryIndex := indirectBlock.CalculateEntryIndex(0, 0)
	// Address will be set when blocks are written to file
	if err := indirectBlock.SetChildAddress(entryIndex, 0); err != nil {
		return fmt.Errorf("failed to set first child address: %w", err)
	}

	// Add existing direct block to DirectBlocks map (key = block offset)
	fh.DirectBlocks[fh.DirectBlock.BlockOffset] = fh.DirectBlock

	// Update heap structure
	fh.RootIndirectBlock = indirectBlock

	// Clear DirectBlock pointer (it's now in DirectBlocks map)
	// Keep reference until write, then nil it
	// fh.DirectBlock = nil  // Don't nil yet - needed for backward compat

	// Update heap header
	fh.Header.CurrentNumRows = numRows

	return nil
}

// needsTransition checks if heap should transition from direct to indirect root.
//
// Transition is needed when:
// - Currently using direct block root (no indirect block yet)
// - Direct block is full or nearly full
// - New object doesn't fit in remaining space.
func (fh *WritableFractalHeap) needsTransition(objectSize uint64) bool {
	// Already using indirect block
	if fh.RootIndirectBlock != nil {
		return false
	}

	// Check if object fits in current direct block
	if fh.DirectBlock.FreeOffset+objectSize <= fh.DirectBlock.Size {
		return false // Still fits
	}

	// Object doesn't fit - need transition
	return true
}

// InsertObject inserts a variable-length object into the fractal heap.
//
// This method handles both direct block root (small heaps) and indirect block root (large heaps).
// When the heap grows beyond a single direct block, it automatically transitions to indirect structure.
//
// Parameters:
// - data: object data (e.g., link name as []byte)
//
// Returns:
// - []byte: heap ID (8-byte ID for managed objects)
// - error: if heap full or data too large
//
// Reference: H5HF.c - H5HF_insert().
func (fh *WritableFractalHeap) InsertObject(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrEmptyObject
	}
	if !fh.adopted {
		return nil, errors.New("fractal heap loaded from file: AdoptObjects must name its live objects before an insert")
	}

	dataSize := uint64(len(data))

	// Check if object fits in managed object size
	if dataSize > uint64(fh.Header.MaxManagedObjectSize) {
		return nil, fmt.Errorf("%w: object size %d exceeds max %d",
			ErrObjectTooLarge, dataSize, fh.Header.MaxManagedObjectSize)
	}

	// Check if transition to indirect root is needed
	if fh.needsTransition(dataSize) {
		if err := fh.transitionToIndirectRoot(); err != nil {
			return nil, fmt.Errorf("failed to transition to indirect root: %w", err)
		}
	}

	// Route to appropriate insertion method
	if fh.RootIndirectBlock != nil {
		// Indirect root path (large heaps with multiple blocks)
		return fh.insertViaIndirect(data)
	}

	// Direct root path (small heaps with single block)
	return fh.insertViaDirect(data)
}

// insertViaDirect inserts object into direct block root (small heaps).
//
// This is the original insertion logic for single-block heaps.
//
// Reference: H5HF.c - H5HF__man_insert().
func (fh *WritableFractalHeap) insertViaDirect(data []byte) ([]byte, error) {
	dataSize := uint64(len(data))

	// Check if enough space in direct block
	if fh.DirectBlock.FreeOffset+dataSize > fh.DirectBlock.Size {
		return nil, fmt.Errorf("%w: need %d bytes, have %d free",
			ErrHeapFull, dataSize, fh.DirectBlock.Size-fh.DirectBlock.FreeOffset)
	}

	// The free space starts at FreeOffset, an offset from the start of the
	// block (prefix included), which is the object's heap offset in the root
	// block (block offset 0; H5HFman.c:386-399).
	objectOffset := fh.DirectBlock.FreeOffset

	// Objects is the block image up to its used end (a loaded heap holds the
	// whole block); extend it for a new heap.
	//nolint:gosec // G115: safe conversion, offset checked against block size above
	neededLen := int(objectOffset + dataSize)
	if neededLen > len(fh.DirectBlock.Objects) {
		fh.DirectBlock.Objects = append(fh.DirectBlock.Objects, make([]byte, neededLen-len(fh.DirectBlock.Objects))...)
	}
	copy(fh.DirectBlock.Objects[objectOffset:], data)

	// The C iterator offset (man_iter_off) tracks blocks, not objects: a
	// root direct block leaves it 0 (H5HFdblock.c:347-366), so only the free
	// position moves.
	fh.DirectBlock.FreeOffset += dataSize

	// Update statistics
	fh.Header.NumManagedObjects++
	fh.Header.FreeSpace -= dataSize

	// Create heap ID for managed object
	// Format: [flags | offset | length]
	heapID := fh.encodeHeapID(objectOffset, dataSize)

	return heapID, nil
}

// insertViaIndirect inserts object into indirect block structure (large heaps).
//
// This method navigates the doubling table to find an appropriate block,
// allocates new child blocks as needed, and inserts the object.
//
// MVP Implementation:
// - Single-level indirect blocks only
// - Simple allocation strategy (fill first block, then second, etc.)
// - No block splitting or compaction
//
// Reference: H5HFiblock.c - H5HF__man_iblock_alloc_row().
func (fh *WritableFractalHeap) insertViaIndirect(data []byte) ([]byte, error) {
	dataSize := uint64(len(data))

	// Find a block with enough free space
	// MVP: Simple linear search through child blocks
	var targetBlock *WritableDirectBlock
	var targetOffset uint64

	// Try each existing child block
	for offset, block := range fh.DirectBlocks {
		if block.FreeOffset+dataSize <= block.Size {
			targetBlock = block
			targetOffset = offset
			break
		}
	}

	// If no block found, allocate a new child block
	if targetBlock == nil {
		// Calculate next block offset
		// MVP: Use ManagedSpaceOffset (grows with heap)
		nextBlockOffset := fh.Header.ManagedSpaceSize

		// Create new child direct block; as the root block, its image starts
		// with the prefix and its objects follow it.
		ovh := fh.directBlockOverhead()
		newBlock := &WritableDirectBlock{
			Version:           0,
			HeapHeaderAddress: 0, // Will be set during write
			BlockOffset:       nextBlockOffset,
			Size:              fh.MaxDirectBlockSize, // Use max block size
			Objects:           make([]byte, ovh, fh.MaxDirectBlockSize),
			FreeOffset:        ovh,
			ChecksumEnabled:   fh.DirectBlock.ChecksumEnabled,
		}

		// Add to DirectBlocks map
		fh.DirectBlocks[nextBlockOffset] = newBlock

		// Update heap statistics
		fh.Header.ManagedSpaceSize += fh.MaxDirectBlockSize
		fh.Header.AllocatedManagedSpace += fh.MaxDirectBlockSize
		fh.Header.FreeSpace += fh.MaxDirectBlockSize - ovh

		// Find entry in indirect block for this new child
		// MVP: Simple append to next available entry
		numExistingBlocks := len(fh.DirectBlocks) - 1 // -1 because we just added one
		if numExistingBlocks >= len(fh.RootIndirectBlock.ChildAddresses) {
			// Need to grow indirect block (add more rows)
			// MVP: For now, return error (will implement growth in future)
			return nil, fmt.Errorf("%w: indirect block full (need to grow)", ErrHeapFull)
		}

		// Set child address in indirect block (address will be set during write)
		if err := fh.RootIndirectBlock.SetChildAddress(numExistingBlocks, 0); err != nil {
			return nil, fmt.Errorf("failed to set child address: %w", err)
		}

		targetBlock = newBlock
		targetOffset = nextBlockOffset
	}

	// Insert object into target block
	objectOffset := targetBlock.FreeOffset

	// Write object at free offset position
	//nolint:gosec // G115: safe conversion, offset checked against block size above
	neededLen := int(objectOffset + dataSize)
	if neededLen > len(targetBlock.Objects) {
		// Extend slice if needed
		targetBlock.Objects = append(targetBlock.Objects, make([]byte, neededLen-len(targetBlock.Objects))...)
	}
	copy(targetBlock.Objects[objectOffset:], data)

	// Update block offsets
	targetBlock.FreeOffset += dataSize

	// Update heap statistics
	fh.Header.NumManagedObjects++
	fh.Header.FreeSpace -= dataSize

	// Create heap ID
	// Offset is: block offset + object offset within block
	globalOffset := targetOffset + objectOffset
	heapID := fh.encodeHeapID(globalOffset, dataSize)

	return heapID, nil
}

// encodeHeapID creates a heap ID for a managed object.
//
// Format:
// - Byte 0: Flags (version and type)
// - Bytes 1-N: Variable-length encoded offset
// - Bytes N+1-M: Variable-length encoded length
//
// Reference: H5HFpkg.h - H5HF_MAN_ID_ENCODE macro.
func (fh *WritableFractalHeap) encodeHeapID(offset, length uint64) []byte {
	// Always use the configured heap ID length (typically 8 bytes)
	// This matches what HDF5 expects - fixed size heap IDs
	heapID := make([]byte, fh.Header.HeapIDLength)

	// Flags byte: version (bits 6-7) = 0, type (bits 4-5) = 0 (managed)
	heapID[0] = 0x00 // Version 0, Type managed

	// Encode offset (little-endian for MVP)
	idx := 1
	writeUintVar(heapID[idx:], offset, int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	idx += int(fh.Header.HeapOffsetSize)

	// Encode length (little-endian for MVP)
	writeUintVar(heapID[idx:], length, int(fh.Header.HeapLengthSize), binary.LittleEndian)
	// Remaining bytes stay zero-padded

	return heapID
}

// WriteToFile writes fractal heap (header + direct block) to file.
//
// Returns:
// - heapHeaderAddr: address of heap header
// - error: if write fails
//
// Reference: H5HFhdr.c - H5HF__hdr_serialize()
//
//	H5HFdblock.c - H5HF__dblock_serialize()
func (fh *WritableFractalHeap) WriteToFile(writer Writer, allocator Allocator, sb *core.Superblock) (uint64, error) {
	if err := fh.checkWritable(sb); err != nil {
		return 0, err
	}

	// 1. Allocate addresses first (but don't write yet)
	// Calculate sizes
	headerSize := 22 + 12*int(sb.LengthSize) + 3*int(sb.OffsetSize) + 4 // +4 for checksum

	// Direct block is allocated at FULL block size, not just used portion
	// This matches HDF5 C library behavior
	directBlockSize := fh.DirectBlock.Size

	// Allocate both addresses
	headerAddr, err := allocator.Allocate(uint64(headerSize))
	if err != nil {
		return 0, fmt.Errorf("failed to allocate heap header: %w", err)
	}

	directBlockAddr, err := allocator.Allocate(directBlockSize)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate direct block: %w", err)
	}

	// 2. Update cross-references
	fh.Header.RootBlockAddress = directBlockAddr
	fh.DirectBlock.HeapHeaderAddress = headerAddr

	// 3. Write both with correct cross-references
	err = fh.writeHeaderAt(writer, headerAddr, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to write heap header: %w", err)
	}

	err = fh.writeDirectBlockAt(writer, directBlockAddr, sb)
	if err != nil {
		return 0, fmt.Errorf("failed to write direct block: %w", err)
	}

	return headerAddr, nil
}

// WriteAt writes fractal heap in-place at previously loaded addresses.
//
// This method is used for Read-Modify-Write (RMW) scenarios:
// - Heap was loaded via LoadFromFile()
// - New objects were inserted
// - Write back to same addresses
//
// Parameters:
//   - writer: File writer (must implement Writer interface)
//   - sb: Superblock for field sizes
//
// Returns:
//   - error: if write fails or heap was not loaded from file
//
// Reference: Same as WriteToFile, but uses stored addresses.
func (fh *WritableFractalHeap) WriteAt(writer Writer, sb *core.Superblock) error {
	// Verify this heap was loaded from file
	if fh.loadedHeaderAddress == 0 {
		return errors.New("cannot use WriteAt: heap not loaded from file (use WriteToFile for new heaps)")
	}
	if !fh.adopted {
		return errors.New("fractal heap loaded from file: AdoptObjects must name its live objects before a write")
	}
	if err := fh.checkWritable(sb); err != nil {
		return err
	}

	// Update cross-references (in case they were cleared)
	fh.Header.RootBlockAddress = fh.loadedDirectBlockAddress
	fh.DirectBlock.HeapHeaderAddress = fh.loadedHeaderAddress

	// Write header at loaded address
	err := fh.writeHeaderAt(writer, fh.loadedHeaderAddress, sb)
	if err != nil {
		return fmt.Errorf("failed to write heap header at 0x%X: %w", fh.loadedHeaderAddress, err)
	}

	// Write direct block at loaded address
	err = fh.writeDirectBlockAt(writer, fh.loadedDirectBlockAddress, sb)
	if err != nil {
		return fmt.Errorf("failed to write direct block at 0x%X: %w", fh.loadedDirectBlockAddress, err)
	}

	return nil
}

// checkWritable refuses what WriteToFile and WriteAt cannot serialize as the
// C library would read it: an indirect root (only the root direct block is
// written; the child blocks of the in-memory doubling table do not follow
// H5HFdblock.c's offsets) and parameters H5HF__hdr_create rejects.
func (fh *WritableFractalHeap) checkWritable(sb *core.Superblock) error {
	if fh.RootIndirectBlock != nil {
		return fmt.Errorf("%w: writing a fractal heap whose root is an indirect block", utils.ErrNotSupported)
	}
	return fh.validateHeapParams(sb)
}

// writeHeaderAt serializes and writes heap header at the given address.
//
// Reference: H5HFcache.c - H5HF__cache_hdr_serialize().
func (fh *WritableFractalHeap) writeHeaderAt(writer Writer, addr uint64, sb *core.Superblock) error {
	// Calculate header size
	// Fixed: 4 (sig) + 1 (ver) + 2 (heap ID len) + 2 (filter len) + 1 (flags) + 4 (max obj size) = 14
	// Variable huge: lengthSize + offsetSize (next ID + btree addr)
	// Variable free: lengthSize + offsetSize (amount + section addr)
	// Variable stats: 4*lengthSize (managed) + 2*lengthSize (huge) + 2*lengthSize (tiny) = 8*lengthSize
	// Doubling table: 2 (width) + 2*lengthSize (start/max size) + 2 (max heap) + 2 (start rows) + offsetSize (root) + 2 (curr rows)
	// Total: 14 + lengthSize + offsetSize + lengthSize + offsetSize + 8*lengthSize + 8 + 2*lengthSize + offsetSize
	//      = 22 + 12*lengthSize + 3*offsetSize
	size := 22 + 12*int(sb.LengthSize) + 3*int(sb.OffsetSize) + 4 // +4 for checksum

	buf := make([]byte, size)
	offset := 0

	// Signature
	copy(buf[offset:], HeapSignature)
	offset += 4

	// Version
	buf[offset] = fh.Header.Version
	offset++

	// Heap ID Length
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.HeapIDLength)
	offset += 2

	// I/O Filters Length
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.IOFiltersLength)
	offset += 2

	// Flags
	buf[offset] = fh.Header.Flags
	offset++

	// Max Managed Object Size
	binary.LittleEndian.PutUint32(buf[offset:], fh.Header.MaxManagedObjectSize)
	offset += 4

	// Next Huge Object ID
	writeUintVar(buf[offset:], fh.Header.NextHugeObjectID, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Huge Object B-tree Address
	writeUintVar(buf[offset:], fh.Header.HugeObjectBTreeAddr, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Free Space Amount
	writeUintVar(buf[offset:], fh.Header.FreeSpace, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Free Section Address
	writeUintVar(buf[offset:], fh.Header.FreeSectionAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Managed Space Statistics (4 fields)
	writeUintVar(buf[offset:], fh.Header.ManagedSpaceSize, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	writeUintVar(buf[offset:], fh.Header.AllocatedManagedSpace, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	writeUintVar(buf[offset:], fh.Header.ManagedSpaceOffset, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	writeUintVar(buf[offset:], fh.Header.NumManagedObjects, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Huge Object Statistics (2 fields)
	writeUintVar(buf[offset:], fh.Header.SizeHugeObjects, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	writeUintVar(buf[offset:], fh.Header.NumHugeObjects, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Tiny Object Statistics (2 fields)
	writeUintVar(buf[offset:], fh.Header.SizeTinyObjects, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	writeUintVar(buf[offset:], fh.Header.NumTinyObjects, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Doubling Table Parameters
	// Table Width
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.TableWidth)
	offset += 2

	// Starting Block Size
	writeUintVar(buf[offset:], fh.Header.StartingBlockSize, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Max Direct Block Size
	writeUintVar(buf[offset:], fh.Header.MaxDirectBlockSize, int(sb.LengthSize), sb.Endianness)
	offset += int(sb.LengthSize)

	// Max Heap Size (log2 bits)
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.MaxHeapSize)
	offset += 2

	// Starting Number of Rows
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.StartingNumRows)
	offset += 2

	// Root Block Address
	writeUintVar(buf[offset:], fh.Header.RootBlockAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Current Number of Rows
	binary.LittleEndian.PutUint16(buf[offset:], fh.Header.CurrentNumRows)
	offset += 2

	// Checksum (CRC32 of header without checksum field)
	checksum := core.JenkinsChecksum(buf[:offset])
	binary.LittleEndian.PutUint32(buf[offset:], checksum)

	// Write to file at pre-allocated address
	return writer.WriteAtAddress(buf, addr)
}

// writeDirectBlockAt serializes and writes direct block at the given address.
//
// NOTE: HDF5 direct blocks are written at FULL block size, not just used portion.
// This allows readers to know the block size without additional metadata.
//
// Reference: H5HFcache.c - H5HF__cache_dblock_pre_serialize().
func (fh *WritableFractalHeap) writeDirectBlockAt(writer Writer, addr uint64, sb *core.Superblock) error {
	// Total size is FULL block size (not just used portion!)
	// This matches HDF5 C library behavior - blocks are fixed size.
	//
	// In the read-modify-write path Size is the header's StartingBlockSize and
	// LoadFromFile has already bounded it through the block funnel; a fresh heap
	// takes it from the caller of NewWritableFractalHeap. SafeInt keeps the
	// conversion sound for both: a size that does not fit in int could never
	// size a Go slice anyway.
	totalSize, err := utils.SafeInt(fh.DirectBlock.Size, "fractal heap direct block")
	if err != nil {
		return err
	}

	if uint64(len(fh.DirectBlock.Objects)) > fh.DirectBlock.Size {
		return fmt.Errorf("fractal heap direct block image of %d bytes exceeds its size %d",
			len(fh.DirectBlock.Objects), fh.DirectBlock.Size)
	}

	// Objects is the block image: its prefix bytes are rewritten below, the
	// objects sit at their heap offsets and the unused tail stays zero, as
	// H5HF__man_dblock_create clears a new block (H5HFdblock.c:139-143).
	buf := make([]byte, totalSize)
	copy(buf, fh.DirectBlock.Objects)

	// Prefix as H5HF__cache_dblock_serialize writes it (H5HFcache.c:
	// 2030-2058): signature, version, heap header address, block offset
	// (always little-endian, UINT64ENCODE_VAR) and, when the heap
	// checksums direct blocks, the checksum of the whole block computed
	// with that field zero. Nothing trails the objects.
	offset := 0
	copy(buf[offset:], DirectBlockSignature)
	offset += 4
	buf[offset] = fh.DirectBlock.Version
	offset++
	writeUintVar(buf[offset:], fh.DirectBlock.HeapHeaderAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)
	writeUintVar(buf[offset:], fh.DirectBlock.BlockOffset, int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	offset += int(fh.Header.HeapOffsetSize)
	if fh.Header.Flags&heapFlagChecksumDirectBlocks != 0 {
		clear(buf[offset : offset+heapChecksumSize])
		binary.LittleEndian.PutUint32(buf[offset:], core.JenkinsChecksum(buf))
	}

	// Write to file at pre-allocated address
	return writer.WriteAtAddress(buf, addr)
}

// readRootDirectBlockImage reads the bytes of the root direct block of a
// loaded heap, the image the rewrite appends to. It only checks that the
// block is one of this heap with block offset 0; the core reader checks the
// block, its checksum included, when AdoptObjects reads the live objects
// through it.
//
// Reference: H5HFcache.c - H5HF__cache_dblock_deserialize() (1760-1801).
func readRootDirectBlockImage(reader io.ReaderAt, hdr *FractalHeapHeader, headerAddr uint64,
	sizeofAddr uint8) ([]byte, error) {
	// ADR-003: the size is file metadata; core.OpenFractalHeap has checked
	// that the starting block size is a power of two no larger than the
	// maximum direct block size, and ReadBlock bounds it by the file.
	buf, err := core.ReadBlock(reader, nil, hdr.RootBlockAddr, hdr.StartingBlockSize,
		hdr.MaxDirectBlockSize, "fractal heap direct block")
	if err != nil {
		return nil, fmt.Errorf("failed to read direct block: %w", err)
	}

	prefix := 4 + 1 + int(sizeofAddr) + int(hdr.HeapOffsetSize)
	if len(buf) < prefix {
		return nil, fmt.Errorf("direct block of %d bytes shorter than its %d-byte prefix: %w",
			len(buf), prefix, utils.ErrCorrupt)
	}
	if string(buf[:4]) != "FHDB" || buf[4] != 0 {
		return nil, fmt.Errorf("root direct block at 0x%X: signature %q, version %d: %w",
			hdr.RootBlockAddr, buf[:4], buf[4], utils.ErrCorrupt)
	}
	if owner := readUint(buf[5:5+int(sizeofAddr)], int(sizeofAddr), binary.LittleEndian); owner != headerAddr {
		return nil, fmt.Errorf("root direct block at 0x%X belongs to the heap at 0x%X, not 0x%X: %w",
			hdr.RootBlockAddr, owner, headerAddr, utils.ErrCorrupt)
	}
	// Block offset (UINT64DECODE_VAR: little-endian).
	off := 5 + int(sizeofAddr)
	if blockOff := readUint(buf[off:prefix], int(hdr.HeapOffsetSize), binary.LittleEndian); blockOff != 0 {
		return nil, fmt.Errorf("root direct block at 0x%X has block offset %d, not 0: %w",
			hdr.RootBlockAddr, blockOff, utils.ErrCorrupt)
	}
	return buf, nil
}

// isLegacyHeapHeader reports whether a fractal heap was written by hdf5-go
// v0.14.2 or earlier. That writer put 0 in both the huge-object B-tree
// address and the free-space manager address; the C library writes
// HADDR_UNDEF until either exists (H5HFhdr.c:357, 360), and address 0 is the
// superblock, so no C heap has both. Its object offsets start after the
// direct block prefix (signature, version, header address, block offset; no
// checksum: its heap flags were 0), not at the start of the block. The core
// reader applies the same rule (agreed with it, so no heap is read in a
// mixed way), and AdoptObjects rewrites such a heap in the C layout,
// setting both addresses to HADDR_UNDEF in the same write.
func isLegacyHeapHeader(hugeBTreeAddr, freeSectionAddr uint64) bool {
	return hugeBTreeAddr == 0 && freeSectionAddr == 0
}

// LoadFromFile loads an existing fractal heap from file for modification.
//
// This enables read-modify-write: load existing heap, add more objects, write back.
//
// Process:
// 1. Read heap header from file address
// 2. Read direct block (MVP: single block only)
// 3. Initialize writable structures with existing data
// 4. New insertions append to existing objects
//
// Parameters:
// - reader: File reader (must implement io.ReaderAt)
// - address: Heap header address in file
// - sb: Superblock for field sizes
//
// Returns:
// - error: If heap cannot be loaded or format unsupported
//
// Reference: H5HF.c - H5HF_open().
func (fh *WritableFractalHeap) LoadFromFile(reader io.ReaderAt, address uint64, sb *core.Superblock) error {
	if reader == nil {
		return errors.New("reader is nil")
	}
	if sb == nil {
		return errors.New("superblock is nil")
	}
	if address == 0 || address == ^uint64(0) {
		return fmt.Errorf("invalid heap address: 0x%X", address)
	}

	// The core reader checks the header (checksum, doubling table) and
	// reads the live objects for AdoptObjects; the fields the rewrite keeps
	// are decoded here.
	src, err := core.OpenFractalHeap(reader, sb, address)
	if err != nil {
		return fmt.Errorf("failed to open fractal heap: %w", err)
	}
	hdr, err := parseFractalHeapHeader(reader, address, sb.LengthSize, sb.OffsetSize, sb.Endianness)
	if err != nil {
		return fmt.Errorf("failed to open fractal heap: %w", err)
	}

	// Verify this is a simple heap we can modify (MVP limitations)
	if hdr.CurrentRowCount != 0 {
		return fmt.Errorf("%w: modifying a fractal heap with indirect blocks (root has %d rows)",
			utils.ErrNotSupported, hdr.CurrentRowCount)
	}
	if hdr.IOFiltersLen != 0 {
		return fmt.Errorf("%w: modifying a filtered fractal heap", utils.ErrNotSupported)
	}
	if !addrDefined(hdr.RootBlockAddr, sb.OffsetSize) {
		return fmt.Errorf("%w: modifying a fractal heap without a root block", utils.ErrNotSupported)
	}

	img, err := readRootDirectBlockImage(reader, hdr, address, sb.OffsetSize)
	if err != nil {
		return err
	}

	// Store loaded addresses for WriteAt() support (RMW)
	fh.loadedHeaderAddress = address
	fh.loadedDirectBlockAddress = hdr.RootBlockAddr

	// Convert read-only header to writable header
	fh.Header = &WritableHeapHeader{
		Version:         hdr.Version,
		HeapIDLength:    hdr.HeapIDLen,
		IOFiltersLength: hdr.IOFiltersLen,
		Flags:           hdr.Flags,

		MaxManagedObjectSize: hdr.MaxManagedObjSize,

		NextHugeObjectID:    hdr.NextHugeObjID,
		HugeObjectBTreeAddr: hdr.HugeObjBTreeAddr,

		FreeSpace:          hdr.FreeSpaceAmount,
		FreeSectionAddress: hdr.FreeSpaceSectionAddr,

		ManagedSpaceSize:      hdr.ManagedObjSpaceSize,
		AllocatedManagedSpace: hdr.ManagedObjAllocSize,
		ManagedSpaceOffset:    hdr.ManagedObjIterOffset,
		NumManagedObjects:     hdr.ManagedObjCount,

		SizeHugeObjects: hdr.HugeObjSize,
		NumHugeObjects:  hdr.HugeObjCount,
		SizeTinyObjects: hdr.TinyObjSize,
		NumTinyObjects:  hdr.TinyObjCount,

		TableWidth:         hdr.TableWidth,
		StartingBlockSize:  hdr.StartingBlockSize,
		MaxDirectBlockSize: hdr.MaxDirectBlockSize,
		MaxHeapSize:        hdr.MaxHeapSize,
		StartingNumRows:    hdr.StartRootIndirectRows,
		RootBlockAddress:   hdr.RootBlockAddr,
		CurrentNumRows:     hdr.CurrentRowCount,

		HeapOffsetSize: hdr.HeapOffsetSize,
		HeapLengthSize: hdr.HeapLengthSize,
	}

	// The whole block image, prefix included: the heap offsets of its
	// objects count from the start of the block (H5HFman.c:386-399).
	fh.DirectBlock = &WritableDirectBlock{
		HeapHeaderAddress: address,
		Size:              hdr.StartingBlockSize,
		Objects:           img,
		ChecksumEnabled:   hdr.ChecksumDirectBlocks,
	}
	fh.sizeofAddr = sb.OffsetSize
	fh.legacy = isLegacyHeapHeader(hdr.HugeObjBTreeAddr, hdr.FreeSpaceSectionAddr)
	fh.src = src

	// The free position is not in the file: the C library keeps free space
	// in its free-space manager, and the iterator offset counts blocks, not
	// objects. AdoptObjects derives it from the live objects.
	fh.adopted = false
	return nil
}

// addrDefined reports whether a heap header address field of sizeofAddr
// bytes is defined: not HADDR_UNDEF (all bits set at that width) and not 0,
// the superblock, which a heap block never is.
func addrDefined(addr uint64, sizeofAddr uint8) bool {
	return addr != 0 && addr != heapUndefAddress>>(64-8*uint(sizeofAddr))
}

// legacyDataStart is where the objects of a legacy heap's block begin: after
// its prefix of signature, version, header address and block offset.
func (fh *WritableFractalHeap) legacyDataStart() uint64 {
	return uint64(4+1) + uint64(fh.sizeofAddr) + uint64(fh.Header.HeapOffsetSize)
}

// AdoptObjects completes LoadFromFile with the heap IDs of every live object
// of the heap, as the indexes that point into it hold them (the name index
// of dense attributes, for example). It must be called before an insert or
// a write.
//
// A heap of the C library: new objects go after the last live one; holes
// left by deleted objects are not reused. The free-space manager, if the
// file has one, is dropped (its address set to HADDR_UNDEF): its sections
// would describe space this writer then fills. The C library treats such a
// heap as one with no tracked free space (H5HFspace.c H5HF__space_start).
// The free-space total becomes the space no live object uses.
//
// A legacy heap (isLegacyHeapHeader): every live object is copied into a
// fresh block in the C layout, with the C parameters of a dense attribute
// heap, at the same addresses; the returned map takes each old heap ID
// (as a string of its bytes) to its new one, and the caller must rewrite
// every index record that holds it. For a C heap the map is nil.
func (fh *WritableFractalHeap) AdoptObjects(ids [][]byte) (map[string][]byte, error) {
	if fh.adopted {
		return nil, errors.New("fractal heap objects already adopted")
	}
	if fh.src == nil {
		return nil, errors.New("fractal heap not loaded from a file")
	}
	// Every live object is read through the core reader, which checks the
	// block (its checksum included) and the object's bounds; the rewrite
	// keeps nothing it could not read.
	src := fh.src
	fh.src = nil
	if fh.legacy {
		return fh.migrateLegacy(src, ids)
	}
	for _, id := range ids {
		if _, err := src.Object(id); err != nil {
			return nil, fmt.Errorf("live heap object %x: %w", id, err)
		}
	}

	end := fh.directBlockOverhead()
	var used uint64
	for _, id := range ids {
		start, length, err := fh.locate(id)
		if err != nil {
			return nil, err
		}
		end = max(end, start+length)
		used += length
	}
	if used > fh.DirectBlock.Size-fh.directBlockOverhead() {
		return nil, fmt.Errorf("live heap objects of %d bytes overfill a %d-byte direct block: %w",
			used, fh.DirectBlock.Size, utils.ErrCorrupt)
	}
	fh.DirectBlock.FreeOffset = end
	fh.Header.FreeSpace = fh.DirectBlock.Size - fh.directBlockOverhead() - used
	fh.Header.FreeSectionAddress = heapUndefAddress
	fh.adopted = true
	return nil, nil //nolint:nilnil // a C heap keeps its heap IDs: no remap
}

// migrateLegacy rewrites a legacy heap in the C layout of a dense attribute
// heap (NewAttributeFractalHeap) at its addresses and returns the heap ID
// remap. Header fields the old writer left inconsistent with its block
// (the address space and table width) take the C values, and both the
// huge-object B-tree and free-space addresses become HADDR_UNDEF in this
// same rewrite, so the heap is never read with mixed offsets.
func (fh *WritableFractalHeap) migrateLegacy(src *core.FractalHeap, ids [][]byte) (map[string][]byte, error) {
	type liveObject struct {
		id    []byte
		start uint64
		data  []byte
	}
	live := make([]liveObject, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[string(id)] {
			continue
		}
		seen[string(id)] = true
		start, _, err := fh.locate(id)
		if err != nil {
			return nil, err
		}
		data, err := src.Object(id)
		if err != nil {
			return nil, fmt.Errorf("live heap object %x: %w", id, err)
		}
		live = append(live, liveObject{id: id, start: start, data: data})
	}
	// Keep the old order in the block.
	slices.SortFunc(live, func(a, b liveObject) int { return cmp.Compare(a.start, b.start) })

	fresh := newFractalHeap(fh.DirectBlock.Size, attrHeapMaxIndex, fh.Header.HeapIDLength)
	fresh.sizeofAddr = fh.sizeofAddr
	fresh.resetDirectBlock()
	remap := make(map[string][]byte, len(live))
	for _, o := range live {
		newID, err := fresh.InsertObject(o.data)
		if err != nil {
			return nil, fmt.Errorf("migrate legacy fractal heap: %w", err)
		}
		remap[string(o.id)] = newID
	}

	fresh.loadedHeaderAddress = fh.loadedHeaderAddress
	fresh.loadedDirectBlockAddress = fh.loadedDirectBlockAddress
	fresh.DirectBlock.HeapHeaderAddress = fh.DirectBlock.HeapHeaderAddress
	fresh.Header.RootBlockAddress = fh.Header.RootBlockAddress
	*fh = *fresh
	return remap, nil
}

// locate decodes a managed heap ID of the root direct block and returns
// where its object lies in DirectBlock.Objects. A C heap's offsets count from
// the start of the block and none lies in its prefix (H5HFman.c:386-395); a
// legacy heap's offsets count from the end of its prefix.
func (fh *WritableFractalHeap) locate(heapID []byte) (start, length uint64, err error) {
	offset, length, err := fh.decodeManagedID(heapID)
	if err != nil {
		return 0, 0, err
	}
	start = offset
	if fh.legacy {
		start = fh.legacyDataStart() + offset
	} else if offset < fh.directBlockOverhead() {
		return 0, 0, fmt.Errorf("%w: heap offset %d in the direct block prefix (%d bytes)",
			ErrObjectNotFound, offset, fh.directBlockOverhead())
	}
	used := uint64(len(fh.DirectBlock.Objects))
	if start > used || length > used-start {
		return 0, 0, fmt.Errorf("%w: object at %d, %d bytes, beyond the %d used bytes of the block",
			ErrObjectNotFound, start, length, used)
	}
	return start, length, nil
}

// decodeManagedID decodes the offset and length of a managed heap ID
// (H5HF_MAN_ID_DECODE, H5HFpkg.h:156: flags, then little-endian offset and
// length fields of the heap's sizes).
func (fh *WritableFractalHeap) decodeManagedID(heapID []byte) (offset, length uint64, err error) {
	if len(heapID) < 1 {
		return 0, 0, ErrInvalidObjectID
	}
	flags := heapID[0]
	if version := (flags & 0xC0) >> 6; version != 0 {
		return 0, 0, fmt.Errorf("%w: unsupported version %d", ErrInvalidObjectID, version)
	}
	if idType := HeapIDType(flags & 0x30); idType != HeapIDTypeManaged {
		return 0, 0, fmt.Errorf("%w: only managed objects supported (type %d)", ErrInvalidObjectID, idType)
	}
	offSize, lenSize := int(fh.Header.HeapOffsetSize), int(fh.Header.HeapLengthSize)
	if len(heapID) < 1+offSize+lenSize {
		return 0, 0, fmt.Errorf("%w: %d-byte ID cannot hold a %d-byte offset and a %d-byte length",
			ErrInvalidObjectID, len(heapID), offSize, lenSize)
	}
	offset = readUint(heapID[1:1+offSize], offSize, binary.LittleEndian)
	length = readUint(heapID[1+offSize:1+offSize+lenSize], lenSize, binary.LittleEndian)
	return offset, length, nil
}

// GetObject retrieves object by ID from fractal heap (for testing).
//
// For MVP:
// - Object ID contains offset and length directly
//
// Parameters:
// - heapID: object ID (returned from InsertObject)
//
// Returns:
// - []byte: object data
// - error: if ID invalid.
func (fh *WritableFractalHeap) GetObject(heapID []byte) ([]byte, error) {
	globalOffset, length, err := fh.decodeManagedID(heapID)
	if err != nil {
		return nil, err
	}

	// Route to appropriate retrieval method based on heap structure
	if fh.RootIndirectBlock != nil {
		// Indirect root: Find correct child block
		return fh.getObjectFromIndirect(globalOffset, length)
	}

	start, length, err := fh.locate(heapID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(fh.DirectBlock.Objects[start : start+length]), nil
}

// getObjectFromIndirect retrieves object from indirect block structure.
//
// The global offset encodes both the block and the offset within that block.
// We need to find which child block contains this offset.
func (fh *WritableFractalHeap) getObjectFromIndirect(globalOffset, length uint64) ([]byte, error) {
	// Find which block contains this offset
	// MVP: Simple linear search through blocks
	for blockOffset, block := range fh.DirectBlocks {
		blockEnd := blockOffset + block.Size
		if globalOffset >= blockOffset && globalOffset < blockEnd {
			// Object is in this block
			localOffset := globalOffset - blockOffset

			// Validate offset within block
			if localOffset >= uint64(len(block.Objects)) {
				return nil, fmt.Errorf("%w: local offset %d >= used space %d in block at 0x%X",
					ErrObjectNotFound, localOffset, len(block.Objects), blockOffset)
			}

			if localOffset+length > uint64(len(block.Objects)) {
				return nil, fmt.Errorf("%w: object extends beyond used space in block at 0x%X",
					ErrObjectNotFound, blockOffset)
			}

			// Extract and return object data
			data := make([]byte, length)
			copy(data, block.Objects[localOffset:localOffset+length])
			return data, nil
		}
	}

	return nil, fmt.Errorf("%w: global offset %d not found in any block", ErrObjectNotFound, globalOffset)
}

// OverwriteObject overwrites an existing object in the heap (same size, in-place).
//
// This function is used for attribute modification when the new attribute data
// is exactly the same size as the old one. The object is modified in-place,
// which is more efficient than delete + insert.
//
// Parameters:
//   - heapID: object ID (from SearchRecord)
//   - newData: new object data (MUST be same size as old data)
//
// Returns:
//   - error: if size doesn't match, ID invalid, or overwrite fails
//
// Reference: H5HF.c - H5HF_write() (in-place modification).
//
// MVP Limitation: Only works for managed objects in direct block.
func (fh *WritableFractalHeap) OverwriteObject(heapID, newData []byte) error {
	if !fh.adopted {
		return errors.New("fractal heap loaded from file: AdoptObjects must name its live objects before a change")
	}
	offset, length, err := fh.locate(heapID)
	if err != nil {
		return err
	}

	// Verify new data is same size
	if uint64(len(newData)) != length {
		return fmt.Errorf("size mismatch: old=%d, new=%d (use delete+insert for different sizes)", length, len(newData))
	}

	// Overwrite in-place
	copy(fh.DirectBlock.Objects[offset:offset+length], newData)

	// Note: No need to update header statistics (object count, allocated space remain same)
	// This is pure in-place modification

	return nil
}

// DeleteObject marks an object as deleted in the fractal heap.
//
// This function is used for attribute modification when the new attribute data
// is a different size than the old one. The old object is marked as free space,
// and a new object is inserted with InsertObject().
//
// Parameters:
//   - heapID: object ID to delete
//
// Returns:
//   - error: if ID invalid or deletion fails
//
// Reference: H5HF.c - H5HF_remove() (mark object as free).
//
// MVP Limitation:
// - Marked as deleted, but space is NOT reclaimed (no free space manager).
// - Free space accumulates until heap compaction (future work).
// - This is acceptable for MVP (matches HDF5 C library behavior without compaction).
func (fh *WritableFractalHeap) DeleteObject(heapID []byte) error {
	if !fh.adopted {
		return errors.New("fractal heap loaded from file: AdoptObjects must name its live objects before a change")
	}
	offset, length, err := fh.locate(heapID)
	if err != nil {
		return err
	}

	// MVP: Mark space as deleted by zeroing it out
	// In full implementation, this would add to free space list
	// For now, we just zero the data (space is lost until compaction)
	for i := offset; i < offset+length; i++ {
		fh.DirectBlock.Objects[i] = 0
	}

	// Update statistics
	fh.Header.NumManagedObjects--
	fh.Header.FreeSpace += length

	// Note: AllocatedManagedSpace stays same (space allocated but not used)
	// This is correct - deleted objects increase free space but don't reduce allocated

	return nil
}

// writeUintVar writes a variable-length unsigned integer.
func writeUintVar(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = uint8(value) //nolint:gosec // G115: Explicit truncation to byte
	case 2:
		endianness.PutUint16(buf[:2], uint16(value)) //nolint:gosec // G115: Size validated by caller
	case 4:
		endianness.PutUint32(buf[:4], uint32(value)) //nolint:gosec // G115: Size validated by caller
	case 8:
		endianness.PutUint64(buf[:8], value)
	default:
		// Variable size - write as bytes
		for i := 0; i < size && i < 8; i++ {
			if endianness == binary.LittleEndian {
				buf[i] = uint8(value >> (8 * i)) //nolint:gosec // G115: Byte extraction from uint64
			} else {
				buf[size-1-i] = uint8(value >> (8 * i)) //nolint:gosec // G115: Byte extraction from uint64
			}
		}
	}
}
