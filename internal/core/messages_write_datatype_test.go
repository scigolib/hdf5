package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// int32LEBytes is H5T_STD_I32LE as the C library encodes it: class 0,
// version 1, signed flag, size 4, bit offset 0, precision 32
// (H5Odtype.c:951-1021).
var int32LEBytes = []byte{0x10, 0x08, 0x00, 0x00, 4, 0, 0, 0, 0, 0, 32, 0}

// TestEncodeDatatypeReference tests encoding a dataset region reference
// datatype: no properties, the reference type in the class bit field
// (H5Odtype.c:1310-1314).
func TestEncodeDatatypeReference(t *testing.T) {
	dt, err := NewReferenceDatatype(ReferenceDatasetRegion, 12)
	require.NoError(t, err)

	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	require.Equal(t, []byte{0x17, 0x01, 0x00, 0x00, 12, 0, 0, 0}, data)
}

// TestEncodeDatatypeOpaque tests encoding an opaque datatype: the tag is
// null-padded to a multiple of 8 bytes, whose length is the class bit field
// (H5Odtype.c:1219-1236).
func TestEncodeDatatypeOpaque(t *testing.T) {
	dt, err := NewOpaqueDatatype(16, "OPAQUE_TAG")
	require.NoError(t, err)

	data, err := EncodeDatatypeMessage(dt)
	require.NoError(t, err)
	want := append([]byte{0x15, 16, 0, 0, 16, 0, 0, 0}, "OPAQUE_TAG\x00\x00\x00\x00\x00\x00"...)
	require.Equal(t, want, data)
}

// TestEncodeArrayDatatypeMessage tests encoding a version 3 array datatype
// over an encoded base type (H5Odtype.c:1367-1403).
func TestEncodeArrayDatatypeMessage(t *testing.T) {
	data, err := EncodeArrayDatatypeMessage(int32LEBytes, []uint64{2, 2}, 16)
	require.NoError(t, err)

	want := cat([]byte{0x3A, 0, 0, 0, 16, 0, 0, 0, 2, 2, 0, 0, 0, 2, 0, 0, 0}, int32LEBytes)
	require.Equal(t, want, data)

	// A base type that is not a valid datatype message is refused.
	bad := make([]byte, 8)
	bad[0] = 3 // class string, version 0
	binary.LittleEndian.PutUint32(bad[4:8], 4)
	_, err = EncodeArrayDatatypeMessage(bad, []uint64{2, 2}, 16)
	require.Error(t, err)
}

// TestEncodeEnumDatatypeMessage tests encoding a version 1 enumeration: base
// type, then all names padded to 8 bytes, then all values (H5Odtype.c:1316-1350).
func TestEncodeEnumDatatypeMessage(t *testing.T) {
	names := []string{"RED", "GREEN", "BLUE"}
	values := make([]byte, 12) // 3 * 4 bytes
	binary.LittleEndian.PutUint32(values[0:4], 0)
	binary.LittleEndian.PutUint32(values[4:8], 1)
	binary.LittleEndian.PutUint32(values[8:12], 2)

	data, err := EncodeEnumDatatypeMessage(int32LEBytes, names, values, 4)
	require.NoError(t, err)

	want := cat([]byte{0x18, 3, 0, 0, 4, 0, 0, 0}, int32LEBytes,
		[]byte("RED\x00\x00\x00\x00\x00GREEN\x00\x00\x00BLUE\x00\x00\x00\x00"), values)
	require.Equal(t, want, data)
}
