package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// fractalHeapHeaderRawBytes builds a minimal FRHP header for
// sizeofSize = sizeofAddr = 8 with the given maximum heap size (log2).
// Field offsets follow readFractalHeapHeaderRaw.
func fractalHeapHeaderRawBytes(maxHeapSizeBits uint16) []byte {
	buf := make([]byte, 144)

	copy(buf[0:4], "FRHP")
	binary.LittleEndian.PutUint16(buf[5:7], 8)       // heap ID length
	binary.LittleEndian.PutUint32(buf[10:14], 4096)  // max managed object size
	binary.LittleEndian.PutUint16(buf[110:112], 4)   // table width
	binary.LittleEndian.PutUint64(buf[112:120], 512) // starting block size
	binary.LittleEndian.PutUint64(buf[120:128], 65536)
	binary.LittleEndian.PutUint16(buf[128:130], maxHeapSizeBits)

	return buf
}

// TestReadFractalHeapHeaderRawRejectsInvalidMaxHeapSize is the TASK-055
// regression test for the attribute-path copy of the offset-width
// computation: `uint8((MaxHeapSize+7)/8)` wrapped in uint16 arithmetic, so a
// corrupt 65535 claimed a zero-width offset field instead of failing. The C
// decoder requires 0 < max_index <= 8 * sizeof_size (H5HFhdr.c:194).
func TestReadFractalHeapHeaderRawRejectsInvalidMaxHeapSize(t *testing.T) {
	sb := &Superblock{Endianness: binary.LittleEndian, LengthSize: 8, OffsetSize: 8}

	for _, bits := range []uint16{0, 65, 65535} {
		_, err := readFractalHeapHeaderRaw(bytes.NewReader(fractalHeapHeaderRawBytes(bits)), 0, sb)
		require.Error(t, err, "max heap size %d bits must be rejected", bits)
		require.Contains(t, err.Error(), "max heap size", "bits=%d", bits)
	}
}

// TestReadFractalHeapHeaderRawOffsetSize pins the valid range for the
// attribute path against the heap path (see
// structures.TestOpenFractalHeapOffsetSize).
func TestReadFractalHeapHeaderRawOffsetSize(t *testing.T) {
	sb := &Superblock{Endianness: binary.LittleEndian, LengthSize: 8, OffsetSize: 8}

	for _, tc := range []struct {
		bits uint16
		want uint8
	}{
		{1, 1}, {8, 1}, {9, 2}, {16, 2}, {64, 8},
	} {
		header, err := readFractalHeapHeaderRaw(bytes.NewReader(fractalHeapHeaderRawBytes(tc.bits)), 0, sb)
		require.NoError(t, err, "bits=%d", tc.bits)
		require.Equal(t, tc.want, header.HeapOffsetSize, "bits=%d", tc.bits)
	}
}
