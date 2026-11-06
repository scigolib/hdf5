package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Reading datasets whose element type is an array (TASK-071). The values come
// back flattened in C order, array dimensions innermost, as h5dump prints them
// and as C converts them (H5T__conv_array, H5Tconv_array.c:195-199).

func TestReadArray_OfficialFiles(t *testing.T) {
	cases := []struct {
		file, path string
		n          int
		want       func(k int) float64
	}{
		{"tarray1.h5", "/Dataset1", 4 * 4, func(k int) float64 { // [4] int32: i*10+j
			return float64(k/4*10 + k%4)
		}},
		{"tarray2.h5", "/Dataset1", 4 * 60, func(k int) float64 { // [3][4][5] int32
			i, r := k/60, k%60
			return float64(i*1000 + r/20*100 + r/5%4*10 + r%5)
		}},
		{"tarray3.h5", "/Dataset1", 4 * 72, func(k int) float64 { // [4] of [6][3] int32
			i, r := k/72, k%72
			return float64(i*1000 + r/18*100 + r/3%6*10 + r%3)
		}},
		{"tarray8.h5", "/DS1", 1025, func(k int) float64 { return float64(k) }}, // [1025] int32
		{"tarray1_big.h5", "/Dataset1", 2000 * 1000, func(k int) float64 { // [1000] int32, 2000 elements
			return float64(k / 1000)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			f := openOfficial(t, tc.file)
			ds := findDatasetPath(t, f, tc.path)

			got, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, got, tc.n)
			for k, v := range got {
				if v != tc.want(k) {
					t.Fatalf("value %d = %v, want %v", k, v, tc.want(k))
				}
			}
		})
	}
}

func TestReadArray_Slice(t *testing.T) {
	f := openOfficial(t, "tarray1.h5")
	ds := findDatasetPath(t, f, "/Dataset1")

	got, err := ds.ReadSlice([]uint64{1}, []uint64{2})
	require.NoError(t, err)
	require.Equal(t, []float64{10, 11, 12, 13, 20, 21, 22, 23}, got)
}

func TestReadArray_Compound(t *testing.T) {
	// tarray4.h5: [4] of {int32 i; float32 f}, i = 10·elem+j, f = 2.5·elem+j.
	f := openOfficial(t, "tarray4.h5")
	ds := findDatasetPath(t, f, "/Dataset1")

	got, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Len(t, got, 16)
	for k, rec := range got {
		elem, j := k/4, k%4
		require.EqualValues(t, elem*10+j, rec["i"], "record %d", k)
		require.InDelta(t, float64(elem)*2.5+float64(j), rec["f"], 0, "record %d", k)
	}
}

func TestReadArray_UnsupportedBase(t *testing.T) {
	// tarray6.h5: [4] of vlen uint32 has no float64 conversion.
	f := openOfficial(t, "tarray6.h5")
	ds := findDatasetPath(t, f, "/Dataset1")

	_, err := ds.Read()
	require.ErrorIs(t, err, ErrNotSupported)
}

func TestReadArray_WriterRoundTrip(t *testing.T) {
	want := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	ints := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	cases := []struct {
		name  string
		build func(t *testing.T, fw *FileWriter)
	}{
		{"contiguous", func(t *testing.T, fw *FileWriter) {
			ds, err := fw.CreateDataset("/vecs", ArrayInt32, []uint64{4}, WithArrayDims([]uint64{3}))
			require.NoError(t, err)
			require.NoError(t, ds.Write(ints))
		}},
		{"chunked", func(t *testing.T, fw *FileWriter) {
			ds, err := fw.CreateDataset("/vecs", ArrayInt32, []uint64{4},
				WithArrayDims([]uint64{3}), WithChunkDims([]uint64{2}))
			require.NoError(t, err)
			require.NoError(t, ds.Write(ints))
		}},
		{"chunked_resized", func(t *testing.T, fw *FileWriter) {
			ds, err := fw.CreateDataset("/vecs", ArrayInt32, []uint64{2},
				WithArrayDims([]uint64{3}), WithChunkDims([]uint64{2}),
				WithMaxDims([]uint64{Unlimited}))
			require.NoError(t, err)
			require.NoError(t, ds.Write(ints[:6]))
			require.NoError(t, ds.Resize([]uint64{4}))
			require.NoError(t, ds.Write(ints))
		}},
		{"float64_2d", func(t *testing.T, fw *FileWriter) {
			ds, err := fw.CreateDataset("/vecs", ArrayFloat64, []uint64{2, 2}, WithArrayDims([]uint64{3}))
			require.NoError(t, err)
			require.NoError(t, ds.Write(want))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "array.h5")
			fw, err := CreateForWrite(path, CreateTruncate)
			require.NoError(t, err)
			tc.build(t, fw)
			require.NoError(t, fw.Close())

			f, err := Open(path)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			ds := findDatasetPath(t, f, "/vecs")

			got, err := ds.Read()
			require.NoError(t, err)
			require.Equal(t, want, got)

			// The structure is recovered from the shape and the array dims.
			info, err := ds.Datatype()
			require.NoError(t, err)
			require.Equal(t, ClassArray, info.Class)
			require.Equal(t, []uint64{3}, info.ArrayDims)
		})
	}
}

func TestReadArray_ReadLimit(t *testing.T) {
	// Four elements of int32[3] in chunks: 48 bytes stored, 96 bytes as
	// float64. A limit of 64 bytes must refuse the read; sized by one float64
	// per element (32 bytes) it would pass.
	path := filepath.Join(t.TempDir(), "array.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/vecs", ArrayInt32, []uint64{4},
		WithArrayDims([]uint64{3}), WithChunkDims([]uint64{2}))
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}))
	require.NoError(t, fw.Close())

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	f.SetReadLimit(64)
	rd := findDatasetPath(t, f, "/vecs")

	_, err = rd.Read()
	require.ErrorIs(t, err, ErrLimit)
	_, err = rd.ReadSlice([]uint64{0}, []uint64{4})
	require.ErrorIs(t, err, ErrLimit)

	f.SetReadLimit(96)
	got, err := rd.Read()
	require.NoError(t, err)
	require.Len(t, got, 12)
}
