package structures

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// testHeapHeaderSize is the HEAP header size for 8-byte offset and length
// fields: signature (4) + version (1) + reserved (3) + size (8) + free list
// (8) + data address (8).
const testHeapHeaderSize = 32

// buildTestHeapHeader builds a synthetic HEAP header for a superblock with
// 8-byte offset and length fields.
func buildTestHeapHeader(order binary.ByteOrder, dataSize, freeList, dataAddr uint64) []byte {
	buf := make([]byte, testHeapHeaderSize)
	copy(buf[0:4], "HEAP")
	buf[4] = 0
	order.PutUint64(buf[8:16], dataSize)
	order.PutUint64(buf[16:24], freeList)
	order.PutUint64(buf[24:32], dataAddr)

	return buf
}

// TestLoadLocalHeap_RejectsDataSegmentBeyondFile proves the data segment size
// is checked against the actual file before it is allocated. The claimed size
// is 8 MB against a 64-byte file: large enough that the pre-fix code performs
// the allocation, small enough that a pre-fix run cannot exhaust memory.
func TestLoadLocalHeap_RejectsDataSegmentBeyondFile(t *testing.T) {
	const claimedSize = 8 * 1024 * 1024

	file := buildTestHeapHeader(binary.LittleEndian, claimedSize, 1, testHeapHeaderSize)
	file = append(file, make([]byte, 32)...) // 64-byte file in total.
	reader := bytes.NewReader(file)

	heap, err := LoadLocalHeap(reader, 0, createMockSuperblock())

	require.Error(t, err)
	require.Nil(t, heap)
	require.Contains(t, err.Error(), "read beyond end of file")
	require.Contains(t, err.Error(), "local heap data")
}

// TestLoadLocalHeap_RejectsFreeListHeadOutsideDataSegment enforces the C
// invariant from H5HLcache.c:189-190: a free-list head other than
// H5HL_FREE_NULL must lie inside the data segment.
func TestLoadLocalHeap_RejectsFreeListHeadOutsideDataSegment(t *testing.T) {
	tests := []struct {
		name     string
		dataSize uint64
		freeList uint64
	}{
		{name: "equal to data segment size", dataSize: 64, freeList: 64},
		{name: "far beyond data segment size", dataSize: 64, freeList: 4096},
		{name: "non-null head in empty data segment", dataSize: 0, freeList: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := buildTestHeapHeader(binary.LittleEndian, tt.dataSize, tt.freeList, testHeapHeaderSize)
			file = append(file, make([]byte, 64)...)
			reader := &mockReaderAt{data: file}

			heap, err := LoadLocalHeap(reader, 0, createMockSuperblock())

			require.Error(t, err)
			require.Nil(t, heap)
			require.Contains(t, err.Error(), "invalid local heap: free list head")
			require.Contains(t, err.Error(), "outside data segment")
		})
	}
}

// TestLoadLocalHeap_RejectsUndefinedDataSegmentAddress mirrors
// H5HLcache.c:200-201, which rejects a data block address that is not defined.
func TestLoadLocalHeap_RejectsUndefinedDataSegmentAddress(t *testing.T) {
	file := buildTestHeapHeader(binary.LittleEndian, 16, 1, math.MaxUint64)
	file = append(file, make([]byte, 16)...)
	reader := bytes.NewReader(file)

	heap, err := LoadLocalHeap(reader, 0, createMockSuperblock())

	require.Error(t, err)
	require.Nil(t, heap)
	require.Contains(t, err.Error(), "invalid local heap: undefined data segment address")
}

// TestLoadLocalHeap_ParsesFreeListHead checks that a valid free-list head
// (neither H5HL_FREE_NULL nor outside the data segment) is accepted and
// exposed on the heap.
func TestLoadLocalHeap_ParsesFreeListHead(t *testing.T) {
	file := buildTestHeapHeader(binary.LittleEndian, 16, 8, testHeapHeaderSize)
	file = append(file, make([]byte, 16)...)
	reader := &mockReaderAt{data: file}

	heap, err := LoadLocalHeap(reader, 0, createMockSuperblock())

	require.NoError(t, err)
	require.NotNil(t, heap)
	require.Equal(t, uint64(8), heap.FreeList)
	require.Len(t, heap.Data, 16)
}
