package structures

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// FractalHeap represents a minimal read-only fractal heap implementation.
// This implementation supports only direct blocks and simple managed objects,
// which covers the majority of dense attribute storage use cases.
//
// Reference: HDF5 C library H5HF*.c files
// Format Spec: https://docs.hdfgroup.org/hdf5/latest/_f_m_t3.html#FractalHeap
//
// Limitations (v0.10.0-beta):
// - Only direct blocks supported (no indirect blocks)
// - No huge objects support (objects stored outside heap)
// - No tiny objects optimization
// - Objects must be < max_direct_size.
type FractalHeap struct {
	Header     *FractalHeapHeader
	reader     io.ReaderAt
	headerAddr uint64
	sizeofSize uint8 // Size of size fields in file
	sizeofAddr uint8 // Size of address fields in file
	endianness binary.ByteOrder
}

// FractalHeapHeader represents the fractal heap header structure.
//
// Reference: H5HFhdr.c, H5HFpkg.h (struct H5HF_hdr_t)
// Format Spec: III.E.2 Disk Format: Level 0E - Fractal Heap.
type FractalHeapHeader struct {
	// Signature and version
	Signature [4]byte // "FRHP" for fractal heap
	Version   uint8   // Version number (currently 0)

	// General heap information
	HeapIDLen    uint16 // Length of heap IDs (in bytes)
	IOFiltersLen uint16 // Length of I/O filter information
	Flags        uint8  // Status flags

	// Object size limits
	MaxManagedObjSize uint32 // Maximum size of managed objects

	// Huge object support (not used in minimal implementation)
	NextHugeObjID    uint64 // Next ID for huge objects
	HugeObjBTreeAddr uint64 // Address of v2 B-tree for huge objects

	// Free space management
	FreeSpaceAmount      uint64 // Amount of managed free space in heap
	FreeSpaceSectionAddr uint64 // Address of managed free space section

	// Heap statistics
	ManagedObjSpaceSize  uint64 // Total managed objects space
	ManagedObjAllocSize  uint64 // Allocated managed objects space
	ManagedObjIterOffset uint64 // Offset of managed objects iterator
	ManagedObjCount      uint64 // Number of managed objects
	HugeObjSize          uint64 // Size of huge objects
	HugeObjCount         uint64 // Number of huge objects
	TinyObjSize          uint64 // Size of tiny objects
	TinyObjCount         uint64 // Number of tiny objects

	// Doubling table parameters
	TableWidth            uint16 // Width of doubling table
	StartingBlockSize     uint64 // Starting block size
	MaxDirectBlockSize    uint64 // Maximum direct block size
	MaxHeapSize           uint16 // Log2 of maximum heap size
	StartRootIndirectRows uint16 // Starting rows in root indirect block
	RootBlockAddr         uint64 // Address of root block
	CurrentRowCount       uint16 // Current number of rows

	// Computed values (not stored on disk)
	HeapOffsetSize       uint8 // Size of heap offsets (bytes)
	HeapLengthSize       uint8 // Size of heap lengths (bytes)
	ChecksumDirectBlocks bool  // Whether direct blocks are checksummed
}

// HeapID represents a fractal heap object identifier.
// Format (for managed objects):
// - Byte 0: Version and type flags
// - Bytes 1+: Offset (variable length)
// - Bytes N+: Length (variable length).
type HeapID struct {
	Raw     []byte
	Version uint8
	Type    HeapIDType
	Offset  uint64 // Offset within heap
	Length  uint64 // Length of object
}

// HeapIDType identifies the type of heap object.
type HeapIDType uint8

const (
	// HeapIDTypeManaged - Managed object stored in fractal heap blocks.
	HeapIDTypeManaged HeapIDType = 0x00
	// HeapIDTypeHuge - Huge object stored in file directly.
	HeapIDTypeHuge HeapIDType = 0x10
	// HeapIDTypeTiny - Tiny object stored in heap ID directly.
	HeapIDTypeTiny HeapIDType = 0x20
)

// DirectBlock represents a fractal heap direct block.
// Direct blocks contain the actual object data.
//
// Reference: H5HFdblock.c, H5HFpkg.h (struct H5HF_direct_t).
type DirectBlock struct {
	Signature      [4]byte // "FHDB" for fractal heap direct block
	Version        uint8
	HeapHeaderAddr uint64 // Address of heap header
	BlockOffset    uint64 // Offset of block within heap
	Checksum       uint32 // Optional checksum
	Data           []byte // Block data (after header)
}

// OpenFractalHeap opens and parses a fractal heap at the given address.
// This is the main entry point for reading fractal heap data.
//
// Parameters:
// - r: Reader for the HDF5 file
// - address: File address of fractal heap header
// - sizeofSize: Size of size fields (from superblock)
// - sizeofAddr: Size of address fields (from superblock)
// - endianness: Byte order (from superblock)
//
// Returns:
// - *FractalHeap: Parsed heap structure
// - error: Any parsing errors.
func OpenFractalHeap(r io.ReaderAt, address uint64, sizeofSize, sizeofAddr uint8, endianness binary.ByteOrder) (*FractalHeap, error) {
	if address == 0 || address == ^uint64(0) {
		return nil, fmt.Errorf("invalid fractal heap address: 0x%X", address)
	}

	header, err := parseFractalHeapHeader(r, address, sizeofSize, sizeofAddr, endianness)
	if err != nil {
		return nil, utils.WrapError("failed to parse fractal heap header", err)
	}

	heap := &FractalHeap{
		Header:     header,
		reader:     r,
		headerAddr: address,
		sizeofSize: sizeofSize,
		sizeofAddr: sizeofAddr,
		endianness: endianness,
	}

	return heap, nil
}

// parseFractalHeapHeader reads and parses the fractal heap header.
//
// Reference: H5HFhdr.c - H5HF__hdr_deserialize()
// Format: See HDF5 spec III.E.2 "Disk Format: Level 0E - Fractal Heap".
//
//nolint:funlen // complex HDF5 format parsing, matches C library structure
func parseFractalHeapHeader(r io.ReaderAt, address uint64, sizeofSize, sizeofAddr uint8, endianness binary.ByteOrder) (*FractalHeapHeader, error) {
	// Calculate header size dynamically based on field sizes
	// Fixed: 4 (sig) + 1 (ver) + 2 (heap ID len) + 2 (filter len) + 1 (flags) + 4 (max obj size) = 14
	// Variable: 1*sizeofSize + 1*sizeofAddr + 1*sizeofSize + 1*sizeofAddr = 2*sizeofSize + 2*sizeofAddr (huge obj fields)
	// Variable: 8*sizeofSize (statistics: 4 managed + 2 huge + 2 tiny)
	// Doubling table: 2 + 2*sizeofSize + 2 + 2 + sizeofAddr + 2 = 8 + 2*sizeofSize + sizeofAddr
	// Total: 14 + 2*sizeofSize + 2*sizeofAddr + 8*sizeofSize + 8 + 2*sizeofSize + sizeofAddr
	//      = 22 + 12*sizeofSize + 3*sizeofAddr
	headerSize := 22 + 12*int(sizeofSize) + 3*int(sizeofAddr)

	buf := make([]byte, headerSize)
	//nolint:gosec // G115: uint64 to int64 conversion safe for file offsets
	if _, err := r.ReadAt(buf, int64(address)); err != nil {
		return nil, fmt.Errorf("failed to read fractal heap header: %w", err)
	}

	header := &FractalHeapHeader{}
	offset := 0

	// Signature (4 bytes) - "FRHP"
	copy(header.Signature[:], buf[offset:offset+4])
	if string(header.Signature[:]) != "FRHP" {
		return nil, fmt.Errorf("invalid fractal heap signature: %q (expected FRHP)", header.Signature)
	}
	offset += 4

	// Version (1 byte) - currently 0
	header.Version = buf[offset]
	if header.Version != 0 {
		return nil, fmt.Errorf("unsupported fractal heap version: %d (only version 0 supported)", header.Version)
	}
	offset++

	// Heap ID Length (2 bytes)
	header.HeapIDLen = endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// I/O Filters Encoded Length (2 bytes)
	header.IOFiltersLen = endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Flags (1 byte)
	header.Flags = buf[offset]
	offset++

	// Parse flags
	header.ChecksumDirectBlocks = (header.Flags & 0x02) != 0

	// Maximum Size of Managed Objects (4 bytes)
	header.MaxManagedObjSize = endianness.Uint32(buf[offset : offset+4])
	offset += 4

	// Next Huge Object ID (sizeof_size bytes)
	header.NextHugeObjID = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// v2 B-tree Address of Huge Objects (sizeof_addr bytes)
	header.HugeObjBTreeAddr = readUint(buf[offset:offset+int(sizeofAddr)], int(sizeofAddr), endianness)
	offset += int(sizeofAddr)

	// Amount of Managed Free Space (sizeof_size bytes)
	header.FreeSpaceAmount = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Address of Managed Free Space (sizeof_addr bytes)
	header.FreeSpaceSectionAddr = readUint(buf[offset:offset+int(sizeofAddr)], int(sizeofAddr), endianness)
	offset += int(sizeofAddr)

	// Managed Objects heap statistics (4 * sizeof_size bytes)
	header.ManagedObjSpaceSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	header.ManagedObjAllocSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	header.ManagedObjIterOffset = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	header.ManagedObjCount = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Huge Objects heap statistics (2 * sizeof_size bytes)
	header.HugeObjSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	header.HugeObjCount = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Tiny Objects heap statistics (2 * sizeof_size bytes)
	header.TinyObjSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	header.TinyObjCount = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Managed Objects Doubling-Table (variable size)
	// Table Width (2 bytes)
	header.TableWidth = endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Starting Block Size (sizeof_size bytes)
	header.StartingBlockSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Maximum Direct Block Size (sizeof_size bytes)
	header.MaxDirectBlockSize = readUint(buf[offset:offset+int(sizeofSize)], int(sizeofSize), endianness)
	offset += int(sizeofSize)

	// Maximum Heap Size (2 bytes) - stored as log2
	header.MaxHeapSize = endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Starting # of Rows in Root Indirect Block (2 bytes)
	header.StartRootIndirectRows = endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Address of Root Block (sizeof_addr bytes)
	header.RootBlockAddr = readUint(buf[offset:offset+int(sizeofAddr)], int(sizeofAddr), endianness)
	offset += int(sizeofAddr)

	// Current # of Rows in Root Indirect Block (2 bytes)
	header.CurrentRowCount = endianness.Uint16(buf[offset : offset+2])
	// Note: offset no longer used after this point

	// Compute derived values
	// Reference: H5HFhdr.c - H5HF__hdr_finish_init_phase1()
	maxIndexBits := header.MaxHeapSize
	//nolint:gosec // G115: safe conversion, maxIndexBits is uint16, result < 255
	header.HeapOffsetSize = uint8((maxIndexBits + 7) / 8)

	// Heap length size is minimum of max direct block offset size and encoded max managed size
	maxDirBlockOffsetSize := computeOffsetSize(header.MaxDirectBlockSize)
	maxManSizeEncoded := computeOffsetSize(uint64(header.MaxManagedObjSize))
	if maxDirBlockOffsetSize < maxManSizeEncoded {
		header.HeapLengthSize = maxDirBlockOffsetSize
	} else {
		header.HeapLengthSize = maxManSizeEncoded
	}

	// Skip I/O filter information if present (not needed for minimal implementation)
	// offset += int(header.IOFiltersLen)

	// Checksum (4 bytes) - at end of header
	// Skip for now

	return header, nil
}

// ReadObject reads an object from the fractal heap given its heap ID.
// This is the main interface for retrieving objects from the heap.
//
// Parameters:
// - heapID: The heap ID (from attribute info message or B-tree)
//
// Returns:
// - []byte: Object data
// - error: Any read errors or unsupported features.
func (fh *FractalHeap) ReadObject(heapID []byte) ([]byte, error) {
	// Parse heap ID
	id, err := fh.parseHeapID(heapID)
	if err != nil {
		return nil, utils.WrapError("failed to parse heap ID", err)
	}

	// Handle different ID types
	switch id.Type {
	case HeapIDTypeManaged:
		return fh.readManagedObject(id)
	case HeapIDTypeHuge:
		return nil, fmt.Errorf("huge objects not supported in minimal implementation (heap ID type: 0x%02X)", id.Type)
	case HeapIDTypeTiny:
		return fh.readTinyObject(id)
	default:
		return nil, fmt.Errorf("unsupported heap ID type: 0x%02X", id.Type)
	}
}

// parseHeapID parses a heap ID into its components.
//
// Reference: H5HFpkg.h - H5HF_MAN_ID_DECODE macro.
func (fh *FractalHeap) parseHeapID(heapID []byte) (*HeapID, error) {
	if len(heapID) < 1 {
		return nil, fmt.Errorf("heap ID too short: %d bytes", len(heapID))
	}

	id := &HeapID{
		Raw: heapID,
	}

	// First byte contains version and type
	flags := heapID[0]
	id.Version = (flags & 0xC0) >> 6   // Bits 6-7
	id.Type = HeapIDType(flags & 0x30) // Bits 4-5

	if id.Version != 0 {
		return nil, fmt.Errorf("unsupported heap ID version: %d", id.Version)
	}

	offset := 1

	// Decode ID type-specific fields
	switch id.Type {
	case HeapIDTypeManaged:
		// For managed objects, decode offset and length
		offsetSize := int(fh.Header.HeapOffsetSize)
		lengthSize := int(fh.Header.HeapLengthSize)

		if len(heapID) < 1+offsetSize+lengthSize {
			return nil, fmt.Errorf("heap ID too short for managed object: %d bytes (need %d)",
				len(heapID), 1+offsetSize+lengthSize)
		}

		// Decode offset
		id.Offset = readUint(heapID[offset:offset+offsetSize], offsetSize, fh.endianness)
		offset += offsetSize

		// Decode length
		id.Length = readUint(heapID[offset:offset+lengthSize], lengthSize, fh.endianness)

	case HeapIDTypeTiny:
		// Tiny objects: length encoded in ID, data inline
		// For now, return the rest of the ID as the object data
		//nolint:gosec // G115: safe conversion, heap ID length bounded by format (max 64K)
		id.Length = uint64(len(heapID) - 1)
		id.Offset = 0
	}

	return id, nil
}

// readManagedObject reads a managed object from a direct block.
//
// Reference: H5HF.c - H5HF_read(), H5HFdblock.c.
func (fh *FractalHeap) readManagedObject(id *HeapID) ([]byte, error) {
	// For minimal implementation, assume root block is a direct block
	// (no indirect block support yet)
	if fh.Header.CurrentRowCount != 0 {
		return nil, fmt.Errorf("indirect blocks not supported in minimal implementation (root has %d rows)",
			fh.Header.CurrentRowCount)
	}

	// Read the direct block at root address
	dblock, err := fh.readDirectBlock(fh.Header.RootBlockAddr, fh.Header.StartingBlockSize)
	if err != nil {
		return nil, utils.WrapError("failed to read direct block", err)
	}

	// Extract object from block data
	// Offset is relative to heap space, need to account for direct block offset
	if id.Offset < dblock.BlockOffset {
		return nil, fmt.Errorf("object offset 0x%X before block offset 0x%X", id.Offset, dblock.BlockOffset)
	}

	relativeOffset := id.Offset - dblock.BlockOffset

	if relativeOffset > uint64(len(dblock.Data)) {
		return nil, fmt.Errorf("object offset 0x%X beyond block data (size: %d)", relativeOffset, len(dblock.Data))
	}

	if relativeOffset+id.Length > uint64(len(dblock.Data)) {
		return nil, fmt.Errorf("object extends beyond block data (offset: 0x%X, length: %d, block size: %d)",
			relativeOffset, id.Length, len(dblock.Data))
	}

	// Extract and return object data
	objData := make([]byte, id.Length)
	copy(objData, dblock.Data[relativeOffset:relativeOffset+id.Length])

	return objData, nil
}

// readTinyObject reads a tiny object (data stored inline in heap ID).
//
// Reference: H5HFtiny.c.
func (fh *FractalHeap) readTinyObject(id *HeapID) ([]byte, error) {
	// Tiny objects store data directly in the heap ID after the first byte
	if len(id.Raw) < 2 {
		return []byte{}, nil
	}

	// Return data portion (skip first byte which is flags)
	data := make([]byte, len(id.Raw)-1)
	copy(data, id.Raw[1:])

	return data, nil
}

// readDirectBlock reads and parses a fractal heap direct block.
//
// Reference: H5HFdblock.c - H5HF__cache_dblock_deserialize().
func (fh *FractalHeap) readDirectBlock(address, blockSize uint64) (*DirectBlock, error) {
	if address == 0 || address == ^uint64(0) {
		return nil, fmt.Errorf("invalid direct block address: 0x%X", address)
	}

	// Calculate header size (currently not used but kept for documentation)
	// Signature (4) + Version (1) + Heap Header Address (sizeof_addr) + Block Offset (heap_off_size)
	_ = 5 + int(fh.sizeofAddr) + int(fh.Header.HeapOffsetSize) // headerSize calculated but not used yet

	// Read entire block (header + data)
	//nolint:gosec // G115: safe conversion, blockSize from HDF5 header (max ~2GB per block)
	totalSize := int(blockSize)
	buf := make([]byte, totalSize)
	//nolint:gosec // G115: uint64 to int64 conversion safe for file offsets
	if _, err := fh.reader.ReadAt(buf, int64(address)); err != nil {
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
	dblock.HeapHeaderAddr = readUint(buf[offset:offset+int(fh.sizeofAddr)], int(fh.sizeofAddr), fh.endianness)
	offset += int(fh.sizeofAddr)

	// Verify heap header address matches
	if dblock.HeapHeaderAddr != fh.headerAddr {
		return nil, fmt.Errorf("direct block heap header address mismatch: 0x%X (expected 0x%X)",
			dblock.HeapHeaderAddr, fh.headerAddr)
	}

	// Block Offset (heap_off_size bytes)
	dblock.BlockOffset = readUint(buf[offset:offset+int(fh.Header.HeapOffsetSize)],
		int(fh.Header.HeapOffsetSize), fh.endianness)
	offset += int(fh.Header.HeapOffsetSize)

	// Checksum (4 bytes) - if enabled, at end of block
	// Checksum validation deferred to v1.0.0 (stable release).
	// Current implementation reads but does not verify checksums.
	// For production use, rely on file system integrity or external validation.
	// Target version: v1.0.0 (comprehensive data integrity features)
	// dblock.Checksum = fh.endianness.Uint32(buf[totalSize-4 : totalSize])

	// Data (remaining bytes, excluding checksum if present)
	dataEnd := totalSize
	if fh.Header.ChecksumDirectBlocks {
		dataEnd -= 4
	}
	dblock.Data = make([]byte, dataEnd-offset)
	copy(dblock.Data, buf[offset:dataEnd])

	return dblock, nil
}

// readUint reads a variable-length unsigned integer.
// Used for reading size and address fields with variable byte widths.
func readUint(data []byte, size int, endianness binary.ByteOrder) uint64 {
	if len(data) < size {
		return 0
	}

	switch size {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(endianness.Uint16(data[:2]))
	case 4:
		return uint64(endianness.Uint32(data[:4]))
	case 8:
		return endianness.Uint64(data[:8])
	default:
		// Variable size - read as little-endian bytes
		var val uint64
		for i := 0; i < size && i < 8; i++ {
			if endianness == binary.LittleEndian {
				val |= uint64(data[i]) << (8 * i)
			} else {
				val = (val << 8) | uint64(data[i])
			}
		}
		return val
	}
}

// computeOffsetSize computes the number of bytes needed to store a value.
// Reference: H5HFpkg.h - H5HF_SIZEOF_OFFSET_LEN macro.
func computeOffsetSize(value uint64) uint8 {
	if value == 0 {
		return 1
	}

	// Find highest bit set
	bits := 0
	v := value
	for v > 0 {
		bits++
		v >>= 1
	}

	// Round up to bytes
	//nolint:gosec // G115: safe conversion, bits <= 64, result <= 8
	return uint8((bits + 7) / 8)
}
