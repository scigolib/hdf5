package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadChunkedData_RejectsRankMismatch covers the guard added after the
// branch review: the per-chunk copy indexes the chunk grid by the dataspace
// rank, so a grid shorter than the rank panicked with a slice bounds error on
// metadata that Open accepts.
func TestReadChunkedData_RejectsRankMismatch(t *testing.T) {
	layout := &DataLayoutMessage{
		Version:   3,
		Class:     LayoutChunked,
		ChunkSize: []uint64{4},
	}

	dataspace := &DataspaceMessage{
		Type:       DataspaceSimple,
		Dimensions: []uint64{8, 8},
	}

	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	_, err := readChunkedData(bytes.NewReader(nil), layout, dataspace,
		&DatatypeMessage{Size: 8}, sb, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunk rank 1 is smaller than dataspace rank 2")
}

// TestReadChunkedData_RejectsUnsupportedChunkIndex covers the index types a
// version 4 layout can name. Only the v1 B-tree is traversed here; any other
// structure sits at the same address and must not be parsed as a B-tree, which
// would return garbage chunks instead of an error.
func TestReadChunkedData_RejectsUnsupportedChunkIndex(t *testing.T) {
	dataspace := &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{8}}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	for _, idx := range []uint8{
		ChunkIndexSingle,
		ChunkIndexNone,
		ChunkIndexFArray,
		ChunkIndexEArray,
		ChunkIndexBT2,
	} {
		layout := &DataLayoutMessage{
			Version:        4,
			Class:          LayoutChunked,
			ChunkSize:      []uint64{4},
			ChunkIndexType: idx,
		}

		_, err := readChunkedData(bytes.NewReader(nil), layout, dataspace,
			&DatatypeMessage{Size: 8}, sb, nil)
		require.Error(t, err, "index type %d", idx)
		require.Contains(t, err.Error(), "unsupported chunk index type")
	}
}
