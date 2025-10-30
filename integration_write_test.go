package hdf5

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFullWriteWorkflow_GroupsAndDatasets tests creating groups and datasets together.
// This demonstrates the full write infrastructure even though linking is not yet implemented.
func TestFullWriteWorkflow_GroupsAndDatasets(t *testing.T) {
	testFile := "test_full_write_workflow.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Create file
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create groups (MVP: not linked, but structure is valid)
	err = fw.CreateGroup("/data")
	require.NoError(t, err)

	// Create datasets (MVP: not linked, but structure is valid)
	ds1, err := fw.CreateDataset("/temperature", Int32, []uint64{10})
	require.NoError(t, err)

	data1 := []int32{20, 21, 22, 23, 24, 25, 26, 27, 28, 29}
	err = ds1.Write(data1)
	require.NoError(t, err)

	ds2, err := fw.CreateDataset("/pressure", Float64, []uint64{5})
	require.NoError(t, err)

	data2 := []float64{1.1, 2.2, 3.3, 4.4, 5.5}
	err = ds2.Write(data2)
	require.NoError(t, err)

	// Close file
	err = fw.Close()
	require.NoError(t, err)

	// Reopen and verify file is valid HDF5
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Verify root group exists
	root := f.Root()
	require.NotNil(t, root)

	// MVP Limitation: Datasets and groups are not linked to root yet
	// So we can't access them via root.Children() or f.OpenDataset()
	// But the file structure is valid and can be reopened
}

// TestFullWriteWorkflow_MultipleDatasets tests writing multiple datasets with different types.
func TestFullWriteWorkflow_MultipleDatasets(t *testing.T) {
	testFile := "test_multiple_datasets.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create datasets with different types
	tests := []struct {
		name string
		dt   Datatype
		dims []uint64
		data interface{}
	}{
		{
			name: "int8_dataset",
			dt:   Int8,
			dims: []uint64{5},
			data: []int8{1, 2, 3, 4, 5},
		},
		{
			name: "uint32_dataset",
			dt:   Uint32,
			dims: []uint64{3},
			data: []uint32{100, 200, 300},
		},
		{
			name: "float32_dataset",
			dt:   Float32,
			dims: []uint64{4},
			data: []float32{1.5, 2.5, 3.5, 4.5},
		},
		{
			name: "2d_matrix",
			dt:   Int32,
			dims: []uint64{2, 3},
			data: []int32{1, 2, 3, 4, 5, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := fw.CreateDataset("/"+tt.name, tt.dt, tt.dims)
			require.NoError(t, err)

			err = ds.Write(tt.data)
			require.NoError(t, err)
		})
	}

	// Close and verify
	err = fw.Close()
	require.NoError(t, err)

	// Verify file is valid
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	require.NotNil(t, f.Root())
}

// TestFullWriteWorkflow_BinaryStructure verifies the binary file structure.
func TestFullWriteWorkflow_BinaryStructure(t *testing.T) {
	testFile := "test_binary_structure.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	// Create a group
	err = fw.CreateGroup("/mygroup")
	require.NoError(t, err)

	// Create a dataset
	ds, err := fw.CreateDataset("/mydata", Float64, []uint64{3})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read raw file and verify structures
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)

	// Must contain HDF5 signature
	require.Equal(t, []byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}, data[0:8], "HDF5 signature")

	// Must contain OHDR (object headers)
	require.Contains(t, string(data), "OHDR", "object headers")

	// Must contain group structures (from CreateGroup)
	require.Contains(t, string(data), "HEAP", "local heap for group names")
	require.Contains(t, string(data), "TREE", "B-tree for group indexing")
	require.Contains(t, string(data), "SNOD", "symbol table node")

	// File size should be reasonable (not huge)
	require.Less(t, len(data), 10*1024, "file size should be < 10KB for this small file")
}

// TestFullWriteWorkflow_ErrorCases tests error handling.
func TestFullWriteWorkflow_ErrorCases(t *testing.T) {
	testFile := "test_error_cases.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Error: Create dataset with mismatched data
	ds, err := fw.CreateDataset("/test", Int32, []uint64{5})
	require.NoError(t, err)

	// Wrong type
	err = ds.Write([]float64{1.0, 2.0, 3.0})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported data type")

	// Wrong size
	err = ds.Write([]int32{1, 2, 3}) // Expected 5, got 3
	require.Error(t, err)
	require.Contains(t, err.Error(), "data size mismatch")
}

// TestFullWriteWorkflow_CloseAndReopen tests write-close-reopen cycle.
func TestFullWriteWorkflow_CloseAndReopen(t *testing.T) {
	testFile := "test_close_reopen.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Write phase
	{
		fw, err := CreateForWrite(testFile, CreateTruncate)
		require.NoError(t, err)

		// Create group
		err = fw.CreateGroup("/data")
		require.NoError(t, err)

		// Create dataset
		ds, err := fw.CreateDataset("/values", Int32, []uint64{5})
		require.NoError(t, err)

		err = ds.Write([]int32{10, 20, 30, 40, 50})
		require.NoError(t, err)

		// Close
		err = fw.Close()
		require.NoError(t, err)
	}

	// Read phase
	{
		f, err := Open(testFile)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		// Verify file structure
		root := f.Root()
		require.NotNil(t, root)

		// MVP: Can't access datasets/groups yet (not linked)
		// But file is valid and can be reopened
	}

	// Verify file still exists and is valid
	info, err := os.Stat(testFile)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(100), "file should not be empty")
}

// TestFullWriteWorkflow_WithDiscovery tests full write-close-reopen-discover cycle.
// This is the comprehensive test that validates Component 3 is complete.
func TestFullWriteWorkflow_WithDiscovery(t *testing.T) {
	testFile := "test_discovery.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Write phase
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	// Create groups
	err = fw.CreateGroup("/data")
	require.NoError(t, err)

	// Create datasets
	ds1, err := fw.CreateDataset("/temperature", Int32, []uint64{10})
	require.NoError(t, err)
	err = ds1.Write([]int32{20, 21, 22, 23, 24, 25, 26, 27, 28, 29})
	require.NoError(t, err)

	ds2, err := fw.CreateDataset("/pressure", Float64, []uint64{5})
	require.NoError(t, err)
	err = ds2.Write([]float64{1.1, 2.2, 3.3, 4.4, 5.5})
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read phase - verify discovery
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Verify root group exists and has children
	root := f.Root()
	require.NotNil(t, root)

	// Verify can list children (discovery works!)
	children := root.Children()
	require.Len(t, children, 3, "should have exactly 3 children")

	// Check child names
	childNames := make([]string, len(children))
	for i, child := range children {
		childNames[i] = child.Name()
	}
	require.Contains(t, childNames, "temperature", "temperature dataset should be discoverable")
	require.Contains(t, childNames, "pressure", "pressure dataset should be discoverable")
	require.Contains(t, childNames, "data", "data group should be discoverable")

	// Find and verify temperature dataset
	var tempDataset *Dataset
	for _, child := range children {
		if child.Name() == "temperature" {
			ds, ok := child.(*Dataset)
			require.True(t, ok, "temperature should be a Dataset")
			tempDataset = ds
			break
		}
	}
	require.NotNil(t, tempDataset, "temperature dataset should be found")

	// Verify can read data back
	tempData, err := tempDataset.Read()
	require.NoError(t, err)

	// Convert to int32 (Read returns []float64)
	tempInt32 := make([]int32, len(tempData))
	for i, v := range tempData {
		tempInt32[i] = int32(v)
	}
	require.Equal(t, []int32{20, 21, 22, 23, 24, 25, 26, 27, 28, 29}, tempInt32)

	// Find and verify pressure dataset
	var presDataset *Dataset
	for _, child := range children {
		if child.Name() == "pressure" {
			ds, ok := child.(*Dataset)
			require.True(t, ok, "pressure should be a Dataset")
			presDataset = ds
			break
		}
	}
	require.NotNil(t, presDataset, "pressure dataset should be found")

	presData, err := presDataset.Read()
	require.NoError(t, err)
	require.Equal(t, []float64{1.1, 2.2, 3.3, 4.4, 5.5}, presData)

	// Find and verify data group
	var dataGroup *Group
	for _, child := range children {
		if child.Name() == "data" {
			grp, ok := child.(*Group)
			require.True(t, ok, "data should be a Group")
			dataGroup = grp
			break
		}
	}
	require.NotNil(t, dataGroup, "data group should be found")
	require.Equal(t, "data", dataGroup.Name())

	// Verify Walk traverses the tree
	visited := []string{}
	f.Walk(func(path string, _ Object) {
		visited = append(visited, path)
	})
	require.Contains(t, visited, "/")
	require.Contains(t, visited, "/data/")
	require.Contains(t, visited, "/temperature")
	require.Contains(t, visited, "/pressure")
}

// TestFullWriteWorkflow_MultipleObjects tests creating many objects.
func TestFullWriteWorkflow_MultipleObjects(t *testing.T) {
	testFile := "test_multiple_objects.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	// Create 10 datasets
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("/dataset_%d", i)
		ds, err := fw.CreateDataset(name, Int32, []uint64{3})
		require.NoError(t, err)

		data := []int32{int32(i), int32(i + 1), int32(i + 2)}
		err = ds.Write(data)
		require.NoError(t, err)
	}

	err = fw.Close()
	require.NoError(t, err)

	// Verify all datasets are discoverable
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	children := f.Root().Children()
	require.Equal(t, 10, len(children), "should have 10 datasets")

	// Create map of datasets by name for easier lookup
	datasetMap := make(map[string]*Dataset)
	for _, child := range children {
		ds, ok := child.(*Dataset)
		require.True(t, ok, "child should be a Dataset")
		datasetMap[child.Name()] = ds
	}

	// Verify can read each dataset
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("dataset_%d", i)
		ds, found := datasetMap[name]
		require.True(t, found, "dataset %s should be found", name)

		data, err := ds.Read()
		require.NoError(t, err)

		// Convert to int32
		dataInt32 := make([]int32, len(data))
		for j, v := range data {
			dataInt32[j] = int32(v)
		}
		require.Equal(t, []int32{int32(i), int32(i + 1), int32(i + 2)}, dataInt32)
	}
}
