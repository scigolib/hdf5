package hdf5_test

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5"
	"github.com/stretchr/testify/require"
)

// datasetAt returns the dataset at an absolute path, or stops the test.
func datasetAt(t *testing.T, file *hdf5.File, path string) *hdf5.Dataset {
	t.Helper()

	var found *hdf5.Dataset
	file.Walk(func(at string, obj hdf5.Object) {
		if ds, ok := obj.(*hdf5.Dataset); ok && at == path {
			found = ds
		}
	})
	require.NotNil(t, found, "no dataset at %s", path)

	return found
}

// TestReadSlice_NoDimensions reads datasets of the official suite whose
// dataspace has no dimensions: a scalar, which holds one value, and a null
// dataspace, which holds none. ReadSlice has nothing to select a block from
// and must say so. For the scalar it used to return no values and no error,
// which a caller cannot tell from an empty dataset.
func TestReadSlice_NoDimensions(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		path   string
		values int // What Read returns.
	}{
		{"scalar dataspace", "h5diff_dset1.h5", "/g1/scalar", 1},
		{"null dataspace", "tnullspace.h5", "/dset", 0},
	}

	selections := map[string][]uint64{"empty": {}, "nil": nil}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := hdf5.Open(filepath.Join("testdata", "hdf5_official", tt.file))
			require.NoError(t, err)
			defer func() { require.NoError(t, file.Close()) }()

			ds := datasetAt(t, file, tt.path)

			shape, err := ds.Shape()
			require.NoError(t, err)
			require.Empty(t, shape, "the fixture is not a dataset without dimensions")

			whole, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, whole, tt.values)

			for name, selection := range selections {
				got, err := ds.ReadSlice(selection, selection)
				require.Error(t, err, "ReadSlice with %s start and count", name)
				require.Nil(t, got, "ReadSlice with %s start and count", name)
			}

			// ReadHyperslab refused the same selection before this change
			// and still does.
			got, err := ds.ReadHyperslab(&hdf5.HyperslabSelection{Start: []uint64{}, Count: []uint64{}})
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}

// TestReadSlice_ReturnsFloat64 pins what the doc comments of ReadSlice and
// ReadHyperslab now say: the values of an integer dataset come back as a
// []float64, as from Read. It passes before and after the change; only the
// comments were wrong.
func TestReadSlice_ReturnsFloat64(t *testing.T) {
	name := filepath.Join(t.TempDir(), "counts.h5")

	fw, err := hdf5.CreateForWrite(name, hdf5.CreateTruncate)
	require.NoError(t, err)
	dw, err := fw.CreateDataset("/counts", hdf5.Uint32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, dw.Write([]uint32{1, 2, 4294967295}))
	require.NoError(t, fw.Close())

	file, err := hdf5.Open(name)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	ds := datasetAt(t, file, "/counts")
	want := []float64{1, 2, 4294967295}

	whole, err := ds.Read()
	require.NoError(t, err)
	require.Equal(t, want, whole)

	sliced, err := ds.ReadSlice([]uint64{0}, []uint64{3})
	require.NoError(t, err)
	require.Equal(t, want, sliced)

	selected, err := ds.ReadHyperslab(&hdf5.HyperslabSelection{Start: []uint64{0}, Count: []uint64{3}})
	require.NoError(t, err)
	require.Equal(t, want, selected)
}
