package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteAddress tests variable-sized address writing.
func TestWriteAddress(t *testing.T) {
	tests := []struct {
		name       string
		addr       uint64
		size       int
		endianness binary.ByteOrder
		want       []byte
	}{
		{
			name:       "1-byte address",
			addr:       0x42,
			size:       1,
			endianness: binary.LittleEndian,
			want:       []byte{0x42},
		},
		{
			name:       "2-byte address little-endian",
			addr:       0x1234,
			size:       2,
			endianness: binary.LittleEndian,
			want:       []byte{0x34, 0x12},
		},
		{
			name:       "2-byte address big-endian",
			addr:       0x1234,
			size:       2,
			endianness: binary.BigEndian,
			want:       []byte{0x12, 0x34},
		},
		{
			name:       "4-byte address little-endian",
			addr:       0x12345678,
			size:       4,
			endianness: binary.LittleEndian,
			want:       []byte{0x78, 0x56, 0x34, 0x12},
		},
		{
			name:       "4-byte address big-endian",
			addr:       0x12345678,
			size:       4,
			endianness: binary.BigEndian,
			want:       []byte{0x12, 0x34, 0x56, 0x78},
		},
		{
			name:       "8-byte address little-endian",
			addr:       0x123456789ABCDEF0,
			size:       8,
			endianness: binary.LittleEndian,
			want:       []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12},
		},
		{
			name:       "8-byte address big-endian",
			addr:       0x123456789ABCDEF0,
			size:       8,
			endianness: binary.BigEndian,
			want:       []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0},
		},
		{
			name:       "truncated address 2-byte",
			addr:       0x123456,
			size:       2,
			endianness: binary.LittleEndian,
			want:       []byte{0x56, 0x34}, // Lower 2 bytes
		},
		{
			name:       "zero address",
			addr:       0,
			size:       8,
			endianness: binary.LittleEndian,
			want:       []byte{0, 0, 0, 0, 0, 0, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			writeAddress(buf, tt.addr, tt.size, tt.endianness)
			require.Equal(t, tt.want, buf)
		})
	}
}

// TestComputeOffsetSize tests offset size computation.
func TestComputeOffsetSize(t *testing.T) {
	tests := []struct {
		name      string
		maxOffset uint64
		want      uint8
	}{
		{
			name:      "zero offset",
			maxOffset: 0,
			want:      1,
		},
		{
			name:      "fits in 1 byte",
			maxOffset: 0xFF,
			want:      1,
		},
		{
			name:      "requires 2 bytes (0x100 = 9 bits)",
			maxOffset: 0x100,
			want:      2,
		},
		{
			name:      "fits in 2 bytes max",
			maxOffset: 0xFFFF,
			want:      2,
		},
		{
			name:      "requires 3 bytes (0x10000 = 17 bits)",
			maxOffset: 0x10000,
			want:      3,
		},
		{
			name:      "fits in 4 bytes max",
			maxOffset: 0xFFFFFFFF,
			want:      4,
		},
		{
			name:      "requires 5 bytes (0x100000000 = 33 bits)",
			maxOffset: 0x100000000,
			want:      5,
		},
		{
			name:      "large value needing 8 bytes",
			maxOffset: 0xFFFFFFFFFFFFFFFF,
			want:      8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOffsetSize(tt.maxOffset)
			require.Equal(t, tt.want, got)
		})
	}
}
