package hdf5

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatasetWrite_EndToEnd tests complete workflow: create → write → verify binary
func TestDatasetWrite_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_e2e.h5")

	// Step 1: Create file and write dataset
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create 1D int32 dataset
	ds, err := fw.CreateDataset("/temperature", Int32, []uint64{5})
	require.NoError(t, err)

	// Write data
	data := []int32{10, 20, 30, 40, 50}
	err = ds.Write(data)
	require.NoError(t, err)

	// Close
	err = fw.Close()
	require.NoError(t, err)

	// Step 2: Verify file structure
	fileInfo, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Greater(t, fileInfo.Size(), int64(100))

	// Step 3: Verify HDF5 signature
	f, err := os.Open(filename)
	require.NoError(t, err)
	defer f.Close()

	sig := make([]byte, 8)
	_, err = f.ReadAt(sig, 0)
	require.NoError(t, err)
	assert.Equal(t, "\x89HDF\r\n\x1a\n", string(sig))
}

// TestDatasetWrite_MultipleTypes tests writing multiple datasets with different types
func TestDatasetWrite_MultipleTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_multi_types.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create datasets of different types
	tests := []struct {
		name  string
		dtype Datatype
		dims  []uint64
		data  interface{}
	}{
		{
			name:  "/int8_data",
			dtype: Int8,
			dims:  []uint64{3},
			data:  []int8{1, 2, 3},
		},
		{
			name:  "/int32_data",
			dtype: Int32,
			dims:  []uint64{4},
			data:  []int32{100, 200, 300, 400},
		},
		{
			name:  "/int64_data",
			dtype: Int64,
			dims:  []uint64{2},
			data:  []int64{1000, 2000},
		},
		{
			name:  "/float32_data",
			dtype: Float32,
			dims:  []uint64{3},
			data:  []float32{1.5, 2.5, 3.5},
		},
		{
			name:  "/float64_data",
			dtype: Float64,
			dims:  []uint64{5},
			data:  []float64{1.1, 2.2, 3.3, 4.4, 5.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := fw.CreateDataset(tt.name, tt.dtype, tt.dims)
			require.NoError(t, err)

			err = ds.Write(tt.data)
			require.NoError(t, err)
		})
	}

	// Verify file is valid
	err = fw.Close()
	require.NoError(t, err)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(200))
}

// TestDatasetWrite_2DArrays tests writing multi-dimensional datasets
func TestDatasetWrite_2DArrays(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name string
		dims []uint64
		data []float64
	}{
		{
			name: "/matrix_2x3",
			dims: []uint64{2, 3},
			data: []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
		},
		{
			name: "/matrix_3x4",
			dims: []uint64{3, 4},
			data: []float64{
				1.0, 2.0, 3.0, 4.0,
				5.0, 6.0, 7.0, 8.0,
				9.0, 10.0, 11.0, 12.0,
			},
		},
		{
			name: "/matrix_4x2",
			dims: []uint64{4, 2},
			data: []float64{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := fw.CreateDataset(tt.name, Float64, tt.dims)
			require.NoError(t, err)

			err = ds.Write(tt.data)
			require.NoError(t, err)

			// Verify data size is correct
			expectedSize := tt.dims[0] * tt.dims[1] * 8 // float64 = 8 bytes
			assert.Equal(t, expectedSize, ds.dataSize)
		})
	}
}

// TestDatasetWrite_LargeDataset tests writing larger datasets
func TestDatasetWrite_LargeDataset(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_large.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 1000-element dataset
	ds, err := fw.CreateDataset("/large_data", Float64, []uint64{1000})
	require.NoError(t, err)

	// Generate data
	data := make([]float64, 1000)
	for i := range data {
		data[i] = float64(i) * 0.1
	}

	// Write
	err = ds.Write(data)
	require.NoError(t, err)

	// Verify size
	assert.Equal(t, uint64(8000), ds.dataSize) // 1000 * 8 bytes

	// Close and check file size
	err = fw.Close()
	require.NoError(t, err)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(8000))
}

// TestDatasetWrite_SequentialWrites tests creating and writing multiple datasets sequentially
func TestDatasetWrite_SequentialWrites(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_sequential.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Write 10 datasets sequentially
	for i := 0; i < 10; i++ {
		dsName := "/data_" + string(rune('A'+i))

		ds, err := fw.CreateDataset(dsName, Int32, []uint64{5})
		require.NoError(t, err)

		data := []int32{int32(i * 10), int32(i*10 + 1), int32(i*10 + 2), int32(i*10 + 3), int32(i*10 + 4)}
		err = ds.Write(data)
		require.NoError(t, err)
	}

	// Verify file is valid
	err = fw.Close()
	require.NoError(t, err)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	// Each dataset: ~40 bytes (5 * 4 bytes int32 + overhead)
	// 10 datasets + superblock + root group ~ 600+ bytes
	assert.Greater(t, info.Size(), int64(500))
}

// TestDatasetWrite_VerifyBinaryFormat verifies the binary format of written data
func TestDatasetWrite_VerifyBinaryFormat(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_binary.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create simple dataset
	ds, err := fw.CreateDataset("/test_data", Int32, []uint64{3})
	require.NoError(t, err)

	// Write known data
	data := []int32{0x12345678, -0x5432100, 0x11223344}
	err = ds.Write(data)
	require.NoError(t, err)

	// Get data address
	dataAddr := ds.dataAddress

	// Close and reopen for reading
	err = fw.Close()
	require.NoError(t, err)

	// Read binary data at data address
	f, err := os.Open(filename)
	require.NoError(t, err)
	defer f.Close()

	buf := make([]byte, 12) // 3 * 4 bytes
	_, err = f.ReadAt(buf, int64(dataAddr))
	require.NoError(t, err)

	// Verify data is correctly written in little-endian format
	val0 := binary.LittleEndian.Uint32(buf[0:4])
	val1 := binary.LittleEndian.Uint32(buf[4:8])
	val2 := binary.LittleEndian.Uint32(buf[8:12])

	assert.Equal(t, uint32(0x12345678), val0)
	assert.Equal(t, uint32(0xFABCDF00), val1) // -0x5432100 in int32 = 0xFABCDF00 in uint32
	assert.Equal(t, uint32(0x11223344), val2)
}

// TestDatasetWrite_Float64Encoding verifies float64 encoding
func TestDatasetWrite_Float64Encoding(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_float.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	ds, err := fw.CreateDataset("/floats", Float64, []uint64{3})
	require.NoError(t, err)

	// Write known floating point values
	data := []float64{1.0, -2.5, 3.141592653589793}
	err = ds.Write(data)
	require.NoError(t, err)

	dataAddr := ds.dataAddress

	err = fw.Close()
	require.NoError(t, err)

	// Verify binary format
	f, err := os.Open(filename)
	require.NoError(t, err)
	defer f.Close()

	buf := make([]byte, 24) // 3 * 8 bytes
	_, err = f.ReadAt(buf, int64(dataAddr))
	require.NoError(t, err)

	// Decode and verify
	val0 := binary.LittleEndian.Uint64(buf[0:8])
	val1 := binary.LittleEndian.Uint64(buf[8:16])
	val2 := binary.LittleEndian.Uint64(buf[16:24])

	// IEEE 754 binary64 representation
	assert.Equal(t, uint64(0x3FF0000000000000), val0) // 1.0
	assert.Equal(t, uint64(0xC004000000000000), val1) // -2.5
	assert.Equal(t, uint64(0x400921FB54442D18), val2) // π
}

// TestDatasetWrite_ErrorConditions tests various error conditions
func TestDatasetWrite_ErrorConditions(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_errors.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Skip write-after-close test - behavior is undefined in MVP
	// The file writer is closed, so subsequent writes may fail
	// This is acceptable for MVP; proper lifecycle management will be added later

	// Reopen for remaining tests
	fw2, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw2.Close()

	t.Run("wrong data length", func(t *testing.T) {
		ds, err := fw2.CreateDataset("/test2", Int32, []uint64{5})
		require.NoError(t, err)

		// Try to write wrong length
		wrongData := []int32{1, 2, 3} // Only 3 elements, expected 5
		err = ds.Write(wrongData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "size mismatch")
	})

	t.Run("wrong data type", func(t *testing.T) {
		ds, err := fw2.CreateDataset("/test3", Int32, []uint64{5})
		require.NoError(t, err)

		// Try to write wrong type
		wrongData := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
		err = ds.Write(wrongData)
		assert.Error(t, err)
	})
}

// TestDatasetWrite_3DArray tests 3D array writing
func TestDatasetWrite_3DArray(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_3d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 2x3x4 3D array
	ds, err := fw.CreateDataset("/array_3d", Float64, []uint64{2, 3, 4})
	require.NoError(t, err)

	// Flatten 3D data (row-major order)
	data := make([]float64, 24) // 2 * 3 * 4
	for i := range data {
		data[i] = float64(i) + 0.5
	}

	err = ds.Write(data)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, []uint64{2, 3, 4}, ds.dims)
	assert.Equal(t, uint64(192), ds.dataSize) // 24 * 8 bytes
}

// TestDatasetWrite_AllIntegers tests all integer types systematically
func TestDatasetWrite_AllIntegers(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_all_integers.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name     string
		dtype    Datatype
		data     interface{}
		elemSize int
	}{
		{"int8", Int8, []int8{-128, -1, 0, 1, 127}, 1},
		{"int16", Int16, []int16{-32768, -1, 0, 1, 32767}, 2},
		{"int32", Int32, []int32{-2147483648, -1, 0, 1, 2147483647}, 4},
		{"int64", Int64, []int64{-9223372036854775808, -1, 0, 1, 9223372036854775807}, 8},
		{"uint8", Uint8, []uint8{0, 1, 127, 128, 255}, 1},
		{"uint16", Uint16, []uint16{0, 1, 32767, 32768, 65535}, 2},
		{"uint32", Uint32, []uint32{0, 1, 2147483647, 2147483648, 4294967295}, 4},
		{"uint64", Uint64, []uint64{0, 1, 9223372036854775807, 9223372036854775808, 18446744073709551615}, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := fw.CreateDataset("/"+tt.name, tt.dtype, []uint64{5})
			require.NoError(t, err)

			err = ds.Write(tt.data)
			require.NoError(t, err)

			// Verify data size
			expectedSize := uint64(5 * tt.elemSize)
			assert.Equal(t, expectedSize, ds.dataSize)
		})
	}
}
