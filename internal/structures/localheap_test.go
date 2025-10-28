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
