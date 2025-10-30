package hdf5

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateForWrite(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_create_for_write.h5")

	// Create file for writing
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	require.NotNil(t, fw)

	// Close
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filename)
	require.NoError(t, err)

	// Verify file can be opened for reading
	f, err := Open(filename)
	require.NoError(t, err)
	defer f.Close()

	// Verify superblock
	assert.Equal(t, uint8(2), f.SuperblockVersion())
}

func TestCreateDataset_1D_Int32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_dataset_1d_int32.h5")

	// Create file
	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Verify dataset properties
	assert.Equal(t, "/data", ds.name)
	assert.Equal(t, uint64(20), ds.dataSize) // 5 * 4 bytes
	assert.Equal(t, []uint64{5}, ds.dims)
}

func TestCreateDataset_2D_Float64(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_dataset_2d_float64.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 3x4 matrix
	ds, err := fw.CreateDataset("/matrix", Float64, []uint64{3, 4})
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Verify
	assert.Equal(t, uint64(96), ds.dataSize) // 3 * 4 * 8 bytes
	assert.Equal(t, []uint64{3, 4}, ds.dims)
}

func TestCreateDataset_InvalidInputs(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_invalid.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name    string
		dsName  string
		dtype   Datatype
		dims    []uint64
		wantErr string
	}{
		{
			name:    "empty name",
			dsName:  "",
			dtype:   Int32,
			dims:    []uint64{10},
			wantErr: "cannot be empty",
		},
		{
			name:    "name without leading slash",
			dsName:  "data",
			dtype:   Int32,
			dims:    []uint64{10},
			wantErr: "must start with '/'",
		},
		{
			name:    "empty dimensions",
			dsName:  "/data",
			dtype:   Int32,
			dims:    []uint64{},
			wantErr: "cannot be empty",
		},
		{
			name:    "zero dimension",
			dsName:  "/data",
			dtype:   Int32,
			dims:    []uint64{10, 0, 5},
			wantErr: "dimension cannot be 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fw.CreateDataset(tt.dsName, tt.dtype, tt.dims)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestDatasetWrite_Int32(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_write_int32.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)

	// Create dataset
	ds, err := fw.CreateDataset("/integers", Int32, []uint64{5})
	require.NoError(t, err)

	// Write data
	data := []int32{10, 20, 30, 40, 50}
	err = ds.Write(data)
	require.NoError(t, err)

	// Close writer
	err = fw.Close()
	require.NoError(t, err)

	// Verify file exists and has expected size
	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(100), "file should have data written")

	// NOTE: Reading back verification will be added once Component 3 (Groups) is complete.
	// For now, we've verified data was written to file
}

func TestDatasetWrite_Float64(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_write_float64.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create dataset
	ds, err := fw.CreateDataset("/floats", Float64, []uint64{10})
	require.NoError(t, err)

	// Write data
	data := make([]float64, 10)
	for i := range data {
		data[i] = float64(i) * 1.5
	}
	err = ds.Write(data)
	require.NoError(t, err)
}

func TestDatasetWrite_2D_Matrix(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_write_2d.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create 3x4 matrix
	ds, err := fw.CreateDataset("/matrix", Float64, []uint64{3, 4})
	require.NoError(t, err)

	// Write flattened data (row-major order)
	data := []float64{
		1.1, 2.2, 3.3, 4.4, // Row 0
		5.5, 6.6, 7.7, 8.8, // Row 1
		9.9, 10.0, 11.1, 12.2, // Row 2
	}
	err = ds.Write(data)
	require.NoError(t, err)
}

func TestDatasetWrite_AllTypes(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_all_types.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	tests := []struct {
		name  string
		dtype Datatype
		data  interface{}
	}{
		{"int8", Int8, []int8{-1, 0, 1, 127, -128}},
		{"int16", Int16, []int16{-100, 0, 100, 32767, -32768}},
		{"int32", Int32, []int32{-1000, 0, 1000}},
		{"int64", Int64, []int64{-10000, 0, 10000}},
		{"uint8", Uint8, []uint8{0, 1, 255}},
		{"uint16", Uint16, []uint16{0, 100, 65535}},
		{"uint32", Uint32, []uint32{0, 1000, 4294967295}},
		{"uint64", Uint64, []uint64{0, 10000, 18446744073709551615}},
		{"float32", Float32, []float32{-1.5, 0.0, 1.5, 3.14159}},
		{"float64", Float64, []float64{-2.5, 0.0, 2.5, 3.141592653589793}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate dimensions based on data
			var dims []uint64
			switch v := tt.data.(type) {
			case []int8:
				dims = []uint64{uint64(len(v))}
			case []int16:
				dims = []uint64{uint64(len(v))}
			case []int32:
				dims = []uint64{uint64(len(v))}
			case []int64:
				dims = []uint64{uint64(len(v))}
			case []uint8:
				dims = []uint64{uint64(len(v))}
			case []uint16:
				dims = []uint64{uint64(len(v))}
			case []uint32:
				dims = []uint64{uint64(len(v))}
			case []uint64:
				dims = []uint64{uint64(len(v))}
			case []float32:
				dims = []uint64{uint64(len(v))}
			case []float64:
				dims = []uint64{uint64(len(v))}
			}

			// Create dataset
			ds, err := fw.CreateDataset("/"+tt.name, tt.dtype, dims)
			require.NoError(t, err, "failed to create dataset for %s", tt.name)

			// Write data
			err = ds.Write(tt.data)
			require.NoError(t, err, "failed to write data for %s", tt.name)
		})
	}
}

func TestDatasetWrite_TypeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_type_mismatch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create int32 dataset
	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)

	// Try to write float64 data (should fail)
	wrongData := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	err = ds.Write(wrongData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported data type")
}

func TestDatasetWrite_SizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_size_mismatch.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create dataset with 5 elements
	ds, err := fw.CreateDataset("/data", Int32, []uint64{5})
	require.NoError(t, err)

	// Try to write wrong number of elements
	wrongData := []int32{1, 2, 3} // Only 3 elements
	err = ds.Write(wrongData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")
}

func TestMultipleDatasets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_multiple_datasets.h5")

	fw, err := CreateForWrite(filename, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create first dataset
	ds1, err := fw.CreateDataset("/int_data", Int32, []uint64{5})
	require.NoError(t, err)

	// Create second dataset
	ds2, err := fw.CreateDataset("/float_data", Float64, []uint64{10})
	require.NoError(t, err)

	// Write to first dataset
	data1 := []int32{1, 2, 3, 4, 5}
	err = ds1.Write(data1)
	require.NoError(t, err)

	// Write to second dataset
	data2 := make([]float64, 10)
	for i := range data2 {
		data2[i] = float64(i) * 2.5
	}
	err = ds2.Write(data2)
	require.NoError(t, err)

	// Verify datasets have different addresses
	assert.NotEqual(t, ds1.dataAddress, ds2.dataAddress)
}

func TestGetDatatypeInfo(t *testing.T) {
	tests := []struct {
		name          string
		dtype         Datatype
		stringSize    uint32
		wantSize      uint32
		wantErr       bool
		checkClass    bool
		expectedClass int
	}{
		{"Int8", Int8, 0, 1, false, false, 0},
		{"Int32", Int32, 0, 4, false, false, 0},
		{"Int64", Int64, 0, 8, false, false, 0},
		{"Float32", Float32, 0, 4, false, false, 0},
		{"Float64", Float64, 0, 8, false, false, 0},
		{"String without size", String, 0, 0, true, false, 0},
		{"String with size", String, 32, 32, false, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := getDatatypeInfo(tt.dtype, tt.stringSize)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSize, info.size)
		})
	}
}

func TestEncodeFixedPointData(t *testing.T) {
	tests := []struct {
		name         string
		data         interface{}
		elemSize     uint32
		expectedSize uint64
		wantErr      bool
	}{
		{
			name:         "int32 array",
			data:         []int32{10, 20, 30},
			elemSize:     4,
			expectedSize: 12,
			wantErr:      false,
		},
		{
			name:         "uint64 array",
			data:         []uint64{100, 200, 300},
			elemSize:     8,
			expectedSize: 24,
			wantErr:      false,
		},
		{
			name:         "type mismatch",
			data:         []float32{1.0, 2.0},
			elemSize:     4,
			expectedSize: 8,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := encodeFixedPointData(tt.data, tt.elemSize, tt.expectedSize)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, int(tt.expectedSize), len(buf))
		})
	}
}

func TestEncodeFloatData(t *testing.T) {
	tests := []struct {
		name         string
		data         interface{}
		elemSize     uint32
		expectedSize uint64
		wantErr      bool
	}{
		{
			name:         "float32 array",
			data:         []float32{1.5, 2.5, 3.5},
			elemSize:     4,
			expectedSize: 12,
			wantErr:      false,
		},
		{
			name:         "float64 array",
			data:         []float64{1.5, 2.5, 3.5},
			elemSize:     8,
			expectedSize: 24,
			wantErr:      false,
		},
		{
			name:         "type mismatch",
			data:         []int32{1, 2, 3},
			elemSize:     4,
			expectedSize: 12,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := encodeFloatData(tt.data, tt.elemSize, tt.expectedSize)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, int(tt.expectedSize), len(buf))
		})
	}
}
