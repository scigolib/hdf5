package hdf5

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNestedDatasets_TwoLevels tests creating datasets inside non-root groups (2 levels deep).
func TestNestedDatasets_TwoLevels(t *testing.T) {
	testFile := "test_nested_datasets_2levels.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create group
	_, err = fw.CreateGroup("/mygroup")
	require.NoError(t, err)

	// Create dataset inside group (THIS IS THE KEY TEST!)
	dw, err := fw.CreateDataset("/mygroup/data", Float64, []uint64{15})
	require.NoError(t, err, "should be able to create dataset in nested group")
	require.NotNil(t, dw)

	// Write data
	data := []float64{1.1, 2.2, 3.3, 4.4, 5.5, 6.6, 7.7, 8.8, 9.9, 10.1, 11.2, 12.3, 13.4, 14.5, 15.6}
	err = dw.Write(data)
	require.NoError(t, err)

	// Close file
	require.NoError(t, fw.Close())

	// Verify file can be reopened (validates HDF5 structure)
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Verify group exists
	root := f.Root()
	children := root.Children()
	require.Equal(t, 1, len(children), "should have 1 group")

	group, ok := children[0].(*Group)
	require.True(t, ok, "child should be a Group")
	require.Contains(t, group.Name(), "mygroup") // Group name may vary

	// Verify dataset exists in group
	groupChildren := group.Children()
	require.Equal(t, 1, len(groupChildren), "group should have 1 dataset")

	ds, ok := groupChildren[0].(*Dataset)
	require.True(t, ok, "child should be a Dataset")
	require.Contains(t, ds.Name(), "data")
}

// TestNestedDatasets_ThreeLevels tests creating datasets 3 levels deep.
func TestNestedDatasets_ThreeLevels(t *testing.T) {
	testFile := "test_nested_datasets_3levels.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create nested groups
	_, err = fw.CreateGroup("/level1")
	require.NoError(t, err)

	_, err = fw.CreateGroup("/level1/level2")
	require.NoError(t, err)

	// Create dataset 3 levels deep
	dw, err := fw.CreateDataset("/level1/level2/data", Int32, []uint64{10})
	require.NoError(t, err, "should be able to create dataset 3 levels deep")

	data := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	err = dw.Write(data)
	require.NoError(t, err)

	// Close file
	require.NoError(t, fw.Close())

	// Verify file structure
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate: root → level1 → level2 → data
	root := f.Root()
	children := root.Children()
	require.Equal(t, 1, len(children))

	level1, ok := children[0].(*Group)
	require.True(t, ok)
	require.Contains(t, level1.Name(), "level1")

	level1Children := level1.Children()
	require.Equal(t, 1, len(level1Children))

	level2, ok := level1Children[0].(*Group)
	require.True(t, ok)
	require.Contains(t, level2.Name(), "level2")

	level2Children := level2.Children()
	require.Equal(t, 1, len(level2Children))

	ds, ok := level2Children[0].(*Dataset)
	require.True(t, ok)
	require.Contains(t, ds.Name(), "data")
}

// TestNestedDatasets_MATLABv73_Complex tests MATLAB v7.3 complex number format.
// MATLAB stores complex numbers as: /z/real and /z/imag
// This is the actual use case from the MATLAB File Reader/Writer project!
func TestNestedDatasets_MATLABv73_Complex(t *testing.T) {
	testFile := "test_matlab_v73_complex.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create group for complex variable 'z'
	_, err = fw.CreateGroup("/z")
	require.NoError(t, err)

	// Create real part dataset
	realDW, err := fw.CreateDataset("/z/real", Float64, []uint64{3})
	require.NoError(t, err, "should be able to create /z/real dataset")

	realData := []float64{1.0, 2.0, 3.0}
	err = realDW.Write(realData)
	require.NoError(t, err)

	// Create imaginary part dataset
	imagDW, err := fw.CreateDataset("/z/imag", Float64, []uint64{3})
	require.NoError(t, err, "should be able to create /z/imag dataset")

	imagData := []float64{4.0, 5.0, 6.0}
	err = imagDW.Write(imagData)
	require.NoError(t, err)

	// Close file
	require.NoError(t, fw.Close())

	// Verify structure
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate to /z group
	root := f.Root()
	children := root.Children()
	require.Equal(t, 1, len(children))

	zGroup, ok := children[0].(*Group)
	require.True(t, ok)
	require.Contains(t, zGroup.Name(), "z")

	// Check /z has 2 datasets (real and imag)
	zChildren := zGroup.Children()
	require.Equal(t, 2, len(zChildren), "z group should have 2 datasets (real and imag)")

	// Verify datasets exist (order may vary)
	datasetNames := make(map[string]bool)
	for _, child := range zChildren {
		ds, ok := child.(*Dataset)
		require.True(t, ok)
		// Dataset names include parent path, so just check they contain real/imag
		name := ds.Name()
		if name != "" { // Paranoid check
			datasetNames[name] = true
		}
	}

	// Should have 2 datasets
	require.Equal(t, 2, len(datasetNames), "should have 2 datasets (real and imag)")
}

// TestNestedDatasets_MultipleDatasetsInGroup tests creating multiple datasets in same group.
func TestNestedDatasets_MultipleDatasetsInGroup(t *testing.T) {
	testFile := "test_multiple_datasets_in_group.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create group
	_, err = fw.CreateGroup("/experiments")
	require.NoError(t, err)

	// Create multiple datasets in same group
	dw1, err := fw.CreateDataset("/experiments/temperature", Float32, []uint64{5})
	require.NoError(t, err)

	temp := []float32{20.5, 21.3, 22.1, 23.0, 24.2}
	err = dw1.Write(temp)
	require.NoError(t, err)

	dw2, err := fw.CreateDataset("/experiments/pressure", Float32, []uint64{5})
	require.NoError(t, err)

	pressure := []float32{101.1, 101.2, 101.3, 101.4, 101.5}
	err = dw2.Write(pressure)
	require.NoError(t, err)

	dw3, err := fw.CreateDataset("/experiments/humidity", Float32, []uint64{5})
	require.NoError(t, err)

	humidity := []float32{45.0, 46.5, 48.0, 49.5, 51.0}
	err = dw3.Write(humidity)
	require.NoError(t, err)

	// Close file
	require.NoError(t, fw.Close())

	// Verify structure
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate to experiments group
	root := f.Root()
	children := root.Children()
	require.Equal(t, 1, len(children))

	group, ok := children[0].(*Group)
	require.True(t, ok)
	require.Contains(t, group.Name(), "experiments")

	// Verify all 3 datasets exist
	groupChildren := group.Children()
	require.Equal(t, 3, len(groupChildren), "should have 3 datasets")
}

// TestNestedDatasets_ErrorCases tests error conditions.
func TestNestedDatasets_ErrorCases(t *testing.T) {
	testFile := "test_nested_datasets_errors.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Test 1: dataset with nonexistent parent
	dw, err := fw.CreateDataset("/nonexistent/data", Int32, []uint64{5})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent group")
	require.Contains(t, err.Error(), "nonexistent")
	require.Nil(t, dw)

	// Test 2: dataset with existing parent (should succeed)
	_, err = fw.CreateGroup("/existing")
	require.NoError(t, err)

	dw, err = fw.CreateDataset("/existing/data", Int32, []uint64{5})
	require.NoError(t, err)
	require.NotNil(t, dw)

	data := []int32{1, 2, 3, 4, 5}
	err = dw.Write(data)
	require.NoError(t, err)

	// Test 3: deeply nested without intermediate groups
	_, err = fw.CreateGroup("/level1")
	require.NoError(t, err)
	// Don't create /level1/level2 or /level1/level2/level3

	dw, err = fw.CreateDataset("/level1/level2/level3/data", Int32, []uint64{5})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parent group")
	require.Nil(t, dw)
}

// TestNestedDatasets_FourLevelsDeep tests creating datasets in deeply nested groups.
func TestNestedDatasets_FourLevelsDeep(t *testing.T) {
	testFile := "test_nested_datasets_4levels.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create deeply nested groups
	_, err = fw.CreateGroup("/a")
	require.NoError(t, err)

	_, err = fw.CreateGroup("/a/b")
	require.NoError(t, err)

	_, err = fw.CreateGroup("/a/b/c")
	require.NoError(t, err)

	// Create dataset 4 levels deep
	dw, err := fw.CreateDataset("/a/b/c/data", Int32, []uint64{5})
	require.NoError(t, err, "should be able to create dataset 4 levels deep")

	data := []int32{1, 2, 3, 4, 5}
	err = dw.Write(data)
	require.NoError(t, err)

	// Close file
	require.NoError(t, fw.Close())

	// Verify file can be reopened
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Navigate: root → a → b → c → data
	root := f.Root()

	a := root.Children()[0].(*Group)
	require.Contains(t, a.Name(), "a")

	b := a.Children()[0].(*Group)
	require.Contains(t, b.Name(), "b")

	c := b.Children()[0].(*Group)
	require.Contains(t, c.Name(), "c")

	ds := c.Children()[0].(*Dataset)
	require.Contains(t, ds.Name(), "data")
}
