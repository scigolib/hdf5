package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// LinkInfoMessage represents the Link Info message (HDF5 message type 0x0002).
// This message provides information about the storage of links in a group.
//
// The Link Info message is used in modern HDF5 groups (1.8+) to indicate:
//   - Whether creation order is tracked and/or indexed
//   - The maximum creation order value seen so far
//   - Addresses of the fractal heap and B-tree v2 structures for "dense" link storage
//
// Format:
//   - Version (1 byte): Always 0 for current spec
//   - Flags (1 byte): Bit 0 = track creation order, Bit 1 = index creation order
//   - Max Creation Order (8 bytes, optional): Present if bit 0 of flags is set
//   - Fractal Heap Address (offsetSize bytes): Address of fractal heap for link names
//   - Name B-tree v2 Address (offsetSize bytes): Address of B-tree for name index
//   - Creation Order B-tree v2 Address (offsetSize bytes, optional): Present if bit 1 of flags is set
//
// Reference: HDF5 Format Spec Section IV.A.2.g (Link Info Message).
// C Reference: H5Olinfo.c - H5O_linfo_t structure and encoding/decoding functions.
type LinkInfoMessage struct {
	Version uint8 // Message version (always 0 for now)
	Flags   uint8 // Bit 0: creation order tracked, Bit 1: creation order indexed

	// Max creation order value (present if bit 0 of flags is set)
	// This is the highest creation order value seen so far in the group
	MaxCreationOrder int64

	// Addresses for dense link storage (0 if not present)
	FractalHeapAddress        uint64 // Address of fractal heap for link name storage
	NameBTreeAddress          uint64 // Address of B-tree v2 for link name index
	CreationOrderBTreeAddress uint64 // Address of B-tree v2 for creation order index (optional)
}

// Flags for LinkInfoMessage.
const (
	LinkInfoTrackCreationOrder uint8 = 0x01 // Bit 0: track creation order
	LinkInfoIndexCreationOrder uint8 = 0x02 // Bit 1: index creation order
)

// HasCreationOrderTracking returns true if creation order is tracked.
func (lim *LinkInfoMessage) HasCreationOrderTracking() bool {
	return (lim.Flags & LinkInfoTrackCreationOrder) != 0
}

// HasCreationOrderIndex returns true if creation order is indexed.
func (lim *LinkInfoMessage) HasCreationOrderIndex() bool {
	return (lim.Flags & LinkInfoIndexCreationOrder) != 0
}

// HasFractalHeap returns true if fractal heap address is set.
func (lim *LinkInfoMessage) HasFractalHeap() bool {
	return lim.FractalHeapAddress != 0
}

// HasNameBTree returns true if name B-tree address is set.
func (lim *LinkInfoMessage) HasNameBTree() bool {
	return lim.NameBTreeAddress != 0
}

// HasCreationOrderBTree returns true if creation order B-tree address is set.
func (lim *LinkInfoMessage) HasCreationOrderBTree() bool {
	return lim.CreationOrderBTreeAddress != 0
}

// ParseLinkInfoMessage parses Link Info message from header message data.
//
// This implements the decoding logic matching the C reference H5Olinfo.c:H5O__linfo_decode().
//
// Format:
//   - Version (1 byte): Must be 0
//   - Flags (1 byte): Bit 0 = track creation order, Bit 1 = index creation order
//   - Max Creation Order (8 bytes, optional): Present if bit 0 of flags is set
//   - Fractal Heap Address (offsetSize bytes): Always present
//   - Name B-tree v2 Address (offsetSize bytes): Always present
//   - Creation Order B-tree v2 Address (offsetSize bytes, optional): Present if bit 1 of flags is set
//
// Reference: H5Olinfo.c - H5O__linfo_decode().
func ParseLinkInfoMessage(data []byte, sb *Superblock) (*LinkInfoMessage, error) {
	if len(data) < 2 {
		return nil, errors.New("link info message too short (need at least 2 bytes for version and flags)")
	}

	lim := &LinkInfoMessage{}
	offset := 0

	// Read version (byte 0)
	lim.Version = data[offset]
	offset++

	if lim.Version != 0 {
		return nil, fmt.Errorf("unsupported link info version: %d (only version 0 is supported)", lim.Version)
	}

	// Read flags (byte 1)
	lim.Flags = data[offset]
	offset++

	// Validate flags (only bits 0-1 are defined)
	const validFlagsMask = uint8(0x03) // Bits 0-1
	if lim.Flags & ^validFlagsMask != 0 {
		return nil, fmt.Errorf("invalid link info flags: 0x%02X (reserved bits set)", lim.Flags)
	}

	// Read max creation order (8 bytes, int64) if creation order is tracked
	if lim.HasCreationOrderTracking() {
		if len(data) < offset+8 {
			return nil, errors.New("link info message truncated (missing max creation order)")
		}
		// Safe conversion: HDF5 max_corder is always non-negative in valid files
		//nolint:gosec // G115: max_corder validation ensures value is non-negative
		lim.MaxCreationOrder = int64(binary.LittleEndian.Uint64(data[offset : offset+8]))
		offset += 8

		// Validate max creation order is non-negative
		if lim.MaxCreationOrder < 0 {
			return nil, fmt.Errorf("invalid max creation order value: %d (must be >= 0)", lim.MaxCreationOrder)
		}
	} else {
		lim.MaxCreationOrder = 0
	}

	// Read fractal heap address (always present)
	if len(data) < offset+int(sb.OffsetSize) {
		return nil, errors.New("link info message truncated (missing fractal heap address)")
	}
	lim.FractalHeapAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Read name B-tree address (always present)
	if len(data) < offset+int(sb.OffsetSize) {
		return nil, errors.New("link info message truncated (missing name B-tree address)")
	}
	lim.NameBTreeAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Read creation order B-tree address (optional, only if indexed)
	if lim.HasCreationOrderIndex() {
		if len(data) < offset+int(sb.OffsetSize) {
			return nil, errors.New("link info message truncated (missing creation order B-tree address)")
		}
		lim.CreationOrderBTreeAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
	} else {
		lim.CreationOrderBTreeAddress = 0
	}

	return lim, nil
}

// EncodeLinkInfoMessage encodes Link Info message for writing.
//
// This implements the encoding logic matching the C reference H5Olinfo.c:H5O__linfo_encode().
//
// Format:
//   - Version (1 byte): Always 0
//   - Flags (1 byte): Bit 0 = track creation order, Bit 1 = index creation order
//   - Max Creation Order (8 bytes, int64, optional): Present if bit 0 of flags is set
//   - Fractal Heap Address (offsetSize bytes): Always present
//   - Name B-tree Address (offsetSize bytes): Always present
//   - Creation Order B-tree Address (offsetSize bytes, optional): Present if bit 1 of flags is set
//
// Parameters:
//   - lim: Link Info message to encode
//   - sb: Superblock for offset size and endianness
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Reference: H5Olinfo.c - H5O__linfo_encode().
func EncodeLinkInfoMessage(lim *LinkInfoMessage, sb *Superblock) ([]byte, error) {
	if lim == nil {
		return nil, errors.New("link info message is nil")
	}

	// Validate version
	if lim.Version != 0 {
		return nil, fmt.Errorf("unsupported link info version: %d (only version 0 is supported)", lim.Version)
	}

	// Calculate message size
	size := 2 // Version (1) + Flags (1)

	// Add max creation order field if tracking
	if lim.HasCreationOrderTracking() {
		size += 8 // MaxCreationOrder (int64)
	}

	// Add heap and name B-tree addresses (always present)
	size += 2 * int(sb.OffsetSize) // FractalHeapAddress + NameBTreeAddress

	// Add creation order B-tree address if indexed
	if lim.HasCreationOrderIndex() {
		size += int(sb.OffsetSize) // CreationOrderBTreeAddress
	}

	buf := make([]byte, size)
	offset := 0

	// Write version (byte 0)
	buf[offset] = lim.Version
	offset++

	// Write flags (byte 1)
	buf[offset] = lim.Flags
	offset++

	// Write max creation order (8 bytes, int64) if tracked
	if lim.HasCreationOrderTracking() {
		// Safe conversion: max_corder is always non-negative (validated in ParseLinkInfoMessage)
		//nolint:gosec // G115: max_corder is validated to be non-negative
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(lim.MaxCreationOrder))
		offset += 8
	}

	// Write fractal heap address (always present)
	writeUint64(buf[offset:], lim.FractalHeapAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Write name B-tree address (always present)
	writeUint64(buf[offset:], lim.NameBTreeAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Write creation order B-tree address if indexed
	if lim.HasCreationOrderIndex() {
		writeUint64(buf[offset:], lim.CreationOrderBTreeAddress, int(sb.OffsetSize), sb.Endianness)
	}

	return buf, nil
}

// Note: readUint64 helper function is defined in datalayout.go
