package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenForWrite_CreateGroup_Issue50 reproduces the exact panic reported
// in https://github.com/scigolib/hdf5/issues/50:
//
//	fw, _ := hdf5.OpenForWrite(filename, hdf5.OpenReadWrite)
//	fw.CreateGroup("/example")  // panic: assignment to entry in nil map
func TestOpenForWrite_CreateGroup_Issue50(t *testing.T) {
	t.Parallel()

	for _, sbVersion := range []int{0, 2} {
		sbVersion := sbVersion
		t.Run(sbVersionName(sbVersion), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "issue50.h5")

			// Step 1: Create a file with one dataset.
			func() {
				opts := []interface{}{}
				if sbVersion == 0 {
					opts = append(opts, WithSuperblockVersion(SuperblockV0))
				}
				fw, err := CreateForWrite(path, CreateTruncate, opts...)
				require.NoError(t, err)
				ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]float64{1, 2, 3}))
				require.NoError(t, fw.Close())
			}()

			// Step 2: Reopen and create a new group — this panicked before the fix.
			func() {
				fw, err := OpenForWrite(path, OpenReadWrite)
				require.NoError(t, err)
				_, err = fw.CreateGroup("/example")
				require.NoError(t, err)
				require.NoError(t, fw.Close())
			}()

			// Step 3: Verify the group exists.
			f, err := Open(path)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			found := false
			f.Walk(func(p string, obj Object) {
				// Walk returns group paths with trailing slash.
				if p == "/example" || p == "/example/" {
					_, ok := obj.(*Group)
					require.True(t, ok, "expected Group, got %T", obj)
					found = true
				}
			})
			require.True(t, found, "group /example not found after OpenForWrite + CreateGroup")
		})
	}
}

// TestOpenForWrite_CreateDataset creates a new dataset in an existing file
// via OpenForWrite. This exercises the full linkToParent path including
// B-tree and local heap access on the root group.
func TestOpenForWrite_CreateDataset(t *testing.T) {
	t.Parallel()

	for _, sbVersion := range []int{0, 2} {
		sbVersion := sbVersion
		t.Run(sbVersionName(sbVersion), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "new_ds.h5")

			func() {
				opts := []interface{}{}
				if sbVersion == 0 {
					opts = append(opts, WithSuperblockVersion(SuperblockV0))
				}
				fw, err := CreateForWrite(path, CreateTruncate, opts...)
				require.NoError(t, err)
				ds, err := fw.CreateDataset("/original", Int32, []uint64{4})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{10, 20, 30, 40}))
				require.NoError(t, fw.Close())
			}()

			func() {
				fw, err := OpenForWrite(path, OpenReadWrite)
				require.NoError(t, err)
				ds, err := fw.CreateDataset("/added", Float64, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]float64{1.1, 2.2, 3.3}))
				require.NoError(t, fw.Close())
			}()

			f, err := Open(path)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			datasets := map[string]bool{}
			f.Walk(func(p string, obj Object) {
				if _, ok := obj.(*Dataset); ok {
					datasets[p] = true
				}
			})
			require.True(t, datasets["/original"], "original dataset missing")
			require.True(t, datasets["/added"], "added dataset missing")

			// Verify data of the new dataset.
			var addedDS *Dataset
			f.Walk(func(p string, obj Object) {
				if p == "/added" {
					addedDS = obj.(*Dataset)
				}
			})
			require.NotNil(t, addedDS)
			data, err := addedDS.Read()
			require.NoError(t, err)
			require.Equal(t, []float64{1.1, 2.2, 3.3}, data)
		})
	}
}

// TestOpenForWrite_VLenStringAttribute writes a variable-length string
// attribute via OpenForWrite. This exercises globalHeapWriter initialization
// which was nil before the fix.
func TestOpenForWrite_VLenStringAttribute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "vlen_attr.h5")

	func() {
		fw, err := CreateForWrite(path, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/sensor", Float64, []uint64{5})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))
		require.NoError(t, fw.Close())
	}()

	func() {
		fw, err := OpenForWrite(path, OpenReadWrite)
		require.NoError(t, err)
		ds, err := fw.OpenDataset("/sensor")
		require.NoError(t, err)
		require.NoError(t, ds.WriteAttribute("description", "Temperature sensor"))
		require.NoError(t, ds.WriteAttribute("unit", "Celsius"))
		require.NoError(t, fw.Close())
	}()

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var ds *Dataset
	f.Walk(func(p string, obj Object) {
		if p == "/sensor" {
			ds = obj.(*Dataset)
		}
	})
	require.NotNil(t, ds)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 2)
}

// TestOpenForWrite_NestedGroup creates a nested group hierarchy via
// OpenForWrite: /level1/level2/data.
func TestOpenForWrite_NestedGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested.h5")

	func() {
		fw, err := CreateForWrite(path, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/existing", Float64, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2}))
		require.NoError(t, fw.Close())
	}()

	func() {
		fw, err := OpenForWrite(path, OpenReadWrite)
		require.NoError(t, err)
		_, err = fw.CreateGroup("/level1")
		require.NoError(t, err)
		_, err = fw.CreateGroup("/level1/level2")
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/level1/level2/data", Int32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{100, 200, 300}))
		require.NoError(t, fw.Close())
	}()

	f, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	paths := map[string]bool{}
	f.Walk(func(p string, _ Object) {
		paths[p] = true
	})
	require.True(t, paths["/existing"], "/existing missing")
	// Walk returns group paths with trailing slash.
	require.True(t, paths["/level1"] || paths["/level1/"], "/level1 missing")
	require.True(t, paths["/level1/level2"] || paths["/level1/level2/"], "/level1/level2 missing")
	require.True(t, paths["/level1/level2/data"], "/level1/level2/data missing")
}

// TestOpenForWrite_RootHeaderAllocSize verifies that OpenForWrite correctly
// computes rootHeaderAllocSz from the existing object header, preventing
// unbounded in-place rewrites.
func TestOpenForWrite_RootHeaderAllocSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "alloc_sz.h5")

	func() {
		fw, err := CreateForWrite(path, CreateTruncate)
		require.NoError(t, err)
		ds, err := fw.CreateDataset("/x", Float64, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{42}))
		require.NoError(t, fw.Close())
	}()

	fw, err := OpenForWrite(path, OpenReadWrite)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	allocSz := fw.lookupHeaderAllocSize(fw.rootGroupAddr)
	require.NotZero(t, allocSz, "rootHeaderAllocSz should be computed from existing object header")
}

func sbVersionName(v int) string {
	switch v {
	case 0:
		return "superblock_v0"
	case 2:
		return "superblock_v2"
	default:
		return "unknown"
	}
}
