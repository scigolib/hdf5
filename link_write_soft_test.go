package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateSoftLink_BasicCreation tests basic soft link creation API.
func TestCreateSoftLink_BasicCreation(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_basic.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create /data group
	_, err = fw.CreateGroup("/data")
	require.NoError(t, err)

	// Create a dataset
	ds, err := fw.CreateDataset("/data/temperature", Float64, []uint64{5})
	require.NoError(t, err)
	require.NotNil(t, ds)

	data := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	err = ds.Write(data)
	require.NoError(t, err)

	// Create soft link to dataset
	err = fw.CreateSoftLink("/data/temp_link", "/data/temperature")
	assert.NoError(t, err, "Soft link creation should succeed")

	// Verify file was written
	err = fw.Close()
	require.NoError(t, err)

	// Verify file can be opened (round-trip test)
	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()

	// Verify root group exists (soft link written successfully)
	root := f.Root()
	require.NotNil(t, root)

	// Verify groups exist in structure
	children := root.Children()
	require.NotNil(t, children, "root should have children")
	assert.Greater(t, len(children), 0, "root should have at least one child (data group)")
}

// TestCreateSoftLink_ToGroup tests soft link to a group.
func TestCreateSoftLink_ToGroup(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_group.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create groups
	_, err = fw.CreateGroup("/group1")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Create soft link to group
	err = fw.CreateSoftLink("/links/group_link", "/group1")
	assert.NoError(t, err, "Soft link to group should succeed")

	// Verify file was written
	err = fw.Close()
	require.NoError(t, err)

	// Verify file can be opened
	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()

	// Verify groups exist
	root := f.Root()
	require.NotNil(t, root)

	children := root.Children()
	require.NotNil(t, children)
	assert.GreaterOrEqual(t, len(children), 2, "should have group1 and links groups")
}

// TestCreateSoftLink_DanglingLink tests creating a soft link to a non-existent target.
// This should be allowed in HDF5 - the target can be created later.
func TestCreateSoftLink_DanglingLink(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_dangling.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create soft link to non-existent target (dangling link allowed)
	err = fw.CreateSoftLink("/link_to_future", "/data/future_dataset")
	assert.NoError(t, err, "Dangling soft links should be allowed")

	// Verify file was written
	err = fw.Close()
	require.NoError(t, err)

	// Verify file can be opened
	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()

	root := f.Root()
	require.NotNil(t, root)
	// Note: link exists in symbol table, but target does not (dangling link)
}

// TestCreateSoftLink_RelativePath tests that relative paths are rejected.
func TestCreateSoftLink_RelativePath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_relative.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Try to create soft link with relative target path
	err = fw.CreateSoftLink("/link", "relative/path")
	assert.Error(t, err, "Should reject relative target path")
	assert.Contains(t, err.Error(), "must be absolute", "Error should mention absolute path requirement")

	// Try with relative link path
	err = fw.CreateSoftLink("relative/link", "/data/target")
	assert.Error(t, err, "Should reject relative link path")
	assert.Contains(t, err.Error(), "must start with '/'", "Error should mention leading slash")
}

// TestCreateSoftLink_InvalidLinkPath tests various invalid link path formats.
func TestCreateSoftLink_InvalidLinkPath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_invalid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name       string
		linkPath   string
		targetPath string
		errMsg     string
	}{
		{
			name:       "empty link path",
			linkPath:   "",
			targetPath: "/data/target",
			errMsg:     "cannot be empty",
		},
		{
			name:       "no leading slash in link",
			linkPath:   "link",
			targetPath: "/data/target",
			errMsg:     "must start with '/'",
		},
		{
			name:       "root link path",
			linkPath:   "/",
			targetPath: "/data/target",
			errMsg:     "cannot create link to root",
		},
		{
			name:       "consecutive slashes in link",
			linkPath:   "/group//link",
			targetPath: "/data/target",
			errMsg:     "consecutive slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateSoftLink(tt.linkPath, tt.targetPath)
			assert.Error(t, err, "Should fail for invalid link path")
			assert.Contains(t, err.Error(), tt.errMsg, "Error message should be descriptive")
		})
	}
}

// TestCreateSoftLink_TargetPathFormat tests various target path formats.
func TestCreateSoftLink_TargetPathFormat(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_softlink_target_fmt.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name       string
		targetPath string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid absolute path",
			targetPath: "/data/dataset1",
			wantErr:    false,
		},
		{
			name:       "valid nested path",
			targetPath: "/group1/group2/group3/dataset",
			wantErr:    false,
		},
		{
			name:       "empty target path",
			targetPath: "",
			wantErr:    true,
			errMsg:     "cannot be empty",
		},
		{
			name:       "relative target path",
			targetPath: "relative/path",
			wantErr:    true,
			errMsg:     "must be absolute",
		},
		{
			name:       "consecutive slashes in target",
			targetPath: "/data//dataset",
			wantErr:    true,
			errMsg:     "consecutive slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linkPath := "/link_" + tt.name
			err := fw.CreateSoftLink(linkPath, tt.targetPath)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateSoftLinkTargetPath tests target path validation function.
func TestValidateSoftLinkTargetPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid absolute path",
			path:    "/data/dataset",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			path:    "/a/b/c/d",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "relative path",
			path:    "relative/path",
			wantErr: true,
			errMsg:  "must be absolute",
		},
		{
			name:    "consecutive slashes",
			path:    "/data//dataset",
			wantErr: true,
			errMsg:  "consecutive slashes",
		},
		{
			name:    "valid single level",
			path:    "/dataset",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSoftLinkTargetPath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestResolveSoftLink_NotImplemented tests that resolution is not yet implemented.
// MVP v0.11.5-beta: Soft link resolution will be added in v0.12.0.
func TestResolveSoftLink_NotImplemented(t *testing.T) {
	// Soft link resolution is not implemented in MVP v0.11.5-beta
	// This test documents the planned API for v0.12.0
	t.Skip("Soft link resolution not implemented in MVP v0.11.5-beta (planned for v0.12.0)")
}

// Future tests (skipped in MVP v0.11.5-beta):
// - TestResolveSoftLink_Simple
// - TestResolveSoftLink_Chain
// - TestResolveSoftLink_CircularReference
// - TestResolveSoftLink_MaxDepth
// - TestResolveSoftLink_DanglingLink
// - TestSoftLink_ReadAfterCreate
// - TestCreateSoftLink_ParentNotExists
// - TestCreateSoftLink_MultipleLinks
