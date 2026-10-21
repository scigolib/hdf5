package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// boundsGCOL builds a global heap collection whose header claims
// collectionSize bytes while the reader holds only len(data) of them.
func boundsGCOL(t *testing.T, collectionSize uint64, actual int) *bytes.Reader {
	t.Helper()

	data := make([]byte, actual)
	copy(data[0:4], "GCOL")
	data[4] = 1 // version
	binary.LittleEndian.PutUint64(data[8:16], collectionSize)

	return bytes.NewReader(data)
}

// TestReadGlobalHeapCollectionRejectsCollectionBeyondReader is the ADR-003
// regression test for globalheap.go: the collection size comes straight from
// the GCOL header, and before the fix the buffer was allocated before anything
// checked it against the file.
//
// The claimed size is a few megabytes rather than gigabytes on purpose: on the
// unfixed code the allocation still succeeds and the test fails on the error
// text. A claim above utils.MaxChunkSize would attempt a gigabyte-sized
// allocation on the unfixed code, and a Go allocation failure is a fatal
// runtime throw rather than an error, so the hard-cap path is guarded by
// ReadBlock and reviewed instead of being exercised by this test.
func TestReadGlobalHeapCollectionRejectsCollectionBeyondReader(t *testing.T) {
	r := boundsGCOL(t, 8<<20, 128)

	_, err := ReadGlobalHeapCollection(r, 0, 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file",
		"size from the GCOL header must be checked against the reader before allocation, got: %v", err)
}

// TestReadGlobalHeapCollectionRejectsOversizedFreeSpaceObject covers the
// free-space branch: its object size is converted to int to advance the offset,
// so a size that does not fit the platform used to wrap the offset negative and
// index the collection buffer out of range.
func TestReadGlobalHeapCollectionRejectsOversizedFreeSpaceObject(t *testing.T) {
	data := make([]byte, 128)
	copy(data[0:4], "GCOL")
	data[4] = 1
	binary.LittleEndian.PutUint64(data[8:16], 128)

	// Object 0 is free space: ID 0, then nrefs, reserved, and a corrupt size.
	offset := 16
	binary.LittleEndian.PutUint16(data[offset:offset+2], 0)
	binary.LittleEndian.PutUint64(data[offset+8:offset+16], 1<<63)

	require.NotPanics(t, func() {
		_, err := ReadGlobalHeapCollection(bytes.NewReader(data), 0, 8)
		require.Error(t, err)
	})
}
