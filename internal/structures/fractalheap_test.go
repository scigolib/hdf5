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
				binary.Write(buf, binary.LittleEndian, uint64(4096))  // Space size
				binary.Write(buf, binary.LittleEndian, uint64(4096))  // Alloc size
				binary.Write(buf, binary.LittleEndian, uint64(0))     // Iter offset
				binary.Write(buf, binary.LittleEndian, uint64(5))     // Object count

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

// TestParseHeapID tests heap ID parsing.
func TestParseHeapID(t *testing.T) {
	tests := []struct {
		name        string
		heapID      []byte
		offsetSize  uint8
		lengthSize  uint8
		wantType    HeapIDType
		wantOffset  uint64
		wantLength  uint64
		wantErr     bool
		errContains string
	}{
		{
			name: "managed object - simple",
			heapID: []byte{
				0x00,       // Version 0, Type Managed (0x00)
				0x80, 0x00, // Offset: 128 (2 bytes, little-endian)
				0x40, 0x00, // Length: 64 (2 bytes, little-endian)
			},
			offsetSize: 2,
			lengthSize: 2,
			wantType:   HeapIDTypeManaged,
			wantOffset: 128,
			wantLength: 64,
			wantErr:    false,
		},
		{
			name: "managed object - larger",
			heapID: []byte{
				0x00,                   // Version 0, Type Managed
				0x00, 0x10, 0x00, 0x00, // Offset: 4096 (4 bytes)
				0x00, 0x04, 0x00, 0x00, // Length: 1024 (4 bytes)
			},
			offsetSize: 4,
			lengthSize: 4,
			wantType:   HeapIDTypeManaged,
			wantOffset: 4096,
			wantLength: 1024,
			wantErr:    false,
		},
		{
			name:   "tiny object",
			heapID: []byte{0x20, 0x01, 0x02, 0x03}, // Type Tiny + 3 bytes data
			wantType:   HeapIDTypeTiny,
			wantLength: 3,
			wantErr:    false,
		},
		{
			name:        "huge object - not supported",
			heapID:      []byte{0x10, 0x00, 0x00}, // Type Huge
			wantType:    HeapIDTypeHuge,
			wantErr:     false, // Parsing succeeds, but reading will fail
		},
		{
			name:        "empty heap ID",
			heapID:      []byte{},
			wantErr:     true,
			errContains: "heap ID too short",
		},
		{
			name:        "unsupported version",
			heapID:      []byte{0x40}, // Version 1 (bits 6-7 = 01)
			wantErr:     true,
			errContains: "unsupported heap ID version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal heap for testing
			heap := &FractalHeap{
				Header: &FractalHeapHeader{
					HeapOffsetSize: tt.offsetSize,
					HeapLengthSize: tt.lengthSize,
				},
				endianness: binary.LittleEndian,
			}

			id, err := heap.parseHeapID(tt.heapID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, id)
				assert.Equal(t, tt.wantType, id.Type)
				if tt.wantOffset > 0 {
					assert.Equal(t, tt.wantOffset, id.Offset)
				}
				if tt.wantLength > 0 {
					assert.Equal(t, tt.wantLength, id.Length)
				}
			}
		})
	}
}

// TestReadTinyObject tests reading tiny objects (data in heap ID).
func TestReadTinyObject(t *testing.T) {
	tests := []struct {
		name     string
		heapID   []byte
		wantData []byte
	}{
		{
			name:     "tiny object with data",
			heapID:   []byte{0x20, 0x01, 0x02, 0x03, 0x04}, // Tiny type + 4 bytes
			wantData: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			name:     "tiny object single byte",
			heapID:   []byte{0x20, 0xFF}, // Tiny type + 1 byte
			wantData: []byte{0xFF},
		},
		{
			name:     "tiny object empty",
			heapID:   []byte{0x20}, // Tiny type, no data
			wantData: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := &FractalHeap{
				Header: &FractalHeapHeader{
					HeapOffsetSize: 2,
					HeapLengthSize: 2,
				},
				endianness: binary.LittleEndian,
			}

			id, err := heap.parseHeapID(tt.heapID)
			require.NoError(t, err)

			data, err := heap.readTinyObject(id)
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, data)
		})
	}
}

// TestReadDirectBlock tests reading direct blocks.
func TestReadDirectBlock(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() []byte
		blockSize   uint64
		offsetSize  uint8
		headerAddr  uint64
		checksummed bool
		wantErr     bool
		errContains string
		validate    func(*testing.T, *DirectBlock)
	}{
		{
			name: "valid direct block",
			setup: func() []byte {
				buf := &bytes.Buffer{}

				// Signature: "FHDB"
				buf.WriteString("FHDB")

				// Version: 0
				buf.WriteByte(0)

				// Heap Header Address: 5000 (8 bytes)
				binary.Write(buf, binary.LittleEndian, uint64(5000))

				// Block Offset: 0 (2 bytes)
				binary.Write(buf, binary.LittleEndian, uint16(0))

				// Data: 100 bytes of test data
				testData := make([]byte, 100)
				for i := range testData {
					testData[i] = byte(i % 256)
				}
				buf.Write(testData)

				return buf.Bytes()
			},
			blockSize:   115, // 4+1+8+2 + 100 bytes
			offsetSize:  2,
			headerAddr:  5000,
			checksummed: false,
			wantErr:     false,
			validate: func(t *testing.T, db *DirectBlock) {
				assert.Equal(t, "FHDB", string(db.Signature[:]))
				assert.Equal(t, uint8(0), db.Version)
				assert.Equal(t, uint64(5000), db.HeapHeaderAddr)
				assert.Equal(t, uint64(0), db.BlockOffset)
				assert.Equal(t, 100, len(db.Data))
				// Verify test pattern
				for i := 0; i < 100; i++ {
					assert.Equal(t, byte(i%256), db.Data[i], "data mismatch at index %d", i)
				}
			},
		},
		{
			name: "invalid signature",
			setup: func() []byte {
				buf := &bytes.Buffer{}
				buf.WriteString("BAAD")
				buf.Write(make([]byte, 100))
				return buf.Bytes()
			},
			blockSize:   115,
			offsetSize:  2,
			headerAddr:  5000,
			wantErr:     true,
			errContains: "invalid direct block signature",
		},
		{
			name: "heap header address mismatch",
			setup: func() []byte {
				buf := &bytes.Buffer{}
				buf.WriteString("FHDB")
				buf.WriteByte(0)
				binary.Write(buf, binary.LittleEndian, uint64(9999)) // Wrong address
				binary.Write(buf, binary.LittleEndian, uint16(0))
				buf.Write(make([]byte, 100))
				return buf.Bytes()
			},
			blockSize:   115,
			offsetSize:  2,
			headerAddr:  5000,
			wantErr:     true,
			errContains: "heap header address mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.setup()
			// Pad data at the beginning so we can read from a non-zero address
			paddedData := make([]byte, 1000)
			copy(paddedData[100:], data) // Data starts at offset 100
			reader := bytes.NewReader(paddedData)

			heap := &FractalHeap{
				Header: &FractalHeapHeader{
					HeapOffsetSize:       tt.offsetSize,
					ChecksumDirectBlocks: tt.checksummed,
				},
				reader:     reader,
				headerAddr: tt.headerAddr,
				sizeofAddr: 8,
				endianness: binary.LittleEndian,
			}

			// Read from address 100 (where the data actually is)
			dblock, err := heap.readDirectBlock(100, tt.blockSize)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, dblock)
				if tt.validate != nil {
					tt.validate(t, dblock)
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
