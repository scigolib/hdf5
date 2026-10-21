package hdf5

import (
	"math"
	"testing"

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
