// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/scigolib/hdf5/internal/core"
)

// Fractal heap write constants.
const (
	HeapSignature        = "FRHP" // Heap header signature
	DirectBlockSignature = "FHDB" // Direct block signature

	DefaultHeapIDLength         = 8      // 8-byte object IDs
	DefaultMaxManagedObjectSize = 65536  // 64KB
	DefaultStartingBlockSize    = 524288 // 512KB starting block
	DefaultTableWidth           = 2      // Width of doubling table
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

// NewWritableFractalHeap creates a new fractal heap for writing.
//
// Parameters:
// - blockSize: size of the direct block (use DefaultStartingBlockSize)
//
// Returns:
// - *WritableFractalHeap: heap structure ready for object insertion.
func NewWritableFractalHeap(blockSize uint64) *WritableFractalHeap {
	// Compute heap offset and length sizes
	// Reference: H5HFhdr.c - H5HF__hdr_finish_init_phase1()
	maxHeapSize := uint16(16)                      // 16 bits for heap size (65KB max offset)
	heapOffsetSize := uint8((maxHeapSize + 7) / 8) //nolint:gosec // G115: Division by 8, result always fits in uint8

	// Length size based on max managed object size
	maxObjSize := uint64(DefaultMaxManagedObjectSize)
	heapLengthSize := computeOffsetSize(maxObjSize) // Typically 3 bytes for 64KB

	header := &WritableHeapHeader{
		Version:         0,
		HeapIDLength:    DefaultHeapIDLength,
		IOFiltersLength: 0,
		Flags:           0, // No checksums, no huge objects for MVP

		MaxManagedObjectSize: DefaultMaxManagedObjectSize,
		NextHugeObjectID:     0,
		HugeObjectBTreeAddr:  0,

		FreeSpace:          blockSize, // Initially all free
		FreeSectionAddress: 0,         // No free space manager in MVP

		ManagedSpaceSize:      blockSize,
		AllocatedManagedSpace: blockSize,
		ManagedSpaceOffset:    0, // Next insert at offset 0
		NumManagedObjects:     0,

		SizeHugeObjects: 0,
		NumHugeObjects:  0,
		SizeTinyObjects: 0,
		NumTinyObjects:  0,

		TableWidth:         DefaultTableWidth,
		StartingBlockSize:  blockSize,
		MaxDirectBlockSize: blockSize,
		MaxHeapSize:        maxHeapSize,
		StartingNumRows:    0, // Not used in MVP
		RootBlockAddress:   0, // Will be set when written
		CurrentNumRows:     0, // 0 = direct block at root

		HeapOffsetSize: heapOffsetSize,
		HeapLengthSize: heapLengthSize,
	}

	directBlock := &WritableDirectBlock{
		Version:           0,
		HeapHeaderAddress: 0, // Will be set when written
		BlockOffset:       0, // First block at offset 0
		Size:              blockSize,
		Objects:           make([]byte, 0, blockSize),
		FreeOffset:        0,     // Next insert at 0
		ChecksumEnabled:   false, // No checksum for MVP
	}

	return &WritableFractalHeap{
		Header:             header,
		DirectBlock:        directBlock,
		RootIndirectBlock:  nil,                                   // Start with direct block root
		MaxDirectBlockSize: blockSize,                             // Transition threshold
		DirectBlocks:       make(map[uint64]*WritableDirectBlock), // Empty initially
	}
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

	// Current offset becomes the object's location
	objectOffset := fh.DirectBlock.FreeOffset

	// Write object at free offset position
	// For new heaps: Objects slice is empty or short, need to extend
	// For loaded heaps: Objects slice is pre-allocated to block size
	//nolint:gosec // G115: safe conversion, offset checked against block size above
	neededLen := int(objectOffset + dataSize)
	if neededLen > len(fh.DirectBlock.Objects) {
		// Extend slice if needed (for new heaps)
		fh.DirectBlock.Objects = append(fh.DirectBlock.Objects, make([]byte, neededLen-len(fh.DirectBlock.Objects))...)
	}
	// Write data at the correct offset
	copy(fh.DirectBlock.Objects[objectOffset:], data)

	// Update offsets
	fh.DirectBlock.FreeOffset += dataSize
	fh.Header.ManagedSpaceOffset += dataSize

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

		// Create new child direct block
		newBlock := &WritableDirectBlock{
			Version:           0,
			HeapHeaderAddress: 0, // Will be set during write
			BlockOffset:       nextBlockOffset,
			Size:              fh.MaxDirectBlockSize, // Use max block size
			Objects:           make([]byte, 0, fh.MaxDirectBlockSize),
			FreeOffset:        0,
			ChecksumEnabled:   false,
		}

		// Add to DirectBlocks map
		fh.DirectBlocks[nextBlockOffset] = newBlock

		// Update heap statistics
		fh.Header.ManagedSpaceSize += fh.MaxDirectBlockSize
		fh.Header.AllocatedManagedSpace += fh.MaxDirectBlockSize
		fh.Header.FreeSpace += fh.MaxDirectBlockSize

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
	fh.Header.ManagedSpaceOffset += dataSize // Iterator offset (total objects inserted)
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
	// 1. Allocate addresses first (but don't write yet)
	// Calculate sizes
	headerSize := 22 + 12*int(sb.LengthSize) + 3*int(sb.OffsetSize) + 4 // +4 for checksum

	// Direct block is allocated at FULL block size, not just used portion
	// This matches HDF5 C library behavior
	directBlockSize := fh.DirectBlock.Size

	// Allocate both addresses
	headerAddr, err := allocator.Allocate(uint64(headerSize)) //nolint:gosec // G115: headerSize bounded by HDF5 format
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
	checksum := crc32.ChecksumIEEE(buf[:offset])
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
	// Checksum is always at the end of the FULL block (not just used portion)
	checksumSize := 4

	// Total size is FULL block size (not just used portion!)
	// This matches HDF5 C library behavior - blocks are fixed size
	//nolint:gosec // G115: block size from header, max ~2GB
	totalSize := int(fh.DirectBlock.Size)

	buf := make([]byte, totalSize)
	offset := 0

	// Signature
	copy(buf[offset:], DirectBlockSignature)
	offset += 4

	// Version
	buf[offset] = fh.DirectBlock.Version
	offset++

	// Heap Header Address
	writeUintVar(buf[offset:], fh.DirectBlock.HeapHeaderAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Block Offset (variable-sized based on heap offset size)
	writeUintVar(buf[offset:], fh.DirectBlock.BlockOffset, int(fh.Header.HeapOffsetSize), sb.Endianness)
	offset += int(fh.Header.HeapOffsetSize)

	// Object data (used portion) - rest is padding
	copy(buf[offset:], fh.DirectBlock.Objects)

	// Checksum at END of block (last 4 bytes)
	checksumOffset := totalSize - checksumSize
	checksum := crc32.ChecksumIEEE(buf[:checksumOffset])
	binary.LittleEndian.PutUint32(buf[checksumOffset:], checksum)

	// Write to file at pre-allocated address
	return writer.WriteAtAddress(buf, addr)
}

// readDirectBlockFromFile reads a direct block from file.
// This is a helper for LoadFromFile that reads the direct block structure.
//
// Reference: H5HFdblock.c - H5HF__cache_dblock_deserialize().
func (fh *WritableFractalHeap) readDirectBlockFromFile(reader io.ReaderAt, address, blockSize uint64,
	heapOffsetSize, fileOffsetSize uint8, endianness binary.ByteOrder, headerAddr uint64) (*DirectBlock, error) {
	if address == 0 || address == ^uint64(0) {
		return nil, fmt.Errorf("invalid direct block address: 0x%X", address)
	}

	// Read entire block (header + data)
	//nolint:gosec // G115: safe conversion, blockSize from HDF5 header (max ~2GB per block)
	totalSize := int(blockSize)
	buf := make([]byte, totalSize)
	//nolint:gosec // G115: uint64 to int64 conversion safe for file offsets
	if _, err := reader.ReadAt(buf, int64(address)); err != nil {
		return nil, fmt.Errorf("failed to read direct block: %w", err)
	}

	dblock := &DirectBlock{}
	offset := 0

	// Signature (4 bytes) - "FHDB"
	copy(dblock.Signature[:], buf[offset:offset+4])
	if string(dblock.Signature[:]) != "FHDB" {
		return nil, fmt.Errorf("invalid direct block signature: %q (expected FHDB)", dblock.Signature)
	}
	offset += 4

	// Version (1 byte)
	dblock.Version = buf[offset]
	if dblock.Version != 0 {
		return nil, fmt.Errorf("unsupported direct block version: %d", dblock.Version)
	}
	offset++

	// Heap Header Address (sizeof_addr bytes)
	dblock.HeapHeaderAddr = readUint(buf[offset:offset+int(fileOffsetSize)], int(fileOffsetSize), endianness)
	offset += int(fileOffsetSize)

	// Verify heap header address matches
	if dblock.HeapHeaderAddr != headerAddr {
		return nil, fmt.Errorf("direct block heap header address mismatch: 0x%X (expected 0x%X)",
			dblock.HeapHeaderAddr, headerAddr)
	}

	// Block Offset (heap_off_size bytes)
	dblock.BlockOffset = readUint(buf[offset:offset+int(heapOffsetSize)], int(heapOffsetSize), endianness)
	offset += int(heapOffsetSize)

	// Data (remaining bytes, excluding checksum if present)
	// For simplicity, we assume checksum is always present for now
	dataEnd := totalSize - 4 // Exclude checksum
	dblock.Data = make([]byte, dataEnd-offset)
	copy(dblock.Data, buf[offset:dataEnd])

	// Checksum at end (not validated in this MVP)
	dblock.Checksum = endianness.Uint32(buf[totalSize-4 : totalSize])

	return dblock, nil
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

	// Use the read-only fractal heap parser
	readHeap, err := OpenFractalHeap(reader, address, sb.LengthSize, sb.OffsetSize, sb.Endianness)
	if err != nil {
		return fmt.Errorf("failed to open fractal heap: %w", err)
	}

	// Verify this is a simple heap we can modify (MVP limitations)
	if readHeap.Header.CurrentRowCount != 0 {
		return fmt.Errorf("cannot modify heap with indirect blocks (root has %d rows)", readHeap.Header.CurrentRowCount)
	}

	// Read the direct block manually (readDirectBlock is private)
	dblock, err := fh.readDirectBlockFromFile(reader, readHeap.Header.RootBlockAddr, readHeap.Header.StartingBlockSize,
		readHeap.Header.HeapOffsetSize, sb.OffsetSize, sb.Endianness, readHeap.headerAddr)
	if err != nil {
		return fmt.Errorf("failed to read direct block: %w", err)
	}

	// Store loaded addresses for WriteAt() support (RMW)
	fh.loadedHeaderAddress = address
	fh.loadedDirectBlockAddress = readHeap.Header.RootBlockAddr

	// Convert read-only header to writable header
	fh.Header = &WritableHeapHeader{
		Version:         readHeap.Header.Version,
		HeapIDLength:    readHeap.Header.HeapIDLen,
		IOFiltersLength: readHeap.Header.IOFiltersLen,
		Flags:           readHeap.Header.Flags,

		MaxManagedObjectSize: readHeap.Header.MaxManagedObjSize,

		NextHugeObjectID:    readHeap.Header.NextHugeObjID,
		HugeObjectBTreeAddr: readHeap.Header.HugeObjBTreeAddr,

		FreeSpace:          readHeap.Header.FreeSpaceAmount,
		FreeSectionAddress: readHeap.Header.FreeSpaceSectionAddr,

		ManagedSpaceSize:      readHeap.Header.ManagedObjSpaceSize,
		AllocatedManagedSpace: readHeap.Header.ManagedObjAllocSize,
		ManagedSpaceOffset:    readHeap.Header.ManagedObjIterOffset,
		NumManagedObjects:     readHeap.Header.ManagedObjCount,

		SizeHugeObjects: readHeap.Header.HugeObjSize,
		NumHugeObjects:  readHeap.Header.HugeObjCount,
		SizeTinyObjects: readHeap.Header.TinyObjSize,
		NumTinyObjects:  readHeap.Header.TinyObjCount,

		TableWidth:         readHeap.Header.TableWidth,
		StartingBlockSize:  readHeap.Header.StartingBlockSize,
		MaxDirectBlockSize: readHeap.Header.MaxDirectBlockSize,
		MaxHeapSize:        readHeap.Header.MaxHeapSize,
		StartingNumRows:    readHeap.Header.StartRootIndirectRows,
		RootBlockAddress:   readHeap.Header.RootBlockAddr,
		CurrentNumRows:     readHeap.Header.CurrentRowCount,

		HeapOffsetSize: readHeap.Header.HeapOffsetSize,
		HeapLengthSize: readHeap.Header.HeapLengthSize,
	}

	// Convert direct block to writable format
	fh.DirectBlock = &WritableDirectBlock{
		Version:           dblock.Version,
		HeapHeaderAddress: dblock.HeapHeaderAddr,
		BlockOffset:       dblock.BlockOffset,
		Size:              readHeap.Header.StartingBlockSize,
		Objects:           make([]byte, len(dblock.Data)),
		FreeOffset:        readHeap.Header.ManagedObjIterOffset, // Next insert position
		ChecksumEnabled:   readHeap.Header.ChecksumDirectBlocks,
	}

	// Copy existing object data
	copy(fh.DirectBlock.Objects, dblock.Data)

	return nil
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
	// Parse heap ID
	if len(heapID) < 1 {
		return nil, ErrInvalidObjectID
	}

	// Check flags byte
	flags := heapID[0]
	version := (flags & 0xC0) >> 6
	idType := HeapIDType(flags & 0x30)

	if version != 0 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidObjectID, version)
	}

	if idType != HeapIDTypeManaged {
		return nil, fmt.Errorf("%w: only managed objects supported (type %d)", ErrInvalidObjectID, idType)
	}

	// Heap IDs are fixed size (typically 8 bytes)
	if len(heapID) != int(fh.Header.HeapIDLength) {
		return nil, fmt.Errorf("%w: wrong size %d (expected %d)", ErrInvalidObjectID, len(heapID), fh.Header.HeapIDLength)
	}

	idx := 1
	globalOffset := readUint(heapID[idx:idx+int(fh.Header.HeapOffsetSize)], int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	idx += int(fh.Header.HeapOffsetSize)

	length := readUint(heapID[idx:idx+int(fh.Header.HeapLengthSize)], int(fh.Header.HeapLengthSize), binary.LittleEndian)

	// Route to appropriate retrieval method based on heap structure
	if fh.RootIndirectBlock != nil {
		// Indirect root: Find correct child block
		return fh.getObjectFromIndirect(globalOffset, length)
	}

	// Direct root: Simple lookup
	return fh.getObjectFromDirect(globalOffset, length)
}

// getObjectFromDirect retrieves object from direct block root.
func (fh *WritableFractalHeap) getObjectFromDirect(offset, length uint64) ([]byte, error) {
	// Validate offset and length
	if offset >= uint64(len(fh.DirectBlock.Objects)) {
		return nil, fmt.Errorf("%w: offset %d >= used space %d", ErrObjectNotFound, offset, len(fh.DirectBlock.Objects))
	}

	if offset+length > uint64(len(fh.DirectBlock.Objects)) {
		return nil, fmt.Errorf("%w: object extends beyond used space", ErrObjectNotFound)
	}

	// Extract and return object data
	data := make([]byte, length)
	copy(data, fh.DirectBlock.Objects[offset:offset+length])

	return data, nil
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
	// Parse heap ID to get offset and length
	if len(heapID) < 1 {
		return ErrInvalidObjectID
	}

	flags := heapID[0]
	version := (flags & 0xC0) >> 6
	idType := HeapIDType(flags & 0x30)

	if version != 0 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidObjectID, version)
	}

	if idType != HeapIDTypeManaged {
		return fmt.Errorf("%w: only managed objects supported", ErrInvalidObjectID)
	}

	if len(heapID) != int(fh.Header.HeapIDLength) {
		return fmt.Errorf("%w: wrong size", ErrInvalidObjectID)
	}

	idx := 1
	offset := readUint(heapID[idx:idx+int(fh.Header.HeapOffsetSize)], int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	idx += int(fh.Header.HeapOffsetSize)

	length := readUint(heapID[idx:idx+int(fh.Header.HeapLengthSize)], int(fh.Header.HeapLengthSize), binary.LittleEndian)

	// Verify new data is same size
	if uint64(len(newData)) != length {
		return fmt.Errorf("size mismatch: old=%d, new=%d (use delete+insert for different sizes)", length, len(newData))
	}

	// Validate offset
	if offset >= uint64(len(fh.DirectBlock.Objects)) {
		return fmt.Errorf("%w: offset %d >= used space %d", ErrObjectNotFound, offset, len(fh.DirectBlock.Objects))
	}

	if offset+length > uint64(len(fh.DirectBlock.Objects)) {
		return fmt.Errorf("%w: object extends beyond used space", ErrObjectNotFound)
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
	// Parse heap ID to get offset and length
	if len(heapID) < 1 {
		return ErrInvalidObjectID
	}

	flags := heapID[0]
	version := (flags & 0xC0) >> 6
	idType := HeapIDType(flags & 0x30)

	if version != 0 {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidObjectID, version)
	}

	if idType != HeapIDTypeManaged {
		return fmt.Errorf("%w: only managed objects supported", ErrInvalidObjectID)
	}

	if len(heapID) != int(fh.Header.HeapIDLength) {
		return fmt.Errorf("%w: wrong size", ErrInvalidObjectID)
	}

	idx := 1
	offset := readUint(heapID[idx:idx+int(fh.Header.HeapOffsetSize)], int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	idx += int(fh.Header.HeapOffsetSize)

	length := readUint(heapID[idx:idx+int(fh.Header.HeapLengthSize)], int(fh.Header.HeapLengthSize), binary.LittleEndian)

	// Validate offset
	if offset >= uint64(len(fh.DirectBlock.Objects)) {
		return fmt.Errorf("%w: offset %d >= used space", ErrObjectNotFound, offset)
	}

	if offset+length > uint64(len(fh.DirectBlock.Objects)) {
		return fmt.Errorf("%w: object extends beyond used space", ErrObjectNotFound)
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
