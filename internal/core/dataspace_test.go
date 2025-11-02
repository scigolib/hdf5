package core

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDataspaceMessage_Version1_4ByteDims(t *testing.T) {
	// Version 1 with 4-byte dimensions (standard)
	data := make([]byte, 16)
	data[0] = 1 // version
	data[1] = 2 // dimensionality = 2
	data[2] = 0 // flags = 0 (no max dims)
	// reserved bytes 3-7
	binary.LittleEndian.PutUint32(data[8:12], 10)  // dim[0] = 10
	binary.LittleEndian.PutUint32(data[12:16], 20) // dim[1] = 20

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, uint8(1), ds.Version)
	require.Equal(t, DataspaceSimple, ds.Type)
	require.Equal(t, []uint64{10, 20}, ds.Dimensions)
	require.Equal(t, uint64(200), ds.TotalElements())
}

func TestParseDataspaceMessage_Version1_8ByteDims(t *testing.T) {
	// Version 1 with 8-byte dimensions (v0 files)
	data := make([]byte, 24)
	data[0] = 1 // version
	data[1] = 2 // dimensionality = 2
	data[2] = 0 // flags = 0 (no max dims)
	// reserved bytes 3-7
	binary.LittleEndian.PutUint64(data[8:16], 10)  // dim[0] = 10 (8 bytes)
	binary.LittleEndian.PutUint64(data[16:24], 20) // dim[1] = 20 (8 bytes)

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, uint8(1), ds.Version)
	require.Equal(t, DataspaceSimple, ds.Type)
	require.Equal(t, []uint64{10, 20}, ds.Dimensions)
	require.Equal(t, uint64(200), ds.TotalElements())
}

func TestParseDataspaceMessage_WithMaxDims(t *testing.T) {
	// Version 1 with max dimensions (4-byte)
	data := make([]byte, 24)
	data[0] = 1 // version
	data[1] = 2 // dimensionality = 2
	data[2] = 1 // flags = 1 (has max dims)
	// reserved bytes 3-7
	binary.LittleEndian.PutUint32(data[8:12], 10)   // dim[0] = 10
	binary.LittleEndian.PutUint32(data[12:16], 20)  // dim[1] = 20
	binary.LittleEndian.PutUint32(data[16:20], 100) // max_dim[0] = 100
	binary.LittleEndian.PutUint32(data[20:24], 200) // max_dim[1] = 200

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, []uint64{10, 20}, ds.Dimensions)
	require.Equal(t, []uint64{100, 200}, ds.MaxDims)
}

func TestParseDataspaceMessage_WithMaxDims_8Byte(t *testing.T) {
	// Version 1 with max dimensions (8-byte) - v0 files
	data := make([]byte, 40)
	data[0] = 1 // version
	data[1] = 2 // dimensionality = 2
	data[2] = 1 // flags = 1 (has max dims)
	// reserved bytes 3-7
	binary.LittleEndian.PutUint64(data[8:16], 2)  // dim[0] = 2
	binary.LittleEndian.PutUint64(data[16:24], 3) // dim[1] = 3
	binary.LittleEndian.PutUint64(data[24:32], 2) // max_dim[0] = 2
	binary.LittleEndian.PutUint64(data[32:40], 3) // max_dim[1] = 3

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, []uint64{2, 3}, ds.Dimensions)
	require.Equal(t, []uint64{2, 3}, ds.MaxDims)
	require.Equal(t, uint64(6), ds.TotalElements())
}

func TestParseDataspaceMessage_Scalar(t *testing.T) {
	// Scalar dataspace
	data := make([]byte, 8)
	data[0] = 1 // version
	data[1] = 0 // dimensionality = 0 (scalar)
	data[2] = 0 // flags

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, DataspaceScalar, ds.Type)
	require.Equal(t, uint64(1), ds.TotalElements())
}

func TestParseDataspaceMessage_1D(t *testing.T) {
	// 1D array
	data := make([]byte, 12)
	data[0] = 1                                    // version
	data[1] = 1                                    // dimensionality = 1
	data[2] = 0                                    // flags
	binary.LittleEndian.PutUint32(data[8:12], 100) // dim[0] = 100

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.True(t, ds.Is1D())
	require.Equal(t, []uint64{100}, ds.Dimensions)
	require.Equal(t, uint64(100), ds.TotalElements())
}

func TestParseDataspaceMessage_Version2(t *testing.T) {
	// Version 2 (always 8-byte dimensions)
	data := make([]byte, 20)
	data[0] = 2                                   // version
	data[1] = 2                                   // dimensionality = 2
	data[2] = 0                                   // flags
	data[3] = 1                                   // type (simple)
	binary.LittleEndian.PutUint64(data[4:12], 5)  // dim[0] = 5
	binary.LittleEndian.PutUint64(data[12:20], 7) // dim[1] = 7

	ds, err := ParseDataspaceMessage(data)
	require.NoError(t, err)

	require.Equal(t, uint8(2), ds.Version)
	require.Equal(t, []uint64{5, 7}, ds.Dimensions)
	require.Equal(t, uint64(35), ds.TotalElements())
}
