package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// TestCollectChunks_RejectsDuplicateKeys is the regression test for
// review-068 finding 3: a 2-chunk grid whose B-tree lists chunk 0 twice made
// the readers count two present chunks, so chunk 1, never written, read as
// zeros instead of the fill value, without an error. Chunk keys are strictly
// increasing (H5D__btree_cmp3, H5Dbtree.c:324-360); a duplicate is corrupt.
//
// Red on 40c6d35: CollectChunks returns both entries.
func TestCollectChunks_RejectsDuplicateKeys(t *testing.T) {
	// 1-D dataset of 8 uint8 elements in chunks of 4: grid of 2 chunks.
	buf := new(bytes.Buffer)
	buf.WriteString("TREE")
	buf.WriteByte(1) // Raw data chunks.
	buf.WriteByte(0) // Leaf.
	require.NoError(t, binary.Write(buf, binary.LittleEndian, uint16(2)))
	require.NoError(t, binary.Write(buf, binary.LittleEndian, undefinedAddress))
	require.NoError(t, binary.Write(buf, binary.LittleEndian, undefinedAddress))
	key := func(offset uint64) {
		require.NoError(t, binary.Write(buf, binary.LittleEndian, uint32(4))) // nbytes
		require.NoError(t, binary.Write(buf, binary.LittleEndian, uint32(0))) // filter mask
		require.NoError(t, binary.Write(buf, binary.LittleEndian, offset))    // element offset
		require.NoError(t, binary.Write(buf, binary.LittleEndian, uint64(0))) // element-size dimension
	}
	key(0)
	require.NoError(t, binary.Write(buf, binary.LittleEndian, uint64(0x200)))
	key(0) // Chunk 0 again.
	require.NoError(t, binary.Write(buf, binary.LittleEndian, uint64(0x200)))
	key(8)
	file := append(buf.Bytes(), make([]byte, 0x300)...)

	d := &DatasetDescriptor{
		datatype:  &DatatypeMessage{Class: DatatypeFixed, Size: 1},
		dataspace: &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{8}},
		layout: &DataLayoutMessage{Class: LayoutChunked, ChunkIndexType: ChunkIndexBTREE,
			ChunkSize: []uint64{4, 1}, DataAddress: 0},
		nelmts: 8, elemSize: 1, dataBytes: 8, chunkBytes: 4,
	}

	_, err := d.CollectChunks(bytes.NewReader(file), sharedTestSB(), utils.DefaultReadLimit)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.ErrorContains(t, err, "more than once")
}
