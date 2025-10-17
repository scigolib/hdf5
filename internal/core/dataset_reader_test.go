package core

import (
	"testing"
)

// Test copyNDChunk with various dimensions.
func TestCopyNDChunk(t *testing.T) {
	tests := []struct {
		name        string
		chunkCoords []uint64
		chunkSize   []uint32
		dataDims    []uint64
		elemSize    uint64
		setupChunk  func() []byte // Returns chunk data.
		verify      func(t *testing.T, fullData []byte)
	}{
		{
			name:        "2D chunk at origin",
			chunkCoords: []uint64{0, 0},
			chunkSize:   []uint32{2, 3},
			dataDims:    []uint64{4, 6},
			elemSize:    4, // int32.
			setupChunk: func() []byte {
				// Chunk contains 2x3 = 6 elements: [0, 1, 2, 3, 4, 5].
				return []byte{
					0, 0, 0, 0, // 0.
					1, 0, 0, 0, // 1.
					2, 0, 0, 0, // 2.
					3, 0, 0, 0, // 3.
					4, 0, 0, 0, // 4.
					5, 0, 0, 0, // 5.
				}
			},
			verify: func(t *testing.T, fullData []byte) {
				// Full array is 4x6 = 24 elements.
				// Chunk [0,0] should be at positions:
				// [0,0]=0, [0,1]=1, [0,2]=2.
				// [1,0]=3, [1,1]=4, [1,2]=5.
				expected := []int32{
					0, 1, 2, 0, 0, 0, // Row 0.
					3, 4, 5, 0, 0, 0, // Row 1.
					0, 0, 0, 0, 0, 0, // Row 2.
					0, 0, 0, 0, 0, 0, // Row 3.
				}
				for i, exp := range expected {
					got := int32(fullData[i*4]) | int32(fullData[i*4+1])<<8 | int32(fullData[i*4+2])<<16 | int32(fullData[i*4+3])<<24
					if got != exp {
						t.Errorf("Position %d: expected %d, got %d", i, exp, got)
					}
				}
			},
		},
		{
			name:        "2D chunk at offset",
			chunkCoords: []uint64{1, 1},
			chunkSize:   []uint32{2, 2},
			dataDims:    []uint64{4, 4},
			elemSize:    4,
			setupChunk: func() []byte {
				// Chunk [1,1] with values [10, 11, 12, 13].
				return []byte{
					10, 0, 0, 0,
					11, 0, 0, 0,
					12, 0, 0, 0,
					13, 0, 0, 0,
				}
			},
			verify: func(t *testing.T, fullData []byte) {
				// Chunk [1,1] with chunk size [2,2] starts at element position [1*2, 1*2] = [2,2].
				// Maps to positions: [2][2], [2][3], [3][2], [3][3].
				checkPos := func(row, col, expected int) {
					idx := row*4 + col
					got := int32(fullData[idx*4])
					if got != int32(expected) {
						t.Errorf("[%d][%d]: expected %d, got %d", row, col, expected, got)
					}
				}
				checkPos(2, 2, 10)
				checkPos(2, 3, 11)
				checkPos(3, 2, 12)
				checkPos(3, 3, 13)
			},
		},
		{
			name:        "3D chunk at origin",
			chunkCoords: []uint64{0, 0, 0},
			chunkSize:   []uint32{2, 2, 2},
			dataDims:    []uint64{4, 4, 4},
			elemSize:    1, // uint8 for simplicity.
			setupChunk: func() []byte {
				// 2x2x2 = 8 elements.
				return []byte{0, 1, 2, 3, 4, 5, 6, 7}
			},
			verify: func(t *testing.T, fullData []byte) {
				// Verify [0][0][0..1], [0][1][0..1], [1][0][0..1], [1][1][0..1].
				checkPos := func(i, j, k int, expected byte) {
					idx := i*16 + j*4 + k
					if fullData[idx] != expected {
						t.Errorf("[%d][%d][%d]: expected %d, got %d", i, j, k, expected, fullData[idx])
					}
				}
				checkPos(0, 0, 0, 0)
				checkPos(0, 0, 1, 1)
				checkPos(0, 1, 0, 2)
				checkPos(0, 1, 1, 3)
				checkPos(1, 0, 0, 4)
				checkPos(1, 0, 1, 5)
				checkPos(1, 1, 0, 6)
				checkPos(1, 1, 1, 7)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create full array (initialized to zero).
			totalElements := uint64(1)
			for _, dim := range tt.dataDims {
				totalElements *= dim
			}
			fullData := make([]byte, totalElements*tt.elemSize)

			// Get chunk data.
			chunkData := tt.setupChunk()

			// Copy chunk to full array.
			err := copyNDChunk(chunkData, fullData, tt.chunkCoords, tt.chunkSize, tt.dataDims, tt.elemSize)
			if err != nil {
				t.Fatalf("copyNDChunk failed: %v", err)
			}

			// Verify result.
			tt.verify(t, fullData)
		})
	}
}

// Test partial chunks at boundaries.
func TestCopyNDChunk_PartialChunks(t *testing.T) {
	// Dataset: 5x7 (partial chunk at boundaries).
	// Chunks: 3x3.
	// Last row chunks: 2x3 (only 2 rows instead of 3).
	// Last column chunks: 3x1 (only 1 col instead of 3).
	// Corner chunk: 2x1.

	// Chunk data: 3x3 in row-major order.
	// [1, 2, 3].
	// [4, 5, 6].
	// [7, 8, 9] <- but we only have 6 bytes, so treating as 3x2.
	chunkData := []byte{1, 2, 3, 4, 5, 6}
	fullData := make([]byte, 5*7)

	// Chunk [1,2] with chunk size [3,3] starts at [1*3, 2*3] = [3, 6].
	// Dataset 5x7 means only 2 rows and 1 col fit: 2x1 sub-chunk.
	err := copyNDChunk(chunkData, fullData, []uint64{1, 2}, []uint32{3, 3}, []uint64{5, 7}, 1)
	if err != nil {
		t.Fatalf("copyNDChunk failed: %v", err)
	}

	// From 3x3 chunk (row-major), we copy first column, first 2 rows:
	// [0][0] = element 0 = 1 → fullData[3][6].
	// [1][0] = element 3 = 4 → fullData[4][6].
	if fullData[3*7+6] != 1 {
		t.Errorf("[3][6]: expected 1, got %d", fullData[3*7+6])
	}
	if fullData[4*7+6] != 4 {
		t.Errorf("[4][6]: expected 4, got %d", fullData[4*7+6])
	}
}
