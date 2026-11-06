package structures

import (
	"bytes"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestWalkGroupBTree_BudgetBoundsRepeatedSNODs is the regression test of the
// v0.14.3 deep review: the group B-tree walk was bounded by a visited set
// only. Version 1 B-tree nodes carry no checksum, and the children of a
// level-0 node were read with no dedup, so one small node listing the same
// symbol table node 2K times made the walk read 2K symbol table nodes from a
// file that holds one. With a large K from the superblock that is millions of
// reads and entries from a file of a few kilobytes. Every node read is now
// charged to a budget limited to the file's size.
func TestWalkGroupBTree_BudgetBoundsRepeatedSNODs(t *testing.T) {
	sb := testSB(8, 8)
	sb.SymInternalK = 1024 // 2K = 2048 children per node
	l := NewGroupBTreeLayout(sb)

	const snodAddr = 0x100
	treeAddr := uint64(snodAddr + 0x200)
	children := make([]uint64, l.TwoK())
	for i := range children {
		children[i] = snodAddr // the same symbol table node, every time
	}
	keys := make([]uint64, l.TwoK()+1)

	file := make([]byte, treeAddr+l.NodeSize())
	copy(file[snodAddr:], encodeSNOD(sampleEntries()[:1], 8, 8))
	copy(file[treeAddr:], encodeGroupNode(0, keys, children, 8, 8))

	calls := 0
	err := WalkGroupBTree(bytes.NewReader(file), sb, treeAddr, func(uint64) error {
		calls++
		return nil
	})
	require.ErrorIs(t, err, utils.ErrCorrupt)
	// The file holds len(file) bytes; at SNODSize per read the budget allows
	// at most that many symbol table node reads, far fewer than 2K.
	maxCalls := len(file) / int(l.SNODSize())
	require.LessOrEqual(t, calls, maxCalls, "walk read more symbol table nodes than the file can hold")
	require.Less(t, calls, l.TwoK())
}
