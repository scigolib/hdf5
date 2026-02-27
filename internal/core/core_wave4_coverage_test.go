package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// readVariableLengthString — full success path through Global Heap
// ---------------------------------------------------------------------------

// TestReadVariableLengthString_SuccessfulRead tests the full happy path:
// builds a valid Global Heap collection in memory, then reads a vlen string
// attribute element that references an object in that collection.
func TestReadVariableLengthString_SuccessfulRead(t *testing.T) {
	t.Parallel()

	// Build a complete in-memory file with a Global Heap collection.
	// Layout:
	//   0x0000 .. 0x00FF : padding (not used)
	//   0x0100 : Global Heap Collection ("GCOL") containing one string object
	const heapAddr = uint64(0x0100)

	buf := make([]byte, 0x0300)

	// --- GCOL at 0x0100 ---
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")                        // signature
	gcol[4] = 1                                    // version
	gcol[5] = 0                                    // reserved
	gcol[6] = 0                                    //
	gcol[7] = 0                                    //
	binary.LittleEndian.PutUint64(gcol[8:16], 256) // collection size

	// Object 1 in the collection: "hello world"
	stringData := []byte("hello world\x00") // null-terminated
	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)   // object ID = 1
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0) // nrefs
	// reserved 4 bytes at [objOffset+4:objOffset+8]
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], uint64(len(stringData))) // object size
	copy(gcol[objOffset+16:], stringData)

	reader := bytes.NewReader(buf)

	attr := &Attribute{
		Name: "vlen_success",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		reader:     reader,
		offsetSize: 8,
	}

	// Build vlen reference data:
	//   length (4 bytes) + heap address (8 bytes) + object index (4 bytes)
	refData := make([]byte, 16)
	binary.LittleEndian.PutUint32(refData[0:4], uint32(len(stringData))) // length
	binary.LittleEndian.PutUint64(refData[4:12], heapAddr)               // heap address
	binary.LittleEndian.PutUint32(refData[12:16], 1)                     // object index

	str, err := attr.readVariableLengthString(refData)
	require.NoError(t, err)
	require.Equal(t, "hello world", str)
}

// TestReadVariableLengthString_InvalidHeap tests error when the global heap
// collection at the referenced address has an invalid signature.
func TestReadVariableLengthString_InvalidHeap(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 0x0200)
	copy(buf[0x0100:0x0104], "XXXX") // invalid signature at 0x0100

	reader := bytes.NewReader(buf)

	attr := &Attribute{
		reader:     reader,
		offsetSize: 8,
	}

	refData := make([]byte, 16)
	binary.LittleEndian.PutUint32(refData[0:4], 5)       // length
	binary.LittleEndian.PutUint64(refData[4:12], 0x0100) // heap address
	binary.LittleEndian.PutUint32(refData[12:16], 1)     // object index

	_, err := attr.readVariableLengthString(refData)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read global heap collection")
}

// TestReadVariableLengthString_InvalidObjectIndex tests error when the object
// index does not exist in the global heap collection.
func TestReadVariableLengthString_InvalidObjectIndex(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 0x0300)
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 256)

	// Object 1 exists.
	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0)
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], 4)
	copy(gcol[objOffset+16:objOffset+20], "test")

	reader := bytes.NewReader(buf)

	attr := &Attribute{
		reader:     reader,
		offsetSize: 8,
	}

	// Reference object index 99 -- does not exist.
	refData := make([]byte, 16)
	binary.LittleEndian.PutUint32(refData[0:4], 4)
	binary.LittleEndian.PutUint64(refData[4:12], 0x0100)
	binary.LittleEndian.PutUint32(refData[12:16], 99)

	_, err := attr.readVariableLengthString(refData)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get object")
}

// TestReadVariableLengthString_OffsetSize4_FullPath tests the full path
// with 4-byte offset size (not just null reference).
func TestReadVariableLengthString_OffsetSize4_FullPath(t *testing.T) {
	t.Parallel()

	// Heap at address 64 (0x40) in the buffer.
	const heapAddr = uint64(0x40)

	// For offsetSize=4, the GCOL header is:
	// sig(4) + ver(1) + reserved(3) + size(4) = 12 bytes.
	// First object starts at 8-byte aligned boundary = 16.
	buf := make([]byte, 0x0200)
	gcol := buf[0x40:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	// collection size as 4-byte value.
	binary.LittleEndian.PutUint32(gcol[8:12], 256)

	// Object 1: "Go HDF5"
	// Object header for offsetSize=4: ID(2) + nrefs(2) + reserved(4) + size(4) = 12 bytes.
	stringData := []byte("Go HDF5\x00")
	objOffset := 16                                                 // 8-byte aligned after 12-byte header
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)   // ID
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0) // nrefs
	// reserved 4 bytes at [objOffset+4:objOffset+8]
	binary.LittleEndian.PutUint32(gcol[objOffset+8:objOffset+12], uint32(len(stringData))) // size (4 bytes)
	copy(gcol[objOffset+12:], stringData)

	reader := bytes.NewReader(buf)

	attr := &Attribute{
		reader:     reader,
		offsetSize: 4,
	}

	// Reference: length(4) + heapAddr(4) + objectIndex(4) = 12 bytes.
	refData := make([]byte, 12)
	binary.LittleEndian.PutUint32(refData[0:4], uint32(len(stringData)))
	binary.LittleEndian.PutUint32(refData[4:8], uint32(heapAddr))
	binary.LittleEndian.PutUint32(refData[8:12], 1)

	str, err := attr.readVariableLengthString(refData)
	require.NoError(t, err)
	require.Equal(t, "Go HDF5", str)
}

// TestReadVariableLengthString_EmptyString tests reading a vlen string
// that is an empty string (zero-length data) from the global heap.
func TestReadVariableLengthString_EmptyString(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 0x0300)
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 256)

	// Object 1 with zero-size data.
	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0)
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], 0) // zero size

	reader := bytes.NewReader(buf)

	attr := &Attribute{
		reader:     reader,
		offsetSize: 8,
	}

	refData := make([]byte, 16)
	binary.LittleEndian.PutUint32(refData[0:4], 0)
	binary.LittleEndian.PutUint64(refData[4:12], 0x0100)
	binary.LittleEndian.PutUint32(refData[12:16], 1)

	str, err := attr.readVariableLengthString(refData)
	require.NoError(t, err)
	require.Equal(t, "", str)
}

// ---------------------------------------------------------------------------
// readDenseAttributes — end-to-end with in-memory BTHD + BTLF + FRHP + FHDB
// ---------------------------------------------------------------------------

// TestReadDenseAttributes_EndToEnd constructs a complete in-memory dense
// attribute storage (B-tree v2 header, leaf node, fractal heap header,
// and direct block) containing one attribute, and verifies the full
// readDenseAttributes pipeline.
func TestReadDenseAttributes_EndToEnd(t *testing.T) {
	t.Parallel()

	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Build a Version 3 attribute message for a scalar int32 named "density".
	attrMsg := buildWave4V3AttrMessage(t, "density", 12345)

	// We need a valid BTHD + BTLF + FRHP + FHDB.
	// Addresses in our memory buffer:
	//   0x0100 : BTHD (B-tree v2 header)
	//   0x0200 : BTLF (B-tree v2 leaf node)
	//   0x0300 : FRHP (Fractal Heap header)
	//   0x0500 : FHDB (Direct block with the attribute data)

	const bthdAddr = 0x0100
	const btlfAddr = 0x0200
	const frhpAddr = 0x0300
	const fhdbAddr = 0x0500

	buf := make([]byte, 0x0800)

	// --- BTHD at 0x0100 ---
	bthd := buf[bthdAddr:]
	copy(bthd[0:4], "BTHD")
	bthd[4] = 0                                          // version
	bthd[5] = 8                                          // type (attribute name index)
	binary.LittleEndian.PutUint32(bthd[6:10], 4096)      // node size
	binary.LittleEndian.PutUint16(bthd[10:12], 11)       // record size (4 hash + 7 heapID)
	binary.LittleEndian.PutUint16(bthd[12:14], 0)        // depth (0 = leaf root)
	bthd[14] = 75                                        // split %
	bthd[15] = 40                                        // merge %
	binary.LittleEndian.PutUint64(bthd[16:24], btlfAddr) // root node address
	binary.LittleEndian.PutUint16(bthd[24:26], 1)        // 1 record in root
	binary.LittleEndian.PutUint64(bthd[26:34], 1)        // 1 total record

	// --- BTLF at 0x0200 ---
	btlf := buf[btlfAddr:]
	copy(btlf[0:4], "BTLF")
	btlf[4] = 0 // version
	btlf[5] = 8 // type
	// Record: hash(4) + heapID(7)
	offset := 6
	binary.LittleEndian.PutUint32(btlf[offset:offset+4], 0xAABBCCDD) // name hash
	offset += 4
	// Heap ID for managed object: type=0 (bits 4-5 of byte 0), offset in heap, length.
	// We need HeapOffsetSize and HeapLengthSize from the FRHP header.
	// With MaxHeapSize=16 => HeapOffsetSize = ceil(16/8) = 2.
	// With MaxDirectBlockSize=4096 and MaxManagedObjSize=512 =>
	//   HeapLengthSize = min(computeOffsetSize(4096), computeOffsetSize(512))
	//   = min(2, 2) = 2.
	// So heap ID: byte0(type=0) + 2-byte offset + 2-byte length + 2 unused = 7 bytes.
	heapIDBytes := [7]byte{}
	heapIDBytes[0] = 0x00 // type=0 (managed)
	// Offset = 0 (start of direct block data).
	heapIDBytes[1] = 0
	heapIDBytes[2] = 0
	// Length = len(attrMsg).
	heapIDBytes[3] = byte(len(attrMsg) & 0xFF)
	heapIDBytes[4] = byte((len(attrMsg) >> 8) & 0xFF)
	// Unused padding.
	heapIDBytes[5] = 0
	heapIDBytes[6] = 0
	copy(btlf[offset:offset+7], heapIDBytes[:])

	// --- FRHP at 0x0300 ---
	frhp := buf[frhpAddr:]
	copy(frhp[0:4], "FRHP")
	frhp[4] = 0                                     // version
	binary.LittleEndian.PutUint16(frhp[5:7], 7)     // Heap ID Length
	binary.LittleEndian.PutUint16(frhp[7:9], 0)     // I/O Filters Encoded Length
	frhp[9] = 0x00                                  // flags (no checksum)
	binary.LittleEndian.PutUint32(frhp[10:14], 512) // Max Managed Object Size
	// offset 110: Table Width.
	binary.LittleEndian.PutUint16(frhp[110:112], 4)
	// offset 112: Starting Block Size (8 bytes).
	binary.LittleEndian.PutUint64(frhp[112:120], 4096)
	// offset 120: Max Direct Block Size (8 bytes).
	binary.LittleEndian.PutUint64(frhp[120:128], 4096)
	// offset 128: Max Heap Size (2 bytes) -- log2 of max heap size.
	binary.LittleEndian.PutUint16(frhp[128:130], 16)
	// offset 132: Root Block Address (8 bytes).
	binary.LittleEndian.PutUint64(frhp[132:140], fhdbAddr)

	// --- FHDB at 0x0500 ---
	fhdb := buf[fhdbAddr:]
	copy(fhdb[0:4], "FHDB")
	fhdb[4] = 0 // version
	// Heap header address (8 bytes).
	binary.LittleEndian.PutUint64(fhdb[5:13], frhpAddr)
	// Block offset in heap space (HeapOffsetSize=2 bytes).
	binary.LittleEndian.PutUint16(fhdb[13:15], 0) // block starts at heap offset 0
	// Attribute message data starts at offset 15 in the direct block.
	copy(fhdb[15:], attrMsg)

	reader := bytes.NewReader(buf)

	attrInfo := &AttributeInfoMessage{
		FractalHeapAddr:    frhpAddr,
		BTreeNameIndexAddr: bthdAddr,
	}

	attrs, err := readDenseAttributes(reader, attrInfo, sb)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	require.Equal(t, "density", attrs[0].Name)
}

// TestReadDenseAttributes_InvalidHeapHeader tests error when fractal heap
// header has an invalid signature.
func TestReadDenseAttributes_InvalidHeapHeader(t *testing.T) {
	t.Parallel()

	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	const bthdAddr = 0x0100
	const btlfAddr = 0x0200
	const frhpAddr = 0x0300

	buf := make([]byte, 0x0500)

	// Valid BTHD.
	bthd := buf[bthdAddr:]
	copy(bthd[0:4], "BTHD")
	bthd[4] = 0
	bthd[5] = 8
	binary.LittleEndian.PutUint32(bthd[6:10], 4096)
	binary.LittleEndian.PutUint16(bthd[10:12], 11)
	binary.LittleEndian.PutUint16(bthd[12:14], 0)
	bthd[14] = 75
	bthd[15] = 40
	binary.LittleEndian.PutUint64(bthd[16:24], btlfAddr)
	binary.LittleEndian.PutUint16(bthd[24:26], 1) // 1 record
	binary.LittleEndian.PutUint64(bthd[26:34], 1)

	// Valid BTLF with 1 record.
	btlf := buf[btlfAddr:]
	copy(btlf[0:4], "BTLF")
	btlf[4] = 0
	btlf[5] = 8
	offset := 6
	binary.LittleEndian.PutUint32(btlf[offset:offset+4], 0x11111111)
	offset += 4
	copy(btlf[offset:offset+7], make([]byte, 7))

	// Invalid FRHP.
	copy(buf[frhpAddr:frhpAddr+4], "XXXX")

	reader := bytes.NewReader(buf)

	attrInfo := &AttributeInfoMessage{
		FractalHeapAddr:    frhpAddr,
		BTreeNameIndexAddr: bthdAddr,
	}

	_, err := readDenseAttributes(reader, attrInfo, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read heap header")
}

// ---------------------------------------------------------------------------
// CollectAllChunks — recursive non-leaf path
// ---------------------------------------------------------------------------

// TestCollectAllChunks_NonLeafNode tests CollectAllChunks on a level-1 node
// that has children pointing to leaf B-tree nodes in memory.
func TestCollectAllChunks_NonLeafNode(t *testing.T) {
	t.Parallel()

	// Memory layout:
	//   0x0000 : level-1 node (internal) with 2 children
	//   0x0200 : leaf node 0 with 2 chunk entries
	//   0x0400 : leaf node 1 with 1 chunk entry

	const leafAddr0 = uint64(0x0200)
	const leafAddr1 = uint64(0x0400)

	chunkDims := []uint64{10}
	ndims := 1
	offsetSize := uint8(8)
	keySize := 4 + 4 + ndims*8 // 4 nbytes + 4 filter_mask + ndims*8 coords

	// --- Build leaf node 0 at 0x0200: 2 entries ---
	leaf0 := buildWave4BTreeLeafNode(t, offsetSize, chunkDims, []wave4ChunkEntry{
		{byteOffsets: []uint64{0}, nbytes: 80, addr: 0xA000},
		{byteOffsets: []uint64{10}, nbytes: 80, addr: 0xA100},
	})
	// Final key for leaf0.
	leaf0FinalKey := make([]byte, keySize)
	binary.LittleEndian.PutUint32(leaf0FinalKey[0:4], 0)
	binary.LittleEndian.PutUint32(leaf0FinalKey[4:8], 0)
	binary.LittleEndian.PutUint64(leaf0FinalKey[8:16], 20)
	leaf0 = append(leaf0, leaf0FinalKey...)

	// --- Build leaf node 1 at 0x0400: 1 entry ---
	leaf1 := buildWave4BTreeLeafNode(t, offsetSize, chunkDims, []wave4ChunkEntry{
		{byteOffsets: []uint64{20}, nbytes: 80, addr: 0xB000},
	})
	leaf1FinalKey := make([]byte, keySize)
	binary.LittleEndian.PutUint32(leaf1FinalKey[0:4], 0)
	binary.LittleEndian.PutUint32(leaf1FinalKey[4:8], 0)
	binary.LittleEndian.PutUint64(leaf1FinalKey[8:16], 30)
	leaf1 = append(leaf1, leaf1FinalKey...)

	// Assemble the complete memory buffer.
	totalSize := 0x0600
	buf := make([]byte, totalSize)
	copy(buf[0x0200:], leaf0)
	copy(buf[0x0400:], leaf1)

	reader := bytes.NewReader(buf)

	// Create the level-1 node manually (it is the root, not read from file).
	rootNode := &BTreeV1Node{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    1, // Internal node.
		EntriesUsed:  2,
		LeftSibling:  0xFFFFFFFFFFFFFFFF,
		RightSibling: 0xFFFFFFFFFFFFFFFF,
		Keys: []ChunkKey{
			{Scaled: []uint64{0}, Nbytes: 80},
			{Scaled: []uint64{2}, Nbytes: 80},
			{Scaled: []uint64{3}, Nbytes: 0}, // Final key.
		},
		Children: []uint64{leafAddr0, leafAddr1},
	}

	chunks, err := rootNode.CollectAllChunks(reader, offsetSize, chunkDims)
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	require.Equal(t, uint64(0xA000), chunks[0].Address)
	require.Equal(t, uint64(0xA100), chunks[1].Address)
	require.Equal(t, uint64(0xB000), chunks[2].Address)
}

// TestCollectAllChunks_NonLeafInvalidChild tests error when a non-leaf node's
// child address points to an invalid B-tree node.
func TestCollectAllChunks_NonLeafInvalidChild(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 256)
	// Put garbage at 0x80 where the child should be.
	copy(buf[0x80:0x84], "XXXX")

	reader := bytes.NewReader(buf)

	rootNode := &BTreeV1Node{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    1,
		EntriesUsed:  1,
		LeftSibling:  0xFFFFFFFFFFFFFFFF,
		RightSibling: 0xFFFFFFFFFFFFFFFF,
		Keys: []ChunkKey{
			{Scaled: []uint64{0}, Nbytes: 80},
			{Scaled: []uint64{1}, Nbytes: 0},
		},
		Children: []uint64{0x80},
	}

	_, err := rootNode.CollectAllChunks(reader, 8, []uint64{10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse child node")
}

// TestCollectAllChunks_NonLeaf_2D tests recursive collection with 2D chunks.
func TestCollectAllChunks_NonLeaf_2D(t *testing.T) {
	t.Parallel()

	chunkDims := []uint64{10, 20}
	ndims := 2
	offsetSize := uint8(8)
	keySize := 4 + 4 + ndims*8

	const leafAddr = uint64(0x0200)

	// Build leaf with 1 entry.
	leaf := buildWave4BTreeLeafNode(t, offsetSize, chunkDims, []wave4ChunkEntry{
		{byteOffsets: []uint64{0, 0}, nbytes: 200, addr: 0xC000},
	})
	finalKey := make([]byte, keySize)
	binary.LittleEndian.PutUint32(finalKey[0:4], 0)
	binary.LittleEndian.PutUint32(finalKey[4:8], 0)
	binary.LittleEndian.PutUint64(finalKey[8:16], 10)
	binary.LittleEndian.PutUint64(finalKey[16:24], 0)
	leaf = append(leaf, finalKey...)

	buf := make([]byte, 0x0400)
	copy(buf[0x0200:], leaf)

	reader := bytes.NewReader(buf)

	rootNode := &BTreeV1Node{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    1,
		EntriesUsed:  1,
		LeftSibling:  0xFFFFFFFFFFFFFFFF,
		RightSibling: 0xFFFFFFFFFFFFFFFF,
		Keys: []ChunkKey{
			{Scaled: []uint64{0, 0}, Nbytes: 200},
			{Scaled: []uint64{1, 0}, Nbytes: 0},
		},
		Children: []uint64{leafAddr},
	}

	chunks, err := rootNode.CollectAllChunks(reader, offsetSize, chunkDims)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, uint64(0xC000), chunks[0].Address)
	require.Equal(t, uint64(0), chunks[0].Key.Scaled[0])
	require.Equal(t, uint64(0), chunks[0].Key.Scaled[1])
}

// ---------------------------------------------------------------------------
// ReadDatasetStrings — contiguous layout path
// ---------------------------------------------------------------------------

// TestReadDatasetStrings_ContiguousLayout tests ReadDatasetStrings with
// contiguous data layout where string data is at a specific file offset.
func TestReadDatasetStrings_ContiguousLayout(t *testing.T) {
	t.Parallel()

	// Store 3 fixed strings of 6 bytes each at offset 0x100 in a memory buffer.
	stringData := []byte("alpha\x00bravo\x00delta\x00")
	const dataAddr = uint64(0x0100)

	buf := make([]byte, 0x0200)
	copy(buf[0x0100:], stringData)

	reader := bytes.NewReader(buf)

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildWave4FixedStringDtMsg(6, 0), // 6 bytes, null-terminated
			},
			{
				Type: MsgDataspace,
				Data: buildWave4DataspaceMsg([]uint64{3}),
			},
			{
				Type: MsgDataLayout,
				Data: buildWave4ContiguousLayoutMsg(dataAddr, 18),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	data, err := ReadDatasetStrings(reader, header, sb)
	require.NoError(t, err)
	require.Len(t, data, 3)
	require.Equal(t, "alpha", data[0])
	require.Equal(t, "bravo", data[1])
	require.Equal(t, "delta", data[2])
}

// TestReadDatasetStrings_UnsupportedLayoutClass tests error when layout
// class is neither compact nor contiguous nor chunked.
func TestReadDatasetStrings_UnsupportedLayoutClass(t *testing.T) {
	t.Parallel()

	layoutMsg := make([]byte, 4)
	layoutMsg[0] = 3  // version 3
	layoutMsg[1] = 99 // invalid class

	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildWave4FixedStringDtMsg(5, 0),
			},
			{
				Type: MsgDataspace,
				Data: buildWave4DataspaceMsg([]uint64{2}),
			},
			{
				Type: MsgDataLayout,
				Data: layoutMsg,
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := ReadDatasetStrings(bytes.NewReader(nil), header, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported layout class")
}

// TestReadDatasetStrings_ContiguousReadError tests error when contiguous
// data read fails.
func TestReadDatasetStrings_ContiguousReadError(t *testing.T) {
	t.Parallel()

	// Address far beyond buffer bounds.
	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{
				Type: MsgDatatype,
				Data: buildWave4FixedStringDtMsg(5, 0),
			},
			{
				Type: MsgDataspace,
				Data: buildWave4DataspaceMsg([]uint64{2}),
			},
			{
				Type: MsgDataLayout,
				Data: buildWave4ContiguousLayoutMsg(0x99999, 10),
			},
		},
	}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := ReadDatasetStrings(bytes.NewReader(make([]byte, 64)), header, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read contiguous data")
}

// ---------------------------------------------------------------------------
// readVariableString (dataset_reader_compound.go) — full success path
// ---------------------------------------------------------------------------

// TestReadVariableString_FullSuccessPath tests the full success path of
// readVariableString from dataset_reader_compound.go, reading a string
// from an in-memory Global Heap collection.
func TestReadVariableString_FullSuccessPath(t *testing.T) {
	t.Parallel()

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	// Build a Global Heap Collection at address 0x100.
	const heapAddr = uint64(0x0100)
	stringVal := "compound_vlen"
	stringData := append([]byte(stringVal), 0) // null-terminated

	buf := make([]byte, 0x0300)
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 256)

	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0)
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], uint64(len(stringData)))
	copy(gcol[objOffset+16:], stringData)

	reader := bytes.NewReader(buf)

	// Global heap reference: heap_address (8 bytes) + object_index (4 bytes).
	refData := make([]byte, 12)
	binary.LittleEndian.PutUint64(refData[0:8], heapAddr)
	binary.LittleEndian.PutUint32(refData[8:12], 1)

	str, err := readVariableString(reader, refData, sb)
	require.NoError(t, err)
	require.Equal(t, stringVal, str)
}

// TestReadVariableString_FullSuccessPath_4ByteOffset tests the full path
// with 4-byte offsets.
func TestReadVariableString_FullSuccessPath_4ByteOffset(t *testing.T) {
	t.Parallel()

	sb := &Superblock{OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}

	const heapAddr = uint64(0x40)
	stringVal := "four_byte"
	stringData := append([]byte(stringVal), 0)

	buf := make([]byte, 0x200)
	gcol := buf[0x40:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint32(gcol[8:12], 256)

	// Object starts at 8-byte aligned offset after 12-byte header = offset 16.
	// Object header for offsetSize=4: ID(2) + nrefs(2) + reserved(4) + size(4) = 12 bytes.
	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)   // ID
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0) // nrefs
	// reserved 4 bytes at [objOffset+4:objOffset+8]
	binary.LittleEndian.PutUint32(gcol[objOffset+8:objOffset+12], uint32(len(stringData))) // size
	copy(gcol[objOffset+12:], stringData)

	reader := bytes.NewReader(buf)

	// With offsetSize=4: heap_address(4) + object_index(4) = 8 bytes.
	refData := make([]byte, 8)
	binary.LittleEndian.PutUint32(refData[0:4], uint32(heapAddr))
	binary.LittleEndian.PutUint32(refData[4:8], 1)

	str, err := readVariableString(reader, refData, sb)
	require.NoError(t, err)
	require.Equal(t, stringVal, str)
}

// TestReadVariableString_HeapCollectionError tests error when global heap
// collection is unreadable (invalid signature).
func TestReadVariableString_HeapCollectionError(t *testing.T) {
	t.Parallel()

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	buf := make([]byte, 0x200)
	copy(buf[0x100:0x104], "XXXX") // invalid signature

	refData := make([]byte, 12)
	binary.LittleEndian.PutUint64(refData[0:8], 0x100)
	binary.LittleEndian.PutUint32(refData[8:12], 1)

	_, err := readVariableString(bytes.NewReader(buf), refData, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read global heap collection")
}

// TestReadVariableString_ObjectNotFound tests error when object index
// does not exist in the collection.
func TestReadVariableString_ObjectNotFound(t *testing.T) {
	t.Parallel()

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	buf := make([]byte, 0x300)
	gcol := buf[0x100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 256)

	// Object with ID=1.
	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0)
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], 3)
	copy(gcol[objOffset+16:objOffset+19], "abc")

	// Request object index 42 -- does not exist.
	refData := make([]byte, 12)
	binary.LittleEndian.PutUint64(refData[0:8], 0x100)
	binary.LittleEndian.PutUint32(refData[8:12], 42)

	_, err := readVariableString(bytes.NewReader(buf), refData, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get object")
}

// ---------------------------------------------------------------------------
// Attribute ReadValue — VarLen string full path
// ---------------------------------------------------------------------------

// TestReadValue_VarLenString_FullPath tests the complete ReadValue path
// for a DatatypeVarLen scalar attribute that reads from a Global Heap.
func TestReadValue_VarLenString_FullPath(t *testing.T) {
	t.Parallel()

	// Build Global Heap with string "attribute_value".
	const heapAddr = uint64(0x0100)
	stringVal := "attribute_value"
	stringData := append([]byte(stringVal), 0)

	buf := make([]byte, 0x0300)
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 256)

	objOffset := 16
	binary.LittleEndian.PutUint16(gcol[objOffset:objOffset+2], 1)
	binary.LittleEndian.PutUint16(gcol[objOffset+2:objOffset+4], 0)
	binary.LittleEndian.PutUint64(gcol[objOffset+8:objOffset+16], uint64(len(stringData)))
	copy(gcol[objOffset+16:], stringData)

	reader := bytes.NewReader(buf)

	// offsetSize=8 => refSize = 8 + 8 = 16.
	// Each vlen element: length(4) + heapAddr(8) + objectIndex(4) = 16 bytes.
	refData := make([]byte, 16)
	binary.LittleEndian.PutUint32(refData[0:4], uint32(len(stringData)))
	binary.LittleEndian.PutUint64(refData[4:12], heapAddr)
	binary.LittleEndian.PutUint32(refData[12:16], 1)

	attr := &Attribute{
		Name: "vlen_attr",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01, // variable-length string
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceScalar,
			Dimensions: []uint64{},
		},
		Data:       refData,
		reader:     reader,
		offsetSize: 8,
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	require.Equal(t, stringVal, val)
}

// TestReadValue_VarLenStringArray tests ReadValue for an array of
// variable-length strings (2 elements).
func TestReadValue_VarLenStringArray(t *testing.T) {
	t.Parallel()

	// Build Global Heap with two strings in the same collection.
	const heapAddr = uint64(0x0100)
	string1 := "first"
	string2 := "second"
	stringData1 := append([]byte(string1), 0)
	stringData2 := append([]byte(string2), 0)

	buf := make([]byte, 0x0400)
	gcol := buf[0x0100:]
	copy(gcol[0:4], "GCOL")
	gcol[4] = 1
	binary.LittleEndian.PutUint64(gcol[8:16], 512)

	// Object 1.
	obj1Offset := 16
	binary.LittleEndian.PutUint16(gcol[obj1Offset:obj1Offset+2], 1)
	binary.LittleEndian.PutUint16(gcol[obj1Offset+2:obj1Offset+4], 0)
	binary.LittleEndian.PutUint64(gcol[obj1Offset+8:obj1Offset+16], uint64(len(stringData1)))
	copy(gcol[obj1Offset+16:], stringData1)

	// Object 2. Aligned to 8 bytes after object 1.
	alignedSize1 := len(stringData1)
	if alignedSize1%8 != 0 {
		alignedSize1 += 8 - (alignedSize1 % 8)
	}
	obj2Offset := obj1Offset + 16 + alignedSize1
	binary.LittleEndian.PutUint16(gcol[obj2Offset:obj2Offset+2], 2)
	binary.LittleEndian.PutUint16(gcol[obj2Offset+2:obj2Offset+4], 0)
	binary.LittleEndian.PutUint64(gcol[obj2Offset+8:obj2Offset+16], uint64(len(stringData2)))
	copy(gcol[obj2Offset+16:], stringData2)

	reader := bytes.NewReader(buf)

	// Build attribute data: 2 vlen references, each 16 bytes (offsetSize=8).
	refData := make([]byte, 32) // 2 * 16

	// Reference 1.
	binary.LittleEndian.PutUint32(refData[0:4], uint32(len(stringData1)))
	binary.LittleEndian.PutUint64(refData[4:12], heapAddr)
	binary.LittleEndian.PutUint32(refData[12:16], 1)

	// Reference 2.
	binary.LittleEndian.PutUint32(refData[16:20], uint32(len(stringData2)))
	binary.LittleEndian.PutUint64(refData[20:28], heapAddr)
	binary.LittleEndian.PutUint32(refData[28:32], 2)

	attr := &Attribute{
		Name: "vlen_array",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{2},
		},
		Data:       refData,
		reader:     reader,
		offsetSize: 8,
	}

	val, err := attr.ReadValue()
	require.NoError(t, err)
	arr, ok := val.([]string)
	require.True(t, ok, "expected []string, got %T", val)
	require.Len(t, arr, 2)
	require.Equal(t, string1, arr[0])
	require.Equal(t, string2, arr[1])
}

// TestReadValue_VarLenString_NoReader tests error when reader is nil.
func TestReadValue_VarLenString_NoReader(t *testing.T) {
	t.Parallel()

	attr := &Attribute{
		Name: "no_reader",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x01,
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data:       make([]byte, 16),
		reader:     nil, // no reader!
		offsetSize: 8,
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "variable-length attribute requires file reader")
}

// TestReadValue_VarLenNonString tests error for non-string VarLen type.
func TestReadValue_VarLenNonString(t *testing.T) {
	t.Parallel()

	attr := &Attribute{
		Name: "vlen_seq",
		Datatype: &DatatypeMessage{
			Class:         DatatypeVarLen,
			Size:          16,
			ClassBitField: 0x00, // type=0 (sequence), not string
		},
		Dataspace: &DataspaceMessage{
			Type:       DataspaceSimple,
			Dimensions: []uint64{1},
		},
		Data:       make([]byte, 16),
		reader:     bytes.NewReader(make([]byte, 256)),
		offsetSize: 8,
	}

	_, err := attr.ReadValue()
	require.Error(t, err)
	require.Contains(t, err.Error(), "variable-length non-string types not yet supported")
}

// ---------------------------------------------------------------------------
// convertToStrings — additional coverage
// ---------------------------------------------------------------------------

// TestConvertToStrings_VariableLengthError tests that variable-length string
// detection returns an appropriate error.
func TestConvertToStrings_VariableLengthError(t *testing.T) {
	t.Parallel()

	dt := &DatatypeMessage{
		Class:         DatatypeVarLen,
		Size:          16,
		ClassBitField: 0x01, // variable-length string
	}

	_, err := convertToStrings(make([]byte, 32), dt, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "variable-length strings not yet supported")
}

// TestConvertToStrings_UnknownStringType tests error for non-fixed,
// non-variable string class (DatatypeString with size=0).
func TestConvertToStrings_UnknownStringType(t *testing.T) {
	t.Parallel()

	// DatatypeString with size=0 => IsFixedString()=false, IsVariableString()=false.
	// Falls through to "unknown string type" error.
	dt := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          0,
		ClassBitField: 0x00,
	}

	_, err := convertToStrings(make([]byte, 0), dt, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown string type")
}

// ---------------------------------------------------------------------------
// Helper functions (wave4 prefix to avoid name conflicts)
// ---------------------------------------------------------------------------

// buildWave4V3AttrMessage builds a Version 3 attribute message for a scalar
// int32 attribute. This is similar to the existing buildV3AttrMessage but
// uses a distinct name to avoid test helper conflicts.
func buildWave4V3AttrMessage(t *testing.T, name string, value int32) []byte {
	t.Helper()

	nameBytes := append([]byte(name), 0)

	// Datatype: int32 (class 0, size 4).
	dtBytes := make([]byte, 8)
	dtBytes[0] = 0x30 // class=0 (fixed), version=3
	binary.LittleEndian.PutUint32(dtBytes[4:8], 4)

	// Dataspace: scalar (version 1, dimensionality 0).
	dsBytes := make([]byte, 8)
	dsBytes[0] = 1 // version
	dsBytes[1] = 0 // dimensionality = 0 (scalar)

	// Value.
	valBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(valBytes, uint32(value))
	headerSize := 9 // 1+1+2+2+2+1
	totalSize := headerSize + len(nameBytes) + len(dtBytes) + len(dsBytes) + len(valBytes)
	data := make([]byte, totalSize)
	offset := 0

	data[offset] = 3 // version
	offset++
	data[offset] = 0 // flags
	offset++
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(nameBytes)))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(dtBytes)))
	offset += 2
	binary.LittleEndian.PutUint16(data[offset:], uint16(len(dsBytes)))
	offset += 2
	data[offset] = 0 // name encoding (ASCII)
	offset++
	copy(data[offset:], nameBytes)
	offset += len(nameBytes)
	copy(data[offset:], dtBytes)
	offset += len(dtBytes)
	copy(data[offset:], dsBytes)
	offset += len(dsBytes)
	copy(data[offset:], valBytes)

	return data
}

// wave4ChunkEntry describes a chunk for building B-tree leaf nodes.
type wave4ChunkEntry struct {
	byteOffsets []uint64
	nbytes      uint32
	addr        uint64
}

// buildWave4BTreeLeafNode builds a serialized B-tree v1 leaf node with the
// given chunk entries. Caller must append the final key separately.
func buildWave4BTreeLeafNode(t *testing.T, offsetSize uint8, chunkDims []uint64, entries []wave4ChunkEntry) []byte {
	t.Helper()

	ndims := len(chunkDims)
	headerSize := 4 + 1 + 1 + 2 + int(offsetSize)*2

	var buf bytes.Buffer

	// Header.
	buf.WriteString("TREE")
	buf.WriteByte(1) // node type
	buf.WriteByte(0) // node level (leaf)
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))
	// Siblings (UNDEFINED).
	for i := 0; i < int(offsetSize); i++ {
		buf.WriteByte(0xFF)
	}
	for i := 0; i < int(offsetSize); i++ {
		buf.WriteByte(0xFF)
	}

	_ = headerSize

	// Entries: key + child for each.
	for _, entry := range entries {
		binary.Write(&buf, binary.LittleEndian, entry.nbytes)
		binary.Write(&buf, binary.LittleEndian, uint32(0))
		for j := 0; j < ndims; j++ {
			binary.Write(&buf, binary.LittleEndian, entry.byteOffsets[j])
		}
		// Child address.
		switch offsetSize {
		case 4:
			binary.Write(&buf, binary.LittleEndian, uint32(entry.addr))
		case 8:
			binary.Write(&buf, binary.LittleEndian, entry.addr)
		}
	}

	return buf.Bytes()
}

// buildWave4FixedStringDtMsg creates a fixed-length string datatype message.
func buildWave4FixedStringDtMsg(size uint32, paddingType uint8) []byte {
	data := make([]byte, 8)
	classBitField := uint32(paddingType & 0x0F)
	classAndVersion := uint32(DatatypeString) | (1 << 4) | (classBitField << 8)
	binary.LittleEndian.PutUint32(data[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(data[4:8], size)
	return data
}

// buildWave4DataspaceMsg creates a simple dataspace message (version 1).
func buildWave4DataspaceMsg(dims []uint64) []byte {
	data := make([]byte, 8+len(dims)*8)
	data[0] = 1
	data[1] = uint8(len(dims))
	data[2] = 0
	offset := 8
	for _, dim := range dims {
		binary.LittleEndian.PutUint64(data[offset:offset+8], dim)
		offset += 8
	}
	return data
}

// buildWave4ContiguousLayoutMsg creates a contiguous layout message (version 3).
func buildWave4ContiguousLayoutMsg(address, size uint64) []byte {
	data := make([]byte, 18)
	data[0] = 3
	data[1] = uint8(LayoutContiguous)
	binary.LittleEndian.PutUint64(data[2:10], address)
	binary.LittleEndian.PutUint64(data[10:18], size)
	return data
}
