package structures

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// mockReaderAt is a simple mock implementation of io.ReaderAt for testing.
type mockReaderAt struct {
	data []byte
	err  error
}

func (m *mockReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if m.err != nil {
		return 0, m.err
	}

	if off < 0 || off >= int64(len(m.data)) {
		return 0, io.EOF
	}

	n = copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// createMockSuperblock creates a minimal superblock for testing.
func createMockSuperblock() *core.Superblock {
	return &core.Superblock{
		Version:    0,
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
		RootGroup:  0x60,
	}
}

func TestReadBTreeEntries_Success(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		address       uint64
		expectedCount int
		checkEntries  func(*testing.T, []BTreeEntry)
	}{
		{
			name: "single entry",
			data: func() []byte {
				buf := make([]byte, 1024)
				// B-tree signature
				copy(buf[0:4], "BTRE")
				// Node type (1 byte)
				buf[4] = 0
				// Level (1 byte) - 0 for leaf
				buf[5] = 0
				// Entry count (2 bytes, little-endian)
				binary.LittleEndian.PutUint16(buf[6:8], 1)

				// Entry 1 at offset 8
				// LinkNameOffset (8 bytes)
				binary.LittleEndian.PutUint64(buf[8:16], 0x100)
				// ObjectAddress (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 0x200)
				// CacheType (4 bytes)
				binary.LittleEndian.PutUint32(buf[24:28], 1)
				// Reserved (4 bytes)
				binary.LittleEndian.PutUint32(buf[28:32], 0)

				return buf
			}(),
			address:       0,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0x100), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x200), entries[0].ObjectAddress)
				require.Equal(t, uint32(1), entries[0].CacheType)
				require.Equal(t, uint32(0), entries[0].Reserved)
			},
		},
		{
			name: "multiple entries",
			data: func() []byte {
				buf := make([]byte, 1024)
				// B-tree signature
				copy(buf[0:4], "BTRE")
				// Node type (1 byte)
				buf[4] = 0
				// Level (1 byte) - 0 for leaf
				buf[5] = 0
				// Entry count (2 bytes, little-endian) - 3 entries
				binary.LittleEndian.PutUint16(buf[6:8], 3)

				// Entry 1 at offset 8
				binary.LittleEndian.PutUint64(buf[8:16], 0x100)
				binary.LittleEndian.PutUint64(buf[16:24], 0x200)
				binary.LittleEndian.PutUint32(buf[24:28], 1)
				binary.LittleEndian.PutUint32(buf[28:32], 0)

				// Entry 2 at offset 32 (8 + 24)
				binary.LittleEndian.PutUint64(buf[32:40], 0x300)
				binary.LittleEndian.PutUint64(buf[40:48], 0x400)
				binary.LittleEndian.PutUint32(buf[48:52], 2)
				binary.LittleEndian.PutUint32(buf[52:56], 0)

				// Entry 3 at offset 56 (8 + 48)
				binary.LittleEndian.PutUint64(buf[56:64], 0x500)
				binary.LittleEndian.PutUint64(buf[64:72], 0x600)
				binary.LittleEndian.PutUint32(buf[72:76], 3)
				binary.LittleEndian.PutUint32(buf[76:80], 0)

				return buf
			}(),
			address:       0,
			expectedCount: 3,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0x100), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x300), entries[1].LinkNameOffset)
				require.Equal(t, uint64(0x500), entries[2].LinkNameOffset)
			},
		},
		{
			name: "non-zero address",
			data: func() []byte {
				buf := make([]byte, 2048)
				offset := 1000
				// B-tree signature at offset 1000
				copy(buf[offset:offset+4], "BTRE")
				buf[offset+4] = 0
				buf[offset+5] = 0
				binary.LittleEndian.PutUint16(buf[offset+6:offset+8], 1)

				// Entry at offset+8
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 0xAAA)
				binary.LittleEndian.PutUint64(buf[offset+16:offset+24], 0xBBB)
				binary.LittleEndian.PutUint32(buf[offset+24:offset+28], 5)
				binary.LittleEndian.PutUint32(buf[offset+28:offset+32], 0)

				return buf
			}(),
			address:       1000,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []BTreeEntry) {
				require.Equal(t, uint64(0xAAA), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0xBBB), entries[0].ObjectAddress)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()

			entries, err := ReadBTreeEntries(reader, tt.address, sb)
			require.NoError(t, err)
			require.Len(t, entries, tt.expectedCount)

			if tt.checkEntries != nil {
				tt.checkEntries(t, entries)
			}
		})
	}
}

func TestReadBTreeEntries_InvalidSignature(t *testing.T) {
	tests := []struct {
		name      string
		signature string
	}{
		{"wrong signature", "XXXX"},
		{"partial signature", "BT\x00\x00"},
		{"empty signature", "\x00\x00\x00\x00"},
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

			entries, err := ReadBTreeEntries(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, entries)
			require.Contains(t, err.Error(), "invalid B-tree signature")
		})
	}
}

func TestReadBTreeEntries_NonLeafNode(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "BTRE")
	buf[4] = 0
	buf[5] = 1 // Level 1 (non-leaf)
	binary.LittleEndian.PutUint16(buf[6:8], 1)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	entries, err := ReadBTreeEntries(reader, 0, sb)
	require.Error(t, err)
	require.Nil(t, entries)
	require.Contains(t, err.Error(), "non-leaf nodes not supported")
}

func TestReadBTreeEntries_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (*mockReaderAt, *core.Superblock)
		wantErr string
	}{
		{
			name: "read header fails",
			setup: func() (*mockReaderAt, *core.Superblock) {
				reader := &mockReaderAt{
					data: []byte{},
					err:  errors.New("read error"),
				}
				return reader, createMockSuperblock()
			},
			wantErr: "B-tree node read failed",
		},
		{
			name: "insufficient data for header",
			setup: func() (*mockReaderAt, *core.Superblock) {
				reader := &mockReaderAt{
					data: []byte{0x00, 0x01, 0x02}, // Too short
				}
				return reader, createMockSuperblock()
			},
			wantErr: "",
		},
		{
			name: "entry count read fails",
			setup: func() (*mockReaderAt, *core.Superblock) {
				buf := make([]byte, 6) // Only enough for signature + type + level
				copy(buf[0:4], "BTRE")
				buf[4] = 0
				buf[5] = 0
				// No space for entry count
				reader := &mockReaderAt{data: buf}
				return reader, createMockSuperblock()
			},
			wantErr: "",
		},
		{
			name: "entry data read fails",
			setup: func() (*mockReaderAt, *core.Superblock) {
				buf := make([]byte, 10)
				copy(buf[0:4], "BTRE")
				buf[4] = 0
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 2) // 2 entries
				// But not enough space for 2 entries (2 * 24 = 48 bytes needed)
				reader := &mockReaderAt{data: buf}
				return reader, createMockSuperblock()
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, sb := tt.setup()
			entries, err := ReadBTreeEntries(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, entries)
			if tt.wantErr != "" {
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReadBTreeEntries_BigEndian(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "BTRE")
	buf[4] = 0
	buf[5] = 0
	// Entry count in big-endian
	binary.BigEndian.PutUint16(buf[6:8], 1)

	// Entry in big-endian
	binary.BigEndian.PutUint64(buf[8:16], 0x123456789ABCDEF0)
	binary.BigEndian.PutUint64(buf[16:24], 0xFEDCBA9876543210)
	binary.BigEndian.PutUint32(buf[24:28], 0x12345678)
	binary.BigEndian.PutUint32(buf[28:32], 0x87654321)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	entries, err := ReadBTreeEntries(reader, 0, sb)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, uint64(0x123456789ABCDEF0), entries[0].LinkNameOffset)
	require.Equal(t, uint64(0xFEDCBA9876543210), entries[0].ObjectAddress)
	require.Equal(t, uint32(0x12345678), entries[0].CacheType)
	require.Equal(t, uint32(0x87654321), entries[0].Reserved)
}

func TestReadBTreeEntries_ZeroEntries(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "BTRE")
	buf[4] = 0
	buf[5] = 0
	// Zero entry count
	binary.LittleEndian.PutUint16(buf[6:8], 0)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	entries, err := ReadBTreeEntries(reader, 0, sb)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestBTreeEntry_Structure(t *testing.T) {
	// Verify the BTreeEntry structure holds the correct values
	entry := BTreeEntry{
		LinkNameOffset: 0x1234567890ABCDEF,
		ObjectAddress:  0xFEDCBA0987654321,
		CacheType:      0xABCDEF01,
		Reserved:       0x12345678,
	}

	require.Equal(t, uint64(0x1234567890ABCDEF), entry.LinkNameOffset)
	require.Equal(t, uint64(0xFEDCBA0987654321), entry.ObjectAddress)
	require.Equal(t, uint32(0xABCDEF01), entry.CacheType)
	require.Equal(t, uint32(0x12345678), entry.Reserved)
}

func BenchmarkReadBTreeEntries(b *testing.B) {
	// Create a B-tree with 10 entries
	buf := make([]byte, 8192)
	copy(buf[0:4], "BTRE")
	buf[4] = 0
	buf[5] = 0
	entryCount := uint16(10)
	binary.LittleEndian.PutUint16(buf[6:8], entryCount)

	// Fill in entries
	offset := 8
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
		_, _ = ReadBTreeEntries(reader, 0, sb)
	}
}
