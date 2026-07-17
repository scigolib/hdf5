package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadAddress tests variable-sized address reading.
func TestReadAddress(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		size int
		want uint64
	}{
		{
			name: "1-byte address",
			data: []byte{0x42},
			size: 1,
			want: 0x42,
		},
		{
			name: "2-byte address little-endian",
			data: []byte{0x34, 0x12},
			size: 2,
			want: 0x1234,
		},
		{
			name: "4-byte address little-endian",
			data: []byte{0x78, 0x56, 0x34, 0x12},
			size: 4,
			want: 0x12345678,
		},
		{
			name: "8-byte address little-endian",
			data: func() []byte {
				buf := make([]byte, 8)
				binary.LittleEndian.PutUint64(buf, 0x123456789ABCDEF0)
				return buf
			}(),
			size: 8,
			want: 0x123456789ABCDEF0,
		},
		{
			name: "3-byte address (non-standard)",
			data: []byte{0x78, 0x56, 0x34},
			size: 3,
			want: 0x345678, // Padded to 8 bytes
		},
		{
			name: "5-byte address (non-standard)",
			data: []byte{0xEF, 0xCD, 0xAB, 0x89, 0x67},
			size: 5,
			want: 0x6789ABCDEF, // Padded to 8 bytes
		},
		{
			name: "size larger than data",
			data: []byte{0x12, 0x34},
			size: 8,
			want: 0x3412, // Uses available data, pads rest
		},
		{
			name: "zero-byte address (edge case)",
			data: []byte{},
			size: 0,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readAddress(tt.data, tt.size)
			require.Equal(t, tt.want, got)
		})
	}
}
