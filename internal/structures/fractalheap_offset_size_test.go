package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

const testHeapHeaderAddr = 100

// heapHeaderWithMaxHeapSize returns a reader holding a minimal FRHP header at
// testHeapHeaderAddr with the given maximum heap size (log2) and an empty root
// indirect block, so parsing never reads a block.
//
// Offsets assume sizeofSize = sizeofAddr = 8 and follow the field order in
// parseFractalHeapHeader; the header itself is 142 bytes there.
func heapHeaderWithMaxHeapSize(maxHeapSizeBits uint16) *bytes.Reader {
	const headerSize = 142
	buf := make([]byte, testHeapHeaderAddr+headerSize)
	h := buf[testHeapHeaderAddr:]

	copy(h[0:4], "FRHP")
	binary.LittleEndian.PutUint16(h[5:7], 8)       // heap ID length
	binary.LittleEndian.PutUint32(h[10:14], 4096)  // max managed object size
	binary.LittleEndian.PutUint16(h[110:112], 4)   // table width
	binary.LittleEndian.PutUint64(h[112:120], 512) // starting block size
	binary.LittleEndian.PutUint64(h[120:128], 65536)
	binary.LittleEndian.PutUint16(h[128:130], maxHeapSizeBits)

	return bytes.NewReader(buf)
}

// TestOpenFractalHeapRejectsInvalidMaxHeapSize is the TASK-055 regression test
// for the offset-width computation. Before the fix
// `uint8((MaxHeapSize+7)/8)` was evaluated in uint16 arithmetic, so 65535
// wrapped and produced a zero-width offset field instead of an error. The C
// decoder requires 0 < max_index <= 8 * sizeof_size (H5HFhdr.c:194 with
// H5HF_SIZEOF_OFFSET_BITS in H5HFpkg.h:133).
func TestOpenFractalHeapRejectsInvalidMaxHeapSize(t *testing.T) {
	for _, bits := range []uint16{0, 65, 65535} {
		_, err := OpenFractalHeap(heapHeaderWithMaxHeapSize(bits), testHeapHeaderAddr, 8, 8, binary.LittleEndian)
		require.Error(t, err, "max heap size %d bits must be rejected", bits)
		require.Contains(t, err.Error(), "max heap size", "bits=%d", bits)
	}
}

// TestOpenFractalHeapOffsetSize pins the valid range: ceil(bits/8) bytes,
// up to the 8 bytes an 8-byte length field can address.
func TestOpenFractalHeapOffsetSize(t *testing.T) {
	for _, tc := range []struct {
		bits uint16
		want uint8
	}{
		{1, 1}, {8, 1}, {9, 2}, {16, 2}, {64, 8},
	} {
		heap, err := OpenFractalHeap(heapHeaderWithMaxHeapSize(tc.bits), testHeapHeaderAddr, 8, 8, binary.LittleEndian)
		require.NoError(t, err, "bits=%d", tc.bits)
		require.Equal(t, tc.want, heap.Header.HeapOffsetSize, "bits=%d", tc.bits)
	}
}
