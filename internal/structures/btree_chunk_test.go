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
func TestChunkBTreeWriter_1D(t *testing.T) {
	writer := NewChunkBTreeWriter(1)

	// Add 10 chunks
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
	// Key format: nbytes (4) + filterMask (4) + coord (8)
	pos := 24

	for i := 0; i < 11; i++ {
		// Read key
		nbytes := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		filterMask := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		coord := binary.LittleEndian.Uint64(data[pos:])
		pos += 8

		if i < 10 {
			require.Equal(t, uint32(0), nbytes, "chunk %d nbytes", i) // AddChunk uses 0 by default
			require.Equal(t, uint64(i), coord, "chunk %d coordinate", i)
		} else {
			// Sentinel max key
			require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), coord, "sentinel key")
		}
		require.Equal(t, uint32(0), filterMask)

		// Read child address (except for sentinel key)
		if i < 10 {
			chunkAddr := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			require.Equal(t, uint64(1000+i*100), chunkAddr, "chunk %d address", i)
		}
	}
}

// TestChunkBTreeWriter_2D tests 2D chunked dataset.
func TestChunkBTreeWriter_2D(t *testing.T) {
	writer := NewChunkBTreeWriter(2)

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

	// Verify sorting by reading interleaved keys and children
	// Format: key0, child0, key1, child1, ..., key3, child3, key4 (sentinel)
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8)
	data := mockWriter.ReadAt(addr)
	pos := 24 // After header

	expectedCoords := [][]uint64{
		{0, 0}, {0, 1}, {1, 0}, {1, 1}, // Sorted in row-major order
		{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}, // Sentinel
	}
	expectedAddrs := []uint64{1000, 1500, 2000, 2500}

	for i, expected := range expectedCoords {
		// Read key: nbytes + filterMask + coords
		pos += 4 // Skip nbytes
		pos += 4 // Skip filter mask
		coord0 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		coord1 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8

		require.Equal(t, expected[0], coord0, "key %d coord[0]", i)
		require.Equal(t, expected[1], coord1, "key %d coord[1]", i)

		// Read child address (except for sentinel key)
		if i < len(expectedAddrs) {
			chunkAddr := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			require.Equal(t, expectedAddrs[i], chunkAddr, "chunk %d address", i)
		}
	}
}

// TestChunkBTreeWriter_3D tests 3D chunked dataset.
func TestChunkBTreeWriter_3D(t *testing.T) {
	writer := NewChunkBTreeWriter(3)

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

	// Verify all 8 chunks are present with interleaved keys and children
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8) + coord2 (8)
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
func TestChunkBTreeWriter_Sorting(t *testing.T) {
	writer := NewChunkBTreeWriter(2)

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

	// Verify chunks are sorted in row-major order
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8)
	data := mockWriter.ReadAt(addr)
	pos := 24

	// Expected order: [0,0], [0,1], [0,2], [0,3], [1,0], ...
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			pos += 4 // nbytes
			pos += 4 // filter mask
			coord0 := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			coord1 := binary.LittleEndian.Uint64(data[pos:])
			pos += 8
			pos += 8 // child address

			require.Equal(t, uint64(i), coord0, "chunk [%d,%d] dim0", i, j)
			require.Equal(t, uint64(j), coord1, "chunk [%d,%d] dim1", i, j)
		}
	}
}

// TestChunkBTreeWriter_EdgeChunks tests edge and corner chunks.
func TestChunkBTreeWriter_EdgeChunks(t *testing.T) {
	writer := NewChunkBTreeWriter(2)

	// Add edge chunks (large coordinates)
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

	// Verify large coordinates are handled correctly
	// Key format: nbytes (4) + filterMask (4) + coord0 (8) + coord1 (8)
	data := mockWriter.ReadAt(addr)
	pos := 24

	expectedCoords := [][]uint64{
		{0, 0}, {0, 200}, {100, 0}, {100, 200},
	}

	for i, expected := range expectedCoords {
		pos += 4 // nbytes
		pos += 4 // filter mask
		coord0 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		coord1 := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		pos += 8 // child address

		require.Equal(t, expected[0], coord0, "chunk %d coord[0]", i)
		require.Equal(t, expected[1], coord1, "chunk %d coord[1]", i)
	}
}

// TestChunkBTreeWriter_SingleChunk tests B-tree with single chunk.
func TestChunkBTreeWriter_SingleChunk(t *testing.T) {
	writer := NewChunkBTreeWriter(1)

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

	// Key format: nbytes (4) + filterMask (4) + coord (8)
	pos := 24

	// First key
	pos += 4 // nbytes
	pos += 4 // filter mask
	coord := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0), coord)
	pos += 8

	// Single child address
	chunkAddr := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(5000), chunkAddr)
	pos += 8

	// Sentinel key
	pos += 4 // nbytes
	pos += 4 // filter mask
	sentinel := binary.LittleEndian.Uint64(data[pos:])
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), sentinel)
}

// TestChunkBTreeWriter_ErrorCases tests error handling.
func TestChunkBTreeWriter_ErrorCases(t *testing.T) {
	t.Run("empty B-tree", func(t *testing.T) {
		writer := NewChunkBTreeWriter(1)

		mockWriter := newMockChunkWriter()
		mockAllocator := newMockChunkAllocator(1000)

		_, err := writer.WriteToFile(mockWriter, mockAllocator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no chunks")
	})

	t.Run("dimension mismatch", func(t *testing.T) {
		writer := NewChunkBTreeWriter(2)

		err := writer.AddChunk([]uint64{0, 0, 0}, 1000) // 3D coord for 2D writer
		require.Error(t, err)
		require.Contains(t, err.Error(), "dimensionality mismatch")
	})
}

// TestSerializeChunkBTreeNode tests serialization directly.
func TestSerializeChunkBTreeNode(t *testing.T) {
	node := &ChunkBTreeNode{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    0,
		EntriesUsed:  2,
		LeftSibling:  0xFFFFFFFFFFFFFFFF,
		RightSibling: 0xFFFFFFFFFFFFFFFF,
		Keys: []ChunkKey{
			{Coords: []uint64{0, 0}, FilterMask: 0, Nbytes: 800},
			{Coords: []uint64{1, 1}, FilterMask: 0, Nbytes: 800},
			{Coords: []uint64{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}, FilterMask: 0, Nbytes: 0}, // Sentinel
		},
		ChildAddrs: []uint64{1000, 2000},
	}

	buf := serializeChunkBTreeNode(node, 2)

	// Verify header
	require.Equal(t, "TREE", string(buf[0:4]))
	require.Equal(t, uint8(1), buf[4])
	require.Equal(t, uint8(0), buf[5])
	require.Equal(t, uint16(2), binary.LittleEndian.Uint16(buf[6:8]))

	// Calculate expected size
	// Header: 24 bytes
	// Keys: 3 keys * (4 + 4 + 2*8) = 3 * 24 = 72 bytes
	// Children: 2 * 8 = 16 bytes
	// Total: 24 + 72 + 16 = 112 bytes
	require.Equal(t, 112, len(buf))
}
