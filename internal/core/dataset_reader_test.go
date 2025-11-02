package core

import (
	"encoding/binary"
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

// TestReadDatasetInfo tests reading dataset metadata.
func TestReadDatasetInfo(t *testing.T) {
	tests := []struct {
		name    string
		header  *ObjectHeader
		sb      *Superblock
		wantErr bool
	}{
		{
			name: "valid dataset with all messages",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildDatatypeMessage(t, DatatypeFloat, 8),
					},
					{
						Type: MsgDataspace,
						Data: buildDataspaceMessage(t, []uint64{2, 3}),
					},
					{
						Type: MsgDataLayout,
						Data: buildDataLayoutMessage(t, LayoutContiguous, 0x1000),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: false,
		},
		{
			name: "missing datatype message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDataspace,
						Data: buildDataspaceMessage(t, []uint64{2, 3}),
					},
					{
						Type: MsgDataLayout,
						Data: buildDataLayoutMessage(t, LayoutContiguous, 0x1000),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: true,
		},
		{
			name: "missing dataspace message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildDatatypeMessage(t, DatatypeFloat, 8),
					},
					{
						Type: MsgDataLayout,
						Data: buildDataLayoutMessage(t, LayoutContiguous, 0x1000),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: true,
		},
		{
			name: "missing layout message",
			header: &ObjectHeader{
				Messages: []*HeaderMessage{
					{
						Type: MsgDatatype,
						Data: buildDatatypeMessage(t, DatatypeFloat, 8),
					},
					{
						Type: MsgDataspace,
						Data: buildDataspaceMessage(t, []uint64{2, 3}),
					},
				},
			},
			sb:      &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ReadDatasetInfo(tt.header, tt.sb)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info == nil {
				t.Fatal("info is nil")
			}
			if info.Datatype == nil {
				t.Error("datatype is nil")
			}
			if info.Dataspace == nil {
				t.Error("dataspace is nil")
			}
			if info.Layout == nil {
				t.Error("layout is nil")
			}
		})
	}
}

// Helper functions to build message data.

func buildDatatypeMessage(t *testing.T, class DatatypeClass, size uint32) []byte {
	t.Helper()
	// Simple datatype message (version 1).
	// Bytes 0-3: class | version << 4 | bitfield << 8.
	// Bytes 4-7: size.
	data := make([]byte, 8)
	classAndVersion := uint32(class) | (1 << 4) // Version 1.
	data[0] = byte(classAndVersion & 0xFF)
	data[1] = byte((classAndVersion >> 8) & 0xFF)
	data[2] = byte((classAndVersion >> 16) & 0xFF)
	data[3] = byte((classAndVersion >> 24) & 0xFF)
	data[4] = byte(size & 0xFF)
	data[5] = byte((size >> 8) & 0xFF)
	data[6] = byte((size >> 16) & 0xFF)
	data[7] = byte((size >> 24) & 0xFF)
	return data
}

func buildDataspaceMessage(t *testing.T, dims []uint64) []byte {
	t.Helper()
	// Simple dataspace message (version 1).
	// Byte 0: version.
	// Byte 1: dimensionality.
	// Byte 2: flags.
	// Bytes 3-4: reserved.
	// Then: dimension sizes (8 bytes each).
	data := make([]byte, 5+len(dims)*8)
	data[0] = 1                // Version 1.
	data[1] = uint8(len(dims)) // Dimensionality.
	data[2] = 0                // Flags (no max dims).
	// Skip reserved bytes 3-4.
	offset := 5
	for _, dim := range dims {
		data[offset] = byte(dim & 0xFF)
		data[offset+1] = byte((dim >> 8) & 0xFF)
		data[offset+2] = byte((dim >> 16) & 0xFF)
		data[offset+3] = byte((dim >> 24) & 0xFF)
		data[offset+4] = byte((dim >> 32) & 0xFF)
		data[offset+5] = byte((dim >> 40) & 0xFF)
		data[offset+6] = byte((dim >> 48) & 0xFF)
		data[offset+7] = byte((dim >> 56) & 0xFF)
		offset += 8
	}
	return data
}

func buildDataLayoutMessage(t *testing.T, class DataLayoutClass, address uint64) []byte {
	t.Helper()
	// Simple contiguous layout message (version 3).
	// Byte 0: version.
	// Byte 1: class.
	// Bytes 2+: address (8 bytes) + size (8 bytes).
	data := make([]byte, 2+8+8)
	data[0] = 3            // Version 3.
	data[1] = uint8(class) // Class.
	// Address (8 bytes).
	data[2] = byte(address & 0xFF)
	data[3] = byte((address >> 8) & 0xFF)
	data[4] = byte((address >> 16) & 0xFF)
	data[5] = byte((address >> 24) & 0xFF)
	data[6] = byte((address >> 32) & 0xFF)
	data[7] = byte((address >> 40) & 0xFF)
	data[8] = byte((address >> 48) & 0xFF)
	data[9] = byte((address >> 56) & 0xFF)
	// Size (8 bytes) - set to 1000 bytes for testing.
	size := uint64(1000)
	data[10] = byte(size & 0xFF)
	data[11] = byte((size >> 8) & 0xFF)
	data[12] = byte((size >> 16) & 0xFF)
	data[13] = byte((size >> 24) & 0xFF)
	data[14] = byte((size >> 32) & 0xFF)
	data[15] = byte((size >> 40) & 0xFF)
	data[16] = byte((size >> 48) & 0xFF)
	data[17] = byte((size >> 56) & 0xFF)
	return data
}
