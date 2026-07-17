package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// LinkType defines the type of link (hard, soft, external).
type LinkType uint8

// Link type constants from HDF5 specification.
const (
	LinkTypeHard     LinkType = 0  // Hard link: direct reference to object
	LinkTypeSoft     LinkType = 1  // Soft link: symbolic path to object
	LinkTypeExternal LinkType = 64 // External link: reference to object in another file
)

// LinkMessage represents a link message in an HDF5 file.
// Link messages are used in modern HDF5 groups (dense storage) to store
// information about links between objects.
//
// Format (HDF5 Spec Section IV.A.2.f):
//   - Version (1 byte): Always 1 for current spec
//   - Flags (1 byte): Link type and creation order tracking
//   - Link Type (1 byte, optional): Present if bit 3 of flags is set
//   - Creation Order (8 bytes, optional): Present if bit 2 of flags is set
//   - Link Name Character Set (1 byte): 0=ASCII, 1=UTF-8
//   - Link Name Length (1, 2, 4, or 8 bytes): Size of link name encoding depends on flags
//   - Link Name (variable): UTF-8 or ASCII encoded name
//   - Link Information (variable): Format depends on link type
//
// Reference: HDF5 Format Spec Section IV.A.2.f (Link Message).
// C Reference: H5Oint.c - H5O_link_t structure and encoding/decoding functions.
type LinkMessage struct {
	Version       uint8    // Message version (always 1 for now)
	Flags         uint8    // Link type and flags
	Type          LinkType // Link type (hard, soft, external)
	CreationOrder uint64   // Creation order value (optional)
	CharSet       uint8    // Character set encoding (0=ASCII, 1=UTF-8)
	Name          string   // Link name
	LinkValue     []byte   // Link-specific data (depends on type)
}

// Link message flags.
const (
	LinkFlagSizeOfLengthMask   uint8 = 0x03 // Bits 0-1: size of length field (0=1, 1=2, 2=4, 3=8 bytes)
	LinkFlagCreationOrderBit   uint8 = 0x04 // Bit 2: creation order field present
	LinkFlagLinkTypeFieldBit   uint8 = 0x08 // Bit 3: link type field present
	LinkFlagCharSetBit         uint8 = 0x10 // Bit 4: link name character set field present
	LinkFlagLinkNameEncodedBit uint8 = 0x18 // Bits 3-4: both must be set for encoded name
)

// HasCreationOrder returns true if creation order field is present.
func (lm *LinkMessage) HasCreationOrder() bool {
	return (lm.Flags & LinkFlagCreationOrderBit) != 0
}

// HasLinkTypeField returns true if link type field is present.
func (lm *LinkMessage) HasLinkTypeField() bool {
	return (lm.Flags & LinkFlagLinkTypeFieldBit) != 0
}

// HasCharSetField returns true if character set field is present.
func (lm *LinkMessage) HasCharSetField() bool {
	return (lm.Flags & LinkFlagCharSetBit) != 0
}

// GetLinkNameLengthSize returns the size of the link name length field (1, 2, 4, or 8 bytes).
func (lm *LinkMessage) GetLinkNameLengthSize() int {
	sizeCode := lm.Flags & LinkFlagSizeOfLengthMask
	switch sizeCode {
	case 0:
		return 1
	case 1:
		return 2
	case 2:
		return 4
	case 3:
		return 8
	default:
		return 1 // Should never happen
	}
}

// EncodeLinkMessage encodes a link message for writing.
//
// This implements the encoding logic matching the C reference H5Oint.c:H5O__link_encode().
//
// Format:
//   - Version (1 byte): Always 1
//   - Flags (1 byte): Link type and flags
//   - Link Type (1 byte, optional): If bit 3 of flags is set
//   - Creation Order (8 bytes, optional): If bit 2 of flags is set
//   - Link Name Character Set (1 byte, optional): If bit 4 of flags is set
//   - Link Name Length (1-8 bytes): Depends on flags bits 0-1
//   - Link Name (variable): UTF-8 or ASCII
//   - Link Information (variable): Depends on link type
//
// Parameters:
//   - lm: Link message to encode
//   - _ : Superblock (unused, kept for API consistency)
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Reference: H5Oint.c - H5O__link_encode().
func EncodeLinkMessage(lm *LinkMessage, _ *Superblock) ([]byte, error) {
	if lm == nil {
		return nil, errors.New("link message is nil")
	}

	// Validate version
	if lm.Version != 1 {
		return nil, fmt.Errorf("unsupported link message version: %d (only version 1 is supported)", lm.Version)
	}

	// Calculate message size
	size := 2 // Version (1) + Flags (1)

	// Add link type field if present
	if lm.HasLinkTypeField() {
		size++
	}

	// Add creation order field if present
	if lm.HasCreationOrder() {
		size += 8
	}

	// Add character set field if present
	if lm.HasCharSetField() {
		size++
	}

	// Add link name length field
	lengthSize := lm.GetLinkNameLengthSize()
	size += lengthSize

	// Add link name
	size += len(lm.Name)

	// Add link value (depends on type)
	size += len(lm.LinkValue)

	buf := make([]byte, size)
	offset := 0

	// Write version (byte 0)
	buf[offset] = lm.Version
	offset++

	// Write flags (byte 1)
	buf[offset] = lm.Flags
	offset++

	// Write link type if present
	if lm.HasLinkTypeField() {
		buf[offset] = uint8(lm.Type)
		offset++
	}

	// Write creation order if present
	if lm.HasCreationOrder() {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], lm.CreationOrder)
		offset += 8
	}

	// Write character set if present
	if lm.HasCharSetField() {
		buf[offset] = lm.CharSet
		offset++
	}

	// Write link name length
	nameLength := uint64(len(lm.Name))
	if err := writeLinkNameLength(buf, offset, nameLength, lengthSize); err != nil {
		return nil, err
	}
	offset += lengthSize

	// Write link name
	copy(buf[offset:], lm.Name)
	offset += len(lm.Name)

	// Write link value
	copy(buf[offset:], lm.LinkValue)

	return buf, nil
}

// writeLinkNameLength writes the link name length field with proper validation.
func writeLinkNameLength(buf []byte, offset int, nameLength uint64, lengthSize int) error {
	switch lengthSize {
	case 1:
		if nameLength > 255 {
			return fmt.Errorf("name length %d exceeds 1-byte maximum (255)", nameLength)
		}
		buf[offset] = uint8(nameLength)
	case 2:
		if nameLength > 65535 {
			return fmt.Errorf("name length %d exceeds 2-byte maximum (65535)", nameLength)
		}
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(nameLength))
	case 4:
		if nameLength > 4294967295 {
			return fmt.Errorf("name length %d exceeds 4-byte maximum (4294967295)", nameLength)
		}
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(nameLength))
	case 8:
		binary.LittleEndian.PutUint64(buf[offset:offset+8], nameLength)
	default:
		return fmt.Errorf("invalid length size: %d", lengthSize)
	}
	return nil
}
