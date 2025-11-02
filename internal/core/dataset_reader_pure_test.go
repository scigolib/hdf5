package core

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConvertToFloat64 tests pure function convertToFloat64.
func TestConvertToFloat64(t *testing.T) {
	tests := []struct {
		name        string
		rawData     []byte
		datatype    *DatatypeMessage
		numElements uint64
		want        []float64
		wantErr     bool
	}{
		{
			name: "float64 little-endian",
			rawData: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f, // 1.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // 2.0
			},
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0x00, // Little-endian (bit 0 = 0)
			},
			numElements: 2,
			want:        []float64{1.0, 2.0},
			wantErr:     false,
		},
		{
			name: "float64 big-endian",
			rawData: []byte{
				0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 1.0
				0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 2.0
			},
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0x01, // Big-endian (bit 0 = 1)
			},
			numElements: 2,
			want:        []float64{1.0, 2.0},
			wantErr:     false,
		},
		{
			name: "float32 little-endian",
			rawData: []byte{
				0x00, 0x00, 0x80, 0x3f, // 1.0
				0x00, 0x00, 0x00, 0x40, // 2.0
			},
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          4,
				ClassBitField: 0x00, // Little-endian
			},
			numElements: 2,
			want:        []float64{1.0, 2.0},
			wantErr:     false,
		},
		{
			name: "int32 little-endian",
			rawData: []byte{
				0x0a, 0x00, 0x00, 0x00, // 10
				0x14, 0x00, 0x00, 0x00, // 20
			},
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0x00, // Little-endian
			},
			numElements: 2,
			want:        []float64{10.0, 20.0},
			wantErr:     false,
		},
		{
			name: "int64 little-endian",
			rawData: []byte{
				0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 100
				0xc8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 200
			},
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          8,
				ClassBitField: 0x00, // Little-endian
			},
			numElements: 2,
			want:        []float64{100.0, 200.0},
			wantErr:     false,
		},
		{
			name:    "empty array",
			rawData: []byte{},
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0x00,
			},
			numElements: 0,
			want:        []float64{},
			wantErr:     false,
		},
		{
			name:    "truncated float64 data",
			rawData: []byte{0x00, 0x00, 0x00}, // Less than 8 bytes
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "truncated float32 data",
			rawData: []byte{0x00, 0x00}, // Less than 4 bytes
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          4,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "truncated int32 data",
			rawData: []byte{0x00, 0x00}, // Less than 4 bytes
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "truncated int64 data",
			rawData: []byte{0x00, 0x00, 0x00, 0x00}, // Less than 8 bytes
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          8,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "unsupported datatype",
			rawData: []byte{0x00, 0x00, 0x00, 0x00},
			datatype: &DatatypeMessage{
				Class:         DatatypeString, // Not supported
				Size:          4,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToFloat64(tt.rawData, tt.datatype, tt.numElements)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestConvertToStrings tests pure function convertToStrings.
func TestConvertToStrings(t *testing.T) {
	tests := []struct {
		name        string
		rawData     []byte
		datatype    *DatatypeMessage
		numElements uint64
		want        []string
		wantErr     bool
	}{
		{
			name:    "fixed null-terminated strings",
			rawData: []byte{'h', 'e', 'l', 'l', 'o', 0, 0, 0, 'w', 'o', 'r', 'l', 'd', 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          8,
				ClassBitField: 0x00, // Null-terminated (bits 0-3)
			},
			numElements: 2,
			want:        []string{"hello", "world"},
			wantErr:     false,
		},
		{
			name:    "fixed null-padded strings",
			rawData: []byte{'f', 'o', 'o', 0, 0, 0, 'b', 'a', 'r', 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          6,
				ClassBitField: 0x01, // Null-padded
			},
			numElements: 2,
			want:        []string{"foo", "bar"},
			wantErr:     false,
		},
		{
			name:    "fixed space-padded strings",
			rawData: []byte{'a', 'b', ' ', ' ', 'c', 'd', ' ', ' '},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          4,
				ClassBitField: 0x02, // Space-padded
			},
			numElements: 2,
			want:        []string{"ab", "cd"},
			wantErr:     false,
		},
		{
			name:    "empty strings",
			rawData: []byte{0, 0, 0, 0, 0, 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          4,
				ClassBitField: 0x00, // Null-terminated
			},
			numElements: 2,
			want:        []string{"", ""},
			wantErr:     false,
		},
		{
			name:    "empty array",
			rawData: []byte{},
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          8,
				ClassBitField: 0x00,
			},
			numElements: 0,
			want:        []string{},
			wantErr:     false,
		},
		{
			name:    "truncated data",
			rawData: []byte{'h', 'e', 'l'}, // Less than size
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          8,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "variable-length string (not supported)",
			rawData: []byte{0, 0, 0, 0, 0, 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeVarLen,
				Size:          16,
				ClassBitField: 0x00,
				Properties:    []byte{0x03}, // Base type = string
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
		{
			name:    "unknown string type (not fixed, not variable)",
			rawData: []byte{0, 0, 0, 0},
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed, // Not a string class
				Size:          4,
				ClassBitField: 0x00,
			},
			numElements: 1,
			want:        nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToStrings(tt.rawData, tt.datatype, tt.numElements)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestDecodeFixedString tests pure function decodeFixedString.
func TestDecodeFixedString(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		paddingType uint8
		want        string
	}{
		{
			name:        "null-terminated",
			data:        []byte{'h', 'e', 'l', 'l', 'o', 0, 0, 0},
			paddingType: 0,
			want:        "hello",
		},
		{
			name:        "null-terminated no null",
			data:        []byte{'h', 'e', 'l', 'l', 'o'},
			paddingType: 0,
			want:        "hello",
		},
		{
			name:        "null-padded",
			data:        []byte{'f', 'o', 'o', 0, 0, 0},
			paddingType: 1,
			want:        "foo",
		},
		{
			name:        "space-padded",
			data:        []byte{'b', 'a', 'r', ' ', ' ', ' '},
			paddingType: 2,
			want:        "bar",
		},
		{
			name:        "unknown padding",
			data:        []byte{'t', 'e', 's', 't', 0, ' '},
			paddingType: 99,
			want:        "test\x00 ",
		},
		{
			name:        "empty null-terminated",
			data:        []byte{0, 0, 0, 0},
			paddingType: 0,
			want:        "",
		},
		{
			name:        "empty null-padded",
			data:        []byte{0, 0, 0, 0},
			paddingType: 1,
			want:        "",
		},
		{
			name:        "empty space-padded",
			data:        []byte{' ', ' ', ' ', ' '},
			paddingType: 2,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeFixedString(tt.data, tt.paddingType)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCopyChunkToArray tests pure function copyChunkToArray.
func TestCopyChunkToArray(t *testing.T) {
	tests := []struct {
		name        string
		chunkData   []byte
		fullData    []byte
		chunkCoords []uint64
		chunkSize   []uint32
		dataDims    []uint64
		elemSize    uint64
		wantData    []byte
		wantErr     bool
	}{
		{
			name:        "1D array, single chunk",
			chunkData:   []byte{1, 2, 3, 4},
			fullData:    make([]byte, 8),
			chunkCoords: []uint64{0},
			chunkSize:   []uint32{4},
			dataDims:    []uint64{8},
			elemSize:    1,
			wantData:    []byte{1, 2, 3, 4, 0, 0, 0, 0},
			wantErr:     false,
		},
		{
			name:        "1D array, second chunk",
			chunkData:   []byte{5, 6, 7, 8},
			fullData:    []byte{1, 2, 3, 4, 0, 0, 0, 0},
			chunkCoords: []uint64{1},
			chunkSize:   []uint32{4},
			dataDims:    []uint64{8},
			elemSize:    1,
			wantData:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
			wantErr:     false,
		},
		{
			name:        "2D array (2x3), chunk [0,0]",
			chunkData:   []byte{1, 2},
			fullData:    make([]byte, 6),
			chunkCoords: []uint64{0, 0},
			chunkSize:   []uint32{1, 2},
			dataDims:    []uint64{2, 3},
			elemSize:    1,
			wantData:    []byte{1, 2, 0, 0, 0, 0},
			wantErr:     false,
		},
		{
			name:        "2D array (2x3), chunk [1,0]",
			chunkData:   []byte{3, 4},
			fullData:    []byte{1, 2, 0, 0, 0, 0},
			chunkCoords: []uint64{1, 0},
			chunkSize:   []uint32{1, 2},
			dataDims:    []uint64{2, 3},
			elemSize:    1,
			wantData:    []byte{1, 2, 0, 3, 4, 0},
			wantErr:     false,
		},
		{
			name:        "dimension mismatch",
			chunkData:   []byte{1, 2},
			fullData:    make([]byte, 6),
			chunkCoords: []uint64{0},
			chunkSize:   []uint32{1, 2},
			dataDims:    []uint64{2, 3},
			elemSize:    1,
			wantData:    nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := copyChunkToArray(tt.chunkData, tt.fullData, tt.chunkCoords, tt.chunkSize, tt.dataDims, tt.elemSize)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantData != nil {
				require.Equal(t, tt.wantData, tt.fullData)
			}
		})
	}
}

// TestCopyNDChunkRecursive tests the recursive N-dimensional chunk copy.
func TestCopyNDChunkRecursive(t *testing.T) {
	tests := []struct {
		name           string
		chunkData      []byte
		fullData       []byte
		indices        []uint64
		dim            int
		copyDims       []uint64
		chunkStrides   []uint64
		dataStrides    []uint64
		dataBaseOffset uint64
		elemSize       uint64
		wantData       []byte
		wantErr        bool
	}{
		{
			name:           "3D copy (2x2x2)",
			chunkData:      []byte{1, 2, 3, 4, 5, 6, 7, 8},
			fullData:       make([]byte, 8),
			indices:        []uint64{0, 0, 0},
			dim:            0,
			copyDims:       []uint64{2, 2, 2},
			chunkStrides:   []uint64{4, 2, 1},
			dataStrides:    []uint64{4, 2, 1},
			dataBaseOffset: 0,
			elemSize:       1,
			wantData:       []byte{1, 2, 3, 4, 5, 6, 7, 8},
			wantErr:        false,
		},
		{
			name:           "chunk data truncated",
			chunkData:      []byte{1, 2}, // Too small
			fullData:       make([]byte, 8),
			indices:        []uint64{0, 0, 0},
			dim:            0,
			copyDims:       []uint64{2, 2, 2},
			chunkStrides:   []uint64{4, 2, 1},
			dataStrides:    []uint64{4, 2, 1},
			dataBaseOffset: 0,
			elemSize:       1,
			wantData:       nil,
			wantErr:        true,
		},
		{
			name:           "full data overflow",
			chunkData:      []byte{1, 2, 3, 4, 5, 6, 7, 8},
			fullData:       make([]byte, 4), // Too small
			indices:        []uint64{0, 0, 0},
			dim:            0,
			copyDims:       []uint64{2, 2, 2},
			chunkStrides:   []uint64{4, 2, 1},
			dataStrides:    []uint64{4, 2, 1},
			dataBaseOffset: 0,
			elemSize:       1,
			wantData:       nil,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := copyNDChunkRecursive(
				tt.chunkData,
				tt.fullData,
				tt.indices,
				tt.dim,
				tt.copyDims,
				tt.chunkStrides,
				tt.dataStrides,
				tt.dataBaseOffset,
				tt.elemSize,
			)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantData != nil {
				require.Equal(t, tt.wantData, tt.fullData)
			}
		})
	}
}

// Benchmark tests for performance-critical functions.

func BenchmarkConvertToFloat64_Float64(b *testing.B) {
	const numElements = 1000
	rawData := make([]byte, numElements*8)
	for i := 0; i < numElements; i++ {
		binary.LittleEndian.PutUint64(rawData[i*8:], math.Float64bits(float64(i)))
	}
	datatype := &DatatypeMessage{
		Class:         DatatypeFloat,
		Size:          8,
		ClassBitField: 0x00, // Little-endian
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = convertToFloat64(rawData, datatype, numElements)
	}
}

func BenchmarkConvertToStrings_Fixed(b *testing.B) {
	const numElements = 100
	const stringSize = 16
	rawData := make([]byte, numElements*stringSize)
	for i := 0; i < numElements; i++ {
		copy(rawData[i*stringSize:], "benchmark string")
	}
	datatype := &DatatypeMessage{
		Class:         DatatypeString,
		Size:          stringSize,
		ClassBitField: 0x00, // Null-terminated
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = convertToStrings(rawData, datatype, numElements)
	}
}
