package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// readBack opens path and returns the dataset at dsPath; the file is closed
// when the test ends.
func readBack(t *testing.T, path, dsPath string) *Dataset {
	t.Helper()
	f, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	var found *Dataset
	f.Walk(func(p string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && p == dsPath {
			found = ds
		}
	})
	require.NotNil(t, found, "dataset %s not found", dsPath)
	return found
}

func zeroRowsCompoundType(t *testing.T) *CompoundType {
	t.Helper()
	ct, err := NewPackedCompoundType([]CompoundMember{
		{Name: "id", Type: Int32},
		{Name: "value", Type: Float64},
	})
	require.NoError(t, err)
	return ct
}

// An extendible dataset may start with no row, as the C library's packet
// tables do (H5PTcreate makes a dataset of dims 0 and maxdims unlimited).
func TestCreateDatasetZeroRowsExtendible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zero.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/empty", Float64, []uint64{0},
		WithChunkDims([]uint64{4}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{}))

	_, err = fw.CreateDataset("/empty2d", Int32, []uint64{0, 3},
		WithChunkDims([]uint64{8, 3}), WithMaxDims([]uint64{Unlimited, 3}),
		WithGZIPCompression(6))
	require.NoError(t, err)

	_, err = fw.CreateCompound("/records", zeroRowsCompoundType(t), []uint64{0},
		WithChunkDims([]uint64{16}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	got := readBack(t, path, "/empty")
	shape, err := got.Shape()
	require.NoError(t, err)
	require.Equal(t, []uint64{0}, shape)
	values, err := got.Read()
	require.NoError(t, err)
	require.Empty(t, values)

	got = readBack(t, path, "/empty2d")
	shape, err = got.Shape()
	require.NoError(t, err)
	require.Equal(t, []uint64{0, 3}, shape)
	values, err = got.Read()
	require.NoError(t, err)
	require.Empty(t, values)

	got = readBack(t, path, "/records")
	shape, err = got.Shape()
	require.NoError(t, err)
	require.Equal(t, []uint64{0}, shape)
	records, err := got.ReadCompound()
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestCreateDatasetZeroRowsNeedsMaxDims(t *testing.T) {
	fw, err := CreateForWrite(filepath.Join(t.TempDir(), "fixed.h5"), CreateTruncate)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	cases := []struct {
		name string
		dims []uint64
		opts []DatasetOption
	}{
		{"contiguous", []uint64{0}, nil},
		{"chunked without maxdims", []uint64{0}, []DatasetOption{WithChunkDims([]uint64{4})}},
		{"maximum of zero", []uint64{0}, []DatasetOption{WithChunkDims([]uint64{4}), WithMaxDims([]uint64{0})}},
		{"other dimension fixed", []uint64{4, 0}, []DatasetOption{
			WithChunkDims([]uint64{4, 4}), WithMaxDims([]uint64{Unlimited, 0}),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fw.CreateDataset("/d", Float64, tc.dims, tc.opts...)
			require.Error(t, err)
			_, err = fw.CreateCompound("/c", zeroRowsCompoundType(t), tc.dims, tc.opts...)
			require.Error(t, err)
		})
	}
}

func TestResizeFromZeroRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grow.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/d", Float64, []uint64{0},
		WithChunkDims([]uint64{2}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, ds.Resize([]uint64{3}))
	require.NoError(t, ds.Write([]float64{1, 2, 3}))

	cds, err := fw.CreateCompound("/c", zeroRowsCompoundType(t), []uint64{0},
		WithChunkDims([]uint64{4}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, cds.Resize([]uint64{1}))
	require.NoError(t, cds.Write([]map[string]any{{"id": 7, "value": 1.5}}))
	require.NoError(t, fw.Close())

	got := readBack(t, path, "/d")
	values, err := got.Read()
	require.NoError(t, err)
	require.Equal(t, []float64{1, 2, 3}, values)

	got = readBack(t, path, "/c")
	records, err := got.ReadCompound()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.EqualValues(t, 7, records[0]["id"])
	require.InDelta(t, 1.5, records[0]["value"], 0)
}

func TestResizeToZeroRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shrink.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/d", Float64, []uint64{3},
		WithChunkDims([]uint64{2}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3}))
	require.NoError(t, ds.Resize([]uint64{0}))
	require.NoError(t, ds.Write([]float64{}))
	require.NoError(t, fw.Close())

	got := readBack(t, path, "/d")
	shape, err := got.Shape()
	require.NoError(t, err)
	require.Equal(t, []uint64{0}, shape)
	values, err := got.Read()
	require.NoError(t, err)
	require.Empty(t, values)
}

// Emptying a dataset frees its chunks and index, so the space serves the
// next allocation instead of staying in the file.
func TestResizeToZeroRowsFreesChunks(t *testing.T) {
	const n = 10000
	data := make([]float64, n)
	for i := range data {
		data[i] = float64(i)
	}
	build := func(path string, fillFirst bool) int64 {
		fw, err := CreateForWrite(path, CreateTruncate)
		require.NoError(t, err)
		first, err := fw.CreateDataset("/first", Float64, []uint64{0},
			WithChunkDims([]uint64{1000}), WithMaxDims([]uint64{Unlimited}))
		require.NoError(t, err)
		if fillFirst {
			require.NoError(t, first.Resize([]uint64{n}))
			require.NoError(t, first.Write(data))
			require.NoError(t, first.Resize([]uint64{0}))
			require.NoError(t, first.Write([]float64{}))
		}
		second, err := fw.CreateDataset("/second", Float64, []uint64{n},
			WithChunkDims([]uint64{1000}), WithMaxDims([]uint64{Unlimited}))
		require.NoError(t, err)
		require.NoError(t, second.Write(data))
		require.NoError(t, fw.Close())
		info, err := os.Stat(path)
		require.NoError(t, err)
		return info.Size()
	}
	dir := t.TempDir()
	never := build(filepath.Join(dir, "never.h5"), false)
	emptied := build(filepath.Join(dir, "emptied.h5"), true)
	t.Logf("never filled: %d bytes; filled then emptied: %d bytes", never, emptied)
	require.LessOrEqual(t, emptied, never+4096,
		"the emptied dataset's %d bytes of chunks stayed in the file", n*8)

	got := readBack(t, filepath.Join(dir, "emptied.h5"), "/second")
	values, err := got.Read()
	require.NoError(t, err)
	require.Equal(t, data, values)
}

func TestCInterop_ZeroRowsExtendible(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	path := filepath.Join(t.TempDir(), "zero.h5")
	fw, err := CreateForWrite(path, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateDataset("/empty", Float64, []uint64{0},
		WithChunkDims([]uint64{4}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	_, err = fw.CreateCompound("/records", zeroRowsCompoundType(t), []uint64{0},
		WithChunkDims([]uint64{16}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	shrunk, err := fw.CreateDataset("/shrunk", Int32, []uint64{3},
		WithChunkDims([]uint64{2}), WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, shrunk.Write([]int32{1, 2, 3}))
	require.NoError(t, shrunk.Resize([]uint64{0}))
	require.NoError(t, fw.Close())

	for _, name := range []string{"/empty", "/records", "/shrunk"} {
		out := runH5(t, h5dump, "--enable-error-stack", "-p", "-d", name, path)
		require.Contains(t, out, "DATASPACE  SIMPLE { ( 0 ) / ( H5S_UNLIMITED ) }", "%s:\n%s", name, out)
	}
}
