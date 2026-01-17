package structures

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// createTestBTreeWithSNOD creates test data with a B-tree that points to an SNOD.
// This properly tests the ReadGroupBTreeEntries function which follows SNOD pointers.
//
// Memory layout:
//   - Offset 0: B-tree header (TREE signature).
//   - After header: Keys and child pointers (interleaved).
//   - snodAddress: SNOD structure with entries.
func createTestBTreeWithSNOD(offsetSize uint8, endianness binary.ByteOrder, snodAddress uint64, entries []struct {
	linkNameOffset uint64
	objectAddress  uint64
	cacheType      uint32
}) []byte {
	buf := make([]byte, 4096)

	// B-tree header at offset 0
	copy(buf[0:4], "TREE")
	buf[4] = 0                          // Node type 0 (group)
	buf[5] = 0                          // Level 0 (leaf)
	endianness.PutUint16(buf[6:8], 1)   // 1 child pointer
	headerSize := 8 + int(offsetSize)*2 // sig(4) + type(1) + level(1) + entries(2) + 2*offsetSize (siblings)

	// Write sibling addresses (undefined)
	switch offsetSize {
	case 2:
		endianness.PutUint16(buf[8:10], 0xFFFF)
		endianness.PutUint16(buf[10:12], 0xFFFF)
	case 4:
		endianness.PutUint32(buf[8:12], 0xFFFFFFFF)
		endianness.PutUint32(buf[12:16], 0xFFFFFFFF)
	case 8:
		endianness.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
		endianness.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)
	}

	// Write keys and child pointers (interleaved)
	// Format: Key[0], Child[0], Key[1] (trailing key)
	pos := headerSize
	// Key[0] - heap offset (we don't use it in enumeration)
	switch offsetSize {
	case 2:
		endianness.PutUint16(buf[pos:], 0)
	case 4:
		endianness.PutUint32(buf[pos:], 0)
	case 8:
		endianness.PutUint64(buf[pos:], 0)
	}
	pos += int(offsetSize)

	// Child[0] - SNOD address
	switch offsetSize {
	case 2:
		endianness.PutUint16(buf[pos:], uint16(snodAddress))
	case 4:
		endianness.PutUint32(buf[pos:], uint32(snodAddress))
	case 8:
		endianness.PutUint64(buf[pos:], snodAddress)
	}

	// SNOD structure at snodAddress
	snodPos := int(snodAddress)
	copy(buf[snodPos:snodPos+4], "SNOD")
	buf[snodPos+4] = 1                                                   // Version
	buf[snodPos+5] = 0                                                   // Reserved
	endianness.PutUint16(buf[snodPos+6:snodPos+8], uint16(len(entries))) // NumSymbols

	// SNOD entries (each is 2*offsetSize + 4 + 4 + 16 = 40 bytes for offsetSize=8)
	entryPos := snodPos + 8

	for _, entry := range entries {
		// Link name offset
		switch offsetSize {
		case 2:
			endianness.PutUint16(buf[entryPos:], uint16(entry.linkNameOffset))
		case 4:
			endianness.PutUint32(buf[entryPos:], uint32(entry.linkNameOffset))
		case 8:
			endianness.PutUint64(buf[entryPos:], entry.linkNameOffset)
		}
		entryPos += int(offsetSize)

		// Object header address
		switch offsetSize {
		case 2:
			endianness.PutUint16(buf[entryPos:], uint16(entry.objectAddress))
		case 4:
			endianness.PutUint32(buf[entryPos:], uint32(entry.objectAddress))
		case 8:
			endianness.PutUint64(buf[entryPos:], entry.objectAddress)
		}
		entryPos += int(offsetSize)

		// Cache type
		endianness.PutUint32(buf[entryPos:entryPos+4], entry.cacheType)
		entryPos += 4

		// Reserved
		endianness.PutUint32(buf[entryPos:entryPos+4], 0)
		entryPos += 4

		// Scratch-pad (16 bytes, zeros)
		entryPos += 16
	}

	return buf
}

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
			data: createTestBTreeWithSNOD(8, binary.LittleEndian, 512, []struct {
				linkNameOffset uint64
				objectAddress  uint64
				cacheType      uint32
			}{
				{0x100, 0x200, 1},
			}),
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
			data: createTestBTreeWithSNOD(8, binary.LittleEndian, 512, []struct {
				linkNameOffset uint64
				objectAddress  uint64
				cacheType      uint32
			}{
				{0x000, 0x000, 0},
				{0x100, 0x200, 1},
				{0x200, 0x400, 2},
			}),
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
			data: createTestBTreeWithSNOD(4, binary.LittleEndian, 256, []struct {
				linkNameOffset uint64
				objectAddress  uint64
				cacheType      uint32
			}{
				{0xAAA, 0xBBB, 5},
			}),
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
			data: createTestBTreeWithSNOD(2, binary.LittleEndian, 128, []struct {
				linkNameOffset uint64
				objectAddress  uint64
				cacheType      uint32
			}{
				{0x111, 0x222, 3},
			}),
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
				buf := make([]byte, 24) // Just header, no key/child data
				copy(buf[0:4], "TREE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 5) // Claims 5 entries
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				binary.LittleEndian.PutUint64(buf[16:24], 0xFFFFFFFFFFFFFFFF)
				return &mockReaderAt{data: buf}, createMockSuperblock()
			},
			wantErr: "B-tree data read failed",
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
	// Big-endian test: B-tree addresses use little-endian in HDF5 format,
	// but SNOD entries use file's endianness.
	data := createTestBTreeWithSNOD(8, binary.BigEndian, 512, []struct {
		linkNameOffset uint64
		objectAddress  uint64
		cacheType      uint32
	}{
		{0x123456789ABCDEF0, 0xFEDCBA0987654321, 0x12345678},
	})

	reader := &mockReaderAt{data: data}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	entries, err := ReadGroupBTreeEntries(reader, 0, sb)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, uint64(0x123456789ABCDEF0), entries[0].LinkNameOffset)
	require.Equal(t, uint64(0xFEDCBA0987654321), entries[0].ObjectAddress)
	require.Equal(t, uint32(0x12345678), entries[0].CacheType)
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
			result := readAddress(tt.data, tt.size, binary.LittleEndian)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestReadAddress_BigEndian(t *testing.T) {
	// Test big-endian reading
	data := []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}
	result := readAddress(data, 8, binary.BigEndian)
	require.Equal(t, uint64(0x123456789ABCDEF0), result)

	// 4 bytes big-endian
	result4 := readAddress(data[:4], 4, binary.BigEndian)
	require.Equal(t, uint64(0x12345678), result4)

	// 2 bytes big-endian
	result2 := readAddress(data[:2], 2, binary.BigEndian)
	require.Equal(t, uint64(0x1234), result2)
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
		_ = readAddress(data, 8, binary.LittleEndian)
	}
}
