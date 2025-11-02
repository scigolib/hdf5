// Package core provides HDF5 file format parsing and manipulation functionality.
package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/scigolib/hdf5/internal/utils"
)

// Attribute represents an HDF5 attribute with metadata and data.
type Attribute struct {
	Name      string
	Datatype  *DatatypeMessage
	Dataspace *DataspaceMessage
	Data      []byte
}

// AttributeInfoMessage represents the Attribute Info Message (0x000F).
// This message contains information about dense attribute storage.
// Reference: H5Adense.c in C library.
type AttributeInfoMessage struct {
	Version            uint8
	Flags              uint8
	FractalHeapAddr    uint64 // Address of fractal heap for dense attribute storage
	BTreeNameIndexAddr uint64 // Address of B-tree v2 for indexing attributes by name
	// Optional fields for creation order (if tracked):
	MaxCreationIndex    uint64 // Only present if creation order tracked
	BTreeOrderIndexAddr uint64 // Only present if creation order indexed
}

// ParseAttributeMessage parses an attribute message (type 0x000C).
// Format according to HDF5 spec:
// - Version (1 byte).
// - Flags (1 byte) - reserved, always 0.
// - Name size (2 bytes).
// - Datatype size (2 bytes).
// - Dataspace size (2 bytes).
// - Name encoding (1 byte) - for version 3+.
// - Name (variable, null-terminated).
// - Datatype message data.
// - Dataspace message data.
// - Data (variable).
func ParseAttributeMessage(data []byte, endianness binary.ByteOrder) (*Attribute, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("attribute message too short: %d bytes", len(data))
	}

	attr := &Attribute{}
	offset := 0

	// Version.
	version := data[offset]
	offset++

	// Flags (reserved).
	offset++

	// Name size (2 bytes).
	nameSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Datatype size (2 bytes).
	datatypeSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// Dataspace size (2 bytes).
	dataspaceSize := endianness.Uint16(data[offset : offset+2])
	offset += 2

	// For version 3+, there's a name encoding byte.
	if version >= 3 {
		// Name encoding (1 byte) - 0 = ASCII, 1 = UTF-8.
		offset++
	}

	// Read name (null-terminated).
	if offset+int(nameSize) > len(data) {
		return nil, fmt.Errorf("name extends beyond message: offset=%d, nameSize=%d, dataLen=%d",
			offset, nameSize, len(data))
	}

	// Name is null-terminated, so subtract 1 for the null byte.
	if nameSize > 0 {
		attr.Name = string(data[offset : offset+int(nameSize)-1])
		offset += int(nameSize)
	}

	// Parse datatype.
	if offset+int(datatypeSize) > len(data) {
		return nil, fmt.Errorf("datatype extends beyond message")
	}

	datatypeData := data[offset : offset+int(datatypeSize)]
	var err error
	attr.Datatype, err = ParseDatatypeMessage(datatypeData)
	if err != nil {
		return nil, utils.WrapError("datatype parse failed", err)
	}
	offset += int(datatypeSize)

	// Parse dataspace.
	if offset+int(dataspaceSize) > len(data) {
		return nil, fmt.Errorf("dataspace extends beyond message")
	}

	dataspaceData := data[offset : offset+int(dataspaceSize)]
	attr.Dataspace, err = ParseDataspaceMessage(dataspaceData)
	if err != nil {
		return nil, utils.WrapError("dataspace parse failed", err)
	}
	offset += int(dataspaceSize)

	// Remaining data is the attribute value.
	if offset < len(data) {
		attr.Data = make([]byte, len(data)-offset)
		copy(attr.Data, data[offset:])
	}

	return attr, nil
}

// ReadValue reads the attribute value as the appropriate Go type.
func (a *Attribute) ReadValue() (interface{}, error) {
	if a.Datatype == nil || a.Dataspace == nil {
		return nil, fmt.Errorf("attribute missing datatype or dataspace")
	}

	totalElements := a.Dataspace.TotalElements()
	if totalElements == 0 || len(a.Data) == 0 {
		// Empty attribute - return empty slice instead of nil.
		return []interface{}{}, nil
	}

	// For scalar attributes, return single value.
	isScalar := len(a.Dataspace.Dimensions) == 0 ||
		(len(a.Dataspace.Dimensions) == 1 && a.Dataspace.Dimensions[0] == 1)

	switch a.Datatype.Class {
	case DatatypeFixed:
		switch a.Datatype.Size {
		case 4:
			values := make([]int32, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 4
				if offset+4 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				//nolint:gosec // G115: HDF5 binary format requires uint32 to int32 conversion
				values[i] = int32(binary.LittleEndian.Uint32(a.Data[offset : offset+4]))
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		case 8:
			values := make([]int64, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 8
				if offset+8 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
				values[i] = int64(binary.LittleEndian.Uint64(a.Data[offset : offset+8]))
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		}

	case DatatypeFloat:
		switch a.Datatype.Size {
		case 4:
			values := make([]float32, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 4
				if offset+4 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				bits := binary.LittleEndian.Uint32(a.Data[offset : offset+4])
				values[i] = float32frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		case 8:
			values := make([]float64, totalElements)
			for i := uint64(0); i < totalElements; i++ {
				offset := i * 8
				if offset+8 > uint64(len(a.Data)) {
					return nil, fmt.Errorf("data too short for element %d", i)
				}
				bits := binary.LittleEndian.Uint64(a.Data[offset : offset+8])
				values[i] = float64frombits(bits)
			}
			if isScalar {
				return values[0], nil
			}
			return values, nil
		}

	case DatatypeString:
		// Fixed-length strings (variable-length not supported yet)
		if !a.Datatype.IsFixedString() {
			return nil, fmt.Errorf("variable-length strings not yet supported in attributes")
		}

		stringSize := uint64(a.Datatype.Size)
		paddingType := a.Datatype.GetStringPadding()
		values := make([]string, totalElements)

		for i := uint64(0); i < totalElements; i++ {
			offset := i * stringSize
			if offset+stringSize > uint64(len(a.Data)) {
				return nil, fmt.Errorf("data too short for string element %d", i)
			}
			stringBytes := a.Data[offset : offset+stringSize]
			values[i] = decodeFixedString(stringBytes, paddingType)
		}

		if isScalar {
			return values[0], nil
		}
		return values, nil
	}

	return nil, fmt.Errorf("unsupported datatype class %d or size %d", a.Datatype.Class, a.Datatype.Size)
}

// ParseAttributesFromMessages extracts all attributes from object header messages.
// Supports both compact attributes (stored in object header) and dense attributes
// (stored in fractal heap).
// Reference: H5Adense.c in C library.
func ParseAttributesFromMessages(r io.ReaderAt, messages []*HeaderMessage, sb *Superblock) ([]*Attribute, error) {
	var attributes []*Attribute
	var attrInfo *AttributeInfoMessage

	// First pass: look for AttributeInfo message (dense storage)
	for _, msg := range messages {
		if msg.Type == MsgAttributeInfo {
			var err error
			attrInfo, err = ParseAttributeInfoMessage(msg.Data, sb)
			if err != nil {
				// If we can't parse attribute info, fall back to compact only
				attrInfo = nil
			}
			break
		}
	}

	// Second pass: collect compact attributes
	for _, msg := range messages {
		if msg.Type == MsgAttribute {
			attr, err := ParseAttributeMessage(msg.Data, sb.Endianness)
			if err != nil {
				// Log error but continue with other attributes
				continue
			}
			attributes = append(attributes, attr)
		}
	}

	// If dense storage exists, read attributes from fractal heap
	if attrInfo != nil && attrInfo.FractalHeapAddr != 0 {
		denseAttrs, err := readDenseAttributes(r, attrInfo, sb)
		if err != nil {
			return nil, fmt.Errorf("failed to read dense attributes: %w", err)
		}
		attributes = append(attributes, denseAttrs...)
	}

	return attributes, nil
}

// Helper functions for float conversion (copied from dataset_reader.go to avoid circular import).
func float32frombits(b uint32) float32 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float32 bit representation
	return *(*float32)(unsafe.Pointer(&b))
}

func float64frombits(b uint64) float64 {
	//nolint:gosec // G103: unsafe.Pointer required for IEEE 754 float64 bit representation
	return *(*float64)(unsafe.Pointer(&b))
}

// readDenseAttributes reads attributes from dense storage (fractal heap + B-tree v2).
//
// Dense attribute storage is used when there are many attributes (> 8 by default).
// In this format, attributes are stored in a fractal heap with a B-tree v2 index.
//
// This is a self-contained implementation that directly reads B-tree and heap structures
// without importing the structures package (to avoid circular dependencies).
//
// Reference:
//   - C Library: H5Adense.c - H5A__dense_iterate()
//   - Format Spec: Section III.E (Fractal Heap) and III.A.2 (B-tree v2)
func readDenseAttributes(r io.ReaderAt, attrInfo *AttributeInfoMessage, sb *Superblock) ([]*Attribute, error) {
	if attrInfo == nil || sb == nil {
		return nil, fmt.Errorf("attribute info or superblock is nil")
	}

	if attrInfo.FractalHeapAddr == 0 || attrInfo.BTreeNameIndexAddr == 0 {
		return nil, fmt.Errorf("invalid dense attribute addresses (heap=0x%X, btree=0x%X)",
			attrInfo.FractalHeapAddr, attrInfo.BTreeNameIndexAddr)
	}

	// Step 1: Read B-tree v2 header to get root node address and record count
	btreeHeader, err := readBTreeV2HeaderRaw(r, attrInfo.BTreeNameIndexAddr, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read B-tree header: %w", err)
	}

	// Step 2: Read B-tree leaf node to get all heap IDs
	heapIDs, err := readBTreeV2LeafRecords(r, btreeHeader.RootNodeAddr, btreeHeader.NumRecordsRoot, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read B-tree leaf: %w", err)
	}

	if len(heapIDs) == 0 {
		return []*Attribute{}, nil
	}

	// Step 3: Read fractal heap header to find direct block
	heapHeader, err := readFractalHeapHeaderRaw(r, attrInfo.FractalHeapAddr, sb)
	if err != nil {
		return nil, fmt.Errorf("failed to read heap header: %w", err)
	}

	// Step 4: For each heap ID, read attribute from heap
	attributes := make([]*Attribute, 0, len(heapIDs))
	for i, heapID := range heapIDs {
		// Parse heap ID to get offset and length
		offset, length, err := parseHeapID(heapID, heapHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to parse heap ID %d: %w", i, err)
		}

		// Read object from direct block
		objectData, err := readHeapObject(r, heapHeader.RootBlockAddress, offset, length, sb, heapHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to read heap object %d: %w", i, err)
		}

		// Parse as attribute message (version 3 for dense attributes)
		attr, err := ParseAttributeMessage(objectData, sb.Endianness)
		if err != nil {
			return nil, fmt.Errorf("failed to parse attribute %d: %w", i, err)
		}

		attributes = append(attributes, attr)
	}

	return attributes, nil
}

// btreeV2HeaderRaw represents a minimal B-tree v2 header.
// Reference: H5B2hdr.c in C library.
type btreeV2HeaderRaw struct {
	Version        uint8
	Type           uint8
	NodeSize       uint32
	RecordSize     uint16
	Depth          uint16
	RootNodeAddr   uint64
	NumRecordsRoot uint16
	TotalRecords   uint64
}

// readBTreeV2HeaderRaw reads a B-tree v2 header directly from file.
// Format (Section III.A.2 of HDF5 spec):
//   - Signature "BTHD" (4 bytes)
//   - Version (1 byte)
//   - Type (1 byte)
//   - Node Size (4 bytes)
//   - Record Size (2 bytes)
//   - Depth (2 bytes)
//   - Split Percent (1 byte)
//   - Merge Percent (1 byte)
//   - Root Node Address (offsetSize bytes)
//   - Number of Records in Root (2 bytes)
//   - Total Records (8 bytes)
//   - Checksum (4 bytes)
func readBTreeV2HeaderRaw(r io.ReaderAt, addr uint64, sb *Superblock) (*btreeV2HeaderRaw, error) {
	// Allocate buffer for header (max size with 8-byte offsets: 4+1+1+4+2+2+1+1+8+2+8+4 = 38 bytes)
	buf := make([]byte, 38)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read failed at 0x%X: %w", addr, err)
	}
	if n < 20 {
		return nil, fmt.Errorf("header too short: %d bytes", n)
	}

	// Check signature
	if string(buf[0:4]) != "BTHD" {
		return nil, fmt.Errorf("invalid B-tree v2 signature: %q", buf[0:4])
	}

	header := &btreeV2HeaderRaw{}
	offset := 4

	// Version
	header.Version = buf[offset]
	offset++

	// Type
	header.Type = buf[offset]
	offset++

	// Node Size (4 bytes)
	header.NodeSize = sb.Endianness.Uint32(buf[offset : offset+4])
	offset += 4

	// Record Size (2 bytes)
	header.RecordSize = sb.Endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Depth (2 bytes)
	header.Depth = sb.Endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Skip Split % and Merge % (1 byte each)
	offset += 2

	// Root Node Address (offsetSize bytes)
	offsetSize := int(sb.OffsetSize)
	if offset+offsetSize > len(buf) {
		return nil, fmt.Errorf("buffer too short for root node address")
	}
	header.RootNodeAddr = readAddress(buf[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// Number of Records in Root (2 bytes)
	if offset+2 > len(buf) {
		return nil, fmt.Errorf("buffer too short for num records")
	}
	header.NumRecordsRoot = sb.Endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Total Records (8 bytes)
	if offset+8 > len(buf) {
		return nil, fmt.Errorf("buffer too short for total records")
	}
	header.TotalRecords = sb.Endianness.Uint64(buf[offset : offset+8])

	return header, nil
}

// readBTreeV2LeafRecords reads heap IDs from a B-tree v2 leaf node.
// Format (Section III.A.2 of HDF5 spec):
//   - Signature "BTLF" (4 bytes)
//   - Version (1 byte)
//   - Type (1 byte)
//   - Records (N × record size):
//     Each record: Name Hash (4 bytes) + Heap ID (7 bytes)
//   - Checksum (4 bytes)
func readBTreeV2LeafRecords(r io.ReaderAt, addr uint64, numRecords uint16, _ *Superblock) ([][7]byte, error) {
	// Each record: 4 (hash) + 7 (heap ID) = 11 bytes
	// Header: 4 (sig) + 1 (ver) + 1 (type) = 6 bytes
	// Checksum: 4 bytes
	bufSize := 6 + int(numRecords)*11 + 4
	buf := make([]byte, bufSize)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read failed at 0x%X: %w", addr, err)
	}
	if n < 10 {
		return nil, fmt.Errorf("leaf node too short: %d bytes", n)
	}

	// Check signature
	if string(buf[0:4]) != "BTLF" {
		return nil, fmt.Errorf("invalid B-tree v2 leaf signature: %q", buf[0:4])
	}

	// Skip version (1) and type (1)
	offset := 6

	// Read records
	heapIDs := make([][7]byte, numRecords)
	for i := uint16(0); i < numRecords; i++ {
		if offset+11 > len(buf) {
			return nil, fmt.Errorf("buffer too short for record %d", i)
		}

		// Skip name hash (4 bytes), copy heap ID (7 bytes)
		offset += 4
		copy(heapIDs[i][:], buf[offset:offset+7])
		offset += 7
	}

	return heapIDs, nil
}

// fractalHeapHeaderRaw represents a minimal fractal heap header.
// Reference: H5HFhdr.c in C library.
type fractalHeapHeaderRaw struct {
	Version            uint8
	HeapIDLen          uint16
	RootBlockAddress   uint64
	MaxHeapSize        uint16 // Log2 of max heap size
	MaxDirectBlockSize uint64 // Max direct block size
	MaxManagedObjSize  uint32 // Max managed object size
	HeapOffsetSize     uint8  // Computed from MaxHeapSize
	HeapLengthSize     uint8  // Computed from MaxDirectBlockSize and MaxManagedObjSize
	ChecksumDirBlocks  bool   // Whether direct blocks have checksums
}

// readFractalHeapHeaderRaw reads a fractal heap header directly from file.
// Format (Section III.E of HDF5 spec):
//   - Signature "FRHP" (4 bytes)
//   - Version (1 byte)
//   - Heap ID Length (2 bytes)
//   - I/O Filters Encoded Length (2 bytes)
//   - Flags (1 byte) - bit 1 indicates checksum
//   - ... many fields ...
//   - Max Direct Block Size (sizeofSize bytes) at variable offset
//   - Max Heap Size (2 bytes)
//   - ... more fields ...
//   - Root Block Address (offsetSize bytes) at offset 132
//   - Checksum (4 bytes)
func readFractalHeapHeaderRaw(r io.ReaderAt, addr uint64, sb *Superblock) (*fractalHeapHeaderRaw, error) {
	// We need to read up to offset 132 + offsetSize + 4 (checksum)
	// Max with 8-byte offsets: 132 + 8 + 4 = 144 bytes
	buf := make([]byte, 144)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	n, err := r.ReadAt(buf, int64(addr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read failed at 0x%X: %w", addr, err)
	}
	if n < 20 {
		return nil, fmt.Errorf("heap header too short: %d bytes", n)
	}

	// Check signature
	if string(buf[0:4]) != "FRHP" {
		return nil, fmt.Errorf("invalid fractal heap signature: %q", buf[0:4])
	}

	header := &fractalHeapHeaderRaw{}
	offset := 4

	// Version
	header.Version = buf[offset]
	offset++

	// Heap ID Length (2 bytes)
	header.HeapIDLen = sb.Endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// I/O Filters Encoded Length (2 bytes)
	filtersLen := sb.Endianness.Uint16(buf[offset : offset+2])
	offset += 2

	// Flags (1 byte)
	flags := buf[offset]
	header.ChecksumDirBlocks = (flags & 0x02) != 0
	offset++

	// Max Managed Object Size (4 bytes)
	header.MaxManagedObjSize = sb.Endianness.Uint32(buf[offset : offset+4])

	// Skip to Table Width (offset 110 from start)
	offset = 110

	// Table Width (2 bytes)
	offset += 2

	// Starting Block Size (sizeofSize bytes)
	sizeofSize := int(sb.LengthSize)
	offset += sizeofSize

	// Max Direct Block Size (sizeofSize bytes)
	var maxDirBlockSizeBytes = buf[offset : offset+sizeofSize]
	header.MaxDirectBlockSize = 0
	for i := 0; i < sizeofSize; i++ {
		header.MaxDirectBlockSize |= uint64(maxDirBlockSizeBytes[i]) << (8 * i)
	}
	offset += sizeofSize

	// Max Heap Size (2 bytes) - log2 of maximum heap size
	if offset+2 > len(buf) {
		return nil, fmt.Errorf("buffer too short for max heap size")
	}
	header.MaxHeapSize = sb.Endianness.Uint16(buf[offset : offset+2])

	// Compute HeapOffsetSize from MaxHeapSize
	// HeapOffsetSize = ceil(MaxHeapSize / 8)
	//nolint:gosec // G115: MaxHeapSize is bounded by format specification
	header.HeapOffsetSize = uint8((header.MaxHeapSize + 7) / 8)

	// Compute HeapLengthSize
	// HeapLengthSize = min(offsetSize(MaxDirectBlockSize), offsetSize(MaxManagedObjSize))
	maxDirBlockOffsetSize := computeOffsetSize(header.MaxDirectBlockSize)
	maxManSizeEncoded := computeOffsetSize(uint64(header.MaxManagedObjSize))
	if maxDirBlockOffsetSize < maxManSizeEncoded {
		header.HeapLengthSize = maxDirBlockOffsetSize
	} else {
		header.HeapLengthSize = maxManSizeEncoded
	}

	// Skip to Root Block Address at offset 132
	offset = 132

	// Root Block Address (offsetSize bytes)
	offsetSize := int(sb.OffsetSize)
	if offset+offsetSize > len(buf) {
		return nil, fmt.Errorf("buffer too short for root block address")
	}
	header.RootBlockAddress = readAddress(buf[offset:offset+offsetSize], offsetSize)

	// Note: We ignore filtersLen for now (assume no filters in dense attributes)
	_ = filtersLen

	return header, nil
}

// computeOffsetSize computes the number of bytes needed to store a value.
// Reference: H5HF.c - H5HF__dtable_size_to_row() and similar.
func computeOffsetSize(value uint64) uint8 {
	if value == 0 {
		return 1
	}

	// Find highest bit set
	bits := 0
	v := value
	for v > 0 {
		bits++
		v >>= 1
	}

	// Round up to bytes
	//nolint:gosec // G115: bits range is bounded by uint64 size (max 64 bits)
	return uint8((bits + 7) / 8)
}

// parseHeapID parses a 7-byte heap ID into offset and length.
// Format:
//   - Byte 0: Version (bits 4-7) and Type (bits 0-3)
//   - Bytes 1-4: Offset (uint32, little-endian)
//   - Bytes 5-6: Length (uint16, little-endian)
func parseHeapID(heapID [7]byte, header *fractalHeapHeaderRaw) (offset, length uint64, err error) {
	// Check type (bits 4-5 of byte 0, per HDF5 format spec)
	heapType := (heapID[0] & 0x30) >> 4
	if heapType != 0 {
		return 0, 0, fmt.Errorf("unsupported heap ID type: %d (only managed objects supported)", heapType)
	}

	idx := 1

	// Offset (variable-length, HeapOffsetSize bytes, little-endian)
	offset = 0
	for i := 0; i < int(header.HeapOffsetSize) && idx < 7; i++ {
		offset |= uint64(heapID[idx]) << (8 * i)
		idx++
	}

	// Length (variable-length, HeapLengthSize bytes, little-endian)
	length = 0
	for i := 0; i < int(header.HeapLengthSize) && idx < 7; i++ {
		length |= uint64(heapID[idx]) << (8 * i)
		idx++
	}

	return offset, length, nil
}

// readHeapObject reads an object from a fractal heap direct block.
// Format of Direct Block (Section III.E of HDF5 spec):
//   - Signature "FHDB" (4 bytes)
//   - Version (1 byte)
//   - Heap Header Address (offsetSize bytes)
//   - Block Offset (heapOffsetSize bytes from heap header)
//   - Managed Objects Data (variable)
//   - Optional Checksum (4 bytes if ChecksumDirBlocks flag set)
func readHeapObject(r io.ReaderAt, blockAddr, offset, length uint64, sb *Superblock, header *fractalHeapHeaderRaw) ([]byte, error) {
	// Read direct block header to determine object data start
	// Header size: 4 (sig) + 1 (ver) + offsetSize (heap addr) + heapOffsetSize (block offset)
	headerSize := 4 + 1 + int(sb.OffsetSize) + int(header.HeapOffsetSize)
	headerBuf := make([]byte, headerSize+16) // Extra bytes for safety
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	n, err := r.ReadAt(headerBuf, int64(blockAddr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read failed at 0x%X: %w", blockAddr, err)
	}
	if n < headerSize {
		return nil, fmt.Errorf("direct block too short: got %d bytes, need %d", n, headerSize)
	}

	// Check signature
	if string(headerBuf[0:4]) != "FHDB" {
		return nil, fmt.Errorf("invalid direct block signature: %q", headerBuf[0:4])
	}

	// Skip version (1 byte)
	headerOffset := 5

	// Skip heap header address (offsetSize bytes)
	headerOffset += int(sb.OffsetSize)

	// Read block offset (heapOffsetSize bytes from heap header)
	// This is the offset of this block in heap space
	blockOffsetBytes := headerBuf[headerOffset : headerOffset+int(header.HeapOffsetSize)]
	var blockOffset uint64
	for i := 0; i < int(header.HeapOffsetSize); i++ {
		blockOffset |= uint64(blockOffsetBytes[i]) << (8 * i)
	}
	headerOffset += int(header.HeapOffsetSize)

	// Calculate relative offset: object offset (in heap space) - block offset (in heap space)
	if offset < blockOffset {
		return nil, fmt.Errorf("object offset 0x%X before block offset 0x%X", offset, blockOffset)
	}
	relativeOffset := offset - blockOffset

	// Now we're at the start of managed objects data
	// Read the object at the relative offset within this block
	//nolint:gosec // G115: headerOffset bounded by header size specification
	objectAddr := blockAddr + uint64(headerOffset) + relativeOffset
	objectData := make([]byte, length)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	n, err = r.ReadAt(objectData, int64(objectAddr))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read object failed at 0x%X: %w", objectAddr, err)
	}
	//nolint:gosec // G115: Safe comparison, length bounded by heap object size
	if uint64(n) < length {
		return nil, fmt.Errorf("object data too short: got %d bytes, expected %d", n, length)
	}

	return objectData, nil
}

// EncodeAttributeFromStruct encodes an Attribute struct to bytes (attribute message format).
//
// This is a convenience wrapper around EncodeAttributeMessage (from messages_write.go)
// that takes an Attribute struct instead of individual parameters.
//
// Parameters:
//   - attr: Attribute to encode
//   - sb: Superblock for endianness (currently unused as we use little-endian)
//
// Returns:
//   - []byte: Encoded attribute message
//   - error: Non-nil if encoding fails
//
// Reference: H5Oattr.c - H5O__attr_encode().
func EncodeAttributeFromStruct(attr *Attribute, sb *Superblock) ([]byte, error) {
	if attr == nil {
		return nil, fmt.Errorf("attribute is nil")
	}

	if attr.Name == "" {
		return nil, fmt.Errorf("attribute name cannot be empty")
	}

	if attr.Datatype == nil {
		return nil, fmt.Errorf("attribute datatype is nil")
	}

	if attr.Dataspace == nil {
		return nil, fmt.Errorf("attribute dataspace is nil")
	}

	// Use the existing EncodeAttributeMessage from messages_write.go
	// It handles all the encoding logic according to HDF5 spec
	_ = sb // Currently unused, encoding is always little-endian

	return EncodeAttributeMessage(attr.Name, attr.Datatype, attr.Dataspace, attr.Data)
}

// ParseAttributeInfoMessage parses an Attribute Info Message (0x000F).
// Reference: H5Oainfo.c in C library.
// Format:
// - Version (1 byte)
// - Flags (1 byte):
//   - Bit 0: Track creation order
//   - Bit 1: Index creation order
//
// - Max Creation Index (2 bytes) - only if track creation order flag set
// - Fractal Heap Address (variable, based on superblock offset size)
// - B-tree Name Index Address (variable)
// - B-tree Order Index Address (variable) - only if index creation order flag set.
func ParseAttributeInfoMessage(data []byte, sb *Superblock) (*AttributeInfoMessage, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("attribute info message too short: %d bytes", len(data))
	}

	msg := &AttributeInfoMessage{}
	offset := 0

	// Version
	msg.Version = data[offset]
	offset++

	// Flags
	msg.Flags = data[offset]
	offset++

	trackCreationOrder := (msg.Flags & 0x01) != 0
	indexCreationOrder := (msg.Flags & 0x02) != 0

	// Max Compact and Min Dense (2 bytes each) - only if creation order tracked
	if trackCreationOrder {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("attribute info message too short for max compact/min dense")
		}
		msg.MaxCreationIndex = uint64(sb.Endianness.Uint16(data[offset : offset+2]))
		offset += 2
		// Skip Min Dense (2 bytes) - not stored in struct
		offset += 2
	}

	// Fractal Heap Address (offset size bytes)
	offsetSize := int(sb.OffsetSize)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for fractal heap address")
	}
	msg.FractalHeapAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Name Index Address (offset size bytes)
	if offset+offsetSize > len(data) {
		return nil, fmt.Errorf("attribute info message too short for btree name index address")
	}
	msg.BTreeNameIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	offset += offsetSize

	// B-tree Order Index Address (offset size bytes) - only if indexed
	if indexCreationOrder {
		if offset+offsetSize > len(data) {
			return nil, fmt.Errorf("attribute info message too short for btree order index address")
		}
		msg.BTreeOrderIndexAddr = readAddress(data[offset:offset+offsetSize], offsetSize)
	}

	return msg, nil
}

// EncodeAttributeInfoMessage encodes Attribute Info Message for writing.
//
// This implements the encoding logic matching the C reference H5Oainfo.c:H5O__ainfo_encode().
//
// Format:
// - Version (1 byte): 0
// - Flags (1 byte): creation order tracking/indexing
// - Max Compact (2 bytes): Optional, if bit 0 of flags set
// - Min Dense (2 bytes): Optional, if bit 0 of flags set
// - Fractal Heap Address (8 bytes)
// - B-tree Name Index Address (8 bytes)
// - B-tree Order Index Address (8 bytes): Optional, if bit 1 of flags set
//
// Parameters:
//   - aim: AttributeInfoMessage to encode
//   - sb: Superblock for endianness and offset size
//
// Returns:
//   - []byte: Encoded message
//   - error: Non-nil if encoding fails
//
// Reference: H5Oainfo.c - H5O__ainfo_encode().
func EncodeAttributeInfoMessage(aim *AttributeInfoMessage, sb *Superblock) ([]byte, error) {
	if aim == nil {
		return nil, fmt.Errorf("attribute info message is nil")
	}

	// Calculate size
	size := 2 // Version (1) + Flags (1)

	trackCreationOrder := (aim.Flags & 0x01) != 0
	indexCreationOrder := (aim.Flags & 0x02) != 0

	// Max Compact/Min Dense (2 bytes each) if creation order tracked
	if trackCreationOrder {
		size += 4 // 2 for max compact + 2 for min dense
	}

	// Heap address (offsetSize bytes)
	offsetSize := int(sb.OffsetSize)
	size += offsetSize

	// B-tree name index address (offsetSize bytes)
	size += offsetSize

	// B-tree order index address (offsetSize bytes) if indexed
	if indexCreationOrder {
		size += offsetSize
	}

	buf := make([]byte, size)
	offset := 0

	// Version
	buf[offset] = aim.Version
	offset++

	// Flags
	buf[offset] = aim.Flags
	offset++

	// Max Compact/Min Dense if creation order tracked
	if trackCreationOrder {
		// For MVP: these are not used (Flags = 0), but encoding should support them
		sb.Endianness.PutUint16(buf[offset:], uint16(aim.MaxCreationIndex)) //nolint:gosec // Safe: validated range
		offset += 2

		// Min Dense (not stored in struct, use 0)
		sb.Endianness.PutUint16(buf[offset:], 0)
		offset += 2
	}

	// Fractal Heap Address
	writeAddress(buf[offset:], aim.FractalHeapAddr, offsetSize, sb.Endianness)
	offset += offsetSize

	// B-tree Name Index Address
	writeAddress(buf[offset:], aim.BTreeNameIndexAddr, offsetSize, sb.Endianness)
	offset += offsetSize

	// B-tree Order Index Address if indexed
	if indexCreationOrder {
		writeAddress(buf[offset:], aim.BTreeOrderIndexAddr, offsetSize, sb.Endianness)
	}

	return buf, nil
}

// writeAddress writes an address to buffer with specified size and endianness.
// Helper function for encoding variable-size addresses.
func writeAddress(buf []byte, addr uint64, size int, endianness binary.ByteOrder) {
	switch size {
	case 1:
		buf[0] = byte(addr)
	case 2:
		endianness.PutUint16(buf, uint16(addr)) //nolint:gosec // Safe: size limited to 2 bytes
	case 4:
		endianness.PutUint32(buf, uint32(addr)) //nolint:gosec // Safe: size limited to 4 bytes
	case 8:
		endianness.PutUint64(buf, addr)
	}
}
