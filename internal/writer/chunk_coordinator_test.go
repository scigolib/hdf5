package writer

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewChunkCoordinator tests coordinator creation.
func TestNewChunkCoordinator(t *testing.T) {
	tests := []struct {
		name        string
		datasetDims []uint64
		chunkDims   []uint64
		wantChunks  []uint64
		wantErr     bool
	}{
		{
			name:        "1D dataset",
			datasetDims: []uint64{100},
			chunkDims:   []uint64{10},
			wantChunks:  []uint64{10},
			wantErr:     false,
		},
		{
			name:        "2D dataset",
			datasetDims: []uint64{10, 20},
			chunkDims:   []uint64{5, 10},
			wantChunks:  []uint64{2, 2},
			wantErr:     false,
		},
		{
			name:        "3D dataset",
			datasetDims: []uint64{4, 5, 6},
			chunkDims:   []uint64{2, 3, 3},
			wantChunks:  []uint64{2, 2, 2},
			wantErr:     false,
		},
		{
			name:        "edge chunks 1D",
			datasetDims: []uint64{105},
			chunkDims:   []uint64{10},
			wantChunks:  []uint64{11}, // 10 full + 1 partial
			wantErr:     false,
		},
		{
			name:        "edge chunks 2D",
			datasetDims: []uint64{25, 35},
			chunkDims:   []uint64{10, 10},
			wantChunks:  []uint64{3, 4}, // 3x4 = 12 chunks
			wantErr:     false,
		},
		{
			name:        "dimension mismatch",
			datasetDims: []uint64{10, 20},
			chunkDims:   []uint64{5},
			wantErr:     true,
		},
		{
			name:        "zero dataset dimension",
			datasetDims: []uint64{10, 0},
			chunkDims:   []uint64{5, 5},
			wantErr:     true,
		},
		{
			name:        "zero chunk dimension",
			datasetDims: []uint64{10, 20},
			chunkDims:   []uint64{5, 0},
			wantErr:     true,
		},
		{
			name:        "empty dimensions",
			datasetDims: []uint64{},
			chunkDims:   []uint64{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc, err := NewChunkCoordinator(tt.datasetDims, tt.chunkDims)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cc)
			require.Equal(t, tt.wantChunks, cc.numChunks)
		})
	}
}

// TestGetTotalChunks tests total chunk count calculation.
func TestGetTotalChunks(t *testing.T) {
	tests := []struct {
		name        string
		datasetDims []uint64
		chunkDims   []uint64
		wantTotal   uint64
	}{
		{
			name:        "1D 10 chunks",
			datasetDims: []uint64{100},
			chunkDims:   []uint64{10},
			wantTotal:   10,
		},
		{
			name:        "2D 4 chunks",
			datasetDims: []uint64{10, 20},
			chunkDims:   []uint64{5, 10},
			wantTotal:   4, // 2 * 2
		},
		{
			name:        "3D 8 chunks",
			datasetDims: []uint64{4, 5, 6},
			chunkDims:   []uint64{2, 3, 3},
			wantTotal:   8, // 2 * 2 * 2
		},
		{
			name:        "2D with edges",
			datasetDims: []uint64{25, 35},
			chunkDims:   []uint64{10, 10},
			wantTotal:   12, // 3 * 4
		},
		{
			name:        "single chunk",
			datasetDims: []uint64{10, 20},
			chunkDims:   []uint64{10, 20},
			wantTotal:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc, err := NewChunkCoordinator(tt.datasetDims, tt.chunkDims)
			require.NoError(t, err)

			total := cc.GetTotalChunks()
			require.Equal(t, tt.wantTotal, total)
		})
	}
}

// TestGetChunkCoordinate tests linear index to N-D coordinate conversion.
func TestGetChunkCoordinate(t *testing.T) {
	t.Run("1D", func(t *testing.T) {
		cc, err := NewChunkCoordinator([]uint64{100}, []uint64{10})
		require.NoError(t, err)

		tests := []struct {
			index uint64
			want  []uint64
		}{
			{0, []uint64{0}},
			{1, []uint64{1}},
			{5, []uint64{5}},
			{9, []uint64{9}},
		}

		for _, tt := range tests {
			coord := cc.GetChunkCoordinate(tt.index)
			require.Equal(t, tt.want, coord, "index %d", tt.index)
		}
	})

	t.Run("2D", func(t *testing.T) {
		// 3x4 chunks (12 total)
		cc, err := NewChunkCoordinator([]uint64{25, 35}, []uint64{10, 10})
		require.NoError(t, err)

		tests := []struct {
			index uint64
			want  []uint64
		}{
			{0, []uint64{0, 0}},  // First chunk
			{1, []uint64{0, 1}},  // Second in row 0
			{3, []uint64{0, 3}},  // Last in row 0
			{4, []uint64{1, 0}},  // First in row 1
			{5, []uint64{1, 1}},  // Second in row 1
			{8, []uint64{2, 0}},  // First in row 2
			{11, []uint64{2, 3}}, // Last chunk
		}

		for _, tt := range tests {
			coord := cc.GetChunkCoordinate(tt.index)
			require.Equal(t, tt.want, coord, "index %d", tt.index)
		}
	})

	t.Run("3D", func(t *testing.T) {
		// 2x2x2 chunks (8 total)
		cc, err := NewChunkCoordinator([]uint64{4, 5, 6}, []uint64{2, 3, 3})
		require.NoError(t, err)

		tests := []struct {
			index uint64
			want  []uint64
		}{
			{0, []uint64{0, 0, 0}},
			{1, []uint64{0, 0, 1}},
			{2, []uint64{0, 1, 0}},
			{3, []uint64{0, 1, 1}},
			{4, []uint64{1, 0, 0}},
			{5, []uint64{1, 0, 1}},
			{6, []uint64{1, 1, 0}},
			{7, []uint64{1, 1, 1}},
		}

		for _, tt := range tests {
			coord := cc.GetChunkCoordinate(tt.index)
			require.Equal(t, tt.want, coord, "index %d", tt.index)
		}
	})
}

// TestGetChunkSize tests chunk size calculation (including edge chunks).
func TestGetChunkSize(t *testing.T) {
	t.Run("1D no edges", func(t *testing.T) {
		cc, err := NewChunkCoordinator([]uint64{100}, []uint64{10})
		require.NoError(t, err)

		for i := uint64(0); i < 10; i++ {
			size := cc.GetChunkSize([]uint64{i})
			require.Equal(t, []uint64{10}, size, "chunk %d", i)
		}
	})

	t.Run("1D with edge", func(t *testing.T) {
		cc, err := NewChunkCoordinator([]uint64{105}, []uint64{10})
		require.NoError(t, err)

		// First 10 chunks are full
		for i := uint64(0); i < 10; i++ {
			size := cc.GetChunkSize([]uint64{i})
			require.Equal(t, []uint64{10}, size, "chunk %d", i)
		}

		// Last chunk is partial (5 elements)
		size := cc.GetChunkSize([]uint64{10})
		require.Equal(t, []uint64{5}, size)
	})

	t.Run("2D with edges", func(t *testing.T) {
		// 25x35 dataset, 10x10 chunks → 3x4 chunks
		cc, err := NewChunkCoordinator([]uint64{25, 35}, []uint64{10, 10})
		require.NoError(t, err)

		tests := []struct {
			coord []uint64
			want  []uint64
		}{
			{[]uint64{0, 0}, []uint64{10, 10}}, // Full chunk
			{[]uint64{0, 1}, []uint64{10, 10}}, // Full chunk
			{[]uint64{0, 3}, []uint64{10, 5}},  // Partial in dim 1
			{[]uint64{1, 0}, []uint64{10, 10}}, // Full chunk
			{[]uint64{2, 0}, []uint64{5, 10}},  // Partial in dim 0
			{[]uint64{2, 3}, []uint64{5, 5}},   // Partial in both
		}

		for _, tt := range tests {
			size := cc.GetChunkSize(tt.coord)
			require.Equal(t, tt.want, size, "coord %v", tt.coord)
		}
	})

	t.Run("3D with edges", func(t *testing.T) {
		// 12x15x18 dataset, 10x10x10 chunks → 2x2x2 chunks
		cc, err := NewChunkCoordinator([]uint64{12, 15, 18}, []uint64{10, 10, 10})
		require.NoError(t, err)

		tests := []struct {
			coord []uint64
			want  []uint64
		}{
			{[]uint64{0, 0, 0}, []uint64{10, 10, 10}}, // Full
			{[]uint64{1, 0, 0}, []uint64{2, 10, 10}},  // Partial dim 0
			{[]uint64{0, 1, 0}, []uint64{10, 5, 10}},  // Partial dim 1
			{[]uint64{0, 0, 1}, []uint64{10, 10, 8}},  // Partial dim 2
			{[]uint64{1, 1, 1}, []uint64{2, 5, 8}},    // Partial all
		}

		for _, tt := range tests {
			size := cc.GetChunkSize(tt.coord)
			require.Equal(t, tt.want, size, "coord %v", tt.coord)
		}
	})
}

// TestExtractChunkData tests chunk data extraction from dataset.
func TestExtractChunkData(t *testing.T) {
	t.Run("1D extraction", func(t *testing.T) {
		// Dataset: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9] (10 uint32)
		// Chunks: 4 elements each
		// Chunks: [0-3], [4-7], [8-9]

		datasetDims := []uint64{10}
		chunkDims := []uint64{4}
		elemSize := uint32(4)

		cc, err := NewChunkCoordinator(datasetDims, chunkDims)
		require.NoError(t, err)

		// Create dataset data
		data := make([]byte, 10*elemSize)
		for i := uint32(0); i < 10; i++ {
			binary.LittleEndian.PutUint32(data[i*elemSize:], i)
		}

		// Extract chunk 0 [0-3]
		chunk0 := cc.ExtractChunkData(data, []uint64{0}, elemSize)
		require.Equal(t, 4*elemSize, uint32(len(chunk0)))
		for i := uint32(0); i < 4; i++ {
			val := binary.LittleEndian.Uint32(chunk0[i*elemSize:])
			require.Equal(t, i, val, "chunk0 element %d", i)
		}

		// Extract chunk 1 [4-7]
		chunk1 := cc.ExtractChunkData(data, []uint64{1}, elemSize)
		require.Equal(t, 4*elemSize, uint32(len(chunk1)))
		for i := uint32(0); i < 4; i++ {
			val := binary.LittleEndian.Uint32(chunk1[i*elemSize:])
			require.Equal(t, i+4, val, "chunk1 element %d", i)
		}

		// Extract chunk 2 [8-9] (edge chunk, 2 elements)
		chunk2 := cc.ExtractChunkData(data, []uint64{2}, elemSize)
		require.Equal(t, 2*elemSize, uint32(len(chunk2)))
		for i := uint32(0); i < 2; i++ {
			val := binary.LittleEndian.Uint32(chunk2[i*elemSize:])
			require.Equal(t, i+8, val, "chunk2 element %d", i)
		}
	})

	t.Run("2D extraction", func(t *testing.T) {
		// Dataset: 4x6 uint32, chunks: 2x3
		// Layout:
		//   0  1  2 |  3  4  5
		//   6  7  8 |  9 10 11
		//   --------+---------
		//  12 13 14 | 15 16 17
		//  18 19 20 | 21 22 23
		//
		// Chunks: [0,0], [0,1], [1,0], [1,1]

		datasetDims := []uint64{4, 6}
		chunkDims := []uint64{2, 3}
		elemSize := uint32(4)

		cc, err := NewChunkCoordinator(datasetDims, chunkDims)
		require.NoError(t, err)

		// Create dataset data (row-major)
		data := make([]byte, 24*elemSize)
		for i := uint32(0); i < 24; i++ {
			binary.LittleEndian.PutUint32(data[i*elemSize:], i)
		}

		// Extract chunk [0,0] → elements [0,1,2,6,7,8]
		chunk00 := cc.ExtractChunkData(data, []uint64{0, 0}, elemSize)
		require.Equal(t, 6*elemSize, uint32(len(chunk00)))
		expected00 := []uint32{0, 1, 2, 6, 7, 8}
		for i, exp := range expected00 {
			val := binary.LittleEndian.Uint32(chunk00[i*4:])
			require.Equal(t, exp, val, "chunk[0,0] element %d", i)
		}

		// Extract chunk [0,1] → elements [3,4,5,9,10,11]
		chunk01 := cc.ExtractChunkData(data, []uint64{0, 1}, elemSize)
		expected01 := []uint32{3, 4, 5, 9, 10, 11}
		for i, exp := range expected01 {
			val := binary.LittleEndian.Uint32(chunk01[i*4:])
			require.Equal(t, exp, val, "chunk[0,1] element %d", i)
		}

		// Extract chunk [1,0] → elements [12,13,14,18,19,20]
		chunk10 := cc.ExtractChunkData(data, []uint64{1, 0}, elemSize)
		expected10 := []uint32{12, 13, 14, 18, 19, 20}
		for i, exp := range expected10 {
			val := binary.LittleEndian.Uint32(chunk10[i*4:])
			require.Equal(t, exp, val, "chunk[1,0] element %d", i)
		}

		// Extract chunk [1,1] → elements [15,16,17,21,22,23]
		chunk11 := cc.ExtractChunkData(data, []uint64{1, 1}, elemSize)
		expected11 := []uint32{15, 16, 17, 21, 22, 23}
		for i, exp := range expected11 {
			val := binary.LittleEndian.Uint32(chunk11[i*4:])
			require.Equal(t, exp, val, "chunk[1,1] element %d", i)
		}
	})

	t.Run("2D edge chunk extraction", func(t *testing.T) {
		// Dataset: 5x7 uint32, chunks: 3x3
		// Chunks: 2x3 = 6 total
		// Edge chunks: [1,0], [0,2], [1,2]

		datasetDims := []uint64{5, 7}
		chunkDims := []uint64{3, 3}
		elemSize := uint32(4)

		cc, err := NewChunkCoordinator(datasetDims, chunkDims)
		require.NoError(t, err)

		// Create dataset data
		data := make([]byte, 35*elemSize)
		for i := uint32(0); i < 35; i++ {
			binary.LittleEndian.PutUint32(data[i*elemSize:], i)
		}

		// Extract chunk [1,2] (edge in both dims: 2x1)
		// Chunk [1,2] covers:
		//   - dim0: rows 3-4 (start=1*3=3, size=min(3, 5-3)=2)
		//   - dim1: col 6 (start=2*3=6, size=min(3, 7-6)=1)
		// Dataset 5x7 row-major layout:
		//   Row 0: 0-6
		//   Row 1: 7-13
		//   Row 2: 14-20
		//   Row 3: 21-27
		//   Row 4: 28-34
		// Elements: row3 col6 = 3*7+6=27, row4 col6 = 4*7+6=34
		chunk := cc.ExtractChunkData(data, []uint64{1, 2}, elemSize)
		require.Equal(t, 2*elemSize, uint32(len(chunk)))

		val0 := binary.LittleEndian.Uint32(chunk[0:4])
		val1 := binary.LittleEndian.Uint32(chunk[4:8])
		require.Equal(t, uint32(27), val0)
		require.Equal(t, uint32(34), val1)
	})

	t.Run("3D extraction", func(t *testing.T) {
		// Dataset: 2x3x4 uint32, chunks: 2x2x2
		// Total: 24 elements
		// Chunks: 1x2x2 = 4 chunks

		datasetDims := []uint64{2, 3, 4}
		chunkDims := []uint64{2, 2, 2}
		elemSize := uint32(4)

		cc, err := NewChunkCoordinator(datasetDims, chunkDims)
		require.NoError(t, err)

		// Create dataset data
		data := make([]byte, 24*elemSize)
		for i := uint32(0); i < 24; i++ {
			binary.LittleEndian.PutUint32(data[i*elemSize:], i)
		}

		// Extract chunk [0,0,0] (2x2x2 = 8 elements)
		chunk := cc.ExtractChunkData(data, []uint64{0, 0, 0}, elemSize)
		require.Equal(t, 8*elemSize, uint32(len(chunk)))

		// Verify elements
		// 3D indexing: index = i*3*4 + j*4 + k
		// Chunk [0,0,0] covers i=0-1, j=0-1, k=0-1
		expected := []uint32{
			0, 1, // i=0, j=0, k=0-1
			4, 5, // i=0, j=1, k=0-1
			12, 13, // i=1, j=0, k=0-1
			16, 17, // i=1, j=1, k=0-1
		}

		for i, exp := range expected {
			val := binary.LittleEndian.Uint32(chunk[i*4:])
			require.Equal(t, exp, val, "element %d", i)
		}
	})
}

// TestChunkCoordinator_Getters tests read-only getters.
func TestChunkCoordinator_Getters(t *testing.T) {
	datasetDims := []uint64{10, 20, 30}
	chunkDims := []uint64{5, 10, 15}

	cc, err := NewChunkCoordinator(datasetDims, chunkDims)
	require.NoError(t, err)

	// Test DatasetDims
	dims := cc.DatasetDims()
	require.Equal(t, datasetDims, dims)
	// Modify returned slice (should not affect internal state)
	dims[0] = 999
	require.Equal(t, uint64(10), cc.datasetDims[0])

	// Test ChunkDims
	chunks := cc.ChunkDims()
	require.Equal(t, chunkDims, chunks)
	chunks[0] = 999
	require.Equal(t, uint64(5), cc.chunkDims[0])

	// Test NumChunks
	nums := cc.NumChunks()
	require.Equal(t, []uint64{2, 2, 2}, nums)
	nums[0] = 999
	require.Equal(t, uint64(2), cc.numChunks[0])
}
