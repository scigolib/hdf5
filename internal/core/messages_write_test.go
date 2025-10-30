package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeLayoutMessage(t *testing.T) {
	tests := []struct {
		name        string
		layoutClass DataLayoutClass
		dataSize    uint64
		dataAddress uint64
		sb          *Superblock
		wantErr     bool
		validate    func(t *testing.T, data []byte)
	}{
		{
			name:        "contiguous layout with 8-byte offsets",
			layoutClass: LayoutContiguous,
			dataSize:    1024,
			dataAddress: 2048,
			sb: &Superblock{
				OffsetSize: 8,
				LengthSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Expected size: 2 (header) + 8 (address) + 8 (size) = 18 bytes
				assert.Equal(t, 18, len(data))

				// Version should be 3
				assert.Equal(t, byte(3), data[0])

				// Class should be 1 (contiguous)
				assert.Equal(t, byte(1), data[1])

				// Address should be 2048
				addr := binary.LittleEndian.Uint64(data[2:10])
				assert.Equal(t, uint64(2048), addr)

				// Size should be 1024
				size := binary.LittleEndian.Uint64(data[10:18])
				assert.Equal(t, uint64(1024), size)
			},
		},
		{
			name:        "contiguous layout with 4-byte offsets",
			layoutClass: LayoutContiguous,
			dataSize:    512,
			dataAddress: 1024,
			sb: &Superblock{
				OffsetSize: 4,
				LengthSize: 4,
				Endianness: binary.LittleEndian,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Expected size: 2 + 4 + 4 = 10 bytes
				assert.Equal(t, 10, len(data))

				// Version 3
				assert.Equal(t, byte(3), data[0])

				// Contiguous class
				assert.Equal(t, byte(1), data[1])

				// Address
				addr := binary.LittleEndian.Uint32(data[2:6])
				assert.Equal(t, uint32(1024), addr)

				// Size
				size := binary.LittleEndian.Uint32(data[6:10])
				assert.Equal(t, uint32(512), size)
			},
		},
		{
			name:        "compact layout not supported",
			layoutClass: LayoutCompact,
			dataSize:    64,
			dataAddress: 0,
			sb: &Superblock{
				OffsetSize: 8,
				LengthSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr: true,
		},
		{
			name:        "chunked layout not supported",
			layoutClass: LayoutChunked,
			dataSize:    2048,
			dataAddress: 4096,
			sb: &Superblock{
				OffsetSize: 8,
				LengthSize: 8,
				Endianness: binary.LittleEndian,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeLayoutMessage(tt.layoutClass, tt.dataSize, tt.dataAddress, tt.sb)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

func TestEncodeDatatypeMessage_Numeric(t *testing.T) {
	tests := []struct {
		name     string
		dt       *DatatypeMessage
		wantErr  bool
		validate func(t *testing.T, data []byte)
	}{
		{
			name: "int32 little-endian",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Version:       1,
				Size:          4,
				ClassBitField: 0x00, // Little-endian
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8 bytes) + properties (4 bytes for integers)
				assert.Equal(t, 12, len(data))

				// Parse header
				classAndVersion := binary.LittleEndian.Uint32(data[0:4])
				class := DatatypeClass(classAndVersion & 0x0F)
				version := uint8((classAndVersion >> 4) & 0x0F)

				assert.Equal(t, DatatypeFixed, class)
				assert.Equal(t, uint8(1), version)

				// Size
				size := binary.LittleEndian.Uint32(data[4:8])
				assert.Equal(t, uint32(4), size)

				// Properties: byte order should be 0 (little-endian)
				assert.Equal(t, byte(0), data[8])
			},
		},
		{
			name: "int64 little-endian",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Version:       1,
				Size:          8,
				ClassBitField: 0x00,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				assert.Equal(t, 12, len(data))

				size := binary.LittleEndian.Uint32(data[4:8])
				assert.Equal(t, uint32(8), size)
			},
		},
		{
			name: "float32",
			dt: &DatatypeMessage{
				Class:         DatatypeFloat,
				Version:       1,
				Size:          4,
				ClassBitField: 0x00,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + float properties (12)
				assert.Equal(t, 20, len(data))

				class := DatatypeClass(binary.LittleEndian.Uint32(data[0:4]) & 0x0F)
				assert.Equal(t, DatatypeFloat, class)

				size := binary.LittleEndian.Uint32(data[4:8])
				assert.Equal(t, uint32(4), size)

				// Float properties: precision should be 32 bits
				precision := data[9]
				assert.Equal(t, byte(32), precision)
			},
		},
		{
			name: "float64",
			dt: &DatatypeMessage{
				Class:         DatatypeFloat,
				Version:       1,
				Size:          8,
				ClassBitField: 0x00,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				assert.Equal(t, 20, len(data))

				size := binary.LittleEndian.Uint32(data[4:8])
				assert.Equal(t, uint32(8), size)

				// Precision should be 64 bits
				precision := data[9]
				assert.Equal(t, byte(64), precision)
			},
		},
		{
			name: "invalid size zero",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          0,
				ClassBitField: 0x00,
			},
			wantErr: true,
		},
		{
			name: "invalid numeric size 3",
			dt: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          3,
				ClassBitField: 0x00,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeDatatypeMessage(tt.dt)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

func TestEncodeDatatypeMessage_String(t *testing.T) {
	tests := []struct {
		name     string
		dt       *DatatypeMessage
		wantErr  bool
		validate func(t *testing.T, data []byte)
	}{
		{
			name: "fixed-length string",
			dt: &DatatypeMessage{
				Class:         DatatypeString,
				Version:       1,
				Size:          16,
				ClassBitField: 0x00,
			},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + string properties (1)
				assert.Equal(t, 9, len(data))

				class := DatatypeClass(binary.LittleEndian.Uint32(data[0:4]) & 0x0F)
				assert.Equal(t, DatatypeString, class)

				size := binary.LittleEndian.Uint32(data[4:8])
				assert.Equal(t, uint32(16), size)
			},
		},
		{
			name: "string size zero not allowed",
			dt: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          0,
				ClassBitField: 0x00,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeDatatypeMessage(tt.dt)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

func TestEncodeDatatypeMessage_Compound(t *testing.T) {
	// Compound types not supported in MVP
	dt := &DatatypeMessage{
		Class: DatatypeCompound,
		Size:  32,
	}

	_, err := EncodeDatatypeMessage(dt)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestEncodeDataspaceMessage(t *testing.T) {
	tests := []struct {
		name     string
		dims     []uint64
		maxDims  []uint64
		wantErr  bool
		validate func(t *testing.T, data []byte)
	}{
		{
			name:    "1D dataspace",
			dims:    []uint64{10},
			maxDims: nil,
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + 1 dimension (8) = 16 bytes
				assert.Equal(t, 16, len(data))

				// Version 1
				assert.Equal(t, byte(1), data[0])

				// Dimensionality 1
				assert.Equal(t, byte(1), data[1])

				// Flags: no max dims
				assert.Equal(t, byte(0), data[2])

				// Dimension value
				dim := binary.LittleEndian.Uint64(data[8:16])
				assert.Equal(t, uint64(10), dim)
			},
		},
		{
			name:    "2D dataspace",
			dims:    []uint64{3, 4},
			maxDims: nil,
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + 2 dimensions (16) = 24 bytes
				assert.Equal(t, 24, len(data))

				// Dimensionality 2
				assert.Equal(t, byte(2), data[1])

				// Dimensions
				dim0 := binary.LittleEndian.Uint64(data[8:16])
				assert.Equal(t, uint64(3), dim0)

				dim1 := binary.LittleEndian.Uint64(data[16:24])
				assert.Equal(t, uint64(4), dim1)
			},
		},
		{
			name:    "3D dataspace",
			dims:    []uint64{2, 3, 4},
			maxDims: nil,
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + 3 dimensions (24) = 32 bytes
				assert.Equal(t, 32, len(data))

				assert.Equal(t, byte(3), data[1])
			},
		},
		{
			name:    "scalar dataspace (1-element array)",
			dims:    []uint64{1},
			maxDims: nil,
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				assert.Equal(t, 16, len(data))

				dim := binary.LittleEndian.Uint64(data[8:16])
				assert.Equal(t, uint64(1), dim)
			},
		},
		{
			name:    "dataspace with max dimensions",
			dims:    []uint64{10},
			maxDims: []uint64{100},
			wantErr: false,
			validate: func(t *testing.T, data []byte) {
				// Header (8) + dims (8) + maxDims (8) = 24 bytes
				assert.Equal(t, 24, len(data))

				// Flags should have bit 0 set
				assert.Equal(t, byte(0x01), data[2])

				// Dimension
				dim := binary.LittleEndian.Uint64(data[8:16])
				assert.Equal(t, uint64(10), dim)

				// Max dimension
				maxDim := binary.LittleEndian.Uint64(data[16:24])
				assert.Equal(t, uint64(100), maxDim)
			},
		},
		{
			name:    "empty dimensions",
			dims:    []uint64{},
			maxDims: nil,
			wantErr: true,
		},
		{
			name:    "mismatched maxDims length",
			dims:    []uint64{10, 20},
			maxDims: []uint64{100},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := EncodeDataspaceMessage(tt.dims, tt.maxDims)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, data)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip_Layout(t *testing.T) {
	// Round-trip test: encode then decode
	sb := &Superblock{
		OffsetSize: 8,
		LengthSize: 8,
		Endianness: binary.LittleEndian,
	}

	originalAddress := uint64(4096)
	originalSize := uint64(2048)

	// Encode
	encoded, err := EncodeLayoutMessage(LayoutContiguous, originalSize, originalAddress, sb)
	require.NoError(t, err)

	// Decode
	decoded, err := ParseDataLayoutMessage(encoded, sb)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, LayoutContiguous, decoded.Class)
	assert.Equal(t, originalAddress, decoded.DataAddress)
	assert.Equal(t, originalSize, decoded.DataSize)
}

func TestEncodeDecodeRoundTrip_Dataspace(t *testing.T) {
	// Round-trip test: encode then decode
	originalDims := []uint64{5, 10, 15}

	// Encode
	encoded, err := EncodeDataspaceMessage(originalDims, nil)
	require.NoError(t, err)

	// Decode
	decoded, err := ParseDataspaceMessage(encoded)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, DataspaceSimple, decoded.Type)
	assert.Equal(t, originalDims, decoded.Dimensions)
}

func TestWriteUint64(t *testing.T) {
	tests := []struct {
		name       string
		value      uint64
		size       int
		endianness binary.ByteOrder
		expected   []byte
	}{
		{
			name:       "1-byte value",
			value:      0x42,
			size:       1,
			endianness: binary.LittleEndian,
			expected:   []byte{0x42},
		},
		{
			name:       "2-byte value little-endian",
			value:      0x1234,
			size:       2,
			endianness: binary.LittleEndian,
			expected:   []byte{0x34, 0x12},
		},
		{
			name:       "4-byte value little-endian",
			value:      0x12345678,
			size:       4,
			endianness: binary.LittleEndian,
			expected:   []byte{0x78, 0x56, 0x34, 0x12},
		},
		{
			name:       "8-byte value little-endian",
			value:      0x123456789ABCDEF0,
			size:       8,
			endianness: binary.LittleEndian,
			expected:   []byte{0xF0, 0xDE, 0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			writeUint64(buf, tt.value, tt.size, tt.endianness)
			assert.Equal(t, tt.expected, buf)
		})
	}
}

func TestEncodeAttributeMessage(t *testing.T) {
	tests := []struct {
		name      string
		attrName  string
		datatype  *DatatypeMessage
		dataspace *DataspaceMessage
		data      []byte
		wantErr   bool
		validate  func(t *testing.T, encoded []byte)
	}{
		{
			name:     "scalar int32 attribute",
			attrName: "version",
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0, // little-endian, unsigned
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{1}, // scalar (HDF5 uses [1] for scalars)
				MaxDims:    nil,
			},
			data:    []byte{0x2A, 0x00, 0x00, 0x00}, // 42 in little-endian
			wantErr: false,
			validate: func(t *testing.T, encoded []byte) {
				// Parse header
				offset := 0

				// Version should be 3
				assert.Equal(t, byte(3), encoded[offset])
				offset++

				// Flags should be 0
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Name size (includes null terminator: "version" = 7 + 1 = 8)
				nameSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(8), nameSize)
				offset += 2

				// Datatype size (should be 12 bytes for int32)
				datatypeSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(12), datatypeSize)
				offset += 2

				// Dataspace size (should be 16 bytes for scalar: 8 header + 8 for one dimension)
				dataspaceSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(16), dataspaceSize)
				offset += 2

				// Name encoding (should be 0 for ASCII)
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Name (null-terminated)
				name := string(encoded[offset : offset+7])
				assert.Equal(t, "version", name)
				offset += 7
				assert.Equal(t, byte(0), encoded[offset]) // null terminator
				offset++

				// Skip datatype (12 bytes)
				offset += 12

				// Skip dataspace (16 bytes for scalar)
				offset += 16

				// Verify data
				assert.Equal(t, []byte{0x2A, 0x00, 0x00, 0x00}, encoded[offset:offset+4])
			},
		},
		{
			name:     "string attribute",
			attrName: "units",
			datatype: &DatatypeMessage{
				Class:         DatatypeString,
				Size:          10, // Fixed-length string (10 chars)
				ClassBitField: 0,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{1}, // scalar (HDF5 uses [1] for scalars)
				MaxDims:    nil,
			},
			data:    []byte("Celsius\x00\x00\x00"), // Padded to 10 chars
			wantErr: false,
			validate: func(t *testing.T, encoded []byte) {
				// Parse header
				offset := 0

				// Version
				assert.Equal(t, byte(3), encoded[offset])
				offset++

				// Flags
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Name size: "units" = 5 + 1 = 6
				nameSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(6), nameSize)
				offset += 2

				// Datatype size (9 bytes for string)
				datatypeSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(9), datatypeSize)
				offset += 2

				// Dataspace size (16 bytes for scalar: 8 header + 8 for one dimension)
				dataspaceSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(16), dataspaceSize)
				offset += 2

				// Name encoding
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Name
				name := string(encoded[offset : offset+5])
				assert.Equal(t, "units", name)
				offset += 5
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Skip datatype and dataspace
				offset += 9 + 16 // datatype 9, dataspace 16 for scalar

				// Verify data
				assert.Equal(t, []byte("Celsius\x00\x00\x00"), encoded[offset:offset+10])
			},
		},
		{
			name:     "array attribute float64",
			attrName: "calibration",
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{2}, // 1D array with 2 elements
				MaxDims:    nil,
			},
			data:    []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // [1.0, 0.0]
			wantErr: false,
			validate: func(t *testing.T, encoded []byte) {
				// Just verify basic structure
				offset := 0

				// Version
				assert.Equal(t, byte(3), encoded[offset])
				offset++

				// Flags
				assert.Equal(t, byte(0), encoded[offset])
				offset++

				// Name size: "calibration" = 11 + 1 = 12
				nameSize := binary.LittleEndian.Uint16(encoded[offset : offset+2])
				assert.Equal(t, uint16(12), nameSize)

				// Total message should contain:
				// - Header: 9 bytes
				// - Name: 12 bytes
				// - Datatype: 20 bytes (float64)
				// - Dataspace: 16 bytes (1D with 1 dim)
				// - Data: 16 bytes (2 * 8 bytes)
				expectedSize := 9 + 12 + 20 + 16 + 16
				assert.Equal(t, expectedSize, len(encoded))
			},
		},
		{
			name:     "empty name error",
			attrName: "",
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{},
			},
			data:    []byte{0x00, 0x00, 0x00, 0x00},
			wantErr: true,
		},
		{
			name:     "nil datatype error",
			attrName: "test",
			datatype: nil,
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{},
			},
			data:    []byte{0x00},
			wantErr: true,
		},
		{
			name:     "nil dataspace error",
			attrName: "test",
			datatype: &DatatypeMessage{
				Class: DatatypeFixed,
				Size:  4,
			},
			dataspace: nil,
			data:      []byte{0x00},
			wantErr:   true,
		},
		{
			name:     "empty data is valid",
			attrName: "empty_attr",
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{1}, // scalar (HDF5 uses [1] for scalars)
			},
			data:    []byte{}, // No data (unusual but valid)
			wantErr: false,
			validate: func(t *testing.T, encoded []byte) {
				// Should still encode valid message, just with no data section
				assert.True(t, len(encoded) > 0)

				// Version should be 3
				assert.Equal(t, byte(3), encoded[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeAttributeMessage(tt.attrName, tt.datatype, tt.dataspace, tt.data)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, encoded)

			if tt.validate != nil {
				tt.validate(t, encoded)
			}
		})
	}
}

func TestEncodeAttributeMessage_RoundTrip(t *testing.T) {
	// Test that we can encode an attribute and then decode it back
	tests := []struct {
		name      string
		attrName  string
		datatype  *DatatypeMessage
		dataspace *DataspaceMessage
		data      []byte
	}{
		{
			name:     "int32 scalar",
			attrName: "test_int",
			datatype: &DatatypeMessage{
				Class:         DatatypeFixed,
				Size:          4,
				ClassBitField: 0,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{1}, // scalar (HDF5 uses [1] for scalars)
				MaxDims:    nil,
			},
			data: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			name:     "float64 array",
			attrName: "test_array",
			datatype: &DatatypeMessage{
				Class:         DatatypeFloat,
				Size:          8,
				ClassBitField: 0,
			},
			dataspace: &DataspaceMessage{
				Dimensions: []uint64{3},
				MaxDims:    nil,
			},
			data: make([]byte, 24), // 3 * 8 bytes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := EncodeAttributeMessage(tt.attrName, tt.datatype, tt.dataspace, tt.data)
			require.NoError(t, err)
			require.NotNil(t, encoded)

			// Decode (using existing ParseAttributeMessage)
			decoded, err := ParseAttributeMessage(encoded, binary.LittleEndian)
			require.NoError(t, err)
			require.NotNil(t, decoded)

			// Verify round-trip
			assert.Equal(t, tt.attrName, decoded.Name)
			assert.Equal(t, tt.datatype.Class, decoded.Datatype.Class)
			assert.Equal(t, tt.datatype.Size, decoded.Datatype.Size)
			assert.Equal(t, len(tt.dataspace.Dimensions), len(decoded.Dataspace.Dimensions))
			assert.Equal(t, tt.data, decoded.Data)
		})
	}
}
