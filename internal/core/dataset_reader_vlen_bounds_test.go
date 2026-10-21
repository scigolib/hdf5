package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildVLenDataspaceMessage encodes a version 1 dataspace with one dimension.
// The shared helper writes the dimensions at offset 5, but the version 1 header
// is 8 bytes (version, rank, flags, 5 reserved), so this test builds it locally.
func buildVLenDataspaceMessage(dim uint64) []byte {
	data := make([]byte, 8+8)
	data[0] = 1 // version
	data[1] = 1 // dimensionality
	data[2] = 0 // flags
	binary.LittleEndian.PutUint64(data[8:16], dim)

	return data
}

// TestReadDatasetVLenBytes_RejectsTooSmallHeapID is the regression for a panic
// found in review: a variable-length element on disk is a global heap reference
// — 4-byte sequence length, offsetSize-byte heap address, 4-byte object index
// (H5Tvlen.c:300). The datatype size comes from the file, and a value below
// that makes the slice [idStart+4 : idStart+size] run backwards, so one
// corrupted byte panicked the caller. Before the guard this test panics with
// "slice bounds out of range [4:1]".
func TestReadDatasetVLenBytes_RejectsTooSmallHeapID(t *testing.T) {
	header := &ObjectHeader{Messages: []*HeaderMessage{
		{Type: MsgDatatype, Data: buildDatatypeMessage(t, DatatypeVarLen, 1)},
		{Type: MsgDataspace, Data: buildVLenDataspaceMessage(1)},
		{Type: MsgDataLayout, Data: buildCoverageCompactLayoutMsg([]byte{0x11})},
	}}

	sb := &Superblock{Version: Version0, OffsetSize: 8, LengthSize: 8}

	_, err := ReadDatasetVLenBytes(bytes.NewReader(nil), header, sb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "smaller than a heap reference")
}
