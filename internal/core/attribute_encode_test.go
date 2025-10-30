package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeAttributeMessage_Int32 tests encoding int32 attribute.
func TestEncodeAttributeMessage_Int32(t *testing.T) {
	attr := &Attribute{
		Name: "test_int",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08, // Signed integer
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceScalar,
			Dimensions: []uint64{1},
			MaxDims:    nil,
		},
		Data: []byte{0x2A, 0x00, 0x00, 0x00}, // 42 in little-endian
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Verify message structure
	assert.Equal(t, byte(3), encoded[0], "Version should be 3")
	assert.Equal(t, byte(0), encoded[1], "Flags should be 0")

	// Name size
	nameSize := binary.LittleEndian.Uint16(encoded[2:4])
	assert.Equal(t, uint16(8+1), nameSize, "Name size should include null terminator") // "test_int\0"

	// Name encoding
	assert.Equal(t, byte(0), encoded[8], "Name encoding should be ASCII (0)")

	// Name
	assert.Equal(t, "test_int", string(encoded[9:9+8]), "Name should match")
	assert.Equal(t, byte(0), encoded[9+8], "Name should be null-terminated")
}

// TestEncodeAttributeMessage_Float64 tests encoding float64 attribute.
func TestEncodeAttributeMessage_Float64(t *testing.T) {
	attr := &Attribute{
		Name: "pi",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFloat,
			Version:       1,
			Size:          8,
			ClassBitField: 0,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceScalar,
			Dimensions: []uint64{1},
			MaxDims:    nil,
		},
		Data: []byte{0x18, 0x2D, 0x44, 0x54, 0xFB, 0x21, 0x09, 0x40}, // π in float64
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Round-trip test
	decoded, err := ParseAttributeMessage(encoded, sb.Endianness)
	require.NoError(t, err)
	assert.Equal(t, attr.Name, decoded.Name)
	assert.Equal(t, attr.Data, decoded.Data)
}

// TestEncodeAttributeMessage_String tests encoding string attribute.
func TestEncodeAttributeMessage_String(t *testing.T) {
	attr := &Attribute{
		Name: "label",
		Datatype: &DatatypeMessage{
			Class:         DatatypeString,
			Version:       1,
			Size:          6, // "hello\0"
			ClassBitField: 0,
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceScalar,
			Dimensions: []uint64{1},
			MaxDims:    nil,
		},
		Data: []byte("hello\x00"),
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Round-trip test
	decoded, err := ParseAttributeMessage(encoded, sb.Endianness)
	require.NoError(t, err)
	assert.Equal(t, attr.Name, decoded.Name)
	assert.Equal(t, attr.Data, decoded.Data)
}

// TestEncodeAttributeMessage_Array tests encoding array attribute.
func TestEncodeAttributeMessage_Array(t *testing.T) {
	attr := &Attribute{
		Name: "values",
		Datatype: &DatatypeMessage{
			Class:         DatatypeFixed,
			Version:       1,
			Size:          4,
			ClassBitField: 0x08, // Signed integer
		},
		Dataspace: &DataspaceMessage{
			Version:    1,
			Type:       DataspaceSimple,
			Dimensions: []uint64{3}, // Array of 3 elements
			MaxDims:    nil,
		},
		Data: []byte{
			0x01, 0x00, 0x00, 0x00, // 1
			0x02, 0x00, 0x00, 0x00, // 2
			0x03, 0x00, 0x00, 0x00, // 3
		},
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.NoError(t, err)
	require.NotNil(t, encoded)

	// Round-trip test
	decoded, err := ParseAttributeMessage(encoded, sb.Endianness)
	require.NoError(t, err)
	assert.Equal(t, attr.Name, decoded.Name)
	assert.Equal(t, attr.Data, decoded.Data)
	assert.Equal(t, len(attr.Dataspace.Dimensions), len(decoded.Dataspace.Dimensions))
	if len(decoded.Dataspace.Dimensions) > 0 {
		assert.Equal(t, attr.Dataspace.Dimensions[0], decoded.Dataspace.Dimensions[0])
	}
}

// TestEncodeAttributeFromStruct_RoundTrip tests encoding then decoding.
func TestEncodeAttributeFromStruct_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		attr *Attribute
	}{
		{
			name: "int32 scalar",
			attr: &Attribute{
				Name: "temperature",
				Datatype: &DatatypeMessage{
					Class:         DatatypeFixed,
					Version:       1,
					Size:          4,
					ClassBitField: 0x08,
				},
				Dataspace: &DataspaceMessage{
					Version:    1,
					Type:       DataspaceScalar,
					Dimensions: []uint64{1},
				},
				Data: []byte{0x14, 0x00, 0x00, 0x00}, // 20
			},
		},
		{
			name: "float64 scalar",
			attr: &Attribute{
				Name: "coefficient",
				Datatype: &DatatypeMessage{
					Class:         DatatypeFloat,
					Version:       1,
					Size:          8,
					ClassBitField: 0,
				},
				Dataspace: &DataspaceMessage{
					Version:    1,
					Type:       DataspaceScalar,
					Dimensions: []uint64{1},
				},
				Data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F}, // 1.0
			},
		},
		{
			name: "int32 array",
			attr: &Attribute{
				Name: "indices",
				Datatype: &DatatypeMessage{
					Class:         DatatypeFixed,
					Version:       1,
					Size:          4,
					ClassBitField: 0x08,
				},
				Dataspace: &DataspaceMessage{
					Version:    1,
					Type:       DataspaceSimple,
					Dimensions: []uint64{5},
				},
				Data: []byte{
					0x0A, 0x00, 0x00, 0x00, // 10
					0x14, 0x00, 0x00, 0x00, // 20
					0x1E, 0x00, 0x00, 0x00, // 30
					0x28, 0x00, 0x00, 0x00, // 40
					0x32, 0x00, 0x00, 0x00, // 50
				},
			},
		},
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded, err := EncodeAttributeFromStruct(tt.attr, sb)
			require.NoError(t, err)
			require.NotNil(t, encoded)

			// Decode
			decoded, err := ParseAttributeMessage(encoded, sb.Endianness)
			require.NoError(t, err)

			// Verify
			assert.Equal(t, tt.attr.Name, decoded.Name)
			assert.Equal(t, tt.attr.Datatype.Class, decoded.Datatype.Class)
			assert.Equal(t, tt.attr.Datatype.Size, decoded.Datatype.Size)
			assert.Equal(t, tt.attr.Data, decoded.Data)
		})
	}
}

// TestEncodeAttributeMessage_EmptyName tests encoding with empty name (error case).
func TestEncodeAttributeMessage_EmptyName(t *testing.T) {
	attr := &Attribute{
		Name: "", // Empty name
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: &DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{0x01, 0x00, 0x00, 0x00},
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.Error(t, err)
	require.Nil(t, encoded)
	assert.Contains(t, err.Error(), "attribute name cannot be empty")
}

// TestEncodeAttributeMessage_NilAttribute tests encoding nil attribute (error case).
func TestEncodeAttributeMessage_NilAttribute(t *testing.T) {
	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(nil, sb)
	require.Error(t, err)
	require.Nil(t, encoded)
	assert.Contains(t, err.Error(), "attribute is nil")
}

// TestEncodeAttributeMessage_NilDatatype tests encoding with nil datatype (error case).
func TestEncodeAttributeMessage_NilDatatype(t *testing.T) {
	attr := &Attribute{
		Name:     "test",
		Datatype: nil, // Nil datatype
		Dataspace: &DataspaceMessage{
			Dimensions: []uint64{1},
		},
		Data: []byte{0x01, 0x00, 0x00, 0x00},
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.Error(t, err)
	require.Nil(t, encoded)
	assert.Contains(t, err.Error(), "datatype is nil")
}

// TestEncodeAttributeMessage_NilDataspace tests encoding with nil dataspace (error case).
func TestEncodeAttributeMessage_NilDataspace(t *testing.T) {
	attr := &Attribute{
		Name: "test",
		Datatype: &DatatypeMessage{
			Class: DatatypeFixed,
			Size:  4,
		},
		Dataspace: nil, // Nil dataspace
		Data:      []byte{0x01, 0x00, 0x00, 0x00},
	}

	sb := &Superblock{
		Endianness: binary.LittleEndian,
	}

	encoded, err := EncodeAttributeFromStruct(attr, sb)
	require.Error(t, err)
	require.Nil(t, encoded)
	assert.Contains(t, err.Error(), "dataspace is nil")
}
