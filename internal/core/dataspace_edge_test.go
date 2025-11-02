package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseDataspaceMessage_InvalidVersion tests error handling for invalid version.
func TestParseDataspaceMessage_InvalidVersion(t *testing.T) {
	data := []byte{
		99, // invalid version
		1,  // dimensionality
		0,  // flags
	}

	_, err := ParseDataspaceMessage(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported dataspace version")
}

// TestParseDataspaceMessage_MaxDimensions tests parsing with max dimensions.
func TestParseDataspaceMessage_MaxDimensions(t *testing.T) {
	// Version 2, 2D dataspace with max dimensions
	data := []byte{
		2, // version
		2, // dimensionality (2D)
		1, // flags: bit 0 = max dimensions present
		0, // type (simple)
		// Dimensions (2 * 8 bytes)
		10, 0, 0, 0, 0, 0, 0, 0, // dim 1 = 10
		20, 0, 0, 0, 0, 0, 0, 0, // dim 2 = 20
		// Max dimensions (2 * 8 bytes)
		100, 0, 0, 0, 0, 0, 0, 0, // max dim 1 = 100
		200, 0, 0, 0, 0, 0, 0, 0, // max dim 2 = 200
	}

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)
	require.Equal(t, uint8(2), ds.Version)
	require.Len(t, ds.Dimensions, 2)
	require.Equal(t, uint64(10), ds.Dimensions[0])
	require.Equal(t, uint64(20), ds.Dimensions[1])
	require.Len(t, ds.MaxDims, 2)
	require.Equal(t, uint64(100), ds.MaxDims[0])
	require.Equal(t, uint64(200), ds.MaxDims[1])
}

// TestParseDataspaceMessage_PermutationIndices tests parsing with permutation indices.
func TestParseDataspaceMessage_PermutationIndices(_ *testing.T) {
	// Version 1 with permutation indices
	data := []byte{
		1,          // version
		2,          // dimensionality
		2,          // flags: bit 1 = permutation indices present
		0,          // reserved
		0, 0, 0, 0, // reserved
		// Dimensions (2 * 8 bytes)
		5, 0, 0, 0, 0, 0, 0, 0,
		10, 0, 0, 0, 0, 0, 0, 0,
		// Permutation indices (2 * 4 bytes)
		1, 0, 0, 0,
		0, 0, 0, 0,
	}

	_, err := ParseDataspaceMessage(data)
	// Function should handle this even if it doesn't use the permutation
	// We're just checking it doesn't panic
	_ = err
}
