package structures

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// Mock writer for testing.
type mockChunkWriter struct {
	data map[uint64][]byte
}

func newMockChunkWriter() *mockChunkWriter {
	return &mockChunkWriter{
		data: make(map[uint64][]byte),
	}
}

func (m *mockChunkWriter) WriteAtAddress(data []byte, address uint64) error {
	// Make a copy to prevent external modification
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.data[address] = dataCopy
	return nil
}

func (m *mockChunkWriter) ReadAt(address uint64) []byte {
	return m.data[address]
}

// Mock allocator for testing.
type mockChunkAllocator struct {
	nextAddr uint64
}

func newMockChunkAllocator(startAddr uint64) *mockChunkAllocator {
	return &mockChunkAllocator{
		nextAddr: startAddr,
	}
}

func (m *mockChunkAllocator) Allocate(size uint64) (uint64, error) {
	addr := m.nextAddr
	m.nextAddr += size
	return addr, nil
}

// TestChunkBTreeWriter_1D tests 1D chunked dataset.
// Per C reference (H5Dbtree.c:687-690), B-tree keys store byte offsets
// (scaled * chunkDim), and have ndims+1 coordinates (last = datatype size, always 0).
func TestChunkBTreeWriter_1D(t *testing.T) {
	chunkDims := []uint64{10}
	elemSize := uint32(4) // int32
	writer := NewChunkBTreeWriter(1, chunkDims, elemSize)

	// Add 10 chunks (scaled indices 0..9)
	for i := uint64(0); i < 10; i++ {
		err := writer.AddChunk([]uint64{i}, 1000+i*100)
		require.NoError(t, err)
	}

	// Write B-tree
	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(5000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)
	require.Equal(t, uint64(5000), addr)

	// Verify written data
	data := mockWriter.ReadAt(addr)
	require.NotEmpty(t, data)

	// Parse header
	require.Equal(t, "TREE", string(data[0:4]))
	require.Equal(t, uint8(1), data[4]) // Node type = 1 (chunk)
	require.Equal(t, uint8(0), data[5]) // Node level = 0 (leaf)
	entriesUsed := binary.LittleEndian.Uint16(data[6:8])
	require.Equal(t, uint16(10), entriesUsed)

	leftSibling := binary.LittleEndian.Uint64(data[8:16])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), leftSibling)

	rightSibling := binary.LittleEndian.Uint64(data[16:24])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), rightSibling)

	// Parse interleaved keys and children
	// Format: key0, child0, key1, child1, ..., key9, child9, key10 (sentinel)
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1_elemsize (8) = 2 dims on disk
	pos := 24

	for i := 0; i < 11; i++ {
		// Read key
		nbytes := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		filterMask := binary.LittleEndian.Uint32(data[pos:])
		pos += 4

		// First coordinate (byte offset = scaled * chunkDim)
		coord0 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		// Second coordinate (trailing datatype size dimension)
		coord1 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8

		if i < 10 {
			require.Equal(t, uint32(0), nbytes, "chunk %d nbytes", i)
			// Byte offset = scaled_index * chunkDim[0] = i * 10
			require.Equal(t, uint64(i)*chunkDims[0], coord0, "chunk %d byte offset", i)
			// Trailing dimension always 0 for data keys
			require.Equal(t, uint64(0), coord1, "chunk %d trailing dim", i)
		} else {
			// Sentinel key: next chunk position after last entry (coord+1)*chunkDim.
			// For 10 chunks (indices 0-9) with chunkDim=10: sentinel=(9+1)*10=100.
			// Element size dim = 0.
			require.Equal(t, uint64(100), coord0, "sentinel key coord0") // (9+1)*10
			require.Equal(t, uint64(0), coord1, "sentinel key coord1")   // element size dim
		}
		require.Equal(t, uint32(0), filterMask)

		// Read child address (except for sentinel key)
		if i < 10 {
			chunkAddr := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			require.Equal(t, 1000+uint64(i)*100, chunkAddr, "chunk %d address", i)
		}
	}
}

// TestChunkBTreeWriter_2D tests 2D chunked dataset.
// Keys have 3 on-disk dimensions (2 data + 1 trailing datatype size).
func TestChunkBTreeWriter_2D(t *testing.T) {
	chunkDims := []uint64{10, 20}
	elemSize := uint32(8) // float64
	writer := NewChunkBTreeWriter(2, chunkDims, elemSize)

	// Add chunks in non-sorted order to test sorting
	chunks := []struct {
		coord []uint64
		addr  uint64
	}{
		{[]uint64{1, 0}, 2000}, // Should be sorted to position 2
		{[]uint64{0, 0}, 1000}, // Should be sorted to position 0
		{[]uint64{0, 1}, 1500}, // Should be sorted to position 1
		{[]uint64{1, 1}, 2500}, // Should be sorted to position 3
	}

	for _, chunk := range chunks {
		err := writer.AddChunk(chunk.coord, chunk.addr)
		require.NoError(t, err)
	}

	// Write B-tree
	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(10000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Verify sorting by reading interleaved keys and children.
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8) + coord2_trailing (8)
	// (3 on-disk dims: 2 data + 1 trailing datatype size)
	data := mockWriter.ReadAt(addr)
	pos := 24 // After header

	// Expected byte offsets: scaled * chunkDim
	expectedByteOffsets := [][]uint64{
		{0 * 10, 0 * 20}, {0 * 10, 1 * 20}, {1 * 10, 0 * 20}, {1 * 10, 1 * 20},
	}
	expectedAddrs := []uint64{1000, 1500, 2000, 2500}

	for i := 0; i < 5; i++ { // 4 entries + 1 sentinel
		// Read key: nbytes + filterMask + 3 coords
		pos += 4 // Skip nbytes
		pos += 4 // Skip filter mask
		coord0 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		coord1 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		coord2 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8

		if i < 4 {
			require.Equal(t, expectedByteOffsets[i][0], coord0, "key %d byte offset[0]", i)
			require.Equal(t, expectedByteOffsets[i][1], coord1, "key %d byte offset[1]", i)
			require.Equal(t, uint64(0), coord2, "key %d trailing dim should be 0", i)
		} else {
			// Sentinel: next position after last entry {1,1} → {2,2} in scaled,
			// byte offsets = {2*10, 2*20} = {20, 40}. Element size dim = 0.
			require.Equal(t, uint64(20), coord0, "sentinel coord0") // (1+1)*10
			require.Equal(t, uint64(40), coord1, "sentinel coord1") // (1+1)*20
			require.Equal(t, uint64(0), coord2, "sentinel coord2")  // element size dim
		}

		// Read child address (except for sentinel key)
		if i < len(expectedAddrs) {
			chunkAddr := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			require.Equal(t, expectedAddrs[i], chunkAddr, "chunk %d address", i)
		}
	}
}

// TestChunkBTreeWriter_3D tests 3D chunked dataset.
// Keys have 4 on-disk dimensions (3 data + 1 trailing datatype size).
func TestChunkBTreeWriter_3D(t *testing.T) {
	chunkDims := []uint64{5, 6, 5}
	elemSize := uint32(2) // uint16
	writer := NewChunkBTreeWriter(3, chunkDims, elemSize)

	// Add 8 chunks (2x2x2 cube)
	chunks := []struct {
		coord []uint64
		addr  uint64
	}{
		{[]uint64{0, 0, 0}, 1000},
		{[]uint64{0, 0, 1}, 1100},
		{[]uint64{0, 1, 0}, 1200},
		{[]uint64{0, 1, 1}, 1300},
		{[]uint64{1, 0, 0}, 1400},
		{[]uint64{1, 0, 1}, 1500},
		{[]uint64{1, 1, 0}, 1600},
		{[]uint64{1, 1, 1}, 1700},
	}

	for _, chunk := range chunks {
		err := writer.AddChunk(chunk.coord, chunk.addr)
		require.NoError(t, err)
	}

	// Write B-tree
	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(20000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Verify data structure
	data := mockWriter.ReadAt(addr)
	require.Equal(t, "TREE", string(data[0:4]))
	require.Equal(t, uint8(1), data[4]) // Chunk B-tree
	require.Equal(t, uint8(0), data[5]) // Leaf

	entriesUsed := binary.LittleEndian.Uint16(data[6:8])
	require.Equal(t, uint16(8), entriesUsed)

	// Verify all 8 chunks are present with interleaved keys and children.
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8) + coord2 (8) + coord3_trailing (8)
	// (4 on-disk dims: 3 data + 1 trailing)
	pos := 24

	for i := 0; i < 9; i++ { // 8 chunks + 1 sentinel
		pos += 4 // nbytes
		pos += 4 // filter mask
		_ = binary.LittleEndian.Uint64(data[pos:])
		pos += 8 // coord0
		_ = binary.LittleEndian.Uint64(data[pos:])
		pos += 8 // coord1
		_ = binary.LittleEndian.Uint64(data[pos:])
		pos += 8 // coord2
		trailingDim := binary.LittleEndian.Uint64(data[pos:])
		pos += 8 // coord3 (trailing datatype size dim)

		if i < 8 {
			require.Equal(t, uint64(0), trailingDim, "chunk %d trailing dim should be 0", i)
		}

		// Read child address (except for sentinel)
		if i < 8 {
			chunkAddr := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			require.Equal(t, uint64(1000+i*100), chunkAddr)
		}
	}
}

// TestCompareChunkCoords tests coordinate comparison.
func TestCompareChunkCoords(t *testing.T) {
	tests := []struct {
		name     string
		a        []uint64
		b        []uint64
		expected int
	}{
		// 1D cases
		{"1D equal", []uint64{5}, []uint64{5}, 0},
		{"1D less", []uint64{3}, []uint64{5}, -1},
		{"1D greater", []uint64{7}, []uint64{5}, 1},

		// 2D cases
		{"2D equal", []uint64{2, 3}, []uint64{2, 3}, 0},
		{"2D less dim0", []uint64{1, 5}, []uint64{2, 3}, -1},
		{"2D greater dim0", []uint64{3, 2}, []uint64{2, 5}, 1},
		{"2D less dim1", []uint64{2, 2}, []uint64{2, 3}, -1},
		{"2D greater dim1", []uint64{2, 5}, []uint64{2, 3}, 1},

		// 3D cases
		{"3D equal", []uint64{1, 2, 3}, []uint64{1, 2, 3}, 0},
		{"3D less dim0", []uint64{0, 5, 5}, []uint64{1, 2, 3}, -1},
		{"3D greater dim0", []uint64{2, 0, 0}, []uint64{1, 5, 5}, 1},
		{"3D less dim2", []uint64{1, 2, 2}, []uint64{1, 2, 3}, -1},

		// Row-major ordering verification
		{"row-major [0,0] < [0,1]", []uint64{0, 0}, []uint64{0, 1}, -1},
		{"row-major [0,1] < [1,0]", []uint64{0, 1}, []uint64{1, 0}, -1},
		{"row-major [1,0] < [1,1]", []uint64{1, 0}, []uint64{1, 1}, -1},
		{"row-major [2,5] > [2,4]", []uint64{2, 5}, []uint64{2, 4}, 1},
		{"row-major [1,10] < [2,0]", []uint64{1, 10}, []uint64{2, 0}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareChunkCoords(tt.a, tt.b)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestChunkBTreeWriter_Sorting tests that chunks are sorted correctly.
// Coordinates are stored as byte offsets on disk.
func TestChunkBTreeWriter_Sorting(t *testing.T) {
	chunkDims := []uint64{10, 20}
	elemSize := uint32(4)
	writer := NewChunkBTreeWriter(2, chunkDims, elemSize)

	// Add chunks in reverse order
	coords := [][]uint64{
		{3, 3}, {3, 2}, {3, 1}, {3, 0},
		{2, 3}, {2, 2}, {2, 1}, {2, 0},
		{1, 3}, {1, 2}, {1, 1}, {1, 0},
		{0, 3}, {0, 2}, {0, 1}, {0, 0},
	}

	for i, coord := range coords {
		err := writer.AddChunk(coord, uint64(1000+i*100))
		require.NoError(t, err)
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(30000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Verify chunks are sorted in row-major order.
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8) + coord2_trailing (8)
	data := mockWriter.ReadAt(addr)
	pos := 24

	// Expected order: [0,0], [0,1], [0,2], [0,3], [1,0], ...
	// On disk, coords are byte offsets: scaled * chunkDim.
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			pos += 4 // nbytes
			pos += 4 // filter mask
			coord0 := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			coord1 := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			_ = binary.LittleEndian.Uint64(data[pos:])
			pos += 8 // trailing dim
			pos += 8 // child address

			require.Equal(t, uint64(i)*chunkDims[0], coord0, "chunk [%d,%d] byte offset[0]", i, j)
			require.Equal(t, uint64(j)*chunkDims[1], coord1, "chunk [%d,%d] byte offset[1]", i, j)
		}
	}
}

// TestChunkBTreeWriter_EdgeChunks tests edge and corner chunks.
// Coordinates stored as byte offsets on disk.
func TestChunkBTreeWriter_EdgeChunks(t *testing.T) {
	chunkDims := []uint64{10, 20}
	elemSize := uint32(8)
	writer := NewChunkBTreeWriter(2, chunkDims, elemSize)

	// Add edge chunks (large scaled coordinates)
	chunks := []struct {
		coord []uint64
		addr  uint64
	}{
		{[]uint64{0, 0}, 1000},
		{[]uint64{100, 0}, 2000},
		{[]uint64{0, 200}, 3000},
		{[]uint64{100, 200}, 4000},
	}

	for _, chunk := range chunks {
		err := writer.AddChunk(chunk.coord, chunk.addr)
		require.NoError(t, err)
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(40000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Verify large coordinates are handled correctly.
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8) + coord2_trailing (8)
	data := mockWriter.ReadAt(addr)
	pos := 24

	// Expected byte offsets (scaled * chunkDim), sorted row-major
	expectedByteOffsets := [][]uint64{
		{0 * 10, 0 * 20}, {0 * 10, 200 * 20}, {100 * 10, 0 * 20}, {100 * 10, 200 * 20},
	}

	for i, expected := range expectedByteOffsets {
		pos += 4 // nbytes
		pos += 4 // filter mask
		coord0 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		coord1 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		_ = binary.LittleEndian.Uint64(data[pos:])
		pos += 8 // trailing dim
		pos += 8 // child address

		require.Equal(t, expected[0], coord0, "chunk %d byte offset[0]", i)
		require.Equal(t, expected[1], coord1, "chunk %d byte offset[1]", i)
	}
}

// TestChunkBTreeWriter_SingleChunk tests B-tree with single chunk.
func TestChunkBTreeWriter_SingleChunk(t *testing.T) {
	chunkDims := []uint64{100}
	elemSize := uint32(4)
	writer := NewChunkBTreeWriter(1, chunkDims, elemSize)

	err := writer.AddChunk([]uint64{0}, 5000)
	require.NoError(t, err)

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(10000)

	addr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	data := mockWriter.ReadAt(addr)
	require.NotEmpty(t, data)

	// Verify 1 entry + 1 sentinel = 2 keys
	entriesUsed := binary.LittleEndian.Uint16(data[6:8])
	require.Equal(t, uint16(1), entriesUsed)

	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1_trailing (8)
	// (2 on-disk dims: 1 data + 1 trailing)
	pos := 24

	// First key
	pos += 4 // nbytes
	pos += 4 // filter mask
	coord0 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0), coord0) // 0 * 100 = 0
	pos += 8
	coord1 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0), coord1) // trailing dim = 0
	pos += 8

	// Single child address
	chunkAddr := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(5000), chunkAddr)
	pos += 8

	// Sentinel key: next position after {0} → {1} in scaled, byte offset = 1*100 = 100.
	pos += 4 // nbytes
	pos += 4 // filter mask
	sentinel0 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(100), sentinel0) // (0+1)*100
	pos += 8
	sentinel1 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0), sentinel1) // element size dim
}

// TestChunkBTreeWriter_ErrorCases tests error handling.
func TestChunkBTreeWriter_ErrorCases(t *testing.T) {
	t.Run("empty B-tree", func(t *testing.T) {
		writer := NewChunkBTreeWriter(1, []uint64{10}, 4)

		mockWriter := newMockChunkWriter()
		mockAllocator := newMockChunkAllocator(1000)

		_, err := writer.WriteToFile(mockWriter, mockAllocator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no chunks")
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		writer := NewChunkBTreeWriter(2, []uint64{10, 20}, 4)

		err := writer.AddChunk([]uint64{0, 0, 0}, 1000) // 3D coord for 2D writer
		require.Error(t, err)
		require.Contains(t, err.Error(), "dimensionality mismatch")
	})
}

// TestChunkBTreeWriter_MultiLevel_65Chunks tests the minimum multi-level case:
// 65 entries require 2 leaf nodes + 1 root internal node (level 1).
func TestChunkBTreeWriter_MultiLevel_65Chunks(t *testing.T) {
	chunkDims := []uint64{10}
	elemSize := uint32(4) // int32
	writer := NewChunkBTreeWriter(1, chunkDims, elemSize)

	// Add 65 chunks (scaled indices 0..64). This exceeds the 64-entry single leaf limit.
	for i := uint64(0); i < 65; i++ {
		err := writer.AddChunk([]uint64{i}, 1000+i*80)
		require.NoError(t, err)
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(100000)

	rootAddr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Root node should be an internal node (level 1).
	rootData := mockWriter.ReadAt(rootAddr)
	require.NotEmpty(t, rootData)
	require.Equal(t, "TREE", string(rootData[0:4]))
	require.Equal(t, uint8(1), rootData[4]) // Node type = chunk
	require.Equal(t, uint8(1), rootData[5]) // Node level = 1 (internal)
	rootEntries := binary.LittleEndian.Uint16(rootData[6:8])
	require.Equal(t, uint16(2), rootEntries) // 2 children (2 leaf nodes)

	// Parse root's children addresses.
	onDiskDims := 2 // 1D + trailing
	keySize := 4 + 4 + onDiskDims*8
	pos := 24

	// key[0], child[0], key[1], child[1], key[2] (sentinel)
	pos += keySize // skip key[0]
	child0Addr := binary.LittleEndian.Uint64(rootData[pos:])
	pos += 8
	pos += keySize // skip key[1]
	child1Addr := binary.LittleEndian.Uint64(rootData[pos:])

	// Both children should be leaf nodes (level 0).
	child0Data := mockWriter.ReadAt(child0Addr)
	require.NotEmpty(t, child0Data)
	require.Equal(t, "TREE", string(child0Data[0:4]))
	require.Equal(t, uint8(0), child0Data[5]) // level 0 = leaf
	child0Entries := binary.LittleEndian.Uint16(child0Data[6:8])
	require.Equal(t, uint16(64), child0Entries) // First leaf: 64 entries

	child1Data := mockWriter.ReadAt(child1Addr)
	require.NotEmpty(t, child1Data)
	require.Equal(t, "TREE", string(child1Data[0:4]))
	require.Equal(t, uint8(0), child1Data[5]) // level 0 = leaf
	child1Entries := binary.LittleEndian.Uint16(child1Data[6:8])
	require.Equal(t, uint16(1), child1Entries) // Second leaf: 1 entry

	// Verify sibling links: child0.right = child1, child1.left = child0.
	child0RightSib := binary.LittleEndian.Uint64(child0Data[16:24])
	require.Equal(t, child1Addr, child0RightSib, "child0 right sibling should point to child1")
	child1LeftSib := binary.LittleEndian.Uint64(child1Data[8:16])
	require.Equal(t, child0Addr, child1LeftSib, "child1 left sibling should point to child0")

	// Outer siblings should be UNDEF.
	child0LeftSib := binary.LittleEndian.Uint64(child0Data[8:16])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), child0LeftSib)
	child1RightSib := binary.LittleEndian.Uint64(child1Data[16:24])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), child1RightSib)

	// Total entries across leaves = 64 + 1 = 65.
	require.Equal(t, uint16(65), child0Entries+child1Entries)
}

// TestChunkBTreeWriter_MultiLevel_100Chunks tests multi-level with 100 entries
// (2 leaf nodes: 64 + 36).
func TestChunkBTreeWriter_MultiLevel_100Chunks(t *testing.T) {
	chunkDims := []uint64{8}
	elemSize := uint32(4)
	writer := NewChunkBTreeWriter(1, chunkDims, elemSize)

	for i := uint64(0); i < 100; i++ {
		err := writer.AddChunk([]uint64{i}, 2000+i*40)
		require.NoError(t, err)
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(50000)

	rootAddr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Root should be internal (level 1) with 2 children.
	rootData := mockWriter.ReadAt(rootAddr)
	require.Equal(t, uint8(1), rootData[5]) // level 1
	rootEntries := binary.LittleEndian.Uint16(rootData[6:8])
	require.Equal(t, uint16(2), rootEntries)

	// Parse children and verify entry distribution.
	onDiskDims := 2
	keySize := 4 + 4 + onDiskDims*8
	pos := 24 + keySize // skip key[0]
	child0Addr := binary.LittleEndian.Uint64(rootData[pos:])
	pos += 8 + keySize // skip child[0] + key[1]
	child1Addr := binary.LittleEndian.Uint64(rootData[pos:])

	child0Entries := binary.LittleEndian.Uint16(mockWriter.ReadAt(child0Addr)[6:8])
	child1Entries := binary.LittleEndian.Uint16(mockWriter.ReadAt(child1Addr)[6:8])

	require.Equal(t, uint16(100), child0Entries+child1Entries)
	require.Equal(t, uint16(64), child0Entries)
	require.Equal(t, uint16(36), child1Entries)
}

// TestChunkBTreeWriter_MultiLevel_200Chunks tests multi-level with 200 entries
// (4 leaf nodes: 64 + 64 + 64 + 8).
func TestChunkBTreeWriter_MultiLevel_200Chunks(t *testing.T) {
	chunkDims := []uint64{5}
	elemSize := uint32(8)
	writer := NewChunkBTreeWriter(1, chunkDims, elemSize)

	// Add 200 entries with known addresses.
	for i := uint64(0); i < 200; i++ {
		err := writer.AddChunkWithSize([]uint64{i}, 3000+i*40, 40)
		require.NoError(t, err)
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(80000)

	rootAddr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	// Root should be level 1 (internal) with 4 children (ceil(200/64) = 4).
	rootData := mockWriter.ReadAt(rootAddr)
	require.Equal(t, "TREE", string(rootData[0:4]))
	require.Equal(t, uint8(1), rootData[5]) // level 1
	rootEntries := binary.LittleEndian.Uint16(rootData[6:8])
	require.Equal(t, uint16(4), rootEntries)

	// Collect all leaf entries from all children and verify every chunk address is present.
	onDiskDims := 2 // 1D + trailing
	keySize := 4 + 4 + onDiskDims*8
	pos := 24

	totalLeafEntries := uint16(0)
	for i := 0; i < int(rootEntries); i++ {
		pos += keySize // skip key[i]
		childAddr := binary.LittleEndian.Uint64(rootData[pos:])
		pos += 8

		childData := mockWriter.ReadAt(childAddr)
		require.Equal(t, uint8(0), childData[5], "child %d should be leaf (level 0)", i)
		entries := binary.LittleEndian.Uint16(childData[6:8])
		totalLeafEntries += entries
	}

	require.Equal(t, uint16(200), totalLeafEntries, "total entries across all leaves should be 200")

	// Verify that leaf nodes have correct sibling chains.
	// Re-read children for sibling verification.
	pos = 24
	childAddrs := make([]uint64, rootEntries)
	for i := 0; i < int(rootEntries); i++ {
		pos += keySize
		childAddrs[i] = binary.LittleEndian.Uint64(rootData[pos:])
		pos += 8
	}

	for i, addr := range childAddrs {
		data := mockWriter.ReadAt(addr)
		leftSib := binary.LittleEndian.Uint64(data[8:16])
		rightSib := binary.LittleEndian.Uint64(data[16:24])

		if i == 0 {
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), leftSib, "first leaf left sibling should be UNDEF")
		} else {
			require.Equal(t, childAddrs[i-1], leftSib, "leaf %d left sibling mismatch", i)
		}

		if i == len(childAddrs)-1 {
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), rightSib, "last leaf right sibling should be UNDEF")
		} else {
			require.Equal(t, childAddrs[i+1], rightSib, "leaf %d right sibling mismatch", i)
		}
	}
}

// TestChunkBTreeWriter_MultiLevel_2D tests multi-level tree with 2D coordinates.
func TestChunkBTreeWriter_MultiLevel_2D(t *testing.T) {
	chunkDims := []uint64{10, 10}
	elemSize := uint32(4)
	writer := NewChunkBTreeWriter(2, chunkDims, elemSize)

	// Create a 10x10 grid of chunks = 100 total (exceeds 64 single-leaf limit).
	for i := uint64(0); i < 10; i++ {
		for j := uint64(0); j < 10; j++ {
			err := writer.AddChunk([]uint64{i, j}, 5000+(i*10+j)*400)
			require.NoError(t, err)
		}
	}

	mockWriter := newMockChunkWriter()
	mockAllocator := newMockChunkAllocator(200000)

	rootAddr, err := writer.WriteToFile(mockWriter, mockAllocator)
	require.NoError(t, err)

	rootData := mockWriter.ReadAt(rootAddr)
	require.Equal(t, uint8(1), rootData[5], "root should be internal (level 1)")
	rootEntries := binary.LittleEndian.Uint16(rootData[6:8])
	require.Equal(t, uint16(2), rootEntries, "100 entries should produce 2 leaf nodes")
}

// TestSerializeChunkBTreeNode tests serialization directly.
// On disk, keys have onDiskDims dimensions (ndims+1) and store byte offsets.
func TestSerializeChunkBTreeNode(t *testing.T) {
	// Keys already have onDiskDims=3 coords (2 data + 1 trailing)
	node := &ChunkBTreeNode{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    0,
		EntriesUsed:  2,
		LeftSibling:  0xFFFFFFFFFFFFFFFF,
		RightSibling: 0xFFFFFFFFFFFFFFFF,
		Keys: []ChunkKey{
			{Coords: []uint64{0, 0}, FilterMask: 0, Nbytes: 800},                                                     // scaled [0,0]
			{Coords: []uint64{1, 1}, FilterMask: 0, Nbytes: 800},                                                     // scaled [1,1]
			{Coords: []uint64{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}, FilterMask: 0, Nbytes: 0}, // Sentinel (3 dims)
		},
		ChildAddrs: []uint64{1000, 2000},
	}

	chunkDims := []uint64{10, 20}
	elemSize := uint32(8)
	onDiskDims := 3 // 2 data + 1 trailing

	buf := serializeChunkBTreeNode(node, onDiskDims, chunkDims, elemSize)

	// Verify header
	require.Equal(t, "TREE", string(buf[0:4]))
	require.Equal(t, uint8(1), buf[4])
	require.Equal(t, uint8(0), buf[5])
	require.Equal(t, uint16(2), binary.LittleEndian.Uint16(buf[6:8]))

	// Calculate expected size per C reference (H5B.c:1670-1678):
	// sizeof_rkey = 4 (nbytes) + 4 (filter_mask) + onDiskDims*8 = 4+4+3*8 = 32
	// sizeof_rnode = H5B_SIZEOF_HDR(24) + 2K*8 + (2K+1)*sizeof_rkey
	//             = 24 + 64*8 + 65*32 = 24 + 512 + 2080 = 2616 bytes
	// (K=32 for chunk B-trees, padded to full capacity)
	require.Equal(t, 2616, len(buf))

	// Verify first key coords are byte offsets
	pos := 24
	pos += 4 // nbytes
	pos += 4 // filter mask
	coord0 := binary.LittleEndian.Uint64(buf[pos:])
	pos += 8
	coord1 := binary.LittleEndian.Uint64(buf[pos:])
	pos += 8
	coord2 := binary.LittleEndian.Uint64(buf[pos:])

	require.Equal(t, uint64(0*10), coord0, "key0 byte offset[0]")
	require.Equal(t, uint64(0*20), coord1, "key0 byte offset[1]")
	require.Equal(t, uint64(0), coord2, "key0 trailing dim")
}
