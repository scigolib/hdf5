package core

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestContigDataSize_EmptyReader: a reader that reports size 0 is an empty
// file, not one of unknown size. utils.ValidateRegion treats a zero limit as
// unknown, so contigDataSize accepted any storage against an empty reader
// (found by FuzzDatasetDescriptor: 83,161,112 bytes at address 0 of a 0-byte
// file). The storage must be rejected as lying past the end of the file.
func TestContigDataSize_EmptyReader(t *testing.T) {
	_, err := contigDataSize(bytes.NewReader(nil), nil, 0, 1_000_000, 8, "dataset data")
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file")

	// Nothing to read is still fine.
	size, err := contigDataSize(bytes.NewReader(nil), nil, 0, 0, 8, "dataset data")
	require.NoError(t, err)
	require.Zero(t, size)
}
