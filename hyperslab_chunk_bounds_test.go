package hdf5

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The ADR-003 tests of generateChunkCoordinates (unrepresentable span, count
// above MaxChunkCoordinates, regular range) were removed with the function:
// the chunked hyperslab reader no longer materializes the grid of overlapping
// chunk coordinates, it filters the stored B-tree entries by chunkRange
// (TASK-062 review finding M1), so there is no grid-sized allocation left to
// bound.

// TestChunkRange_RankMismatch covers the review finding that a chunk grid
// shorter than the selection rank was indexed past its end.
func TestChunkRange_RankMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{1, 1},
		Stride: []uint64{1, 1},
		Block:  []uint64{1, 1},
	}

	_, _, err := chunkRange(sel, []uint64{4}, []uint64{10, 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rank mismatch")
}

// TestChunkRange_RegularRange pins the overlapping range of a normal 2-D
// selection: rows 3-12 and columns 0-4 of a 4×4-chunked grid.
func TestChunkRange_RegularRange(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{3, 0},
		Count:  []uint64{10, 5},
		Stride: []uint64{1, 1},
		Block:  []uint64{1, 1},
	}

	first, last, err := chunkRange(sel, []uint64{4, 4, 8}, []uint64{16, 16})
	require.NoError(t, err)
	require.Equal(t, []uint64{0, 0}, first)
	require.Equal(t, []uint64{3, 1}, last)
	require.True(t, chunkInRange([]uint64{2, 1}, first, last))
	require.False(t, chunkInRange([]uint64{2, 2}, first, last))
}
