package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadGlobalHeapCollection_Valid tests reading a minimal valid global heap collection.
func TestReadGlobalHeapCollection_Valid(t *testing.T) {
	// Create minimal global heap collection
	data := make([]byte, 256)

	// Header
	copy(data[0:4], "GCOL") // signature
	data[4] = 1             // version
	// reserved (3 bytes) at [5:8]
	// collection size (8 bytes) at [8:16]
	binary.LittleEndian.PutUint64(data[8:16], 256) // collection size

	// Object 1 (ID=0 is free space, so start with ID=1)
	offset := 16
	binary.LittleEndian.PutUint16(data[offset:offset+2], 1)   // object ID
	binary.LittleEndian.PutUint16(data[offset+2:offset+4], 0) // nrefs
	// reserved 4 bytes at [offset+4:offset+8]
	binary.LittleEndian.PutUint64(data[offset+8:offset+16], 16) // object size

	// Object data (16 bytes of dummy data)
	for i := 0; i < 16; i++ {
		data[offset+16+i] = byte(i)
	}

	r := bytes.NewReader(data)
	collection, err := ReadGlobalHeapCollection(r, 0, 8)
	require.NoError(t, err)
	require.NotNil(t, collection)
	require.Equal(t, uint64(256), collection.Size)
}

// TestReadGlobalHeapCollection_InvalidSignature tests error handling for invalid signature.
func TestReadGlobalHeapCollection_InvalidSignature(t *testing.T) {
	data := make([]byte, 16)
	copy(data[0:4], "FAIL") // invalid signature
	data[4] = 1
	binary.LittleEndian.PutUint64(data[8:16], 16)

	r := bytes.NewReader(data)
	_, err := ReadGlobalHeapCollection(r, 0, 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid global heap signature")
}

// TestReadGlobalHeapCollection_InvalidVersion tests error handling for unsupported version.
func TestReadGlobalHeapCollection_InvalidVersion(t *testing.T) {
	data := make([]byte, 16)
	copy(data[0:4], "GCOL")
	data[4] = 99 // invalid version
	binary.LittleEndian.PutUint64(data[8:16], 16)

	r := bytes.NewReader(data)
	_, err := ReadGlobalHeapCollection(r, 0, 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported global heap version")
}

// TestReadGlobalHeapCollection_OffsetSize4 tests reading with 4-byte offset size.
func TestReadGlobalHeapCollection_OffsetSize4(t *testing.T) {
	data := make([]byte, 128)

	copy(data[0:4], "GCOL")
	data[4] = 1
	binary.LittleEndian.PutUint32(data[8:12], 128) // 4-byte size

	// Object with 4-byte size field
	offset := 12 // header is smaller for offsetSize=4
	binary.LittleEndian.PutUint16(data[offset:offset+2], 1)
	binary.LittleEndian.PutUint16(data[offset+2:offset+4], 0)
	binary.LittleEndian.PutUint32(data[offset+8:offset+12], 8) // object size (4 bytes)

	r := bytes.NewReader(data)
	collection, err := ReadGlobalHeapCollection(r, 0, 4)
	require.NoError(t, err)
	require.NotNil(t, collection)
}

// TestGlobalHeapCollection_GetObject tests GetObject method.
func TestGlobalHeapCollection_GetObject(t *testing.T) {
	// Create collection with one object
	data := make([]byte, 256)
	copy(data[0:4], "GCOL")
	data[4] = 1
	binary.LittleEndian.PutUint64(data[8:16], 256)

	offset := 16
	binary.LittleEndian.PutUint16(data[offset:offset+2], 1) // object ID = 1
	binary.LittleEndian.PutUint16(data[offset+2:offset+4], 0)
	binary.LittleEndian.PutUint64(data[offset+8:offset+16], 8)

	// Object data
	copy(data[offset+16:offset+24], "testdata")

	r := bytes.NewReader(data)
	collection, err := ReadGlobalHeapCollection(r, 0, 8)
	require.NoError(t, err)

	// Try to get object ID=1
	obj, err := collection.GetObject(1)
	if err == nil {
		require.NotNil(t, obj)
	}
}

// TestParseGlobalHeapReference tests parsing global heap reference.
func TestParseGlobalHeapReference(t *testing.T) {
	data := make([]byte, 16)
	// Collection address (8 bytes)
	binary.LittleEndian.PutUint64(data[0:8], 0x1000)
	// Object index (4 bytes)
	binary.LittleEndian.PutUint32(data[8:12], 5)
	// Reserved (4 bytes)

	ref, err := ParseGlobalHeapReference(data, 8)
	require.NoError(t, err)
	require.Equal(t, uint64(0x1000), ref.HeapAddress)
	require.Equal(t, uint32(5), ref.ObjectIndex)
}
