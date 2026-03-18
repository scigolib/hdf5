package hdf5

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCreateChunkedDataset_1D tests 1D chunked dataset creation.
func TestCreateChunkedDataset_1D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_1d.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 1D chunked dataset: 100 elements, chunks of 10
	ds, err := fw.CreateDataset("/data", Int32, []uint64{100}, WithChunkDims([]uint64{10}))
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Verify chunked properties
	require.True(t, ds.isChunked)
	require.Equal(t, []uint64{10}, ds.chunkDims)
	require.NotNil(t, ds.chunkCoordinator)

	// Create test data
	data := make([]int32, 100)
	for i := range data {
		data[i] = int32(i)
	}

	// Write data
	err = ds.Write(data)
	require.NoError(t, err)

	// Verify B-tree address was assigned
	require.NotEqual(t, uint64(0), ds.dataAddress, "B-tree address should be non-zero after Write()")

	// Flush
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists and has content
	info, err := os.Stat(filename)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

// TestCreateChunkedDataset_2D tests 2D chunked dataset creation.
func TestCreateChunkedDataset_2D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_2d.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 2D chunked dataset: 20x30, chunks 10x10
	ds, err := fw.CreateDataset("/matrix", Float64, []uint64{20, 30}, WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)

	// Create test data (row-major)
	data := make([]float64, 20*30)
	for i := range data {
		data[i] = float64(i)
	}

	// Write data
	err = ds.Write(data)
	require.NoError(t, err)

	// Close
	err = fw.Close()
	require.NoError(t, err)
}

// TestCreateChunkedDataset_3D tests 3D chunked dataset creation.
func TestCreateChunkedDataset_3D(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "chunked_3d.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 3D chunked dataset: 10x12x15, chunks 5x6x5
	ds, err := fw.CreateDataset("/volume", Uint16, []uint64{10, 12, 15}, WithChunkDims([]uint64{5, 6, 5}))
	require.NoError(t, err)

	// Create test data
	data := make([]uint16, 10*12*15)
	for i := range data {
		data[i] = uint16(i % 65536)
	}

	// Write data
	err = ds.Write(data)
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)
}

// TestChunkedDataset_ValidationErrors tests validation errors.
func TestChunkedDataset_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "validation.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name      string
		dims      []uint64
		chunkDims []uint64
		wantErr   string
	}{
		{
			name:      "dimension mismatch",
			dims:      []uint64{10, 20},
			chunkDims: []uint64{5},
			wantErr:   "chunk dimensions (1) must match dataset dimensions (2)",
		},
		{
			name:      "zero chunk dimension",
			dims:      []uint64{10, 20},
			chunkDims: []uint64{5, 0},
			wantErr:   "chunk dimension 1 cannot be zero",
		},
		{
			name:      "chunk larger than dataset",
			dims:      []uint64{10, 20},
			chunkDims: []uint64{15, 10},
			wantErr:   "chunk dimension 0 (15) cannot exceed dataset dimension (10)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fw.CreateDataset("/test", Int32, tt.dims, WithChunkDims(tt.chunkDims))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestChunkedDataset_EdgeChunks tests datasets with edge chunks.
func TestChunkedDataset_EdgeChunks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "edge_chunks.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Dataset 25x35, chunks 10x10 → 3x4 chunks (some partial)
	ds, err := fw.CreateDataset("/data", Int32, []uint64{25, 35}, WithChunkDims([]uint64{10, 10}))
	require.NoError(t, err)

	data := make([]int32, 25*35)
	for i := range data {
		data[i] = int32(i)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	// Verify chunk coordinator
	totalChunks := ds.chunkCoordinator.GetTotalChunks()
	require.Equal(t, uint64(12), totalChunks) // 3x4 = 12

	err = fw.Close()
	require.NoError(t, err)
}

// TestChunkedDataset_SmallChunks tests many small chunks.
func TestChunkedDataset_SmallChunks(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "small_chunks.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// 100 elements, 5 per chunk → 20 chunks
	ds, err := fw.CreateDataset("/data", Uint8, []uint64{100}, WithChunkDims([]uint64{5}))
	require.NoError(t, err)

	data := make([]uint8, 100)
	for i := range data {
		data[i] = uint8(i % 256)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	require.Equal(t, uint64(20), ds.chunkCoordinator.GetTotalChunks())

	err = fw.Close()
	require.NoError(t, err)
}

// TestChunkedWrite_MultiChunk_RoundTrip writes a 1D dataset with multiple chunks,
// reads it back, and verifies all data is correct. This validates the B-tree key
// byte offset encoding and layout ndims+1 fix (Issue #34).
func TestChunkedWrite_MultiChunk_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "multi_chunk_roundtrip.h5")

	// Write: 100 float64 elements, chunk size 10 = 10 chunks.
	expected := make([]float64, 100)
	for i := range expected {
		expected[i] = float64(i) * 1.5
	}

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/data", Float64, []uint64{100}, WithChunkDims([]uint64{10}))
		require.NoError(t, err)

		err = ds.Write(expected)
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Read back and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found bool
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/data" {
			found = true
			values, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, values, 100, "should have 100 elements")

			for i, v := range values {
				require.InDelta(t, expected[i], v, 1e-10, "element %d mismatch", i)
			}
		}
	})
	require.True(t, found, "dataset /data not found")
}

// TestChunkedWrite_SingleChunk_RoundTrip verifies that single-chunk datasets
// still work correctly after the byte offset encoding fix (regression test).
func TestChunkedWrite_SingleChunk_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "single_chunk_roundtrip.h5")

	// Write: 10 float64 elements, chunk size 10 = 1 chunk.
	expected := make([]float64, 10)
	for i := range expected {
		expected[i] = math.Pi * float64(i)
	}

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/single", Float64, []uint64{10}, WithChunkDims([]uint64{10}))
		require.NoError(t, err)

		err = ds.Write(expected)
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Read back and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found bool
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/single" {
			found = true
			values, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, values, 10)

			for i, v := range values {
				require.InDelta(t, expected[i], v, 1e-10, "element %d mismatch", i)
			}
		}
	})
	require.True(t, found, "dataset /single not found")
}

// TestChunkedWrite_2D_MultiChunk verifies 2D chunked dataset round-trip with
// multiple chunks in each dimension. Validates correct coordinate encoding.
func TestChunkedWrite_2D_MultiChunk(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "2d_multi_chunk.h5")

	rows, cols := uint64(20), uint64(30)
	chunkRows, chunkCols := uint64(10), uint64(10)

	// Write: 20x30 float64 matrix, chunks 10x10 = 2x3 = 6 chunks.
	expected := make([]float64, rows*cols)
	for i := range expected {
		expected[i] = float64(i) * 0.1
	}

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/matrix", Float64,
			[]uint64{rows, cols},
			WithChunkDims([]uint64{chunkRows, chunkCols}))
		require.NoError(t, err)

		err = ds.Write(expected)
		require.NoError(t, err)

		require.NoError(t, fw.Close())
	}()

	// Read back and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found bool
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/matrix" {
			found = true
			values, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, values, int(rows*cols), "should have %d elements", rows*cols)

			for i, v := range values {
				require.InDelta(t, expected[i], v, 1e-10, "element %d mismatch", i)
			}
		}
	})
	require.True(t, found, "dataset /matrix not found")
}

// TestChunkedWrite_LargeDataset verifies a large 1D dataset with many chunks.
// 1000 float64 elements, chunk size 10 = 100 chunks. Tests that all chunk
// coordinates are correctly encoded as byte offsets in the B-tree.
func TestChunkedWrite_LargeDataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "large_chunked.h5")

	n := uint64(1000)
	chunkSize := uint64(10)

	// Write.
	expected := make([]float64, n)
	for i := range expected {
		expected[i] = float64(i) * 0.001
	}

	func() {
		fw, err := CreateForWrite(filename, CreateTruncate)
		require.NoError(t, err)
		defer func() { _ = fw.Close() }()

		ds, err := fw.CreateDataset("/large", Float64,
			[]uint64{n}, WithChunkDims([]uint64{chunkSize}))
		require.NoError(t, err)

		err = ds.Write(expected)
		require.NoError(t, err)

		// Verify correct number of chunks.
		require.Equal(t, n/chunkSize, ds.chunkCoordinator.GetTotalChunks())

		require.NoError(t, fw.Close())
	}()

	// Read back and verify.
	f, err := Open(filename)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	var found bool
	f.Walk(func(path string, obj Object) {
		if ds, ok := obj.(*Dataset); ok && path == "/large" {
			found = true
			values, err := ds.Read()
			require.NoError(t, err)
			require.Len(t, values, int(n), "should have %d elements", n)

			for i, v := range values {
				require.InDelta(t, expected[i], v, 1e-10, "element %d mismatch", i)
			}
		}
	})
	require.True(t, found, "dataset /large not found")
}
