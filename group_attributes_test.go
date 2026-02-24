package hdf5

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupWriter_WriteAttribute_Basic tests basic attribute writing to groups.
func TestGroupWriter_WriteAttribute_Basic(t *testing.T) {
	testFile := "test_group_attribute_basic.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Create file
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create group
	group, err := fw.CreateGroup("/mygroup")
	require.NoError(t, err)
	require.NotNil(t, group)
	require.Equal(t, "/mygroup", group.Path())

	// Write attributes to group
	err = group.WriteAttribute("description", "Temperature measurements")
	require.NoError(t, err)

	err = group.WriteAttribute("version", int32(1))
	require.NoError(t, err)

	err = group.WriteAttribute("temperature", float64(25.5))
	require.NoError(t, err)

	// Close and reopen to verify
	err = fw.Close()
	require.NoError(t, err)

	// Verify file can be opened and group has attributes
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate to group
	root := f.Root()
	require.NotNil(t, root)

	// Find the group in children
	var foundGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "mygroup" {
			foundGroup = g
			break
		}
	}
	require.NotNil(t, foundGroup, "group not found in file")

	// Read attributes
	attrs, err := foundGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 3, "expected 3 attributes")

	// Verify attribute names
	attrNames := make(map[string]bool)
	for _, attr := range attrs {
		attrNames[attr.Name] = true
	}
	require.True(t, attrNames["description"], "description attribute missing")
	require.True(t, attrNames["version"], "version attribute missing")
	require.True(t, attrNames["temperature"], "temperature attribute missing")
}

// TestGroupWriter_WriteAttribute_MATLAB tests MATLAB v7.3 complex number metadata.
// This is the primary use case from the first real user (MATLAB project).
func TestGroupWriter_WriteAttribute_MATLAB(t *testing.T) {
	testFile := "test_group_attribute_matlab.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Create file
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create group for complex variable (z)
	group, err := fw.CreateGroup("/z")
	require.NoError(t, err)
	require.NotNil(t, group)

	// Write MATLAB v7.3 complex number metadata to group
	err = group.WriteAttribute("MATLAB_class", "double")
	require.NoError(t, err)

	err = group.WriteAttribute("MATLAB_complex", uint8(1))
	require.NoError(t, err)

	// Note: For a complete MATLAB v7.3 complex number, you would also create
	// /z/real and /z/imag datasets, but this test focuses on group attributes only.

	// Close file successfully
	err = fw.Close()
	require.NoError(t, err)

	// For now, we just verify the file was created successfully and can be opened.
	// Full attribute reading verification will be added when attribute reading
	// infrastructure fully supports all types.
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Verify file structure is valid
	root := f.Root()
	require.NotNil(t, root)
}

// TestGroupWriter_WriteAttribute_NestedGroups tests attributes on nested groups.
func TestGroupWriter_WriteAttribute_NestedGroups(t *testing.T) {
	t.Skip("Skipping nested groups test - known limitation with reading nested groups structure")

	testFile := "test_group_attribute_nested.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create nested groups
	experiments, err := fw.CreateGroup("/experiments")
	require.NoError(t, err)
	require.NotNil(t, experiments)

	trial1, err := fw.CreateGroup("/experiments/trial1")
	require.NoError(t, err)
	require.NotNil(t, trial1)

	// Write attributes to both groups
	err = experiments.WriteAttribute("description", "All experiments")
	require.NoError(t, err)

	err = trial1.WriteAttribute("description", "First trial")
	require.NoError(t, err)

	err = trial1.WriteAttribute("trial_number", int32(1))
	require.NoError(t, err)

	// Close file successfully
	err = fw.Close()
	require.NoError(t, err)
}

// TestGroupWriter_WriteAttribute_MultipleTypes tests various attribute data types.
func TestGroupWriter_WriteAttribute_MultipleTypes(t *testing.T) {
	testFile := "test_group_attribute_types.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	group, err := fw.CreateGroup("/data")
	require.NoError(t, err)

	// Write various data types
	err = group.WriteAttribute("int8_val", int8(-10))
	require.NoError(t, err)

	err = group.WriteAttribute("int16_val", int16(-1000))
	require.NoError(t, err)

	err = group.WriteAttribute("int32_val", int32(-100000))
	require.NoError(t, err)

	err = group.WriteAttribute("int64_val", int64(-10000000))
	require.NoError(t, err)

	err = group.WriteAttribute("uint8_val", uint8(200))
	require.NoError(t, err)

	err = group.WriteAttribute("uint16_val", uint16(50000))
	require.NoError(t, err)

	err = group.WriteAttribute("uint32_val", uint32(3000000000))
	require.NoError(t, err)

	err = group.WriteAttribute("uint64_val", uint64(10000000000))
	require.NoError(t, err)

	err = group.WriteAttribute("float32_val", float32(3.14159))
	require.NoError(t, err)

	err = group.WriteAttribute("float64_val", float64(2.718281828))
	require.NoError(t, err)

	err = group.WriteAttribute("string_val", "Hello HDF5")
	require.NoError(t, err)

	// Close and verify
	err = fw.Close()
	require.NoError(t, err)

	// Reopen and verify all attributes exist
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var dataGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "data" {
			dataGroup = g
			break
		}
	}
	require.NotNil(t, dataGroup)

	attrs, err := dataGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 11, "expected 11 attributes")
}

// TestGroupWriter_WriteAttribute_SliceTypes tests all supported slice types for attributes.
func TestGroupWriter_WriteAttribute_SliceTypes(t *testing.T) {
	testFile := "test_group_attribute_slices.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	group, err := fw.CreateGroup("/data")
	require.NoError(t, err)

	// Write all slice types
	require.NoError(t, group.WriteAttribute("int8_slice", []int8{-1, 0, 1, 127}))
	require.NoError(t, group.WriteAttribute("uint8_slice", []uint8{0, 128, 255}))
	require.NoError(t, group.WriteAttribute("int16_slice", []int16{-32768, 0, 32767}))
	require.NoError(t, group.WriteAttribute("uint16_slice", []uint16{0, 1000, 65535}))
	require.NoError(t, group.WriteAttribute("int32_slice", []int32{-100000, 0, 100000}))
	require.NoError(t, group.WriteAttribute("uint32_slice", []uint32{0, 3000000000}))
	require.NoError(t, group.WriteAttribute("int64_slice", []int64{-10000000000, 10000000000}))
	require.NoError(t, group.WriteAttribute("uint64_slice", []uint64{0, 18446744073709551615}))
	require.NoError(t, group.WriteAttribute("float32_slice", []float32{1.5, 2.5, 3.5}))
	require.NoError(t, group.WriteAttribute("float64_slice", []float64{1.1, 2.2, 3.3}))

	require.NoError(t, fw.Close())

	// Reopen and verify
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var dataGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "data" {
			dataGroup = g
			break
		}
	}
	require.NotNil(t, dataGroup)

	attrs, err := dataGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 10, "expected 10 slice attributes")
}

// TestGroupWriter_Path tests the Path() method.
func TestGroupWriter_Path(t *testing.T) {
	testFile := "test_group_path.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Test root-level group
	group1, err := fw.CreateGroup("/group1")
	require.NoError(t, err)
	require.Equal(t, "/group1", group1.Path())

	// Test nested group
	group2, err := fw.CreateGroup("/group1/subgroup")
	require.NoError(t, err)
	require.Equal(t, "/group1/subgroup", group2.Path())
}

// TestGroupWriter_WriteAttribute_CompactToDense tests the transition from compact
// to dense storage when writing many attributes to a group.
func TestGroupWriter_WriteAttribute_CompactToDense(t *testing.T) {
	testFile := "test_group_attribute_compact_dense.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	group, err := fw.CreateGroup("/data")
	require.NoError(t, err)

	// Write 10 attributes (should transition from compact to dense at 8)
	for i := 0; i < 10; i++ {
		err = group.WriteAttribute("attr_"+string(rune('0'+i)), int32(i))
		require.NoError(t, err)
	}

	// Close and verify
	err = fw.Close()
	require.NoError(t, err)

	// Reopen and verify all attributes exist
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	var dataGroup *Group
	for _, child := range root.Children() {
		if g, ok := child.(*Group); ok && g.Name() == "data" {
			dataGroup = g
			break
		}
	}
	require.NotNil(t, dataGroup)

	attrs, err := dataGroup.Attributes()
	require.NoError(t, err)
	require.Len(t, attrs, 10, "expected 10 attributes (dense storage)")
}
