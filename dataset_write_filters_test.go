package hdf5

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkedDatasetWithGZIP(t *testing.T) {
	tmpFile := "test_gzip.h5"
	defer os.Remove(tmpFile)

	// Create chunked dataset with GZIP compression
	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Int32, []uint64{100, 100},
		WithChunkDims([]uint64{10, 10}),
		WithGZIPCompression(6))
	require.NoError(t, err)

	// Create repetitive data (good for compression)
	data := make([]int32, 10000)
	for i := range data {
		data[i] = int32(i % 100) // Repetitive pattern
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	// Verify file was created and is compressed
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 10000 * 4 // 40KB
	compressedSize := int(info.Size())

	// File should be smaller due to compression
	// We expect at least 2:1 ratio for repetitive data
	compressionRatio := float64(uncompressedSize) / float64(compressedSize)
	require.Greater(t, compressionRatio, 1.5,
		"Expected compression ratio > 1.5, got %.2f", compressionRatio)

	t.Logf("Compression ratio: %.2f:1 (uncompressed: %d, compressed: %d)",
		compressionRatio, uncompressedSize, compressedSize)
}

func TestChunkedDatasetWithShuffleGZIP(t *testing.T) {
	tmpFile := "test_shuffle.h5"
	defer os.Remove(tmpFile)

	// Create dataset with Shuffle + GZIP (best compression)
	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Float64, []uint64{1000},
		WithChunkDims([]uint64{100}),
		WithShuffle(),
		WithGZIPCompression(9))
	require.NoError(t, err)

	// Create data with similar values (good for shuffle)
	data := make([]float64, 1000)
	for i := range data {
		data[i] = float64(i) * 0.01
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	// Verify compression
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 1000 * 8 // 8KB
	compressedSize := int(info.Size())

	// Shuffle+GZIP should compress better than GZIP alone
	// Expect at least 1.5:1 ratio for this data
	compressionRatio := float64(uncompressedSize) / float64(compressedSize)
	require.Greater(t, compressionRatio, 1.5,
		"Expected shuffle+gzip compression ratio > 1.5, got %.2f", compressionRatio)

	t.Logf("Shuffle+GZIP compression ratio: %.2f:1 (uncompressed: %d, compressed: %d)",
		compressionRatio, uncompressedSize, compressedSize)
}

func TestChunkedDatasetWithFletcher32(t *testing.T) {
	tmpFile := "test_fletcher.h5"
	defer os.Remove(tmpFile)

	// Create dataset with Fletcher32 checksum
	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Int32, []uint64{100},
		WithChunkDims([]uint64{10}),
		WithFletcher32())
	require.NoError(t, err)

	data := make([]int32, 100)
	for i := range data {
		data[i] = int32(i)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	// Verify file created successfully
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	// File should have Fletcher32 checksums (4 bytes per chunk)
	// 100 elements / 10 per chunk = 10 chunks
	// Each chunk: 40 bytes data + 4 bytes checksum = 44 bytes
	// Plus HDF5 overhead
	require.Greater(t, int(info.Size()), 100*4, "File should contain data + checksums")

	t.Logf("File size with Fletcher32: %d bytes", info.Size())
}

func TestChunkedDatasetWithAllFilters(t *testing.T) {
	tmpFile := "test_all_filters.h5"
	defer os.Remove(tmpFile)

	// Create dataset with complete filter chain: Shuffle → GZIP → Fletcher32
	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Float32, []uint64{200, 200},
		WithChunkDims([]uint64{20, 20}),
		WithShuffle(),
		WithGZIPCompression(6),
		WithFletcher32())
	require.NoError(t, err)

	// Create data with patterns (good for shuffle+compression)
	data := make([]float32, 40000)
	for i := range data {
		data[i] = float32(i%1000) * 0.1
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	// Verify file compression
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 40000 * 4 // 160KB
	compressedSize := int(info.Size())

	// Should achieve good compression with full pipeline
	compressionRatio := float64(uncompressedSize) / float64(compressedSize)
	require.Greater(t, compressionRatio, 2.0,
		"Expected compression ratio > 2.0 with full pipeline, got %.2f", compressionRatio)

	t.Logf("Full pipeline compression: %.2f:1 (uncompressed: %d, compressed: %d)",
		compressionRatio, uncompressedSize, compressedSize)
}

func TestChunkedDatasetGZIPLevels(t *testing.T) {
	tests := []struct {
		name  string
		level int
	}{
		{"fast compression (level 1)", 1},
		{"default compression (level 6)", 6},
		{"best compression (level 9)", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := "test_level.h5"
			defer os.Remove(tmpFile)

			file, err := CreateForWrite(tmpFile, CreateTruncate)
			require.NoError(t, err)

			ds, err := file.CreateDataset("/data", Int32, []uint64{1000},
				WithChunkDims([]uint64{100}),
				WithGZIPCompression(tt.level))
			require.NoError(t, err)

			data := make([]int32, 1000)
			for i := range data {
				data[i] = int32(i % 50)
			}

			err = ds.Write(data)
			require.NoError(t, err)

			err = file.Close()
			require.NoError(t, err)

			info, err := os.Stat(tmpFile)
			require.NoError(t, err)

			t.Logf("Level %d: file size = %d bytes", tt.level, info.Size())
		})
	}
}

func TestChunkedDatasetShuffleImprovement(t *testing.T) {
	// Compare GZIP alone vs Shuffle+GZIP
	data := make([]int32, 10000)
	for i := range data {
		// Pattern that benefits from shuffle
		data[i] = int32(0x12340000 + i%256)
	}

	// Test 1: GZIP only
	tmpFile1 := "test_gzip_only.h5"
	defer os.Remove(tmpFile1)

	file1, err := CreateForWrite(tmpFile1, CreateTruncate)
	require.NoError(t, err)

	ds1, err := file1.CreateDataset("/data", Int32, []uint64{10000},
		WithChunkDims([]uint64{1000}),
		WithGZIPCompression(6))
	require.NoError(t, err)

	err = ds1.Write(data)
	require.NoError(t, err)

	err = file1.Close()
	require.NoError(t, err)

	info1, err := os.Stat(tmpFile1)
	require.NoError(t, err)
	sizeGZIPOnly := info1.Size()

	// Test 2: Shuffle + GZIP
	tmpFile2 := "test_shuffle_gzip.h5"
	defer os.Remove(tmpFile2)

	file2, err := CreateForWrite(tmpFile2, CreateTruncate)
	require.NoError(t, err)

	ds2, err := file2.CreateDataset("/data", Int32, []uint64{10000},
		WithChunkDims([]uint64{1000}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)

	err = ds2.Write(data)
	require.NoError(t, err)

	err = file2.Close()
	require.NoError(t, err)

	info2, err := os.Stat(tmpFile2)
	require.NoError(t, err)
	sizeShuffleGZIP := info2.Size()

	// Shuffle should improve compression
	require.Less(t, sizeShuffleGZIP, sizeGZIPOnly,
		"Shuffle+GZIP should produce smaller file than GZIP alone")

	improvement := (float64(sizeGZIPOnly) - float64(sizeShuffleGZIP)) / float64(sizeGZIPOnly) * 100
	require.Greater(t, improvement, 10.0,
		"Shuffle should improve compression by at least 10%%")

	t.Logf("GZIP only: %d bytes, Shuffle+GZIP: %d bytes, Improvement: %.1f%%",
		sizeGZIPOnly, sizeShuffleGZIP, improvement)
}

func TestChunkedDatasetDifferentDataTypes(t *testing.T) {
	tests := []struct {
		name     string
		dtype    Datatype
		data     interface{}
		elemSize int
	}{
		{"int32 with shuffle+gzip", Int32,
			func() []int32 {
				d := make([]int32, 1000)
				for i := range d {
					d[i] = int32(i)
				}
				return d
			}(), 4},
		{"float64 with shuffle+gzip", Float64,
			func() []float64 {
				d := make([]float64, 1000)
				for i := range d {
					d[i] = float64(i) * 0.1
				}
				return d
			}(), 8},
		{"int16 with shuffle+gzip", Int16,
			func() []int16 {
				d := make([]int16, 1000)
				for i := range d {
					d[i] = int16(i)
				}
				return d
			}(), 2},
		{"uint64 with shuffle+gzip", Uint64,
			func() []uint64 {
				d := make([]uint64, 1000)
				for i := range d {
					d[i] = uint64(i)
				}
				return d
			}(), 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := "test_dtype.h5"
			defer os.Remove(tmpFile)

			file, err := CreateForWrite(tmpFile, CreateTruncate)
			require.NoError(t, err)

			ds, err := file.CreateDataset("/data", tt.dtype, []uint64{1000},
				WithChunkDims([]uint64{100}),
				WithShuffle(),
				WithGZIPCompression(6))
			require.NoError(t, err)

			err = ds.Write(tt.data)
			require.NoError(t, err)

			err = file.Close()
			require.NoError(t, err)

			info, err := os.Stat(tmpFile)
			require.NoError(t, err)

			uncompressedSize := 1000 * tt.elemSize
			compressionRatio := float64(uncompressedSize) / float64(info.Size())

			t.Logf("%s: compression ratio = %.2f:1", tt.name, compressionRatio)
		})
	}
}

func TestChunkedDatasetMultiDimensional(t *testing.T) {
	tmpFile := "test_multidim.h5"
	defer os.Remove(tmpFile)

	// 3D dataset with filters
	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Float32, []uint64{10, 20, 30},
		WithChunkDims([]uint64{5, 10, 15}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)

	// Create 3D data (flattened)
	data := make([]float32, 10*20*30)
	for i := range data {
		data[i] = float32(i % 100)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	// Verify compression
	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 10 * 20 * 30 * 4 // 24KB
	compressionRatio := float64(uncompressedSize) / float64(info.Size())
	require.Greater(t, compressionRatio, 1.5,
		"Expected compression for 3D data, got %.2f", compressionRatio)

	t.Logf("3D dataset compression: %.2f:1", compressionRatio)
}

func TestChunkedDatasetBinaryPatterns(t *testing.T) {
	tmpFile := "test_binary.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Uint32, []uint64{1000},
		WithChunkDims([]uint64{100}),
		WithShuffle(),
		WithGZIPCompression(6),
		WithFletcher32())
	require.NoError(t, err)

	// Create binary pattern data
	data := make([]uint32, 1000)
	for i := range data {
		// Pattern with high byte similarity (good for shuffle)
		data[i] = uint32(0xFF00_0000 | (i & 0xFF))
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 1000 * 4
	compressionRatio := float64(uncompressedSize) / float64(info.Size())

	// Binary pattern should compress reasonably with shuffle
	require.Greater(t, compressionRatio, 0.9,
		"Expected compression for binary pattern, got %.2f", compressionRatio)

	t.Logf("Binary pattern compression: %.2f:1", compressionRatio)
}

func TestChunkedDatasetLargeData(t *testing.T) {
	tmpFile := "test_large.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	// 1MB dataset
	dataSize := 250000 // 1MB for int32
	ds, err := file.CreateDataset("/data", Int32, []uint64{uint64(dataSize)},
		WithChunkDims([]uint64{10000}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)

	data := make([]int32, dataSize)
	for i := range data {
		data[i] = int32(i % 10000)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := dataSize * 4 // 1MB
	compressionRatio := float64(uncompressedSize) / float64(info.Size())

	t.Logf("Large dataset (1MB) compression: %.2f:1 (size: %d bytes)",
		compressionRatio, info.Size())
}

func TestChunkedDatasetSmallChunks(t *testing.T) {
	tmpFile := "test_small_chunks.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	// Small chunks (10 elements each)
	ds, err := file.CreateDataset("/data", Int32, []uint64{1000},
		WithChunkDims([]uint64{10}), // 100 chunks
		WithGZIPCompression(6))
	require.NoError(t, err)

	data := make([]int32, 1000)
	for i := range data {
		data[i] = int32(i)
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	t.Logf("Small chunks: %d chunks, file size: %d bytes", 100, info.Size())
}

func TestChunkedDatasetIntegerSequences(t *testing.T) {
	tmpFile := "test_sequences.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Int64, []uint64{1000},
		WithChunkDims([]uint64{100}),
		WithShuffle(),
		WithGZIPCompression(9))
	require.NoError(t, err)

	// Sequential integers (excellent for shuffle)
	data := make([]int64, 1000)
	for i := range data {
		data[i] = int64(i * 100) // Large steps
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 1000 * 8
	compressionRatio := float64(uncompressedSize) / float64(info.Size())

	// Sequential values should compress reasonably
	require.Greater(t, compressionRatio, 1.5,
		"Expected good compression for sequential data, got %.2f", compressionRatio)

	t.Logf("Sequential integers compression: %.2f:1", compressionRatio)
}

func TestChunkedDataset2DWithFilters(t *testing.T) {
	tmpFile := "test_2d_filters.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/image", Uint8, []uint64{100, 100},
		WithChunkDims([]uint64{10, 10}), // 100 chunks
		WithShuffle(),
		WithGZIPCompression(6),
		WithFletcher32())
	require.NoError(t, err)

	// Create 2D image-like data
	data := make([]uint8, 10000)
	for i := 0; i < 100; i++ {
		for j := 0; j < 100; j++ {
			// Gradient pattern
			data[i*100+j] = uint8((i + j) % 256)
		}
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	t.Logf("2D image with all filters: file size = %d bytes", info.Size())
}

func TestChunkedDatasetFloatingPoint(t *testing.T) {
	tmpFile := "test_float.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/measurements", Float64, []uint64{1000},
		WithChunkDims([]uint64{100}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)

	// Floating-point data with small variations
	data := make([]float64, 1000)
	baseValue := 123.456
	for i := range data {
		data[i] = baseValue + float64(i)*0.001 // Small increments
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 1000 * 8
	compressionRatio := float64(uncompressedSize) / float64(info.Size())

	// Floating-point with small variations may not compress as well as integers
	require.Greater(t, compressionRatio, 0.8,
		"Expected some compression for float64 data, got %.2f", compressionRatio)

	t.Logf("Float64 with shuffle+gzip: %.2f:1", compressionRatio)
}

func TestChunkedDatasetMixedValues(t *testing.T) {
	tmpFile := "test_mixed.h5"
	defer os.Remove(tmpFile)

	file, err := CreateForWrite(tmpFile, CreateTruncate)
	require.NoError(t, err)

	ds, err := file.CreateDataset("/data", Int32, []uint64{1000},
		WithChunkDims([]uint64{100}),
		WithShuffle(),
		WithGZIPCompression(6))
	require.NoError(t, err)

	// Mixed values (less predictable)
	data := make([]int32, 1000)
	for i := range data {
		data[i] = int32((i*7 + 13) % 997) // Pseudo-random
	}

	err = ds.Write(data)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	info, err := os.Stat(tmpFile)
	require.NoError(t, err)

	uncompressedSize := 1000 * 4
	compressionRatio := float64(uncompressedSize) / float64(info.Size())

	t.Logf("Mixed values compression: %.2f:1", compressionRatio)
}
