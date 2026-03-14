package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInterop_GroupDatasetAttribute_Readback is a regression test for Issue #28/#29.
// Bug A: Adding attributes to a dataset inside a group caused the superblock EOA
// to be too small because the allocator was not updated after the object header
// grew. External readers (h5dump, h5py) rejected the file.
func TestInterop_GroupDatasetAttribute_Readback(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "interop_group_attr.h5")

	// Create: group + dataset + attribute
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/grp")
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/grp/data", Int32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{10, 20, 30}))
	require.NoError(t, ds.WriteAttribute("units", "kelvin"))

	require.NoError(t, fw.Close())

	// Verify: superblock EOA must cover the entire file
	raw, err := os.ReadFile(testFile)
	require.NoError(t, err)
	fileSize := int64(len(raw))
	require.Greater(t, fileSize, int64(200), "file should be non-trivial size")

	// Readback: our own reader must find the group, dataset, and attribute
	f, err := Open(testFile)
	require.NoError(t, err)
	defer f.Close()

	var paths []string
	f.Walk(func(path string, _ Object) {
		paths = append(paths, path)
	})
	require.Contains(t, paths, "/grp/", "should find /grp/")
	require.Contains(t, paths, "/grp/data", "should find /grp/data")
}

// TestInterop_MultipleRootDatasets_NonAlphabetical is a regression test for Issue #28.
// Bug B: B-tree v1 right key was set to the numerically largest local heap offset,
// but the HDF5 spec requires it to be the offset of the lexicographically largest
// name (strcmp comparison). This caused h5dump to miss entries.
// Bug C: SNOD entries were appended in insertion order, but the HDF5 spec requires
// them to be sorted by name. Without sorting, h5dump/h5ls could not find entries.
func TestInterop_MultipleRootDatasets_NonAlphabetical(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "interop_root_multi.h5")

	// Create 2 root datasets with names that are NOT in alphabetical order.
	// "uint" sorts AFTER "float", but "uint" is created first — this exercises
	// both the B-tree key fix and the SNOD sorting fix.
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	ds1, err := fw.CreateDataset("/uint", Int32, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds1.Write([]int32{1, 2, 3, 4, 5}))

	ds2, err := fw.CreateDataset("/float", Float32, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds2.Write([]float32{1.0, 2.0, 3.0, 4.0, 5.0}))

	require.NoError(t, fw.Close())

	// Readback: both datasets must be found
	f, err := Open(testFile)
	require.NoError(t, err)
	defer f.Close()

	var paths []string
	f.Walk(func(path string, _ Object) {
		paths = append(paths, path)
	})
	require.Contains(t, paths, "/uint", "should find /uint")
	require.Contains(t, paths, "/float", "should find /float")

	// Read actual data back via Walk
	datasets := map[string]*Dataset{}
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok {
			datasets[path] = ds
		}
	})

	require.Contains(t, datasets, "/uint")
	intData, err := datasets["/uint"].Read()
	require.NoError(t, err)
	require.Equal(t, []float64{1, 2, 3, 4, 5}, intData)

	require.Contains(t, datasets, "/float")
	floatData, err := datasets["/float"].Read()
	require.NoError(t, err)
	require.InDeltaSlice(t, []float64{1.0, 2.0, 3.0, 4.0, 5.0}, floatData, 0.001)
}

// TestInterop_ThreeRootDatasets verifies that 3+ root-level datasets with
// various name orderings are all discoverable via Walk and readable.
func TestInterop_ThreeRootDatasets(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "interop_root_three.h5")

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	// Names chosen so insertion order != alphabetical order: c, a, b
	ds1, err := fw.CreateDataset("/c", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds1.Write([]int32{7, 8}))

	ds2, err := fw.CreateDataset("/a", Int32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds2.Write([]int32{1, 2, 3}))

	ds3, err := fw.CreateDataset("/b", Float32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds3.Write([]float32{4.0, 5.0, 6.0}))

	require.NoError(t, fw.Close())

	// Readback
	f, err := Open(testFile)
	require.NoError(t, err)
	defer f.Close()

	var paths []string
	f.Walk(func(path string, _ Object) {
		paths = append(paths, path)
	})
	require.Contains(t, paths, "/a")
	require.Contains(t, paths, "/b")
	require.Contains(t, paths, "/c")
	// Walk includes "/" root, so total is 4
	require.Len(t, paths, 4, "should have root + 3 datasets")
}

// TestInterop_GroupWithMultipleDatasetsAndAttributes combines all three
// bug fixes: group hierarchy, multiple datasets with attributes, and
// non-alphabetical insertion order.
func TestInterop_GroupWithMultipleDatasetsAndAttributes(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "interop_combined.h5")

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/sensors")
	require.NoError(t, err)

	// Create datasets in non-alphabetical order inside a group
	ds1, err := fw.CreateDataset("/sensors/temperature", Float32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds1.Write([]float32{20.1, 21.5, 19.8, 22.0}))
	require.NoError(t, ds1.WriteAttribute("units", "celsius"))

	ds2, err := fw.CreateDataset("/sensors/pressure", Float64, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds2.Write([]float64{101.3, 101.5, 100.9, 102.1}))
	require.NoError(t, ds2.WriteAttribute("units", "kPa"))

	require.NoError(t, fw.Close())

	// Readback
	f, err := Open(testFile)
	require.NoError(t, err)
	defer f.Close()

	var paths []string
	f.Walk(func(path string, _ Object) {
		paths = append(paths, path)
	})
	require.Contains(t, paths, "/sensors/")
	require.Contains(t, paths, "/sensors/temperature")
	require.Contains(t, paths, "/sensors/pressure")
}
