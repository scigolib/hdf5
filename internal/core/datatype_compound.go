package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// CompoundMember represents a single field in a compound datatype.
type CompoundMember struct {
	Name   string           // Field name.
	Offset uint32           // Byte offset within the compound structure.
	Type   *DatatypeMessage // Member datatype (can be any type, including nested compound).
}

// CompoundType represents a parsed compound datatype with all its members.
type CompoundType struct {
	Size    uint32           // Total size of the compound structure in bytes.
	Members []CompoundMember // List of members/fields.
}

// ParseCompoundType parses compound datatype properties to extract member information.
// Properties format (version 1):
// - Bytes 0-1: Number of members (uint16).
// - For each member:
//   - Byte offset in structure (uint32 for version 1).
//   - Dimensionality (1 byte, usually 0 for scalar).
//   - Reserved bytes (3 bytes for version 1).
//   - Member name (null-terminated string, padded to multiple of 8 bytes).
//   - Member datatype (recursive datatype message, 8+ bytes).
func ParseCompoundType(dt *DatatypeMessage) (*CompoundType, error) {
	if dt.Class != DatatypeCompound {
		return nil, errors.New("not a compound datatype")
	}

	if len(dt.Properties) < 2 {
		return nil, errors.New("compound properties too short")
	}

	compound := &CompoundType{
		Size:    dt.Size,
		Members: make([]CompoundMember, 0),
	}

	// Parse based on version.
	switch dt.Version {
	case 1:
		// For version 1, number of members is in ClassBitField bits 0-15.
		//nolint:gosec // G115: HDF5 binary format bitfield extraction
		numMembers := uint16(dt.ClassBitField & 0xFFFF)
		return parseCompoundV1(compound, dt.Properties, numMembers)
	case 3:
		return parseCompoundV3(compound, dt.Properties)
	default:
		return nil, fmt.Errorf("unsupported compound datatype version: %d", dt.Version)
	}
}

// parseCompoundV1 parses version 1 compound datatype properties.
// Format per member (H5Odtype.c:360-481):
//  1. Name (null-terminated, padded to 8-byte boundary).
//  2. Offset (uint32, 4 bytes).
//  3. Array info (28 bytes total):
//     - Dimensionality (1 byte).
//     - Reserved (3 bytes).
//     - Dimension permutation (4 bytes).
//     - Reserved (4 bytes).
//     - Dimension sizes (4 × uint32 = 16 bytes).
//  4. Member datatype (recursive, NO padding between members).
func parseCompoundV1(compound *CompoundType, properties []byte, numMembers uint16) (*CompoundType, error) {
	offset := 0

	for i := uint16(0); i < numMembers; i++ {
		member := CompoundMember{}

		// 1. Member name (null-terminated, padded to multiple of 8 bytes).
		nameStart := offset
		nameEnd := nameStart
		for nameEnd < len(properties) && properties[nameEnd] != 0 {
			nameEnd++
		}

		if nameEnd >= len(properties) {
			return nil, fmt.Errorf("member %d name not null-terminated", i)
		}

		member.Name = string(properties[nameStart:nameEnd])
		nameLen := nameEnd - nameStart

		// Advance to 8-byte boundary: ((nameLen + 8) / 8) * 8.
		offset = nameStart + ((nameLen+8)/8)*8

		// 2. Member byte offset (uint32).
		if offset+4 > len(properties) {
			return nil, fmt.Errorf("member %d: offset field truncated", i)
		}
		member.Offset = binary.LittleEndian.Uint32(properties[offset : offset+4])
		offset += 4

		// 3. Array info (always 28 bytes for version 1, even for scalar members).
		if offset+28 > len(properties) {
			return nil, fmt.Errorf("member %d: array info truncated", i)
		}
		// Skip array info (we don't support array members yet).
		offset += 28

		// 4. Member datatype (recursive parse).
		if offset+8 > len(properties) {
			return nil, fmt.Errorf("member %d: datatype header truncated", i)
		}

		memberType, err := ParseDatatypeMessage(properties[offset:])
		if err != nil {
			return nil, fmt.Errorf("member %d (%s): failed to parse datatype: %w", i, member.Name, err)
		}
		member.Type = memberType

		// Advance past member datatype (no padding between members).
		offset += memberType.GetEncodedSize()

		compound.Members = append(compound.Members, member)
	}

	return compound, nil
}

// parseCompoundV3 parses version 3 compound datatype properties.
func parseCompoundV3(compound *CompoundType, properties []byte) (*CompoundType, error) {
	// Version 3 uses uint32 for member count.
	if len(properties) < 4 {
		return nil, errors.New("compound v3 properties too short")
	}

	numMembers := binary.LittleEndian.Uint32(properties[0:4])
	offset := 4

	for i := uint32(0); i < numMembers; i++ {
		if offset+8 > len(properties) {
			return nil, errors.New("compound v3 properties truncated")
		}

		member := CompoundMember{}

		// Member name (null-terminated, NOT padded in version 3).
		nameStart := offset
		nameEnd := nameStart
		for nameEnd < len(properties) && properties[nameEnd] != 0 {
			nameEnd++
		}

		if nameEnd >= len(properties) {
			return nil, errors.New("member name not null-terminated")
		}

		member.Name = string(properties[nameStart:nameEnd])
		offset = nameEnd + 1

		// Member byte offset (uint32).
		if offset+4 > len(properties) {
			return nil, errors.New("compound v3 member offset truncated")
		}
		member.Offset = binary.LittleEndian.Uint32(properties[offset : offset+4])
		offset += 4

		// Member datatype (recursive parse).
		if offset+8 > len(properties) {
			return nil, fmt.Errorf("compound v3 properties truncated (member %d datatype)", i)
		}

		memberType, err := ParseDatatypeMessage(properties[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse member %d datatype: %w", i, err)
		}
		member.Type = memberType

		// Advance past member datatype.
		memberTypeSize := 8 + len(memberType.Properties)
		offset += memberTypeSize

		compound.Members = append(compound.Members, member)
	}

	return compound, nil
}

// String returns human-readable compound type description.
func (ct *CompoundType) String() string {
	result := fmt.Sprintf("compound{size=%d, members=[", ct.Size)
	for i, member := range ct.Members {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s:%s@%d", member.Name, member.Type.String(), member.Offset)
	}
	result += "]}"
	return result
}
