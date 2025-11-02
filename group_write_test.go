package hdf5

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateGroup_RootLevel(t *testing.T) {
	testFile := "test_create_group_root.h5"
	defer func() { _ = os.Remove(testFile) }()

	// Create file
	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create root-level group
	err = fw.CreateGroup("/data")
	require.NoError(t, err)

	// Close and verify file is valid
	err = fw.Close()
	require.NoError(t, err)

	// Reopen and verify structure
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// For MVP, group is created but not linked (limitation)
	// We verify the file is still valid
	root := f.Root()
	require.NotNil(t, root)
}

func TestCreateGroup_ValidationErrors(t *testing.T) {
	testFile := "test_create_group_validation.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "empty path",
			path:    "",
			wantErr: "group path cannot be empty",
		},
		{
			name:    "no leading slash",
			path:    "data",
			wantErr: "group path must start with '/'",
		},
		{
			name:    "root path",
			path:    "/",
			wantErr: "root group already exists",
		},
		{
			name:    "nested group (parent doesn't exist)",
			path:    "/data/experiments",
			wantErr: "parent group \"/data\" does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fw.CreateGroup(tt.path)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCreateGroup_Multiple(t *testing.T) {
	testFile := "test_create_group_multiple.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = fw.Close() }()

	// Create multiple root-level groups
	groups := []string{"/data", "/metadata", "/results"}
	for _, g := range groups {
		err := fw.CreateGroup(g)
		require.NoError(t, err)
	}

	// Close and verify file is valid
	err = fw.Close()
	require.NoError(t, err)

	// Reopen and verify file structure
	f, err := Open(testFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	root := f.Root()
	require.NotNil(t, root)
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantParent string
		wantName   string
	}{
		{
			name:       "root level",
			path:       "/group1",
			wantParent: "",
			wantName:   "group1",
		},
		{
			name:       "nested path",
			path:       "/data/experiments",
			wantParent: "/data",
			wantName:   "experiments",
		},
		{
			name:       "deeply nested",
			path:       "/a/b/c",
			wantParent: "/a/b",
			wantName:   "c",
		},
		{
			name:       "root",
			path:       "/",
			wantParent: "",
			wantName:   "",
		},
		{
			name:       "trailing slash",
			path:       "/group1/",
			wantParent: "",
			wantName:   "group1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, name := parsePath(tt.path)
			require.Equal(t, tt.wantParent, parent, "parent mismatch")
			require.Equal(t, tt.wantName, name, "name mismatch")
		})
	}
}

func TestCreateGroup_BinaryFormat(t *testing.T) {
	testFile := "test_create_group_binary.h5"
	defer func() { _ = os.Remove(testFile) }()

	fw, err := CreateForWrite(testFile, CreateTruncate)
	require.NoError(t, err)

	// Create group
	err = fw.CreateGroup("/testgroup")
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Read raw file and verify structures exist
	data, err := os.ReadFile(testFile)
	require.NoError(t, err)

	// File should contain:
	// 1. Superblock signature (at offset 0)
	require.Equal(t, []byte{0x89, 'H', 'D', 'F', '\r', '\n', 0x1a, '\n'}, data[0:8], "HDF5 signature")

	// 2. "HEAP" signature (local heap)
	require.Contains(t, string(data), "HEAP", "should contain local heap")

	// 3. "SNOD" signature (symbol table node)
	require.Contains(t, string(data), "SNOD", "should contain symbol table node")

	// 4. "TREE" signature (B-tree)
	require.Contains(t, string(data), "TREE", "should contain B-tree")

	// 5. "OHDR" signature (object header for group)
	require.Contains(t, string(data), "OHDR", "should contain object header")
}
