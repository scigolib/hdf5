package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAppendRowsFailureIsSticky checks that once an append fails while writing, every later append and
// flush returns that error rather than writing rows at the wrong place, and the rows flushed before it
// stay readable.
func TestAppendRowsFailureIsSticky(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "sticky.h5")
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float32, []uint64{0, 2},
		WithChunkDims([]uint64{2, 2}), WithMaxDims([]uint64{Unlimited, 2}))
	require.NoError(t, err)

	require.NoError(t, ds.AppendRows([]float32{1, 2, 3, 4}))
	require.NoError(t, ds.Flush())

	// Every write to the file now fails.
	require.NoError(t, fw.writer.File().Close())

	failed := ds.AppendRows([]float32{5, 6, 7, 8, 9, 10})
	require.Error(t, failed)
	require.ErrorIs(t, ds.AppendRows([]float32{5, 6}), failed, "a retry returns the first error")
	require.ErrorIs(t, ds.AppendRawRows(make([]byte, 8)), failed)
	require.ErrorIs(t, ds.Flush(), failed)
	require.ErrorIs(t, ds.Close(), failed)

	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()
	var got []float64
	f.Walk(func(p string, obj Object) {
		if d, ok := obj.(*Dataset); ok && p == "/data" {
			got, err = d.Read()
			require.NoError(t, err)
		}
	})
	require.Equal(t, []float64{1, 2, 3, 4}, got)
}
