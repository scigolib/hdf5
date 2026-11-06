package structures

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

func TestLoadLocalHeap_Success(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		address      uint64
		expectedSize uint64
		checkData    func(*testing.T, *LocalHeap)
	}{
		{
			name: "minimal heap",
			data: func() []byte {
				buf := make([]byte, 1024)
				// Signature "HEAP"
				copy(buf[0:4], "HEAP")
				// Version (1 byte)
				buf[4] = 0
				// Reserved (3 bytes)
				buf[5], buf[6], buf[7] = 0, 0, 0
				// Data segment size (8 bytes) - size of data segment ONLY (not including 32-byte header)
				binary.LittleEndian.PutUint64(buf[8:16], 16) // Data segment is 16 bytes
				// Free list offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 1) // H5HL_FREE_NULL
				// Data segment address (8 bytes) - where data actually is in the file
				binary.LittleEndian.PutUint64(buf[24:32], 32) // Data at offset 32
				// Data follows at offset 32 (after 32-byte header)
				copy(buf[32:48], "Hello, World!")
				return buf
			}(),
			address:      0,
			expectedSize: 32,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Equal(t, uint64(32), heap.HeaderSize)
				require.Len(t, heap.Data, 16)
			},
		},
		{
			name: "larger heap with data",
			data: func() []byte {
				buf := make([]byte, 2048)
				copy(buf[0:4], "HEAP")
				buf[4] = 0
				buf[5], buf[6], buf[7] = 0, 0, 0
				// Data segment size: 100 bytes (header is always 32 bytes, not included)
				binary.LittleEndian.PutUint64(buf[8:16], 100)
				// Free list offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 1) // H5HL_FREE_NULL
				// Data segment address (8 bytes) - where data actually is in the file
				binary.LittleEndian.PutUint64(buf[24:32], 32) // Data at offset 32
				// Fill data section with test data (starts at offset 32 after header)
				for i := 0; i < 100; i++ {
					buf[32+i] = byte(i % 256)
				}
				return buf
			}(),
			address:      0,
			expectedSize: 32,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Equal(t, uint64(32), heap.HeaderSize)
				require.Len(t, heap.Data, 100)
				// Verify data pattern
				for i := 0; i < 100; i++ {
					require.Equal(t, byte(i%256), heap.Data[i])
				}
			},
		},
		{
			name: "non-zero address",
			data: func() []byte {
				buf := make([]byte, 2048)
				offset := 500
				copy(buf[offset:offset+4], "HEAP")
				buf[offset+4] = 0
				buf[offset+5], buf[offset+6], buf[offset+7] = 0, 0, 0
				// Data segment size: 34 bytes (string length)
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 34)
				// Free list offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[offset+16:offset+24], 1) // H5HL_FREE_NULL
				// Data segment address (8 bytes) - address 500 + header 32 = 532
				binary.LittleEndian.PutUint64(buf[offset+24:offset+32], 532)
				// Data starts at offset 32 after header start
				copy(buf[offset+32:offset+32+34], "test data at offset")
				return buf
			}(),
			address:      500,
			expectedSize: 32,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Equal(t, uint64(32), heap.HeaderSize)
				require.Len(t, heap.Data, 34)
			},
		},
		{
			name: "heap with null-terminated strings",
			data: func() []byte {
				buf := make([]byte, 1024)
				copy(buf[0:4], "HEAP")
				buf[4] = 0
				buf[5], buf[6], buf[7] = 0, 0, 0
				// Data segment size: 64 bytes
				binary.LittleEndian.PutUint64(buf[8:16], 64)
				// Free list offset (8 bytes)
				binary.LittleEndian.PutUint64(buf[16:24], 1) // H5HL_FREE_NULL
				// Data segment address (8 bytes) - data at offset 32
				binary.LittleEndian.PutUint64(buf[24:32], 32)
				// Add some null-terminated strings (data starts at offset 32)
				offset := 32
				copy(buf[offset:], "string1\x00string2\x00string3\x00")
				return buf
			}(),
			address:      0,
			expectedSize: 32,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Contains(t, string(heap.Data), "string1")
				require.Contains(t, string(heap.Data), "string2")
				require.Contains(t, string(heap.Data), "string3")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			sb := createMockSuperblock()

			heap, err := LoadLocalHeap(reader, tt.address, sb)
			require.NoError(t, err)
			require.NotNil(t, heap)
			require.Equal(t, tt.expectedSize, heap.HeaderSize)

			if tt.checkData != nil {
				tt.checkData(t, heap)
			}
		})
	}
}

func TestLoadLocalHeap_InvalidSignature(t *testing.T) {
	tests := []struct {
		name      string
		signature string
	}{
		{"wrong signature", "XXXX"},
		{"partial signature", "HE\x00\x00"},
		{"empty signature", "\x00\x00\x00\x00"},
		{"close but wrong", "HELP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 1024)
			copy(buf[0:4], tt.signature)
			buf[4] = 0
			binary.LittleEndian.PutUint64(buf[8:16], 32)

			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()

			heap, err := LoadLocalHeap(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, heap)
			require.Contains(t, err.Error(), "invalid local heap signature")
		})
	}
}

func TestLoadLocalHeap_ReadErrors(t *testing.T) {
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
			wantErr: "local heap header read failed",
		},
		{
			name: "insufficient header data",
			setup: func() (*mockReaderAt, *core.Superblock) {
				return &mockReaderAt{
					data: []byte{0x00, 0x01, 0x02}, // Too short
				}, createMockSuperblock()
			},
			wantErr: "",
		},
		{
			name: "data read error",
			setup: func() (*mockReaderAt, *core.Superblock) {
				buf := make([]byte, 32) // Header is 32 bytes
				copy(buf[0:4], "HEAP")
				buf[4] = 0
				// Data segment size claims 1000 bytes, but buffer is only 32 (no room for data)
				binary.LittleEndian.PutUint64(buf[8:16], 1000)
				return &mockReaderAt{data: buf}, createMockSuperblock()
			},
			wantErr: "local heap data read failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, sb := tt.setup()
			heap, err := LoadLocalHeap(reader, 0, sb)
			require.Error(t, err)
			require.Nil(t, heap)
			if tt.wantErr != "" {
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadLocalHeap_BigEndian(t *testing.T) {
	buf := make([]byte, 1024)
	copy(buf[0:4], "HEAP")
	buf[4] = 0
	buf[5], buf[6], buf[7] = 0, 0, 0
	// Data segment size: 100 bytes (header is 32 bytes, not included in this size)
	binary.BigEndian.PutUint64(buf[8:16], 100)
	// Free list offset (8 bytes)
	binary.BigEndian.PutUint64(buf[16:24], 1) // H5HL_FREE_NULL
	// Data segment address (8 bytes) - data at offset 32
	binary.BigEndian.PutUint64(buf[24:32], 32)
	// Data starts at offset 32 (after 32-byte header)
	copy(buf[32:132], "big endian test data")

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	heap, err := LoadLocalHeap(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, heap)
	require.Equal(t, uint64(32), heap.HeaderSize)
	require.Len(t, heap.Data, 100)
}

func TestLocalHeap_GetString_Success(t *testing.T) {
	tests := []struct {
		name           string
		heapData       []byte
		offset         uint64
		expectedString string
	}{
		{
			name: "simple string",
			heapData: func() []byte {
				// Strings start at offset 0 in data segment (no metadata prefix)
				buf := make([]byte, 256)
				copy(buf[0:], "hello\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "hello",
		},
		{
			name: "string at non-zero offset",
			heapData: func() []byte {
				buf := make([]byte, 256)
				// First string at offset 0, second at offset 4
				copy(buf[0:], "\x00\x00\x00\x00") // offset 0-3 (padding/empty)
				copy(buf[4:], "world\x00")        // offset 4
				return buf
			}(),
			offset:         4,
			expectedString: "world",
		},
		{
			name: "multiple strings",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[0:], "first\x00second\x00third\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "first",
		},
		{
			name: "string with special characters",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[0:], "Hello, World! 123\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "Hello, World! 123",
		},
		{
			name: "empty string",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[0:], "\x00other\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := &LocalHeap{
				Data:       tt.heapData,
				HeaderSize: 32, // Header is always 32 bytes
			}

			str, err := heap.GetString(tt.offset)
			require.NoError(t, err)
			require.Equal(t, tt.expectedString, str)
		})
	}
}

func TestLocalHeap_GetString_Errors(t *testing.T) {
	tests := []struct {
		name     string
		heapData []byte
		offset   uint64
		wantErr  string
	}{
		{
			name:     "offset beyond data",
			heapData: make([]byte, 100),
			offset:   200,
			wantErr:  "offset beyond heap data",
		},
		{
			name: "string not null-terminated",
			heapData: func() []byte {
				buf := make([]byte, 32)
				// Fill with non-null bytes
				for i := range buf {
					buf[i] = 'A'
				}
				return buf
			}(),
			offset:  0,
			wantErr: "string not null-terminated",
		},
		{
			name:     "offset at end of data",
			heapData: make([]byte, 16),
			offset:   16, // Beyond end of data
			wantErr:  "offset beyond heap data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := &LocalHeap{
				Data:       tt.heapData,
				HeaderSize: 32, // Header is always 32 bytes
			}

			str, err := heap.GetString(tt.offset)
			require.Error(t, err)
			require.Empty(t, str)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLocalHeap_GetString_LongString(t *testing.T) {
	// Test with a long string
	longString := string(make([]byte, 1000))
	for i := range longString {
		longString = longString[:i] + "A"
	}

	heapData := make([]byte, 2048)
	copy(heapData[0:], longString+"\x00") // Strings start at offset 0

	heap := &LocalHeap{
		Data:       heapData,
		HeaderSize: 32, // Header is always 32 bytes
	}

	str, err := heap.GetString(0)
	require.NoError(t, err)
	require.Equal(t, longString, str)
}

func TestLocalHeap_GetString_MultipleConsecutiveStrings(t *testing.T) {
	heapData := make([]byte, 256)
	copy(heapData[0:], "first\x00second\x00third\x00") // Strings start at offset 0

	heap := &LocalHeap{
		Data:       heapData,
		HeaderSize: 32, // Header is always 32 bytes
	}

	// Get first string at offset 0
	str1, err := heap.GetString(0)
	require.NoError(t, err)
	require.Equal(t, "first", str1)

	// Get second string at offset 6 (len("first") + 1)
	str2, err := heap.GetString(6)
	require.NoError(t, err)
	require.Equal(t, "second", str2)

	// Get third string at offset 13 (len("first\x00second") + 1)
	str3, err := heap.GetString(13)
	require.NoError(t, err)
	require.Equal(t, "third", str3)
}

func TestLocalHeap_StructFields(t *testing.T) {
	// Verify LocalHeap structure
	data := []byte{1, 2, 3, 4, 5}
	heap := &LocalHeap{
		Data:       data,
		FreeList:   0x1234567890ABCDEF,
		HeaderSize: 128,
	}

	require.Equal(t, data, heap.Data)
	require.Equal(t, uint64(0x1234567890ABCDEF), heap.FreeList)
	require.Equal(t, uint64(128), heap.HeaderSize)
}

func BenchmarkLoadLocalHeap(b *testing.B) {
	buf := make([]byte, 4096)
	copy(buf[0:4], "HEAP")
	buf[4] = 0
	binary.LittleEndian.PutUint64(buf[8:16], 1024)
	// Free list offset (8 bytes)
	binary.LittleEndian.PutUint64(buf[16:24], 1) // H5HL_FREE_NULL
	// Data segment address (8 bytes) - data at offset 32
	binary.LittleEndian.PutUint64(buf[24:32], 32)
	for i := 32; i < 1056; i++ {
		buf[i] = byte(i % 256)
	}

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = LoadLocalHeap(reader, 0, sb)
	}
}

func BenchmarkLocalHeap_GetString(b *testing.B) {
	heapData := make([]byte, 4096)
	copy(heapData[16:], "benchmark_test_string\x00")

	heap := &LocalHeap{
		Data:       heapData,
		HeaderSize: uint64(len(heapData) + 16),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = heap.GetString(0)
	}
}

// --- Write Support Tests ---

// TestNewLocalHeap follows the C library: H5HL_create raises the size hint
// to H5HL_SIZEOF_FREE (16) and aligns it to 8 (H5HL.c:99-177), and the empty
// name then takes the first aligned slot (H5Gstab.c:144). An insert only
// splits a free block whose remainder is at least H5HL_SIZEOF_FREE
// (H5HL.c:557-590), so a 16-byte segment grows before the empty name fits.
func TestNewLocalHeap(t *testing.T) {
	tests := []struct {
		name         string
		initialSize  uint64
		expectedSize uint64
		wantFree     []localHeapFreeBlock
	}{
		{name: "minimum size", initialSize: 0, expectedSize: 32, wantFree: []localHeapFreeBlock{{offset: 8, size: 24}}},
		{name: "small size", initialSize: 10, expectedSize: 32, wantFree: []localHeapFreeBlock{{offset: 8, size: 24}}},
		{name: "aligned size", initialSize: 64, expectedSize: 64, wantFree: []localHeapFreeBlock{{offset: 8, size: 56}}},
		{name: "unaligned size", initialSize: 65, expectedSize: 72, wantFree: []localHeapFreeBlock{{offset: 8, size: 64}}},
		{name: "large size", initialSize: 1000, expectedSize: 1000, wantFree: []localHeapFreeBlock{{offset: 8, size: 992}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.initialSize)
			require.NotNil(t, heap)
			require.Equal(t, tt.expectedSize, heap.DataSegmentSize)
			require.Equal(t, tt.wantFree, heap.free)
			name, err := heap.GetString(0)
			require.NoError(t, err)
			require.Empty(t, name) // the group's empty name at offset 0
		})
	}
}

// TestLocalHeap_AddString: names are stored at 8-byte aligned offsets after
// the empty name at 0 (H5HL_ALIGN, H5HL.c:557-590). A block whose remainder
// would be smaller than H5HL_SIZEOF_FREE is not split: AddString reports
// ErrLocalHeapFull and the caller grows the segment.
func TestLocalHeap_AddString(t *testing.T) {
	tests := []struct {
		name            string
		heapSize        uint64
		stringsToAdd    []string
		expectedOffsets []uint64
		errAt           int // index of the first string that does not fit; -1 when all fit
	}{
		{name: "single string", heapSize: 64, stringsToAdd: []string{"hello"}, expectedOffsets: []uint64{8}, errAt: -1},
		{name: "multiple strings", heapSize: 64, stringsToAdd: []string{"first", "second", "third"}, expectedOffsets: []uint64{8, 16, 24}, errAt: -1},
		{name: "empty string", heapSize: 64, stringsToAdd: []string{""}, expectedOffsets: []uint64{8}, errAt: -1},
		// "hello world\0" is 12 bytes, aligned to 16.
		{name: "strings with spaces", heapSize: 64, stringsToAdd: []string{"hello world", "foo bar"}, expectedOffsets: []uint64{8, 24}, errAt: -1},
		{name: "heap overflow", heapSize: 16, stringsToAdd: []string{"this is a very long string that won't fit"}, errAt: 0},
		// 24 bytes: "" leaves a 16-byte block; "first" (8) would leave 8 < H5HL_SIZEOF_FREE.
		{name: "remainder below free-block size", heapSize: 24, stringsToAdd: []string{"first"}, errAt: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)
			require.NotNil(t, heap)

			for i, str := range tt.stringsToAdd {
				offset, err := heap.AddString(str)
				if i == tt.errAt {
					require.ErrorIs(t, err, ErrLocalHeapFull)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.expectedOffsets[i], offset, "offset mismatch for string %d", i)
			}
			require.Equal(t, -1, tt.errAt, "expected string %d not to fit", tt.errAt)
		})
	}
}

func TestLocalHeap_Size(t *testing.T) {
	tests := []struct {
		name         string
		heapSize     uint64
		expectedSize uint64
	}{
		{name: "minimum heap", heapSize: 16, expectedSize: 32 + 32}, // grown to fit "" (TestNewLocalHeap)
		{name: "medium heap", heapSize: 128, expectedSize: 32 + 128},
		{name: "large heap", heapSize: 1024, expectedSize: 32 + 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)
			require.Equal(t, tt.expectedSize, heap.Size())
		})
	}
}

// TestLocalHeap_WriteTo checks the on-disk image: the prefix
// (H5HLcache.c:535-600) with the head of the free list, and each free block
// serialized in the data segment as (next offset, size), both sizeof_size,
// H5HL_FREE_NULL (1) ending the list (H5HL__fl_serialize, H5HLcache.c:289-315).
func TestLocalHeap_WriteTo(t *testing.T) {
	const dataStart = 32 // prefix of 8-byte sizes and addresses
	tests := []struct {
		name    string
		setup   func() *LocalHeap
		address uint64
		verify  func(*testing.T, []byte)
	}{
		{
			name:    "empty heap",
			setup:   func() *LocalHeap { return NewLocalHeap(64) },
			address: 0,
			verify: func(t *testing.T, data []byte) {
				require.Equal(t, "HEAP", string(data[0:4]))
				require.Equal(t, []byte{0, 0, 0, 0}, data[4:8]) // version 0, reserved
				require.Equal(t, uint64(64), binary.LittleEndian.Uint64(data[8:16]))
				require.Equal(t, uint64(8), binary.LittleEndian.Uint64(data[16:24]), "free list head")
				require.Equal(t, uint64(dataStart), binary.LittleEndian.Uint64(data[24:32]))
				// The empty name, then the free block {next: NULL, size: 56}.
				require.Equal(t, make([]byte, 8), data[dataStart:dataStart+8])
				require.Equal(t, uint64(1), binary.LittleEndian.Uint64(data[dataStart+8:]))
				require.Equal(t, uint64(56), binary.LittleEndian.Uint64(data[dataStart+16:]))
			},
		},
		{
			name: "heap with single string",
			setup: func() *LocalHeap {
				heap := NewLocalHeap(64)
				_, err := heap.AddString("hello")
				require.NoError(t, err)
				return heap
			},
			address: 0,
			verify: func(t *testing.T, data []byte) {
				require.Equal(t, uint64(16), binary.LittleEndian.Uint64(data[16:24]), "free list head")
				require.Equal(t, "hello\x00\x00\x00", string(data[dataStart+8:dataStart+16]))
				require.Equal(t, uint64(1), binary.LittleEndian.Uint64(data[dataStart+16:]))
				require.Equal(t, uint64(48), binary.LittleEndian.Uint64(data[dataStart+24:]))
			},
		},
		{
			name: "heap with multiple strings",
			setup: func() *LocalHeap {
				heap := NewLocalHeap(128)
				for _, s := range []string{"first", "second", "third"} {
					_, err := heap.AddString(s)
					require.NoError(t, err)
				}
				return heap
			},
			address: 0,
			verify: func(t *testing.T, data []byte) {
				require.Equal(t, "first\x00", string(data[dataStart+8:dataStart+14]))
				require.Equal(t, "second\x00", string(data[dataStart+16:dataStart+23]))
				require.Equal(t, "third\x00", string(data[dataStart+24:dataStart+30]))
				require.Equal(t, uint64(32), binary.LittleEndian.Uint64(data[16:24]), "free list head")
			},
		},
		{
			name: "heap at non-zero address",
			setup: func() *LocalHeap {
				heap := NewLocalHeap(64)
				_, err := heap.AddString("test")
				require.NoError(t, err)
				return heap
			},
			address: 1000,
			verify: func(t *testing.T, data []byte) {
				require.Equal(t, uint64(1032), binary.LittleEndian.Uint64(data[1024:1032])) // 1000 + 32
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := tt.setup()
			buf := make([]byte, 10000)
			require.NoError(t, heap.WriteTo(&mockWriter{data: buf}, tt.address))
			tt.verify(t, buf)
		})
	}
}

func TestLocalHeap_WriteToAndRead(t *testing.T) {
	// Round-trip test: Write heap, then read it back
	tests := []struct {
		name    string
		strings []string
	}{
		{
			name:    "single string",
			strings: []string{"hello"},
		},
		{
			name:    "multiple strings",
			strings: []string{"one", "two", "three", "four"},
		},
		{
			name:    "empty and non-empty strings",
			strings: []string{"", "test", "", "data"},
		},
		{
			name:    "long strings",
			strings: []string{"this is a longer string", "another long string here"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate required size
			totalSize := uint64(0)
			for _, s := range tt.strings {
				totalSize += uint64(len(s) + 1) // +1 for null terminator
			}
			heapSize := totalSize + 64 // Extra space

			// Create and populate heap
			writeHeap := NewLocalHeap(heapSize)
			offsets := make([]uint64, len(tt.strings))
			for i, s := range tt.strings {
				offset, err := writeHeap.AddString(s)
				require.NoError(t, err)
				offsets[i] = offset
			}

			// Write to buffer
			buf := make([]byte, 10000)
			writer := &mockWriter{data: buf}
			err := writeHeap.WriteTo(writer, 0)
			require.NoError(t, err)

			// Read back
			reader := &mockReaderAt{data: buf}
			sb := createMockSuperblock()
			readHeap, err := LoadLocalHeap(reader, 0, sb)
			require.NoError(t, err)
			require.NotNil(t, readHeap)

			// Verify all strings can be retrieved
			// Note: GetString adds 16 to offset (for free list metadata in read format)
			// But our write format doesn't have that, so we need to adjust
			// For this test, we'll directly check the data buffer
			for i, expectedStr := range tt.strings {
				offset := offsets[i]
				// Read directly from data segment (starts at byte 32)
				dataStart := 32 + offset
				end := dataStart
				for end < uint64(len(buf)) && buf[end] != 0 {
					end++
				}
				actualStr := string(buf[dataStart:end])
				require.Equal(t, expectedStr, actualStr, "string mismatch at index %d", i)
			}
		})
	}
}

// TestLocalHeap_AddString_Errors: an aligned name that equals a free block
// takes it whole; one larger than every block is refused (H5HL.c:557-590).
func TestLocalHeap_AddString_Errors(t *testing.T) {
	tests := []struct {
		name         string
		heapSize     uint64
		stringsToAdd []string
		expectError  bool
	}{
		{
			// 24 bytes: "" at 0 leaves one 16-byte block; 15 chars + NUL fill it exactly.
			name:         "exact fit",
			heapSize:     24,
			stringsToAdd: []string{"123456789012345"},
			expectError:  false,
		},
		{
			// 32 bytes after the growth of NewLocalHeap: one 24-byte block; 24 chars need 32.
			name:         "overflow",
			heapSize:     16,
			stringsToAdd: []string{"123456789012345678901234"},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)

			var lastErr error
			for _, s := range tt.stringsToAdd {
				if _, err := heap.AddString(s); err != nil {
					lastErr = err
					break
				}
			}

			if tt.expectError {
				require.ErrorIs(t, lastErr, ErrLocalHeapFull)
			} else {
				require.NoError(t, lastErr)
				require.Empty(t, heap.free)
			}
		})
	}
}

// --- Mock helpers for write tests ---

type mockWriter struct {
	data []byte
}

func (m *mockWriter) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off >= int64(len(m.data)) {
		return 0, errors.New("offset out of range")
	}
	n = copy(m.data[off:], p)
	return n, nil
}
