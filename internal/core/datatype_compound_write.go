package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// CompoundFieldDef defines a field for compound datatype creation.
// This is used when creating new compound datatypes for writing.
type CompoundFieldDef struct {
	Name   string           // Field name (null-terminated in encoding).
	Offset uint32           // Byte offset within compound structure.
	Type   *DatatypeMessage // Field datatype (can be nested compound).
}

// EncodeCompoundDatatypeV3 encodes a version 3 compound datatype message.
// This is the preferred format for new files (HDF5 1.8+).
//
// Format (version 3):
//   - Header (8 bytes):
//   - Byte 0-3: Class (4 bits) | Version (4 bits) | Reserved (24 bits)
//   - Byte 4-7: Size (total compound size in bytes)
//   - Member count (4 bytes): Number of fields
//   - For each member:
//   - Name (null-terminated, NOT padded)
//   - Offset (4 bytes, uint32)
//   - Member datatype (recursive, variable length)
//
// Parameters:
//   - totalSize: Total size of compound structure in bytes
//   - fields: List of field definitions with names, offsets, and types
//
// Returns:
//   - Encoded datatype message bytes
//   - Error if encoding fails
//
// Reference: HDF5 Format Spec III.C, H5Odtype.c:1630-1800.
func EncodeCompoundDatatypeV3(totalSize uint32, fields []CompoundFieldDef) ([]byte, error) {
	if len(fields) == 0 {
		return nil, errors.New("compound datatype must have at least one field")
	}

	if totalSize == 0 {
		return nil, errors.New("compound datatype size cannot be 0")
	}

	// Validate field count fits in uint32 (version 3 uses uint32)
	if len(fields) > 0xFFFFFFFF {
		return nil, fmt.Errorf("too many fields: %d (max: %d)", len(fields), 0xFFFFFFFF)
	}

	// Calculate properties size (all member definitions)
	propsSize := 4 // Member count (uint32)

	for i, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("field %d: name cannot be empty", i)
		}
		if field.Type == nil {
			return nil, fmt.Errorf("field %d (%s): type cannot be nil", i, field.Name)
		}

		// Name (null-terminated, NOT padded in version 3)
		propsSize += len(field.Name) + 1 // +1 for null terminator

		// Offset (4 bytes)
		propsSize += 4

		// Member datatype (inline encoding: header + properties)
		propsSize += 8                          // Header (8 bytes always)
		propsSize += len(field.Type.Properties) // Properties (variable)
	}

	// Allocate buffer: header (8 bytes) + properties
	buf := make([]byte, 8+propsSize)
	offset := 0

	// Encode header (8 bytes)
	// Byte 0-3: Class (low 4 bits) | Version (next 4 bits) | Reserved (high 24 bits)
	version := uint8(3)
	classAndVersion := uint32(DatatypeCompound) | (uint32(version) << 4)
	binary.LittleEndian.PutUint32(buf[offset:], classAndVersion)
	offset += 4

	// Byte 4-7: Total compound size
	binary.LittleEndian.PutUint32(buf[offset:], totalSize)
	offset += 4

	// Encode properties: member count + member definitions
	// Member count (4 bytes, uint32 for version 3)
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(fields))) //nolint:gosec // G115: validated above
	offset += 4

	// Encode each member
	for _, field := range fields {
		// 1. Member name (null-terminated, NOT padded in version 3)
		copy(buf[offset:], field.Name)
		offset += len(field.Name)
		buf[offset] = 0 // Null terminator
		offset++

		// 2. Member byte offset (4 bytes, uint32)
		binary.LittleEndian.PutUint32(buf[offset:], field.Offset)
		offset += 4

		// 3. Member datatype (inline encoding - just header + properties)
		// The member datatype is encoded inline (not through EncodeDatatypeMessage recursion)
		// Format: header (8 bytes) + properties

		// Encode member datatype header (8 bytes)
		memberClassAndVersion := uint32(field.Type.Class) | (uint32(field.Type.Version) << 4) | (field.Type.ClassBitField << 8)
		binary.LittleEndian.PutUint32(buf[offset:], memberClassAndVersion)
		offset += 4

		binary.LittleEndian.PutUint32(buf[offset:], field.Type.Size)
		offset += 4

		// Encode member datatype properties
		// Properties must be pre-populated in field.Type.Properties
		copy(buf[offset:], field.Type.Properties)
		offset += len(field.Type.Properties)
	}

	// Validate we used exactly the expected space
	if offset != len(buf) {
		return nil, fmt.Errorf("internal error: buffer size mismatch (expected %d, used %d)", len(buf), offset)
	}

	return buf, nil
}

// EncodeCompoundDatatypeV1 encodes a version 1 compound datatype message.
// This is the legacy format for HDF5 1.0-1.6 compatibility.
//
// Format (version 1):
//   - Header (8 bytes):
//   - Byte 0-3: Class (4 bits) | Version (4 bits) | NumMembers (16 bits, low)
//   - Byte 4-7: Size (total compound size in bytes)
//   - For each member:
//   - Name (null-terminated, padded to 8-byte boundary)
//   - Offset (4 bytes, uint32)
//   - Array info (28 bytes, always present even for scalars)
//   - Member datatype (recursive, variable length, NO padding between members)
//
// Parameters:
//   - totalSize: Total size of compound structure in bytes
//   - fields: List of field definitions
//
// Returns:
//   - Encoded datatype message bytes
//   - Error if encoding fails
//
// Reference: H5Odtype.c:360-481.
func EncodeCompoundDatatypeV1(totalSize uint32, fields []CompoundFieldDef) ([]byte, error) {
	if len(fields) == 0 {
		return nil, errors.New("compound datatype must have at least one field")
	}

	if totalSize == 0 {
		return nil, errors.New("compound datatype size cannot be 0")
	}

	// Validate field count fits in uint16 (version 1 uses 16 bits in ClassBitField)
	if len(fields) > 0xFFFF {
		return nil, fmt.Errorf("too many fields for version 1: %d (max: 65535)", len(fields))
	}

	// Calculate properties size
	propsSize := 0

	for i, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("field %d: name cannot be empty", i)
		}
		if field.Type == nil {
			return nil, fmt.Errorf("field %d (%s): type cannot be nil", i, field.Name)
		}

		// Name (null-terminated, padded to 8-byte boundary)
		nameLen := len(field.Name)
		paddedNameLen := ((nameLen + 8) / 8) * 8 // Round up to nearest 8 bytes
		propsSize += paddedNameLen

		// Offset (4 bytes)
		propsSize += 4

		// Array info (28 bytes, always present in version 1)
		propsSize += 28

		// Member datatype (inline encoding: header + properties, NO padding)
		propsSize += 8                          // Header (8 bytes always)
		propsSize += len(field.Type.Properties) // Properties (variable)
	}

	// Allocate buffer: header (8 bytes) + properties
	buf := make([]byte, 8+propsSize)
	offset := 0

	// Encode header (8 bytes)
	// Byte 0-3: Class (4 bits) | Version (4 bits) | NumMembers (16 bits low) | Reserved (8 bits)
	version := uint8(1)
	numMembers := uint16(len(fields)) //nolint:gosec // G115: validated above
	classAndVersion := uint32(DatatypeCompound) | (uint32(version) << 4) | (uint32(numMembers) << 8)
	binary.LittleEndian.PutUint32(buf[offset:], classAndVersion)
	offset += 4

	// Byte 4-7: Total compound size
	binary.LittleEndian.PutUint32(buf[offset:], totalSize)
	offset += 4

	// Encode each member
	for _, field := range fields {
		// 1. Member name (null-terminated, padded to 8-byte boundary)
		nameStart := offset
		copy(buf[offset:], field.Name)
		offset += len(field.Name)
		buf[offset] = 0 // Null terminator
		// offset++ is not needed here - will be recalculated below

		// Pad to 8-byte boundary
		nameLen := len(field.Name)
		paddedLen := ((nameLen + 8) / 8) * 8
		offset = nameStart + paddedLen

		// 2. Member byte offset (4 bytes, uint32)
		binary.LittleEndian.PutUint32(buf[offset:], field.Offset)
		offset += 4

		// 3. Array info (28 bytes, always zeros for scalar members in MVP)
		// Format: dimensionality (1) + reserved (3) + permutation (4) + reserved (4) + dimensions (16)
		// We support only scalar members for now, so all zeros
		offset += 28 // Skip, already zeros

		// 4. Member datatype (inline encoding, NO padding)
		// Same as V3: encode header + properties inline

		// Encode member datatype header (8 bytes)
		memberClassAndVersion := uint32(field.Type.Class) | (uint32(field.Type.Version) << 4) | (field.Type.ClassBitField << 8)
		binary.LittleEndian.PutUint32(buf[offset:], memberClassAndVersion)
		offset += 4

		binary.LittleEndian.PutUint32(buf[offset:], field.Type.Size)
		offset += 4

		// Encode member datatype properties
		copy(buf[offset:], field.Type.Properties)
		offset += len(field.Type.Properties)
	}

	// Validate we used exactly the expected space
	if offset != len(buf) {
		return nil, fmt.Errorf("internal error: buffer size mismatch (expected %d, used %d)", len(buf), offset)
	}

	return buf, nil
}

// CreateCompoundTypeFromFields creates a DatatypeMessage for a compound type.
// This is a convenience function for creating compound datatypes with automatic
// offset calculation.
//
// Parameters:
//   - fields: List of field definitions (offsets will be calculated)
//
// Returns:
//   - DatatypeMessage ready for writing
//   - Error if creation fails.
func CreateCompoundTypeFromFields(fields []CompoundFieldDef) (*DatatypeMessage, error) {
	if len(fields) == 0 {
		return nil, errors.New("compound type must have at least one field")
	}

	// Validate offsets are correct and calculate total size
	currentOffset := uint32(0)
	for i, field := range fields {
		if field.Offset != currentOffset {
			return nil, fmt.Errorf("field %d (%s): offset mismatch (expected %d, got %d)", i, field.Name, currentOffset, field.Offset)
		}
		currentOffset += field.Type.Size
	}

	totalSize := currentOffset

	// Encode as version 3 (modern format)
	encoded, err := EncodeCompoundDatatypeV3(totalSize, fields)
	if err != nil {
		return nil, fmt.Errorf("failed to encode compound type: %w", err)
	}

	// Parse back to get DatatypeMessage (includes validation)
	dt, err := ParseDatatypeMessage(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to parse encoded compound type: %w", err)
	}

	return dt, nil
}

// CreateBasicDatatypeMessage creates a simple datatype message for basic types.
// This is a helper for creating member types in compound datatypes.
//
// For integer types, properties are 4 bytes (bit offset + precision).
// For float types, properties are 12 bytes (full IEEE 754 info).
// For string types, properties are minimal (1 byte for padding/charset).
func CreateBasicDatatypeMessage(class DatatypeClass, size uint32) (*DatatypeMessage, error) {
	version := uint8(1)
	var properties []byte

	switch class {
	case DatatypeFixed:
		// Integer: 4 bytes properties
		properties = make([]byte, 4)
		properties[0] = 0              // Byte order: 0=little-endian
		properties[1] = byte(size * 8) // Precision in bits
		properties[2] = 0              // Offset
		properties[3] = 0              // Padding

	case DatatypeFloat:
		// Float: 12 bytes properties
		properties = make([]byte, 12)
		properties[0] = 0              // Byte order: 0=little-endian
		properties[1] = byte(size * 8) // Precision in bits
		properties[2] = 0              // Offset
		// Rest: exponent/mantissa info (simplified for now)

	case DatatypeString:
		// String: 1 byte properties (padding/charset)
		properties = []byte{0} // Null-terminated ASCII

	default:
		return nil, fmt.Errorf("unsupported datatype class: %d", class)
	}

	return &DatatypeMessage{
		Class:         class,
		Version:       version,
		Size:          size,
		ClassBitField: 0, // Little-endian, no special flags
		Properties:    properties,
	}, nil
}
