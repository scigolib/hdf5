package hdf5

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ReadStrings tests
// ---------------------------------------------------------------------------

// TestReadStrings_FixedLength writes fixed-length strings and reads them back.
func TestReadStrings_FixedLength(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_strings_fixed.h5")

	// Write phase
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/words", String, []uint64{4}, WithStringSize(12))
	require.NoError(t, err)

	err = ds.Write([]string{"alpha", "beta", "gamma", "delta"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read phase
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Len(t, children, 1)

	dataset, ok := children[0].(*Dataset)
	require.True(t, ok)

	result, err := dataset.ReadStrings()
	require.NoError(t, err)
	require.Len(t, result, 4)

	assert.Equal(t, "alpha", result[0])
	assert.Equal(t, "beta", result[1])
	assert.Equal(t, "gamma", result[2])
	assert.Equal(t, "delta", result[3])
}

// TestReadStrings_EmptyStrings writes empty strings and verifies they round-trip.
func TestReadStrings_EmptyStrings(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_strings_empty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/empty", String, []uint64{3}, WithStringSize(8))
	require.NoError(t, err)

	err = ds.Write([]string{"", "", ""})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/empty" {
			if d, ok := obj.(*Dataset); ok {
				found = d
			}
		}
	})
	require.NotNil(t, found)

	result, err := found.ReadStrings()
	require.NoError(t, err)
	require.Len(t, result, 3)

	for i, s := range result {
		assert.Equal(t, "", s, "string %d should be empty", i)
	}
}

// TestReadStrings_MixedLengths writes strings of varying length within the same dataset.
func TestReadStrings_MixedLengths(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_strings_mixed.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/mixed", String, []uint64{3}, WithStringSize(16))
	require.NoError(t, err)

	err = ds.Write([]string{"a", "hello world", "Go"})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/mixed" {
			if d, ok := obj.(*Dataset); ok {
				found = d
			}
		}
	})
	require.NotNil(t, found)

	result, err := found.ReadStrings()
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.Equal(t, "a", result[0])
	assert.Equal(t, "hello world", result[1])
	assert.Equal(t, "Go", result[2])
}

// ---------------------------------------------------------------------------
// ReadCompound tests
// ---------------------------------------------------------------------------

// TestReadCompound_Simple writes a compound dataset with int32+float32 and reads it back.
func TestReadCompound_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_compound_simple.h5")

	// Build compound type: struct { int32 id; float32 value }
	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)
	float32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 4)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "id", Offset: 0, Type: int32Type},
		{Name: "value", Offset: 4, Type: float32Type},
	}
	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)

	// Write
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateCompoundDataset("/records", compoundType, []uint64{3})
	require.NoError(t, err)

	// Encode raw data: 3 records of {id, value}
	data := make([]byte, 3*8)
	// Record 0: id=10, value=1.5
	binary.LittleEndian.PutUint32(data[0:4], 10)
	binary.LittleEndian.PutUint32(data[4:8], 0x3FC00000) // 1.5f
	// Record 1: id=20, value=2.5
	binary.LittleEndian.PutUint32(data[8:12], 20)
	binary.LittleEndian.PutUint32(data[12:16], 0x40200000) // 2.5f
	// Record 2: id=30, value=3.5
	binary.LittleEndian.PutUint32(data[16:20], 30)
	binary.LittleEndian.PutUint32(data[20:24], 0x40600000) // 3.5f

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/records" {
			if d, ok := obj.(*Dataset); ok {
				found = d
			}
		}
	})
	require.NotNil(t, found)

	compounds, err := found.ReadCompound()
	require.NoError(t, err)
	require.Len(t, compounds, 3)

	// Verify field values (compound reader returns numeric types)
	assert.Equal(t, int32(10), compounds[0]["id"])
	assert.InDelta(t, float32(1.5), compounds[0]["value"], 1e-6)
	assert.Equal(t, int32(20), compounds[1]["id"])
	assert.InDelta(t, float32(2.5), compounds[1]["value"], 1e-6)
	assert.Equal(t, int32(30), compounds[2]["id"])
	assert.InDelta(t, float32(3.5), compounds[2]["value"], 1e-6)
}

// TestReadCompound_MixedTypes writes a compound dataset with int32+float64+int64 fields.
func TestReadCompound_MixedTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "read_compound_mixed.h5")

	int32Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 4)
	require.NoError(t, err)
	float64Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFloat, 8)
	require.NoError(t, err)
	int64Type, err := core.CreateBasicDatatypeMessage(core.DatatypeFixed, 8)
	require.NoError(t, err)

	fields := []core.CompoundFieldDef{
		{Name: "count", Offset: 0, Type: int32Type},
		{Name: "weight", Offset: 4, Type: float64Type},
		{Name: "flag", Offset: 12, Type: int64Type},
	}
	compoundType, err := core.CreateCompoundTypeFromFields(fields)
	require.NoError(t, err)
	require.Equal(t, uint32(20), compoundType.Size)

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateCompoundDataset("/mixed_compound", compoundType, []uint64{2})
	require.NoError(t, err)

	// Record size = 20 bytes: int32(4) + float64(8) + int64(8)
	data := make([]byte, 2*20)
	// Record 0: count=42, weight=3.14, flag=1
	binary.LittleEndian.PutUint32(data[0:4], 42)
	binary.LittleEndian.PutUint64(data[4:12], 0x40091EB851EB851F) // 3.14
	binary.LittleEndian.PutUint64(data[12:20], 1)
	// Record 1: count=99, weight=2.718, flag=7
	binary.LittleEndian.PutUint32(data[20:24], 99)
	binary.LittleEndian.PutUint64(data[24:32], 0x4005BE76C8B43958) // 2.718
	binary.LittleEndian.PutUint64(data[32:40], 7)

	err = ds.WriteRaw(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read back
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found *Dataset
	f.Walk(func(path string, obj Object) {
		if path == "/mixed_compound" {
			if d, ok := obj.(*Dataset); ok {
				found = d
			}
		}
	})
	require.NotNil(t, found)

	compounds, err := found.ReadCompound()
	require.NoError(t, err)
	require.Len(t, compounds, 2)

	assert.Equal(t, int32(42), compounds[0]["count"])
	assert.InDelta(t, 3.14, compounds[0]["weight"], 1e-10)
	assert.Equal(t, int64(1), compounds[0]["flag"])

	assert.Equal(t, int32(99), compounds[1]["count"])
	assert.InDelta(t, 2.718, compounds[1]["weight"], 1e-10)
	assert.Equal(t, int64(7), compounds[1]["flag"])
}

// ---------------------------------------------------------------------------
// NamedDatatype tests
// ---------------------------------------------------------------------------

// TestNamedDatatype_Accessor reads a file containing a named datatype and verifies the accessor.
func TestNamedDatatype_Accessor(t *testing.T) {
	// The official reference file memleak_H5O_dtype_decode_helper_H5Odtype.h5
	// should contain a named datatype. Open it and walk to find one.
	testFile := "testdata/reference/memleak_H5O_dtype_decode_helper_H5Odtype.h5"

	f, err := Open(testFile)
	if err != nil {
		t.Skipf("skipping: cannot open reference file %s: %v", testFile, err)
	}
	defer func() { _ = f.Close() }()

	var namedDT *NamedDatatype
	f.Walk(func(_ string, obj Object) {
		if nd, ok := obj.(*NamedDatatype); ok {
			namedDT = nd
		}
	})

	if namedDT == nil {
		t.Skip("no NamedDatatype found in reference file; skipping accessor test")
	}

	// Verify Name() returns non-empty string.
	assert.NotEmpty(t, namedDT.Name(), "NamedDatatype.Name() should not be empty")

	// Verify Datatype() returns non-nil.
	dt := namedDT.Datatype()
	assert.NotNil(t, dt, "NamedDatatype.Datatype() should not be nil")
}

// ---------------------------------------------------------------------------
// loadObject tests (via Open + Walk)
// ---------------------------------------------------------------------------

// TestLoadObject_DatasetType creates datasets, reopens the file and verifies
// that Walk delivers them as *Dataset objects.
func TestLoadObject_DatasetType(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "load_object_datasets.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds1, err := fw.CreateDataset("/temperature", Float64, []uint64{5})
	require.NoError(t, err)
	err = ds1.Write([]float64{1.0, 2.0, 3.0, 4.0, 5.0})
	require.NoError(t, err)

	ds2, err := fw.CreateDataset("/pressure", Int32, []uint64{3})
	require.NoError(t, err)
	err = ds2.Write([]int32{100, 200, 300})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	datasets := make(map[string]*Dataset)
	f.Walk(func(path string, obj Object) {
		if d, ok := obj.(*Dataset); ok {
			datasets[path] = d
		}
	})

	require.Contains(t, datasets, "/temperature")
	require.Contains(t, datasets, "/pressure")

	// Verify we can read from them
	tempData, err := datasets["/temperature"].Read()
	require.NoError(t, err)
	require.Len(t, tempData, 5)
	assert.InDelta(t, 1.0, tempData[0], 1e-10)
	assert.InDelta(t, 5.0, tempData[4], 1e-10)

	pressureData, err := datasets["/pressure"].Read()
	require.NoError(t, err)
	require.Len(t, pressureData, 3)
	assert.InDelta(t, 100.0, pressureData[0], 1e-10)
	assert.InDelta(t, 300.0, pressureData[2], 1e-10)
}

// TestLoadObject_GroupType creates nested groups, reopens and verifies they load as *Group.
func TestLoadObject_GroupType(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "load_object_groups.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/science")
	require.NoError(t, err)

	_, err = fw.CreateGroup("/science/physics")
	require.NoError(t, err)

	// Add a dataset in the nested group so we can verify the full tree
	ds, err := fw.CreateDataset("/science/physics/energy", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{9.8, 3.14, 2.71})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	groups := make(map[string]*Group)
	datasets := make(map[string]*Dataset)
	f.Walk(func(path string, obj Object) {
		switch v := obj.(type) {
		case *Group:
			groups[path] = v
		case *Dataset:
			datasets[path] = v
		}
	})

	// Root group always present
	require.Contains(t, groups, "/")

	// Nested groups (Walk appends trailing "/" for groups)
	require.Contains(t, groups, "/science/")
	require.Contains(t, groups, "/science/physics/")

	// Dataset in nested group
	require.Contains(t, datasets, "/science/physics/energy")

	// Verify data round-trip
	data, err := datasets["/science/physics/energy"].Read()
	require.NoError(t, err)
	require.Len(t, data, 3)
	assert.InDelta(t, 9.8, data[0], 1e-10)
}

// ---------------------------------------------------------------------------
// loadChildren tests
// ---------------------------------------------------------------------------

// TestLoadChildren_MultipleEntries creates a group with many children and verifies all are loaded.
func TestLoadChildren_MultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "load_children_multiple.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create 5 datasets at the root level
	names := []string{"/ds_a", "/ds_b", "/ds_c", "/ds_d", "/ds_e"}
	for _, name := range names {
		ds, err := fw.CreateDataset(name, Int32, []uint64{2})
		require.NoError(t, err)
		err = ds.Write([]int32{1, 2})
		require.NoError(t, err)
	}

	err = fw.Close()
	require.NoError(t, err)

	// Read
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Equal(t, len(names), len(children), "root should have %d children", len(names))

	// Collect child names
	childNames := make(map[string]bool)
	for _, child := range children {
		childNames[child.Name()] = true
	}

	for _, name := range names {
		// Strip leading slash for the object name
		short := name[1:] // e.g. "ds_a"
		assert.True(t, childNames[short], "child %q should be present", short)
	}
}

// TestLoadChildren_GroupsAndDatasets mixes groups and datasets under the same parent.
func TestLoadChildren_GroupsAndDatasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "load_children_mixed.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/grp1")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/grp2")
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/dataset1", Float64, []uint64{2})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.Equal(t, 3, len(children), "root should have 3 children (2 groups + 1 dataset)")

	var groupCount, datasetCount int
	for _, child := range children {
		switch child.(type) {
		case *Group:
			groupCount++
		case *Dataset:
			datasetCount++
		}
	}

	assert.Equal(t, 2, groupCount, "should have 2 groups")
	assert.Equal(t, 1, datasetCount, "should have 1 dataset")
}

// ---------------------------------------------------------------------------
// Group Attributes tests
// ---------------------------------------------------------------------------

// TestGroup_Attributes_Empty verifies that a group with no attributes returns an empty slice.
func TestGroup_Attributes_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_attrs_empty.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	_, err = fw.CreateGroup("/plain")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var grp *Group
	f.Walk(func(path string, obj Object) {
		// Walk appends trailing "/" for groups
		if path == "/plain/" {
			if g, ok := obj.(*Group); ok {
				grp = g
			}
		}
	})
	require.NotNil(t, grp, "group /plain/ should be found via Walk")

	attrs, err := grp.Attributes()
	require.NoError(t, err)
	assert.NotNil(t, attrs, "attributes should not be nil")
	assert.Empty(t, attrs, "group with no attributes should return empty slice")
}

// TestGroup_Attributes_RootGroup verifies the root group Attributes() call works.
func TestGroup_Attributes_RootGroup(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "group_attrs_root.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Just create a dataset so the file has content
	ds, err := fw.CreateDataset("/data", Int32, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]int32{1, 2, 3})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	attrs, err := root.Attributes()
	require.NoError(t, err)
	assert.NotNil(t, attrs, "root group attributes should not be nil")
}

// TestGroup_Attributes_WithReferenceFile reads a reference file known to have groups
// and verifies that Attributes() does not error.
func TestGroup_Attributes_WithReferenceFile(t *testing.T) {
	f, err := Open("testdata/with_groups.h5")
	if err != nil {
		t.Skipf("skipping: cannot open testdata/with_groups.h5: %v", err)
	}
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)

	attrs, err := root.Attributes()
	require.NoError(t, err)
	assert.NotNil(t, attrs, "root group attributes should not be nil (may be empty)")
}
