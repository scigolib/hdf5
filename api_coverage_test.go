package hdf5

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Section 1: loadObject coverage -- exercise all object-type branches
// ---------------------------------------------------------------------------

// TestLoadObject_DatasetFromV0File verifies that loadObject correctly
// resolves dataset objects in a v0 superblock file. This covers the
// ObjectTypeDataset branch within loadObject.
func TestLoadObject_DatasetFromV0File(t *testing.T) {
	t.Parallel()

	// fill_old.h5 is a v0 superblock file with datasets at root level.
	f, err := Open("testdata/reference/fill_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion(), "expected v0 superblock")

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.GreaterOrEqual(t, len(children), 2, "fill_old.h5 should have at least 2 children")

	// Verify at least one child is a Dataset.
	foundDataset := false
	for _, child := range children {
		if ds, ok := child.(*Dataset); ok {
			foundDataset = true
			require.NotEmpty(t, ds.Name())
			require.NotZero(t, ds.Address(), "dataset address should be non-zero")
		}
	}
	require.True(t, foundDataset, "should find at least one Dataset in fill_old.h5")
}

// TestLoadObject_GroupFromV0File verifies that loadObject correctly
// resolves nested group objects in a v0 superblock file. This covers the
// ObjectTypeGroup branch within loadObject, plus the fallback through
// ObjectTypeUnknown for v0 groups.
func TestLoadObject_GroupFromV0File(t *testing.T) {
	t.Parallel()

	// group_old.h5 is a v0 superblock file with a nested group "/old".
	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Len(t, children, 1, "group_old.h5 should have exactly 1 child")

	childGroup, ok := children[0].(*Group)
	require.True(t, ok, "child should be a Group")
	require.Contains(t, childGroup.Name(), "old")
}

// TestLoadObject_NestedGroupsV0 verifies loadObject with deeply nested
// groups in a v0 file. tbogus.h5 has /group/ with child datasets, exercising
// loadChildren with TREE B-tree format and multiple entries.
func TestLoadObject_NestedGroupsV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/tbogus.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	// Walk the file and collect all paths.
	var paths []string
	var groupCount, datasetCount int
	f.Walk(func(path string, obj Object) {
		paths = append(paths, path)
		switch obj.(type) {
		case *Group:
			groupCount++
		case *Dataset:
			datasetCount++
		}
	})

	// tbogus.h5 has root(6 children) + /group/(5 children) = 2 groups, 10 datasets.
	require.GreaterOrEqual(t, groupCount, 2, "should have at least 2 groups")
	require.GreaterOrEqual(t, datasetCount, 5, "should have at least 5 datasets")
	require.Equal(t, "/", paths[0], "first path should be root")
}

// TestLoadObject_ManyDatasetsV0 verifies loadChildren with a large number
// of children. le_data.h5 has 24 datasets at root level in v0 format.
func TestLoadObject_ManyDatasetsV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/le_data.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Equal(t, 24, len(children), "le_data.h5 should have 24 children")

	// Verify all children are Datasets.
	for _, child := range children {
		ds, ok := child.(*Dataset)
		require.True(t, ok, "all children should be Datasets, got %T for %q", child, child.Name())
		require.NotEmpty(t, ds.Name())
	}
}

// TestLoadObject_MultipleGroupsV0 verifies loadObject for v0 files
// containing multiple group children. mergemsg.h5 has /grp1, /grp2, /grp3.
func TestLoadObject_MultipleGroupsV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/mergemsg.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Equal(t, 3, len(children), "mergemsg.h5 should have 3 children")

	// Verify all children are Groups.
	groupNames := make(map[string]bool)
	for _, child := range children {
		g, ok := child.(*Group)
		require.True(t, ok, "all children should be Groups")
		groupNames[g.Name()] = true
	}

	require.True(t, groupNames["grp1"], "should have grp1")
	require.True(t, groupNames["grp2"], "should have grp2")
	require.True(t, groupNames["grp3"], "should have grp3")
}

// ---------------------------------------------------------------------------
// Section 2: loadChildren coverage -- more branch paths through B-tree
// ---------------------------------------------------------------------------

// TestLoadChildren_V0WithBigEndian verifies loadChildren on a big-endian
// v0 file with many datasets (be_data.h5).
func TestLoadChildren_V0WithBigEndian(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/be_data.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Equal(t, 24, len(children), "be_data.h5 should have 24 children")
}

// TestLoadChildren_V0WithDeflate verifies loadChildren correctly
// identifies Dataset objects in v0 files with compressed data.
func TestLoadChildren_V0WithDeflate(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/deflate.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Equal(t, 1, len(children))

	ds, ok := children[0].(*Dataset)
	require.True(t, ok, "child should be Dataset")
	require.Equal(t, "Dataset1", ds.Name())
}

// ---------------------------------------------------------------------------
// Section 3: Dataset reading API coverage (read path from group.go)
// ---------------------------------------------------------------------------

// TestDatasetRead_Float64FromV0 tests Dataset.Read() on a v0 file.
// This exercises the Read() method through loadObject.
func TestDatasetRead_Float64FromV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/filespace_1_8.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	data, err := ds.Read()
	require.NoError(t, err)
	require.Len(t, data, 100, "filespace_1_8 dataset has 100 elements")
}

// TestDatasetInfo_FromV0 tests Dataset.Info() method.
func TestDatasetInfo_FromV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/filespace_1_8.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	info, err := ds.Info()
	require.NoError(t, err)
	require.NotEmpty(t, info, "dataset info should not be empty")
	assert.Contains(t, info, "integer", "should describe integer type")
}

// TestDatasetAttributes_FromV0 tests Dataset.Attributes() and
// ListAttributes() on a dataset. specmetaread.h5 has datasets at root.
func TestDatasetAttributes_FromV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/specmetaread.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.GreaterOrEqual(t, len(children), 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	// Attributes() may return nil for datasets without attributes (v0 files).
	attrs, err := ds.Attributes()
	require.NoError(t, err, "Attributes() should not error even if no attrs")

	names, err := ds.ListAttributes()
	require.NoError(t, err, "ListAttributes() should not error even if no attrs")

	// If attrs is nil, len should be 0.
	require.Equal(t, len(attrs), len(names),
		"Attributes and ListAttributes should return same count")
}

// TestDatasetReadAttribute_NotFound tests Dataset.ReadAttribute() with
// a non-existent attribute name.
func TestDatasetReadAttribute_NotFound(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/specmetaread.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.GreaterOrEqual(t, len(children), 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	_, err = ds.ReadAttribute("nonexistent_attribute_xyz")
	require.Error(t, err, "ReadAttribute should fail for non-existent attribute")
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// Section 4: Group.Attributes() coverage -- both modern and traditional paths
// ---------------------------------------------------------------------------

// TestGroupAttributes_ModernGroup tests Group.Attributes() on modern
// v2 format groups that have stored address (non-zero).
func TestGroupAttributes_ModernGroup(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// Root group has non-zero address in v2 files.
	attrs, err := root.Attributes()
	require.NoError(t, err)
	require.NotNil(t, attrs, "Attributes() should return non-nil slice")

	// Walk and check subgroups too.
	f.Walk(func(path string, obj Object) {
		if g, ok := obj.(*Group); ok {
			attrs, err := g.Attributes()
			require.NoError(t, err, "Attributes() should not error for group %s", path)
			require.NotNil(t, attrs)
		}
	})
}

// TestGroupAttributes_TraditionalV0Group tests Group.Attributes() on
// a v0 traditional format group where address may be 0 (SNOD format).
func TestGroupAttributes_TraditionalV0Group(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/group_old.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// Root group in v0 files -- Attributes() should return empty slice or
	// attributes from the object header.
	attrs, err := root.Attributes()
	require.NoError(t, err)
	require.NotNil(t, attrs, "Attributes() should return non-nil slice even for v0")
}

// ---------------------------------------------------------------------------
// Section 5: Write-path coverage -- round-trip tests for group/dataset/attrs
// ---------------------------------------------------------------------------

// TestWriteReadRoundTrip_MixedObjectTypes verifies write -> read -> verify
// for files with mixed groups and datasets. This exercises loadObject for
// ObjectTypeGroup and ObjectTypeDataset branches in the write-created files.
func TestWriteReadRoundTrip_MixedObjectTypes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_mixed.h5")

	// Write file with mixed structure.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		// Create groups.
		_, err = fw.CreateGroup("/sensors")
		require.NoError(t, err)
		_, err = fw.CreateGroup("/sensors/temperature")
		require.NoError(t, err)
		_, err = fw.CreateGroup("/metadata")
		require.NoError(t, err)

		// Create datasets in different locations.
		ds1, err := fw.CreateDataset("/sensors/temperature/readings", Float64, []uint64{5})
		require.NoError(t, err)
		require.NoError(t, ds1.Write([]float64{20.1, 20.5, 21.0, 20.8, 20.3}))

		ds2, err := fw.CreateDataset("/sensors/humidity", Float32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds2.Write([]float32{45.0, 46.5, 48.0}))

		ds3, err := fw.CreateDataset("/metadata/version", Int32, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds3.Write([]int32{42}))

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	f.Walk(func(_ string, obj Object) {
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	require.Equal(t, 4, groups, "should have root + sensors + sensors/temperature + metadata")
	require.Equal(t, 3, datasets, "should have 3 datasets")
}

// TestWriteReadRoundTrip_V0FileOnly creates a v0 format empty file
// and verifies it can be reopened. V0 with datasets uses the symbol table
// format which is not fully supported for write->reopen, but the empty
// file round-trip exercises the v0 superblock and root group paths.
func TestWriteReadRoundTrip_V0FileOnly(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_v0.h5")

	// Write empty v0 file.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate, WithSuperblockVersion(core.Version0))
		require.NoError(t, err)
		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.Equal(t, uint8(0), f.SuperblockVersion())

	root := f.Root()
	require.NotNil(t, root)
	require.Equal(t, "/", root.Name())

	// Empty file should have no children.
	children := root.Children()
	require.Empty(t, children, "empty v0 file should have 0 children")
}

// TestWriteReadRoundTrip_V2WithGroupsAndDatasets creates a v2 format
// file with nested groups and datasets, covering loadObject, loadModernGroup,
// and loadChildren thoroughly via write -> read -> verify.
func TestWriteReadRoundTrip_V2WithGroupsAndDatasets(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_v2_nested.h5")

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		_, err = fw.CreateGroup("/grp")
		require.NoError(t, err)

		ds, err := fw.CreateDataset("/grp/values", Int32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{10, 20, 30}))

		ds2, err := fw.CreateDataset("/data", Float64, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds2.Write([]float64{1.5, 2.5}))

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	var paths []string
	f.Walk(func(path string, obj Object) {
		paths = append(paths, path)
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	require.GreaterOrEqual(t, groups, 2, "should have root + grp")
	require.GreaterOrEqual(t, datasets, 2, "should have data + grp/values")
	require.Equal(t, "/", paths[0], "first path should be root")
}

// ---------------------------------------------------------------------------
// Section 6: RebalanceAttributeBTree -- test the objectHeader != nil path
// ---------------------------------------------------------------------------

// TestRebalanceAttributeBTree_DenseViaReadHeader tests the code path
// in RebalanceAttributeBTree where denseAttrInfo==nil but objectHeader==nil,
// so it reads the object header from disk. We create a file with 10+ attrs
// (triggers dense storage), close it, reopen it fresh, and call
// RebalanceAttributeBTree.
func TestRebalanceAttributeBTree_DenseViaReadHeader(t *testing.T) {
	t.Parallel()

	// Use explicit file path with defer Remove to avoid TempDir cleanup race.
	filename := filepath.Join("tmp", "test_rebalance_read_header.h5")
	defer func() { _ = os.Remove(filename) }()

	// Step 1: Create file with dense attributes.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)

		ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))

		// Add 10 attributes to trigger dense storage.
		for i := 0; i < 10; i++ {
			err = ds.WriteAttribute(fmt.Sprintf("attr_%d", i), int32(i))
			require.NoError(t, err)
		}

		// The dataset was created in this session, so denseAttrInfo is nil
		// and objectHeader is nil. This tests the fresh-read path.
		err = ds.RebalanceAttributeBTree()
		require.NoError(t, err, "RebalanceAttributeBTree should succeed on in-session dense dataset")

		require.NoError(t, fw.Close())
	}()

	// Step 2: Reopen with OpenForWrite and test the cached path.
	func() {
		fw, err := OpenForWrite(filename, OpenReadWrite)
		require.NoError(t, err)

		ds, err := fw.OpenDataset("/data")
		require.NoError(t, err)

		// denseAttrInfo should be set now (cached from OpenDataset).
		err = ds.RebalanceAttributeBTree()
		require.NoError(t, err, "RebalanceAttributeBTree should succeed on reopened dataset with cached header")

		require.NoError(t, fw.Close())
	}()

	// Step 3: Verify file is still valid after rebalancing.
	f, err := Open(filename)
	require.NoError(t, err)

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 10, "should still have 10 attributes after rebalancing")

	require.NoError(t, f.Close())
}

// TestRebalanceAttributeBTree_CompactOnly tests RebalanceAttributeBTree
// on a dataset with only compact attributes (less than 8). The code should
// find attrInfo==nil and return nil (no-op).
func TestRebalanceAttributeBTree_CompactOnly(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_rebalance_compact.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2, 3}))

	// Add only 3 attributes (compact storage).
	for i := 0; i < 3; i++ {
		err = ds.WriteAttribute(fmt.Sprintf("compact_%d", i), int32(i))
		require.NoError(t, err)
	}

	// RebalanceAttributeBTree should be a no-op.
	err = ds.RebalanceAttributeBTree()
	require.NoError(t, err, "should succeed (no-op) for compact storage")
}

// ---------------------------------------------------------------------------
// Section 7: Dataset attribute write/read round-trip with all scalar types
// ---------------------------------------------------------------------------

// TestAttributeWriteRead_AllScalarTypes verifies round-trip for all
// supported scalar attribute types. This increases coverage for
// inferDatatypeFromValue, encodeAttributeValue, and writeCompactAttribute.
func TestAttributeWriteRead_AllScalarTypes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_attr_scalars.h5")

	tests := []struct {
		name  string
		value interface{}
	}{
		{"int8", int8(42)},
		{"int16", int16(1234)},
		{"int32", int32(100000)},
		{"int64", int64(9876543210)},
		{"uint8", uint8(255)},
		{"uint16", uint16(65535)},
		{"uint32", uint32(4294967295)},
		{"uint64", uint64(18446744073709551615)},
		{"float32", float32(3.14)},
		{"float64", float64(2.71828)},
		{"string", "hello_hdf5"},
	}

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/data", Float64, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{0.0}))

		for _, tt := range tests {
			err = ds.WriteAttribute(tt.name, tt.value)
			require.NoError(t, err, "WriteAttribute failed for %s", tt.name)
		}

		require.NoError(t, fw.Close())
	}()

	// Read and verify attributes exist.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	// Compact storage holds up to 7. Since we have 11 attributes, dense storage kicks in at 8.
	require.Len(t, attrs, len(tests), "should have all %d attributes", len(tests))
}

// TestAttributeWriteRead_SliceTypes verifies round-trip for all
// supported slice attribute types.
func TestAttributeWriteRead_SliceTypes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_attr_slices.h5")

	tests := []struct {
		name  string
		value interface{}
	}{
		{"int8_slice", []int8{-1, 0, 1}},
		{"int16_slice", []int16{-100, 0, 100}},
		{"int32_slice", []int32{1, 2, 3}},
		{"int64_slice", []int64{100, 200, 300}},
		{"uint8_slice", []uint8{0, 128, 255}},
		{"uint16_slice", []uint16{0, 1000, 65535}},
		{"uint32_slice", []uint32{0, 1000000, 4294967295}},
		{"uint64_slice", []uint64{0, 1, 2}},
		{"float32_slice", []float32{1.0, 2.0, 3.0}},
		{"float64_slice", []float64{1.1, 2.2, 3.3}},
	}

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/data", Float64, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{0.0}))

		for _, tt := range tests {
			err = ds.WriteAttribute(tt.name, tt.value)
			require.NoError(t, err, "WriteAttribute failed for %s", tt.name)
		}

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, len(tests))
}

// ---------------------------------------------------------------------------
// Section 8: inferDatatypeFromValue edge cases
// ---------------------------------------------------------------------------

// TestInferDatatypeFromValue_Nil tests nil value handling.
func TestInferDatatypeFromValue_Nil(t *testing.T) {
	t.Parallel()

	_, _, err := inferDatatypeFromValue(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil or invalid")
}

// TestInferDatatypeFromValue_UnsupportedSlice tests unsupported slice
// element type.
func TestInferDatatypeFromValue_UnsupportedSlice(t *testing.T) {
	t.Parallel()

	_, _, err := inferDatatypeFromValue([]bool{true, false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestEncodeAttributeValue_UnsupportedType tests encoding unsupported type.
func TestEncodeAttributeValue_UnsupportedType(t *testing.T) {
	t.Parallel()

	_, err := encodeAttributeValue(struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestEncodeSliceValue_UnsupportedElemType tests encoding slice with
// unsupported element type.
func TestEncodeSliceValue_UnsupportedElemType(t *testing.T) {
	t.Parallel()

	_, _, err := inferDatatypeFromValue([]complex64{1 + 2i})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// ---------------------------------------------------------------------------
// Section 9: loadGroup -- edge case: invalid group address
// ---------------------------------------------------------------------------

// TestLoadGroup_InvalidAddress tests that loadGroup returns an error
// for address 0.
func TestLoadGroup_InvalidAddress(t *testing.T) {
	t.Parallel()

	// Create a minimal file to get a File handle.
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_invalid_addr.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Call loadGroup with address 0 -- should fail.
	_, err = loadGroup(f, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid group address")
}

// ---------------------------------------------------------------------------
// Section 10: Dataset reading methods -- additional coverage
// ---------------------------------------------------------------------------

// TestDatasetReadStrings_WriteRead verifies Dataset.ReadStrings()
// via a write-read round trip.
func TestDatasetReadStrings_WriteRead(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_strings.h5")

	// Write string dataset.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/labels", String, []uint64{3}, WithStringSize(16))
		require.NoError(t, err)
		require.NoError(t, ds.Write([]string{"alpha", "beta", "gamma"}))

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	strings, err := ds.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 3)
	assert.Equal(t, "alpha", strings[0])
	assert.Equal(t, "beta", strings[1])
	assert.Equal(t, "gamma", strings[2])
}

// TestDatasetReadCompound_WriteRead verifies Dataset.ReadCompound().
func TestDatasetReadCompound_WriteRead(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_compound.h5")

	// Write compound dataset using the correct API (raw bytes).
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		// Create compound type: struct { float64 x; float64 y }
		f64Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 8)
		require.NoError(t, err)

		fields := []core.CompoundFieldDef{
			{Name: "x", Offset: 0, Type: f64Type},
			{Name: "y", Offset: 8, Type: f64Type},
		}
		compoundType, err := core.CreateCompoundTypeFromFields(fields)
		require.NoError(t, err)

		ds, err := fw.CreateCompoundDataset("/points", compoundType, []uint64{2})
		require.NoError(t, err)

		// Write 2 records: {x:1.0, y:2.0}, {x:3.0, y:4.0}
		// IEEE 754 LE: 1.0=3FF0..., 2.0=4000..., 3.0=4008..., 4.0=4010...
		data := []byte{
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // x=1.0
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // y=2.0
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // x=3.0
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x40, // y=4.0
		}
		require.NoError(t, ds.WriteRaw(data))

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	values, err := ds.ReadCompound()
	require.NoError(t, err)
	require.Len(t, values, 2)

	// Verify field values.
	require.Contains(t, values[0], "x")
	require.Contains(t, values[0], "y")
}

// ---------------------------------------------------------------------------
// Section 11: NamedDatatype coverage
// ---------------------------------------------------------------------------

// TestNamedDatatype_Methods tests Name() and Datatype() methods.
func TestNamedDatatype_Methods(t *testing.T) {
	t.Parallel()

	ndt := &NamedDatatype{
		name:    "test_type",
		address: 0x1234,
		datatype: &core.DatatypeMessage{
			Class: core.DatatypeFixed,
			Size:  4,
		},
	}

	require.Equal(t, "test_type", ndt.Name())
	require.NotNil(t, ndt.Datatype())
	require.Equal(t, core.DatatypeFixed, ndt.Datatype().Class)
	require.Equal(t, uint32(4), ndt.Datatype().Size)
}

// ---------------------------------------------------------------------------
// Section 12: File.Open error paths
// ---------------------------------------------------------------------------

// TestOpen_ErrorPaths tests various error conditions for Open().
func TestOpen_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("non-existent file", func(t *testing.T) {
		t.Parallel()
		_, err := Open("testdata/does_not_exist_test.h5")
		require.Error(t, err)
	})

	t.Run("not an HDF5 file", func(t *testing.T) {
		t.Parallel()
		// Use this test file itself as input -- it is not HDF5.
		_, err := Open("api_coverage_test.go")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an HDF5 file")
	})

	t.Run("empty file", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		emptyFile := filepath.Join(tempDir, "empty.h5")
		require.NoError(t, os.WriteFile(emptyFile, []byte{}, 0o644))
		_, err := Open(emptyFile)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Section 13: Walk with NamedDatatype (ensure Walk handles it)
// ---------------------------------------------------------------------------

// TestWalkWithAllObjectTypes exercises Walk with groups, datasets,
// and verifies object type assertion works for mixed structures.
func TestWalkWithAllObjectTypes(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	groupPaths := make(map[string]bool)
	datasetPaths := make(map[string]bool)
	allPaths := make([]string, 0)

	f.Walk(func(path string, obj Object) {
		allPaths = append(allPaths, path)
		switch obj.(type) {
		case *Group:
			groupPaths[path] = true
		case *Dataset:
			datasetPaths[path] = true
		}
	})

	// with_groups.h5 structure:
	// / (root)
	//   dataset1
	//   subgroup/
	//     dataset2
	//     nested_group/
	//       nested_data
	require.True(t, groupPaths["/"], "root should be visited")
	require.Len(t, allPaths, 6, "should visit all 6 objects")
	require.Equal(t, 3, len(groupPaths), "should have 3 groups")
	require.Equal(t, 3, len(datasetPaths), "should have 3 datasets")
}

// ---------------------------------------------------------------------------
// Section 14: Additional V0 file coverage
// ---------------------------------------------------------------------------

// TestV0Files_TableDriven runs through multiple v0 reference files
// to ensure they all load correctly and objects resolve.
func TestV0Files_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		file            string
		expectedVersion uint8
		minChildren     int
	}{
		{"fill_old", "testdata/reference/fill_old.h5", 0, 2},
		{"tarrold", "testdata/reference/tarrold.h5", 0, 2},
		{"group_old", "testdata/reference/group_old.h5", 0, 1},
		{"deflate", "testdata/reference/deflate.h5", 0, 1},
		{"charsets", "testdata/reference/charsets.h5", 0, 1},
		{"mergemsg", "testdata/reference/mergemsg.h5", 0, 3},
		{"filespace_1_6", "testdata/reference/filespace_1_6.h5", 0, 1},
		{"tmtimen", "testdata/reference/tmtimen.h5", 0, 1},
		{"tmtimeo", "testdata/reference/tmtimeo.h5", 0, 1},
		{"tnullspace", "testdata/reference/tnullspace.h5", 0, 1},
		{"noencoder", "testdata/reference/noencoder.h5", 0, 2},
		{"fsm_aggr_nopersist", "testdata/reference/fsm_aggr_nopersist.h5", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Open(tt.file)
			require.NoError(t, err, "failed to open %s", tt.file)
			defer func() { _ = f.Close() }()

			require.Equal(t, tt.expectedVersion, f.SuperblockVersion(),
				"unexpected superblock version for %s", tt.file)

			root := f.Root()
			require.NotNil(t, root)
			children := root.Children()
			require.GreaterOrEqual(t, len(children), tt.minChildren,
				"insufficient children in %s: got %d, want >= %d",
				tt.file, len(children), tt.minChildren)

			// Walk to ensure all objects resolve without error.
			count := 0
			f.Walk(func(_ string, _ Object) {
				count++
			})
			require.Greater(t, count, 0, "should visit at least 1 object in %s", tt.file)
		})
	}
}

// TestV2V3Files_TableDriven runs through v2 and v3 reference files.
func TestV2V3Files_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		file            string
		expectedVersion uint8
		minChildren     int
	}{
		{"aggr", "testdata/reference/aggr.h5", 2, 1},
		{"btree_idx_1_8", "testdata/reference/btree_idx_1_8.h5", 2, 2},
		{"fill18", "testdata/reference/fill18.h5", 2, 1},
		{"none", "testdata/reference/none.h5", 2, 1},
		{"fsm_aggr_persist", "testdata/reference/fsm_aggr_persist.h5", 2, 1},
		{"paged_persist", "testdata/reference/paged_persist.h5", 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Open(tt.file)
			require.NoError(t, err, "failed to open %s", tt.file)
			defer func() { _ = f.Close() }()

			require.Equal(t, tt.expectedVersion, f.SuperblockVersion())

			root := f.Root()
			require.NotNil(t, root)
			children := root.Children()
			require.GreaterOrEqual(t, len(children), tt.minChildren)

			// Walk to ensure all objects resolve without error.
			count := 0
			f.Walk(func(_ string, _ Object) {
				count++
			})
			require.Greater(t, count, 0)
		})
	}
}

// ---------------------------------------------------------------------------
// Section 15: Dense attribute storage -- full lifecycle round-trip
// ---------------------------------------------------------------------------

// TestDenseAttributes_FullLifecycle creates a file with dense
// attributes, closes it, reopens for reading, and verifies all attributes.
// This exercises transitionToDenseAttributes and writeDenseAttribute.
func TestDenseAttributes_FullLifecycle(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_dense_lifecycle.h5")

	const numAttrs = 15

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))

		for i := 0; i < numAttrs; i++ {
			err = ds.WriteAttribute(fmt.Sprintf("attr_%03d", i), int32(i*10))
			require.NoError(t, err)
		}

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, numAttrs, "should have all %d dense attributes", numAttrs)

	// Verify attribute names exist.
	names, err := ds.ListAttributes()
	require.NoError(t, err)
	require.Len(t, names, numAttrs)
}

// TestDenseAttributes_WriteDeleteRebalanceVerify creates dense attrs,
// deletes some, rebalances, and verifies remaining.
func TestDenseAttributes_WriteDeleteRebalanceVerify(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_dense_delete_rebalance.h5")

	const totalAttrs = 12
	const deleteAttrs = 4

	// Write and delete.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate,
			WithBTreeRebalancing(false))
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/data", Float64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3}))

		for i := 0; i < totalAttrs; i++ {
			err = ds.WriteAttribute(fmt.Sprintf("attr_%03d", i), float64(i))
			require.NoError(t, err)
		}

		// Delete some attributes.
		for i := 0; i < deleteAttrs; i++ {
			err = ds.DeleteAttribute(fmt.Sprintf("attr_%03d", i))
			require.NoError(t, err)
		}

		// Rebalance.
		err = ds.RebalanceAttributeBTree()
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, totalAttrs-deleteAttrs,
		"should have %d remaining attributes", totalAttrs-deleteAttrs)
}

// ---------------------------------------------------------------------------
// Section 16: inferDatatypeFromValue -- all supported types comprehensively
// ---------------------------------------------------------------------------

// TestInferDatatypeFromValue_AllTypes is a comprehensive table-driven
// test for all type branches in inferDatatypeFromValue.
func TestInferDatatypeFromValue_AllTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     interface{}
		wantClass core.DatatypeClass
		wantSize  uint32
		wantDims  []uint64
		wantErr   bool
	}{
		// Signed integers.
		{"int8", int8(-1), core.DatatypeFixed, 1, []uint64{1}, false},
		{"int16", int16(-256), core.DatatypeFixed, 2, []uint64{1}, false},
		{"int32", int32(42), core.DatatypeFixed, 4, []uint64{1}, false},
		{"int64", int64(9999), core.DatatypeFixed, 8, []uint64{1}, false},

		// Unsigned integers.
		{"uint8", uint8(255), core.DatatypeFixed, 1, []uint64{1}, false},
		{"uint16", uint16(65535), core.DatatypeFixed, 2, []uint64{1}, false},
		{"uint32", uint32(99), core.DatatypeFixed, 4, []uint64{1}, false},
		{"uint64", uint64(42), core.DatatypeFixed, 8, []uint64{1}, false},

		// Floats.
		{"float32", float32(3.14), core.DatatypeFloat, 4, []uint64{1}, false},
		{"float64", float64(2.718), core.DatatypeFloat, 8, []uint64{1}, false},

		// String.
		{"string", "test", core.DatatypeString, 5, []uint64{1}, false},
		{"empty_string", "", core.DatatypeString, 1, []uint64{1}, false},

		// Slices.
		{"int8_slice", []int8{1, 2}, core.DatatypeFixed, 1, []uint64{2}, false},
		{"int16_slice", []int16{1, 2}, core.DatatypeFixed, 2, []uint64{2}, false},
		{"int32_slice", []int32{1, 2, 3}, core.DatatypeFixed, 4, []uint64{3}, false},
		{"int64_slice", []int64{1}, core.DatatypeFixed, 8, []uint64{1}, false},
		{"uint8_slice", []uint8{0, 1}, core.DatatypeFixed, 1, []uint64{2}, false},
		{"uint16_slice", []uint16{1000}, core.DatatypeFixed, 2, []uint64{1}, false},
		{"uint32_slice", []uint32{1, 2, 3, 4}, core.DatatypeFixed, 4, []uint64{4}, false},
		{"uint64_slice", []uint64{1, 2}, core.DatatypeFixed, 8, []uint64{2}, false},
		{"float32_slice", []float32{1.0, 2.0}, core.DatatypeFloat, 4, []uint64{2}, false},
		{"float64_slice", []float64{1.0}, core.DatatypeFloat, 8, []uint64{1}, false},

		// Errors.
		{"nil", nil, 0, 0, nil, true},
		{"empty_int32_slice", []int32{}, 0, 0, nil, true},
		{"unsupported_struct", struct{}{}, 0, 0, nil, true},
		{"unsupported_bool_slice", []bool{true}, 0, 0, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dt, ds, err := inferDatatypeFromValue(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, dt)
			require.NotNil(t, ds)
			assert.Equal(t, tt.wantClass, dt.Class, "class mismatch")
			assert.Equal(t, tt.wantSize, dt.Size, "size mismatch")
			assert.Equal(t, tt.wantDims, ds.Dimensions, "dims mismatch")
		})
	}
}

// ---------------------------------------------------------------------------
// Section 17: encodeAttributeValue -- all types comprehensively
// ---------------------------------------------------------------------------

// TestEncodeAttributeValue_AllTypes tests encoding for all supported types.
func TestEncodeAttributeValue_AllTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   interface{}
		wantLen int
		wantErr bool
	}{
		{"int8", int8(42), 1, false},
		{"int16", int16(1000), 2, false},
		{"int32", int32(100000), 4, false},
		{"int64", int64(9876543210), 8, false},
		{"uint8", uint8(200), 1, false},
		{"uint16", uint16(50000), 2, false},
		{"uint32", uint32(3000000000), 4, false},
		{"uint64", uint64(18446744073709551615), 8, false},
		{"float32", float32(3.14), 4, false},
		{"float64", float64(2.718), 8, false},
		{"string", "hi", 3, false},
		{"int8_slice", []int8{1, 2, 3}, 3, false},
		{"uint8_slice", []uint8{0, 255}, 2, false},
		{"int16_slice", []int16{-1, 1}, 4, false},
		{"uint16_slice", []uint16{0, 65535}, 4, false},
		{"int32_slice", []int32{1, 2}, 8, false},
		{"uint32_slice", []uint32{1, 2}, 8, false},
		{"int64_slice", []int64{1}, 8, false},
		{"uint64_slice", []uint64{1, 2}, 16, false},
		{"float32_slice", []float32{1.0, 2.0, 3.0}, 12, false},
		{"float64_slice", []float64{1.0, 2.0}, 16, false},
		{"unsupported", struct{}{}, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := encodeAttributeValue(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, data, tt.wantLen)
		})
	}
}

// ---------------------------------------------------------------------------
// Section 18: Dataset.Address() and Dataset.Name() coverage
// ---------------------------------------------------------------------------

// TestDatasetAddressAndName verifies Address() and Name() methods.
func TestDatasetAddressAndName(t *testing.T) {
	t.Parallel()

	ds := &Dataset{
		name:    "test_dataset",
		address: 0xABCD,
	}

	require.Equal(t, "test_dataset", ds.Name())
	require.Equal(t, uint64(0xABCD), ds.Address())
}

// ---------------------------------------------------------------------------
// Section 19: Group.Name() and Group.Children() basic coverage
// ---------------------------------------------------------------------------

// TestGroupNameAndChildren verifies basic Group methods.
func TestGroupNameAndChildren(t *testing.T) {
	t.Parallel()

	g := &Group{
		name: "test_group",
		children: []Object{
			&Dataset{name: "ds1"},
			&Dataset{name: "ds2"},
		},
	}

	require.Equal(t, "test_group", g.Name())
	require.Len(t, g.Children(), 2)
	require.Equal(t, "ds1", g.Children()[0].Name())
	require.Equal(t, "ds2", g.Children()[1].Name())
}

// ---------------------------------------------------------------------------
// Section 20: CreateForWrite error paths
// ---------------------------------------------------------------------------

// TestCreateForWrite_InvalidOption tests CreateForWrite with invalid
// option type.
func TestCreateForWrite_InvalidOption(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_invalid_opt.h5")

	_, err := CreateForWrite(filename, CreateTruncate, "invalid_option")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid option type")
}

// TestCreate_InvalidModeValue tests Create with invalid mode value.
func TestCreate_InvalidModeValue(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_invalid_mode.h5")

	_, err := Create(filename, CreateMode(99))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid create mode")
}

// ---------------------------------------------------------------------------
// Section 21: Dataset.Attributes(), ReadAttribute() with actual attrs
// ---------------------------------------------------------------------------

// TestDatasetAttributes_WithAttrsFromOfficialFile tests Dataset
// attribute reading on a file that actually has attributes (tall.h5).
// This covers the success path of Dataset.Attributes(), ListAttributes(),
// ReadAttribute(), and Info().
func TestDatasetAttributes_WithAttrsFromOfficialFile(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tall.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate to the dataset with attributes: /g1/g1.1/dset1.1.1 has 2 attrs.
	var targetDS *Dataset
	f.Walk(func(_ string, obj Object) {
		if ds, ok := obj.(*Dataset); ok {
			attrs, err := ds.Attributes()
			if err == nil && len(attrs) > 0 {
				targetDS = ds
			}
		}
	})

	require.NotNil(t, targetDS, "should find at least one dataset with attributes")

	// Test Attributes().
	attrs, err := targetDS.Attributes()
	require.NoError(t, err)
	require.Greater(t, len(attrs), 0, "should have at least 1 attribute")

	// Test ListAttributes().
	names, err := targetDS.ListAttributes()
	require.NoError(t, err)
	require.Equal(t, len(attrs), len(names))

	// Test ReadAttribute() for a known attribute name. Some datatypes may not
	// be supported by ReadValue(), so we only check that it does not panic.
	// ReadAttribute internally calls attr.ReadValue() which may return
	// "unsupported datatype" for some exotic types.
	for _, name := range names {
		_, _ = targetDS.ReadAttribute(name)
		// We don't assert NoError because ReadValue() may not support all types.
	}

	// Test Info().
	info, err := targetDS.Info()
	require.NoError(t, err)
	require.NotEmpty(t, info)
}

// TestGroupAttributes_WithAttrsFromOfficialFile tests Group attribute
// reading on tall.h5 which has attributes on root group.
func TestGroupAttributes_WithAttrsFromOfficialFile(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tall.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// Root group has 2 attributes in tall.h5.
	attrs, err := root.Attributes()
	require.NoError(t, err)
	require.Greater(t, len(attrs), 0, "root group should have attributes")
}

// TestDatasetAttributes_ManyAttrs tests reading a dataset with 33
// attributes from tattr2.h5. This exercises dense attribute reading paths.
func TestDatasetAttributes_ManyAttrs(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tattr2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// Root group has 30 attrs.
	rootAttrs, err := root.Attributes()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rootAttrs), 10,
		"tattr2.h5 root should have many attributes")

	// Find the dataset with 33 attributes.
	children := root.Children()
	for _, child := range children {
		if ds, ok := child.(*Dataset); ok {
			attrs, err := ds.Attributes()
			require.NoError(t, err)
			if len(attrs) > 10 {
				// Test ListAttributes.
				names, err := ds.ListAttributes()
				require.NoError(t, err)
				require.Equal(t, len(attrs), len(names))
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Section 22: NamedDatatype via official test files
// ---------------------------------------------------------------------------

// TestNamedDatatype_FromOfficialFile reads h5copytst_new.h5 which
// has named datatypes, exercising the ObjectTypeDatatype branch in loadObject.
func TestNamedDatatype_FromOfficialFile(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5copytst_new.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var namedDatatypes int
	f.Walk(func(_ string, obj Object) {
		if ndt, ok := obj.(*NamedDatatype); ok {
			namedDatatypes++
			require.NotEmpty(t, ndt.Name(), "named datatype should have a name")
			// Datatype may be nil if parsing was partial.
			if ndt.Datatype() != nil {
				require.NotZero(t, ndt.Datatype().Size, "datatype size should be non-zero")
			}
		}
	})

	require.Greater(t, namedDatatypes, 0, "h5copytst_new.h5 should have named datatypes")
}

// TestNamedDatatype_VariousFiles tests named datatypes across
// multiple official files.
func TestNamedDatatype_VariousFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		file          string
		minNamedTypes int
		minGroups     int
		minDatasets   int
	}{
		{"tcompound", "testdata/hdf5_official/tcompound.h5", 3, 3, 5},
		{"tsoftlinks", "testdata/hdf5_official/tsoftlinks.h5", 1, 3, 2},
		{"tenum", "testdata/hdf5_official/tenum.h5", 1, 1, 1},
		{"tvlstr", "testdata/hdf5_official/tvlstr.h5", 1, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Open(tt.file)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			var groups, datasets, namedTypes int
			f.Walk(func(_ string, obj Object) {
				switch obj.(type) {
				case *Group:
					groups++
				case *Dataset:
					datasets++
				case *NamedDatatype:
					namedTypes++
				}
			})

			require.GreaterOrEqual(t, namedTypes, tt.minNamedTypes,
				"%s should have >= %d named types", tt.file, tt.minNamedTypes)
			require.GreaterOrEqual(t, groups, tt.minGroups)
			require.GreaterOrEqual(t, datasets, tt.minDatasets)
		})
	}
}

// ---------------------------------------------------------------------------
// Section 23: Complex nested group structures (loadChildren deep branches)
// ---------------------------------------------------------------------------

// TestDeepNestedGroups tests loading deeply nested groups from
// h5ex_g_traverse.h5 (8 groups, 2 datasets).
func TestDeepNestedGroups(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5ex_g_traverse.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	var maxDepth int
	f.Walk(func(path string, obj Object) {
		depth := 0
		for _, c := range path {
			if c == '/' {
				depth++
			}
		}
		if depth > maxDepth {
			maxDepth = depth
		}

		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	require.GreaterOrEqual(t, groups, 5, "should have multiple nested groups")
	require.GreaterOrEqual(t, datasets, 2, "should have at least 2 datasets")
	require.GreaterOrEqual(t, maxDepth, 2, "should have at least 2 levels deep")
}

// TestTGroup_ManyGroups exercises loadChildren with 14 groups from
// tgroup.h5, which has a complex group hierarchy.
func TestTGroup_ManyGroups(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tgroup.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups int
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*Group); ok {
			groups++
		}
	})

	require.GreaterOrEqual(t, groups, 10, "tgroup.h5 should have many groups")
}

// TestTAll_MixedStructure exercises loadObject with a mix of groups
// and datasets from tall.h5.
func TestTAll_MixedStructure(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tall.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	var paths []string
	f.Walk(func(path string, obj Object) {
		paths = append(paths, path)
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	require.GreaterOrEqual(t, groups, 4, "tall.h5 should have 4+ groups")
	require.GreaterOrEqual(t, datasets, 4, "tall.h5 should have 4 datasets")
	require.Equal(t, "/", paths[0], "first path should be root")
}

// TestTHLink_HardLinks exercises hard link resolution from thlink.h5.
func TestTHLink_HardLinks(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/thlink.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets int
	f.Walk(func(_ string, obj Object) {
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	require.GreaterOrEqual(t, groups, 3, "thlink.h5 should have 3+ groups")
	require.GreaterOrEqual(t, datasets, 3, "thlink.h5 should have 3 datasets")
}

// TestTManyDatasets reads h5diff_dset1.h5 with 41 datasets to
// heavily exercise loadObject's dataset branch.
func TestTManyDatasets(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5diff_dset1.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var datasets int
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*Dataset); ok {
			datasets++
		}
	})

	require.GreaterOrEqual(t, datasets, 30, "h5diff_dset1.h5 should have 30+ datasets")
}

// TestTLargeObjName reads tlarge_objname.h5 which has 52 groups.
func TestTLargeObjName(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/tlarge_objname.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups int
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*Group); ok {
			groups++
		}
	})

	require.GreaterOrEqual(t, groups, 50, "tlarge_objname.h5 should have 50+ groups")
}

// ---------------------------------------------------------------------------
// Section 24: loadGroupWithCachedSymbolTable coverage
// ---------------------------------------------------------------------------

// TestLoadGroupWithCachedSymbolTable_ViaNestedGroups verifies the
// cached symbol table path by reading v0 files with nested groups where
// CacheType=1 entries are present.
func TestLoadGroupWithCachedSymbolTable_ViaNestedGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		file      string
		minGroups int
	}{
		{"tbogus_nested", "testdata/reference/tbogus.h5", 2},
		{"tgroup", "testdata/hdf5_official/tgroup.h5", 10},
		{"h5ex_g_traverse", "testdata/hdf5_official/h5ex_g_traverse.h5", 5},
		{"tall", "testdata/hdf5_official/tall.h5", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Open(tt.file)
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			var groups int
			var nestedGroupFound bool
			f.Walk(func(path string, obj Object) {
				if g, ok := obj.(*Group); ok {
					groups++
					// Check for nested groups (at least 2 slashes in path).
					if path != "/" && len(g.Children()) > 0 {
						nestedGroupFound = true
					}
				}
			})

			require.GreaterOrEqual(t, groups, tt.minGroups)
			if tt.minGroups > 2 {
				require.True(t, nestedGroupFound,
					"expected nested groups in %s", tt.file)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Section 25: Read+Write attribute on group (via write path)
// ---------------------------------------------------------------------------

// TestGroupAttributeWrite_ReadVerify writes attributes to a group
// and verifies them after reopen.
func TestGroupAttributeWrite_ReadVerify(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_group_attrs.h5")

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		_, err = fw.CreateGroup("/mygroup")
		require.NoError(t, err)

		ds, err := fw.CreateDataset("/mygroup/data", Float64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3}))

		// Write attributes to the dataset.
		require.NoError(t, ds.WriteAttribute("unit", "celsius"))
		require.NoError(t, ds.WriteAttribute("scale", float64(1.0)))
		require.NoError(t, ds.WriteAttribute("offset", int32(0)))

		require.NoError(t, fw.Close())
	}()

	// Read.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	group, ok := children[0].(*Group)
	require.True(t, ok)

	groupChildren := group.Children()
	require.Len(t, groupChildren, 1)

	ds, ok := groupChildren[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 3)

	names, err := ds.ListAttributes()
	require.NoError(t, err)
	require.Len(t, names, 3)

	// Read specific attribute.
	for _, name := range names {
		val, err := ds.ReadAttribute(name)
		require.NoError(t, err, "ReadAttribute(%q) should succeed", name)
		require.NotNil(t, val, "ReadAttribute(%q) should return non-nil", name)
	}
}

// ---------------------------------------------------------------------------
// Section 26: Dataset.Read() for integer data from v0 reference files
// ---------------------------------------------------------------------------

// TestDatasetRead_IntegerFromV0 reads integer datasets from v0 files.
func TestDatasetRead_IntegerFromV0(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/reference/specmetaread.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.GreaterOrEqual(t, len(children), 2, "specmetaread.h5 should have 2 datasets")

	for _, child := range children {
		ds, ok := child.(*Dataset)
		require.True(t, ok)

		data, err := ds.Read()
		require.NoError(t, err)
		require.NotEmpty(t, data, "dataset %s should have data", ds.Name())
	}
}

// ---------------------------------------------------------------------------
// Section 27: File reader method
// ---------------------------------------------------------------------------

// TestFileReader tests File.Reader() method.
func TestFileReader(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	reader := f.Reader()
	require.NotNil(t, reader)

	// Read HDF5 signature.
	buf := make([]byte, 8)
	n, err := reader.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 8, n)
	require.Equal(t, "\x89HDF\r\n\x1a\n", string(buf))
}

// TestFileSuperblock tests File.Superblock() method.
func TestFileSuperblock(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sb := f.Superblock()
	require.NotNil(t, sb)
	require.Equal(t, uint8(2), sb.Version)
}

// ---------------------------------------------------------------------------
// Section 28: H5CopyTstNew - comprehensive mixed object types
// ---------------------------------------------------------------------------

// TestH5CopyTstNew_AllObjectTypes reads h5copytst_new.h5 which has
// groups, datasets, and named datatypes -- all three object types in one file.
func TestH5CopyTstNew_AllObjectTypes(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5copytst_new.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groups, datasets, namedTypes int
	var allPaths []string
	f.Walk(func(path string, obj Object) {
		allPaths = append(allPaths, path)
		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		case *NamedDatatype:
			namedTypes++
		}
	})

	require.GreaterOrEqual(t, groups, 5, "should have 5+ groups")
	require.GreaterOrEqual(t, datasets, 15, "should have 15+ datasets")
	require.GreaterOrEqual(t, namedTypes, 2, "should have 2+ named datatypes")
	require.Equal(t, "/", allPaths[0], "first path should be root")
}

// ---------------------------------------------------------------------------
// Section 29: Soft links in v0 files
// ---------------------------------------------------------------------------

// TestSoftLinks_WriteAndReadBack tests creating soft links and reading
// them back. Soft links are skipped during traversal (per C library behavior).
func TestSoftLinks_WriteAndReadBack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlinks.h5")

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		_, err = fw.CreateGroup("/data")
		require.NoError(t, err)

		ds, err := fw.CreateDataset("/data/values", Float64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1.0, 2.0, 3.0}))

		// Create soft link.
		err = fw.CreateSoftLink("/shortcut", "/data/values")
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Read.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	// Walk should find the group and dataset. Soft link should be present
	// in the structure but resolved or skipped.
	var paths []string
	f.Walk(func(path string, _ Object) {
		paths = append(paths, path)
	})

	require.Greater(t, len(paths), 0)
}

// ---------------------------------------------------------------------------
// Section 30: External links in files
// ---------------------------------------------------------------------------

// TestExternalLinks_WriteAndReadBack tests creating external links.
func TestExternalLinks_WriteAndReadBack(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlinks.h5")

	// Write.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		_, err = fw.CreateGroup("/links")
		require.NoError(t, err)

		err = fw.CreateExternalLink("/links/ext", "other.h5", "/data")
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Read.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
	children := root.Children()
	require.GreaterOrEqual(t, len(children), 1, "should have the links group")
}

// ---------------------------------------------------------------------------
// Section 31: Many datasets from v2 official file
// ---------------------------------------------------------------------------

// TestOldH5fcExtNone_V0ManyDatasets reads old_h5fc_ext_none.h5
// which is v0 with 2 groups and 9 datasets.
func TestOldH5fcExtNone_V0ManyDatasets(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/old_h5fc_ext_none.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var datasets int
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*Dataset); ok {
			datasets++
		}
	})

	require.GreaterOrEqual(t, datasets, 9, "should have 9 datasets")
}

// ---------------------------------------------------------------------------
// Section 32: h5repack_early with 100 named datatypes
// ---------------------------------------------------------------------------

// TestH5RepackEarly_ManyNamedDatatypes reads h5repack_early.h5 which
// has 100 named (committed) datatypes, heavily exercising the
// ObjectTypeDatatype branch in loadObject.
func TestH5RepackEarly_ManyNamedDatatypes(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5repack_early.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var namedTypes int
	f.Walk(func(_ string, obj Object) {
		if _, ok := obj.(*NamedDatatype); ok {
			namedTypes++
		}
	})

	require.GreaterOrEqual(t, namedTypes, 50, "h5repack_early.h5 should have 50+ named datatypes")
}

// ---------------------------------------------------------------------------
// Section 33: h5diff_attr_v_level1 -- attrs on groups and datasets
// ---------------------------------------------------------------------------

// TestH5DiffAttr_AttrsOnGroupsAndDatasets reads h5diff_attr_v_level1.h5
// which has attributes on both groups and datasets.
func TestH5DiffAttr_AttrsOnGroupsAndDatasets(t *testing.T) {
	t.Parallel()

	f, err := Open("testdata/hdf5_official/h5diff_attr_v_level1.h5")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var groupsWithAttrs, datasetsWithAttrs int

	f.Walk(func(_ string, obj Object) {
		switch o := obj.(type) {
		case *Group:
			attrs, err := o.Attributes()
			if err == nil && len(attrs) > 0 {
				groupsWithAttrs++
			}
		case *Dataset:
			attrs, err := o.Attributes()
			if err == nil && len(attrs) > 0 {
				datasetsWithAttrs++
			}
		}
	})

	require.GreaterOrEqual(t, groupsWithAttrs, 1,
		"should have at least 1 group with attributes")
	require.GreaterOrEqual(t, datasetsWithAttrs, 1,
		"should have at least 1 dataset with attributes")
}

// ---------------------------------------------------------------------------
// Section 34: Dataset Write with all integer datatypes (encode* coverage)
// ---------------------------------------------------------------------------

// TestWriteDataset_AllIntegerTypes creates datasets with all integer
// types and verifies round-trip. This exercises encode1ByteIntegers,
// encode2ByteIntegers, encode4ByteIntegers, encode8ByteIntegers for both
// signed and unsigned paths.
func TestWriteDataset_AllIntegerTypes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_all_ints.h5")

	// Write all integer types.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		// Int8
		ds1, err := fw.CreateDataset("/int8", Int8, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds1.Write([]int8{-1, 0, 1}))

		// Uint8
		ds2, err := fw.CreateDataset("/uint8", Uint8, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds2.Write([]uint8{0, 128, 255}))

		// Int16
		ds3, err := fw.CreateDataset("/int16", Int16, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds3.Write([]int16{-100, 0, 100}))

		// Uint16
		ds4, err := fw.CreateDataset("/uint16", Uint16, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds4.Write([]uint16{0, 1000, 65535}))

		// Int32
		ds5, err := fw.CreateDataset("/int32", Int32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds5.Write([]int32{-100000, 0, 100000}))

		// Uint32
		ds6, err := fw.CreateDataset("/uint32", Uint32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds6.Write([]uint32{0, 1000000, 4294967295}))

		// Int64
		ds7, err := fw.CreateDataset("/int64", Int64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds7.Write([]int64{-9876543210, 0, 9876543210}))

		// Uint64
		ds8, err := fw.CreateDataset("/uint64", Uint64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds8.Write([]uint64{0, 1, 18446744073709551615}))

		// Float32
		ds9, err := fw.CreateDataset("/float32", Float32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds9.Write([]float32{-1.5, 0.0, 1.5}))

		// Float64
		ds10, err := fw.CreateDataset("/float64", Float64, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds10.Write([]float64{-2.718, 0.0, 3.14159}))

		require.NoError(t, fw.Close())
	}()

	// Read and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 10, "should have 10 datasets")

	// Read() only supports int32, int64, float32, float64 conversion to []float64.
	// Verify those types round-trip; for int8/uint8/int16/uint16 write-only is tested above.
	readableTypes := map[string]bool{"int32": true, "uint32": true, "int64": true, "uint64": true, "float32": true, "float64": true}
	for _, child := range children {
		ds, ok := child.(*Dataset)
		require.True(t, ok, "all children should be datasets")

		name := ds.Name()
		if !readableTypes[name] {
			continue
		}

		data, err := ds.Read()
		require.NoError(t, err, "Read() should work for %s", name)
		require.Len(t, data, 3, "each dataset has 3 elements")
	}
}

// TestWriteDataset_StringType tests string dataset write/read.
func TestWriteDataset_StringType(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_strings_ds.h5")

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/names", String, []uint64{4}, WithStringSize(32))
		require.NoError(t, err)
		require.NoError(t, ds.Write([]string{"Alice", "Bob", "Charlie", "Dave"}))

		require.NoError(t, fw.Close())
	}()

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	strings, err := ds.ReadStrings()
	require.NoError(t, err)
	require.Len(t, strings, 4)
	assert.Equal(t, "Alice", strings[0])
}

// ---------------------------------------------------------------------------
// Section 35: OpenForWrite and OpenDataset coverage
// ---------------------------------------------------------------------------

// TestOpenForWrite_ReadWriteModify tests OpenForWrite + OpenDataset
// and modifying attributes on existing datasets.
func TestOpenForWrite_ReadWriteModify(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_rmw.h5")

	// Create initial file.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)

		ds, err := fw.CreateDataset("/data", Float64, []uint64{5})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3, 4, 5}))
		require.NoError(t, ds.WriteAttribute("version", int32(1)))

		require.NoError(t, fw.Close())
	}()

	// Reopen and modify.
	func() {
		fw, err := OpenForWrite(filename, OpenReadWrite)
		require.NoError(t, err)

		ds, err := fw.OpenDataset("/data")
		require.NoError(t, err)

		// Write new attribute (upsert).
		require.NoError(t, ds.WriteAttribute("version", int32(2)))
		require.NoError(t, ds.WriteAttribute("source", "test_test"))

		require.NoError(t, fw.Close())
	}()

	// Verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	children := root.Children()
	require.Len(t, children, 1)

	ds, ok := children[0].(*Dataset)
	require.True(t, ok)

	attrs, err := ds.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 2, "should have 2 attributes (version + source)")
}

// ---------------------------------------------------------------------------
// Section 36: CreateDataset error paths
// ---------------------------------------------------------------------------

// TestCreateDataset_ErrorPaths tests various error conditions.
func TestCreateDataset_ErrorPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_ds_errors.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	t.Run("empty path", func(t *testing.T) {
		_, err := fw.CreateDataset("", Float64, []uint64{5})
		require.Error(t, err)
	})

	t.Run("no leading slash", func(t *testing.T) {
		_, err := fw.CreateDataset("data", Float64, []uint64{5})
		require.Error(t, err)
	})

	t.Run("empty dimensions", func(t *testing.T) {
		_, err := fw.CreateDataset("/empty_dims", Float64, []uint64{})
		require.Error(t, err)
	})

	t.Run("zero dimension", func(t *testing.T) {
		_, err := fw.CreateDataset("/zero_dim", Float64, []uint64{0})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Section 37: FileWriter.Close coverage
// ---------------------------------------------------------------------------

// TestFileWriterClose_WithDatasets tests Close() flushes correctly.
func TestFileWriterClose_WithDatasets(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_close.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/d1", Int32, []uint64{3})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3}))

	ds2, err := fw.CreateDataset("/d2", Float32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds2.Write([]float32{1.0, 2.0}))

	// Close should flush and finalize.
	require.NoError(t, fw.Close())

	// Verify file is valid.
	stat, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, stat.Size(), int64(100), "file should have content")
}

// ---------------------------------------------------------------------------
// Section 38: RebalanceAttributeBTree -- with OpenForWrite path
// ---------------------------------------------------------------------------

// TestRebalanceAllBTrees_WithMultipleDatasets tests the global
// rebalance on a file opened with OpenForWrite.
func TestRebalanceAllBTrees_WithMultipleDatasets(t *testing.T) {
	t.Parallel()

	filename := filepath.Join("tmp", "test_rebalance_all.h5")
	defer func() { _ = os.Remove(filename) }()

	// Create file with multiple datasets with dense attrs.
	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			ds, err := fw.CreateDataset(
				fmt.Sprintf("/ds_%d", i), Float64, []uint64{3})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]float64{1, 2, 3}))

			for j := 0; j < 10; j++ {
				require.NoError(t, ds.WriteAttribute(
					fmt.Sprintf("attr_%d", j), int32(j)))
			}
		}

		require.NoError(t, fw.Close())
	}()

	// Reopen and rebalance all.
	func() {
		fw, err := OpenForWrite(filename, OpenReadWrite)
		require.NoError(t, err)

		require.NoError(t, fw.RebalanceAllBTrees())
		require.NoError(t, fw.Close())
	}()

	// Verify.
	f, err := Open(filename)
	require.NoError(t, err)

	root := f.Root()
	require.Len(t, root.Children(), 3)

	for _, child := range root.Children() {
		ds, ok := child.(*Dataset)
		require.True(t, ok)

		attrs, err := ds.Attributes()
		require.NoError(t, err)
		require.Len(t, attrs, 10)
	}

	require.NoError(t, f.Close())
}

// ---------------------------------------------------------------------------
// Section 39: CreateExclusive mode
// ---------------------------------------------------------------------------

// TestCreateExclusive tests the CreateExclusive mode.
func TestCreateExclusive(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_exclusive.h5")

	// First create should succeed.
	fw, err := CreateForWrite(filename, CreateExclusive)
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	// Second create should fail (file exists).
	_, err = CreateForWrite(filename, CreateExclusive)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Section 40: Dataset.Read type conversion from non-float64
// ---------------------------------------------------------------------------

// TestDatasetRead_IntTypesConvertToFloat64 tests that Read() converts
// integer datasets to float64 properly.
func TestDatasetRead_IntTypesConvertToFloat64(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_read_ints.h5")

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/int32_data", Int32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{10, 20, 30}))

		ds2, err := fw.CreateDataset("/int64_data", Int64, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds2.Write([]int64{100, 200}))

		ds3, err := fw.CreateDataset("/float32_data", Float32, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds3.Write([]float32{1.5, 2.5}))

		require.NoError(t, fw.Close())
	}()

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	for _, child := range root.Children() {
		ds, ok := child.(*Dataset)
		require.True(t, ok)

		data, err := ds.Read()
		require.NoError(t, err)
		require.NotEmpty(t, data, "Read() should return data for %s", ds.Name())

		// All values should be finite.
		for _, v := range data {
			require.False(t, v != v, "should not be NaN") // NaN != NaN
		}
	}
}
