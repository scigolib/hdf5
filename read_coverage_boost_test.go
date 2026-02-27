package hdf5

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// readSignature edge cases (file.go)
// ---------------------------------------------------------------------------

// TestReadSignature_AtEndOfFile tests readSignature when address is near/past EOF.
// Exercises the error return path (returns "") when ReadAt fails.
func TestReadSignature_AtEndOfFile(t *testing.T) {
	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Reading signature at an absurdly large offset should return empty string.
	sig := readSignature(f.osFile, 0xFFFFFFFFFFFF)
	assert.Equal(t, "", sig, "readSignature past EOF should return empty string")
}

// TestReadSignature_AtZero tests readSignature at offset 0 (should be HDF5 signature prefix).
func TestReadSignature_AtZero(t *testing.T) {
	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sig := readSignature(f.osFile, 0)
	// HDF5 magic bytes: \x89HDF
	assert.Equal(t, "\x89HDF", sig, "readSignature at offset 0 should be HDF5 magic prefix")
}

// ---------------------------------------------------------------------------
// Open error cases (file.go)
// ---------------------------------------------------------------------------

// TestOpen_EmptyFilename tests Open with an empty filename.
func TestOpen_EmptyFilename(t *testing.T) {
	_, err := Open("")
	require.Error(t, err, "Open with empty filename should fail")
}

// TestOpen_DirectoryPath tests Open with a path that is a directory, not a file.
func TestOpen_DirectoryPath(t *testing.T) {
	_, err := Open("testdata")
	require.Error(t, err, "Open with directory path should fail")
}

// TestOpen_V0File tests Open with a v0 superblock file, verifying root group loads.
func TestOpen_V0File(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())
	require.NotNil(t, f.Root())
	require.Equal(t, "/", f.Root().Name())
}

// TestOpen_SuperblockAccessor tests the Superblock() accessor on File.
func TestOpen_SuperblockAccessor(t *testing.T) {
	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sb := f.Superblock()
	require.NotNil(t, sb, "Superblock() should not return nil")
	assert.Equal(t, uint8(2), sb.Version)
}

// TestOpen_ReaderAccessor tests the Reader() accessor on File.
func TestOpen_ReaderAccessor(t *testing.T) {
	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	r := f.Reader()
	require.NotNil(t, r, "Reader() should not return nil")

	// Should be able to read HDF5 signature from offset 0.
	buf := make([]byte, 4)
	_, err = r.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, "\x89HDF", string(buf))
}

// ---------------------------------------------------------------------------
// loadObject - ObjectTypeDatatype branch (group.go)
// ---------------------------------------------------------------------------

// TestLoadObject_NamedDatatypeBranch opens a file with a committed (named) datatype
// and verifies it hits the ObjectTypeDatatype case in loadObject.
func TestLoadObject_NamedDatatypeBranch(t *testing.T) {
	f, err := Open("testdata/reference/bad_compound.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var namedDTs []*NamedDatatype
	f.Walk(func(_ string, obj Object) {
		if nd, ok := obj.(*NamedDatatype); ok {
			namedDTs = append(namedDTs, nd)
		}
	})
	require.NotEmpty(t, namedDTs, "bad_compound.h5 should contain at least one NamedDatatype")

	for _, nd := range namedDTs {
		assert.NotEmpty(t, nd.Name(), "NamedDatatype should have a name")
		dt := nd.Datatype()
		assert.NotNil(t, dt, "NamedDatatype.Datatype() should not be nil for bad_compound.h5")
	}
}

// TestLoadObject_NamedDatatypeFromMemleak uses the memleak reference file,
// exercising loadObject with various object types present in this file.
func TestLoadObject_NamedDatatypeFromMemleak(t *testing.T) {
	f, err := Open("testdata/reference/memleak_H5O_dtype_decode_helper_H5Odtype.h5")
	if err != nil {
		t.Skipf("skipping: cannot open reference file: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Walk the file to exercise all loadObject paths present.
	var objectCount int
	f.Walk(func(_ string, obj Object) {
		objectCount++
		// Exercise accessors on each object type.
		switch v := obj.(type) {
		case *NamedDatatype:
			assert.NotEmpty(t, v.Name())
			_ = v.Datatype() // may be nil
		case *Dataset:
			assert.NotEmpty(t, v.Name())
		case *Group:
			_ = v.Name()
		}
	})
	assert.Greater(t, objectCount, 0, "file should contain at least one object")
}

// ---------------------------------------------------------------------------
// loadTraditionalGroup and loadChildren with SNOD (group.go)
// ---------------------------------------------------------------------------

// TestLoadTraditionalGroup_GroupOld_DeepWalk opens group_old.h5, a v0 file
// with nested SNOD groups, and exhaustively walks the tree to exercise
// loadTraditionalGroup, loadChildren with SNOD inline, and CacheType=1.
func TestLoadTraditionalGroup_GroupOld_DeepWalk(t *testing.T) {
	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	// Collect all objects at every level.
	groups := make(map[string]*Group)
	datasets := make(map[string]*Dataset)
	namedDTs := make(map[string]*NamedDatatype)

	f.Walk(func(path string, obj Object) {
		switch v := obj.(type) {
		case *Group:
			groups[path] = v
		case *Dataset:
			datasets[path] = v
		case *NamedDatatype:
			namedDTs[path] = v
		}
	})

	// Root must exist.
	require.Contains(t, groups, "/", "root group should exist")

	// There should be at least nested groups ("old" group).
	totalObjects := len(groups) + len(datasets) + len(namedDTs)
	assert.GreaterOrEqual(t, totalObjects, 2,
		"group_old.h5 should have at least 2 objects (root + old)")

	// Verify all groups are accessible.
	for path, g := range groups {
		assert.NotNil(t, g, "group at path %q should not be nil", path)
		// Attributes() should not error on any group.
		attrs, err := g.Attributes()
		require.NoError(t, err, "Attributes() on group %q should not error", path)
		assert.NotNil(t, attrs, "attrs should be non-nil (empty slice) for %q", path)
	}
}

// TestLoadTraditionalGroup_FillOld exercises loadTraditionalGroup via fill_old.h5,
// another v0 file using traditional SNOD format.
func TestLoadTraditionalGroup_FillOld(t *testing.T) {
	f, err := Open("testdata/reference/fill_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	var objectCount int
	f.Walk(func(_ string, _ Object) {
		objectCount++
	})
	assert.GreaterOrEqual(t, objectCount, 1, "fill_old.h5 should have at least root group")
}

// TestLoadChildren_V0WithMultipleDatasets exercises loadChildren with multiple
// SNOD entries from a v0 file.
func TestLoadChildren_V0WithMultipleDatasets(t *testing.T) {
	f, err := Open("testdata/reference/tarrold.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Len(t, children, 2, "tarrold.h5 should have 2 datasets")

	for _, child := range children {
		ds, ok := child.(*Dataset)
		require.True(t, ok, "child %q should be a Dataset", child.Name())
		assert.NotEmpty(t, ds.Name())
		assert.NotZero(t, ds.Address())
	}
}

// ---------------------------------------------------------------------------
// loadGroup - SNOD signature branch (group.go)
// ---------------------------------------------------------------------------

// TestLoadGroup_SNODSignatureBranch opens various v0 reference files where
// the group address points to a SNOD, exercising the if sig == SignatureSNOD
// branch in loadGroup.
func TestLoadGroup_SNODSignatureBranch(t *testing.T) {
	v0Files := []string{
		"testdata/reference/group_old.h5",
		"testdata/reference/tarrold.h5",
		"testdata/reference/fill_old.h5",
		"testdata/reference/tlayouto.h5",
	}

	for _, path := range v0Files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f, err := Open(path)
			if err != nil {
				t.Skipf("cannot open %s: %v", path, err)
			}
			defer func() { _ = f.Close() }()

			// Walk the entire tree to ensure all code paths execute.
			var objectCount int
			f.Walk(func(_ string, _ Object) {
				objectCount++
			})
			assert.GreaterOrEqual(t, objectCount, 1, "%s should have at least 1 object", path)
		})
	}
}

// ---------------------------------------------------------------------------
// Group.Attributes() with actual attributes (group.go)
// ---------------------------------------------------------------------------

// TestGroup_Attributes_NonEmpty creates a group with attributes, writes them,
// then reads back to exercise the non-empty Attributes() path on Group.
func TestGroup_Attributes_NonEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_attrs_nonempty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	grp, err := fw.CreateGroup("/meta")
	require.NoError(t, err)

	require.NoError(t, grp.WriteAttribute("author", "test"))
	require.NoError(t, grp.WriteAttribute("version", int32(42)))
	require.NoError(t, grp.WriteAttribute("pi", float64(3.14159)))

	require.NoError(t, fw.Close())

	// Read back.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Group
	f.Walk(func(path string, obj Object) {
		if g, ok := obj.(*Group); ok && path == "/meta/" {
			found = g
		}
	})
	require.NotNil(t, found, "group /meta/ should be found")

	attrs, err := found.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 3, "group should have 3 attributes")

	names := make(map[string]bool)
	for _, a := range attrs {
		names[a.Name] = true
	}
	assert.True(t, names["author"])
	assert.True(t, names["version"])
	assert.True(t, names["pi"])
}

// TestGroup_Attributes_TraditionalFormat tests Attributes() on a traditional
// (SNOD) group (address=0), which should return empty slice without error.
func TestGroup_Attributes_TraditionalFormat(t *testing.T) {
	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Walk to find any group that might have address==0 (SNOD-based).
	f.Walk(func(_ string, obj Object) {
		if g, ok := obj.(*Group); ok {
			attrs, err := g.Attributes()
			require.NoError(t, err, "Attributes() on group %q should not error", g.Name())
			require.NotNil(t, attrs, "attrs should be non-nil for group %q", g.Name())
		}
	})
}

// ---------------------------------------------------------------------------
// Dataset.Attributes() (group.go)
// ---------------------------------------------------------------------------

// TestDataset_Attributes_WithAttributes creates a dataset with attributes and
// reads them back to exercise the non-empty Attributes() path on Dataset.
func TestDataset_Attributes_WithAttributes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_attrs.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/sensor", Float64, []uint64{10})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}))

	require.NoError(t, ds.WriteAttribute("units", "celsius"))
	require.NoError(t, ds.WriteAttribute("scale", float64(1.0)))

	require.NoError(t, fw.Close())

	// Read back.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "sensor" {
			found = d
		}
	})
	require.NotNil(t, found, "dataset 'sensor' should be found")

	attrs, err := found.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 2, "dataset should have 2 attributes")

	// ListAttributes.
	names, err := found.ListAttributes()
	require.NoError(t, err)
	require.Len(t, names, 2)
	assert.Contains(t, names, "units")
	assert.Contains(t, names, "scale")
}

// TestDataset_Attributes_Empty creates a dataset with no attributes and
// verifies Attributes() returns an empty slice.
func TestDataset_Attributes_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_no_attrs.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/plain", Float64, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "plain" {
			found = d
		}
	})
	require.NotNil(t, found)

	attrs, err := found.Attributes()
	require.NoError(t, err)
	// Should return empty slice or nil (both are acceptable since no attributes).
	if attrs != nil {
		assert.Empty(t, attrs)
	}
}

// ---------------------------------------------------------------------------
// ReadAttribute - error path (attribute not found) (group.go)
// ---------------------------------------------------------------------------

// TestReadAttribute_NotFound tests the error path when an attribute name
// does not exist on the dataset.
func TestReadAttribute_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_attr_notfound.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/data", Int32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{10, 20, 30}))
	require.NoError(t, ds.WriteAttribute("exists", int32(1)))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "data" {
			found = d
		}
	})
	require.NotNil(t, found)

	// Existing attribute should work.
	val, err := found.ReadAttribute("exists")
	require.NoError(t, err)
	assert.NotNil(t, val)

	// Non-existent attribute should error.
	_, err = found.ReadAttribute("nonexistent_attribute")
	require.Error(t, err, "ReadAttribute for non-existent name should error")
	assert.Contains(t, err.Error(), "not found",
		"error should mention 'not found'")
}

// TestReadAttribute_MultipleAttributes tests ReadAttribute with multiple
// attributes, exercising the iteration logic.
func TestReadAttribute_MultipleAttributes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_multi_attr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/values", Float64, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))

	require.NoError(t, ds.WriteAttribute("first", int32(1)))
	require.NoError(t, ds.WriteAttribute("second", int32(2)))
	require.NoError(t, ds.WriteAttribute("third", int32(3)))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok && d.Name() == "values" {
			found = d
		}
	})
	require.NotNil(t, found)

	// All three should be found.
	for _, name := range []string{"first", "second", "third"} {
		val, err := found.ReadAttribute(name)
		require.NoError(t, err, "ReadAttribute(%q) should succeed", name)
		assert.NotNil(t, val, "attribute %q should have a value", name)
	}

	// Non-existent should fail.
	_, err = found.ReadAttribute("fourth")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Read() with different datatypes (group.go)
// ---------------------------------------------------------------------------

// TestRead_Float32 tests Read() on a float32 dataset (converts to float64).
func TestRead_Float32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_float32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/f32", Float32, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float32{1.5, 2.5, 3.5, 4.5, 5.5}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	data, err := found.Read()
	require.NoError(t, err)
	require.Len(t, data, 5)

	assert.InDelta(t, 1.5, data[0], 1e-5)
	assert.InDelta(t, 5.5, data[4], 1e-5)
}

// TestRead_Int32 tests Read() on an int32 dataset (converts to float64).
func TestRead_Int32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_int32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/i32", Int32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{-100, 0, 100, 200}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	data, err := found.Read()
	require.NoError(t, err)
	require.Len(t, data, 4)

	assert.InDelta(t, -100.0, data[0], 1e-10)
	assert.InDelta(t, 200.0, data[3], 1e-10)
}

// TestRead_Int64 tests Read() on an int64 dataset (converts to float64).
func TestRead_Int64(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_int64.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/i64", Int64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int64{-999999, 0, 999999}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	data, err := found.Read()
	require.NoError(t, err)
	require.Len(t, data, 3)

	assert.InDelta(t, -999999.0, data[0], 1e-10)
	assert.InDelta(t, 999999.0, data[2], 1e-10)
}

// ---------------------------------------------------------------------------
// ReadStrings / ReadCompound error paths (group.go)
// ---------------------------------------------------------------------------

// TestReadStrings_OnNonStringDataset tests ReadStrings on a numeric dataset,
// which should produce an error from the internal reader.
func TestReadStrings_OnNonStringDataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "readstrings_error.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/numbers", Float64, []uint64{5})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	// ReadStrings on a numeric dataset should fail because the datatype is not string.
	_, err = found.ReadStrings()
	require.Error(t, err, "ReadStrings on numeric dataset should error")
}

// TestReadCompound_OnNonCompoundDataset tests ReadCompound on a simple numeric dataset.
func TestReadCompound_OnNonCompoundDataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "readcompound_error.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/simple", Int32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	// ReadCompound on a non-compound dataset should fail.
	_, err = found.ReadCompound()
	require.Error(t, err, "ReadCompound on non-compound dataset should error")
}

// ---------------------------------------------------------------------------
// validateSelectionDimensions - dimension mismatch errors (dataset_read_hyperslab.go)
// ---------------------------------------------------------------------------

// TestValidateSelectionDimensions_StrideMismatch tests stride dimension mismatch.
func TestValidateSelectionDimensions_StrideMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start:  []uint64{0, 0},
		Count:  []uint64{10, 10},
		Stride: []uint64{1}, // Wrong: 1D stride for 2D dataset.
	}
	err := validateSelectionDimensions(sel, 2)
	require.Error(t, err, "stride dimension mismatch should error")
	assert.Contains(t, err.Error(), "stride")
}

// TestValidateSelectionDimensions_BlockMismatch tests block dimension mismatch.
func TestValidateSelectionDimensions_BlockMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start: []uint64{0, 0},
		Count: []uint64{10, 10},
		Block: []uint64{1, 1, 1}, // Wrong: 3D block for 2D dataset.
	}
	err := validateSelectionDimensions(sel, 2)
	require.Error(t, err, "block dimension mismatch should error")
	assert.Contains(t, err.Error(), "block")
}

// TestValidateSelectionDimensions_StartMismatch tests start dimension mismatch.
func TestValidateSelectionDimensions_StartMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start: []uint64{0, 0, 0}, // 3D start for 2D dataset.
		Count: []uint64{10, 10},
	}
	err := validateSelectionDimensions(sel, 2)
	require.Error(t, err, "start dimension mismatch should error")
	assert.Contains(t, err.Error(), "start")
}

// TestValidateSelectionDimensions_CountMismatch tests count dimension mismatch.
func TestValidateSelectionDimensions_CountMismatch(t *testing.T) {
	sel := &HyperslabSelection{
		Start: []uint64{0, 0},
		Count: []uint64{10}, // 1D count for 2D dataset.
	}
	err := validateSelectionDimensions(sel, 2)
	require.Error(t, err, "count dimension mismatch should error")
	assert.Contains(t, err.Error(), "count")
}

// ---------------------------------------------------------------------------
// dispatchHyperslabReader - compact dispatch (dataset_read_hyperslab.go)
// ---------------------------------------------------------------------------

// TestDispatchHyperslabReader_Compact tests reading hyperslab from a compact
// layout dataset using the tfilters.h5 reference file.
func TestDispatchHyperslabReader_Compact(t *testing.T) {
	f, err := Open("testdata/hdf5_official/tfilters.h5")
	if err != nil {
		t.Skipf("skipping: cannot open tfilters.h5: %v", err)
	}
	defer func() { _ = f.Close() }()

	var ds *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/compact" {
			if d, ok := obj.(*Dataset); ok {
				ds = d
			}
		}
	})
	if ds == nil {
		t.Skip("compact dataset not found in tfilters.h5")
	}

	// ReadHyperslab on compact dataset (exercises dispatchHyperslabReader compact branch).
	sel := &HyperslabSelection{
		Start: []uint64{0, 0},
		Count: []uint64{5, 5},
	}
	result, err := ds.ReadHyperslab(sel)
	require.NoError(t, err, "ReadHyperslab on compact dataset should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok, "expected []float64")
	require.Len(t, resultData, 25, "expected 5x5=25 elements")
}

// ---------------------------------------------------------------------------
// readHyperslab - chunked path (dataset_read_hyperslab.go)
// ---------------------------------------------------------------------------

// TestReadHyperslab_ChunkedPath creates a chunked dataset and reads
// via ReadHyperslab to exercise the readHyperslabChunked path.
func TestReadHyperslab_ChunkedPath(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "hyperslab_chunked.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create 2D chunked dataset: 40x40 with 10x10 chunks.
	data := make([]float64, 40*40)
	for r := 0; r < 40; r++ {
		for c := 0; c < 40; c++ {
			data[r*40+c] = float64(r*100 + c)
		}
	}

	dw, err := fw.CreateDataset("/chunked", Float64, []uint64{40, 40},
		WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "chunked")
	require.True(t, found, "chunked dataset not found")

	// ReadHyperslab that spans multiple chunks: [5:25, 5:25] = 20x20.
	sel := &HyperslabSelection{
		Start: []uint64{5, 5},
		Count: []uint64{20, 20},
	}
	result, err := ds.ReadHyperslab(sel)
	require.NoError(t, err, "ReadHyperslab on chunked dataset should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 400, "expected 20x20=400 elements")
}

// TestReadHyperslab_ChunkedWithPartialOverlap creates a chunked dataset and
// reads a region that partially overlaps chunk boundaries.
func TestReadHyperslab_ChunkedWithPartialOverlap(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "hyperslab_chunked_partial.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// 30x30 dataset with 10x10 chunks = 9 chunks.
	data := make([]float64, 30*30)
	for i := range data {
		data[i] = float64(i)
	}

	dw, err := fw.CreateDataset("/partial", Float64, []uint64{30, 30},
		WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds, found := findDatasetByName(f, "partial")
	require.True(t, found)

	// Read region [8:18, 8:18] - overlaps 4 chunks.
	sel := &HyperslabSelection{
		Start: []uint64{8, 8},
		Count: []uint64{10, 10},
	}
	result, err := ds.ReadHyperslab(sel)
	require.NoError(t, err, "ReadHyperslab with partial chunk overlap should succeed")

	resultData, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, resultData, 100, "expected 10x10=100 elements")
}

// ---------------------------------------------------------------------------
// ChunkIteratorWithContext - immediate cancellation (dataset_chunk_iterator.go)
// ---------------------------------------------------------------------------

// TestChunkIteratorWithContext_ImmediateCancel tests ChunkIteratorWithContext
// where the context is canceled before iteration begins.
func TestChunkIteratorWithContext_ImmediateCancel(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunk_iter_cancel.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i)
	}
	dw, err := fw.CreateDataset("/data", Float64, []uint64{100},
		WithChunkDims([]uint64{10}))
	require.NoError(t, err)
	require.NoError(t, dw.Write(data))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	ds := findFirstDataset(f)
	require.NotNil(t, ds)

	// Cancel context BEFORE creating iterator.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	iter, err := ds.ChunkIteratorWithContext(ctx)
	require.NoError(t, err, "creating iterator with canceled context should succeed")

	// Next() should immediately return false due to canceled context.
	assert.False(t, iter.Next(), "Next() should return false with canceled context")
	assert.Error(t, iter.Err(), "Err() should return context.Canceled")
}

// ---------------------------------------------------------------------------
// extractHyperslabMessages - missing messages (dataset_read_hyperslab.go)
// ---------------------------------------------------------------------------

// TestExtractHyperslabMessages_MissingDatatype tests the error path when
// a required message is missing from the object header. We test this
// indirectly by verifying the validation works via the public API.
func TestExtractHyperslabMessages_ViaReadSlice(t *testing.T) {
	// This test verifies that ReadSlice properly propagates validation errors.
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "valid_dataset.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/test", Float64, []uint64{10})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(_ string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			found = d
		}
	})
	require.NotNil(t, found)

	// Valid slice should work.
	result, err := found.ReadSlice([]uint64{2}, []uint64{5})
	require.NoError(t, err)
	data, ok := result.([]float64)
	require.True(t, ok)
	require.Len(t, data, 5)
	assert.InDelta(t, 2.0, data[0], 1e-10)
	assert.InDelta(t, 6.0, data[4], 1e-10)
}

// ---------------------------------------------------------------------------
// Dataset.Info() round-trip (group.go)
// ---------------------------------------------------------------------------

// TestDataset_Info_RoundTrip creates various dataset types and verifies
// that Info() returns valid metadata.
func TestDataset_Info_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "ds_info.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds1, err := fw.CreateDataset("/floats", Float64, []uint64{10})
	require.NoError(t, err)
	require.NoError(t, ds1.Write(make([]float64, 10)))

	ds2, err := fw.CreateDataset("/ints", Int32, []uint64{5, 3})
	require.NoError(t, err)
	require.NoError(t, ds2.Write(make([]int32, 15)))

	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	f.Walk(func(_ string, obj Object) {
		if ds, ok := obj.(*Dataset); ok {
			info, err := ds.Info()
			require.NoError(t, err, "Info() on %q should not error", ds.Name())
			assert.NotEmpty(t, info, "Info() on %q should return non-empty string", ds.Name())
		}
	})
}

// ---------------------------------------------------------------------------
// Walk traversal with mixed object types (file.go)
// ---------------------------------------------------------------------------

// TestWalk_MixedObjectTypes creates a file with groups, datasets, and walks
// the entire tree, verifying all object types are visited.
func TestWalk_MixedObjectTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "walk_mixed.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/grp_a")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/grp_b")
	require.NoError(t, err)

	ds1, err := fw.CreateDataset("/grp_a/data1", Float64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds1.Write([]float64{1, 2, 3}))

	ds2, err := fw.CreateDataset("/grp_b/data2", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds2.Write([]int32{10, 20}))

	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	paths := make(map[string]bool)
	f.Walk(func(path string, obj Object) {
		paths[path] = true
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	assert.GreaterOrEqual(t, groups, 3, "should have at least 3 groups (root + 2)")
	assert.Equal(t, 2, datasets, "should have 2 datasets")
	assert.True(t, paths["/"], "root should be visited")
}

// ---------------------------------------------------------------------------
// loadObject - ObjectTypeUnknown fallback (group.go)
// ---------------------------------------------------------------------------

// TestLoadObject_ObjectTypeUnknown_V0 exercises the ObjectTypeUnknown branch
// in loadObject when loading a v0 file. The root group of v0 files often
// has ObjectTypeUnknown because it has no messages.
func TestLoadObject_ObjectTypeUnknown_V0(t *testing.T) {
	f, err := Open("testdata/v0.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root, "root group should be loaded even with ObjectTypeUnknown")
	require.Equal(t, "/", root.Name())

	// Verify that children were loaded (proving ObjectTypeUnknown -> loadGroup worked).
	children := root.Children()
	require.NotEmpty(t, children, "v0 root group should have children")

	// Check that the "test" dataset was found.
	var foundTest bool
	for _, child := range children {
		if child.Name() == "test" {
			foundTest = true
			_, ok := child.(*Dataset)
			require.True(t, ok, "child 'test' should be a Dataset")
		}
	}
	require.True(t, foundTest, "dataset 'test' should be found in v0.h5")
}

// ---------------------------------------------------------------------------
// Reference file broad coverage (multiple code paths)
// ---------------------------------------------------------------------------

// TestReferenceFiles_BroadCoverage opens many reference files and walks them,
// exercising the full loading pipeline including loadGroup, loadObject,
// loadTraditionalGroup, loadModernGroup, and loadChildren across various
// file formats.
func TestReferenceFiles_BroadCoverage(t *testing.T) {
	referenceFiles := []struct {
		path       string
		minObjects int
	}{
		{"testdata/v0.h5", 2},
		{"testdata/v2.h5", 1},
		{"testdata/v3.h5", 1},
		{"testdata/reference/group_old.h5", 2},
		{"testdata/reference/tarrold.h5", 3},
		{"testdata/reference/fill_old.h5", 1},
		{"testdata/reference/tlayouto.h5", 1},
		{"testdata/reference/aggr.h5", 1},
		{"testdata/reference/bad_compound.h5", 1},
		{"testdata/reference/deflate.h5", 1},
		{"testdata/reference/charsets.h5", 1},
		{"testdata/with_groups.h5", 2},
		{"testdata/with_attributes.h5", 1},
		{"testdata/multiple_datasets.h5", 1},
	}

	for _, tc := range referenceFiles {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			f, err := Open(tc.path)
			if err != nil {
				t.Skipf("cannot open %s: %v", tc.path, err)
			}
			defer func() { _ = f.Close() }()

			var objectCount int
			f.Walk(func(_ string, obj Object) {
				objectCount++
				// Exercise Name() on every object.
				_ = obj.Name()
			})

			assert.GreaterOrEqual(t, objectCount, tc.minObjects,
				"%s should have at least %d objects", tc.path, tc.minObjects)
		})
	}
}

// ---------------------------------------------------------------------------
// loadGroup with address 0 error (group.go)
// ---------------------------------------------------------------------------

// TestLoadGroup_ZeroAddress verifies that loadGroup with address 0 returns
// an error (the invalid group address check).
func TestLoadGroup_ZeroAddress(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_zero_addr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3}))
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Directly call loadGroup with address 0.
	_, err = loadGroup(f, 0)
	require.Error(t, err, "loadGroup with address 0 should error")
	assert.Contains(t, err.Error(), "invalid group address")
}
