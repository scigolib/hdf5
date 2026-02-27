package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// readChunkedData coverage: synthetic B-tree + chunk data tests
// ---------------------------------------------------------------------------

// TestReadChunkedData_Synthetic tests readChunkedData with a synthetic B-tree v1 in memory.
// This constructs a valid B-tree node + chunk data in a byte buffer and exercises
// readChunkedData, ParseBTreeV1Node, CollectAllChunks, and copyChunkToArray.
func TestReadChunkedData_Synthetic(t *testing.T) {
	// Create a 1D dataset: 8 float64 values, stored in 2 chunks of 4 values each.
	//
	// Dataset: [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0]
	// Chunk 0: [1.0, 2.0, 3.0, 4.0] at offset 0
	// Chunk 1: [5.0, 6.0, 7.0, 8.0] at offset 1

	chunkDims := []uint64{4}                            // Each chunk holds 4 elements.
	ndims := len(chunkDims) + 1                         // B-tree stores an extra dimension for element size.
	chunkDimsWithExtra := []uint64{4, 8}                // chunk_dim[0]=4 elements, chunk_dim[1]=8 bytes per element.
	elemSize := uint64(8)                               // float64 = 8 bytes
	chunkBytes := uint32(chunkDims[0] * elemSize)       // 32 bytes per chunk
	offsetSize := uint8(8)                              // 8-byte offsets
	keySize := 4 + 4 + ndims*8                          // 4 (nbytes) + 4 (filter_mask) + ndims*8 (coords)
	childSize := int(offsetSize)                        // 8 bytes per child address
	headerSize := 4 + 1 + 1 + 2 + int(offsetSize)*2     // TREE + type + level + entries + 2 siblings
	dataSize := 2*(keySize+childSize) + keySize         // 2 entries + final key (3 keys + 2 children)
	chunk0Offset := uint64(headerSize + dataSize + 256) // Place chunk data after B-tree + padding
	chunk1Offset := chunk0Offset + uint64(chunkBytes)   // Second chunk right after first

	// Build the entire file buffer.
	totalSize := int(chunk1Offset) + int(chunkBytes) + 256 // Extra padding
	buf := make([]byte, totalSize)

	// Write B-tree node at offset 0.
	btreeOffset := 0

	// Header: signature + type + level + entries + siblings.
	copy(buf[btreeOffset:btreeOffset+4], "TREE")
	buf[btreeOffset+4] = 1                                             // NodeType = 1 (chunk B-tree)
	buf[btreeOffset+5] = 0                                             // NodeLevel = 0 (leaf)
	binary.LittleEndian.PutUint16(buf[btreeOffset+6:btreeOffset+8], 2) // EntriesUsed = 2

	// Left sibling (UNDEFINED).
	for i := 0; i < int(offsetSize); i++ {
		buf[btreeOffset+8+i] = 0xFF
	}
	// Right sibling (UNDEFINED).
	for i := 0; i < int(offsetSize); i++ {
		buf[btreeOffset+8+int(offsetSize)+i] = 0xFF
	}

	// Write keys and children.
	off := btreeOffset + headerSize

	// Key 0: chunk at scaled position [0, 0], nbytes = chunkBytes.
	binary.LittleEndian.PutUint32(buf[off:off+4], chunkBytes) // nbytes
	off += 4
	binary.LittleEndian.PutUint32(buf[off:off+4], 0) // filter_mask
	off += 4
	// Coordinates as byte offsets: dim0_byte_offset=0*4=0, dim1_byte_offset=0*8=0.
	binary.LittleEndian.PutUint64(buf[off:off+8], 0) // coord[0] byte offset
	off += 8
	binary.LittleEndian.PutUint64(buf[off:off+8], 0) // coord[1] byte offset
	off += 8

	// Child 0: address of chunk 0.
	binary.LittleEndian.PutUint64(buf[off:off+8], chunk0Offset)
	off += 8

	// Key 1: chunk at scaled position [1, 0], nbytes = chunkBytes.
	binary.LittleEndian.PutUint32(buf[off:off+4], chunkBytes)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:off+4], 0) // filter_mask
	off += 4
	// coord[0] byte offset = 1 * chunkDimsWithExtra[0] = 1 * 4 = 4.
	binary.LittleEndian.PutUint64(buf[off:off+8], 1*chunkDimsWithExtra[0])
	off += 8
	binary.LittleEndian.PutUint64(buf[off:off+8], 0) // coord[1] byte offset
	off += 8

	// Child 1: address of chunk 1.
	binary.LittleEndian.PutUint64(buf[off:off+8], chunk1Offset)
	off += 8

	// Final key (key 2): sentinel.
	binary.LittleEndian.PutUint32(buf[off:off+4], 0) // nbytes = 0
	off += 4
	binary.LittleEndian.PutUint32(buf[off:off+4], 0) // filter_mask
	off += 4
	binary.LittleEndian.PutUint64(buf[off:off+8], 2*chunkDimsWithExtra[0])
	off += 8
	binary.LittleEndian.PutUint64(buf[off:off+8], 0)

	// Write chunk data.
	// Chunk 0: [1.0, 2.0, 3.0, 4.0].
	for i := 0; i < 4; i++ {
		bits := math.Float64bits(float64(i + 1))
		binary.LittleEndian.PutUint64(buf[int(chunk0Offset)+i*8:int(chunk0Offset)+i*8+8], bits)
	}
	// Chunk 1: [5.0, 6.0, 7.0, 8.0].
	for i := 0; i < 4; i++ {
		bits := math.Float64bits(float64(i + 5))
		binary.LittleEndian.PutUint64(buf[int(chunk1Offset)+i*8:int(chunk1Offset)+i*8+8], bits)
	}

	// Create layout, dataspace, datatype for readChunkedData.
	layout := &DataLayoutMessage{
		Version:     3,
		Class:       LayoutChunked,
		DataAddress: 0, // B-tree address (start of buffer)
		ChunkSize:   chunkDimsWithExtra,
	}

	dataspace := &DataspaceMessage{
		Type:       DataspaceSimple,
		Dimensions: []uint64{8}, // 8 total elements
	}

	datatype := &DatatypeMessage{
		Class:         DatatypeFloat,
		Size:          8,
		ClassBitField: 0x20, // Little-endian IEEE 754
	}

	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Call readChunkedData.
	rawData, err := readChunkedData(
		bytes.NewReader(buf),
		layout, dataspace, datatype, sb,
		nil, // No filter pipeline
	)
	require.NoError(t, err)
	require.Len(t, rawData, 64, "expected 8 float64 = 64 bytes")

	// Verify the values.
	for i := 0; i < 8; i++ {
		bits := binary.LittleEndian.Uint64(rawData[i*8 : i*8+8])
		value := math.Float64frombits(bits)
		require.InDelta(t, float64(i+1), value, 0.001, "element %d", i)
	}
}

// TestReadChunkedData_ErrorMissingBTree tests readChunkedData with invalid B-tree address.
func TestReadChunkedData_ErrorMissingBTree(t *testing.T) {
	layout := &DataLayoutMessage{
		Version:     3,
		Class:       LayoutChunked,
		DataAddress: 9999, // Invalid address
		ChunkSize:   []uint64{4, 8},
	}

	dataspace := &DataspaceMessage{
		Type:       DataspaceSimple,
		Dimensions: []uint64{8},
	}

	datatype := &DatatypeMessage{
		Class: DatatypeFloat,
		Size:  8,
	}

	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Small buffer that cannot contain valid B-tree.
	_, err := readChunkedData(
		bytes.NewReader(make([]byte, 100)),
		layout, dataspace, datatype, sb, nil,
	)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// readVariableLengthString coverage: test the method on Attribute
// ---------------------------------------------------------------------------

// TestReadVariableLengthString_NullReference tests that a null heap reference returns empty string.
func TestReadVariableLengthString_NullReference(t *testing.T) {
	// Create attribute with vlen string type.
	attr := &Attribute{
		Name: "vlen_test",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01, // VLen string
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		reader:     bytes.NewReader(make([]byte, 1024)),
		offsetSize: 8,
	}

	// Build vlen reference data with null heap address (all zeros).
	// Format: length (4 bytes) + heapAddress (8 bytes) + objectIndex (4 bytes)
	data := make([]byte, 16)
	// length = 0, heap address = 0, object index = 0 -> all zeros

	str, err := attr.readVariableLengthString(data)
	require.NoError(t, err)
	require.Equal(t, "", str, "null reference should return empty string")
}

// TestReadVariableLengthString_TooShort tests error on short data.
func TestReadVariableLengthString_TooShort(t *testing.T) {
	attr := &Attribute{
		Name: "vlen_test",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		reader:     bytes.NewReader(make([]byte, 1024)),
		offsetSize: 8,
	}

	// Data too short for expected size (4 + 8 + 4 = 16 bytes).
	shortData := make([]byte, 5)
	_, err := attr.readVariableLengthString(shortData)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

// TestReadVariableLengthString_OffsetSize4 tests with 4-byte offset.
func TestReadVariableLengthString_OffsetSize4(t *testing.T) {
	attr := &Attribute{
		Name: "vlen_test",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          12,
			ClassBitField: 0x01,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		reader:     bytes.NewReader(make([]byte, 1024)),
		offsetSize: 4,
	}

	// Format: length (4) + heapAddress (4) + objectIndex (4) = 12 bytes.
	// Null heap address.
	data := make([]byte, 12)
	str, err := attr.readVariableLengthString(data)
	require.NoError(t, err)
	require.Equal(t, "", str, "null reference should return empty string with 4-byte offsets")
}

// ---------------------------------------------------------------------------
// readDenseAttributes coverage
// ---------------------------------------------------------------------------

// TestReadDenseAttributes_NilInput tests error handling for nil inputs.
func TestReadDenseAttributes_NilInput(t *testing.T) {
	_, err := readDenseAttributes(nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

// TestReadDenseAttributes_ZeroAddresses tests error handling for zero addresses.
func TestReadDenseAttributes_ZeroAddresses(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	attrInfo := &AttributeInfoMessage{
		FractalHeapAddr:    0,
		BTreeNameIndexAddr: 0,
	}
	_, err := readDenseAttributes(bytes.NewReader(make([]byte, 256)), attrInfo, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid dense attribute addresses")
}

// TestReadDenseAttributes_InvalidBTreeSignature tests error when B-tree header is corrupt.
func TestReadDenseAttributes_InvalidBTreeSignature(t *testing.T) {
	// Create a fake file with garbage at the B-tree address.
	fakeData := make([]byte, 4096)
	copy(fakeData[100:104], "XXXX") // Invalid signature at address 100

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	attrInfo := &AttributeInfoMessage{
		FractalHeapAddr:    200,
		BTreeNameIndexAddr: 100,
	}

	_, err := readDenseAttributes(bytes.NewReader(fakeData), attrInfo, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "B-tree")
}

// TestReadDenseAttributes_RealFile tests dense attribute reading from a reference file.
func TestReadDenseAttributes_RealFile(t *testing.T) {
	// Dense attributes typically exist in files with many attributes (>8).
	f, err := os.Open("../../testdata/with_attributes.h5")
	if os.IsNotExist(err) {
		t.Skip("with_attributes.h5 not found")
	}
	require.NoError(t, err)
	defer f.Close()

	sb, err := ReadSuperblock(f)
	require.NoError(t, err)

	header, err := ReadObjectHeader(f, sb.RootGroup, sb)
	require.NoError(t, err)

	// Try to find AttributeInfo message (indicates dense storage).
	var hasAttrInfo bool
	for _, msg := range header.Messages {
		if msg.Type == MsgAttributeInfo {
			hasAttrInfo = true
			break
		}
	}

	if !hasAttrInfo {
		t.Skip("No dense attribute storage in this file (no AttributeInfo message)")
	}

	// If we have dense attributes, parse them via ParseAttributesFromMessages.
	attrs, err := ParseAttributesFromMessages(f, header.Messages, sb)
	require.NoError(t, err)
	require.NotEmpty(t, attrs, "expected attributes from dense storage")
	t.Logf("Found %d attributes (including dense)", len(attrs))
}

// ---------------------------------------------------------------------------
// ReadDatasetStrings coverage - additional tests
// ---------------------------------------------------------------------------

// TestReadDatasetStrings_FixedStringWithCompactLayout tests reading fixed strings from compact layout.
func TestReadDatasetStrings_FixedStringWithCompactLayout(t *testing.T) {
	// Build fixed string data: 3 strings, each 5 bytes, null-terminated.
	stringData := []byte("abcd\x00efgh\x00ijkl\x00")

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildCoverageFixedStringDatatypeMsg(5, 0), // 5 bytes, null-terminated
			},
			{
				Type: MsgDataspace,
				Data: buildCoverageSimpleDataspaceMsg([]uint64{3}),
			},
			{
				Type: MsgDataLayout,
				Data: buildCoverageCompactLayoutMsg(stringData),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetStrings(bytes.NewReader(nil), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 3)
	require.Equal(t, "abcd", data[0])
	require.Equal(t, "efgh", data[1])
	require.Equal(t, "ijkl", data[2])
}

// TestReadDatasetStrings_EmptyDataspace tests reading from empty dataspace.
func TestReadDatasetStrings_EmptyDataspace(t *testing.T) {
	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildCoverageFixedStringDatatypeMsg(10, 0),
			},
			{
				Type: MsgDataspace,
				Data: buildCoverageSimpleDataspaceMsg([]uint64{0}),
			},
			{
				Type: MsgDataLayout,
				Data: buildCoverageContiguousLayoutMsg(0x1000, 0),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetStrings(bytes.NewReader(nil), header, sb)
	require.NoError(t, err)
	require.Empty(t, data)
}

// TestReadDatasetStrings_SpacePadded tests reading space-padded strings.
func TestReadDatasetStrings_SpacePadded(t *testing.T) {
	// 2 strings, each 6 bytes, space-padded.
	stringData := []byte("hello world ")

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildCoverageFixedStringDatatypeMsg(6, 2), // 6 bytes, space-padded
			},
			{
				Type: MsgDataspace,
				Data: buildCoverageSimpleDataspaceMsg([]uint64{2}),
			},
			{
				Type: MsgDataLayout,
				Data: buildCoverageCompactLayoutMsg(stringData),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetStrings(bytes.NewReader(nil), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 2)
	require.Equal(t, "hello", data[0])
	require.Equal(t, "world", data[1])
}

// TestReadDatasetStrings_NullPadded tests reading null-padded strings.
func TestReadDatasetStrings_NullPadded(t *testing.T) {
	// 2 strings, each 5 bytes, null-padded.
	stringData := []byte("ab\x00\x00\x00cd\x00\x00\x00")

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildCoverageFixedStringDatatypeMsg(5, 1), // 5 bytes, null-padded
			},
			{
				Type: MsgDataspace,
				Data: buildCoverageSimpleDataspaceMsg([]uint64{2}),
			},
			{
				Type: MsgDataLayout,
				Data: buildCoverageCompactLayoutMsg(stringData),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetStrings(bytes.NewReader(nil), header, sb)
	require.NoError(t, err)
	require.Len(t, data, 2)
	require.Equal(t, "ab", data[0])
	require.Equal(t, "cd", data[1])
}

// ---------------------------------------------------------------------------
// CollectAllChunks coverage
// ---------------------------------------------------------------------------

// TestCollectAllChunks_LeafNode tests collecting chunks from a leaf B-tree node.
func TestCollectAllChunks_LeafNode(t *testing.T) {
	node := &BTreeV1Node{
		Signature:   [4]byte{'T', 'R', 'E', 'E'},
		NodeType:    1,
		NodeLevel:   0, // Leaf
		EntriesUsed: 3,
		Keys: []ChunkKey{
			{Scaled: []uint64{0}, Nbytes: 100, FilterMask: 0},
			{Scaled: []uint64{1}, Nbytes: 100, FilterMask: 0},
			{Scaled: []uint64{2}, Nbytes: 100, FilterMask: 0},
			{Scaled: []uint64{3}, Nbytes: 0, FilterMask: 0}, // Final key
		},
		Children: []uint64{0x1000, 0x2000, 0x3000},
	}

	chunks, err := node.CollectAllChunks(nil, 8, []uint64{10})
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	// Verify chunk entries.
	require.Equal(t, uint64(0x1000), chunks[0].Address)
	require.Equal(t, uint64(0x2000), chunks[1].Address)
	require.Equal(t, uint64(0x3000), chunks[2].Address)

	require.Equal(t, uint64(0), chunks[0].Key.Scaled[0])
	require.Equal(t, uint64(1), chunks[1].Key.Scaled[0])
	require.Equal(t, uint64(2), chunks[2].Key.Scaled[0])
}

// TestCollectAllChunks_EmptyNode tests collecting chunks from an empty node.
func TestCollectAllChunks_EmptyNode(t *testing.T) {
	node := &BTreeV1Node{
		Signature:   [4]byte{'T', 'R', 'E', 'E'},
		NodeType:    1,
		NodeLevel:   0,
		EntriesUsed: 0,
		Keys:        []ChunkKey{},
		Children:    []uint64{},
	}

	chunks, err := node.CollectAllChunks(nil, 8, []uint64{10})
	require.NoError(t, err)
	require.Empty(t, chunks)
}

// TestCollectAllChunks_MultiDimensional tests with 2D chunk coordinates.
func TestCollectAllChunks_MultiDimensional(t *testing.T) {
	node := &BTreeV1Node{
		Signature:   [4]byte{'T', 'R', 'E', 'E'},
		NodeType:    1,
		NodeLevel:   0,
		EntriesUsed: 2,
		Keys: []ChunkKey{
			{Scaled: []uint64{0, 0}, Nbytes: 200, FilterMask: 0},
			{Scaled: []uint64{0, 1}, Nbytes: 200, FilterMask: 0},
			{Scaled: []uint64{1, 0}, Nbytes: 0, FilterMask: 0}, // Final key
		},
		Children: []uint64{0xA000, 0xB000},
	}

	chunks, err := node.CollectAllChunks(nil, 8, []uint64{5, 5})
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, uint64(0xA000), chunks[0].Address)
	require.Equal(t, uint64(0xB000), chunks[1].Address)
}

// ---------------------------------------------------------------------------
// readVariableString coverage (from dataset_reader_compound.go)
// ---------------------------------------------------------------------------

// TestReadVariableString_NullRef tests readVariableString with null heap reference.
func TestReadVariableString_NullRef(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Build data with null heap address (all zeros): 8 bytes address + 4 bytes index.
	data := make([]byte, 12)

	str, err := readVariableString(bytes.NewReader(make([]byte, 1024)), data, sb)
	require.NoError(t, err)
	require.Equal(t, "", str)
}

// TestReadVariableString_InvalidHeapAddress tests readVariableString with invalid heap address.
func TestReadVariableString_InvalidHeapAddress(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Build data with non-null heap address pointing to invalid location.
	data := make([]byte, 12)
	binary.LittleEndian.PutUint64(data[0:8], 0x99999999) // Invalid address
	binary.LittleEndian.PutUint32(data[8:12], 1)         // Object index

	_, err := readVariableString(bytes.NewReader(make([]byte, 256)), data, sb)
	require.Error(t, err)
}

// TestReadVariableString_4ByteOffset tests readVariableString with 4-byte offsets.
func TestReadVariableString_4ByteOffset(t *testing.T) {
	sb := &Superblock{OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}

	// Null reference with 4-byte offsets: 4 bytes address + 4 bytes index.
	data := make([]byte, 8)

	str, err := readVariableString(bytes.NewReader(make([]byte, 256)), data, sb)
	require.NoError(t, err)
	require.Equal(t, "", str)
}

// ---------------------------------------------------------------------------
// EncodeDatatypeMessage coverage - additional types
// ---------------------------------------------------------------------------

// TestEncodeDatatypeMessage_AllClasses tests encoding various datatype classes.
func TestEncodeDatatypeMessage_AllClasses(t *testing.T) {
	tests := []struct {
		name    string
		dt      *DatatypeMessage
		wantErr bool
		errMsg  string
	}{
		{
			name: "int32",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				Version:       1,
				ClassBitField: 0x08, // Signed
			},
		},
		{
			name: "int64",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          8,
				Version:       1,
				ClassBitField: 0x08,
			},
		},
		{
			name: "float32",
			dt: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          4,
				Version:       1,
				ClassBitField: 0x00,
			},
		},
		{
			name: "float64",
			dt: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				Version:       1,
				ClassBitField: 0x00,
			},
		},
		{
			name: "fixed string",
			dt: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          10,
				Version:       1,
				ClassBitField: 0x00,
			},
		},
		{
			name: "zero size error",
			dt: &DatatypeMessage{
				Class:   DatatypeFixed,
				Size:    0,
				Version: 1,
			},
			wantErr: true,
			errMsg:  "size cannot be 0",
		},
		{
			name: "unsupported class",
			dt: &DatatypeMessage{
				Class:   DatatypeClass(99),
				Size:    4,
				Version: 1,
			},
			wantErr: true,
			errMsg:  "unsupported datatype class",
		},
		{
			name: "array via EncodeDatatypeMessage returns error",
			dt: &DatatypeMessage{
				Class:   DatatypeArray,
				Size:    16,
				Version: 1,
			},
			wantErr: true,
			errMsg:  "EncodeArrayDatatypeMessage",
		},
		{
			name: "enum via EncodeDatatypeMessage returns error",
			dt: &DatatypeMessage{
				Class:   DatatypeEnum,
				Size:    4,
				Version: 1,
			},
			wantErr: true,
			errMsg:  "EncodeEnumDatatypeMessage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeDatatypeMessage(tt.dt)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, encoded)
			require.GreaterOrEqual(t, len(encoded), 8, "encoded datatype must be at least 8 bytes")

			// Verify round-trip: parse the encoded bytes back.
			parsed, err := ParseDatatypeMessage(encoded)
			require.NoError(t, err)
			require.Equal(t, tt.dt.Class, parsed.Class)
			require.Equal(t, tt.dt.Size, parsed.Size)
		})
	}
}

// TestEncodeDatatypeMessage_VarLenString tests encoding variable-length string datatype.
func TestEncodeDatatypeMessage_VarLenString(t *testing.T) {
	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Size:          16, // Typical vlen string size in HDF5
		Version:       1,
		ClassBitField: 0x01, // String type
		Properties:    make([]byte, 8),
	}

	encoded, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	require.NotNil(t, encoded)
}

// ---------------------------------------------------------------------------
// ParseAttributeInfoMessage coverage
// ---------------------------------------------------------------------------

// TestParseAttributeInfoMessage_CreationOrder tests parsing with creation order flags.
func TestParseAttributeInfoMessage_CreationOrder(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	tests := []struct {
		name  string
		flags uint8
		build func() []byte
	}{
		{
			name:  "no creation order",
			flags: 0x00,
			build: func() []byte {
				// version(1) + flags(1) + heapAddr(8) + btreeNameAddr(8) = 18
				data := make([]byte, 18)
				data[0] = 0                                        // version
				data[1] = 0x00                                     // flags: no creation order
				binary.LittleEndian.PutUint64(data[2:10], 0x1000)  // heap addr
				binary.LittleEndian.PutUint64(data[10:18], 0x2000) // btree addr
				return data
			},
		},
		{
			name:  "with creation order tracking",
			flags: 0x01,
			build: func() []byte {
				// version(1) + flags(1) + maxCompact(2) + minDense(2) + heapAddr(8) + btreeNameAddr(8) = 22
				data := make([]byte, 22)
				data[0] = 0                                        // version
				data[1] = 0x01                                     // flags: track creation order
				binary.LittleEndian.PutUint16(data[2:4], 8)        // max compact
				binary.LittleEndian.PutUint16(data[4:6], 6)        // min dense
				binary.LittleEndian.PutUint64(data[6:14], 0x3000)  // heap addr
				binary.LittleEndian.PutUint64(data[14:22], 0x4000) // btree addr
				return data
			},
		},
		{
			name:  "with creation order tracking and indexing",
			flags: 0x03,
			build: func() []byte {
				// version(1) + flags(1) + maxCompact(2) + minDense(2) + heapAddr(8) + btreeNameAddr(8) + btreeOrderAddr(8) = 30
				data := make([]byte, 30)
				data[0] = 0    // version
				data[1] = 0x03 // flags: track + index creation order
				binary.LittleEndian.PutUint16(data[2:4], 8)
				binary.LittleEndian.PutUint16(data[4:6], 6)
				binary.LittleEndian.PutUint64(data[6:14], 0x5000)  // heap addr
				binary.LittleEndian.PutUint64(data[14:22], 0x6000) // btree name addr
				binary.LittleEndian.PutUint64(data[22:30], 0x7000) // btree order addr
				return data
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.build()
			msg, err := ParseAttributeInfoMessage(data, sb)
			require.NoError(t, err)
			require.NotNil(t, msg)
			require.Equal(t, tt.flags, msg.Flags)
			require.NotZero(t, msg.FractalHeapAddr)
			require.NotZero(t, msg.BTreeNameIndexAddr)

			if tt.flags&0x02 != 0 {
				require.NotZero(t, msg.BTreeOrderIndexAddr)
			}
		})
	}
}

// TestParseAttributeInfoMessage_TooShort tests error on truncated data.
func TestParseAttributeInfoMessage_TooShort(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, Endianness: binary.LittleEndian}
	_, err := ParseAttributeInfoMessage([]byte{0}, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

// ---------------------------------------------------------------------------
// computeOffsetSize coverage
// ---------------------------------------------------------------------------

// TestComputeOffsetSize_Coverage tests the offset size computation with more values.
func TestComputeOffsetSize_Coverage(t *testing.T) {
	tests := []struct {
		value    uint64
		expected uint8
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{0xFFFFFF, 3},
		{0x1000000, 4},
		{0xFFFFFFFF, 4},
		{0x100000000, 5},
	}

	for _, tt := range tests {
		result := computeOffsetSize(tt.value)
		require.Equal(t, tt.expected, result, "computeOffsetSize(%d)", tt.value)
	}
}

// ---------------------------------------------------------------------------
// parseHeapID coverage
// ---------------------------------------------------------------------------

// TestParseHeapID_UnsupportedType tests error on non-managed heap ID type.
func TestParseHeapID_UnsupportedType(t *testing.T) {
	heapHeader := &fractalHeapHeaderRaw{
		HeapOffsetSize: 4,
		HeapLengthSize: 2,
	}

	// Heap ID with type=1 (huge) instead of type=0 (managed).
	heapID := [7]byte{0x10, 0, 0, 0, 0, 0, 0} // Type 1 in bits 4-5
	_, _, err := parseHeapID(heapID, heapHeader)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported heap ID type")
}

// TestParseHeapID_Managed tests parsing managed heap ID.
func TestParseHeapID_Managed(t *testing.T) {
	heapHeader := &fractalHeapHeaderRaw{
		HeapOffsetSize: 4,
		HeapLengthSize: 2,
	}

	// Build managed heap ID: type=0, offset=0x100, length=0x50.
	var heapID [7]byte
	heapID[0] = 0x00 // Type 0 (managed)
	// Offset: 4 bytes, little-endian.
	heapID[1] = 0x00
	heapID[2] = 0x01
	heapID[3] = 0x00
	heapID[4] = 0x00
	// Length: 2 bytes, little-endian.
	heapID[5] = 0x50
	heapID[6] = 0x00

	offset, length, err := parseHeapID(heapID, heapHeader)
	require.NoError(t, err)
	require.Equal(t, uint64(0x100), offset)
	require.Equal(t, uint64(0x50), length)
}

// ---------------------------------------------------------------------------
// Helper functions for building test messages
// ---------------------------------------------------------------------------

// buildCoverageFixedStringDatatypeMsg creates a fixed-length string datatype message for tests.
func buildCoverageFixedStringDatatypeMsg(size uint32, paddingType uint8) []byte {
	data := make([]byte, 8)
	classBitField := uint32(paddingType & 0x0F)
	classAndVersion := uint32(DatatypeString) | (1 << 4) | (classBitField << 8)
	binary.LittleEndian.PutUint32(data[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(data[4:8], size)
	return data
}

// buildCoverageSimpleDataspaceMsg creates a simple dataspace message for tests.
// Version 1 format: version(1) + dimensionality(1) + flags(1) + reserved(5) + dims(N*8).
func buildCoverageSimpleDataspaceMsg(dims []uint64) []byte {
	data := make([]byte, 8+len(dims)*8)
	data[0] = 1 // Version 1
	data[1] = uint8(len(dims))
	data[2] = 0 // Flags (no max dims)
	// data[3..7] = reserved (zeros)
	offset := 8
	for _, dim := range dims {
		binary.LittleEndian.PutUint64(data[offset:offset+8], dim)
		offset += 8
	}
	return data
}

// buildCoverageContiguousLayoutMsg creates a contiguous layout message for tests.
func buildCoverageContiguousLayoutMsg(address, size uint64) []byte {
	data := make([]byte, 18)
	data[0] = 3                                        // Version 3
	data[1] = uint8(LayoutContiguous)                  // Contiguous
	binary.LittleEndian.PutUint64(data[2:10], address) // Address
	binary.LittleEndian.PutUint64(data[10:18], size)   // Size
	return data
}

// buildCoverageCompactLayoutMsg creates a compact layout message for tests.
func buildCoverageCompactLayoutMsg(compactData []byte) []byte {
	// Version 3, compact layout (class 0).
	// Format: version(1) + class(1) + size(2) + data
	data := make([]byte, 4+len(compactData))
	data[0] = 3                    // Version 3
	data[1] = uint8(LayoutCompact) // Compact
	binary.LittleEndian.PutUint16(data[2:4], uint16(len(compactData)))
	copy(data[4:], compactData)
	return data
}
