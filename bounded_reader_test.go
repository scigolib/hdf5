package hdf5

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// boundedReaderFixture is a reference file with one dataset; api_coverage_test.go
// already relies on its structure (one dataset of 100 float64 elements).
const boundedReaderFixture = "testdata/reference/filespace_1_8.h5"

// TestFileReaderBounded verifies that File.Reader() returns a reader bounded by
// the real file size (ADR-003): reads inside the file work as before, reads
// past it fail with an error instead of a platform short read.
func TestFileReaderBounded(t *testing.T) {
	t.Parallel()

	fi, err := os.Stat(boundedReaderFixture)
	require.NoError(t, err)
	size := fi.Size()
	require.Greater(t, size, int64(8), "fixture is too small for this test")

	f, err := Open(boundedReaderFixture)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	reader := f.Reader()
	require.NotNil(t, reader)

	// Read inside the file: the HDF5 signature at offset 0.
	sig := make([]byte, 8)
	n, err := reader.ReadAt(sig, 0)
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.Equal(t, "\x89HDF\r\n\x1a\n", string(sig))

	// The last byte of the file is readable.
	last := make([]byte, 1)
	n, err = reader.ReadAt(last, size-1)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// A read that ends exactly at the file size is readable.
	tail := make([]byte, 4)
	n, err = reader.ReadAt(tail, size-4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// A zero-length read at the file size is allowed.
	n, err = reader.ReadAt(nil, size)
	require.NoError(t, err)
	require.Zero(t, n)

	// A read starting at the file size is past the end.
	n, err = reader.ReadAt(make([]byte, 1), size)
	require.Error(t, err)
	require.Zero(t, n)
	require.Contains(t, err.Error(), "read beyond end of file")

	// A read starting inside but ending past the file size is past the end.
	_, err = reader.ReadAt(make([]byte, 8), size-4)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file")

	// A negative offset is rejected.
	_, err = reader.ReadAt(make([]byte, 1), -1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file")
}

// TestDatasetReadWithBoundedReader verifies that ordinary dataset reads are
// unchanged by the bounded reader.
func TestDatasetReadWithBoundedReader(t *testing.T) {
	t.Parallel()

	f, err := Open(boundedReaderFixture)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	data, err := ds.Read()
	require.NoError(t, err)
	require.Len(t, data, 100, "filespace_1_8 dataset has 100 elements")
}
