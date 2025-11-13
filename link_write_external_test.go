package hdf5

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateExternalLink_BasicCreation tests basic external link creation API.
func TestCreateExternalLink_BasicCreation(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_basic.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create /links group
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Create external link to dataset in another file
	err = fw.CreateExternalLink("/links/external1", "other.h5", "/data/dataset1")
	assert.NoError(t, err, "External link creation should succeed")

	// Verify file was written
	err = fw.Close()
	require.NoError(t, err)

	// Verify file can be opened (round-trip test)
	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()

	// Verify root group exists (external link written successfully)
	root := f.Root()
	require.NotNil(t, root)

	// Verify groups exist in structure
	children := root.Children()
	require.NotNil(t, children)
	assert.Greater(t, len(children), 0, "root should have at least one child (links group)")
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
			assert.NoError(t, err, "Valid external link should succeed")
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

	// Create /links group
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Test absolute path (Unix-style)
	err = fw.CreateExternalLink("/links/ext1", "/absolute/path/file.h5", "/dataset1")
	assert.NoError(t, err, "Absolute file path should be allowed")
}

// TestCreateExternalLink_RelativeFilePath tests external link with relative file path.
func TestCreateExternalLink_RelativeFilePath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_relative.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create /links group
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Test relative path
	err = fw.CreateExternalLink("/links/ext1", "./relative/file.h5", "/dataset1")
	assert.NoError(t, err, "Relative file path should be allowed")

	// Test relative path with subdirectory
	err = fw.CreateExternalLink("/links/ext2", "subdir/file.h5", "/group1/dataset2")
	assert.NoError(t, err, "Relative file path should be allowed")
}

// TestCreateExternalLink_WindowsPath tests external link with Windows-style paths.
func TestCreateExternalLink_WindowsPath(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test_extlink_windows.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create /links group
	_, err = fw.CreateGroup("/links")
	require.NoError(t, err)

	// Test Windows absolute path (C:\path\file.h5)
	// Note: filepath handles OS-specific path separators
	windowsPath := `C:\Users\Data\file.h5`
	err = fw.CreateExternalLink("/links/ext1", windowsPath, "/dataset1")
	assert.NoError(t, err, "Windows path should be allowed (no path traversal)")
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
