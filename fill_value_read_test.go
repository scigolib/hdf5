package hdf5

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRead_FillUnallocatedContiguous: a contiguous dataset whose storage
// was never allocated (address undefined) reads as its fill value, as H5D__read
// does (H5Dio.c:239-279). All of these carry a fill value message with the
// default value; h5dump prints zeros for each. The reader used to refuse them.
func TestRead_FillUnallocatedContiguous(t *testing.T) {
	cases := []struct {
		file, path string
		want       []float64
	}{
		{"testdata/hdf5_official/tmtimen.h5", "/Dataset1", []float64{0}},
		{"testdata/hdf5_official/tnamed_dtype_attr.h5", "/Dataset", []float64{0}},
		{"testdata/hdf5_official/h5repack_named_dtypes.h5", "/A", []float64{0, 0, 0}},
		{"testdata/hdf5_official/h5repack_named_dtypes.h5", "/M/M/B", []float64{0, 0, 0}},
		{"testdata/hdf5_official/h5repack_named_dtypes.h5", "/O", []float64{0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.file+tc.path, func(t *testing.T) {
			_, ds := openDataset(t, tc.file, tc.path)

			got, err := ds.Read()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			if len(tc.want) > 1 {
				slice, err := ds.ReadSlice([]uint64{1}, []uint64{2})
				require.NoError(t, err)
				require.Equal(t, tc.want[1:], slice)
			}
		})
	}
}

// TestRead_FillUnallocatedContiguousHuge: tlayouto.h5's dataset is
// 8589934592 x 268435457 unallocated elements. h5dump prints fill values
// forever; here the result has to be allocated, so the read limit refuses it
// instead of an attempt at an exabyte buffer.
func TestRead_FillUnallocatedContiguousHuge(t *testing.T) {
	f, err := Open("testdata/hdf5_official/tlayouto.h5")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	ds := findFirstDataset(f)
	require.NotNil(t, ds)

	_, err = ds.Read()
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLimit), "want ErrLimit, got %v", err)

	slice, err := ds.ReadSlice([]uint64{5, 7}, []uint64{1, 2})
	require.NoError(t, err)
	require.Equal(t, []float64{0, 0}, slice)
}

// TestRead_FillMissingChunks: chunked_fill_missing.h5 is fill18.h5
// with DS1 grown from 6x10 to 10x10 (dataspace patched, checksum recomputed),
// so chunk row 2 (rows 8-9) was never written. The fill value is 99 and fill
// time IFSET, so C reads the missing chunks as 99 (H5Dchunk.c:2936-2949);
// h5dump agrees (want below is its output). The reader used to return zeros.
func TestRead_FillMissingChunks(t *testing.T) {
	want := make([]float64, 0, 100)
	want = append(want,
		0, -1, -2, -3, -4, -5, -6, 99, 99, 99,
		0, 0, 0, 0, 0, 0, 0, 99, 99, 99,
		0, 1, 2, 3, 4, 5, 6, 99, 99, 99,
		0, 2, 4, 6, 8, 10, 12, 99, 99, 99)
	for len(want) < 100 {
		want = append(want, 99)
	}

	_, ds := openDataset(t, "testdata/fill/chunked_fill_missing.h5", "/DS1")

	got, err := ds.Read()
	require.NoError(t, err)
	require.Equal(t, want, got)

	// A slice across present and missing chunks.
	slice, err := ds.ReadSlice([]uint64{2, 5}, []uint64{8, 3})
	require.NoError(t, err)
	var wantSlice []float64
	for r := 2; r < 10; r++ {
		wantSlice = append(wantSlice, want[r*10+5:r*10+8]...)
	}
	require.Equal(t, wantSlice, slice)

	// A slice entirely inside the missing chunk row.
	slice, err = ds.ReadSlice([]uint64{8, 0}, []uint64{2, 10})
	require.NoError(t, err)
	require.Equal(t, want[80:], slice)
}
