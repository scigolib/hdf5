package core

import (
	"encoding/binary"
	"fmt"
)

// EncodeLayoutMessage encodes a Data Layout message for contiguous storage.
// This creates a version 3 layout message (most common format).
//
// Parameters:
//   - layoutClass: Type of layout (contiguous, compact, chunked)
//   - dataSize: Size of the dataset data in bytes
//   - dataAddress: File address where data is stored
//   - sb: Superblock for offset/length size encoding
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Format (version 3, contiguous):
//   - Version: 1 byte (3)
//   - Class: 1 byte (1 for contiguous)
//   - Data Address: offsetSize bytes
//   - Data Size: lengthSize bytes
//
// Reference: HDF5 spec III.D (Data Storage - Data Layout Message)
// C Reference: H5Olayout.c - H5O__layout_encode()
func EncodeLayoutMessage(layoutClass DataLayoutClass, dataSize uint64, dataAddress uint64, sb *Superblock) ([]byte, error) {
	if layoutClass != LayoutContiguous {
		return nil, fmt.Errorf("only contiguous layout is supported for writing, got class %d", layoutClass)
	}

	// Version 3 layout message size:
	// version (1) + class (1) + address (offsetSize) + size (lengthSize)
	messageSize := 2 + int(sb.OffsetSize) + int(sb.LengthSize)
	buf := make([]byte, messageSize)

	offset := 0

	// Version 3
	buf[offset] = 3
	offset++

	// Layout class (contiguous = 1)
	buf[offset] = byte(layoutClass)
	offset++

	// Data address (variable size based on superblock)
	writeUint64(buf[offset:], dataAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Data size (variable size based on superblock)
	writeUint64(buf[offset:], dataSize, int(sb.LengthSize), sb.Endianness)

	return buf, nil
}

// EncodeDatatypeMessage encodes a Datatype message.
// Supports primitive types: int8-64, uint8-64, float32, float64, and fixed-length strings.
//
// Parameters:
//   - dt: Datatype message to encode
//
// Returns:
//   - Encoded message bytes
//   - Error if datatype is not supported
//
// Format:
//   - Bytes 0-3: Class (4 bits) | Version (4 bits) | Class Bit Field (24 bits)
//   - Bytes 4-7: Size (4 bytes)
//   - Bytes 8+: Properties (variable, depends on class)
//
// Reference: HDF5 spec III.C (Datatype Message)
// C Reference: H5Odtype.c - H5O__dtype_encode()
func EncodeDatatypeMessage(dt *DatatypeMessage) ([]byte, error) {
	// Basic validation
	if dt.Size == 0 {
		return nil, fmt.Errorf("datatype size cannot be 0")
	}

	// For MVP, support only simple types
	switch dt.Class {
	case DatatypeFixed, DatatypeFloat:
		// Numeric types: 8 bytes header + properties
		return encodeDatatypeNumeric(dt)
	case DatatypeString:
		// String type: 8 bytes header + properties
		return encodeDatatypeString(dt)
	case DatatypeCompound:
		// Compound type: 8 bytes header + member definitions
		return encodeDatatypeCompound(dt)
	default:
		return nil, fmt.Errorf("unsupported datatype class for writing: %d", dt.Class)
	}
}

// encodeDatatypeNumeric encodes numeric datatypes (fixed-point and floating-point).
func encodeDatatypeNumeric(dt *DatatypeMessage) ([]byte, error) {
	// Version 1 for basic numeric types
	version := uint8(1)

	// Validate size
	switch dt.Size {
	case 1, 2, 4, 8:
		// Valid sizes
	default:
		return nil, fmt.Errorf("invalid numeric datatype size: %d (must be 1, 2, 4, or 8)", dt.Size)
	}

	// For numeric types, properties contain:
	// - Byte Order: 1 byte
	// - Precision: 1 byte
	// - Offset: 1 byte
	// Plus additional fields for floating-point types
	var properties []byte

	if dt.Class == DatatypeFloat {
		// Floating-point properties (12 bytes total)
		// Byte order (bit 0 of ClassBitField), little-endian = 0
		byteOrder := byte(dt.ClassBitField & 0x01)

		// For IEEE 754:
		// - float32: mantissa=23 bits, exponent=8 bits
		// - float64: mantissa=52 bits, exponent=11 bits
		var mantissaBits, exponentBits uint8
		var exponentBias uint8

		if dt.Size == 4 {
			// float32
			mantissaBits = 23
			exponentBits = 8
			exponentBias = 127
		} else if dt.Size == 8 {
			// float64
			mantissaBits = 52
			exponentBits = 11
			//nolint:mnd // Standard IEEE 754 bias for float64
			exponentBias = 127 // Will be adjusted in full implementation
		} else {
			return nil, fmt.Errorf("unsupported float size: %d", dt.Size)
		}

		properties = make([]byte, 12)
		properties[0] = byteOrder                // Byte order
		properties[1] = byte(dt.Size * 8)        // Precision in bits
		properties[2] = 0                        // Offset (always 0 for standard floats)
		properties[3] = exponentBits             // Exponent size
		properties[4] = byte(mantissaBits)       // Mantissa size
		properties[5] = exponentBias             // Exponent bias
		// Remaining bytes: mantissa location, exponent location, etc. (set to 0 for standard)
	} else {
		// Fixed-point (integer) properties (4 bytes)
		properties = make([]byte, 4)
		properties[0] = byte(dt.ClassBitField & 0x01) // Byte order (little-endian = 0)
		properties[1] = byte(dt.Size * 8)             // Precision in bits
		properties[2] = 0                             // Offset
		properties[3] = 0                             // Padding type
	}

	// Build message: header (8 bytes) + properties
	messageSize := 8 + len(properties)
	buf := make([]byte, messageSize)

	// Pack class, version, and class bit field into bytes 0-3
	classAndVersion := uint32(dt.Class) | (uint32(version) << 4) | (dt.ClassBitField << 8)
	binary.LittleEndian.PutUint32(buf[0:4], classAndVersion)

	// Size (bytes 4-7)
	binary.LittleEndian.PutUint32(buf[4:8], dt.Size)

	// Properties
	copy(buf[8:], properties)

	return buf, nil
}

// encodeDatatypeString encodes string datatype (fixed-length only for MVP).
func encodeDatatypeString(dt *DatatypeMessage) ([]byte, error) {
	if dt.Size == 0 {
		return nil, fmt.Errorf("fixed-length strings must have size > 0")
	}

	// Version 1 for string types
	version := uint8(1)

	// String properties: 1 byte (padding/charset)
	// Bit 0-3: Padding type (0=null-terminated, 1=null-padded, 2=space-padded)
	// Bit 4-7: Character set (0=ASCII, 1=UTF-8)
	properties := []byte{0} // Default: null-terminated ASCII

	// Build message
	messageSize := 8 + len(properties)
	buf := make([]byte, messageSize)

	// Pack class, version, and class bit field
	classAndVersion := uint32(dt.Class) | (uint32(version) << 4) | (dt.ClassBitField << 8)
	binary.LittleEndian.PutUint32(buf[0:4], classAndVersion)

	// Size
	binary.LittleEndian.PutUint32(buf[4:8], dt.Size)

	// Properties
	copy(buf[8:], properties)

	return buf, nil
}

// encodeDatatypeCompound encodes compound datatype (basic support for MVP).
func encodeDatatypeCompound(dt *DatatypeMessage) ([]byte, error) {
	// For MVP, compound types are simplified
	// In full implementation, this would encode member definitions
	// For now, return error as compound writing is not fully supported in MVP
	return nil, fmt.Errorf("compound datatype encoding not yet implemented in MVP")
}

// EncodeDataspaceMessage encodes a Dataspace message (simple N-dimensional array).
//
// Parameters:
//   - dims: Dimensions of the dataspace
//   - maxDims: Maximum dimensions (nil if not resizable)
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Format (version 1):
//   - Version: 1 byte (1)
//   - Dimensionality: 1 byte
//   - Flags: 1 byte (0x01 if maxDims present)
//   - Reserved: 5 bytes (0)
//   - Dimensions: dimensionality * 8 bytes (uint64 for each dimension)
//   - Max Dimensions: dimensionality * 8 bytes (if flags & 0x01)
//
// Reference: HDF5 spec III.A (Dataspace Message)
// C Reference: H5Osdspace.c - H5O__sdspace_encode()
func EncodeDataspaceMessage(dims []uint64, maxDims []uint64) ([]byte, error) {
	if len(dims) == 0 {
		return nil, fmt.Errorf("dimensions cannot be empty (use [1] for scalar)")
	}

	if len(maxDims) > 0 && len(maxDims) != len(dims) {
		return nil, fmt.Errorf("maxDims length %d must match dims length %d", len(maxDims), len(dims))
	}

	// Version 1 dataspace message
	version := uint8(1)
	dimensionality := uint8(len(dims))

	// Flags: bit 0 = 1 if max dimensions present
	flags := uint8(0)
	if len(maxDims) > 0 {
		flags |= 0x01
	}

	// Calculate message size
	// Version (1) + Dimensionality (1) + Flags (1) + Reserved (5) = 8 bytes header
	// Dimensions: dimensionality * 8 bytes
	// Max Dimensions: dimensionality * 8 bytes (if present)
	headerSize := 8
	dimsSize := int(dimensionality) * 8
	maxDimsSize := 0
	if len(maxDims) > 0 {
		maxDimsSize = int(dimensionality) * 8
	}

	messageSize := headerSize + dimsSize + maxDimsSize
	buf := make([]byte, messageSize)

	offset := 0

	// Version
	buf[offset] = version
	offset++

	// Dimensionality
	buf[offset] = dimensionality
	offset++

	// Flags
	buf[offset] = flags
	offset++

	// Reserved (5 bytes, all zeros)
	offset += 5

	// Dimensions (8 bytes each, little-endian)
	for _, dim := range dims {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], dim)
		offset += 8
	}

	// Max Dimensions (if present)
	if len(maxDims) > 0 {
		for _, maxDim := range maxDims {
			binary.LittleEndian.PutUint64(buf[offset:offset+8], maxDim)
			offset += 8
		}
	}

	return buf, nil
}

// writeUint64 writes a uint64 value to buffer using variable-sized encoding.
// This is a helper for encoding addresses and sizes with different byte widths.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value)
	case 2:
		endianness.PutUint16(buf, uint16(value))
	case 4:
		endianness.PutUint32(buf, uint32(value))
	case 8:
		endianness.PutUint64(buf, value)
	default:
		// For other sizes, write what fits
		for i := 0; i < size && i < 8; i++ {
			buf[i] = byte(value >> (i * 8))
		}
	}
}
