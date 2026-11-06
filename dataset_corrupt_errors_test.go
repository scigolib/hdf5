package hdf5

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadSlice_Issue75_40GB is the reproduction from issue #75: a 12-value
// float64 dataset whose datatype size byte 3 is 0xCB claims 3,405,774,856
// bytes per element, 40,869,298,272 bytes in a 5,656-byte file. Before the fix
// ReadSlice asked the runtime for 40,873,492,480 bytes and the process died
// (fatal error: out of memory, dataset_read_hyperslab.go:483 on 257fb7d).
//
// This test cannot be shown red safely: on the old code it kills the test
// process, or blocks while the operating system commits 40 GB. The red proof
// of the same path uses a megabyte-sized claim
// (TestReadSlice_CorruptSize_Contiguous1D).
func TestReadSlice_Issue75_40GB(t *testing.T) {
	const elemSize = 0xCB000008 // 3,405,774,856 bytes.

	path := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{12}), "/x", elemSize)
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	var readErr error
	allocated := allocatedBy(func() { _, readErr = ds.ReadSlice([]uint64{0}, []uint64{12}) })
	require.ErrorIs(t, readErr, ErrCorrupt)
	require.Less(t, allocated, uint64(allocBudget))

	_, err = ds.Read()
	require.ErrorIs(t, err, ErrCorrupt)

	_, err = ds.ReadHyperslab(&HyperslabSelection{Start: []uint64{0}, Count: []uint64{12}})
	require.ErrorIs(t, err, ErrCorrupt)
}

// TestCorruptSize_ErrorsWrapErrCorrupt: every rejection of the fixtures in
// dataset_read_bounds_test.go is reported as corruption, so callers can tell
// it from a refused read (ErrLimit).
func TestCorruptSize_ErrorsWrapErrCorrupt(t *testing.T) {
	contig := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{4, 3}), "/x", patchedElemSize)
	chunked := patchDatatypeSize(t, writeFloat64Dataset(t, []uint64{12}, WithChunkDims([]uint64{4})), "/x", patchedElemSize)

	for _, tc := range []struct {
		name, path string
		want       string
	}{
		{"contiguous", contig, "read beyond end of file"},
		{"chunk element size mismatch", chunked, "stored datatype size in chunk layout (8) does not match datatype description (1048584)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Open(tc.path)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()
			ds := findDatasetPath(t, f, "/x")

			_, err = ds.Read()
			require.ErrorIs(t, err, ErrCorrupt)
			require.False(t, errors.Is(err, ErrLimit))
			require.Contains(t, err.Error(), tc.want)
			require.Contains(t, err.Error(), `dataset "x"`)

			_, err = ds.ChunkIterator()
			require.ErrorIs(t, err, ErrCorrupt)
		})
	}
}

// TestDataset_DescriptorConcurrent reads one dataset from several goroutines:
// the descriptor is built once and shared; run with -race.
func TestDataset_DescriptorConcurrent(t *testing.T) {
	path := writeFloat64Dataset(t, []uint64{64}, WithChunkDims([]uint64{16}))
	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	ds := findDatasetPath(t, f, "/x")

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var err error
			if g%2 == 0 {
				_, err = ds.Read()
			} else {
				_, err = ds.ReadSlice([]uint64{uint64(g)}, []uint64{4})
			}
			if err != nil {
				t.Errorf("goroutine %d: %v", g, err)
			}
		}(g)
	}
	wg.Wait()
}
