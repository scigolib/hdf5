package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// narrowSuperblock is the superblock of a file with 4-byte addresses, as the
// C library writes with H5Pset_sizes(fcpl, 4, 4).
func narrowSuperblock() *Superblock {
	return &Superblock{OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}
}

// TestDecodeAddress_NarrowUndefined: an address made of 0xff bytes only is
// HADDR_UNDEF at any width (H5F_addr_decode_len, H5Fint.c:3046-3050), so a
// 4-byte or 2-byte all-ones address decodes to the 64-bit undefined value.
func TestDecodeAddress_NarrowUndefined(t *testing.T) {
	for _, size := range []int{2, 4, 8} {
		require.Equal(t, undefinedAddress, DecodeAddress(bytes.Repeat([]byte{0xff}, size), size, binary.LittleEndian),
			"%d-byte all-ones address", size)
		require.Equal(t, undefinedAddress, readAddress(bytes.Repeat([]byte{0xff}, size), size),
			"%d-byte all-ones address, B-tree v1 decoder", size)
	}
	require.Equal(t, uint64(0xfffffffe), DecodeAddress([]byte{0xfe, 0xff, 0xff, 0xff}, 4, binary.LittleEndian))
}

// TestParseAttributeInfoMessage_NarrowUndefined: with 4-byte addresses a C
// object whose attributes are compact keeps an attribute info message with
// the heap address 0xffffffff (H5Oattribute.c:225-236). That is no dense
// storage: the reader used to take it for a heap at 0xffffffff.
func TestParseAttributeInfoMessage_NarrowUndefined(t *testing.T) {
	sb := narrowSuperblock()
	// Version 0, tracked, max creation index 3; heap and name index undefined.
	data := append([]byte{0, 0x01, 3, 0}, bytes.Repeat([]byte{0xff}, 8)...)
	info, err := ParseAttributeInfoMessage(data, sb)
	require.NoError(t, err)
	require.Equal(t, undefinedAddress, info.FractalHeapAddr)
	require.Equal(t, undefinedAddress, info.BTreeNameIndexAddr)
	require.False(t, info.HasDenseStorage(sb.OffsetSize), "compact attributes")

	defined := []byte{0, 0, 0x00, 0x10, 0, 0, 0x00, 0x20, 0, 0}
	info, err = ParseAttributeInfoMessage(defined, sb)
	require.NoError(t, err)
	require.True(t, info.HasDenseStorage(sb.OffsetSize), "heap at 0x1000")
}

// TestParseLinkInfoMessage_NarrowUndefined: a group with compact links and
// 4-byte addresses stores 0xffffffff for the heap and the name index
// (H5Olinfo.c); those are not a dense link storage.
func TestParseLinkInfoMessage_NarrowUndefined(t *testing.T) {
	data := append([]byte{0, 0}, bytes.Repeat([]byte{0xff}, 8)...)
	lim, err := ParseLinkInfoMessage(data, narrowSuperblock())
	require.NoError(t, err)
	require.False(t, lim.HasFractalHeap(), "no link heap")
	require.False(t, lim.HasNameBTree(), "no link name index")
}
