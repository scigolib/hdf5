package structures

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

func TestReadGroupBTreeEntries_Success(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		address       uint64
		offsetSize    uint8
		expectedCount int
		checkEntries  func(*testing.T, []BTreeEntry)
	}{
		{
			name: "single entry - offset size 8",
			data: func() []byte {
				buf := make([]byte, 2048)
				// Signature "TREE"
				copy(buf[0:4], "TREE")
				// Node type (1 byte) - 0 for groups
				buf[4] = 0
				// Node level (1 byte) - 0 for leaf
				buf[5] = 0
				// Entries used (2 bytes)
				binary.LittleEndian.PutUint16(buf[6:8], 1)
				// Left sibling address (8 bytes)
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				// Right sibling address (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)

				// Entry starts at offset 24
				// Link name offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[24:32], 0x100)
				// Object header address (8 bytes)
				binary.LittleEndian.PutUint64(buf[32:40], 0x200)
				// Cache type (4 bytes)
				binary.LittleEndian.PutUint32(buf[40:44], 1)
				// Reserved (4 bytes)
				binary.LittleEndian.PutUint32(buf[44:48], 0)

				return buf
			}(),
			address:       0,
			offsetSize:    8,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0x100), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x200), entries[0].ObjectAddress)
				require.Equal(t, uint32(1), entries[0].CacheType)
			},
		},
		{
			name: "multiple entries - offset size 8",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 3)
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)

				offset := 24
				for i := 0; i < 3; i++ {
					binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*0x100)
					binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*0x200)
					binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
					binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
					offset += 24
				}

				return buf
			}(),
			address:       0,
			offsetSize:    8,
			expectedCount: 3,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0x000), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x100), entries[1].LinkNameOffset)
				require.Equal(t, uint64(0x200), entries[2].LinkNameOffset)
			},
		},
		{
			name: "offset size 4",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 1)
				// Left sibling (4 bytes for offset size 4)
				binary.LittleEndian.PutUint32(buf[8:12], 0xFFFFFFFF)
				// Right sibling (4 bytes)
				binary.LittleEndian.PutUint32(buf[12:16], 0xFFFFFFFF)

				// Entry starts at offset 16 (header is 4+1+1+2+4+4=16)
				// Link name offset (4 bytes)
				binary.LittleEndian.PutUint32(buf[16:20], 0xAAA)
				// Object header address (4 bytes)
				binary.LittleEndian.PutUint32(buf[20:24], 0xBBB)
				// Cache type (4 bytes)
				binary.LittleEndian.PutUint32(buf[24:28], 5)
				// Reserved (4 bytes)
				binary.LittleEndian.PutUint32(buf[28:32], 0)

				return buf
			}(),
			address:       0,
			offsetSize:    4,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0xAAA), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0xBBB), entries[0].ObjectAddress)
				require.Equal(t, uint32(5), entries[0].CacheType)
			},
		},
		{
			name: "offset size 2",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 1)
				// Left sibling (2 bytes)
				binary.LittleEndian.PutUint16(buf[8:10], 0xFFFF)
				// Right sibling (2 bytes)
				binary.LittleEndian.PutUint16(buf[10:12], 0xFFFF)

				// Entry starts at offset 12
				binary.LittleEndian.PutUint16(buf[12:14], 0x111)
				binary.LittleEndian.PutUint16(buf[14:16], 0x222)
				binary.LittleEndian.PutUint32(buf[16:20], 3)
				binary.LittleEndian.PutUint32(buf[20:24], 0)

				return buf
			}(),
			address:       0,
			offsetSize:    2,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0x111), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x222), entries[0].ObjectAddress)
			},
		},
		{
			name: "zero entries",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 0) // Zero entries
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)
				return buf
			}(),
			address:       0,
			offsetSize:    8,
			expectedCount: 0,
			checkEntries:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()
			sb.OffsetSize = tt.offsetSize

			entries, err := ReadGroupBTreeEntries(reader, tt.address, sb)
			require.NoError(t, err)
			require.Len(t, entries, tt.expectedCount)

			if tt.checkEntries != nil {
				tt.checkEntries(t, entries)
			}
		})
	}
}

func TestReadGroupBTreeEntries_InvalidSignature(t *testing.T) {
	tests := []struct {
		name      string
		signature string
	}{
		{"wrong signature", "XXXX"},
		{"partial signature", "TR\x00\x00"},
		{"empty signature", "\x00\x00\x00\x00"},
		{"close but wrong", "TRES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			copy(buf[0:4], tt.signature)
			buf[4] = 0
			buf[5] = 0
			binary.LittleEndian.PutUint16(buf[6:8], 1)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			entries, err := ReadGroupBTreeEntries(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, entries)
			require.Contains(t, err.Error(), "invalid B-tree signature")
		})
	}
}

func TestReadGroupBTreeEntries_InvalidNodeType(t *testing.T) {
	tests := []struct {
		name     string
		nodeType uint8
	}{
		{"type 1", 1},
		{"type 2", 2},
		{"type 255", 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			copy(buf[0:4], "TREE")
			buf[4] = tt.nodeType // Wrong type
			buf[5] = 0
			binary.LittleEndian.PutUint16(buf[6:8], 1)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			entries, err := ReadGroupBTreeEntries(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, entries)
			require.Contains(t, err.Error(), "expected group B-tree")
		})
	}
}

func TestReadGroupBTreeEntries_NonLeafNode(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "TREE")
	buf[4] = 0
	buf[5] = 1 // Level 1 (non-leaf)
	binary.LittleEndian.PutUint16(buf[6:8], 1)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	entries, err := ReadGroupBTreeEntries(reader, 0, sb)
	require.Error(t, err)
	require.Nil(t, entries)
	require.Contains(t, err.Error(), "non-leaf B-tree nodes not supported")
}

func TestReadGroupBTreeEntries_ReadErrors(t *testing.T) {
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
			wantErr: "B-tree node header read failed",
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
				buf := make([]byte, 24) // Just header, no entry data
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 5) // Claims 5 entries
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)
				return &mockReaderAt{data: buf}, createMockSuperblock()
			},
			wantErr: "B-tree entries read failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, sb := tt.setup()
			entries, err := ReadGroupBTreeEntries(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, entries)
			if tt.wantErr != "" {
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReadGroupBTreeEntries_BigEndian(t *testing.T) {
	// Note: readAddress function in btree_group.go always uses LittleEndian
	// This is consistent with HDF5 B-tree format which uses little-endian regardless of file endianness
	buf := make([]byte, 2048)
	copy(buf[0:4], "TREE")
	buf[4] = 0
	buf[5] = 0
	binary.BigEndian.PutUint16(buf[6:8], 1)
	binary.BigEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
	binary.BigEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)

	// Entry - addresses use little-endian in B-tree format
	binary.LittleEndian.PutUint64(buf[24:32], 0x123456789ABCDEF0)
	binary.LittleEndian.PutUint64(buf[32:40], 0xFEDCBA0987654321)
	binary.BigEndian.PutUint32(buf[40:44], 0x12345678)
	binary.BigEndian.PutUint32(buf[44:48], 0x87654321)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	entries, err := ReadGroupBTreeEntries(reader, 0, sb)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, uint64(0x123456789ABCDEF0), entries[0].LinkNameOffset)
	require.Equal(t, uint64(0xFEDCBA0987654321), entries[0].ObjectAddress)
	require.Equal(t, uint32(0x12345678), entries[0].CacheType)
	require.Equal(t, uint32(0x87654321), entries[0].Reserved)
}

func TestReadAddress(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		size     int
		expected uint64
	}{
		{
			name:     "1 byte",
			data:     []byte{0x42, 0xFF, 0xFF},
			size:     1,
			expected: 0x42,
		},
		{
			name:     "2 bytes",
			data:     []byte{0x34, 0x12, 0xFF},
			size:     2,
			expected: 0x1234,
		},
		{
			name:     "4 bytes",
			data:     []byte{0x78, 0x56, 0x34, 0x12, 0xFF},
			size:     4,
			expected: 0x12345678,
		},
		{
			name:     "8 bytes",
			data:     []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12},
			size:     8,
			expected: 0x123456789ABCDEF0,
		},
		{
			name:     "3 bytes (padded)",
			data:     []byte{0x01, 0x02, 0x03, 0xFF},
			size:     3,
			expected: 0x030201,
		},
		{
			name:     "size exceeds data length",
			data:     []byte{0x01, 0x02},
			size:     10,
			expected: 0x0201,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := readAddress(tt.data, tt.size)
			require.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkReadGroupBTreeEntries(b *testing.B) {
	buf := make([]byte, 8192)
	copy(buf[0:4], "TREE")
	buf[4] = 0
	buf[5] = 0
	entryCount := uint16(10)
	binary.LittleEndian.PutUint16(buf[6:8], entryCount)
	binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
	binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)

	offset := 24
	for i := uint16(0); i < entryCount; i++ {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*100)
		binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*200)
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
		offset += 24
	}

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ReadGroupBTreeEntries(reader, 0, sb)
	}
}

func BenchmarkReadAddress(b *testing.B) {
	data := []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = readAddress(data, 8)
	}
}
