package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestReadChunkedData_RejectsRankMismatch covers the guard added after the
// branch review: the per-chunk copy indexes the chunk grid by the dataspace
// rank, so a grid shorter than the rank panicked with a slice bounds error on
// metadata that Open accepts. The check now runs when the descriptor is built
// (the grid must have rank+1 dimensions, H5Dchunk.c:909-913), before any
// reader can see the layout.
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

	_, err := newDatasetDescriptor(bytes.NewReader(nil), sb, &DatatypeMessage{Size: 8}, dataspace, layout)
	require.Error(t, err)
	require.True(t, errors.Is(err, utils.ErrCorrupt), "want ErrCorrupt, got %v", err)
	require.Contains(t, err.Error(), "chunked layout has 1 dimensions, the dataspace rank 2 needs 3")
}

// TestReadChunkedData_IndexTypesReadTheirOwnStructure covers the index types a
// version 4 layout can name. Each structure sits at the layout's address and
// is read as itself, never as a v1 B-tree (which would return garbage chunks):
// here nothing valid is there, so every type is utils.ErrCorrupt, as is a type
// past the format's last.
func TestReadChunkedData_IndexTypesReadTheirOwnStructure(t *testing.T) {
	dataspace := &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{8}}
	sb := &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

	for _, idx := range []uint8{
		ChunkIndexSingle,
		ChunkIndexNone,
		ChunkIndexFArray,
		ChunkIndexEArray,
		ChunkIndexBT2,
		ChunkIndexBT2 + 1,
	} {
		layout := &DataLayoutMessage{
			Version:        4,
			Class:          LayoutChunked,
			ChunkSize:      []uint64{4, 8}, // Chunk dimension + element size.
			ChunkIndexType: idx,
		}

		r := bytes.NewReader(nil)
		desc, err := newDatasetDescriptor(r, sb, &DatatypeMessage{Size: 8}, dataspace, layout)
		require.NoError(t, err, "index type %d", idx)

		_, err = readChunkedData(r, sb, desc, ResultFloat64, utils.DefaultReadLimit)
		require.ErrorIs(t, err, utils.ErrCorrupt, "index type %d", idx)
	}
}
