// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/scigolib/hdf5/internal/core"
)

// IndirectBlock represents a fractal heap indirect block (read-only).
//
// An indirect block is a node in the fractal heap's doubling table structure that
// contains addresses of child blocks (either direct blocks or other indirect blocks).
// This enables the heap to grow beyond a single direct block and support large objects.
//
// Reference: H5HFiblock.c, H5HFpkg.h (struct H5HF_indirect_t)
// Format Spec: https://docs.hdfgroup.org/hdf5/latest/_f_m_t3.html#FractalHeap
//
// On-Disk Format:
//   - Signature: "FHIB" (4 bytes)
//   - Version: 0 (1 byte)
//   - Heap Header Address (sizeof_addr bytes)
//   - Block Offset (heap_off_size bytes) - offset within heap's address space
//   - Child Block Addresses (K×M entries, sizeof_addr each)
//   - K = number of rows (nrows)
//   - M = table width (from heap header, typically 2)
//   - First entries point to direct blocks, later to indirect blocks
//   - Checksum (4 bytes, optional)
//
// Doubling Table Structure:
//
//	Each row contains blocks of size = starting_size × 2^row
//	Rows 0..N-1: Direct blocks (size <= max_direct_block_size)
//	Rows N..K-1: Indirect blocks (point to child indirect blocks)
type IndirectBlock struct {
	Header  IndirectBlockHeader
	Entries []uint64 // Child block addresses (direct or indirect)
}

// IndirectBlockHeader represents the metadata portion of an indirect block.
type IndirectBlockHeader struct {
	Signature      [4]byte // "FHIB"
	Version        uint8   // Currently 0
	HeapHeaderAddr uint64  // Address of heap header
	BlockOffset    uint64  // Offset within heap's address space
	NumRows        uint16  // Number of rows in this block
	// Computed/derived fields (not stored on disk)
	MaxDirectRows   uint16 // Max rows of direct blocks (before indirect)
	TableWidth      uint16 // Width of doubling table (from heap header)
	ChecksumPresent bool   // Whether checksum is at end of block
}

// WritableIndirectBlock represents an indirect block for writing.
//
// This structure supports building indirect blocks during heap construction
// and modification operations (RMW - Read-Modify-Write).
//
// MVP Implementation Notes:
//   - Single-level indirect blocks (no multiply-indirect yet)
//   - Direct block children only
//   - No compression/filtering
//   - Simple allocation strategy (append-only)
type WritableIndirectBlock struct {
	Header         WritableIndirectBlockHeader
	ChildAddresses []uint64 // Addresses of child blocks

	// For RMW scenarios
	loadedAddress uint64 // Address where this block was loaded from
}

// WritableIndirectBlockHeader represents the header for a writable indirect block.
type WritableIndirectBlockHeader struct {
	Version         uint8
	HeapHeaderAddr  uint64
	BlockOffset     uint64 // Offset in heap's address space
	NumRows         uint16 // Number of rows in this block
	TableWidth      uint16 // Width of doubling table
	MaxDirectRows   uint16 // Rows containing direct blocks
	ChecksumEnabled bool
}

// Indirect block constants.
const (
	IndirectBlockSignature = "FHIB" // Signature for indirect blocks
	IndirectBlockVersion   = 0      // Current version
)

// NewWritableIndirectBlock creates a new writable indirect block.
//
// Parameters:
//   - heapHeaderAddr: Address of the heap header
//   - blockOffset: Offset of this block within heap's address space
//   - numRows: Number of rows in this indirect block
//   - tableWidth: Width of doubling table (from heap header, typically 2)
//   - maxDirectRows: Number of rows containing direct blocks
//
// Returns:
//   - *WritableIndirectBlock: New indirect block ready for child allocation.
//
// Reference: H5HFiblock.c - H5HF__man_iblock_create().
func NewWritableIndirectBlock(heapHeaderAddr, blockOffset uint64, numRows, tableWidth, maxDirectRows uint16) *WritableIndirectBlock {
	// Calculate total number of entries (K × M)
	// K = number of rows, M = table width
	numEntries := int(numRows) * int(tableWidth)

	return &WritableIndirectBlock{
		Header: WritableIndirectBlockHeader{
			Version:         IndirectBlockVersion,
			HeapHeaderAddr:  heapHeaderAddr,
			BlockOffset:     blockOffset,
			NumRows:         numRows,
			TableWidth:      tableWidth,
			MaxDirectRows:   maxDirectRows,
			ChecksumEnabled: false, // MVP: no checksum
		},
		ChildAddresses: make([]uint64, numEntries),
		loadedAddress:  0,
	}
}

// SetChildAddress sets the address of a child block at the given entry index.
//
// Parameters:
//   - entryIndex: Index in the child address table (0-based)
//   - address: Address of the child block (direct or indirect)
//
// Returns:
//   - error: if entry index is out of bounds
//
// The entry index is calculated from (row, column):
//
//	entryIndex = row × table_width + column
func (wb *WritableIndirectBlock) SetChildAddress(entryIndex int, address uint64) error {
	if entryIndex < 0 || entryIndex >= len(wb.ChildAddresses) {
		return fmt.Errorf("invalid entry index %d (valid range: 0-%d)", entryIndex, len(wb.ChildAddresses)-1)
	}

	wb.ChildAddresses[entryIndex] = address
	return nil
}

// GetChildAddress retrieves the address of a child block at the given entry index.
//
// Parameters:
//   - entryIndex: Index in the child address table (0-based)
//
// Returns:
//   - uint64: Address of the child block (0 = unallocated)
//   - error: if entry index is out of bounds
func (wb *WritableIndirectBlock) GetChildAddress(entryIndex int) (uint64, error) {
	if entryIndex < 0 || entryIndex >= len(wb.ChildAddresses) {
		return 0, fmt.Errorf("invalid entry index %d (valid range: 0-%d)", entryIndex, len(wb.ChildAddresses)-1)
	}

	return wb.ChildAddresses[entryIndex], nil
}

// CalculateEntryIndex calculates the entry index from row and column.
//
// Parameters:
//   - row: Row in the doubling table
//   - col: Column in the doubling table
//
// Returns:
//   - int: Entry index (row × table_width + col)
//
// Example (table_width=2):
//
//	Row 0: entries [0, 1]
//	Row 1: entries [2, 3]
//	Row 2: entries [4, 5]
func (wb *WritableIndirectBlock) CalculateEntryIndex(row, col uint16) int {
	return int(row)*int(wb.Header.TableWidth) + int(col)
}

// WriteToFile serializes and writes the indirect block to file.
//
// Parameters:
//   - writer: File writer interface
//   - allocator: Space allocator for obtaining file address
//   - sb: Superblock for field sizes and endianness
//
// Returns:
//   - uint64: Address where block was written
//   - error: if serialization or write fails.
//
// Reference: H5HFcache.c - H5HF__cache_iblock_serialize().
func (wb *WritableIndirectBlock) WriteToFile(writer Writer, allocator Allocator, sb *core.Superblock, heapOffsetSize uint8) (uint64, error) {
	// Calculate block size on disk
	// Fixed: 4 (sig) + 1 (ver) + sizeof_addr (heap header addr) + heap_off_size (block offset)
	// Variable: K×M entries × sizeof_addr (child addresses)
	// Checksum: 4 bytes (if enabled)

	numEntries := len(wb.ChildAddresses)
	headerSize := 5 + int(sb.OffsetSize) + int(heapOffsetSize)
	entriesSize := numEntries * int(sb.OffsetSize)
	checksumSize := 0
	if wb.Header.ChecksumEnabled {
		checksumSize = 4
	}

	totalSize := headerSize + entriesSize + checksumSize

	// Allocate space in file
	address, err := allocator.Allocate(uint64(totalSize)) //nolint:gosec // G115: totalSize bounded by format
	if err != nil {
		return 0, fmt.Errorf("failed to allocate space for indirect block: %w", err)
	}

	// Serialize and write
	err = wb.writeAt(writer, address, sb, heapOffsetSize)
	if err != nil {
		return 0, fmt.Errorf("failed to write indirect block at 0x%X: %w", address, err)
	}

	// Store address for RMW scenarios
	wb.loadedAddress = address

	return address, nil
}

// WriteAt writes the indirect block at a previously allocated address (RMW).
//
// Parameters:
//   - writer: File writer interface
//   - sb: Superblock for field sizes
//   - heapOffsetSize: Size of heap offset fields
//
// Returns:
//   - error: if block was not loaded from file or write fails.
//
// Reference: Same as WriteToFile, but uses stored address.
func (wb *WritableIndirectBlock) WriteAt(writer Writer, sb *core.Superblock, heapOffsetSize uint8) error {
	if wb.loadedAddress == 0 {
		return fmt.Errorf("cannot use WriteAt: block not loaded from file (use WriteToFile for new blocks)")
	}

	return wb.writeAt(writer, wb.loadedAddress, sb, heapOffsetSize)
}

// writeAt is the internal serialization routine used by both WriteToFile and WriteAt.
//
// Reference: H5HFcache.c - H5HF__cache_iblock_serialize().
func (wb *WritableIndirectBlock) writeAt(writer Writer, address uint64, sb *core.Superblock, heapOffsetSize uint8) error {
	// Calculate size
	numEntries := len(wb.ChildAddresses)
	headerSize := 5 + int(sb.OffsetSize) + int(heapOffsetSize)
	entriesSize := numEntries * int(sb.OffsetSize)
	checksumSize := 0
	if wb.Header.ChecksumEnabled {
		checksumSize = 4
	}

	totalSize := headerSize + entriesSize + checksumSize
	buf := make([]byte, totalSize)
	offset := 0

	// Signature (4 bytes)
	copy(buf[offset:], IndirectBlockSignature)
	offset += 4

	// Version (1 byte)
	buf[offset] = wb.Header.Version
	offset++

	// Heap Header Address (sizeof_addr bytes)
	writeUintVar(buf[offset:], wb.Header.HeapHeaderAddr, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Block Offset (heap_off_size bytes)
	writeUintVar(buf[offset:], wb.Header.BlockOffset, int(heapOffsetSize), sb.Endianness)
	offset += int(heapOffsetSize)

	// Child Block Addresses (K×M entries, sizeof_addr each)
	for _, addr := range wb.ChildAddresses {
		writeUintVar(buf[offset:], addr, int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)
	}

	// Checksum (if enabled)
	if wb.Header.ChecksumEnabled {
		checksum := crc32.ChecksumIEEE(buf[:offset])
		binary.LittleEndian.PutUint32(buf[offset:], checksum)
	}

	// Write to file
	return writer.WriteAtAddress(buf, address)
}

// ParseIndirectBlock reads and parses an indirect block from file.
//
// Parameters:
//   - reader: File reader interface
//   - address: Address of indirect block in file
//   - numRows: Expected number of rows in block
//   - tableWidth: Table width from heap header
//   - sizeofAddr: Size of address fields (from superblock)
//   - heapOffsetSize: Size of heap offset fields (from heap header)
//   - endianness: Byte order (from superblock)
//   - expectedHeapHeaderAddr: Expected heap header address (for validation)
//
// Returns:
//   - *IndirectBlock: Parsed indirect block
//   - error: if parsing fails or validation errors.
//
// Reference: H5HFcache.c - H5HF__cache_iblock_deserialize().
func ParseIndirectBlock(reader io.ReaderAt, address uint64, numRows, tableWidth uint16,
	sizeofAddr, heapOffsetSize uint8, endianness binary.ByteOrder, expectedHeapHeaderAddr uint64) (*IndirectBlock, error) {
	if address == 0 || address == ^uint64(0) {
		return nil, fmt.Errorf("invalid indirect block address: 0x%X", address)
	}

	// Calculate block size
	numEntries := int(numRows) * int(tableWidth)
	headerSize := 5 + int(sizeofAddr) + int(heapOffsetSize)
	entriesSize := numEntries * int(sizeofAddr)
	// Assume checksum present (common case)
	totalSize := headerSize + entriesSize + 4

	// Read block data
	buf := make([]byte, totalSize)
	//nolint:gosec // G115: uint64 to int64 conversion safe for file offsets
	if _, err := reader.ReadAt(buf, int64(address)); err != nil {
		return nil, fmt.Errorf("failed to read indirect block: %w", err)
	}

	offset := 0
	iblock := &IndirectBlock{}

	// Signature (4 bytes) - "FHIB"
	copy(iblock.Header.Signature[:], buf[offset:offset+4])
	if string(iblock.Header.Signature[:]) != IndirectBlockSignature {
		return nil, fmt.Errorf("invalid indirect block signature: %q (expected %s)",
			iblock.Header.Signature, IndirectBlockSignature)
	}
	offset += 4

	// Version (1 byte)
	iblock.Header.Version = buf[offset]
	if iblock.Header.Version != IndirectBlockVersion {
		return nil, fmt.Errorf("unsupported indirect block version: %d (only version %d supported)",
			iblock.Header.Version, IndirectBlockVersion)
	}
	offset++

	// Heap Header Address (sizeof_addr bytes)
	iblock.Header.HeapHeaderAddr = readUint(buf[offset:offset+int(sizeofAddr)], int(sizeofAddr), endianness)
	offset += int(sizeofAddr)

	// Validate heap header address
	if iblock.Header.HeapHeaderAddr != expectedHeapHeaderAddr {
		return nil, fmt.Errorf("indirect block heap header address mismatch: 0x%X (expected 0x%X)",
			iblock.Header.HeapHeaderAddr, expectedHeapHeaderAddr)
	}

	// Block Offset (heap_off_size bytes)
	iblock.Header.BlockOffset = readUint(buf[offset:offset+int(heapOffsetSize)], int(heapOffsetSize), endianness)
	offset += int(heapOffsetSize)

	// Store metadata
	iblock.Header.NumRows = numRows
	iblock.Header.TableWidth = tableWidth

	// Child Block Addresses (K×M entries, sizeof_addr each)
	iblock.Entries = make([]uint64, numEntries)
	for i := 0; i < numEntries; i++ {
		iblock.Entries[i] = readUint(buf[offset:offset+int(sizeofAddr)], int(sizeofAddr), endianness)
		offset += int(sizeofAddr)
	}

	// Checksum (4 bytes) - validate if present
	// For MVP: skip validation, just note presence
	iblock.Header.ChecksumPresent = (totalSize == headerSize+entriesSize+4)

	return iblock, nil
}

// LoadFromFile loads an existing indirect block for modification (RMW).
//
// Parameters:
//   - reader: File reader interface
//   - address: Address of indirect block in file
//   - numRows: Expected number of rows
//   - tableWidth: Table width from heap header
//   - maxDirectRows: Max rows with direct blocks
//   - sb: Superblock for field sizes
//   - heapOffsetSize: Size of heap offset fields
//   - expectedHeapHeaderAddr: Expected heap header address
//
// Returns:
//   - error: if loading fails.
//
// Reference: H5HFiblock.c - H5HF__man_iblock_protect().
func (wb *WritableIndirectBlock) LoadFromFile(reader io.ReaderAt, address uint64, numRows, tableWidth, maxDirectRows uint16,
	sb *core.Superblock, heapOffsetSize uint8, expectedHeapHeaderAddr uint64) error {
	// Parse the indirect block
	iblock, err := ParseIndirectBlock(reader, address, numRows, tableWidth,
		sb.OffsetSize, heapOffsetSize, sb.Endianness, expectedHeapHeaderAddr)
	if err != nil {
		return fmt.Errorf("failed to parse indirect block: %w", err)
	}

	// Store loaded address for WriteAt() support
	wb.loadedAddress = address

	// Convert to writable format
	wb.Header = WritableIndirectBlockHeader{
		Version:         iblock.Header.Version,
		HeapHeaderAddr:  iblock.Header.HeapHeaderAddr,
		BlockOffset:     iblock.Header.BlockOffset,
		NumRows:         numRows,
		TableWidth:      tableWidth,
		MaxDirectRows:   maxDirectRows,
		ChecksumEnabled: iblock.Header.ChecksumPresent,
	}

	// Copy child addresses
	wb.ChildAddresses = make([]uint64, len(iblock.Entries))
	copy(wb.ChildAddresses, iblock.Entries)

	return nil
}
