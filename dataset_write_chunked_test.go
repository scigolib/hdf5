package hdf5

import (
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
