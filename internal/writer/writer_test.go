package writer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileWriter(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		filename      string
		mode          CreateMode
		initialOffset uint64
		wantErr       bool
		setupExisting bool // Create file before test
	}{
		{
			name:          "create new file truncate mode",
			filename:      "test1.h5",
			mode:          ModeTruncate,
			initialOffset: 48,
			wantErr:       false,
		},
		{
			name:          "create new file exclusive mode",
			filename:      "test2.h5",
			mode:          ModeExclusive,
			initialOffset: 48,
			wantErr:       false,
		},
		{
			name:          "truncate existing file",
			filename:      "test3.h5",
			mode:          ModeTruncate,
			initialOffset: 48,
			setupExisting: true,
			wantErr:       false,
		},
		{
			name:          "exclusive mode fails on existing",
			filename:      "test4.h5",
			mode:          ModeExclusive,
			initialOffset: 48,
			setupExisting: true,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.filename)

			// Setup existing file if needed
			if tt.setupExisting {
				f, err := os.Create(path)
				require.NoError(t, err)
				_, err = f.WriteString("existing content")
				require.NoError(t, err)
				f.Close()
			}

			writer, err := NewFileWriter(path, tt.mode, tt.initialOffset)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, writer)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, writer)
			defer writer.Close()

			// Verify initial state
			assert.NotNil(t, writer.File())
			assert.Equal(t, tt.initialOffset, writer.EndOfFile())

			// Verify file exists
			_, err = os.Stat(path)
			assert.NoError(t, err)
		})
	}
}

func TestFileWriter_Allocate(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	writer, err := NewFileWriter(path, ModeTruncate, 48)
	require.NoError(t, err)
	defer writer.Close()

	t.Run("sequential allocations", func(t *testing.T) {
		addr1, err := writer.Allocate(100)
		require.NoError(t, err)
		assert.Equal(t, uint64(48), addr1)
		assert.Equal(t, uint64(148), writer.EndOfFile())

		addr2, err := writer.Allocate(200)
		require.NoError(t, err)
		assert.Equal(t, uint64(148), addr2)
		assert.Equal(t, uint64(348), writer.EndOfFile())
	})

	t.Run("zero size allocation fails", func(t *testing.T) {
		_, err := writer.Allocate(0)
		assert.Error(t, err)
	})
}

func TestFileWriter_WriteAt(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	writer, err := NewFileWriter(path, ModeTruncate, 0)
	require.NoError(t, err)
	defer writer.Close()

	t.Run("write data at address", func(t *testing.T) {
		data := []byte("Hello, HDF5!")
		addr, err := writer.Allocate(uint64(len(data)))
		require.NoError(t, err)

		n, err := writer.WriteAt(data, int64(addr))
		require.NoError(t, err)
		assert.Equal(t, len(data), n)

		// Read back to verify
		buf := make([]byte, len(data))
		n, err = writer.ReadAt(buf, int64(addr))
		require.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf)
	})

	t.Run("write empty data", func(t *testing.T) {
		n, err := writer.WriteAt([]byte{}, 0)
		assert.NoError(t, err)
		assert.Equal(t, 0, n) // Should be no-op
	})

	t.Run("write at specific address", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04}
		addr, _ := writer.Allocate(uint64(len(data)))

		n, err := writer.WriteAt(data, int64(addr))
		require.NoError(t, err)
		assert.Equal(t, len(data), n)

		// Verify
		buf := make([]byte, len(data))
		_, err = writer.ReadAt(buf, int64(addr))
		require.NoError(t, err)
		assert.Equal(t, data, buf)
	})
}

func TestFileWriter_WriteAtWithAllocation(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	writer, err := NewFileWriter(path, ModeTruncate, 0)
	require.NoError(t, err)
	defer writer.Close()

	t.Run("allocate and write", func(t *testing.T) {
		data := []byte("Test data")

		addr, err := writer.WriteAtWithAllocation(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), addr) // First allocation

		// Verify
		buf := make([]byte, len(data))
		_, err = writer.ReadAt(buf, int64(addr))
		require.NoError(t, err)
		assert.Equal(t, data, buf)
	})

	t.Run("empty data fails", func(t *testing.T) {
		_, err := writer.WriteAtWithAllocation([]byte{})
		assert.Error(t, err)
	})

	t.Run("multiple writes", func(t *testing.T) {
		data1 := []byte("First")
		data2 := []byte("Second")

		addr1, err := writer.WriteAtWithAllocation(data1)
		require.NoError(t, err)

		addr2, err := writer.WriteAtWithAllocation(data2)
		require.NoError(t, err)

		// Addresses should be sequential
		assert.Equal(t, addr1+uint64(len(data1)), addr2)

		// Verify both
		buf1 := make([]byte, len(data1))
		_, err = writer.ReadAt(buf1, int64(addr1))
		require.NoError(t, err)
		assert.Equal(t, data1, buf1)

		buf2 := make([]byte, len(data2))
		_, err = writer.ReadAt(buf2, int64(addr2))
		require.NoError(t, err)
		assert.Equal(t, data2, buf2)
	})
}

func TestFileWriter_Flush(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	writer, err := NewFileWriter(path, ModeTruncate, 0)
	require.NoError(t, err)
	defer writer.Close()

	// Write data
	data := []byte("Test flush")
	addr, err := writer.WriteAtWithAllocation(data)
	require.NoError(t, err)

	// Flush
	err = writer.Flush()
	require.NoError(t, err)

	// Verify data persists (open another reader)
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	buf := make([]byte, len(data))
	n, err := f.ReadAt(buf, int64(addr))
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)
}

func TestFileWriter_Close(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	writer, err := NewFileWriter(path, ModeTruncate, 0)
	require.NoError(t, err)

	// Close once
	err = writer.Close()
	assert.NoError(t, err)

	// Close again (should be safe)
	err = writer.Close()
	assert.NoError(t, err)

	// Operations after close should fail
	_, err = writer.Allocate(100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	_, err = writer.WriteAt([]byte("test"), 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")

	err = writer.Flush()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestFileWriter_EndOfFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.h5")

	tests := []struct {
		name          string
		initialOffset uint64
		writes        []int
		expectedEOF   uint64
	}{
		{
			name:          "no writes",
			initialOffset: 48,
			writes:        []int{},
			expectedEOF:   48,
		},
		{
			name:          "single write",
			initialOffset: 48,
			writes:        []int{100},
			expectedEOF:   148,
		},
		{
			name:          "multiple writes",
			initialOffset: 48,
			writes:        []int{100, 200, 50},
			expectedEOF:   398,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := NewFileWriter(path, ModeTruncate, tt.initialOffset)
			require.NoError(t, err)
			defer writer.Close()

			for _, size := range tt.writes {
				data := make([]byte, size)
				_, err := writer.WriteAtWithAllocation(data)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedEOF, writer.EndOfFile())
		})
	}
}

func TestFileWriter_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "integration.h5")

	t.Run("complete write workflow", func(t *testing.T) {
		// Create writer
		writer, err := NewFileWriter(path, ModeTruncate, 48)
		require.NoError(t, err)

		// Write multiple blocks
		block1 := []byte("Block 1 data")
		addr1, err := writer.WriteAtWithAllocation(block1)
		require.NoError(t, err)

		block2 := []byte("Block 2 data with more content")
		addr2, err := writer.WriteAtWithAllocation(block2)
		require.NoError(t, err)

		block3 := []byte("Block 3")
		addr3, err := writer.WriteAtWithAllocation(block3)
		require.NoError(t, err)

		// Verify EOF
		expectedEOF := 48 + uint64(len(block1)) + uint64(len(block2)) + uint64(len(block3))
		assert.Equal(t, expectedEOF, writer.EndOfFile())

		// Verify no overlaps
		err = writer.Allocator().ValidateNoOverlaps()
		assert.NoError(t, err)

		// Flush and close
		err = writer.Flush()
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		// Reopen and verify data persists
		f, err := os.Open(path)
		require.NoError(t, err)
		defer f.Close()

		buf1 := make([]byte, len(block1))
		_, err = f.ReadAt(buf1, int64(addr1))
		require.NoError(t, err)
		assert.Equal(t, block1, buf1)

		buf2 := make([]byte, len(block2))
		_, err = f.ReadAt(buf2, int64(addr2))
		require.NoError(t, err)
		assert.Equal(t, block2, buf2)

		buf3 := make([]byte, len(block3))
		_, err = f.ReadAt(buf3, int64(addr3))
		require.NoError(t, err)
		assert.Equal(t, block3, buf3)
	})
}
