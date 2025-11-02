package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEncodeDatatypeReference tests encoding reference datatype.
func TestEncodeDatatypeReference(t *testing.T) {
	dt := &DatatypeMessage{
		Class:   9, // Reference class
		Version: 1,
		Size:    12,
	}

	data, err := encodeDatatypeReference(dt)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.GreaterOrEqual(t, len(data), 8)
}

// TestEncodeDatatypeOpaque tests encoding opaque datatype.
func TestEncodeDatatypeOpaque(t *testing.T) {
	dt := &DatatypeMessage{
		Class:      5, // Opaque class
		Version:    1,
		Size:       16,
		Properties: []byte("OPAQUE_TAG"),
	}

	data, err := encodeDatatypeOpaque(dt)
	require.NoError(t, err)
	require.NotNil(t, data)
}

// TestEncodeArrayDatatypeMessage tests encoding array datatype.
func TestEncodeArrayDatatypeMessage(t *testing.T) {
	// Create base type (int32)
	baseType := make([]byte, 8)
	baseType[0] = 3                                 // version
	baseType[1] = 0                                 // integer class
	binary.LittleEndian.PutUint32(baseType[4:8], 4) // size

	dims := []uint64{2, 2}
	arraySize := uint32(16)

	data, err := EncodeArrayDatatypeMessage(baseType, dims, arraySize)
	require.NoError(t, err)
	require.NotNil(t, data)
}

// TestEncodeEnumDatatypeMessage tests encoding enum datatype.
func TestEncodeEnumDatatypeMessage(t *testing.T) {
	// Create base type (int32)
	baseType := make([]byte, 8)
	baseType[0] = 3                                 // version
	baseType[1] = 0                                 // integer class
	binary.LittleEndian.PutUint32(baseType[4:8], 4) // size

	names := []string{"RED", "GREEN", "BLUE"}
	values := make([]byte, 12) // 3 * 4 bytes
	binary.LittleEndian.PutUint32(values[0:4], 0)
	binary.LittleEndian.PutUint32(values[4:8], 1)
	binary.LittleEndian.PutUint32(values[8:12], 2)

	data, err := EncodeEnumDatatypeMessage(baseType, names, values, 4)
	require.NoError(t, err)
	require.NotNil(t, data)
}
