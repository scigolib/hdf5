package structures

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseFractalHeapHeader tests parsing of fractal heap header.
func TestParseFractalHeapHeader(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() []byte
		sizeofSize  uint8
		sizeofAddr  uint8
		wantErr     bool
		errContains string
		validate    func(*testing.T, *FractalHeapHeader)
	}{
		{
			name: "valid basic header",
			setup: func() []byte {
				buf := &bytes.Buffer{}

				// Signature: "FRHP"
				buf.WriteString("FRHP")

				// Version: 0
				buf.WriteByte(0)

				// Heap ID Length: 8 bytes
				binary.Write(buf, binary.LittleEndian, uint16(8))

				// I/O Filters Length: 0
				binary.Write(buf, binary.LittleEndian, uint16(0))

				// Flags: 0x02 (checksum enabled)
				buf.WriteByte(0x02)

				// Max Managed Object Size: 4096
				binary.Write(buf, binary.LittleEndian, uint32(4096))

				// Next Huge Object ID: 0 (8 bytes for sizeofSize=8)
				binary.Write(buf, binary.LittleEndian, uint64(0))

				// Huge Object B-tree Address: invalid (8 bytes for sizeofAddr=8)
				binary.Write(buf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))

				// Free Space Amount: 3000 (8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(3000))

				// Free Space Section Address: invalid (8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))

				// Managed Objects Statistics (4 * 8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(4096)) // Space size
				binary.Write(buf, binary.LittleEndian, uint64(4096)) // Alloc size
				binary.Write(buf, binary.LittleEndian, uint64(0))    // Iter offset
				binary.Write(buf, binary.LittleEndian, uint64(5))    // Object count

				// Huge Objects Statistics (2 * 8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(0)) // Size
				binary.Write(buf, binary.LittleEndian, uint64(0)) // Count

				// Tiny Objects Statistics (2 * 8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(0)) // Size
				binary.Write(buf, binary.LittleEndian, uint64(0)) // Count

				// Doubling Table Parameters
				binary.Write(buf, binary.LittleEndian, uint16(16))    // Table width
				binary.Write(buf, binary.LittleEndian, uint64(1024))  // Starting block size
				binary.Write(buf, binary.LittleEndian, uint64(4096))  // Max direct block size
				binary.Write(buf, binary.LittleEndian, uint16(48))    // Max heap size (log2)
				binary.Write(buf, binary.LittleEndian, uint16(0))     // Start root indirect rows
				binary.Write(buf, binary.LittleEndian, uint64(10000)) // Root block address
				binary.Write(buf, binary.LittleEndian, uint16(0))     // Current row count

				return buf.Bytes()
			},
			sizeofSize: 8,
			sizeofAddr: 8,
			wantErr:    false,
			validate: func(t *testing.T, h *FractalHeapHeader) {
				assert.Equal(t, uint8(0), h.Version)
				assert.Equal(t, uint16(8), h.HeapIDLen)
				assert.Equal(t, uint32(4096), h.MaxManagedObjSize)
				assert.Equal(t, uint64(5), h.ManagedObjCount)
				assert.Equal(t, uint16(16), h.TableWidth)
				assert.Equal(t, uint64(1024), h.StartingBlockSize)
				assert.Equal(t, uint64(4096), h.MaxDirectBlockSize)
				assert.Equal(t, uint64(10000), h.RootBlockAddr)
				assert.True(t, h.ChecksumDirectBlocks)
			},
		},
		{
			name: "invalid signature",
			setup: func() []byte {
				buf := &bytes.Buffer{}
				buf.WriteString("BAAD") // Invalid signature
				buf.WriteByte(0)        // Version
				// Fill rest with zeros - need at least 22 + 12*8 + 3*8 = 142 bytes
				buf.Write(make([]byte, 200))
				return buf.Bytes()
			},
			sizeofSize:  8,
			sizeofAddr:  8,
			wantErr:     true,
			errContains: "invalid fractal heap signature",
		},
		{
			name: "unsupported version",
			setup: func() []byte {
				buf := &bytes.Buffer{}
				buf.WriteString("FRHP")
				buf.WriteByte(99) // Unsupported version
				// Fill rest with zeros - need enough for full header
				buf.Write(make([]byte, 200))
				return buf.Bytes()
			},
			sizeofSize:  8,
			sizeofAddr:  8,
			wantErr:     true,
			errContains: "unsupported fractal heap version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.setup()
			reader := bytes.NewReader(data)

			header, err := parseFractalHeapHeader(reader, 0, tt.sizeofSize, tt.sizeofAddr, binary.LittleEndian)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, header)
				if tt.validate != nil {
					tt.validate(t, header)
				}
			}
		})
	}
}

// TestComputeOffsetSize tests offset size computation.
func TestComputeOffsetSize(t *testing.T) {
	tests := []struct {
		value    uint64
		wantSize uint8
	}{
		{0, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{16777215, 3},
		{16777216, 4},
		{4294967295, 4},
		{4294967296, 5},
		{0xFFFFFFFFFFFFFFFF, 8},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("value_%d", tt.value), func(t *testing.T) {
			size := computeOffsetSize(tt.value)
			assert.Equal(t, tt.wantSize, size)
		})
	}
}

// TestReadUint tests variable-length unsigned integer reading.
func TestReadUint(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		size       int
		endianness binary.ByteOrder
		want       uint64
	}{
		{
			name:       "1 byte",
			data:       []byte{0x42},
			size:       1,
			endianness: binary.LittleEndian,
			want:       0x42,
		},
		{
			name:       "2 bytes little-endian",
			data:       []byte{0x34, 0x12},
			size:       2,
			endianness: binary.LittleEndian,
			want:       0x1234,
		},
		{
			name:       "2 bytes big-endian",
			data:       []byte{0x12, 0x34},
			size:       2,
			endianness: binary.BigEndian,
			want:       0x1234,
		},
		{
			name:       "4 bytes little-endian",
			data:       []byte{0x78, 0x56, 0x34, 0x12},
			size:       4,
			endianness: binary.LittleEndian,
			want:       0x12345678,
		},
		{
			name:       "8 bytes little-endian",
			data:       []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11},
			size:       8,
			endianness: binary.LittleEndian,
			want:       0x1122334455667788,
		},
		{
			name:       "insufficient data",
			data:       []byte{0x12},
			size:       4,
			endianness: binary.LittleEndian,
			want:       0, // Returns 0 when data too short
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := readUint(tt.data, tt.size, tt.endianness)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestOpenFractalHeap tests opening a fractal heap.
func TestOpenFractalHeap(t *testing.T) {
	t.Run("invalid address", func(t *testing.T) {
		reader := bytes.NewReader([]byte{})
		_, err := OpenFractalHeap(reader, 0, 8, 8, binary.LittleEndian)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid fractal heap address")
	})

	t.Run("undefined address", func(t *testing.T) {
		reader := bytes.NewReader([]byte{})
		_, err := OpenFractalHeap(reader, ^uint64(0), 8, 8, binary.LittleEndian)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid fractal heap address")
	})
}
