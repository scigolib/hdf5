package structures

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

func TestParseSymbolTableNode_Success(t *testing.T) {
	tests := []struct {
		name            string
		data            []byte
		address         uint64
		offsetSize      uint8
		expectedSymbols uint16
		checkEntries    func(*testing.T, *SymbolTableNode)
	}{
		{
			name: "single symbol - offset size 8",
			data: func() []byte {
				buf := make([]byte, 2048)
				// Signature "SNOD"
				copy(buf[0:4], "SNOD")
				// Version (1 byte)
				buf[4] = 1
				// Reserved (1 byte)
				buf[5] = 0
				// Number of symbols (2 bytes)
				binary.LittleEndian.PutUint16(buf[6:8], 1)

				// Entry starts at offset 8
				// Link name offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[8:16], 0x100)
				// Object header address (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 0x200)
				// Cache type (4 bytes)
				binary.LittleEndian.PutUint32(buf[24:28], 1)
				// Reserved (4 bytes)
				binary.LittleEndian.PutUint32(buf[28:32], 0)
				// Scratch-pad (16 bytes)
				for i := 0; i < 16; i++ {
					buf[32+i] = 0
				}

				return buf
			}(),
			address:         0,
			offsetSize:      8,
			expectedSymbols: 1,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Len(t, node.Entries, 1)
				require.Equal(t, uint64(0x100), node.Entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x200), node.Entries[0].ObjectAddress)
				require.Equal(t, uint32(1), node.Entries[0].CacheType)
			},
		},
		{
			name: "multiple symbols - offset size 8",
			data: func() []byte {
				buf := make([]byte, 4096)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 3)

				// Entry size: 8 + 8 + 4 + 4 + 16 = 40 bytes
				offset := 8
				for i := 0; i < 3; i++ {
					binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*0x100)
					binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*0x200)
					binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
					binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
					// Scratch-pad
					for j := 0; j < 16; j++ {
						buf[offset+24+j] = 0
					}
					offset += 40
				}

				return buf
			}(),
			address:         0,
			offsetSize:      8,
			expectedSymbols: 3,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Len(t, node.Entries, 3)
				require.Equal(t, uint64(0x000), node.Entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x100), node.Entries[1].LinkNameOffset)
				require.Equal(t, uint64(0x200), node.Entries[2].LinkNameOffset)
			},
		},
		{
			name: "offset size 4",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 1)

				// Entry: 4 + 4 + 4 + 4 + 16 = 32 bytes
				offset := 8
				binary.LittleEndian.PutUint32(buf[offset:offset+4], 0xAAA)
				binary.LittleEndian.PutUint32(buf[offset+4:offset+8], 0xBBB)
				binary.LittleEndian.PutUint32(buf[offset+8:offset+12], 5)
				binary.LittleEndian.PutUint32(buf[offset+12:offset+16], 0)
				for i := 0; i < 16; i++ {
					buf[offset+16+i] = 0
				}

				return buf
			}(),
			address:         0,
			offsetSize:      4,
			expectedSymbols: 1,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Len(t, node.Entries, 1)
				require.Equal(t, uint64(0xAAA), node.Entries[0].LinkNameOffset)
				require.Equal(t, uint64(0xBBB), node.Entries[0].ObjectAddress)
				require.Equal(t, uint32(5), node.Entries[0].CacheType)
			},
		},
		{
			name: "offset size 2",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 1)

				// Entry: 2 + 2 + 4 + 4 + 16 = 28 bytes
				offset := 8
				binary.LittleEndian.PutUint16(buf[offset:offset+2], 0x111)
				binary.LittleEndian.PutUint16(buf[offset+2:offset+4], 0x222)
				binary.LittleEndian.PutUint32(buf[offset+4:offset+8], 3)
				binary.LittleEndian.PutUint32(buf[offset+8:offset+12], 0)
				for i := 0; i < 16; i++ {
					buf[offset+12+i] = 0
				}

				return buf
			}(),
			address:         0,
			offsetSize:      2,
			expectedSymbols: 1,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Len(t, node.Entries, 1)
				require.Equal(t, uint64(0x111), node.Entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x222), node.Entries[0].ObjectAddress)
			},
		},
		{
			name: "zero symbols",
			data: func() []byte {
				buf := make([]byte, 1024)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 0) // Zero symbols
				return buf
			}(),
			address:         0,
			offsetSize:      8,
			expectedSymbols: 0,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Empty(t, node.Entries)
			},
		},
		{
			name: "non-zero address",
			data: func() []byte {
				buf := make([]byte, 3000)
				offset := 1000
				copy(buf[offset:offset+4], "SNOD")
				buf[offset+4] = 1
				buf[offset+5] = 0
				binary.LittleEndian.PutUint16(buf[offset+6:offset+8], 1)

				entryOffset := offset + 8
				binary.LittleEndian.PutUint64(buf[entryOffset:entryOffset+8], 0x999)
				binary.LittleEndian.PutUint64(buf[entryOffset+8:entryOffset+16], 0x888)
				binary.LittleEndian.PutUint32(buf[entryOffset+16:entryOffset+20], 7)
				binary.LittleEndian.PutUint32(buf[entryOffset+20:entryOffset+24], 0)
				for i := 0; i < 16; i++ {
					buf[entryOffset+24+i] = 0
				}

				return buf
			}(),
			address:         1000,
			offsetSize:      8,
			expectedSymbols: 1,
			checkEntries: func(t *testing.T, node *SymbolTableNode) {
				require.Equal(t, uint64(0x999), node.Entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x888), node.Entries[0].ObjectAddress)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()
			sb.OffsetSize = tt.offsetSize

			node, err := ParseSymbolTableNode(reader, tt.address, sb)
			require.NoError(t, err)
			require.NotNil(t, node)
			require.Equal(t, uint8(1), node.Version)
			require.Equal(t, tt.expectedSymbols, node.NumSymbols)

			if tt.checkEntries != nil {
				tt.checkEntries(t, node)
			}
		})
	}
}

func TestParseSymbolTableNode_InvalidSignature(t *testing.T) {
	tests := []struct {
		name      string
		signature string
	}{
		{"wrong signature", "XXXX"},
		{"partial signature", "SN\x00\x00"},
		{"empty signature", "\x00\x00\x00\x00"},
		{"close but wrong", "SNOT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			copy(buf[0:4], tt.signature)
			buf[4] = 1
			buf[5] = 0
			binary.LittleEndian.PutUint16(buf[6:8], 0)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			node, err := ParseSymbolTableNode(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, node)
			require.Contains(t, err.Error(), "invalid SNOD signature")
		})
	}
}

func TestParseSymbolTableNode_UnsupportedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version uint8
	}{
		{"version 0", 0},
		{"version 2", 2},
		{"version 255", 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			copy(buf[0:4], "SNOD")
			buf[4] = tt.version
			buf[5] = 0
			binary.LittleEndian.PutUint16(buf[6:8], 0)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			node, err := ParseSymbolTableNode(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, node)
			require.Contains(t, err.Error(), "unsupported SNOD version")
		})
	}
}

func TestParseSymbolTableNode_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (*mockReaderAt, *core.Superblock)
		wantErr string
	}{
		{
			name: "header read error",
			setup: func() (*mockReaderAt, *core.Superblock) {
				return &mockReaderAt{
					data: []byte{},
					err:  errors.New("IO error"),
				}, createMockSuperblock()
			},
			wantErr: "SNOD header read failed",
		},
		{
			name: "insufficient header data",
			setup: func() (*mockReaderAt, *core.Superblock) {
				return &mockReaderAt{
					data: []byte{0x00, 0x01, 0x02},
				}, createMockSuperblock()
			},
			wantErr: "",
		},
		{
			name: "entries data read error",
			setup: func() (*mockReaderAt, *core.Superblock) {
				buf := make([]byte, 8) // Just header
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 5) // Claims 5 entries but no data
				return &mockReaderAt{data: buf}, createMockSuperblock()
			},
			wantErr: "SNOD entries read failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, sb := tt.setup()
			node, err := ParseSymbolTableNode(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, node)
			if tt.wantErr != "" {
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseSymbolTableNode_BigEndian(t *testing.T) {
	buf := make([]byte, 2048)
	copy(buf[0:4], "SNOD")
	buf[4] = 1
	buf[5] = 0
	binary.BigEndian.PutUint16(buf[6:8], 1)

	// Entry in big-endian
	binary.BigEndian.PutUint64(buf[8:16], 0x123456789ABCDEF0)
	binary.BigEndian.PutUint64(buf[16:24], 0xFEDCBA0987654321)
	binary.BigEndian.PutUint32(buf[24:28], 0x12345678)
	binary.BigEndian.PutUint32(buf[28:32], 0x87654321)
	for i := 0; i < 16; i++ {
		buf[32+i] = 0
	}

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	node, err := ParseSymbolTableNode(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, node)
	require.Len(t, node.Entries, 1)
	require.Equal(t, uint64(0x123456789ABCDEF0), node.Entries[0].LinkNameOffset)
	require.Equal(t, uint64(0xFEDCBA0987654321), node.Entries[0].ObjectAddress)
	require.Equal(t, uint32(0x12345678), node.Entries[0].CacheType)
	require.Equal(t, uint32(0x87654321), node.Entries[0].Reserved)
}

func TestParseSymbolTableNode_DataTruncation(t *testing.T) {
	// Test truncated data in entry parsing
	buf := make([]byte, 20) // Not enough for a full entry
	copy(buf[0:4], "SNOD")
	buf[4] = 1
	buf[5] = 0
	binary.LittleEndian.PutUint16(buf[6:8], 1)
	// Entry data is missing/truncated

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	node, err := ParseSymbolTableNode(reader, 0, sb)
	require.Error(t, err)
	require.Nil(t, node)
}

func TestReadAddressFromBytes(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		size       int
		endianness binary.ByteOrder
		expected   uint64
	}{
		{
			name:       "1 byte",
			data:       []byte{0x42, 0xFF, 0xFF},
			size:       1,
			endianness: binary.LittleEndian,
			expected:   0x42,
		},
		{
			name:       "2 bytes - little endian",
			data:       []byte{0x34, 0x12, 0xFF},
			size:       2,
			endianness: binary.LittleEndian,
			expected:   0x1234,
		},
		{
			name:       "2 bytes - big endian",
			data:       []byte{0x12, 0x34, 0xFF},
			size:       2,
			endianness: binary.BigEndian,
			expected:   0x1234,
		},
		{
			name:       "4 bytes - little endian",
			data:       []byte{0x78, 0x56, 0x34, 0x12, 0xFF},
			size:       4,
			endianness: binary.LittleEndian,
			expected:   0x12345678,
		},
		{
			name:       "4 bytes - big endian",
			data:       []byte{0x12, 0x34, 0x56, 0x78, 0xFF},
			size:       4,
			endianness: binary.BigEndian,
			expected:   0x12345678,
		},
		{
			name:       "8 bytes - little endian",
			data:       []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12},
			size:       8,
			endianness: binary.LittleEndian,
			expected:   0x123456789ABCDEF0,
		},
		{
			name:       "8 bytes - big endian",
			data:       []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0},
			size:       8,
			endianness: binary.BigEndian,
			expected:   0x123456789ABCDEF0,
		},
		{
			name:       "3 bytes (padded) - little endian",
			data:       []byte{0x01, 0x02, 0x03, 0xFF},
			size:       3,
			endianness: binary.LittleEndian,
			expected:   0x030201,
		},
		{
			name:       "size exceeds data",
			data:       []byte{0x01, 0x02},
			size:       10,
			endianness: binary.LittleEndian,
			expected:   0x0201,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := readAddressFromBytes(tt.data, tt.size, tt.endianness)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSymbolTableNode_Structure(t *testing.T) {
	// Verify structure fields
	entries := []SymbolTableEntry{
		{LinkNameOffset: 0x100, ObjectAddress: 0x200, CacheType: 1, Reserved: 0},
		{LinkNameOffset: 0x300, ObjectAddress: 0x400, CacheType: 2, Reserved: 0},
	}

	node := &SymbolTableNode{
		Version:    1,
		NumSymbols: 2,
		Entries:    entries,
	}

	require.Equal(t, uint8(1), node.Version)
	require.Equal(t, uint16(2), node.NumSymbols)
	require.Len(t, node.Entries, 2)
	require.Equal(t, entries, node.Entries)
}

func BenchmarkParseSymbolTableNode(b *testing.B) {
	buf := make([]byte, 4096)
	copy(buf[0:4], "SNOD")
	buf[4] = 1
	buf[5] = 0
	symbolCount := uint16(10)
	binary.LittleEndian.PutUint16(buf[6:8], symbolCount)

	offset := 8
	for i := uint16(0); i < symbolCount; i++ {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*100)
		binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*200)
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
		for j := 0; j < 16; j++ {
			buf[offset+24+j] = 0
		}
		offset += 40
	}

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ParseSymbolTableNode(reader, 0, sb)
	}
}

func BenchmarkReadAddressFromBytes(b *testing.B) {
	data := []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = readAddressFromBytes(data, 8, binary.LittleEndian)
	}
}
