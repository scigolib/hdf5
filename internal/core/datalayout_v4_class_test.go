package core

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDataLayoutV4Chunked_Class pins the layout class of a version 4
// chunked message. parseChunkedLayoutV4 never set msg.Class, so every version 4
// chunked dataset was reported as compact (class 0) with no data: Read failed
// with "data truncated", ChunkIterator refused it as not chunked, and the
// descriptor would now reject it as a compact size mismatch. C decodes the class
// byte for every class before branching (H5Olayout.c:222).
//
// The message is /DS_chunked_layout_4 of testdata/hdf5_official/
// bounds_latest_latest.h5, for which h5dump -H reports
// CHUNKED ( 50, 50 ) on a ( 100, 200 ) H5T_IEEE_F32LE dataspace: version 4,
// class 2, flags 0x01, 3 dimensions of 1 byte (50, 50 and the element size 4),
// fixed-array index (3) with one parameter byte, index address 0x1DF.
func TestParseDataLayoutV4Chunked_Class(t *testing.T) {
	data, err := hex.DecodeString("0402010301323204030adf01000000000000")
	require.NoError(t, err)

	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	layout, err := ParseDataLayoutMessage(data, sb)
	require.NoError(t, err)
	require.Equal(t, LayoutChunked, layout.Class, "a version 4 chunked layout must report the chunked class")
	require.True(t, layout.IsChunked())
	require.Equal(t, []uint64{50, 50, 4}, layout.ChunkSize)
	require.Equal(t, ChunkIndexFArray, layout.ChunkIndexType)
	require.Equal(t, uint64(0x1DF), layout.DataAddress)
}

// TestParseDataLayout_UndefinedAddressNarrowOffsets: with 4-byte file offsets
// the undefined address is stored as ff ff ff ff, and H5F_addr_decode_len maps
// an all-0xff address of any width to HADDR_UNDEF (H5Fint.c:3009-3049). The
// layout parser kept 0xffffffff, a real-looking location, so an unallocated
// contiguous dataset was reported as storage past the end of the file instead
// of storage that does not exist yet (found by FuzzDatasetDescriptor).
func TestParseDataLayout_UndefinedAddressNarrowOffsets(t *testing.T) {
	sb := &Superblock{Version: 0, OffsetSize: 4, LengthSize: 4, Endianness: binary.LittleEndian}

	// Version 3, contiguous, address ff ff ff ff, size 16.
	contig := []byte{3, 1, 0xff, 0xff, 0xff, 0xff, 16, 0, 0, 0}
	layout, err := ParseDataLayoutMessage(contig, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), layout.DataAddress)

	// Version 3, chunked, 2 dimensions, B-tree address ff ff ff ff, dims 4 and 8.
	chunked := []byte{3, 2, 2, 0xff, 0xff, 0xff, 0xff, 4, 0, 0, 0, 8, 0, 0, 0}
	layout, err = ParseDataLayoutMessage(chunked, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0xFFFFFFFFFFFFFFFF), layout.DataAddress)

	// A defined address is decoded unchanged.
	contig[2] = 0xfe
	layout, err = ParseDataLayoutMessage(contig, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0xFFFFFFFE), layout.DataAddress)
}
