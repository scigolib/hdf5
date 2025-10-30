package hdf5

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenFile tests basic file opening functionality.
func TestOpenFile(t *testing.T) {
	tests := []struct {
		name         string
		filepath     string
		wantErr      bool
		checkVersion func(*testing.T, *File)
	}{
		{
			name:     "valid v2 file",
			filepath: "testdata/v2.h5",
			wantErr:  false,
			checkVersion: func(t *testing.T, f *File) {
				require.Equal(t, uint8(2), f.SuperblockVersion())
			},
		},
		{
			name:     "valid v3 file",
			filepath: "testdata/v3.h5",
			wantErr:  false,
			checkVersion: func(t *testing.T, f *File) {
				require.Equal(t, uint8(3), f.SuperblockVersion())
			},
		},
		{
			name:     "non-existent file",
			filepath: "testdata/does_not_exist.h5",
			wantErr:  true,
		},
		{
			name:     "invalid file",
			filepath: "file_test.go", // Not an HDF5 file.
			wantErr:  true,
		},
		// Note: v0 files are not tested as they require Object Header v1 support.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := Open(tt.filepath)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, file)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, file)
			defer func() { _ = file.Close() }()

			if tt.checkVersion != nil {
				tt.checkVersion(t, file)
			}
		})
	}
}

// TestFileClose ensures files can be closed without error.
func TestFileClose(t *testing.T) {
	file, err := Open("testdata/v2.h5")
	require.NoError(t, err)
	require.NotNil(t, file)

	err = file.Close()
	require.NoError(t, err)

	// Second close should also work (idempotent).
	err = file.Close()
	require.NoError(t, err)
}

// TestWalk tests the Walk functionality for traversing file structure.
func TestWalk(t *testing.T) {
	file, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	var paths []string
	var groups int
	var datasets int

	file.Walk(func(path string, obj Object) {
		paths = append(paths, path)

		switch obj.(type) {
		case *Group:
			groups++
		case *Dataset:
			datasets++
		}
	})

	// Should have at least the root group.
	require.Greater(t, len(paths), 0, "should have at least one object")
	require.Greater(t, groups, 0, "should have at least the root group")

	// Root group should be first.
	require.Equal(t, "/", paths[0])
}

// TestSuperblockVersions tests that different superblock versions are handled correctly.
func TestSuperblockVersions(t *testing.T) {
	versions := []struct {
		file    string
		version uint8
	}{
		{"testdata/v2.h5", 2},
		{"testdata/v3.h5", 3},
		// Note: v0 requires Object Header v1 support (not implemented yet).
	}

	for _, v := range versions {
		t.Run(v.file, func(t *testing.T) {
			file, err := Open(v.file)
			require.NoError(t, err)
			defer func() { _ = file.Close() }()

			require.Equal(t, v.version, file.SuperblockVersion())
		})
	}
}

// TestGroupChildren tests that group children are loaded correctly.
func TestGroupChildren(t *testing.T) {
	file, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	var rootGroup *Group
	file.Walk(func(path string, obj Object) {
		if path == "/" {
			if g, ok := obj.(*Group); ok {
				rootGroup = g
			}
		}
	})

	require.NotNil(t, rootGroup, "should have root group")
	children := rootGroup.Children()
	require.Greater(t, len(children), 0, "root group should have children")

	// Children should have names (though some traditional format groups may not).
	namedChildren := 0
	for _, child := range children {
		if child.Name() != "" {
			namedChildren++
		}
	}
	require.Greater(t, namedChildren, 0, "at least some children should have names")
}

// TestDatasetObject tests dataset object creation.
func TestDatasetObject(t *testing.T) {
	file, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	var foundDataset bool
	file.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok {
			foundDataset = true
			require.NotEmpty(t, ds.Name(), "dataset should have a name")
			require.NotEmpty(t, path, "dataset path should not be empty")
		}
	})

	// File should contain at least one dataset.
	require.True(t, foundDataset, "file should contain at least one dataset")
}

// TestGroupAttributes tests reading attributes from groups.
func TestGroupAttributes(t *testing.T) {
	file, err := Open("testdata/with_groups.h5")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	var rootGroup *Group
	file.Walk(func(path string, obj Object) {
		if path == "/" {
			if g, ok := obj.(*Group); ok {
				rootGroup = g
			}
		}
	})

	require.NotNil(t, rootGroup, "should have root group")

	// Try to read attributes - should not error even if empty.
	attrs, err := rootGroup.Attributes()
	require.NoError(t, err, "reading group attributes should not error")
	require.NotNil(t, attrs, "attributes should not be nil (may be empty slice)")

	// If group has address stored, it should be non-zero for modern groups.
	// Traditional SNOD groups may have zero address and return empty attributes.
	// Both cases are valid.
}

// BenchmarkOpenFile benchmarks file opening performance.
func BenchmarkOpenFile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		file, err := Open("testdata/v2.h5")
		if err != nil {
			b.Fatal(err)
		}
		_ = file.Close()
	}
}

// BenchmarkWalk benchmarks file tree traversal.
func BenchmarkWalk(b *testing.B) {
	file, err := Open("testdata/with_groups.h5")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file.Walk(func(_ string, _ Object) {
			// Just traverse, don't do anything.
		})
	}
}
