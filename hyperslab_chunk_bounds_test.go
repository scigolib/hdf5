package hdf5

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// The ADR-003 tests of generateChunkCoordinates (unrepresentable span, count
// above MaxChunkCoordinates, regular range) were removed with the function:
// the chunked hyperslab reader never materializes the grid of overlapping
// chunk coordinates. It lists, per dimension, the chunks the selection
// touches (selectedChunks) and visits only those (TASK-062 review finding M1,
// TASK-058).

// TestSelectedChunks_RankMismatch covers the review finding that a chunk grid
// shorter than the selection rank was indexed past its end.
func TestSelectedChunks_RankMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{1, 1},
		Stride: []uint64{1, 1},
		Block:  []uint64{1, 1},
	}

	_, err := selectedChunks(sel, []uint64{4}, []uint64{10, 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rank mismatch")
}

// TestSelectedChunks_RegularRange pins the touched chunks of a normal 2-D
// selection: rows 3-12 and columns 0-4 of a 4×4-chunked grid.
func TestSelectedChunks_RegularRange(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{3, 0},
		Count:  []uint64{10, 5},
		Stride: []uint64{1, 1},
		Block:  []uint64{1, 1},
	}

	got, err := selectedChunks(sel, []uint64{4, 4, 8}, []uint64{16, 16})
	require.NoError(t, err)
	require.Equal(t, core.ChunkSelection{{{First: 0, Last: 3}}, {{First: 0, Last: 1}}}, got)
}

// TestSelectedChunks_StridedSkipsGaps: blocks of 2 every 16 elements in a
// 4-chunked dimension touch every fourth chunk only, not the bounding range.
func TestSelectedChunks_StridedSkipsGaps(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{3},
		Count:  []uint64{3},
		Stride: []uint64{16},
		Block:  []uint64{2},
	}

	got, err := selectedChunks(sel, []uint64{4, 8}, []uint64{64})
	require.NoError(t, err)
	// Blocks 3-4, 19-20, 35-36: chunks 0-1, 4-5, 8-9.
	require.Equal(t, core.ChunkSelection{{{First: 0, Last: 1}, {First: 4, Last: 5}, {First: 8, Last: 9}}}, got)
}
