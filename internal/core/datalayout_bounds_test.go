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

	t.Run("dimensionality above the format maximum", func(t *testing.T) {
		d := make([]byte, 3+8+4)
		d[0] = 3
		d[1] = byte(LayoutChunked)
		d[2] = maxChunkLayoutDims + 1
		binary.LittleEndian.PutUint64(d[3:11], 0x1000)

		_, err := ParseDataLayoutMessage(d, sb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds the maximum")
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

// TestParseDataLayout_V4ChunkedMalformed covers damaged version 4 chunked
// bodies. The encoded dimension width drives every offset that follows it, so a
// corrupt value must be rejected here: reading past it would size the grid and
// the address from the wrong bytes.
func TestParseDataLayout_V4ChunkedMalformed(t *testing.T) {
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "unknown layout flag",
			data:    []byte{4, byte(LayoutChunked), 0x04, 1, 1},
			wantErr: "unknown flags",
		},
		{
			name:    "dimensionality above the format maximum",
			data:    []byte{4, byte(LayoutChunked), 0, maxChunkLayoutDims + 1, 1},
			wantErr: "exceeds the maximum",
		},
		{
			name:    "zero encoded dimension width",
			data:    []byte{4, byte(LayoutChunked), 0, 1, 0},
			wantErr: "invalid dimension width",
		},
		{
			name:    "encoded dimension width above eight bytes",
			data:    []byte{4, byte(LayoutChunked), 0, 1, 9, 1},
			wantErr: "invalid dimension width",
		},
		{
			name:    "grid truncated inside the dimensions",
			data:    []byte{4, byte(LayoutChunked), 0, 2, 1, 5},
			wantErr: "dimensions truncated",
		},
		{
			name:    "missing chunk index type",
			data:    []byte{4, byte(LayoutChunked), 0, 1, 1, 2},
			wantErr: "missing chunk index type",
		},
		{
			name:    "index creation parameters truncated",
			data:    []byte{4, byte(LayoutChunked), 0, 1, 1, 2, ChunkIndexEArray},
			wantErr: "index data truncated",
		},
		{
			name:    "unknown chunk index type",
			data:    []byte{4, byte(LayoutChunked), 0, 1, 1, 2, 6},
			wantErr: "unsupported chunk index type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDataLayoutMessage(tt.data, sb)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
