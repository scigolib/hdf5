package hdf5

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// The chunk index tests of TASK-058. Before it every version 4 index was
// refused ("unsupported chunk index type N: only the v1 B-tree index"); the
// red census of the official and corpus files (88 readable v4 datasets, all
// refused) is in the task report, and after the change their values match
// h5dump one for one.

// sliceToFloat64 converts a numeric slice returned by ReadSlice.
func sliceToFloat64(t *testing.T, v any) []float64 {
	t.Helper()
	rv := reflect.ValueOf(v)
	require.Equal(t, reflect.Slice, rv.Kind(), "ReadSlice returned %T", v)
	out := make([]float64, rv.Len())
	for i := range out {
		e := rv.Index(i)
		switch e.Kind() { //nolint:exhaustive // Only the numeric kinds of these fixtures.
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out[i] = float64(e.Int())
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			out[i] = float64(e.Uint())
		case reflect.Float32, reflect.Float64:
			out[i] = e.Float()
		default:
			t.Fatalf("ReadSlice returned %T", v)
		}
	}
	return out
}

// checkChunkIndexDataset reads one dataset whole and through a window and
// compares both with want (row-major, the dataset's whole contents); the
// chunk iterator lists the stored chunks, each once.
func checkChunkIndexDataset(t *testing.T, f *File, path string, index uint8, chunks int, want []float64) {
	t.Helper()
	ds := findDatasetPath(t, f, path)
	desc, err := ds.descriptor()
	require.NoError(t, err)
	require.Equal(t, index, desc.Layout().ChunkIndexType, "%s: chunk index type", path)

	all, err := ds.Read()
	require.NoError(t, err)
	require.Equal(t, want, all, path)

	// A window that starts and ends inside chunks: [lo, hi) per dimension.
	dims := desc.Dims()
	start := make([]uint64, len(dims))
	count := make([]uint64, len(dims))
	for i, n := range dims {
		start[i] = n / 3
		count[i] = max(n-start[i]-n/4, 1)
	}
	part, err := ds.ReadSlice(start, count)
	require.NoError(t, err)

	var exp []float64
	idx := make([]uint64, len(dims))
	for {
		lin := uint64(0)
		for i := range dims {
			lin = lin*dims[i] + start[i] + idx[i]
		}
		exp = append(exp, want[lin])
		i := len(dims) - 1
		for ; i >= 0; i-- {
			idx[i]++
			if idx[i] < count[i] {
				break
			}
			idx[i] = 0
		}
		if i < 0 {
			break
		}
	}
	require.Equal(t, exp, sliceToFloat64(t, part), "%s: window %v+%v", path, start, count)

	it, err := ds.ChunkIterator()
	require.NoError(t, err)
	seen := map[string]bool{}
	for it.Next() {
		k := fmt.Sprint(it.ChunkCoords())
		require.False(t, seen[k], "%s: chunk %s listed twice", path, k)
		seen[k] = true
	}
	require.NoError(t, it.Err())
	require.Len(t, seen, chunks, "%s: stored chunks", path)
}

func seqFloat(n int, offset float64) []float64 {
	v := make([]float64, n)
	for i := range v {
		v[i] = float64(i) + offset
	}
	return v
}

// TestChunkIndex_OfficialFiles reads the version 4 indexes of the official
// suite (h5fc_ext1_f.h5, h5fc_edge_v3.h5, h5copytst.h5) and compares them
// with h5dump 1.14.6: fixed array, extensible array, v2 B-tree and implicit,
// with and without written chunks, filtered, and with the "partial edge
// chunks are not filtered" flag.
func TestChunkIndex_OfficialFiles(t *testing.T) {
	t.Run("h5fc_ext1_f", func(t *testing.T) {
		f, err := Open("testdata/hdf5_official/h5fc_ext1_f.h5")
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		full := seqFloat(24, 0)
		// DSET_EA has only its first column of chunks written (h5dump).
		ea := []float64{0, 1, 2, 0, 0, 0, 6, 7, 8, 0, 0, 0, 12, 13, 14, 0, 0, 0, 18, 19, 20, 0, 0, 0}
		zero := make([]float64, 24)

		checkChunkIndexDataset(t, f, "/DSET_EA", core.ChunkIndexEArray, 2, ea)
		checkChunkIndexDataset(t, f, "/DSET_FA", core.ChunkIndexFArray, 4, full)
		checkChunkIndexDataset(t, f, "/DSET_NONE", core.ChunkIndexNone, 4, full)
		checkChunkIndexDataset(t, f, "/GROUP/DSET_BT2", core.ChunkIndexBT2, 4, full)
		checkChunkIndexDataset(t, f, "/DSET_NDATA_BT2", core.ChunkIndexBT2, 0, zero)
		checkChunkIndexDataset(t, f, "/GROUP/DSET_NDATA_EA", core.ChunkIndexEArray, 0, zero)
		checkChunkIndexDataset(t, f, "/GROUP/DSET_NDATA_FA", core.ChunkIndexFArray, 0, zero)
	})

	t.Run("edge chunks not filtered", func(t *testing.T) {
		f, err := Open("testdata/hdf5_official/h5fc_edge_v3.h5")
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		// 12×6 in 5×5 deflated chunks with layout flag 0x01: the edge chunks
		// are stored raw (H5Dchunk.c:4721-4728). h5dump: every value 100.
		want := make([]float64, 72)
		for i := range want {
			want[i] = 100
		}
		checkChunkIndexDataset(t, f, "/DSET_EDGE", core.ChunkIndexFArray, 6, want)
	})

	t.Run("filtered fixed array", func(t *testing.T) {
		f, err := Open("testdata/hdf5_official/h5copytst.h5")
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		checkChunkIndexDataset(t, f, "/compressed", core.ChunkIndexFArray, 3, []float64{1, 2, 3, 4, 5, 6})
	})
}

// TestChunkIndex_CLibraryFixture reads testdata/chunk_index/v4_indexes.h5,
// written by the C library: this library wrote a source file with v1 B-tree
// chunked datasets (values 0,1,2,… as int16, or i+0.5 as float64), and
// `h5repack --low=2 --high=2` (HDF5 1.14.6) rewrote it with the latest format,
// where the library picks the index from the maximum dimensions
// (H5Dchunk.c:966-990). It covers what the official files do not: the single
// chunk index (plain and filtered), a paged fixed array (1100 chunks; pages
// of 1024), an extensible array past its index block into super blocks
// (300 chunks), filtered extensible arrays and v2 B-trees, and a v2 B-tree
// with internal nodes (240 chunks). Edge chunks: 31 rows in chunks of 2.
func TestChunkIndex_CLibraryFixture(t *testing.T) {
	f, err := Open("testdata/chunk_index/v4_indexes.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	for _, tc := range []struct {
		path   string
		index  uint8
		n      int
		chunks int
		offset float64
	}{
		{"/single", core.ChunkIndexSingle, 24, 1, 0.5},
		{"/single_gzip", core.ChunkIndexSingle, 24, 1, 0.5},
		{"/fa_paged", core.ChunkIndexFArray, 1100, 1100, 0},
		{"/fa_paged_gzip", core.ChunkIndexFArray, 1100, 1100, 0},
		{"/ea_sblk", core.ChunkIndexEArray, 300, 300, 0},
		{"/ea_sblk_gzip", core.ChunkIndexEArray, 300, 300, 0},
		{"/ea_2d", core.ChunkIndexEArray, 45, 5, 0.5},
		{"/bt2", core.ChunkIndexBT2, 930, 240, 0},
		{"/bt2_gzip", core.ChunkIndexBT2, 930, 240, 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			checkChunkIndexDataset(t, f, tc.path, tc.index, tc.chunks, seqFloat(tc.n, tc.offset))
		})
	}
}
