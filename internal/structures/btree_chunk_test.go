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
			// Sentinel max key
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord0, "sentinel key coord0")
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord1, "sentinel key coord1")
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
			// Sentinel
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord0, "sentinel coord0")
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord1, "sentinel coord1")
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord2, "sentinel coord2")
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

	// Sentinel key
	pos += 4 // nbytes
	pos += 4 // filter mask
	sentinel0 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), sentinel0)
	pos += 8
	sentinel1 := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), sentinel1)
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

	// Calculate expected size
	// Header: 24 bytes
	// Keys: 3 keys * (4 + 4 + 3*8) = 3 * 32 = 96 bytes
	// Children: 2 * 8 = 16 bytes
	// Total: 24 + 96 + 16 = 136 bytes
	require.Equal(t, 136, len(buf))

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
