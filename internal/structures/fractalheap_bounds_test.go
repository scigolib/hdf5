package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRootDirectBlockImageRejectsBlockBeyondReader is the ADR-003 regression
// test for the root direct block image LoadFromFile reads for a rewrite (it
// replaces the tests of the deleted FractalHeap.readDirectBlock, the old
// reader of this package): the block size comes from the heap header, and
// the read must be checked against the reader before the allocation.
//
// The claimed size is a few megabytes rather than gigabytes, so an unchecked
// allocation would still succeed and the test fail on the error text.
func TestRootDirectBlockImageRejectsBlockBeyondReader(t *testing.T) {
	hdr := &FractalHeapHeader{HeapOffsetSize: 4, RootBlockAddr: 100,
		StartingBlockSize: 8 << 20, MaxDirectBlockSize: 8 << 20}

	_, err := readRootDirectBlockImage(bytes.NewReader(make([]byte, 256)), hdr, 0x1000, 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file",
		"direct block size must be checked against the reader before allocation, got: %v", err)
}

// TestRootDirectBlockImageRejectsBlockBeyondMaxDirectBlockSize covers the
// header bound: a direct block cannot be larger than MaxDirectBlockSize.
func TestRootDirectBlockImageRejectsBlockBeyondMaxDirectBlockSize(t *testing.T) {
	hdr := &FractalHeapHeader{HeapOffsetSize: 4, RootBlockAddr: 100,
		StartingBlockSize: 8 << 20, MaxDirectBlockSize: 1024}

	_, err := readRootDirectBlockImage(bytes.NewReader(make([]byte, 256)), hdr, 0x1000, 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum 1024",
		"direct block size must be bounded by MaxDirectBlockSize, got: %v", err)
}

// TestParseIndirectBlockRejectsBlockBeyondReader is the ADR-003 regression test
// for fractalheap_indirect.go ParseIndirectBlock: the block size is computed
// from the row count and table width, and before the fix the buffer was
// allocated with no bound.
//
// Row count and table width are chosen so the claim is about 4 MB: enough to
// fail the test on the unfixed code by its error text, small enough that the
// unfixed allocation cannot kill the test process. A corrupt pair of uint16
// fields can claim ~34 GB (65535 rows x 65535 columns x 8 bytes), and that case
// is rejected by the utils.MaxChunkSize cap inside core.ReadBlock before any
// allocation; it is not exercised here for the same reason.
func TestParseIndirectBlockRejectsBlockBeyondReader(t *testing.T) {
	reader := bytes.NewReader(make([]byte, 256))

	// 65535 rows x 8 columns x 8-byte addresses = 4,194,240 bytes of entries.
	_, err := ParseIndirectBlock(reader, 100, 65535, 8, 8, 4, binary.LittleEndian, 0x1000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file",
		"indirect block size must be checked against the reader before allocation, got: %v", err)
}
