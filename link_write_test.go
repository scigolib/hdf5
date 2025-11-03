package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateHardLink_BasicCreation(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_hardlink.h5")

	// Create file with dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create /data group
	_, err = fw.CreateGroup("/data")
	require.NoError(t, err)

	// Create a dataset
	ds, err := fw.CreateDataset("/data/original", Float64, []uint64{5})
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Write data to dataset
	data := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	err = ds.Write(data)
	require.NoError(t, err)

	// Create hard link to dataset
	err = fw.CreateHardLink("/data/link_to_original", "/data/original")
	require.NoError(t, err, "Hard link creation should succeed")
}

func TestCreateHardLink_ReferenceCount(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_refcount.h5")

	// Create file with dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create a dataset (refcount = 1)
	ds, err := fw.CreateDataset("/dataset1", Float64, []uint64{3})
	require.NoError(t, err)

	// Write data
	data := []float64{1.0, 2.0, 3.0}
	err = ds.Write(data)
	require.NoError(t, err)

	// Get dataset address
	datasetAddr, err := fw.resolveObjectAddress("/dataset1")
	require.NoError(t, err)

	// Read object header
	oh1, err := core.ReadObjectHeader(fw.writer, datasetAddr, fw.file.sb)
	require.NoError(t, err)
	initialRefCount := oh1.GetReferenceCount()
	t.Logf("Initial refcount: %d", initialRefCount)
	assert.Equal(t, uint32(1), initialRefCount, "New object should have refcount = 1")

	// Create first hard link (refcount = 2)
	err = fw.CreateHardLink("/link1", "/dataset1")
	require.NoError(t, err)

	// Verify refcount increased
	oh2, err := core.ReadObjectHeader(fw.writer, datasetAddr, fw.file.sb)
	require.NoError(t, err)
	refCount2 := oh2.GetReferenceCount()
	t.Logf("After first link: %d", refCount2)
	assert.Equal(t, uint32(2), refCount2, "Reference count should be 2 after first hard link")

	// Create second hard link (refcount = 3)
	err = fw.CreateHardLink("/link2", "/dataset1")
	require.NoError(t, err)

	// Verify refcount increased again
	oh3, err := core.ReadObjectHeader(fw.writer, datasetAddr, fw.file.sb)
	require.NoError(t, err)
	refCount3 := oh3.GetReferenceCount()
	t.Logf("After second link: %d", refCount3)
	assert.Equal(t, uint32(3), refCount3, "Reference count should be 3 after second hard link")
}

func TestCreateHardLink_DifferentGroups(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_links_groups.h5")

	// Create file with two groups
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create two groups
	_, err = fw.CreateGroup("/group1")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/group2")
	require.NoError(t, err)

	// Create dataset in group1
	ds, err := fw.CreateDataset("/group1/dataset", Int32, []uint64{4})
	require.NoError(t, err)

	data := []int32{10, 20, 30, 40}
	err = ds.Write(data)
	require.NoError(t, err)

	// Get dataset address
	datasetAddr, err := fw.resolveObjectAddress("/group1/dataset")
	require.NoError(t, err)

	// Create hard link in group2
	err = fw.CreateHardLink("/group2/link_to_dataset", "/group1/dataset")
	require.NoError(t, err)

	// Verify link points to same object
	linkAddr, err := fw.resolveObjectAddress("/group2/link_to_dataset")
	require.NoError(t, err)
	assert.Equal(t, datasetAddr, linkAddr, "Link should point to same object address")

	// Verify refcount is 2
	oh, err := core.ReadObjectHeader(fw.writer, datasetAddr, fw.file.sb)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), oh.GetReferenceCount(), "Object should have 2 links")
}

func TestCreateHardLink_ToGroup(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_link_group.h5")

	// Create file with group
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create group
	_, err = fw.CreateGroup("/mygroup")
	require.NoError(t, err)

	// Get group address
	groupAddr, err := fw.resolveObjectAddress("/mygroup")
	require.NoError(t, err)

	// Create hard link to group
	err = fw.CreateHardLink("/group_link", "/mygroup")
	require.NoError(t, err)

	// Verify link points to same object
	linkAddr, err := fw.resolveObjectAddress("/group_link")
	require.NoError(t, err)
	assert.Equal(t, groupAddr, linkAddr, "Link should point to same group address")

	// Verify refcount is 2
	oh, err := core.ReadObjectHeader(fw.writer, groupAddr, fw.file.sb)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), oh.GetReferenceCount(), "Group should have 2 links")
}

func TestCreateHardLink_InvalidTarget(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_invalid_target.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Try to create link to non-existent target
	err = fw.CreateHardLink("/link", "/does/not/exist")
	assert.Error(t, err, "Should fail when target does not exist")
	assert.Contains(t, err.Error(), "not found", "Error should mention target not found")
}

func TestCreateHardLink_InvalidLinkPath(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_invalid_link.h5")

	// Create file with dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateDataset("/dataset", Float64, []uint64{2})
	require.NoError(t, err)

	data := []float64{1.0, 2.0}
	err = ds.Write(data)
	require.NoError(t, err)

	// Test various invalid link paths
	tests := []struct {
		name     string
		linkPath string
		errMsg   string
	}{
		{"empty path", "", "path cannot be empty"},
		{"no leading slash", "link", "must start with '/'"},
		{"root path", "/", "cannot create link to root"},
		{"consecutive slashes", "/group//link", "consecutive slashes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateHardLink(tt.linkPath, "/dataset")
			assert.Error(t, err, "Should fail for invalid link path")
			assert.Contains(t, err.Error(), tt.errMsg, "Error message should be descriptive")
		})
	}
}

func TestCreateHardLink_ParentNotExists(t *testing.T) {
	// Create temporary file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_parent_missing.h5")

	// Create file with dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	ds, err := fw.CreateDataset("/dataset", Float64, []uint64{2})
	require.NoError(t, err)

	data := []float64{1.0, 2.0}
	err = ds.Write(data)
	require.NoError(t, err)

	// Try to create link in non-existent parent group
	err = fw.CreateHardLink("/nonexistent/link", "/dataset")
	assert.Error(t, err, "Should fail when parent group does not exist")
	assert.Contains(t, err.Error(), "does not exist", "Error should mention parent not found")
}

func TestCreateHardLink_SymbolTable(t *testing.T) {
	// Test hard links in symbol table format groups
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_symboltable_link.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/data", Int32, []uint64{3})
	require.NoError(t, err)

	data := []int32{1, 2, 3}
	err = ds.Write(data)
	require.NoError(t, err)

	// Get dataset address
	dataAddr, err := fw.resolveObjectAddress("/data")
	require.NoError(t, err)

	// Create hard link (uses symbol table storage via linkToParent)
	err = fw.CreateHardLink("/link", "/data")
	require.NoError(t, err)

	// Verify link points to same object
	linkAddr, err := fw.resolveObjectAddress("/link")
	require.NoError(t, err)
	assert.Equal(t, dataAddr, linkAddr, "Link should point to same object")
}

func TestValidateLinkPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{"valid path", "/data/link", false, ""},
		{"empty path", "", true, "cannot be empty"},
		{"no leading slash", "data/link", true, "must start with '/'"},
		{"root path", "/", true, "cannot create link to root"},
		{"consecutive slashes", "/data//link", true, "consecutive slashes"},
		{"valid single level", "/link", false, ""},
		{"valid nested", "/a/b/c/d/link", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLinkPath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestIncrementReferenceCount tests the reference counting methods directly.
func TestIncrementReferenceCount(t *testing.T) {
	oh := &core.ObjectHeader{
		Version:        2,
		ReferenceCount: 1,
	}

	// Test increment
	newCount := oh.IncrementReferenceCount()
	assert.Equal(t, uint32(2), newCount)
	assert.Equal(t, uint32(2), oh.GetReferenceCount())

	// Test multiple increments
	oh.IncrementReferenceCount()
	oh.IncrementReferenceCount()
	assert.Equal(t, uint32(4), oh.GetReferenceCount())
}

// TestDecrementReferenceCount tests the decrement method.
func TestDecrementReferenceCount(t *testing.T) {
	oh := &core.ObjectHeader{
		Version:        2,
		ReferenceCount: 3,
	}

	// Test decrement
	newCount := oh.DecrementReferenceCount()
	assert.Equal(t, uint32(2), newCount)
	assert.Equal(t, uint32(2), oh.GetReferenceCount())

	// Test decrement to zero
	oh.DecrementReferenceCount()
	oh.DecrementReferenceCount()
	assert.Equal(t, uint32(0), oh.GetReferenceCount())

	// Test decrement when already zero (should stay zero)
	newCount = oh.DecrementReferenceCount()
	assert.Equal(t, uint32(0), newCount)
	assert.Equal(t, uint32(0), oh.GetReferenceCount())
}

func TestCreateHardLink_MultipleLinks(t *testing.T) {
	// Test creating multiple hard links to the same object
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_multiple_links.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/original", Float64, []uint64{2})
	require.NoError(t, err)
	err = ds.Write([]float64{1.0, 2.0})
	require.NoError(t, err)

	// Get original address
	originalAddr, err := fw.resolveObjectAddress("/original")
	require.NoError(t, err)

	// Create 5 hard links
	for i := 1; i <= 5; i++ {
		linkPath := "/link" + string(rune('0'+i))
		err = fw.CreateHardLink(linkPath, "/original")
		require.NoError(t, err, "Failed to create link %d", i)

		// Verify link points to original
		linkAddr, err := fw.resolveObjectAddress(linkPath)
		require.NoError(t, err)
		assert.Equal(t, originalAddr, linkAddr, "Link %d should point to original", i)

		// Verify refcount
		oh, err := core.ReadObjectHeader(fw.writer, originalAddr, fw.file.sb)
		require.NoError(t, err)
		expectedRefCount := uint32(1 + i) // original + i links
		assert.Equal(t, expectedRefCount, oh.GetReferenceCount(), "Refcount should be %d after link %d", expectedRefCount, i)
	}
}
