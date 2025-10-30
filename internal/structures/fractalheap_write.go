// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

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
// This is an MVP implementation for dense groups phase 2.
//
// MVP Limitations:
// - Single direct block only (no indirect blocks)
// - No filters/compression
// - No huge objects
// - No tiny object optimization
// - Simple object IDs (offset-based)
//
// Reference: H5HF.c, H5HFhdr.c, H5HFdblock.c.
type WritableFractalHeap struct {
	Header      *WritableHeapHeader
	DirectBlock *WritableDirectBlock
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
	heapOffsetSize := uint8((maxHeapSize + 7) / 8) // 2 bytes

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
		Header:      header,
		DirectBlock: directBlock,
	}
}

// InsertObject inserts a variable-length object into the fractal heap.
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

	// Check if enough space in direct block
	if fh.DirectBlock.FreeOffset+dataSize > fh.DirectBlock.Size {
		return nil, fmt.Errorf("%w: need %d bytes, have %d free",
			ErrHeapFull, dataSize, fh.DirectBlock.Size-fh.DirectBlock.FreeOffset)
	}

	// Current offset becomes the object's location
	objectOffset := fh.DirectBlock.FreeOffset

	// Append object to direct block
	fh.DirectBlock.Objects = append(fh.DirectBlock.Objects, data...)

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

	directBlockHeaderSize := 5 + int(sb.OffsetSize) + int(fh.Header.HeapOffsetSize)
	checksumSize := 0
	if fh.DirectBlock.ChecksumEnabled {
		checksumSize = 4
	}
	directBlockSize := directBlockHeaderSize + len(fh.DirectBlock.Objects) + checksumSize

	// Allocate both addresses
	headerAddr, err := allocator.Allocate(uint64(headerSize))
	if err != nil {
		return 0, fmt.Errorf("failed to allocate heap header: %w", err)
	}

	directBlockAddr, err := allocator.Allocate(uint64(directBlockSize))
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
	if err := writer.WriteAtAddress(buf, addr); err != nil {
		return err
	}

	return nil
}

// writeDirectBlockAt serializes and writes direct block at the given address.
//
// Reference: H5HFcache.c - H5HF__cache_dblock_pre_serialize().
func (fh *WritableFractalHeap) writeDirectBlockAt(writer Writer, addr uint64, sb *core.Superblock) error {
	// Calculate header size
	// Signature (4) + Version (1) + Heap Header Address (offsetSize) + Block Offset (heapOffsetSize)
	headerSize := 5 + int(sb.OffsetSize) + int(fh.Header.HeapOffsetSize)

	// Add checksum if enabled
	checksumSize := 0
	if fh.DirectBlock.ChecksumEnabled {
		checksumSize = 4
	}

	// Total size is header + used objects + checksum
	totalSize := headerSize + len(fh.DirectBlock.Objects) + checksumSize

	buf := make([]byte, totalSize)
	offset := 0

	// Signature
	copy(buf[offset:], DirectBlockSignature)
	offset += 4

	// Version
	buf[offset] = fh.DirectBlock.Version
	offset++

	// Heap Header Address (will be 0 initially, updated after header write)
	writeUintVar(buf[offset:], fh.DirectBlock.HeapHeaderAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Block Offset (variable-sized based on heap offset size)
	writeUintVar(buf[offset:], fh.DirectBlock.BlockOffset, int(fh.Header.HeapOffsetSize), sb.Endianness)
	offset += int(fh.Header.HeapOffsetSize)

	// Object data
	copy(buf[offset:], fh.DirectBlock.Objects)
	offset += len(fh.DirectBlock.Objects)

	// Checksum (if enabled)
	if fh.DirectBlock.ChecksumEnabled {
		checksum := crc32.ChecksumIEEE(buf[:offset])
		binary.LittleEndian.PutUint32(buf[offset:], checksum)
	}

	// Write to file at pre-allocated address
	if err := writer.WriteAtAddress(buf, addr); err != nil {
		return err
	}

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
	offset := readUint(heapID[idx:idx+int(fh.Header.HeapOffsetSize)], int(fh.Header.HeapOffsetSize), binary.LittleEndian)
	idx += int(fh.Header.HeapOffsetSize)

	length := readUint(heapID[idx:idx+int(fh.Header.HeapLengthSize)], int(fh.Header.HeapLengthSize), binary.LittleEndian)

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

// writeUintVar writes a variable-length unsigned integer.
func writeUintVar(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = uint8(value)
	case 2:
		endianness.PutUint16(buf[:2], uint16(value))
	case 4:
		endianness.PutUint32(buf[:4], uint32(value))
	case 8:
		endianness.PutUint64(buf[:8], value)
	default:
		// Variable size - write as bytes
		for i := 0; i < size && i < 8; i++ {
			if endianness == binary.LittleEndian {
				buf[i] = uint8(value >> (8 * i))
			} else {
				buf[size-1-i] = uint8(value >> (8 * i))
			}
		}
	}
}
