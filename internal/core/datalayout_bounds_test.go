package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDataLayout_RejectsInvalidChunkGrid covers the layout-side half of
// the review finding: a chunk dimensionality or dimension of zero reached the
// readers, where it divided by zero or shrank a slice. Both are rejected at the
// only point where the values enter the library.
func TestParseDataLayout_RejectsInvalidChunkGrid(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	t.Run("zero dimensionality", func(t *testing.T) {
		d := make([]byte, 3+8)
		d[0] = 3
		d[1] = byte(LayoutChunked)
		d[2] = 0
		binary.LittleEndian.PutUint64(d[3:11], 0x1000)

		_, err := ParseDataLayoutMessage(d, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "zero dimensionality")
	})

	t.Run("zero chunk dimension", func(t *testing.T) {
		d := make([]byte, 3+8+8)
		d[0] = 3
		d[1] = byte(LayoutChunked)
		d[2] = 2
		binary.LittleEndian.PutUint64(d[3:11], 0x1000)
		binary.LittleEndian.PutUint32(d[11:15], 0)
		binary.LittleEndian.PutUint32(d[15:19], 100)

		_, err := ParseDataLayoutMessage(d, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "dimension 0 is zero")
	})

	t.Run("valid chunk grid", func(t *testing.T) {
		d := make([]byte, 3+8+8)
		d[0] = 3
		d[1] = byte(LayoutChunked)
		d[2] = 2
		binary.LittleEndian.PutUint64(d[3:11], 0x1000)
		binary.LittleEndian.PutUint32(d[11:15], 4)
		binary.LittleEndian.PutUint32(d[15:19], 8)

		msg, err := ParseDataLayoutMessage(d, sb)
		require.NoError(t, err)
		require.Equal(t, []uint64{4, 8}, msg.ChunkSize)
	})
}

// TestParseDataLayout_V4Chunked covers the version 4 chunked body, which the
// parser treated as version 3 until the review: flags, a per-dimension encoded
// width, the chunk grid, an index type and only then the address. Real files in
// the reference suite (h5fc_ext*) use it with an extensible-array index.
func TestParseDataLayout_V4Chunked(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	build := func(flags, encBytes byte, dims []byte, idxType byte, params []byte) []byte {
		d := make([]byte, 0, 5+len(dims)+1+len(params)+8)
		d = append(d, 4, byte(LayoutChunked), flags, byte(len(dims)/int(encBytes)), encBytes)
		d = append(d, dims...)
		d = append(d, idxType)
		d = append(d, params...)
		addr := make([]byte, 8)
		binary.LittleEndian.PutUint64(addr, 0x4DB)
		d = append(d, addr...)

		return d
	}

	t.Run("extensible array index", func(t *testing.T) {
		params := []byte{16, 4, 4, 4, 6} // max_nelmts_bits, idx_blk_elmts, sup_blk_min_data_ptrs, data_blk_min_elmts, page bits
		d := build(0, 1, []byte{2, 3, 4}, ChunkIndexEArray, params)

		msg, err := ParseDataLayoutMessage(d, sb)
		require.NoError(t, err)
		require.Equal(t, []uint64{2, 3, 4}, msg.ChunkSize)
		require.Equal(t, ChunkIndexEArray, msg.ChunkIndexType)
		require.Equal(t, uint64(0x4DB), msg.DataAddress)
	})

	t.Run("version 4 cannot carry a v1 b-tree index", func(t *testing.T) {
		d := build(0, 1, []byte{2, 3}, ChunkIndexBTREE, nil)

		_, err := ParseDataLayoutMessage(d, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "v1 B-tree index")
	})

	t.Run("zero dimension is rejected", func(t *testing.T) {
		d := build(0, 1, []byte{2, 0}, ChunkIndexNone, nil)

		_, err := ParseDataLayoutMessage(d, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "is zero")
	})
}
