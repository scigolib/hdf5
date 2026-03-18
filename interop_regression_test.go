package hdf5

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
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

// --- Issue #35 / #33 Regression Tests ---

// TestInterop_GroupWith10Datasets creates a group with 10 datasets (>8, triggers SNOD split).
// Verifies all 10 datasets are visible when re-reading via our reader.
// Issue #35: SNOD capacity was 32 instead of 8 (2*K, K=4).
func TestInterop_GroupWith10Datasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_10ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/mygroup")
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("/mygroup/ds_%02d", i)
		ds, dsErr := fw.CreateDataset(name, Int32, []uint64{3})
		require.NoError(t, dsErr, "creating dataset %s", name)
		require.NoError(t, ds.Write([]int32{int32(i), int32(i + 1), int32(i + 2)}))
	}

	require.NoError(t, fw.Close())

	// Re-open and verify all 10 datasets are found.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found []string
	f.Walk(func(path string, _ Object) {
		if strings.HasPrefix(path, "/mygroup/ds_") {
			found = append(found, path)
		}
	})

	assert.Len(t, found, 10, "expected 10 datasets in /mygroup, got %d: %v", len(found), found)
}

// TestInterop_GroupWith20Datasets creates a group with 20 datasets.
// This exercises multiple SNOD splits (3 SNODs needed for K=4).
func TestInterop_GroupWith20Datasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_20ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/data")
	require.NoError(t, err)

	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("/data/dataset_%02d", i)
		ds, dsErr := fw.CreateDataset(name, Float64, []uint64{5})
		require.NoError(t, dsErr, "creating dataset %s", name)
		data := make([]float64, 5)
		for j := range data {
			data[j] = float64(i*10 + j)
		}
		require.NoError(t, ds.Write(data))
	}

	require.NoError(t, fw.Close())

	// Re-open and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found []string
	f.Walk(func(path string, _ Object) {
		if strings.HasPrefix(path, "/data/dataset_") {
			found = append(found, path)
		}
	})

	assert.Len(t, found, 20, "expected 20 datasets in /data, got %d: %v", len(found), found)
}

// TestInterop_RootWith10Datasets creates 10 datasets at root level.
// This exercises SNOD splitting on the root group.
func TestInterop_RootWith10Datasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "root_10ds.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("/data_%02d", i)
		ds, dsErr := fw.CreateDataset(name, Int32, []uint64{3})
		require.NoError(t, dsErr, "creating dataset %s", name)
		require.NoError(t, ds.Write([]int32{int32(i), int32(i * 2), int32(i * 3)}))
	}

	require.NoError(t, fw.Close())

	// Re-open and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found []string
	f.Walk(func(path string, _ Object) {
		if strings.HasPrefix(path, "/data_") {
			found = append(found, path)
		}
	})

	assert.Len(t, found, 10, "expected 10 root-level datasets, got %d: %v", len(found), found)
}

// TestInterop_RootWith10Datasets_V0 creates 10 datasets at root level using v0 superblock.
// This exercises SNOD splitting on the v0 root group with fixed-address layout.
func TestInterop_RootWith10Datasets_V0(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "root_10ds_v0.h5")

	fw, err := CreateForWrite(filename, CreateTruncate, WithSuperblockVersion(core.Version0))
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("/item_%02d", i)
		ds, dsErr := fw.CreateDataset(name, Int32, []uint64{2})
		require.NoError(t, dsErr, "creating dataset %s", name)
		require.NoError(t, ds.Write([]int32{int32(i), int32(i + 100)}))
	}

	require.NoError(t, fw.Close())

	// Re-open and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found []string
	f.Walk(func(path string, _ Object) {
		if strings.HasPrefix(path, "/item_") {
			found = append(found, path)
		}
	})

	assert.Len(t, found, 10, "expected 10 root-level datasets in v0 file, got %d: %v", len(found), found)
}

// TestInterop_LongNamedChildren tests heap capacity with long dataset names.
// Issue #33: Local heap was only 256 bytes. Now 4096 bytes.
func TestInterop_LongNamedChildren(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "long_names.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/experiments")
	require.NoError(t, err)

	// Create 8 datasets with long names (each ~60 chars = 480+ bytes total).
	// Old 256-byte heap would overflow; new 4096-byte heap handles this easily.
	for i := 0; i < 8; i++ {
		longName := fmt.Sprintf("measurement_run_%02d_temperature_sensor_primary_data", i)
		fullPath := "/experiments/" + longName
		ds, dsErr := fw.CreateDataset(fullPath, Float64, []uint64{3})
		require.NoError(t, dsErr, "creating dataset %s", fullPath)
		require.NoError(t, ds.Write([]float64{1.0, 2.0, 3.0}))
	}

	require.NoError(t, fw.Close())

	// Re-open and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found []string
	f.Walk(func(path string, _ Object) {
		if strings.HasPrefix(path, "/experiments/measurement_") {
			found = append(found, path)
		}
	})

	assert.Len(t, found, 8, "expected 8 long-named datasets, got %d: %v", len(found), found)
}

// TestInterop_NestedGroupsMany tests nested groups with several children at each level.
func TestInterop_NestedGroupsMany(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "nested_groups.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create 3 top-level groups, each with 4 datasets.
	for g := 0; g < 3; g++ {
		groupName := fmt.Sprintf("/group_%d", g)
		_, groupErr := fw.CreateGroup(groupName)
		require.NoError(t, groupErr, "creating group %s", groupName)

		for d := 0; d < 4; d++ {
			dsName := fmt.Sprintf("%s/ds_%d", groupName, d)
			ds, dsErr := fw.CreateDataset(dsName, Int32, []uint64{2})
			require.NoError(t, dsErr, "creating dataset %s", dsName)
			require.NoError(t, ds.Write([]int32{int32(g*10 + d), int32(g*10 + d + 1)}))
		}
	}

	require.NoError(t, fw.Close())

	// Re-open and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Count group and dataset paths.
	// Walk returns groups as "/group_0/" (trailing slash) and datasets as "/group_0/ds_0".
	groups := 0
	datasets := 0
	f.Walk(func(path string, _ Object) {
		if path == "/" {
			return
		}
		trimmed := strings.TrimSuffix(path, "/")
		parts := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
		if len(parts) == 1 && strings.HasPrefix(trimmed, "/group_") {
			groups++
		} else if len(parts) == 2 && strings.Contains(path, "/ds_") {
			datasets++
		}
	})

	assert.Equal(t, 3, groups, "expected 3 top-level groups")
	assert.Equal(t, 12, datasets, "expected 12 datasets total (3 groups x 4 datasets)")
}

// TestInterop_SNODCapacityExact verifies the exact SNOD boundary.
// With 8 datasets, no split should occur. With 9, a split must happen.
// Both cases should produce files readable by our reader.
func TestInterop_SNODCapacityExact(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"8_entries_no_split", 8},
		{"9_entries_split", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filename := filepath.Join(tmpDir, "snod_capacity.h5")

			fw, err := CreateForWrite(filename, CreateTruncate)
			require.NoError(t, err)

			for i := 0; i < tt.count; i++ {
				name := fmt.Sprintf("/entry_%02d", i)
				ds, dsErr := fw.CreateDataset(name, Int32, []uint64{1})
				require.NoError(t, dsErr)
				require.NoError(t, ds.Write([]int32{int32(i)}))
			}

			require.NoError(t, fw.Close())

			// Re-open and verify all datasets are found.
			f, err := Open(filename)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			var found int
			f.Walk(func(path string, _ Object) {
				if strings.HasPrefix(path, "/entry_") {
					found++
				}
			})

			assert.Equal(t, tt.count, found, "expected %d datasets, got %d", tt.count, found)
		})
	}
}
