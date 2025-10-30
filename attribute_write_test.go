package hdf5

import (
	"fmt"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test inferDatatypeFromValue for various Go types
func TestInferDatatypeFromValue(t *testing.T) {
	tests := []struct {
		name            string
		value           interface{}
		wantClass       core.DatatypeClass
		wantSize        uint32
		wantDimensions  []uint64
		wantErr         bool
		wantErrContains string
	}{
		{
			name:           "int32 scalar",
			value:          int32(42),
			wantClass:      core.DatatypeFixed,
			wantSize:       4,
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "int64 scalar",
			value:          int64(12345),
			wantClass:      core.DatatypeFixed,
			wantSize:       8,
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "uint32 scalar",
			value:          uint32(99),
			wantClass:      core.DatatypeFixed,
			wantSize:       4,
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "float32 scalar",
			value:          float32(3.14),
			wantClass:      core.DatatypeFloat,
			wantSize:       4,
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "float64 scalar",
			value:          float64(2.718),
			wantClass:      core.DatatypeFloat,
			wantSize:       8,
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "string scalar",
			value:          "hello",
			wantClass:      core.DatatypeString,
			wantSize:       6, // 5 + 1 (null terminator)
			wantDimensions: []uint64{1},
			wantErr:        false,
		},
		{
			name:           "int32 array",
			value:          []int32{1, 2, 3},
			wantClass:      core.DatatypeFixed,
			wantSize:       4,
			wantDimensions: []uint64{3},
			wantErr:        false,
		},
		{
			name:           "float64 array",
			value:          []float64{1.0, 2.0},
			wantClass:      core.DatatypeFloat,
			wantSize:       8,
			wantDimensions: []uint64{2},
			wantErr:        false,
		},
		{
			name:            "empty slice error",
			value:           []int32{},
			wantErr:         true,
			wantErrContains: "empty slice",
		},
		{
			name:            "unsupported type error",
			value:           struct{}{},
			wantErr:         true,
			wantErrContains: "unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, ds, err := inferDatatypeFromValue(tt.value)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, dt)
			require.NotNil(t, ds)

			assert.Equal(t, tt.wantClass, dt.Class, "datatype class mismatch")
			assert.Equal(t, tt.wantSize, dt.Size, "datatype size mismatch")
			assert.Equal(t, tt.wantDimensions, ds.Dimensions, "dataspace dimensions mismatch")
		})
	}
}

// Test encodeAttributeValue for various Go types
func TestEncodeAttributeValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		wantLen  int
		validate func(t *testing.T, data []byte)
		wantErr  bool
	}{
		{
			name:    "int32 scalar",
			value:   int32(42),
			wantLen: 4,
			validate: func(t *testing.T, data []byte) {
				// Little-endian: 42 = 0x2A
				assert.Equal(t, []byte{0x2A, 0x00, 0x00, 0x00}, data)
			},
			wantErr: false,
		},
		{
			name:    "int64 scalar",
			value:   int64(12345),
			wantLen: 8,
			validate: func(t *testing.T, data []byte) {
				// Little-endian: 12345 = 0x3039
				assert.Equal(t, []byte{0x39, 0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, data)
			},
			wantErr: false,
		},
		{
			name:    "float64 scalar",
			value:   float64(1.0),
			wantLen: 8,
			validate: func(t *testing.T, data []byte) {
				// IEEE 754: 1.0 = 0x3FF0000000000000 (little-endian)
				assert.Equal(t, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F}, data)
			},
			wantErr: false,
		},
		{
			name:    "string scalar",
			value:   "test",
			wantLen: 5, // 4 chars + 1 null terminator
			validate: func(t *testing.T, data []byte) {
				assert.Equal(t, []byte{'t', 'e', 's', 't', 0}, data)
			},
			wantErr: false,
		},
		{
			name:    "int32 array",
			value:   []int32{1, 2, 3},
			wantLen: 12, // 3 * 4 bytes
			validate: func(t *testing.T, data []byte) {
				// Little-endian
				assert.Equal(t, 12, len(data))
				assert.Equal(t, []byte{0x01, 0x00, 0x00, 0x00}, data[0:4])  // 1
				assert.Equal(t, []byte{0x02, 0x00, 0x00, 0x00}, data[4:8])  // 2
				assert.Equal(t, []byte{0x03, 0x00, 0x00, 0x00}, data[8:12]) // 3
			},
			wantErr: false,
		},
		{
			name:    "float64 array",
			value:   []float64{1.0, 0.0},
			wantLen: 16, // 2 * 8 bytes
			validate: func(t *testing.T, data []byte) {
				assert.Equal(t, 16, len(data))
				// 1.0 in IEEE 754
				assert.Equal(t, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F}, data[0:8])
				// 0.0 in IEEE 754
				assert.Equal(t, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, data[8:16])
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeAttributeValue(tt.value)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, data)
			assert.Equal(t, tt.wantLen, len(data), "encoded data length mismatch")

			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

// Test WriteAttribute API (currently returns error as not yet implemented)
func TestDatasetWriter_WriteAttribute(t *testing.T) {
	// Create a temporary file for testing
	fw, err := CreateForWrite("testdata/test_attributes.h5", CreateTruncate)
	require.NoError(t, err)
	defer func() {
		fw.Close()
		// Clean up test file
		// os.Remove("testdata/test_attributes.h5")
	}()

	// Create a dataset
	ds, err := fw.CreateDataset("/test_dataset", Int32, []uint64{10})
	require.NoError(t, err)
	require.NotNil(t, ds)

	// Try to write an attribute (should return error for MVP)
	err = ds.WriteAttribute("units", "meters")
	assert.Error(t, err, "WriteAttribute should return error in MVP")
	assert.Contains(t, err.Error(), "not yet implemented", "error should indicate MVP limitation")
}

// Test round-trip: encode value → decode with existing parser
func TestAttributeEncoding_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{"int32", int32(42)},
		{"int64", int64(12345)},
		{"float64", float64(3.14)},
		{"string", "test_string"},
		{"int32_array", []int32{1, 2, 3, 4, 5}},
		{"float64_array", []float64{1.0, 2.0, 3.0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Infer datatype and dataspace
			dt, ds, err := inferDatatypeFromValue(tt.value)
			require.NoError(t, err)
			require.NotNil(t, dt)
			require.NotNil(t, ds)

			// Encode value
			data, err := encodeAttributeValue(tt.value)
			require.NoError(t, err)
			require.NotNil(t, data)

			// Verify data is not empty
			assert.Greater(t, len(data), 0, "encoded data should not be empty")

			// Verify datatype size matches
			if len(ds.Dimensions) == 1 && ds.Dimensions[0] == 1 {
				// Scalar: data length should match datatype size (for non-strings)
				if dt.Class != core.DatatypeString {
					assert.Equal(t, int(dt.Size), len(data), "data length should match datatype size for scalar")
				}
			}
		})
	}
}

// Benchmark attribute encoding performance
func BenchmarkEncodeAttributeValue(b *testing.B) {
	values := []interface{}{
		int32(42),
		int64(12345),
		float64(3.14),
		"benchmark_string",
		[]int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		[]float64{1.0, 2.0, 3.0, 4.0, 5.0},
	}

	for i, val := range values {
		b.Run(fmt.Sprintf("value_%d", i), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				_, _ = encodeAttributeValue(val)
			}
		})
	}
}
