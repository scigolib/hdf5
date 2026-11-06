package hdf5

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// TestReadAddrFromBuf_NarrowUndefined: the writer reads symbol table B-tree
// children and symbol table messages with readAddrFromBuf and compares them
// with the 64-bit undefined address; an all-ones address is HADDR_UNDEF at
// any width (H5F_addr_decode_len, H5Fint.c:3046-3050).
func TestReadAddrFromBuf_NarrowUndefined(t *testing.T) {
	require.Equal(t, undefinedAddress, readAddrFromBuf([]byte{0xff, 0xff, 0xff, 0xff}, 4, binary.LittleEndian))
	require.Equal(t, undefinedAddress, readAddrFromBuf([]byte{0xff, 0xff}, 2, binary.LittleEndian))
	require.Equal(t, uint64(0x1000), readAddrFromBuf([]byte{0, 0x10, 0, 0}, 4, binary.LittleEndian))
}

// TestOpen_FourByteOffsets reads a file written by the C library with
// sizeof_addr = 4 (superblock v0). Its root group B-tree node stores the
// undefined sibling addresses as 4 bytes of 0xff; they must decode to the
// 64-bit undefined value, and the file must still open, walk and read.
func TestOpen_FourByteOffsets(t *testing.T) {
	const name = "testdata/reference/tsizeslheap.h5"
	f, err := Open(name)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	sb := f.Superblock()
	require.Equal(t, uint8(4), sb.OffsetSize)
	require.NotEqual(t, undefinedAddress, sb.RootBTreeAddr)
	require.NotEqual(t, undefinedAddress, sb.RootHeapAddr)

	var ds *Dataset
	f.Walk(func(path string, obj Object) {
		if d, ok := obj.(*Dataset); ok && path == "/Dataset1" {
			ds = d
		}
	})
	require.NotNil(t, ds, "/Dataset1 not found")
	values, err := ds.Read()
	require.NoError(t, err)
	require.Equal(t, []float64{0}, values)

	// Group B-tree node: "TREE", type, level, entries used (2), then the
	// left and right sibling addresses (H5B__cache_deserialize, H5Bcache.c).
	raw, err := os.ReadFile(name)
	require.NoError(t, err)
	node := raw[sb.RootBTreeAddr:]
	require.Equal(t, "TREE", string(node[:4]))
	left, right := node[8:12], node[12:16]
	require.Equal(t, []byte{0xff, 0xff, 0xff, 0xff}, left)
	require.Equal(t, undefinedAddress, core.DecodeAddress(left, 4, sb.Endianness))
	require.Equal(t, undefinedAddress, core.DecodeAddress(right, 4, sb.Endianness))
}
