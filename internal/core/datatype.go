package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DatatypeClass represents HDF5 datatype class.
type DatatypeClass uint8

// Datatype class constants identify different HDF5 data types for datasets.
const (
	DatatypeFixed     DatatypeClass = 0  // Fixed-point (integers).
	DatatypeFloat     DatatypeClass = 1  // Floating-point.
	DatatypeTime      DatatypeClass = 2  // Time.
	DatatypeString    DatatypeClass = 3  // String.
	DatatypeBitfield  DatatypeClass = 4  // Bitfield.
	DatatypeOpaque    DatatypeClass = 5  // Opaque.
	DatatypeCompound  DatatypeClass = 6  // Compound.
	DatatypeReference DatatypeClass = 7  // Reference.
	DatatypeEnum      DatatypeClass = 8  // Enumerated.
	DatatypeVarLen    DatatypeClass = 9  // Variable-length.
	DatatypeArray     DatatypeClass = 10 // Array.
	DatatypeComplex   DatatypeClass = 11 // Complex (HDF5 2.0+).
)

// DatatypeMessage represents HDF5 datatype message.
type DatatypeMessage struct {
	Class         DatatypeClass
	Version       uint8
	Size          uint32
	ClassBitField uint32
	Properties    []byte
}

// calculateCompoundPropsLen calculates the exact length of compound datatype properties.
// This is needed for inline parsing of nested compounds, where we can't just take "all remaining".
//
// Algorithm:
//  1. Read member count (4 bytes for v3, 2 bytes embedded in header for v1)
//  2. For each member:
//     - Skip name (null-terminated, padded to 8-byte boundary for v1)
//     - Skip offset field (4 bytes)
//     - Skip array info (28 bytes for v1, not present in v3)
//     - Recursively calculate member datatype size
//  3. Return total properties length
func calculateCompoundPropsLen(properties []byte, version uint8) (int, error) {
	// Version 1 or 2: member count is embedded in ClassBitField (not in properties)
	// This is complex, so for now return error to use fallback
	if version != 3 {
		return 0, errors.New("compound v1/v2 inline size calculation not yet implemented")
	}

	// Version 3: member count is first 4 bytes
	if len(properties) < 4 {
		return 0, errors.New("compound v3 properties too short for member count")
	}
	numMembers := binary.LittleEndian.Uint32(properties[0:4])
	offset := 4

	for i := uint32(0); i < numMembers; i++ {
		// Skip member name (null-terminated, NOT padded in v3)
		nameEnd := offset
		for nameEnd < len(properties) && properties[nameEnd] != 0 {
			nameEnd++
		}
		if nameEnd >= len(properties) {
			return 0, fmt.Errorf("member %d: name not null-terminated", i)
		}
		offset = nameEnd + 1 // Skip past null terminator

		// Skip member offset field (4 bytes)
		if offset+4 > len(properties) {
			return 0, fmt.Errorf("member %d: offset field truncated", i)
		}
		offset += 4

		// Parse member datatype to calculate its size
		if offset+8 > len(properties) {
			return 0, fmt.Errorf("member %d: datatype header truncated", i)
		}

		// Recursively calculate member datatype size
		//nolint:gosec // G602: bounds checked above (offset+8 <= len(properties))
		memberDt, err := ParseDatatypeMessage(properties[offset:])
		if err != nil {
			return 0, fmt.Errorf("member %d: failed to parse datatype: %w", i, err)
		}
		memberDtSize := 8 + len(memberDt.Properties)
		offset += memberDtSize
	}

	return offset, nil
}

// ParseDatatypeMessage parses a datatype message from header message data.
func ParseDatatypeMessage(data []byte) (*DatatypeMessage, error) {
	if len(data) < 8 {
		return nil, errors.New("datatype message too short")
	}

	// Bytes 0-3: Class and Version packed.
	classAndVersion := binary.LittleEndian.Uint32(data[0:4])

	//nolint:gosec // G115: HDF5 binary format unpacking
	class := DatatypeClass(classAndVersion & 0x0F)
	//nolint:gosec // G115: HDF5 binary format unpacking
	version := uint8((classAndVersion >> 4) & 0x0F)
	classBitField := (classAndVersion >> 8) & 0x00FFFFFF

	// Bytes 4-7: Size.
	size := binary.LittleEndian.Uint32(data[4:8])

	// Calculate property size based on class
	// This is needed for inline parsing (e.g., compound members)
	var propsLen int
	switch class {
	case DatatypeFixed: // Integer
		propsLen = 4 // bit offset + precision
	case DatatypeFloat:
		propsLen = 12 // full IEEE 754 info
	case DatatypeBitfield:
		propsLen = 4
	case DatatypeTime:
		propsLen = 2
	case DatatypeString:
		// String properties are variable, but typically minimal
		// For now, take all remaining (safe for top-level parsing)
		propsLen = len(data) - 8
	case DatatypeCompound:
		// Compound types: properties are variable length and self-describing
		// For inline parsing (nested compounds), we must calculate the exact size
		// by walking through the member definitions
		calculatedLen, err := calculateCompoundPropsLen(data[8:], version)
		if err != nil {
			// Fallback: take all remaining (for backward compatibility)
			propsLen = len(data) - 8
		} else {
			propsLen = calculatedLen
		}
	case DatatypeArray, DatatypeEnum, DatatypeReference, DatatypeOpaque, DatatypeVarLen:
		// Complex types: properties are variable length
		// For inline parsing, take all remaining
		propsLen = len(data) - 8
	default:
		// Unknown type: take all remaining
		propsLen = len(data) - 8
	}

	// Ensure we don't read past end of data
	if 8+propsLen > len(data) {
		propsLen = len(data) - 8
	}

	//nolint:gosec // G602: bounds checked above (8+propsLen <= len(data))
	return &DatatypeMessage{
		Class:         class,
		Version:       version,
		Size:          size,
		ClassBitField: classBitField,
		Properties:    data[8 : 8+propsLen],
	}, nil
}

// IsFloat64 checks if datatype is IEEE 754 double precision (64-bit).
func (dt *DatatypeMessage) IsFloat64() bool {
	return dt.Class == DatatypeFloat && dt.Size == 8
}

// IsFloat32 checks if datatype is IEEE 754 single precision (32-bit).
func (dt *DatatypeMessage) IsFloat32() bool {
	return dt.Class == DatatypeFloat && dt.Size == 4
}

// IsInt32 checks if datatype is 32-bit signed integer.
func (dt *DatatypeMessage) IsInt32() bool {
	return dt.Class == DatatypeFixed && dt.Size == 4
}

// IsInt64 checks if datatype is 64-bit signed integer.
func (dt *DatatypeMessage) IsInt64() bool {
	return dt.Class == DatatypeFixed && dt.Size == 8
}

// IsString checks if datatype is a string type.
func (dt *DatatypeMessage) IsString() bool {
	return dt.Class == DatatypeString
}

// IsFixedString checks if datatype is a fixed-length string.
func (dt *DatatypeMessage) IsFixedString() bool {
	if dt.Class != DatatypeString {
		return false
	}
	// Fixed-length strings have explicit Size.
	// Variable-length strings would have Size = 0 or use DatatypeVarLen class.
	return dt.Size > 0
}

// IsVariableString checks if datatype is a variable-length string.
// Reference: HDF5 Format Specification III.A.2.4.d (Variable-Length Types).
func (dt *DatatypeMessage) IsVariableString() bool {
	if dt.Class == DatatypeVarLen {
		// For variable-length types, ClassBitField contains:
		// - Bits 0-3: Type (0=Sequence, 1=String)
		// - Bits 4-7: Padding type (for strings)
		// - Bits 8-11: Character set (for strings, 0=ASCII, 1=UTF-8)
		vlType := dt.ClassBitField & 0x0F
		return vlType == 1 // 1 = variable-length string
	}
	return false
}

// IsCompound checks if datatype is a compound type (struct).
func (dt *DatatypeMessage) IsCompound() bool {
	return dt.Class == DatatypeCompound
}

// GetStringPadding returns the string padding type.
// 0 = null-terminated, 1 = null-padded, 2 = space-padded.
func (dt *DatatypeMessage) GetStringPadding() uint8 {
	//nolint:gosec // G115: HDF5 binary format bitfield extraction
	return uint8(dt.ClassBitField & 0x0F)
}

// String returns human-readable datatype description.
func (dt *DatatypeMessage) String() string {
	var className string
	switch dt.Class {
	case DatatypeFixed:
		className = "integer"
	case DatatypeFloat:
		className = "float"
	case DatatypeString:
		className = "string"
	case DatatypeCompound:
		className = "compound"
	case DatatypeArray:
		className = "array"
	default:
		className = fmt.Sprintf("class_%d", dt.Class)
	}

	return fmt.Sprintf("%s (size=%d bytes)", className, dt.Size)
}

// GetByteOrder returns byte order for numeric types.
func (dt *DatatypeMessage) GetByteOrder() binary.ByteOrder {
	// Bit 0 of class bit field indicates byte order for numeric types.
	// 0 = little-endian, 1 = big-endian.
	if dt.ClassBitField&0x01 == 0 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// GetEncodedSize returns the total size of this datatype message when encoded.
// This includes the 8-byte header plus properties.
// Property sizes from HDF5 spec (H5Odtype.c:1630):
// - Integer: 4 bytes (offset + precision).
// - Float: 12 bytes (byte order, padding, mantissa, exponent info).
// - Bitfield: 4 bytes (offset + precision).
// - Time: 2 bytes.
// - String: variable (character set + padding type).
// - Compound: variable (member definitions).
func (dt *DatatypeMessage) GetEncodedSize() int {
	switch dt.Class {
	case DatatypeFixed: // Integer.
		// 8-byte header + 4 bytes properties (bit offset + precision).
		return 12
	case DatatypeFloat:
		// 8-byte header + 12 bytes properties (byte orders, padding, exponents, etc).
		return 20
	case DatatypeBitfield:
		// 8-byte header + 4 bytes properties (bit offset + precision).
		return 12
	case DatatypeTime:
		// 8-byte header + 2 bytes properties.
		return 10
	case DatatypeString:
		// String properties are minimal, usually just 8-byte header.
		// but can have padding/charset info.
		return 8 + len(dt.Properties)
	case DatatypeCompound:
		// Compound types: 8-byte header + all member definitions.
		return 8 + len(dt.Properties)
	default:
		// For other types, use actual properties length.
		return 8 + len(dt.Properties)
	}
}
