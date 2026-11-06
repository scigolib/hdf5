package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// Unit tests of the bounds added for the TASK-062 review (H1, H2, M3, L4).
// The public tests (chunk_btree_corrupt_test.go, dataset_amplification_test.go)
// carry the red proofs on crafted files.

// TestResultSize_PlatformAddressSpace (M3): the converted result counts
// toward the platform bound, not only the raw bytes, and the default limit
// fits the address space. 300M int8 elements are 300 MB raw and 2.4 GB as
// []float64, more than a 32-bit target can address even with no read limit.
func TestResultSize_PlatformAddressSpace(t *testing.T) {
	int8Type := &DatatypeMessage{Class: DatatypeFixed, Size: 1}
	desc, err := newDatasetDescriptor(bytes.NewReader(nil), descTestSB(), int8Type,
		simpleSpace(300_000_000), chunkedLayout(1024, 1))
	require.NoError(t, err)

	size, err := desc.ResultSize(desc.NumElements(), ResultFloat64, math.MaxUint64)
	if strconv.IntSize == 32 {
		require.ErrorIs(t, err, utils.ErrLimit)
		require.Contains(t, err.Error(), "more than this platform can address")
	} else {
		require.NoError(t, err)
		require.Equal(t, 300_000_000, size)
	}

	require.LessOrEqual(t, utils.DefaultReadLimit, uint64(math.MaxInt/4),
		"the default read limit must leave room in the address space")
}

// TestCompoundResultSize_CountsMembers (H2): a compound element costs its map
// and an entry per member, recursively, so the read limit sees the maps and
// not only the raw bytes (the former estimate was 8 bytes per element).
func TestCompoundResultSize_CountsMembers(t *testing.T) {
	f64 := &DatatypeMessage{Class: DatatypeFloat, Size: 8, ClassBitField: 0x20}
	pair := &CompoundType{Size: 16, Members: []CompoundMember{
		{Name: "a", Offset: 0, Type: f64},
		{Name: "b", Offset: 8, Type: f64},
	}}

	two := CompoundResultSize(pair)
	require.Equal(t, compoundMapBase+2*(compoundPerEntry+compoundBoxed), two)
	require.Greater(t, two, 16*uint64(maxFreeConversion),
		"a compound result is limited even for contiguous data, whose free conversion is 8×")

	str := &DatatypeMessage{Class: DatatypeString, Size: 100}
	withString := &CompoundType{Size: 116, Members: append(pair.Members, CompoundMember{Name: "s", Offset: 16, Type: str})}
	require.Equal(t, two+compoundPerEntry+compoundStrHead+100, CompoundResultSize(withString))
}

// TestParseDataspaceMessage_RankLimit (L4): C refuses a rank above
// H5S_MAX_RANK (32) while decoding the message (H5Osdspace.c:137-138).
func TestParseDataspaceMessage_RankLimit(t *testing.T) {
	message := func(rank int) []byte {
		data := []byte{1, byte(rank), 0, 0, 0, 0, 0, 0}
		for i := 0; i < rank; i++ {
			data = binary.LittleEndian.AppendUint64(data, 1)
		}
		return data
	}

	ds, err := ParseDataspaceMessage(message(32))
	require.NoError(t, err)
	require.Len(t, ds.Dimensions, 32)

	_, err = ParseDataspaceMessage(message(33))
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "dimensionality 33 is too large")
}

// selfReferencingNode returns a level-1 chunk B-tree node at address 0 with
// one entry whose child is the node itself (rank-1 keys: two coordinates).
func selfReferencingNode() []byte {
	node := []byte("TREE")
	node = append(node, btreeTypeChunk, 1)           // Type, level 1.
	node = binary.LittleEndian.AppendUint16(node, 1) // Entries used.
	node = binary.LittleEndian.AppendUint64(node, math.MaxUint64)
	node = binary.LittleEndian.AppendUint64(node, math.MaxUint64)
	key := make([]byte, 24) // nbytes, filter mask, two coordinates: all 0.
	node = append(node, key...)
	node = binary.LittleEndian.AppendUint64(node, 0) // Child: this node.
	node = append(node, key...)

	return node
}

// TestBTreeV1_SelfReferenceRejected (H1): a child that is its own parent
// recursed until the Go stack overflowed, a fatal error. Both walks now check
// the child's level as C does (H5Bcache.c:173-176). Red only in WSL under
// prlimit (the public TestChunkBTree_ChildIsItsParent).
func TestBTreeV1_SelfReferenceRejected(t *testing.T) {
	r := bytes.NewReader(selfReferencingNode())
	chunkDims := []uint64{4, 8}

	node, err := ParseBTreeV1Node(r, 0, 8, 2, chunkDims)
	require.NoError(t, err)

	_, err = node.CollectAllChunks(r, 8, chunkDims)
	require.ErrorIs(t, err, utils.ErrCorrupt)

	_, err = node.FindChunk(r, []uint64{0, 0}, 8, chunkDims)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "has level 1, its parent 0x0 has level 1")
}
