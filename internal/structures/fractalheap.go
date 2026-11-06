package structures

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
)

// FractalHeapHeader holds the fields of a fractal heap header that
// WritableFractalHeap.LoadFromFile carries into its rewrite of the heap.
// Objects are read through core.OpenFractalHeap, the one fractal heap
// reader; this package keeps only what writing needs.
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

	// Huge objects
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

// parseFractalHeapHeader decodes the fields of the fractal heap header at
// address for a rewrite of the heap. The caller has opened the heap with
// core.OpenFractalHeap first, which checks the header (signature, version,
// checksum and the doubling table parameters).
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
	heapOffsetSize, err := core.FractalHeapOffsetSize(header.MaxHeapSize, sizeofSize)
	if err != nil {
		return nil, err
	}
	header.HeapOffsetSize = heapOffsetSize

	// Heap length size is the minimum of the max direct block offset size,
	// H5HF_SIZEOF_OFFSET_LEN(max_direct) = (log2(max_direct) + 7) / 8
	// (H5HFdtable.c:96, H5HFpkg.h:133-134), and the encoded max managed size,
	// H5VM_limit_enc_size(max_man) (H5HFhdr.c:199-200).
	header.HeapLengthSize = heapLengthSize(header.MaxDirectBlockSize, header.MaxManagedObjSize)

	return header, nil
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
