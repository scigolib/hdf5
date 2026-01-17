package structures

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// LocalHeap represents an HDF5 local heap for storing short strings.
// Used by symbol tables to store object names.
//
// Format (HDF5 specification):
//
//	Header (32 bytes for 8-byte addressing):
//	  - Signature: "HEAP" (4 bytes)
//	  - Version: 0 (1 byte)
//	  - Reserved: 0 (3 bytes)
//	  - Data segment size (size_t - 8 bytes)
//	  - Offset to head of free list (size_t - 8 bytes)
//	  - Data segment address (address_t - 8 bytes)
//	Data segment:
//	  - Null-terminated strings, stored sequentially
//	  - Free blocks tracked by free list (not used in MVP)
type LocalHeap struct {
	// Reading fields
	Data       []byte
	FreeList   uint64
	HeaderSize uint64

	// Writing fields
	DataSegmentSize      uint64 // Size of data segment
	OffsetToHeadFreeList uint64 // Offset to head of free list (MVP: always 1 = null)
	DataSegmentAddress   uint64 // Address where data segment will be written
	strings              []byte // Buffer for storing strings during construction
}

// LoadLocalHeap loads a local heap from the specified file address.
func LoadLocalHeap(r io.ReaderAt, address uint64, sb *core.Superblock) (*LocalHeap, error) {
	// Calculate header size based on offset/length sizes
	// Format: Signature(4) + Version(1) + Reserved(3) + DataSegmentSize(lengthSize) +
	//         FreeListOffset(lengthSize) + DataSegmentAddress(offsetSize)
	headerSize := 8 + int(sb.LengthSize)*2 + int(sb.OffsetSize)

	headerBuf := utils.GetBuffer(headerSize)
	defer utils.ReleaseBuffer(headerBuf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(headerBuf, int64(address)); err != nil {
		return nil, utils.WrapError("local heap header read failed", err)
	}

	if string(headerBuf[0:4]) != "HEAP" {
		return nil, errors.New("invalid local heap signature")
	}

	// Parse header fields using file's endianness
	pos := 8 // After signature, version, reserved

	// Data segment size (lengthSize bytes)
	var dataSegmentSize uint64
	switch sb.LengthSize {
	case 2:
		dataSegmentSize = uint64(sb.Endianness.Uint16(headerBuf[pos : pos+2]))
	case 4:
		dataSegmentSize = uint64(sb.Endianness.Uint32(headerBuf[pos : pos+4]))
	case 8:
		dataSegmentSize = sb.Endianness.Uint64(headerBuf[pos : pos+8])
	}
	pos += int(sb.LengthSize)

	// Free list offset (lengthSize bytes) - skip for now
	pos += int(sb.LengthSize)

	// Data segment address (offsetSize bytes)
	var dataSegmentAddr uint64
	switch sb.OffsetSize {
	case 2:
		dataSegmentAddr = uint64(sb.Endianness.Uint16(headerBuf[pos : pos+2]))
	case 4:
		dataSegmentAddr = uint64(sb.Endianness.Uint32(headerBuf[pos : pos+4]))
	case 8:
		dataSegmentAddr = sb.Endianness.Uint64(headerBuf[pos : pos+8])
	}

	heap := &LocalHeap{
		//nolint:gosec // G115: headerSize is calculated from small values (LengthSize, OffsetSize <= 8)
		HeaderSize: uint64(headerSize),
	}

	// Allocate and read data segment from the ACTUAL address in the header
	heap.Data = make([]byte, dataSegmentSize)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(heap.Data, int64(dataSegmentAddr)); err != nil {
		return nil, utils.WrapError("local heap data read failed", err)
	}

	return heap, nil
}

// GetString retrieves a null-terminated string from the heap at the given offset.
// The offset is relative to the start of the data segment (after the 32-byte header).
func (h *LocalHeap) GetString(offset uint64) (string, error) {
	if offset >= uint64(len(h.Data)) {
		return "", errors.New("offset beyond heap data")
	}

	end := offset
	for end < uint64(len(h.Data)) && h.Data[end] != 0 {
		end++
	}

	if end >= uint64(len(h.Data)) {
		return "", errors.New("string not null-terminated")
	}

	return string(h.Data[offset:end]), nil
}

// --- Write Support Functions ---

// NewLocalHeap creates a new local heap with the specified initial size.
// The heap is used to store null-terminated strings for symbol table entries.
//
// Parameters:
//   - initialSize: Initial size of the heap data segment (will be rounded up)
//
// Returns:
//   - *LocalHeap: New local heap ready for adding strings
//
// For MVP:
//   - No free list management (append-only)
//   - Strings are stored sequentially with null terminators
//   - Size is fixed at creation (no dynamic growth)
func NewLocalHeap(initialSize uint64) *LocalHeap {
	// Ensure minimum size (at least 16 bytes for alignment)
	if initialSize < 16 {
		initialSize = 16
	}

	// Round up to 8-byte alignment (HDF5 requirement)
	if initialSize%8 != 0 {
		initialSize = ((initialSize / 8) + 1) * 8
	}

	return &LocalHeap{
		DataSegmentSize:      initialSize,
		OffsetToHeadFreeList: 1, // 1 = H5HL_FREE_NULL (no free list in MVP)
		DataSegmentAddress:   0, // Will be set when heap is written
		strings:              make([]byte, 0, initialSize),
	}
}

// AddString adds a null-terminated string to the heap and returns its offset.
// The offset can be used in symbol table entries to reference this string.
//
// Parameters:
//   - s: String to add (will be null-terminated automatically)
//
// Returns:
//   - offset: Offset of the string in the data segment (0-based)
//   - error: If the heap is full or string is too long
//
// Thread safety: Not thread-safe, caller must synchronize.
func (h *LocalHeap) AddString(s string) (offset uint64, err error) {
	// Calculate space needed: string length + null terminator
	needed := len(s) + 1

	// Check if we have space
	currentSize := uint64(len(h.strings))
	if currentSize+uint64(needed) > h.DataSegmentSize { //nolint:gosec // Safe: size calculation
		return 0, errors.New("local heap is full")
	}

	// Record offset before adding
	offset = currentSize

	// DEBUG: Log before adding
	// fmt.Printf("DEBUG AddString: adding '%s' at offset %d\n", s, offset)
	// fmt.Printf("DEBUG AddString: strings before: %q (len=%d)\n", h.strings, len(h.strings))

	// Add string with null terminator
	h.strings = append(h.strings, []byte(s)...)
	h.strings = append(h.strings, 0) // Null terminator

	// DEBUG: Log after adding
	// fmt.Printf("DEBUG AddString: strings after: %q (len=%d)\n", h.strings, len(h.strings))

	return offset, nil
}

// WriteTo writes the local heap to the file at the specified address.
// This includes the header and the data segment.
//
// Parameters:
//   - w: Writer to write to (must support WriteAt)
//   - address: Address where heap header will be written
//
// Returns:
//   - error: If write fails
//
// Format written:
//   - Header (32 bytes): Signature + version + size + free list + data address
//   - Data segment (at address + 32): Strings with null terminators
//
// The data segment address in the header is set to address + 32.
func (h *LocalHeap) WriteTo(w io.WriterAt, address uint64) error {
	// Set data segment address (immediately after header)
	// Header size is 32 bytes for 8-byte addressing (4 + 1 + 3 + 8 + 8 + 8)
	headerSize := uint64(32)
	h.DataSegmentAddress = address + headerSize

	// Pad strings buffer to full data segment size
	if uint64(len(h.strings)) < h.DataSegmentSize {
		padding := make([]byte, h.DataSegmentSize-uint64(len(h.strings)))
		h.strings = append(h.strings, padding...)
	}

	// Build header (32 bytes)
	header := make([]byte, headerSize)
	offset := 0

	// Signature: "HEAP" (4 bytes)
	copy(header[offset:offset+4], "HEAP")
	offset += 4

	// Version: 0 (1 byte)
	header[offset] = 0
	offset++

	// Reserved: 0 (3 bytes)
	header[offset] = 0
	header[offset+1] = 0
	header[offset+2] = 0
	offset += 3

	// Data segment size (8 bytes, little-endian)
	binary.LittleEndian.PutUint64(header[offset:offset+8], h.DataSegmentSize)
	offset += 8

	// Offset to head of free list (8 bytes, little-endian)
	// MVP: Always 1 (H5HL_FREE_NULL = no free list)
	binary.LittleEndian.PutUint64(header[offset:offset+8], h.OffsetToHeadFreeList)
	offset += 8

	// Data segment address (8 bytes, little-endian)
	binary.LittleEndian.PutUint64(header[offset:offset+8], h.DataSegmentAddress)

	// Write header
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	if _, err := w.WriteAt(header, int64(address)); err != nil {
		return utils.WrapError("failed to write local heap header", err)
	}

	// Write data segment (strings)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	if _, err := w.WriteAt(h.strings, int64(h.DataSegmentAddress)); err != nil {
		return utils.WrapError("failed to write local heap data", err)
	}

	return nil
}

// Size returns the total size of the local heap (header + data segment).
// This is used for space allocation before writing.
func (h *LocalHeap) Size() uint64 {
	// Header size (32 bytes) + data segment size
	return 32 + h.DataSegmentSize
}

// PrepareForModification converts a read-mode heap to write-mode.
// This allows adding new strings to an existing heap loaded from disk.
//
// This method copies the existing Data into the private strings buffer,
// enabling AddString() to append new entries.
//
// Offsets are relative to the start of the data segment. The strings buffer
// contains all data from the data segment, preserving existing offsets.
//
// CRITICAL: Must preserve null terminators after each string!
//
// Returns:
//   - error: If preparation fails
func (h *LocalHeap) PrepareForModification() error {
	if h.Data == nil {
		return errors.New("heap has no data to prepare")
	}

	// Find the actual used size (up to last non-zero byte, PLUS its null terminator)
	// We need to include the null terminator after the last string!
	usedSize := 0
	for i := len(h.Data) - 1; i >= 0; i-- {
		if h.Data[i] != 0 {
			// Found last non-zero byte
			// usedSize must include this byte AND its null terminator
			// Look ahead to find the null terminator after this string
			usedSize = i + 1

			// Find the null terminator after this byte
			for j := i + 1; j < len(h.Data); j++ {
				if h.Data[j] == 0 {
					usedSize = j + 1 // Include the null terminator
					break
				}
			}
			break
		}
	}

	// Copy existing data to strings buffer (preserving all content and offsets)
	// This MUST include all null terminators to maintain correct offsets
	h.strings = make([]byte, usedSize)
	copy(h.strings, h.Data[:usedSize])

	// Set data segment size (for capacity checking)
	if h.DataSegmentSize == 0 {
		h.DataSegmentSize = uint64(len(h.Data))
	}

	return nil
}
