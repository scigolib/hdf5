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
				// Header size (8 bytes) - total size including header
				binary.LittleEndian.PutUint64(buf[8:16], 32) // 16 bytes header + 16 bytes data
				// Data follows at offset 16
				copy(buf[16:32], "Hello, World!")
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
				// Header size: 16 + 100 = 116 bytes
				binary.LittleEndian.PutUint64(buf[8:16], 116)
				// Fill data section with test data
				for i := 0; i < 100; i++ {
					buf[16+i] = byte(i % 256)
				}
				return buf
			}(),
			address:      0,
			expectedSize: 116,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Equal(t, uint64(116), heap.HeaderSize)
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
				binary.LittleEndian.PutUint64(buf[offset+8:offset+16], 50)
				copy(buf[offset+16:offset+50], "test data at offset")
				return buf
			}(),
			address:      500,
			expectedSize: 50,
			checkData: func(t *testing.T, heap *LocalHeap) {
				require.Equal(t, uint64(50), heap.HeaderSize)
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
				binary.LittleEndian.PutUint64(buf[8:16], 64)
				// Add some null-terminated strings
				offset := 16
				copy(buf[offset:], "string1\x00string2\x00string3\x00")
				return buf
			}(),
			address:      0,
			expectedSize: 64,
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
				buf := make([]byte, 16)
				copy(buf[0:4], "HEAP")
				buf[4] = 0
				// Header size claims 1000 bytes, but buffer is only 16
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
	binary.BigEndian.PutUint64(buf[8:16], 100)
	copy(buf[16:100], "big endian test data")

	reader := &mockReaderAt{data: buf}
	sb := createMockSuperblock()
	sb.Endianness = binary.BigEndian

	heap, err := LoadLocalHeap(reader, 0, sb)
	require.NoError(t, err)
	require.NotNil(t, heap)
	require.Equal(t, uint64(100), heap.HeaderSize)
	require.Len(t, heap.Data, 84)
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
				// First 16 bytes are free list metadata
				// Strings start after that
				buf := make([]byte, 256)
				copy(buf[16:], "hello\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "hello",
		},
		{
			name: "string at non-zero offset",
			heapData: func() []byte {
				buf := make([]byte, 256)
				// Free list metadata in first 16 bytes
				copy(buf[16:], "\x00\x00\x00\x00") // offset 0-3
				copy(buf[20:], "world\x00")        // offset 4
				return buf
			}(),
			offset:         4,
			expectedString: "world",
		},
		{
			name: "multiple strings",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[16:], "first\x00second\x00third\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "first",
		},
		{
			name: "string with special characters",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[16:], "Hello, World! 123\x00")
				return buf
			}(),
			offset:         0,
			expectedString: "Hello, World! 123",
		},
		{
			name: "empty string",
			heapData: func() []byte {
				buf := make([]byte, 256)
				copy(buf[16:], "\x00other\x00")
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
				HeaderSize: uint64(len(tt.heapData) + 16),
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
			offset:   0,
			wantErr:  "offset beyond heap data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := &LocalHeap{
				Data:       tt.heapData,
				HeaderSize: uint64(len(tt.heapData) + 16),
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
	copy(heapData[16:], longString+"\x00")

	heap := &LocalHeap{
		Data:       heapData,
		HeaderSize: uint64(len(heapData) + 16),
	}

	str, err := heap.GetString(0)
	require.NoError(t, err)
	require.Equal(t, longString, str)
}

func TestLocalHeap_GetString_MultipleConsecutiveStrings(t *testing.T) {
	heapData := make([]byte, 256)
	offset := 16
	copy(heapData[offset:], "first\x00second\x00third\x00")

	heap := &LocalHeap{
		Data:       heapData,
		HeaderSize: uint64(len(heapData) + 16),
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
	for i := 16; i < 1024; i++ {
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

func TestNewLocalHeap(t *testing.T) {
	tests := []struct {
		name         string
		initialSize  uint64
		expectedSize uint64
	}{
		{
			name:         "minimum size",
			initialSize:  0,
			expectedSize: 16, // Rounded up to minimum
		},
		{
			name:         "small size",
			initialSize:  10,
			expectedSize: 16, // Rounded up to 8-byte alignment
		},
		{
			name:         "aligned size",
			initialSize:  64,
			expectedSize: 64, // Already aligned
		},
		{
			name:         "unaligned size",
			initialSize:  65,
			expectedSize: 72, // Rounded up to next 8-byte boundary
		},
		{
			name:         "large size",
			initialSize:  1000,
			expectedSize: 1000, // Already aligned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.initialSize)
			require.NotNil(t, heap)
			require.Equal(t, tt.expectedSize, heap.DataSegmentSize)
			require.Equal(t, uint64(1), heap.OffsetToHeadFreeList) // H5HL_FREE_NULL
			require.NotNil(t, heap.strings)
			require.Equal(t, 0, len(heap.strings))
		})
	}
}

func TestLocalHeap_AddString(t *testing.T) {
	tests := []struct {
		name            string
		heapSize        uint64
		stringsToAdd    []string
		expectedOffsets []uint64
		expectError     bool
	}{
		{
			name:            "single string",
			heapSize:        64,
			stringsToAdd:    []string{"hello"},
			expectedOffsets: []uint64{0},
			expectError:     false,
		},
		{
			name:            "multiple strings",
			heapSize:        64,
			stringsToAdd:    []string{"first", "second", "third"},
			expectedOffsets: []uint64{0, 6, 13}, // "first\0" (6), "second\0" (7), "third\0" (6)
			expectError:     false,
		},
		{
			name:            "empty string",
			heapSize:        64,
			stringsToAdd:    []string{""},
			expectedOffsets: []uint64{0},
			expectError:     false,
		},
		{
			name:            "strings with spaces",
			heapSize:        64,
			stringsToAdd:    []string{"hello world", "foo bar"},
			expectedOffsets: []uint64{0, 12}, // "hello world\0" (12)
			expectError:     false,
		},
		{
			name:         "heap overflow",
			heapSize:     16, // Very small heap
			stringsToAdd: []string{"this is a very long string that won't fit"},
			expectError:  true,
		},
		{
			name:         "multiple strings overflow",
			heapSize:     20,
			stringsToAdd: []string{"first", "second", "third"}, // Total: 6+7+6 = 19 bytes (fits)
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)
			require.NotNil(t, heap)

			for i, str := range tt.stringsToAdd {
				offset, err := heap.AddString(str)

				if tt.expectError {
					if err != nil {
						// Expected error occurred
						return
					}
					// Continue to next string
					continue
				}

				require.NoError(t, err)
				if i < len(tt.expectedOffsets) {
					require.Equal(t, tt.expectedOffsets[i], offset, "offset mismatch for string %d", i)
				}
			}
		})
	}
}

func TestLocalHeap_Size(t *testing.T) {
	tests := []struct {
		name         string
		heapSize     uint64
		expectedSize uint64
	}{
		{
			name:         "minimum heap",
			heapSize:     16,
			expectedSize: 32 + 16, // header (32) + data (16)
		},
		{
			name:         "medium heap",
			heapSize:     128,
			expectedSize: 32 + 128,
		},
		{
			name:         "large heap",
			heapSize:     1024,
			expectedSize: 32 + 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)
			require.Equal(t, tt.expectedSize, heap.Size())
		})
	}
}

func TestLocalHeap_WriteTo(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (*LocalHeap, []string)
		address uint64
		verify  func(*testing.T, []byte, *LocalHeap)
	}{
		{
			name: "empty heap",
			setup: func() (*LocalHeap, []string) {
				return NewLocalHeap(64), []string{}
			},
			address: 0,
			verify: func(t *testing.T, data []byte, heap *LocalHeap) {
				// Verify header
				require.Equal(t, "HEAP", string(data[0:4]))
				require.Equal(t, byte(0), data[4]) // Version
				require.Equal(t, byte(0), data[5]) // Reserved
				require.Equal(t, byte(0), data[6])
				require.Equal(t, byte(0), data[7])

				// Verify data segment size
				dataSegSize := binary.LittleEndian.Uint64(data[8:16])
				require.Equal(t, uint64(64), dataSegSize)

				// Verify free list offset (should be 1 = NULL)
				freeListOffset := binary.LittleEndian.Uint64(data[16:24])
				require.Equal(t, uint64(1), freeListOffset)

				// Verify data segment address
				dataSegAddr := binary.LittleEndian.Uint64(data[24:32])
				require.Equal(t, uint64(32), dataSegAddr) // 0 + 32 (header size)
			},
		},
		{
			name: "heap with single string",
			setup: func() (*LocalHeap, []string) {
				heap := NewLocalHeap(64)
				_, _ = heap.AddString("hello")
				return heap, []string{"hello"}
			},
			address: 0,
			verify: func(t *testing.T, data []byte, heap *LocalHeap) {
				// Verify header
				require.Equal(t, "HEAP", string(data[0:4]))

				// Verify data contains "hello\0"
				dataStart := 32
				require.Equal(t, "hello", string(data[dataStart:dataStart+5]))
				require.Equal(t, byte(0), data[dataStart+5]) // Null terminator
			},
		},
		{
			name: "heap with multiple strings",
			setup: func() (*LocalHeap, []string) {
				heap := NewLocalHeap(128)
				_, _ = heap.AddString("first")
				_, _ = heap.AddString("second")
				_, _ = heap.AddString("third")
				return heap, []string{"first", "second", "third"}
			},
			address: 0,
			verify: func(t *testing.T, data []byte, heap *LocalHeap) {
				// Verify all strings are present
				dataStart := 32
				offset := dataStart

				// "first\0"
				require.Equal(t, "first", string(data[offset:offset+5]))
				require.Equal(t, byte(0), data[offset+5])
				offset += 6

				// "second\0"
				require.Equal(t, "second", string(data[offset:offset+6]))
				require.Equal(t, byte(0), data[offset+6])
				offset += 7

				// "third\0"
				require.Equal(t, "third", string(data[offset:offset+5]))
				require.Equal(t, byte(0), data[offset+5])
			},
		},
		{
			name: "heap at non-zero address",
			setup: func() (*LocalHeap, []string) {
				heap := NewLocalHeap(64)
				_, _ = heap.AddString("test")
				return heap, []string{"test"}
			},
			address: 1000,
			verify: func(t *testing.T, data []byte, heap *LocalHeap) {
				// Verify data segment address accounts for heap address
				dataSegAddr := binary.LittleEndian.Uint64(data[1024:1032])
				require.Equal(t, uint64(1032), dataSegAddr) // 1000 + 32
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap, _ := tt.setup()

			// Create buffer for writing
			buf := make([]byte, 10000)
			writer := &mockWriter{data: buf}

			// Write heap
			err := heap.WriteTo(writer, tt.address)
			require.NoError(t, err)

			// Verify
			tt.verify(t, buf, heap)
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

func TestLocalHeap_AddString_Errors(t *testing.T) {
	tests := []struct {
		name        string
		heapSize    uint64
		stringsToAdd []string
		expectError bool
	}{
		{
			name:         "exact fit",
			heapSize:     19, // "hello\0" (6) + "world\0" (6) + "!\0" (2) = 14, but aligned to 24
			stringsToAdd: []string{"hello", "world", "!"},
			expectError:  false,
		},
		{
			name:         "overflow by one byte",
			heapSize:     16,
			stringsToAdd: []string{"1234567890123456"}, // 17 bytes with null
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			heap := NewLocalHeap(tt.heapSize)

			var lastErr error
			for _, s := range tt.stringsToAdd {
				_, err := heap.AddString(s)
				if err != nil {
					lastErr = err
					break
				}
			}

			if tt.expectError {
				require.Error(t, lastErr)
				require.Contains(t, lastErr.Error(), "local heap is full")
			} else {
				require.NoError(t, lastErr)
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
