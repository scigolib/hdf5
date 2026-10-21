package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenUserBlockFiles reads official HDF5 files whose superblock sits behind
// a user block. HDF5 metadata addresses are relative to the superblock position
// (the base address), so reading these files proves the reader applies the base
// at the I/O boundary (H5FDint.c:267).
func TestOpenUserBlockFiles(t *testing.T) {
	tests := []struct {
		path string
		base uint64
	}{
		{"testdata/hdf5_official/twithub.h5", 512},
		{"testdata/hdf5_official/twithub513.h5", 1024},
		// Generated from aggr.h5 with the C library: h5repack -b 512.
		{"testdata/reference/userblock-512.h5", 512},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f, err := Open(tt.path)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()

			require.Equal(t, tt.base, f.Superblock().BaseAddress,
				"the base address is the position of the superblock")

			var paths []string
			f.Walk(func(path string, _ Object) {
				paths = append(paths, path)
			})

			require.NotEmpty(t, paths, "the file must expose at least its root group")
			require.Contains(t, paths, "/", "the root group must load")

			// A user block must not leak into the address space: the root group's
			// object header is read at base+RootGroup, which only works if the
			// reader shifts addresses.
			require.NotZero(t, f.Superblock().RootGroup)
		})
	}
}

// TestOpenForWriteRefusesUserBlock pins the scope: the write path addresses the
// file absolutely, so it must refuse a file with a user block rather than write
// at the wrong offsets.
func TestOpenForWriteRefusesUserBlock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "twithub.h5")

	data, err := os.ReadFile("testdata/hdf5_official/twithub.h5")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, data, 0o600))

	_, err = OpenForWrite(target, OpenReadWrite)
	require.Error(t, err)
	require.Contains(t, err.Error(), "user block")
}

// TestOpenUserBlockFileData reads a dataset from a user-block file, proving the
// shift reaches the data path and not only the superblock.
func TestOpenUserBlockFileData(t *testing.T) {
	f, err := Open("testdata/hdf5_official/twithub.h5")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	read := 0
	f.Walk(func(path string, obj Object) {
		ds, ok := obj.(*Dataset)
		if !ok {
			return
		}

		_, readErr := ds.Read()
		require.NoError(t, readErr, "dataset %s", path)
		read++
	})

	require.Positive(t, read, "the fixture must contain at least one dataset")
}
