package hdf5

import (
	"math"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// TestGenerateChunkCoordinates_RejectsUnrepresentableSpan is the ADR-003
// regression test for the hyperslab chunk-limit computation. The span is
// derived from dataset dimensions and the selection, i.e. from file metadata;
// converting it to int without a check wraps, and the wrapped value used to
// size the result slice in make (a panic on any platform, not only 32-bit).
func TestGenerateChunkCoordinates_RejectsUnrepresentableSpan(t *testing.T) {
	first := []uint64{0}
	last := []uint64{math.MaxUint64 - 1}

	_, err := generateChunkCoordinates(first, last)
	require.Error(t, err)
	require.Contains(t, err.Error(), "platform limit")
}

// TestGenerateChunkCoordinates_RejectsChunkCountAbovePolicy covers the policy
// cap: a representable but enormous chunk count would allocate gigabytes of
// descriptors before the loop even starts.
func TestGenerateChunkCoordinates_RejectsChunkCountAbovePolicy(t *testing.T) {
	first := []uint64{0}
	last := []uint64{utils.MaxChunkCoordinates} // span = cap + 1

	_, err := generateChunkCoordinates(first, last)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum")
}

// TestFindOverlappingChunks_RankMismatch covers the review finding that a chunk
// grid shorter than the selection rank was indexed past its end.
func TestFindOverlappingChunks_RankMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{1, 1},
		Stride: []uint64{1, 1},
		Block:  []uint64{1, 1},
	}

	_, err := findOverlappingChunks(sel, []uint64{4}, []uint64{10, 10})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rank mismatch")
}

// TestGenerateChunkCoordinates_RegularRange pins the behavior the guard must
// not change: a normal 2-D span still produces every coordinate.
func TestGenerateChunkCoordinates_RegularRange(t *testing.T) {
	first := []uint64{0, 0}
	last := []uint64{1, 1}

	coords, err := generateChunkCoordinates(first, last)
	require.NoError(t, err)
	require.Len(t, coords, 4)
	require.ElementsMatch(t, [][]uint64{{0, 0}, {0, 1}, {1, 0}, {1, 1}}, coords)
}
