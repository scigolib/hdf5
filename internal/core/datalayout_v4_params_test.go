package core

import (
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// chunkedLayoutTestAddr is the index address every buildChunkedLayout body stores.
const chunkedLayoutTestAddr = 0x100

// buildChunkedLayout encodes a version 4 or 5 chunked layout body:
// version, class, flags, ndims, enc bytes, dims, index type, params, and
// chunkedLayoutTestAddr as the address.
func buildChunkedLayout(version, flags byte, dims []byte, idxType byte, params []byte) []byte {
	d := make([]byte, 0, 6+len(dims)+len(params)+8)
	d = append(d, version, byte(LayoutChunked), flags, byte(len(dims)), 1)
	d = append(d, dims...)
	d = append(d, idxType)
	d = append(d, params...)

	return binary.LittleEndian.AppendUint64(d, chunkedLayoutTestAddr)
}

// TestParseDataLayout_V4IndexParams covers the chunk index creation parameters
// of a version 4 chunked layout, decoded and checked as H5O__layout_decode
// does (H5Olayout.c:383-516). They size the index structures the reader walks
// later, so a zero page size or an impossible split percentage must stop here.
func TestParseDataLayout_V4IndexParams(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	t.Run("fixed array page bits", func(t *testing.T) {
		msg, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexFArray, []byte{10}), sb)
		require.NoError(t, err)
		require.Equal(t, uint8(10), msg.Index.FArrayPageBits)
		require.Equal(t, uint64(0x100), msg.DataAddress)

		_, err = ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexFArray, []byte{0}), sb)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})

	t.Run("extensible array params", func(t *testing.T) {
		params := []byte{32, 4, 16, 4, 10}
		msg, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexEArray, params), sb)
		require.NoError(t, err)
		require.Equal(t, EArrayParams{
			MaxNelmtsBits: 32, IdxBlkElmts: 4, SupBlkMinDataPtrs: 16, DataBlkMinElmts: 4, MaxDblkPageNelmtsBits: 10,
		}, msg.Index.EArray)

		for i := range params {
			bad := append([]byte(nil), params...)
			bad[i] = 0
			_, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexEArray, bad), sb)
			require.ErrorIs(t, err, utils.ErrCorrupt, "param %d", i)
		}
	})

	t.Run("v2 B-tree params", func(t *testing.T) {
		params := binary.LittleEndian.AppendUint32(nil, 512)
		msg, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexBT2, append(params, 100, 40)), sb)
		require.NoError(t, err)
		require.Equal(t, BT2Params{NodeSize: 512, SplitPercent: 100, MergePercent: 40}, msg.Index.BT2)

		for _, sm := range [][2]byte{{0, 40}, {101, 40}, {100, 0}, {100, 101}} {
			_, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexBT2, append(params, sm[0], sm[1])), sb)
			require.ErrorIs(t, err, utils.ErrCorrupt, "split %d merge %d", sm[0], sm[1])
		}
	})

	t.Run("single chunk with filter", func(t *testing.T) {
		params := binary.LittleEndian.AppendUint64(nil, 77)
		params = binary.LittleEndian.AppendUint32(params, 0x2)
		msg, err := ParseDataLayoutMessage(buildChunkedLayout(4, chunkLayoutFlagSingleWithFilter, []byte{4, 8}, ChunkIndexSingle, params), sb)
		require.NoError(t, err)
		require.Equal(t, uint64(77), msg.Index.SingleNbytes)
		require.Equal(t, uint32(0x2), msg.Index.SingleFilterMask)
		require.Equal(t, uint8(chunkLayoutFlagSingleWithFilter), msg.ChunkFlags)
		require.Equal(t, uint64(0x100), msg.DataAddress)
	})

	t.Run("truncated params", func(t *testing.T) {
		d := buildChunkedLayout(4, 0, []byte{4, 8}, ChunkIndexEArray, []byte{32, 4, 16, 4, 10})
		_, err := ParseDataLayoutMessage(d[:len(d)-9], sb)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})

	t.Run("unknown index type", func(t *testing.T) {
		_, err := ParseDataLayoutMessage(buildChunkedLayout(4, 0, []byte{4, 8}, 6, nil), sb)
		require.ErrorIs(t, err, utils.ErrCorrupt)
	})
}

// TestParseDataLayout_V5 covers HDF5 2.0's layout version 5: its body is the
// version 4 body (H5Oprivate.h:442-455, H5Olayout.c:305 decodes both), and
// only the width of a filtered chunk's size in the index changes.
func TestParseDataLayout_V5(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	msg, err := ParseDataLayoutMessage(buildChunkedLayout(5, 0, []byte{4, 8}, ChunkIndexFArray, []byte{10}), sb)
	require.NoError(t, err)
	require.Equal(t, uint8(5), msg.Version)
	require.Equal(t, LayoutChunked, msg.Class)
	require.Equal(t, []uint64{4, 8}, msg.ChunkSize)
	require.Equal(t, ChunkIndexFArray, msg.ChunkIndexType)

	// A version 5 contiguous body is the version 3 one.
	contig := []byte{5, byte(LayoutContiguous)}
	contig = binary.LittleEndian.AppendUint64(contig, 0x800)
	contig = binary.LittleEndian.AppendUint64(contig, 64)
	msg, err = ParseDataLayoutMessage(contig, sb)
	require.NoError(t, err)
	require.Equal(t, uint64(0x800), msg.DataAddress)
	require.Equal(t, uint64(64), msg.DataSize)
}
