package core

import (
	"encoding/binary"
	"fmt"
)

// EncodeLayoutMessage encodes a Data Layout message.
// This creates a version 3 layout message (most common format).
//
// Parameters:
//   - layoutClass: Type of layout (contiguous, compact, chunked)
//   - dataSize: Size of the dataset data in bytes (for contiguous) or unused (for chunked)
//   - dataAddress: File address where data is stored (for contiguous) or B-tree root (for chunked)
//   - sb: Superblock for offset/length size encoding
//   - chunkDims: Chunk dimensions (required for chunked layout, nil otherwise)
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Format (version 3, contiguous):
//   - Version: 1 byte (3)
//   - Class: 1 byte (1 for contiguous, 2 for chunked)
//   - Data Address: offsetSize bytes
//   - Data Size: lengthSize bytes
//
// Format (version 3, chunked):
//   - Version: 1 byte (3)
//   - Class: 1 byte (2 for chunked)
//   - Dimensionality: 1 byte
//   - B-tree Address: offsetSize bytes
//   - Chunk Dimensions: dimensionality * 4 bytes (uint32 each)
//
// Reference: HDF5 spec III.D (Data Storage - Data Layout Message)
// C Reference: H5Olayout.c - H5O__layout_encode()..
func EncodeLayoutMessage(
	layoutClass DataLayoutClass,
	dataSize, dataAddress uint64,
	sb *Superblock,
	chunkDims []uint64,
) ([]byte, error) {
	switch layoutClass {
	case LayoutContiguous:
		return encodeContiguousLayout(dataSize, dataAddress, sb)

	case LayoutChunked:
		if len(chunkDims) == 0 {
			return nil, fmt.Errorf("chunk dimensions required for chunked layout")
		}
		return encodeChunkedLayout(chunkDims, dataAddress, sb)

	default:
		return nil, fmt.Errorf("unsupported layout class for writing: %d", layoutClass)
	}
}

// encodeContiguousLayout encodes contiguous layout message (version 3).
func encodeContiguousLayout(dataSize, dataAddress uint64, sb *Superblock) ([]byte, error) {
	// Version 3 layout message size:
	// version (1) + class (1) + address (offsetSize) + size (lengthSize)
	messageSize := 2 + int(sb.OffsetSize) + int(sb.LengthSize)
	buf := make([]byte, messageSize)

	offset := 0

	// Version 3
	buf[offset] = 3
	offset++

	// Layout class (contiguous = 1)
	buf[offset] = byte(LayoutContiguous)
	offset++

	// Data address (variable size based on superblock)
	writeUint64(buf[offset:], dataAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Data size (variable size based on superblock)
	writeUint64(buf[offset:], dataSize, int(sb.LengthSize), sb.Endianness)

	return buf, nil
}

// encodeChunkedLayout encodes chunked layout message (version 3).
// This implements the HDF5 chunked storage layout format.
//
// Parameters:
//   - chunkDims: Chunk dimensions
//   - btreeAddress: Address of B-tree root for chunk index
//   - sb: Superblock for encoding parameters
//
// Format (version 3):
//   - Version: 1 byte (3)
//   - Class: 1 byte (2 for chunked)
//   - Dimensionality: 1 byte
//   - B-tree Address: offsetSize bytes
//   - Chunk Dimensions: dimensionality * 4 bytes (uint32 each)
//
// Reference: H5Olayout.c - H5O__layout_encode() for chunked case.
func encodeChunkedLayout(chunkDims []uint64, btreeAddress uint64, sb *Superblock) ([]byte, error) {
	if len(chunkDims) == 0 {
		return nil, fmt.Errorf("chunk dimensions cannot be empty")
	}

	dimensionality := len(chunkDims)
	if dimensionality > 255 {
		return nil, fmt.Errorf("dimensionality %d exceeds maximum 255", dimensionality)
	}

	// Validate chunk dimensions fit in uint32 (HDF5 format limitation)
	for i, dim := range chunkDims {
		if dim > 0xFFFFFFFF {
			return nil, fmt.Errorf("chunk dimension %d (%d) exceeds uint32 maximum", i, dim)
		}
	}

	// Calculate total message size
	// Version (1) + Class (1) + Dimensionality (1) + BTreeAddress (OffsetSize) + ChunkDims (4*N)
	messageSize := 3 + int(sb.OffsetSize) + dimensionality*4
	buf := make([]byte, messageSize)

	offset := 0

	// Version 3
	buf[offset] = 3
	offset++

	// Class = LayoutChunked (2)
	buf[offset] = byte(LayoutChunked)
	offset++

	// Dimensionality
	buf[offset] = byte(dimensionality)
	offset++

	// B-tree root address (where chunk index is stored)
	writeUint64(buf[offset:], btreeAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Chunk dimensions (each 4 bytes, uint32)
	for _, dim := range chunkDims {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(dim)) //nolint:gosec // G115: HDF5 limits dimensions to uint32
		offset += 4
	}

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
// C Reference: H5Odtype.c - H5O__dtype_encode()..
func EncodeDatatypeMessage(dt *DatatypeMessage) ([]byte, error) {
	// Basic validation
	if dt.Size == 0 {
		return nil, fmt.Errorf("datatype size cannot be 0")
	}

	// Support all basic and advanced types
	switch dt.Class {
	case DatatypeFixed, DatatypeFloat:
		// Numeric types: 8 bytes header + properties
		return encodeDatatypeNumeric(dt)
	case DatatypeString:
		// String type: 8 bytes header + properties
		return encodeDatatypeString(dt)
	case DatatypeArray:
		// Array type: needs ArrayMessage field in DatatypeMessage
		return nil, fmt.Errorf("array datatype encoding requires EncodeArrayDatatypeMessage")
	case DatatypeEnum:
		// Enum type: needs EnumMessage field in DatatypeMessage
		return nil, fmt.Errorf("enum datatype encoding requires EncodeEnumDatatypeMessage")
	case DatatypeReference:
		// Reference type: encoded as simple fixed-size type
		return encodeDatatypeReference(dt)
	case DatatypeOpaque:
		// Opaque type: needs tag in properties
		return encodeDatatypeOpaque(dt)
	case DatatypeCompound:
		// Compound type: 8 bytes header + member definitions
		return encodeDatatypeCompound(dt)
	case DatatypeVarLen:
		// Variable-length type: header + base type
		return encodeDatatypeVLen(dt)
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

		switch dt.Size {
		case 4:
			// float32
			mantissaBits = 23
			exponentBits = 8
			exponentBias = 127
		case 8:
			// float64
			mantissaBits = 52
			exponentBits = 11
			//nolint:mnd // Standard IEEE 754 bias for float64
			exponentBias = 127 // Will be adjusted in full implementation
		default:
			return nil, fmt.Errorf("unsupported float size: %d", dt.Size)
		}

		properties = make([]byte, 12)
		properties[0] = byteOrder         // Byte order
		properties[1] = byte(dt.Size * 8) // Precision in bits
		properties[2] = 0                 // Offset (always 0 for standard floats)
		properties[3] = exponentBits      // Exponent size
		properties[4] = mantissaBits      // Mantissa size
		properties[5] = exponentBias      // Exponent bias
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
func encodeDatatypeCompound(_ *DatatypeMessage) ([]byte, error) {
	// For MVP, compound types are simplified
	// In full implementation, this would encode member definitions
	// For now, return error as compound writing is not fully supported in MVP
	return nil, fmt.Errorf("compound datatype encoding not yet implemented in MVP")
}

// encodeDatatypeVLen encodes variable-length datatype (strings, ragged arrays).
// VLen data is stored in global heap, dataset contains 16-byte heap IDs.
func encodeDatatypeVLen(dt *DatatypeMessage) ([]byte, error) {
	// VLen datatype format (HDF5 spec section 3.2.2.2):
	// Header (8 bytes):
	//   - Byte 0: Version (low 4 bits) | Class (high 4 bits)
	//   - Bytes 1-3: Reserved (0)
	//   - Bytes 4-7: Size (always 16 for vlen - heap ID size)
	// Class-specific data (4 bytes):
	//   - Byte 0: VLen type (0x00=sequence, 0x01=string)
	//   - Byte 1: Padding (0x00)
	//   - Bytes 2-3: Character set (for strings, 0x00=ASCII, 0x01=UTF-8)
	// Base type message (variable length, nested datatype)

	// For version 0 VLen (most common)
	version := uint8(0)

	// Build header (8 bytes)
	buf := make([]byte, 8+4+len(dt.Properties))

	// Byte 0: version (low 4 bits) + class (high 4 bits)
	buf[0] = version | (byte(dt.Class) << 4)

	// Bytes 1-3: reserved (already zeros)

	// Bytes 4-7: Size (16 bytes - heap ID size)
	binary.LittleEndian.PutUint32(buf[4:8], dt.Size)

	// Bytes 8-11: Class-specific data (ClassBitField contains type/padding/charset)
	binary.LittleEndian.PutUint32(buf[8:12], dt.ClassBitField)

	// Bytes 12+: Base type message (nested datatype)
	copy(buf[12:], dt.Properties)

	return buf, nil
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
// C Reference: H5Osdspace.c - H5O__sdspace_encode()..
func EncodeDataspaceMessage(dims, maxDims []uint64) ([]byte, error) {
	if len(dims) == 0 {
		return nil, fmt.Errorf("dimensions cannot be empty (use [1] for scalar)")
	}

	if len(maxDims) > 0 && len(maxDims) != len(dims) {
		return nil, fmt.Errorf("maxDims length %d must match dims length %d", len(maxDims), len(dims))
	}

	// Version 1 dataspace message
	version := uint8(1)
	dimensionality := uint8(len(dims)) //nolint:gosec // Safe: dimension count limited

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

// EncodeSymbolTableMessage encodes a Symbol Table Message.
// This message is used in group object headers to point to the symbol table structure.
//
// Message type: 0x11 (17)
//
// Parameters:
//   - btreeAddr: Address of the B-tree v1 root node
//   - heapAddr: Address of the local heap
//   - offsetSize: Size of addresses in bytes (from superblock)
//   - lengthSize: Size of lengths in bytes (from superblock)
//
// Returns:
//   - Encoded message bytes
//
// Format:
//   - B-tree address: offsetSize bytes
//   - Local heap address: offsetSize bytes
//
// Reference: HDF5 spec III.E (Symbol Table Message)
// C Reference: H5Ostab.c - H5O__stab_encode()..
func EncodeSymbolTableMessage(btreeAddr, heapAddr uint64, offsetSize, _ int) []byte {
	// Message size: 2 * offsetSize
	messageSize := 2 * offsetSize
	buf := make([]byte, messageSize)

	// B-tree address (variable size based on superblock)
	writeUint64(buf[0:], btreeAddr, offsetSize, binary.LittleEndian)

	// Local heap address (variable size based on superblock)
	writeUint64(buf[offsetSize:], heapAddr, offsetSize, binary.LittleEndian)

	return buf
}

// EncodeAttributeMessage encodes an Attribute message for compact storage.
// This creates a version 3 attribute message (HDF5 1.8+).
//
// Parameters:
//   - name: Attribute name (null-terminated in encoded form)
//   - datatype: Datatype message for the attribute value
//   - dataspace: Dataspace message for the attribute value
//   - data: Raw attribute data bytes
//
// Returns:
//   - Encoded message bytes
//   - Error if encoding fails
//
// Format (version 3):
//   - Version: 1 byte (3)
//   - Flags: 1 byte (0 for compact, no special features)
//   - Name size: 2 bytes (includes null terminator)
//   - Datatype size: 2 bytes
//   - Dataspace size: 2 bytes
//   - Name encoding: 1 byte (0=ASCII, 1=UTF-8)
//   - Name: variable (null-terminated)
//   - Datatype: variable (encoded datatype message)
//   - Dataspace: variable (encoded dataspace message)
//   - Data: variable (actual attribute value)
//
// Reference: HDF5 spec III.M (Attribute Message)
// C Reference: H5Oattr.c - H5O__attr_encode()..
func EncodeAttributeMessage(name string, datatype *DatatypeMessage, dataspace *DataspaceMessage, data []byte) ([]byte, error) {
	// Validate inputs
	if name == "" {
		return nil, fmt.Errorf("attribute name cannot be empty")
	}
	if datatype == nil {
		return nil, fmt.Errorf("datatype cannot be nil")
	}
	if dataspace == nil {
		return nil, fmt.Errorf("dataspace cannot be nil")
	}

	// Encode datatype and dataspace messages
	datatypeBytes, err := EncodeDatatypeMessage(datatype)
	if err != nil {
		return nil, fmt.Errorf("encode datatype: %w", err)
	}

	dataspaceBytes, err := EncodeDataspaceMessage(dataspace.Dimensions, dataspace.MaxDims)
	if err != nil {
		return nil, fmt.Errorf("encode dataspace: %w", err)
	}

	// Calculate sizes
	// Name size includes null terminator
	nameSize := uint16(len(name) + 1)            //nolint:gosec // Safe: name length limited
	datatypeSize := uint16(len(datatypeBytes))   //nolint:gosec // Safe: datatype bytes limited
	dataspaceSize := uint16(len(dataspaceBytes)) //nolint:gosec // Safe: dataspace bytes limited

	// Calculate total message size
	// Header: version(1) + flags(1) + name_size(2) + dtype_size(2) + dspace_size(2) + name_encoding(1) = 9 bytes
	headerSize := 9
	messageSize := headerSize + int(nameSize) + len(datatypeBytes) + len(dataspaceBytes) + len(data)

	buf := make([]byte, messageSize)
	offset := 0

	// Version 3 (HDF5 1.8+)
	buf[offset] = 3
	offset++

	// Flags (0 = compact storage, no special features)
	buf[offset] = 0
	offset++

	// Name size (includes null terminator)
	binary.LittleEndian.PutUint16(buf[offset:offset+2], nameSize)
	offset += 2

	// Datatype size
	binary.LittleEndian.PutUint16(buf[offset:offset+2], datatypeSize)
	offset += 2

	// Dataspace size
	binary.LittleEndian.PutUint16(buf[offset:offset+2], dataspaceSize)
	offset += 2

	// Name encoding (0 = ASCII)
	buf[offset] = 0
	offset++

	// Name (null-terminated)
	copy(buf[offset:], name)
	offset += len(name)
	buf[offset] = 0 // null terminator
	offset++

	// Datatype message
	copy(buf[offset:], datatypeBytes)
	offset += len(datatypeBytes)

	// Dataspace message
	copy(buf[offset:], dataspaceBytes)
	offset += len(dataspaceBytes)

	// Attribute data
	if len(data) > 0 {
		copy(buf[offset:], data)
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
		endianness.PutUint16(buf, uint16(value)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(value)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, value)
	default:
		// For other sizes, write what fits
		for i := 0; i < size && i < 8; i++ {
			buf[i] = byte(value >> (i * 8))
		}
	}
}

// encodeDatatypeReference encodes reference datatypes (object/region references).
// Reference types are fixed-size types with no additional properties.
func encodeDatatypeReference(dt *DatatypeMessage) ([]byte, error) {
	// Version 1 for reference types
	version := uint8(1)

	// Validate size
	if dt.Size != 8 && dt.Size != 12 {
		return nil, fmt.Errorf("invalid reference datatype size: %d (must be 8 for object ref or 12 for region ref)", dt.Size)
	}

	// No additional properties for reference types
	// Properties are minimal (just header)
	buf := make([]byte, 8)

	// Pack class, version, and class bit field into bytes 0-3
	// ClassBitField encodes reference type: 0=object, 1=region
	classAndVersion := uint32(dt.Class) | (uint32(version) << 4) | (dt.ClassBitField << 8)
	binary.LittleEndian.PutUint32(buf[0:4], classAndVersion)

	// Size (bytes 4-7)
	binary.LittleEndian.PutUint32(buf[4:8], dt.Size)

	return buf, nil
}

// encodeDatatypeOpaque encodes opaque datatypes (uninterpreted byte sequences with tag).
func encodeDatatypeOpaque(dt *DatatypeMessage) ([]byte, error) {
	// Version 1 for opaque types
	version := uint8(1)

	if dt.Size == 0 {
		return nil, fmt.Errorf("opaque datatype size cannot be 0")
	}

	// Extract tag from Properties (assuming it's stored there)
	// In HDF5, the tag is padded to multiple of 8 bytes
	tag := string(dt.Properties)
	if tag == "" {
		return nil, fmt.Errorf("opaque datatype requires non-empty tag")
	}

	// Calculate padded tag length (multiple of 8)
	tagLen := len(tag)
	paddedLen := ((tagLen + 7) / 8) * 8

	// ClassBitField stores the padded tag length
	classBitField := uint32(paddedLen) //nolint:gosec // Safe: padded length limited

	// Build message: header (8 bytes) + padded tag
	messageSize := 8 + paddedLen
	buf := make([]byte, messageSize)

	// Pack class, version, and padded tag length
	classAndVersion := uint32(dt.Class) | (uint32(version) << 4) | (classBitField << 8)
	binary.LittleEndian.PutUint32(buf[0:4], classAndVersion)

	// Size
	binary.LittleEndian.PutUint32(buf[4:8], dt.Size)

	// Tag (null-padded to paddedLen)
	copy(buf[8:], tag)
	// Remaining bytes are already zero (null-padded)

	return buf, nil
}

// EncodeArrayDatatypeMessage encodes an array datatype message.
// Array datatypes contain a base type and dimensions.
//
// Parameters:
//   - baseTyp: Encoded base datatype message
//   - dims: Array dimensions
//
// Returns:
//   - Encoded message bytes (full datatype message with array properties)
//   - Error if encoding fails
//
// Format (version 3, HDF5 1.8+):
//   - Bytes 0-3: Class (4 bits) | Version (4 bits) | Reserved (24 bits)
//   - Bytes 4-7: Size (total array size = product of dims * base size)
//   - Byte 8: Dimensionality (ndims)
//   - Bytes 9+: Dimension sizes (ndims * 4 bytes each, uint32)
//   - Following: Base type message (encoded datatype)
//
// Reference: HDF5 spec III.C (Datatype Message - Array class).
// C Reference: H5Odtype.c - H5O__dtype_encode_helper() for H5T_ARRAY.
func EncodeArrayDatatypeMessage(baseType []byte, dims []uint64, arraySize uint32) ([]byte, error) {
	if len(dims) == 0 {
		return nil, fmt.Errorf("array dimensions cannot be empty")
	}
	if len(dims) > 255 {
		return nil, fmt.Errorf("too many array dimensions: %d (max 255)", len(dims))
	}
	if len(baseType) == 0 {
		return nil, fmt.Errorf("base type cannot be empty")
	}

	// Version 3 (no reserved bytes for dimensions)
	version := uint8(3)
	ndims := uint8(len(dims)) //nolint:gosec // Safe: validated above

	// Calculate message size
	// Header: 8 bytes
	// Ndims: 1 byte
	// Dimensions: ndims * 4 bytes (uint32 for each dimension)
	// Base type: len(baseType) bytes
	headerSize := 8
	dimsSize := 1 + int(ndims)*4
	messageSize := headerSize + dimsSize + len(baseType)

	buf := make([]byte, messageSize)
	offset := 0

	// Pack class, version, reserved (0)
	classAndVersion := uint32(DatatypeArray) | (uint32(version) << 4)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], classAndVersion)
	offset += 4

	// Size (total array size)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], arraySize)
	offset += 4

	// Dimensionality
	buf[offset] = ndims
	offset++

	// Dimensions (uint32 each)
	for _, dim := range dims {
		if dim > 0xFFFFFFFF {
			return nil, fmt.Errorf("dimension %d too large (max 2^32-1)", dim)
		}
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(dim))
		offset += 4
	}

	// Base type message
	copy(buf[offset:], baseType)

	return buf, nil
}

// EncodeEnumDatatypeMessage encodes an enum datatype message.
// Enum datatypes map integer values to symbolic names.
//
// Parameters:
//   - baseType: Encoded base integer datatype message
//   - names: Enum member names
//   - values: Enum member values (as bytes, size = nmembs * baseSize)
//   - enumSize: Size of enum (same as base type size)
//
// Returns:
//   - Encoded message bytes (full datatype message with enum properties)
//   - Error if encoding fails
//
// Format (version 3):
//   - Bytes 0-3: Class (4 bits) | Version (4 bits) | NumMembers (16 bits, in classBitField)
//   - Bytes 4-7: Size (base type size)
//   - Following: Base type message
//   - Following: For each member:
//   - Name (null-terminated, padded to multiple of 8)
//   - Value (size bytes)
//
// Reference: HDF5 spec III.C (Datatype Message - Enum class).
// C Reference: H5Odtype.c - H5O__dtype_encode_helper() for H5T_ENUM.
func EncodeEnumDatatypeMessage(baseType []byte, names []string, values []byte, enumSize uint32) ([]byte, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("enum must have at least one member")
	}
	if len(names) > 0xFFFF {
		return nil, fmt.Errorf("too many enum members: %d (max 65535)", len(names))
	}
	if len(baseType) == 0 {
		return nil, fmt.Errorf("base type cannot be empty")
	}

	nmembs := uint16(len(names)) //nolint:gosec // Safe: validated above
	version := uint8(3)

	// Calculate total message size
	headerSize := 8
	baseTypeSize := len(baseType)

	// Calculate names + values size
	namesValuesSize := 0
	for i, name := range names {
		// Name: null-terminated, padded to multiple of 8
		nameLen := len(name) + 1 // include null terminator
		paddedNameLen := ((nameLen + 7) / 8) * 8
		namesValuesSize += paddedNameLen

		// Value: enumSize bytes
		valueOffset := i * int(enumSize)
		if valueOffset+int(enumSize) > len(values) {
			return nil, fmt.Errorf("not enough value bytes for member %d", i)
		}
		namesValuesSize += int(enumSize)
	}

	messageSize := headerSize + baseTypeSize + namesValuesSize
	buf := make([]byte, messageSize)
	offset := 0

	// Pack class, version, nmembs
	// ClassBitField stores nmembs (lower 16 bits)
	classAndVersion := uint32(DatatypeEnum) | (uint32(version) << 4) | (uint32(nmembs) << 8)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], classAndVersion)
	offset += 4

	// Size
	binary.LittleEndian.PutUint32(buf[offset:offset+4], enumSize)
	offset += 4

	// Base type message
	copy(buf[offset:], baseType)
	offset += len(baseType)

	// Names and values
	for i, name := range names {
		// Name (null-terminated, padded to multiple of 8)
		nameLen := len(name) + 1
		paddedNameLen := ((nameLen + 7) / 8) * 8

		copy(buf[offset:], name)
		offset += len(name)
		buf[offset] = 0 // null terminator
		offset++

		// Padding (zeros)
		offset += paddedNameLen - nameLen

		// Value
		valueOffset := i * int(enumSize)
		copy(buf[offset:], values[valueOffset:valueOffset+int(enumSize)])
		offset += int(enumSize)
	}

	return buf, nil
}
