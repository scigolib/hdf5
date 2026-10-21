package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// boundsFractalHeap returns a heap that reads from a small buffer at address
// 100, so any header-claimed block size beyond a few hundred bytes is
// necessarily outside the reader.
func boundsFractalHeap(buf []byte, header *FractalHeapHeader) *FractalHeap {
	return &FractalHeap{
		Header:     header,
		reader:     bytes.NewReader(buf),
		headerAddr: 0x1000,
		sizeofAddr: 8,
		endianness: binary.LittleEndian,
	}
}

// TestReadDirectBlockRejectsBlockBeyondReader is the ADR-003 regression test
// for fractalheap.go readDirectBlock: the block size comes from the heap
// header, and before the fix it was allocated and read without any bound.
//
// The claimed size is a few megabytes rather than gigabytes: on the unfixed
// code the allocation still succeeds and the test fails on the error text. A
// claim above utils.MaxChunkSize would attempt a gigabyte allocation first, and
// a Go allocation failure is a fatal runtime throw rather than a test failure.
func TestReadDirectBlockRejectsBlockBeyondReader(t *testing.T) {
	heap := boundsFractalHeap(make([]byte, 256), &FractalHeapHeader{HeapOffsetSize: 4})

	_, err := heap.readDirectBlock(100, 8<<20)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read beyond end of file",
		"direct block size must be checked against the reader before allocation, got: %v", err)
}

// TestReadDirectBlockRejectsBlockBeyondMaxDirectBlockSize covers the header
// bound: a direct block cannot be larger than MaxDirectBlockSize.
func TestReadDirectBlockRejectsBlockBeyondMaxDirectBlockSize(t *testing.T) {
	header := &FractalHeapHeader{
		HeapOffsetSize:     4,
		MaxDirectBlockSize: 1024,
	}
	heap := boundsFractalHeap(make([]byte, 256), header)

	_, err := heap.readDirectBlock(100, 8<<20)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds MaxDirectBlockSize",
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
