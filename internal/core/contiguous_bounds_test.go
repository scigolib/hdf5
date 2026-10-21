package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests pin the bound ADR-003 places on contiguous dataset reads: the
// element count comes from file metadata, so a corrupt dataspace may claim far
// more data than the file holds. The readers must reject that claim before
// allocating the buffer. The claimed sizes are megabytes and the backing reader
// holds a few dozen bytes, so the pre-fix code fails with a clean EOF instead
// of killing the test runner with a fatal OOM.

const (
	// oversizeElementCount is the element count the synthetic dataspaces claim.
	oversizeElementCount = 1_000_000
	// smallFileSize is how many bytes actually back the test reader.
	smallFileSize = 64
)

// newBoundsTestSuperblock returns a superblock carrying the 8-byte offset and
// length fields the synthetic messages below are encoded with.
func newBoundsTestSuperblock() *Superblock {
	return &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
}

// newOversizeContiguousHeader builds an object header for a contiguous dataset
// that claims oversizeElementCount elements of the datatype in dtMsg.
func newOversizeContiguousHeader(dtMsg []byte) *ObjectHeader {
	return &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: dtMsg},
			{Type: MsgDataspace, Data: buildDataspaceV1Message([]uint64{oversizeElementCount})},
			{Type: MsgDataLayout, Data: buildContiguousLayoutMessage(0x1000, 0)},
		},
	}
}

// newSmallReader returns a reader smaller than any claimed dataset.
func newSmallReader() *bytes.Reader {
	return bytes.NewReader(make([]byte, smallFileSize))
}

// TestReadDatasetFloat64_ContiguousOversizeDataspace verifies that the float64
// reader rejects a claimed 8,000,000-byte dataset inside a 64-byte file and
// names both numbers in the error.
func TestReadDatasetFloat64_ContiguousOversizeDataspace(t *testing.T) {
	header := newOversizeContiguousHeader(buildFloat64DatatypeMessage())

	_, err := ReadDatasetFloat64(newSmallReader(), header, newBoundsTestSuperblock())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read contiguous data")
	require.Contains(t, err.Error(), "read beyond end of file")
	require.Contains(t, err.Error(), "size=8000000")
	require.Contains(t, err.Error(), "fileSize=64")
}

// TestReadDatasetCompound_ContiguousOversizeDataspace verifies the bound in the
// compound reader: 1,000,000 four-byte structs is 4,000,000 bytes claimed.
func TestReadDatasetCompound_ContiguousOversizeDataspace(t *testing.T) {
	props := buildCompoundV3Props(t, []testCompoundMember{
		{name: "id", offset: 0, dtClass: DatatypeFixed, dtSize: 4},
	})
	header := newOversizeContiguousHeader(buildCompoundDatatypeV3(4, props))

	_, err := ReadDatasetCompound(newSmallReader(), header, newBoundsTestSuperblock())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read contiguous data")
	require.Contains(t, err.Error(), "read beyond end of file")
	require.Contains(t, err.Error(), "size=4000000")
	require.Contains(t, err.Error(), "fileSize=64")
}

// TestReadDatasetStrings_ContiguousOversizeDataspace verifies the bound in the
// string reader: 1,000,000 four-byte strings is 4,000,000 bytes claimed.
func TestReadDatasetStrings_ContiguousOversizeDataspace(t *testing.T) {
	header := newOversizeContiguousHeader(buildCoverageFixedStringDatatypeMsg(4, 0))

	_, err := ReadDatasetStrings(newSmallReader(), header, newBoundsTestSuperblock())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read contiguous data")
	require.Contains(t, err.Error(), "read beyond end of file")
	require.Contains(t, err.Error(), "size=4000000")
	require.Contains(t, err.Error(), "fileSize=64")
}

// TestReadDatasetVLenBytes_ContiguousOversizeDataspace verifies the bound in
// the variable-length reader: 1,000,000 sixteen-byte heap IDs is 16,000,000
// bytes claimed.
func TestReadDatasetVLenBytes_ContiguousOversizeDataspace(t *testing.T) {
	header := newOversizeContiguousHeader(buildVLenDatatypeMessage(16))

	_, err := ReadDatasetVLenBytes(newSmallReader(), header, newBoundsTestSuperblock())

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read contiguous data")
	require.Contains(t, err.Error(), "read beyond end of file")
	require.Contains(t, err.Error(), "size=16000000")
	require.Contains(t, err.Error(), "fileSize=64")
}

// TestReadDatasetFloat64_ContiguousSizeOverflow verifies that an element count
// whose byte size cannot be represented in uint64 is rejected before any
// allocation. 1<<61 elements at 8 bytes each is exactly 2^64, which wraps to
// zero in unchecked arithmetic (H5D__contig_check detects the same condition
// by division, H5Dcontig.c:377-379).
//
// This test must not be run against the pre-fix code: there the wrapped size
// reaches convertToFloat64, which allocates []float64 of 1<<61 elements and
// kills the process. It is compiled from the start but excluded from the red
// run, which uses -run ContiguousOversizeDataspace.
func TestReadDatasetFloat64_ContiguousSizeOverflow(t *testing.T) {
	header := &ObjectHeader{
		Messages: []*HeaderMessage{
			{Type: MsgDatatype, Data: buildFloat64DatatypeMessage()},
			{Type: MsgDataspace, Data: buildDataspaceV1Message([]uint64{1 << 61})},
			{Type: MsgDataLayout, Data: buildContiguousLayoutMessage(0x1000, 0)},
		},
	}

	_, err := ReadDatasetFloat64(newSmallReader(), header, newBoundsTestSuperblock())

	require.Error(t, err)
	require.Contains(t, err.Error(), "dataset size overflow")
}

// buildVLenDatatypeMessage builds a variable-length datatype message. The
// properties are left zeroed: the vlen reader only uses the class and the
// 16-byte heap ID size, and the bound under test is applied before that data
// would be dereferenced.
func buildVLenDatatypeMessage(size uint32) []byte {
	data := make([]byte, 16)
	classAndVersion := uint32(DatatypeVarLen) | (1 << 4)
	binary.LittleEndian.PutUint32(data[0:4], classAndVersion)
	binary.LittleEndian.PutUint32(data[4:8], size)

	return data
}
