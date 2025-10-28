package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockReaderAt is a mock implementation of ReaderAt for testing.
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

func TestReadUint64_LittleEndian(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		offset   int64
		expected uint64
		order    binary.ByteOrder
	}{
		{
			name:     "zero value",
			data:     []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0,
			expected: 0,
			order:    binary.LittleEndian,
		},
		{
			name:     "max value",
			data:     []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			offset:   0,
			expected: 0xFFFFFFFFFFFFFFFF,
			order:    binary.LittleEndian,
		},
		{
			name:     "small value little endian",
			data:     []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0,
			expected: 1,
			order:    binary.LittleEndian,
		},
		{
			name:     "large value little endian",
			data:     []byte{0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0,
			expected: 0x1000,
			order:    binary.LittleEndian,
		},
		{
			name:     "with offset",
			data:     []byte{0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   2,
			expected: 1,
			order:    binary.LittleEndian,
		},
		{
			name:     "typical HDF5 address",
			data:     []byte{0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0,
			expected: 0x60,
			order:    binary.LittleEndian,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			val, err := ReadUint64(reader, tt.offset, tt.order)
			require.NoError(t, err)
			require.Equal(t, tt.expected, val)
		})
	}
}

func TestReadUint64_BigEndian(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		offset   int64
		expected uint64
	}{
		{
			name:     "zero value",
			data:     []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0,
			expected: 0,
		},
		{
			name:     "max value",
			data:     []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			offset:   0,
			expected: 0xFFFFFFFFFFFFFFFF,
		},
		{
			name:     "small value big endian",
			data:     []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			offset:   0,
			expected: 1,
		},
		{
			name:     "large value big endian",
			data:     []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00},
			offset:   0,
			expected: 0x1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &mockReaderAt{data: tt.data}
			val, err := ReadUint64(reader, tt.offset, binary.BigEndian)
			require.NoError(t, err)
			require.Equal(t, tt.expected, val)
		})
	}
}

func TestReadUint64_Errors(t *testing.T) {
	tests := []struct {
		name   string
		reader ReaderAt
		offset int64
		order  binary.ByteOrder
	}{
		{
			name:   "read error",
			reader: &mockReaderAt{data: []byte{}, err: errors.New("read error")},
			offset: 0,
			order:  binary.LittleEndian,
		},
		{
			name:   "offset beyond data",
			reader: &mockReaderAt{data: []byte{0x01, 0x02}},
			offset: 100,
			order:  binary.LittleEndian,
		},
		{
			name:   "not enough data",
			reader: &mockReaderAt{data: []byte{0x01, 0x02, 0x03}},
			offset: 0,
			order:  binary.LittleEndian,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadUint64(tt.reader, tt.offset, tt.order)
			require.Error(t, err)
		})
	}
}

func TestReadUint64_WithBytesReader(t *testing.T) {
	// Test with actual bytes.Reader (implements io.ReaderAt)
	data := []byte{
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
	}

	reader := bytes.NewReader(data)
	val, err := ReadUint64(reader, 0, binary.LittleEndian)
	require.NoError(t, err)

	expected := binary.LittleEndian.Uint64(data)
	require.Equal(t, expected, val)
}

func TestReadUint64_BufferPoolIntegration(t *testing.T) {
	// This test verifies that ReadUint64 properly uses the buffer pool
	// by checking that it doesn't panic and returns correct results
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader := &mockReaderAt{data: data}

	// Read multiple times to ensure buffer pool works correctly
	for offset := int64(0); offset <= int64(len(data)-8); offset += 8 {
		val, err := ReadUint64(reader, offset, binary.LittleEndian)
		require.NoError(t, err)

		// Verify the value is correct
		expected := binary.LittleEndian.Uint64(data[offset : offset+8])
		require.Equal(t, expected, val, "offset: %d", offset)
	}
}

func TestReaderAtInterface(t *testing.T) {
	// Verify that common types implement ReaderAt
	t.Run("bytes.Reader", func(_ *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
		var _ ReaderAt = bytes.NewReader(data)
	})

	t.Run("mockReaderAt", func(_ *testing.T) {
		var _ ReaderAt = &mockReaderAt{}
	})
}

func BenchmarkReadUint64(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i)
	}
	reader := &mockReaderAt{data: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		offset := int64((i * 8) % (len(data) - 8))
		_, _ = ReadUint64(reader, offset, binary.LittleEndian)
	}
}

func BenchmarkReadUint64_BigEndian(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i)
	}
	reader := &mockReaderAt{data: data}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		offset := int64((i * 8) % (len(data) - 8))
		_, _ = ReadUint64(reader, offset, binary.BigEndian)
	}
}
