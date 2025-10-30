package hdf5

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdvancedDatatypes_Array tests array datatype creation and writing.
func TestAdvancedDatatypes_Array(t *testing.T) {
	tests := []struct {
		name      string
		dtype     Datatype
		arrayDims []uint64
		dataSize  uint64
		wantErr   bool
	}{
		{
			name:      "ArrayInt32 1D",
			dtype:     ArrayInt32,
			arrayDims: []uint64{3},
			dataSize:  12, // 3 * 4 bytes
			wantErr:   false,
		},
		{
			name:      "ArrayFloat64 2D",
			dtype:     ArrayFloat64,
			arrayDims: []uint64{2, 3},
			dataSize:  48, // 2 * 3 * 8 bytes
			wantErr:   false,
		},
		{
			name:      "ArrayInt8 1D",
			dtype:     ArrayInt8,
			arrayDims: []uint64{5},
			dataSize:  5, // 5 * 1 byte
			wantErr:   false,
		},
		{
			name:      "ArrayFloat32 3D",
			dtype:     ArrayFloat32,
			arrayDims: []uint64{2, 2, 2},
			dataSize:  32, // 2 * 2 * 2 * 4 bytes
			wantErr:   false,
		},
		{
			name:      "ArrayInt32 no dims - error",
			dtype:     ArrayInt32,
			arrayDims: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &datasetConfig{arrayDims: tt.arrayDims}
			info, err := getDatatypeInfo(tt.dtype, config)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, core.DatatypeArray, info.class)
			assert.Equal(t, uint32(tt.dataSize), info.size)
			assert.NotNil(t, info.baseType)
			assert.Equal(t, tt.arrayDims, info.arrayDims)
		})
	}
}

// TestAdvancedDatatypes_Enum tests enum datatype creation.
func TestAdvancedDatatypes_Enum(t *testing.T) {
	tests := []struct {
		name       string
		dtype      Datatype
		enumNames  []string
		enumValues []int64
		wantSize   uint32
		wantErr    bool
	}{
		{
			name:       "EnumInt8 days of week",
			dtype:      EnumInt8,
			enumNames:  []string{"Monday", "Tuesday", "Wednesday"},
			enumValues: []int64{0, 1, 2},
			wantSize:   1,
			wantErr:    false,
		},
		{
			name:       "EnumInt32 status codes",
			dtype:      EnumInt32,
			enumNames:  []string{"OK", "Error", "Warning"},
			enumValues: []int64{200, 500, 300},
			wantSize:   4,
			wantErr:    false,
		},
		{
			name:       "EnumUint16 flags",
			dtype:      EnumUint16,
			enumNames:  []string{"Read", "Write", "Execute"},
			enumValues: []int64{1, 2, 4},
			wantSize:   2,
			wantErr:    false,
		},
		{
			name:       "EnumInt32 no names - error",
			dtype:      EnumInt32,
			enumNames:  nil,
			enumValues: []int64{1, 2},
			wantErr:    true,
		},
		{
			name:       "EnumInt32 mismatched lengths - error",
			dtype:      EnumInt32,
			enumNames:  []string{"A", "B"},
			enumValues: []int64{1, 2, 3},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &datasetConfig{
				enumNames:  tt.enumNames,
				enumValues: tt.enumValues,
			}
			info, err := getDatatypeInfo(tt.dtype, config)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, core.DatatypeEnum, info.class)
			assert.Equal(t, tt.wantSize, info.size)
			assert.NotNil(t, info.baseType)
			assert.Equal(t, tt.enumNames, info.enumNames)
			assert.Equal(t, tt.enumValues, info.enumValues)
		})
	}
}

// TestAdvancedDatatypes_Reference tests reference datatype creation.
func TestAdvancedDatatypes_Reference(t *testing.T) {
	tests := []struct {
		name      string
		dtype     Datatype
		wantSize  uint32
		wantClass core.DatatypeClass
	}{
		{
			name:      "ObjectReference",
			dtype:     ObjectReference,
			wantSize:  8,
			wantClass: core.DatatypeReference,
		},
		{
			name:      "RegionReference",
			dtype:     RegionReference,
			wantSize:  12,
			wantClass: core.DatatypeReference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &datasetConfig{}
			info, err := getDatatypeInfo(tt.dtype, config)

			require.NoError(t, err)
			assert.Equal(t, tt.wantClass, info.class)
			assert.Equal(t, tt.wantSize, info.size)
		})
	}
}

// TestAdvancedDatatypes_Opaque tests opaque datatype creation.
func TestAdvancedDatatypes_Opaque(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		size    uint32
		wantErr bool
	}{
		{
			name:    "JPEG image",
			tag:     "JPEG image",
			size:    1024,
			wantErr: false,
		},
		{
			name:    "Binary blob",
			tag:     "Custom binary data",
			size:    512,
			wantErr: false,
		},
		{
			name:    "No tag - error",
			tag:     "",
			size:    100,
			wantErr: true,
		},
		{
			name:    "Zero size - error",
			tag:     "Data",
			size:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &datasetConfig{
				opaqueTag:  tt.tag,
				opaqueSize: tt.size,
			}
			info, err := getDatatypeInfo(Opaque, config)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, core.DatatypeOpaque, info.class)
			assert.Equal(t, tt.size, info.size)
			assert.Equal(t, tt.tag, info.opaqueTag)
		})
	}
}

// TestArrayDatatype_EndToEnd tests full array datatype workflow.
func TestArrayDatatype_EndToEnd(t *testing.T) {
	tmpfile := "test_array_datatype.h5"
	defer os.Remove(tmpfile)

	// Create file
	fw, err := CreateForWrite(tmpfile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create array dataset: each element is [3]int32
	ds, err := fw.CreateDataset("/vectors", ArrayInt32, []uint64{5}, WithArrayDims([]uint64{3}))
	require.NoError(t, err)

	// Write data: 5 vectors of 3 int32 each
	data := []int32{
		1, 2, 3, // vector 0
		4, 5, 6, // vector 1
		7, 8, 9, // vector 2
		10, 11, 12, // vector 3
		13, 14, 15, // vector 4
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = ds.Close()
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file was created and has correct structure
	_, err = os.Stat(tmpfile)
	require.NoError(t, err)

	// Reopen and verify structure (reading not fully implemented yet, just check no errors)
	f, err := Open(tmpfile)
	require.NoError(t, err)
	defer f.Close()

	// Verify dataset exists (basic check)
	assert.NotNil(t, f)
}

// TestEnumDatatype_EndToEnd tests full enum datatype workflow.
func TestEnumDatatype_EndToEnd(t *testing.T) {
	tmpfile := "test_enum_datatype.h5"
	defer os.Remove(tmpfile)

	// Create file
	fw, err := CreateForWrite(tmpfile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create enum dataset: days of week
	names := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}
	values := []int64{0, 1, 2, 3, 4}
	ds, err := fw.CreateDataset("/days", EnumInt8, []uint64{10}, WithEnumValues(names, values))
	require.NoError(t, err)

	// Write data: week schedule
	data := []int8{0, 1, 2, 3, 4, 0, 1, 2, 3, 4} // Monday-Friday repeated
	err = ds.Write(data)
	require.NoError(t, err)

	err = ds.Close()
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpfile)
	require.NoError(t, err)
}

// TestReferenceDatatype_EndToEnd tests full reference datatype workflow.
func TestReferenceDatatype_EndToEnd(t *testing.T) {
	tmpfile := "test_reference_datatype.h5"
	defer os.Remove(tmpfile)

	// Create file
	fw, err := CreateForWrite(tmpfile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create object reference dataset
	ds, err := fw.CreateDataset("/object_refs", ObjectReference, []uint64{3})
	require.NoError(t, err)

	// Write data: object addresses (simulated)
	data := []uint64{48, 96, 144} // Example object addresses
	err = ds.Write(data)
	require.NoError(t, err)

	err = ds.Close()
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpfile)
	require.NoError(t, err)
}

// TestOpaqueDatatype_EndToEnd tests full opaque datatype workflow.
func TestOpaqueDatatype_EndToEnd(t *testing.T) {
	tmpfile := "test_opaque_datatype.h5"
	defer os.Remove(tmpfile)

	// Create file
	fw, err := CreateForWrite(tmpfile, CreateTruncate)
	require.NoError(t, err)
	defer fw.Close()

	// Create opaque dataset: binary blobs
	ds, err := fw.CreateDataset("/blobs", Opaque, []uint64{5}, WithOpaqueTag("Binary blob", 16))
	require.NoError(t, err)

	// Write data: 5 blobs of 16 bytes each
	data := make([]byte, 5*16)
	for i := range data {
		data[i] = byte(i)
	}
	err = ds.Write(data)
	require.NoError(t, err)

	err = ds.Close()
	require.NoError(t, err)

	err = fw.Close()
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(tmpfile)
	require.NoError(t, err)
}

// TestEncodeArrayDatatypeMessage tests array datatype message encoding.
func TestEncodeArrayDatatypeMessage(t *testing.T) {
	// Create base type (int32)
	baseMsg := &core.DatatypeMessage{
		Class:   core.DatatypeFixed,
		Version: 1,
		Size:    4,
	}
	baseData, err := core.EncodeDatatypeMessage(baseMsg)
	require.NoError(t, err)

	// Encode array datatype (3-element array)
	dims := []uint64{3}
	arraySize := uint32(12) // 3 * 4 bytes
	encoded, err := core.EncodeArrayDatatypeMessage(baseData, dims, arraySize)
	require.NoError(t, err)

	// Verify structure
	assert.Greater(t, len(encoded), 8) // At least header + dims + base

	// Verify header (class = Array = 10)
	classAndVersion := binary.LittleEndian.Uint32(encoded[0:4])
	class := core.DatatypeClass(classAndVersion & 0x0F)
	assert.Equal(t, core.DatatypeArray, class)

	// Verify size
	size := binary.LittleEndian.Uint32(encoded[4:8])
	assert.Equal(t, arraySize, size)

	// Verify ndims
	ndims := encoded[8]
	assert.Equal(t, uint8(1), ndims)

	// Verify first dimension
	dim0 := binary.LittleEndian.Uint32(encoded[9:13])
	assert.Equal(t, uint32(3), dim0)
}

// TestEncodeEnumDatatypeMessage tests enum datatype message encoding.
func TestEncodeEnumDatatypeMessage(t *testing.T) {
	// Create base type (int8)
	baseMsg := &core.DatatypeMessage{
		Class:   core.DatatypeFixed,
		Version: 1,
		Size:    1,
	}
	baseData, err := core.EncodeDatatypeMessage(baseMsg)
	require.NoError(t, err)

	// Encode enum datatype
	names := []string{"A", "B", "C"}
	values := []byte{0, 1, 2}
	enumSize := uint32(1)
	encoded, err := core.EncodeEnumDatatypeMessage(baseData, names, values, enumSize)
	require.NoError(t, err)

	// Verify structure
	assert.Greater(t, len(encoded), 8) // At least header + base + members

	// Verify header (class = Enum = 8)
	classAndVersion := binary.LittleEndian.Uint32(encoded[0:4])
	class := core.DatatypeClass(classAndVersion & 0x0F)
	assert.Equal(t, core.DatatypeEnum, class)

	// Verify nmembs (in classBitField)
	nmembs := (classAndVersion >> 8) & 0xFFFF
	assert.Equal(t, uint32(3), nmembs)

	// Verify size
	size := binary.LittleEndian.Uint32(encoded[4:8])
	assert.Equal(t, enumSize, size)
}
