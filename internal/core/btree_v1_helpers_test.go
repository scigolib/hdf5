package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompareCoords tests chunk coordinate comparison.
func TestCompareCoords(t *testing.T) {
	tests := []struct {
		name string
		a, b []uint64
		want int
	}{
		{
			name: "equal 1D coords",
			a:    []uint64{5},
			b:    []uint64{5},
			want: 0,
		},
		{
			name: "a < b in first dimension",
			a:    []uint64{3},
			b:    []uint64{5},
			want: -1,
		},
		{
			name: "a > b in first dimension",
			a:    []uint64{7},
			b:    []uint64{5},
			want: 1,
		},
		{
			name: "equal 2D coords",
			a:    []uint64{3, 7},
			b:    []uint64{3, 7},
			want: 0,
		},
		{
			name: "a < b in second dimension",
			a:    []uint64{3, 5},
			b:    []uint64{3, 7},
			want: -1,
		},
		{
			name: "a > b in second dimension",
			a:    []uint64{3, 9},
			b:    []uint64{3, 7},
			want: 1,
		},
		{
			name: "a < b in first dimension (3D)",
			a:    []uint64{2, 5, 8},
			b:    []uint64{3, 5, 8},
			want: -1,
		},
		{
			name: "a > b in third dimension (3D)",
			a:    []uint64{3, 5, 9},
			b:    []uint64{3, 5, 8},
			want: 1,
		},
		{
			name: "different lengths - shorter equal prefix",
			a:    []uint64{3},
			b:    []uint64{3, 5},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareCoords(tt.a, tt.b)
			require.Equal(t, tt.want, got)
		})
	}
}

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

// TestBTreeV1NodeString tests String() method.
func TestBTreeV1NodeString(t *testing.T) {
	tests := []struct {
		name string
		node *BTreeV1Node
		want string
	}{
		{
			name: "leaf node",
			node: &BTreeV1Node{
				NodeType:    1,
				NodeLevel:   0,
				EntriesUsed: 5,
			},
			want: "B-tree v1 node: type=1 level=0 entries=5",
		},
		{
			name: "internal node",
			node: &BTreeV1Node{
				NodeType:    1,
				NodeLevel:   2,
				EntriesUsed: 10,
			},
			want: "B-tree v1 node: type=1 level=2 entries=10",
		},
		{
			name: "empty node",
			node: &BTreeV1Node{
				NodeType:    1,
				NodeLevel:   0,
				EntriesUsed: 0,
			},
			want: "B-tree v1 node: type=1 level=0 entries=0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.node.String()
			require.Equal(t, tt.want, got)
		})
	}
}
