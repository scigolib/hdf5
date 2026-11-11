package core

import (
	"encoding/binary"
	"fmt"
)

// MsgFlagConstant marks a header message whose value never changes once
// written (H5O_MSG_FLAG_CONSTANT, H5Oprivate.h:73). H5Dcreate sets it on a
// dataset's datatype, fill value and filter pipeline messages
// (H5Dint.c:1014/1018, H5Dlayout.c:310).
const MsgFlagConstant uint8 = 0x01

// EncodeLayoutMessage encodes a Data Layout message.
// This creates a version 3 layout message (most common format).
//
// Parameters:
//   - layoutClass: Type of layout (contiguous, compact, chunked)
//   - dataSize: Size of the dataset data in bytes (for contiguous) or unused (for chunked)
//   - dataAddress: File address where data is stored (for contiguous) or B-tree root (for chunked)
//   - sb: Superblock for offset/length size encoding
//   - chunkDims: Chunk dimensions (required for chunked layout, nil otherwise)
//   - elementSize: Size of one element in bytes (required for chunked layout, 0 otherwise).
//     Per C reference (H5Dchunk.c:909-913), the layout stores ndims+1 dimensions where
//     the last dimension is the datatype element size.
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
//   - Dimensionality: 1 byte (ndims + 1, includes datatype size dimension)
//   - B-tree Address: offsetSize bytes
//   - Chunk Dimensions: dimensionality * 4 bytes (uint32 each, last = element size)
//
// Reference: HDF5 spec III.D (Data Storage - Data Layout Message)
// C Reference: H5Olayout.c - H5O__layout_encode(), H5Dchunk.c - H5D__chunk_construct().
func EncodeLayoutMessage(
	layoutClass DataLayoutClass,
	dataSize, dataAddress uint64,
	sb *Superblock,
	chunkDims []uint64,
	elementSize uint32,
) ([]byte, error) {
	switch layoutClass {
	case LayoutContiguous:
		return encodeContiguousLayout(dataSize, dataAddress, sb)

	case LayoutChunked:
		if len(chunkDims) == 0 {
			return nil, fmt.Errorf("chunk dimensions required for chunked layout")
		}
		return encodeChunkedLayout(chunkDims, dataAddress, sb, elementSize)

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
// Per C reference (H5Dchunk.c:909-913), the layout dimensionality is ndims+1,
// where the extra dimension stores the datatype element size. This is required
// for correct interop with h5dump, h5ls, h5py and the C library.
//
// Parameters:
//   - chunkDims: Chunk dimensions (N dimensions)
//   - btreeAddress: Address of B-tree root for chunk index
//   - sb: Superblock for encoding parameters
//   - elementSize: Size of one datatype element in bytes (stored as last dimension)
//
// Format (version 3):
//   - Version: 1 byte (3)
//   - Class: 1 byte (2 for chunked)
//   - Dimensionality: 1 byte (ndims + 1, per H5Dchunk.c:913)
//   - B-tree Address: offsetSize bytes
//   - Chunk Dimensions: (ndims+1) * 4 bytes (uint32 each, last = element size)
//
// Reference: H5Olayout.c - H5O__layout_encode() for chunked case.
// C Reference: H5Dchunk.c:909-913 (ndims++) and H5Dchunk.c:826-835 (dim[ndims-1] = type size).
func encodeChunkedLayout(chunkDims []uint64, btreeAddress uint64, sb *Superblock, elementSize uint32) ([]byte, error) {
	if len(chunkDims) == 0 {
		return nil, fmt.Errorf("chunk dimensions cannot be empty")
	}

	// Per C reference: layout->u.chunk.ndims++ (includes datatype size dimension).
	dimensionality := len(chunkDims) + 1
	if dimensionality > 255 {
		return nil, fmt.Errorf("dimensionality %d exceeds maximum 255", dimensionality)
	}

	// Validate chunk dimensions fit in uint32 (HDF5 format limitation).
	for i, dim := range chunkDims {
		if dim > 0xFFFFFFFF {
			return nil, fmt.Errorf("chunk dimension %d (%d) exceeds uint32 maximum", i, dim)
		}
	}

	// Calculate total message size
	// Version (1) + Class (1) + Dimensionality (1) + BTreeAddress (OffsetSize) + ChunkDims (4 * (ndims+1))
	messageSize := 3 + int(sb.OffsetSize) + dimensionality*4
	buf := make([]byte, messageSize)

	offset := 0

	// Version 3
	buf[offset] = 3
	offset++

	// Class = LayoutChunked (2)
	buf[offset] = byte(LayoutChunked)
	offset++

	// Dimensionality (ndims + 1, per C reference)
	buf[offset] = byte(dimensionality)
	offset++

	// B-tree root address (where chunk index is stored)
	writeUint64(buf[offset:], btreeAddress, int(sb.OffsetSize), sb.Endianness)
	offset += int(sb.OffsetSize)

	// Chunk dimensions (each 4 bytes, uint32)
	for _, dim := range chunkDims {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(dim)) //nolint:gosec // G115: chunk dims bounded by HDF5 format limits
		offset += 4
	}

	// Last dimension = datatype element size (per H5Dchunk.c:826-835)
	binary.LittleEndian.PutUint32(buf[offset:], elementSize)

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
	return EncodeDataspaceMessageVersion(dims, maxDims, DataspaceVersion1)
}

// Dataspace message versions (H5O_SDSPACE_VERSION_1/2, H5Spkg.h:76-82).
const (
	DataspaceVersion1 uint8 = 1
	DataspaceVersion2 uint8 = 2
)

// maxDataspaceRank is H5S_MAX_RANK (H5Spublic.h:55): the rank byte of the
// message can hold more, the library cannot.
const maxDataspaceRank = 32

// DataspaceVersionFor returns the dataspace message version H5S_set_version
// gives a new dataset or attribute (H5S.c:1990-2013): the low bound of the
// file's format, version 1 for the earliest format (superblock 0 and 1) and
// version 2 from the 1.8 format on (H5O_sdspace_ver_bounds, H5S.c:61-69).
func DataspaceVersionFor(superblockVersion uint8) uint8 {
	if formatIs18(superblockVersion) {
		return DataspaceVersion2
	}
	return DataspaceVersion1
}

// The writer picks every message version here, from one notion of the
// file's format: the 1.8 format from superblock version 2 on, the earliest
// format before. C takes the low bound from the file access property list
// instead (H5F_LOW_BOUND): the C library reopening a superblock 2 file with
// the default property list writes the earliest versions. Both are valid; the
// superblock is what this library has (review of TASK-072, design note 2).
func formatIs18(superblockVersion uint8) bool {
	return superblockVersion >= Version2
}

// Filter pipeline message versions (H5Opline.c:85-93), shared by the
// reader and the writer's encoder.
const (
	PipelineVersion1 = pipelineVersion1
	PipelineVersion2 = pipelineVersion2
)

// PipelineVersionFor returns the filter pipeline message version C writes
// for a new dataset: MAX(1, H5O_pline_ver_bounds[low bound]) (H5Opline.c:
// 711), version 1 for the earliest format, version 2 from 1.8 on.
func PipelineVersionFor(superblockVersion uint8) uint8 {
	if formatIs18(superblockVersion) {
		return PipelineVersion2
	}
	return PipelineVersion1
}

// FillValueVersionFor returns the fill value message version C writes for a
// new dataset: MAX(2, H5O_fill_ver_bounds[low bound]) (H5Ofill.c:150-158,
// 1036), the DCPL default being version 2 (H5Pdcpl.c:163); version 3 from
// the 1.8 format on.
func FillValueVersionFor(superblockVersion uint8) uint8 {
	if formatIs18(superblockVersion) {
		return fillVersion3
	}
	return fillVersion2
}

// EncodeDataspaceMessageVersion encodes a simple dataspace as
// H5O__sdspace_encode does (H5Osdspace.c:249-297): version, rank and flags
// (H5S_VALID_MAX when maxDims is given), then in version 1 five reserved
// bytes and in version 2 the dataspace type (H5S_SIMPLE); the sizes and the
// maxima follow as 8-byte lengths.
func EncodeDataspaceMessageVersion(dims, maxDims []uint64, version uint8) ([]byte, error) {
	if len(dims) == 0 {
		return nil, fmt.Errorf("dimensions cannot be empty (use [1] for scalar)")
	}
	if len(dims) > maxDataspaceRank {
		return nil, fmt.Errorf("dataspace rank %d exceeds the maximum %d", len(dims), maxDataspaceRank)
	}
	if len(maxDims) > 0 && len(maxDims) != len(dims) {
		return nil, fmt.Errorf("maxDims length %d must match dims length %d", len(maxDims), len(dims))
	}

	var flags uint8
	if len(maxDims) > 0 {
		flags |= 0x01 // H5S_VALID_MAX
	}

	buf := make([]byte, 0, 8+16*len(dims))
	buf = append(buf, version, uint8(len(dims)), flags) //nolint:gosec // G115: rank <= 32, checked above
	switch version {
	case DataspaceVersion1:
		buf = append(buf, 0, 0, 0, 0, 0)
	case DataspaceVersion2:
		buf = append(buf, byte(DataspaceSimple))
	default:
		return nil, fmt.Errorf("dataspace message version %d is not 1 or 2", version)
	}

	for _, dim := range dims {
		buf = binary.LittleEndian.AppendUint64(buf, dim)
	}
	for _, maxDim := range maxDims {
		buf = binary.LittleEndian.AppendUint64(buf, maxDim)
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

	dataspaceBytes, err := encodeDataspaceOf(dataspace)
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

// encodeDataspaceOf encodes a dataspace that may have been read from a file.
// A null dataspace and a rank-0 (scalar) one are encoded as H5O__sdspace_encode
// writes them (H5Osdspace.c:249-297): no dimensions, a null one in version 2
// because version 1 cannot express it, and a scalar one in its message's
// version. Messages the writer builds carry dimensions (a scalar as [1]) and
// keep the simple encoding, except an attribute written under
// WithScalarAttributes; the Type of the others is left at its zero value, so
// the class alone cannot tell them apart.
func encodeDataspaceOf(ds *DataspaceMessage) ([]byte, error) {
	switch {
	case ds.Type == DataspaceNull:
		return []byte{2, 0, 0, byte(DataspaceNull)}, nil
	case len(ds.Dimensions) == 0 && ds.Version == DataspaceVersion2:
		return []byte{DataspaceVersion2, 0, 0, byte(DataspaceScalar)}, nil
	case len(ds.Dimensions) == 0:
		return []byte{1, 0, 0, 0, 0, 0, 0, 0}, nil
	default:
		// A simple dataspace keeps its version; 0 (unset) is version 1.
		version := ds.Version
		if version == 0 {
			version = DataspaceVersion1
		}
		return EncodeDataspaceMessageVersion(ds.Dimensions, ds.MaxDims, version)
	}
}

// writeUint64 writes a uint64 value to buffer using variable-sized encoding.
// This is a helper for encoding addresses and sizes with different byte widths.
func writeUint64(buf []byte, value uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(value) //nolint:gosec // G115: variable-size encoding
	case 2:
		endianness.PutUint16(buf, uint16(value)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(value)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, value)
	default:
		// For other sizes, write what fits
		for i := 0; i < size && i < 8; i++ {
			buf[i] = byte(value >> (i * 8)) //nolint:gosec // G115: variable-size encoding
		}
	}
}

// EncodeArrayDatatypeMessage encodes an array datatype message (version 3)
// over an already encoded base datatype message. The encoding is done by the
// datatype codec (see [NewArrayDatatype], H5Odtype.c:1367-1403); arraySize
// must equal the product of dims times the base type size.
func EncodeArrayDatatypeMessage(baseType []byte, dims []uint64, arraySize uint32) ([]byte, error) {
	if len(dims) == 0 {
		return nil, fmt.Errorf("array dimensions cannot be empty")
	}
	if len(dims) > arrayMaxRank {
		return nil, fmt.Errorf("too many array dimensions: %d (max %d)", len(dims), arrayMaxRank)
	}
	base, err := decodeEncodedBase(baseType)
	if err != nil {
		return nil, err
	}
	dims32 := make([]uint32, len(dims))
	for i, dim := range dims {
		if dim > 0xFFFFFFFF {
			return nil, fmt.Errorf("dimension %d too large (max 2^32-1)", dim)
		}
		dims32[i] = uint32(dim)
	}
	return EncodeDatatypeMessage(&DatatypeMessage{
		Class: DatatypeArray, Version: dtypeVersion3, Size: arraySize, ArrayDims: dims32, Base: base,
	})
}

// EncodeEnumDatatypeMessage encodes an enumeration datatype message (version
// 1: names padded to eight bytes, all names then all values) over an already
// encoded base datatype message. values holds len(names)*enumSize bytes. The
// encoding is done by the datatype codec (see [NewEnumDatatype],
// H5Odtype.c:1316-1350).
func EncodeEnumDatatypeMessage(baseType []byte, names []string, values []byte, enumSize uint32) ([]byte, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("enum must have at least one member")
	}
	if len(names) > maxListedMembers {
		return nil, fmt.Errorf("too many enum members: %d (max %d)", len(names), maxListedMembers)
	}
	base, err := decodeEncodedBase(baseType)
	if err != nil {
		return nil, err
	}
	need := uint64(len(names)) * uint64(enumSize)
	if uint64(len(values)) < need {
		return nil, fmt.Errorf("not enough value bytes: %d, need %d", len(values), need)
	}
	return EncodeDatatypeMessage(&DatatypeMessage{
		Class: DatatypeEnum, Version: dtypeVersion1, Size: enumSize,
		EnumNames: names, EnumValues: values[:need], Base: base,
	})
}

// decodeEncodedBase decodes an encoded base datatype message that must
// consist of exactly one datatype.
func decodeEncodedBase(baseType []byte) (*DatatypeMessage, error) {
	if len(baseType) == 0 {
		return nil, fmt.Errorf("base type cannot be empty")
	}
	base, n, err := DecodeDatatype(baseType)
	if err != nil {
		return nil, fmt.Errorf("base type: %w", err)
	}
	if n != len(baseType) {
		return nil, fmt.Errorf("base type: %d trailing bytes after the datatype", len(baseType)-n)
	}
	return base, nil
}
