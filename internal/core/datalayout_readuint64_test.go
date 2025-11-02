package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadUint64 tests variable-sized unsigned integer reading.
func TestReadUint64(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		size       int
		endianness binary.ByteOrder
		want       uint64
	}{
		{
			name:       "1-byte value",
			data:       []byte{0x42},
			size:       1,
			endianness: binary.LittleEndian,
			want:       0x42,
		},
		{
			name:       "2-byte value little-endian",
			data:       []byte{0x34, 0x12},
			size:       2,
			endianness: binary.LittleEndian,
			want:       0x1234,
		},
		{
			name:       "2-byte value big-endian",
			data:       []byte{0x12, 0x34},
			size:       2,
			endianness: binary.BigEndian,
			want:       0x1234,
		},
		{
			name:       "4-byte value little-endian",
			data:       []byte{0x78, 0x56, 0x34, 0x12},
			size:       4,
			endianness: binary.LittleEndian,
			want:       0x12345678,
		},
		{
			name:       "4-byte value big-endian",
			data:       []byte{0x12, 0x34, 0x56, 0x78},
			size:       4,
			endianness: binary.BigEndian,
			want:       0x12345678,
		},
		{
			name: "8-byte value little-endian",
			data: func() []byte {
				buf := make([]byte, 8)
				binary.LittleEndian.PutUint64(buf, 0x123456789ABCDEF0)
				return buf
			}(),
			size:       8,
			endianness: binary.LittleEndian,
			want:       0x123456789ABCDEF0,
		},
		{
			name: "8-byte value big-endian",
			data: func() []byte {
				buf := make([]byte, 8)
				binary.BigEndian.PutUint64(buf, 0x123456789ABCDEF0)
				return buf
			}(),
			size:       8,
			endianness: binary.BigEndian,
			want:       0x123456789ABCDEF0,
		},
		{
			name:       "3-byte value (non-standard) little-endian",
			data:       []byte{0x78, 0x56, 0x34},
			size:       3,
			endianness: binary.LittleEndian,
			want:       0x345678, // Padded to 8 bytes
		},
		{
			name:       "5-byte value (non-standard) little-endian",
			data:       []byte{0xEF, 0xCD, 0xAB, 0x89, 0x67},
			size:       5,
			endianness: binary.LittleEndian,
			want:       0x6789ABCDEF, // Padded to 8 bytes
		},
		{
			name:       "size larger than data",
			data:       []byte{0x12, 0x34},
			size:       8,
			endianness: binary.LittleEndian,
			want:       0x3412, // Uses available data
		},
		{
			name:       "zero size",
			data:       []byte{0x12, 0x34},
			size:       0,
			endianness: binary.LittleEndian,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readUint64(tt.data, tt.size, tt.endianness)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestParseLayoutV4 tests version 4 layout parsing (delegates to v3).
func TestParseLayoutV4(t *testing.T) {
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	// Test contiguous layout v4.
	data := make([]byte, 2+8+8) // version + class + address + size
	data[0] = 4                 // version 4
	data[1] = byte(LayoutContiguous)
	binary.LittleEndian.PutUint64(data[2:10], 0x1000)  // data address
	binary.LittleEndian.PutUint64(data[10:18], 0x4000) // data size

	msg, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, uint8(4), msg.Version)
	require.Equal(t, LayoutContiguous, msg.Class)
	require.Equal(t, uint64(0x1000), msg.DataAddress)
	require.Equal(t, uint64(0x4000), msg.DataSize)
}
