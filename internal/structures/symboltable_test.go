package structures

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSymbolTable_Success(t *testing.T) {
	tests := []struct {
		name            string
		data            []byte
		offset          uint64
		expectedEntry   *SymbolTable
		checkVersion    uint8
		checkEntryCount uint16
		checkBTreeAddr  uint64
		checkHeapAddr   uint64
	}{
		{
			name: "valid symbol table",
			data: func() []byte {
				buf := make([]byte, 1024)
				// Signature "SNOD"
				copy(buf[0:4], "SNOD")
				// Version (1 byte)
				buf[4] = 1
				// Reserved (1 byte)
				buf[5] = 0
				// Entry count (2 bytes)
				binary.LittleEndian.PutUint16(buf[6:8], 5)
				// B-tree address (8 bytes)
				binary.LittleEndian.PutUint64(buf[8:16], 0x1000)
				// Heap address (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 0x2000)
				return buf
			}(),
			offset:          0,
			checkVersion:    1,
			checkEntryCount: 5,
			checkBTreeAddr:  0x1000,
			checkHeapAddr:   0x2000,
		},
		{
			name: "zero entries",
			data: func() []byte {
				buf := make([]byte, 1024)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 0)
				binary.LittleEndian.PutUint64(buf[8:16], 0x1000)
				binary.LittleEndian.PutUint64(buf[16:24], 0x2000)
				return buf
			}(),
			offset:          0,
			checkVersion:    1,
			checkEntryCount: 0,
			checkBTreeAddr:  0x1000,
			checkHeapAddr:   0x2000,
		},
		{
			name: "non-zero offset",
			data: func() []byte {
				buf := make([]byte, 2048)
				offset := 500
				copy(buf[offset:offset+4], "SNOD")
				buf[offset+4] = 1
				buf[offset+5] = 0
				binary.LittleEndian.PutUint16(buf[offset+6:offset+8], 10)
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 0xAABB)
				binary.LittleEndian.PutUint64(buf[offset+16:offset+24], 0xCCDD)
				return buf
			}(),
			offset:          500,
			checkVersion:    1,
			checkEntryCount: 10,
			checkBTreeAddr:  0xAABB,
			checkHeapAddr:   0xCCDD,
		},
		{
			name: "large entry count",
			data: func() []byte {
				buf := make([]byte, 1024)
				copy(buf[0:4], "SNOD")
				buf[4] = 1
				buf[5] = 0
				binary.LittleEndian.PutUint16(buf[6:8], 65535)
				binary.LittleEndian.PutUint64(buf[8:16], 0xFFFFFFFFFFFFFFFF)
				binary.LittleEndian.PutUint64(buf[16:24], 0x1234567890ABCDEF)
				return buf
			}(),
			offset:          0,
			checkVersion:    1,
			checkEntryCount: 65535,
			checkBTreeAddr:  0xFFFFFFFFFFFFFFFF,
			checkHeapAddr:   0x1234567890ABCDEF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()

			st, err := ParseSymbolTable(reader, tt.offset, sb)
			require.NoError(t, err)
			require.NotNil(t, st)
			require.Equal(t, tt.checkVersion, st.Version)
			require.Equal(t, tt.checkEntryCount, st.EntryCount)
			require.Equal(t, tt.checkBTreeAddr, st.BTreeAddress)
			require.Equal(t, tt.checkHeapAddr, st.HeapAddress)
		})
	}
}

func TestParseSymbolTable_BigEndian(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "SNOD")
	buf[4] = 1
	buf[5] = 0
	binary.BigEndian.PutUint16(buf[6:8], 100)
	binary.BigEndian.PutUint64(buf[8:16], 0x123456789ABCDEF0)
	binary.BigEndian.PutUint64(buf[16:24], 0xFEDCBA0987654321)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	st, err := ParseSymbolTable(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.Equal(t, uint16(100), st.EntryCount)
	require.Equal(t, uint64(0x123456789ABCDEF0), st.BTreeAddress)
	require.Equal(t, uint64(0xFEDCBA0987654321), st.HeapAddress)
}

func TestParseSymbolTable_InvalidSignature(t *testing.T) {
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
			binary.LittleEndian.PutUint16(buf[6:8], 1)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			st, err := ParseSymbolTable(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, st)
			require.Contains(t, err.Error(), "invalid symbol table signature")
		})
	}
}

func TestParseSymbolTable_UnsupportedVersion(t *testing.T) {
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
			binary.LittleEndian.PutUint16(buf[6:8], 1)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			st, err := ParseSymbolTable(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, st)
			require.Contains(t, err.Error(), "unsupported symbol table version")
		})
	}
}

func TestParseSymbolTable_ReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *mockReaderAt
		wantErr string
	}{
		{
			name: "read error",
			setup: func() *mockReaderAt {
				return &mockReaderAt{
					data: []byte{},
					err:  errors.New("read error"),
				}
			},
			wantErr: "symbol table read failed",
		},
		{
			name: "insufficient data",
			setup: func() *mockReaderAt {
				return &mockReaderAt{
					data: []byte{0x00, 0x01, 0x02}, // Too short
				}
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := tt.setup()
			sb := createMockSuperblock()

			st, err := ParseSymbolTable(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, st)
			if tt.wantErr != "" {
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseSymbolTableEntry_Success(t *testing.T) {
	tests := []struct {
		name              string
		data              []byte
		offset            uint64
		expectedLinkOff   uint64
		expectedObjAddr   uint64
		expectedCacheType uint32
		expectedReserved  uint32
	}{
		{
			name: "basic entry",
			data: func() []byte {
				buf := make([]byte, 1024)
				binary.LittleEndian.PutUint64(buf[0:8], 0x100)
				binary.LittleEndian.PutUint64(buf[8:16], 0x200)
				binary.LittleEndian.PutUint32(buf[16:20], 1)
				binary.LittleEndian.PutUint32(buf[20:24], 0)
				return buf
			}(),
			offset:            0,
			expectedLinkOff:   0x100,
			expectedObjAddr:   0x200,
			expectedCacheType: 1,
			expectedReserved:  0,
		},
		{
			name: "non-zero offset",
			data: func() []byte {
				buf := make([]byte, 2048)
				offset := 1000
				binary.LittleEndian.PutUint64(buf[offset:offset+8], 0xAAA)
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 0xBBB)
				binary.LittleEndian.PutUint32(buf[offset+16:offset+20], 5)
				binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 9)
				return buf
			}(),
			offset:            1000,
			expectedLinkOff:   0xAAA,
			expectedObjAddr:   0xBBB,
			expectedCacheType: 5,
			expectedReserved:  9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()

			entry, err := ParseSymbolTableEntry(reader, tt.offset, sb)
			require.NoError(t, err)
			require.NotNil(t, entry)
			require.Equal(t, tt.expectedLinkOff, entry.LinkNameOffset)
			require.Equal(t, tt.expectedObjAddr, entry.ObjectAddress)
			require.Equal(t, tt.expectedCacheType, entry.CacheType)
			require.Equal(t, tt.expectedReserved, entry.Reserved)
		})
	}
}

func TestParseSymbolTableEntry_ReadError(t *testing.T) {
	reader := &mockReaderAt{
		data: []byte{},
		err:  errors.New("IO error"),
	}
	sb := createMockSuperblock()

	entry, err := ParseSymbolTableEntry(reader, 0, sb)
	require.Error(t, err)
	require.Nil(t, entry)
	require.Contains(t, err.Error(), "symbol table entry read failed")
}

func TestReadSymbolTableEntries_Success(t *testing.T) {
	tests := []struct {
		name          string
		setupTable    func() *SymbolTable
		setupData     func() []byte
		tableAddress  uint64
		expectedCount int
		checkEntries  func(*testing.T, []SymbolTableEntry)
	}{
		{
			name: "single entry",
			setupTable: func() *SymbolTable {
				return &SymbolTable{
					Version:      1,
					EntryCount:   1,
					BTreeAddress: 0x1000,
					HeapAddress:  0x2000,
				}
			},
			setupData: func() []byte {
				buf := make([]byte, 2048)
				// Entries start at offset 24 (after symbol table header)
				offset := 24
				binary.LittleEndian.PutUint64(buf[offset:offset+8], 0x100)
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 0x200)
				binary.LittleEndian.PutUint32(buf[offset+16:offset+20], 1)
				binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
				return buf
			},
			tableAddress:  0,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []SymbolTableEntry) {
				require.Equal(t, uint64(0x100), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x200), entries[0].ObjectAddress)
			},
		},
		{
			name: "multiple entries",
			setupTable: func() *SymbolTable {
				return &SymbolTable{
					Version:      1,
					EntryCount:   3,
					BTreeAddress: 0x1000,
					HeapAddress:  0x2000,
				}
			},
			setupData: func() []byte {
				buf := make([]byte, 2048)
				offset := 24
				for i := 0; i < 3; i++ {
					binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*0x100)
					binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*0x200)
					binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
					binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
					offset += 24
				}
				return buf
			},
			tableAddress:  0,
			expectedCount: 3,
			checkEntries: func(t *testing.T, entries []SymbolTableEntry) {
				require.Equal(t, uint64(0x000), entries[0].LinkNameOffset)
				require.Equal(t, uint64(0x100), entries[1].LinkNameOffset)
				require.Equal(t, uint64(0x200), entries[2].LinkNameOffset)
			},
		},
		{
			name: "zero entries",
			setupTable: func() *SymbolTable {
				return &SymbolTable{
					Version:      1,
					EntryCount:   0,
					BTreeAddress: 0x1000,
					HeapAddress:  0x2000,
				}
			},
			setupData: func() []byte {
				return make([]byte, 1024)
			},
			tableAddress:  0,
			expectedCount: 0,
			checkEntries:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.setupData()}
			sb := createMockSuperblock()
			table := tt.setupTable()

			entries, err := ReadSymbolTableEntries(reader, tt.tableAddress, table, sb)
			require.NoError(t, err)
			require.Len(t, entries, tt.expectedCount)

			if tt.checkEntries != nil {
				tt.checkEntries(t, entries)
			}
		})
	}
}

func TestReadSymbolTableEntries_ReadError(t *testing.T) {
	table := &SymbolTable{
		Version:      1,
		EntryCount:   1,
		BTreeAddress: 0x1000,
		HeapAddress:  0x2000,
	}

	reader := &mockReaderAt{
		data: make([]byte, 10), // Too short
	}
	sb := createMockSuperblock()

	entries, err := ReadSymbolTableEntries(reader, 0, table, sb)
	require.Error(t, err)
	require.Nil(t, entries)
}

func TestSymbolTableSignature(t *testing.T) {
	// Verify the constant
	require.Equal(t, "SNOD", SymbolTableSignature)
}

func BenchmarkParseSymbolTable(b *testing.B) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "SNOD")
	buf[4] = 1
	buf[5] = 0
	binary.LittleEndian.PutUint16(buf[6:8], 10)
	binary.LittleEndian.PutUint64(buf[8:16], 0x1000)
	binary.LittleEndian.PutUint64(buf[16:24], 0x2000)

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ParseSymbolTable(reader, 0, sb)
	}
}

func BenchmarkReadSymbolTableEntries(b *testing.B) {
	entryCount := uint16(20)
	buf := make([]byte, 8192)
	offset := 24
	for i := uint16(0); i < entryCount; i++ {
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i)*0x100)
		binary.LittleEndian.PutUint64(buf[offset+8:offset+16], uint64(i)*0x200)
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], uint32(i))
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], 0)
		offset += 24
	}

	table := &SymbolTable{
		Version:      1,
		EntryCount:   entryCount,
		BTreeAddress: 0x1000,
		HeapAddress:  0x2000,
	}

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ReadSymbolTableEntries(reader, 0, table, sb)
	}
}
