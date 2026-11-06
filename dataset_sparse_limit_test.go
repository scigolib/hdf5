package hdf5

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// Sparse chunked datasets from the C test suite: their dataspaces are far
// larger than their files because only a few chunks were ever written, which
// is legal (unwritten chunks read as the fill value, H5Dchunk.c:3041-3046).
// A whole read is refused under the default 4 GiB read limit without
// allocating; a window of the same dataset still reads, with the values
// h5dump prints.
//
// These tests cannot be shown red safely: before the fix a whole Read() of
// /dset4gb allocates 4 GiB of raw bytes and then a 32 GiB []float64.

func openOfficial(t *testing.T, name string) *File {
	t.Helper()

	f, err := Open(filepath.Join("testdata", "hdf5_official", name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	return f
}

// TestReadLimit_SparseTbigdims: /dset4gb is 4,294,967,306 int8 values
// (10 bytes over 4 GiB raw, 32 GiB as []float64) in a 6 KB file with two
// 1024-byte chunks.
func TestReadLimit_SparseTbigdims(t *testing.T) {
	f := openOfficial(t, "tbigdims.h5")
	ds := findDatasetPath(t, f, "/dset4gb")

	var err error
	allocated := allocatedBy(func() { _, err = ds.Read() })
	require.ErrorIs(t, err, ErrLimit)
	require.False(t, errors.Is(err, ErrCorrupt))
	require.Contains(t, err.Error(), "needs 34359738448 bytes") // 4,294,967,306 × 8.
	require.Contains(t, err.Error(), "read limit of 4294967296 bytes")
	require.Less(t, allocated, uint64(allocBudget))

	// h5dump -d /dset4gb -s 0 -c 8: fill value 0.
	head, err := ds.ReadSlice([]uint64{0}, []uint64{8})
	require.NoError(t, err)
	require.Equal(t, []float64{0, 0, 0, 0, 0, 0, 0, 0}, head)

	// h5dump -d /dset4gb -s 4294967296 -c 10: 10 … 19 (the last written chunk).
	tail, err := ds.ReadSlice([]uint64{4294967296}, []uint64{10})
	require.NoError(t, err)
	require.Equal(t, []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}, tail)
}

// TestReadLimit_SparseConvertedResult: /big holds 1 Gi int8 values. The raw
// buffer (1 GiB) is within the default limit, but Read returns []float64, an
// 8 GiB allocation, and the larger allocation is what the limit bounds.
func TestReadLimit_SparseConvertedResult(t *testing.T) {
	f := openOfficial(t, "h5diff_hyper1.h5")
	ds := findDatasetPath(t, f, "/big")

	var err error
	allocated := allocatedBy(func() { _, err = ds.Read() })
	require.ErrorIs(t, err, ErrLimit)
	require.Contains(t, err.Error(), "needs 8589934592 bytes")
	require.Less(t, allocated, uint64(allocBudget))

	// The one stored chunk is chunk 256 (elements 268435456 …);
	// h5dump -d /big -s 268435456 -c 8: 31 × 8.
	window, err := ds.ReadSlice([]uint64{268435456}, []uint64{8})
	require.NoError(t, err)
	require.Equal(t, []float64{31, 31, 31, 31, 31, 31, 31, 31}, window)

	// Windows in unwritten chunks read as the fill value, -1
	// (h5dump -d /big -s 0 -c 4), as H5D__chunk_read does
	// (H5Dchunk.c:2936-2949); they used to read as 0.
	window, err = ds.ReadSlice([]uint64{0}, []uint64{4})
	require.NoError(t, err)
	require.Equal(t, []float64{-1, -1, -1, -1}, window)
}

// TestRead_UnallocatedContiguous: /g1/arrayd of h5diff_dset1.h5 is one element
// of a 16,777,217-double array type (134,217,736 bytes) that was never written:
// h5ls reports "0 allocated bytes", its address is undefined. C accepts it at
// open (H5Dcontig.c:399 checks the region only for a defined address) and reads
// the fill value, which this library now does too (TestRead_FillUnallocated*).
// What still refuses this one is the array datatype, which has no float64
// conversion: the read is refused as not supported, before the 128 MiB element
// is allocated, and it is not reported as corruption. (It used to be refused
// as "not allocated".)
func TestRead_UnallocatedContiguous(t *testing.T) {
	f := openOfficial(t, "h5diff_dset1.h5")
	ds := findDatasetPath(t, f, "/g1/arrayd")

	var err error
	allocated := allocatedBy(func() { _, err = ds.ReadSlice([]uint64{0}, []uint64{1}) })
	require.Error(t, err)
	require.ErrorIs(t, err, utils.ErrNotSupported) // hdf5.ErrNotSupported once main re-exports it.
	require.False(t, errors.Is(err, ErrCorrupt))
	require.Less(t, allocated, uint64(allocBudget))
}
