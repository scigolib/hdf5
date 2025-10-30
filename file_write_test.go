package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		mode    CreateMode
		wantErr bool
		setup   func(t *testing.T, filename string) // Setup before create
		verify  func(t *testing.T, filename string) // Additional verification
	}{
		{
			name:    "create new file with truncate mode",
			mode:    CreateTruncate,
			wantErr: false,
			verify: func(t *testing.T, filename string) {
				// Verify file exists
				info, err := os.Stat(filename)
				require.NoError(t, err)
				assert.Greater(t, info.Size(), int64(0), "File should not be empty")

				// Verify file can be opened
				f, err := Open(filename)
				require.NoError(t, err)
				defer func() { _ = f.Close() }()

				// Verify root group
				assert.NotNil(t, f.Root())
				assert.Equal(t, "/", f.Root().Name())
			},
		},
		{
			name:    "create file with exclusive mode",
			mode:    CreateExclusive,
			wantErr: false,
		},
		{
			name:    "exclusive mode fails if file exists",
			mode:    CreateExclusive,
			wantErr: true,
			setup: func(t *testing.T, filename string) {
				// Create file first
				f, err := Create(filename, CreateTruncate)
				require.NoError(t, err)
				require.NoError(t, f.Close())
			},
		},
		{
			name:    "truncate mode overwrites existing file",
			mode:    CreateTruncate,
			wantErr: false,
			setup: func(t *testing.T, filename string) {
				// Create file with some content first
				require.NoError(t, os.WriteFile(filename, []byte("old content"), 0o644))
			},
			verify: func(t *testing.T, filename string) {
				// Verify file was overwritten with valid HDF5 content
				f, err := Open(filename)
				require.NoError(t, err)
				defer func() { _ = f.Close() }()

				assert.NotNil(t, f.Root())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test
			tempDir := t.TempDir()
			filename := filepath.Join(tempDir, "test.h5")

			// Run setup if provided
			if tt.setup != nil {
				tt.setup(t, filename)
			}

			// Create file
			f, err := Create(filename, tt.mode)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, f)

			// Close file
			err = f.Close()
			require.NoError(t, err)

			// Run additional verification
			if tt.verify != nil {
				tt.verify(t, filename)
			}
		})
	}
}

func TestCreate_RoundTrip(t *testing.T) {
	// Create file
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "roundtrip.h5")

	f1, err := Create(filename, CreateTruncate)
	require.NoError(t, err)
	require.NoError(t, f1.Close())

	// Reopen and verify
	f2, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f2.Close() }()

	// Verify superblock
	assert.Equal(t, uint8(2), f2.SuperblockVersion(), "Should be superblock v2")

	// Verify root group
	root := f2.Root()
	require.NotNil(t, root)
	assert.Equal(t, "/", root.Name())

	// Verify no children in empty file
	children := root.Children()
	assert.Empty(t, children, "Root group should be empty")
}

func TestCreate_FileStructure(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "structure.h5")

	// Create file
	f, err := Create(filename, CreateTruncate)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Verify file size
	// Minimum size: 48 (superblock v2) + 29 (minimal root group object header)
	// = 77 bytes
	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, info.Size(), int64(77),
		"File should be at least 77 bytes (superblock + root group)")

	// Verify HDF5 signature
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 8)

	signature := string(data[0:8])
	assert.Equal(t, "\x89HDF\r\n\x1a\n", signature, "Should have HDF5 signature")

	// Verify superblock version
	assert.Equal(t, uint8(2), data[8], "Should be superblock v2")
}

func TestCreate_MultipleFiles(t *testing.T) {
	tempDir := t.TempDir()

	// Create multiple files in sequence
	for i := 0; i < 5; i++ {
		filename := filepath.Join(tempDir, "file"+string(rune('0'+i))+".h5")

		f, err := Create(filename, CreateTruncate)
		require.NoError(t, err, "Failed to create file %d", i)
		require.NoError(t, f.Close(), "Failed to close file %d", i)

		// Verify each file
		f2, err := Open(filename)
		require.NoError(t, err, "Failed to reopen file %d", i)
		assert.Equal(t, "/", f2.Root().Name(), "File %d root group invalid", i)
		require.NoError(t, f2.Close())
	}
}

func TestCreate_InvalidMode(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "invalid.h5")

	// Use invalid mode (999)
	f, err := Create(filename, CreateMode(999))
	assert.Error(t, err)
	assert.Nil(t, f)
	assert.Contains(t, err.Error(), "invalid create mode")
}

func TestCreate_InvalidPath(t *testing.T) {
	// Try to create file in non-existent directory (no temp dir)
	filename := "/nonexistent/path/to/file.h5"

	f, err := Create(filename, CreateTruncate)
	assert.Error(t, err)
	assert.Nil(t, f)
}

func TestCreate_ReadOnlyAfterCreate(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "readonly.h5")

	// Create file
	f, err := Create(filename, CreateTruncate)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// For MVP, file is read-only after creation
	// Verify we can read root group
	root := f.Root()
	assert.NotNil(t, root)
	assert.Equal(t, "/", root.Name())

	// Verify we can walk (even though empty)
	walkCount := 0
	f.Walk(func(path string, obj Object) {
		walkCount++
	})
	assert.Equal(t, 1, walkCount, "Should walk only root group")
}
