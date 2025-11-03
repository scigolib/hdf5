package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateExternalLink_BasicCreation tests basic external link creation API.
// MVP v0.11.5-beta: Tests that API exists and returns not-implemented error.
func TestCreateExternalLink_BasicCreation(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_basic.h5")

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

	// Try to create external link to dataset in another file
	// MVP v0.11.5-beta: Should return not-implemented error
	err = fw.CreateExternalLink("/links/external1", "other.h5", "/data/dataset1")
	assert.Error(t, err, "External link creation should return not-implemented error in MVP")
	assert.Contains(t, err.Error(), "not yet implemented", "Error should indicate feature not yet implemented")
}

// TestCreateExternalLink_ValidFileNames tests various valid file name formats.
func TestCreateExternalLink_ValidFileNames(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_filenames.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name     string
		fileName string
		linkPath string
	}{
		{
			name:     "simple .h5 file",
			fileName: "other.h5",
			linkPath: "/link1",
		},
		{
			name:     ".hdf5 extension",
			fileName: "data.hdf5",
			linkPath: "/link2",
		},
		{
			name:     "relative path",
			fileName: "./subdir/file.h5",
			linkPath: "/link3",
		},
		{
			name:     "no extension",
			fileName: "custom_file",
			linkPath: "/link4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateExternalLink(tt.linkPath, tt.fileName, "/dataset")

			// MVP: All should fail with "not yet implemented" (validation should pass)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "not yet implemented", "Should get not-implemented, not validation error")
		})
	}
}

// TestCreateExternalLink_InvalidFileName tests invalid file names.
func TestCreateExternalLink_InvalidFileName(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_invalid_file.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name     string
		fileName string
		errMsg   string
	}{
		{
			name:     "empty file name",
			fileName: "",
			errMsg:   "cannot be empty",
		},
		{
			name:     "path traversal with ..",
			fileName: "../../../etc/passwd",
			errMsg:   "path traversal",
		},
		{
			name:     "path traversal in middle",
			fileName: "dir/../file.h5",
			errMsg:   "path traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateExternalLink("/link", tt.fileName, "/dataset")
			assert.Error(t, err, "Should reject invalid file name")
			assert.Contains(t, err.Error(), tt.errMsg, "Error message should be descriptive")
		})
	}
}

// TestCreateExternalLink_InvalidLinkPath tests various invalid link path formats.
func TestCreateExternalLink_InvalidLinkPath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_invalid_link.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name     string
		linkPath string
		errMsg   string
	}{
		{
			name:     "empty link path",
			linkPath: "",
			errMsg:   "cannot be empty",
		},
		{
			name:     "no leading slash",
			linkPath: "link",
			errMsg:   "must start with '/'",
		},
		{
			name:     "root link path",
			linkPath: "/",
			errMsg:   "cannot create link to root",
		},
		{
			name:     "consecutive slashes",
			linkPath: "/group//link",
			errMsg:   "consecutive slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateExternalLink(tt.linkPath, "other.h5", "/dataset")
			assert.Error(t, err, "Should fail for invalid link path")
			assert.Contains(t, err.Error(), tt.errMsg, "Error message should be descriptive")
		})
	}
}

// TestCreateExternalLink_InvalidObjectPath tests invalid target object paths.
func TestCreateExternalLink_InvalidObjectPath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_invalid_objpath.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name       string
		objectPath string
		errMsg     string
	}{
		{
			name:       "empty object path",
			objectPath: "",
			errMsg:     "cannot be empty",
		},
		{
			name:       "relative object path",
			objectPath: "relative/path",
			errMsg:     "must be absolute",
		},
		{
			name:       "consecutive slashes in object path",
			objectPath: "/data//dataset",
			errMsg:     "consecutive slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateExternalLink("/link", "other.h5", tt.objectPath)
			assert.Error(t, err, "Should fail for invalid object path")
			assert.Contains(t, err.Error(), tt.errMsg, "Error message should be descriptive")
		})
	}
}

// TestCreateExternalLink_AbsoluteFilePath tests external link with absolute file path.
func TestCreateExternalLink_AbsoluteFilePath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_absolute.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Test absolute path (Unix-style)
	err = fw.CreateExternalLink("/links/ext1", "/absolute/path/file.h5", "/dataset1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented", "Should get not-implemented, not validation error")
}

// TestCreateExternalLink_RelativeFilePath tests external link with relative file path.
func TestCreateExternalLink_RelativeFilePath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_relative.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Test relative path
	err = fw.CreateExternalLink("/links/ext1", "./relative/file.h5", "/dataset1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented", "Should get not-implemented, not validation error")

	// Test relative path with subdirectory
	err = fw.CreateExternalLink("/links/ext2", "subdir/file.h5", "/group1/dataset2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented", "Should get not-implemented, not validation error")
}

// TestCreateExternalLink_WindowsPath tests external link with Windows-style paths.
func TestCreateExternalLink_WindowsPath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_windows.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Test Windows absolute path (C:\path\file.h5)
	// Note: filepath handles OS-specific path separators
	windowsPath := `C:\Users\Data\file.h5`
	err = fw.CreateExternalLink("/links/ext1", windowsPath, "/dataset1")
	assert.Error(t, err)
	// Should either succeed validation or fail with not-implemented (not path traversal)
	if !assert.Contains(t, err.Error(), "not yet implemented") {
		// If it fails, it should not be due to path traversal (no ".." in path)
		assert.NotContains(t, err.Error(), "path traversal")
	}
}

// TestValidateExternalFileName tests file name validation function.
func TestValidateExternalFileName(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid .h5 file",
			fileName: "file.h5",
			wantErr:  false,
		},
		{
			name:     "valid .hdf5 file",
			fileName: "data.hdf5",
			wantErr:  false,
		},
		{
			name:     "no extension",
			fileName: "custom_file",
			wantErr:  false,
		},
		{
			name:     "relative path",
			fileName: "./subdir/file.h5",
			wantErr:  false,
		},
		{
			name:     "absolute path",
			fileName: "/absolute/path/file.h5",
			wantErr:  false,
		},
		{
			name:     "empty file name",
			fileName: "",
			wantErr:  true,
			errMsg:   "cannot be empty",
		},
		{
			name:     "path traversal ..",
			fileName: "../file.h5",
			wantErr:  true,
			errMsg:   "path traversal",
		},
		{
			name:     "path traversal in middle",
			fileName: "dir/../file.h5",
			wantErr:  true,
			errMsg:   "path traversal",
		},
		{
			name:     "multiple path traversals",
			fileName: "../../etc/passwd",
			wantErr:  true,
			errMsg:   "path traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExternalFileName(tt.fileName)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestResolveExternalLink_NotImplemented tests that resolution is not yet implemented.
// MVP v0.11.5-beta: External link resolution will be added in v0.12.0.
func TestResolveExternalLink_NotImplemented(t *testing.T) {
	// External link resolution is not implemented in MVP v0.11.5-beta
	// This test documents the planned API for v0.12.0
	t.Skip("External link resolution not implemented in MVP v0.11.5-beta (planned for v0.12.0)")
}

// Future tests (skipped in MVP v0.11.5-beta):
// - TestResolveExternalLink_Simple
// - TestResolveExternalLink_FileNotFound
// - TestResolveExternalLink_ObjectNotFound
// - TestResolveExternalLink_CircularReferences
// - TestResolveExternalLink_AbsolutePath
// - TestResolveExternalLink_RelativePath
// - TestExternalLink_ReadAfterCreate
// - TestExternalLink_FileCaching
// - TestCreateExternalLink_ParentNotExists
// - TestCreateExternalLink_MultipleExternalLinks
