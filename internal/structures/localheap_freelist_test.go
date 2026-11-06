package structures

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// localHeapImage builds a heap prefix at 0 (8-byte sizes and addresses) with
// a data segment of dataSize bytes at 32 and the given free-list head; fill
// writes into the segment.
func localHeapImage(dataSize, head uint64, fill func(seg []byte)) []byte {
	buf := make([]byte, 32+dataSize)
	copy(buf, "HEAP")
	binary.LittleEndian.PutUint64(buf[8:], dataSize)
	binary.LittleEndian.PutUint64(buf[16:], head)
	binary.LittleEndian.PutUint64(buf[24:], 32)
	if fill != nil {
		fill(buf[32:])
	}
	return buf
}

func putFreeBlock(seg []byte, at, next, size uint64) {
	binary.LittleEndian.PutUint64(seg[at:], next)
	binary.LittleEndian.PutUint64(seg[at+8:], size)
}

// TestLoadLocalHeap_FreeListErrors: a free list that H5HL__fl_deserialize
// (H5HLcache.c:217-280) rejects is corrupt, plus a cycle, which the C loop
// would follow without end.
func TestLoadLocalHeap_FreeListErrors(t *testing.T) {
	tests := []struct {
		name string
		head uint64
		fill func(seg []byte)
		want string
	}{
		{name: "head outside segment", head: 60, want: "outside data segment"},
		{name: "next offset zero", head: 8, fill: func(s []byte) { putFreeBlock(s, 8, 0, 16) }, want: "links to offset 0"},
		{name: "block ends outside segment", head: 8, fill: func(s []byte) { putFreeBlock(s, 8, 1, 64) }, want: "outside data segment"},
		{name: "cycle", head: 8, fill: func(s []byte) {
			putFreeBlock(s, 8, 32, 16)
			putFreeBlock(s, 32, 8, 16)
		}, want: "does not end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := localHeapImage(64, tt.head, tt.fill)
			_, err := LoadLocalHeap(&mockReaderAt{data: img}, 0, createMockSuperblock())
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestLoadLocalHeap_CFreeList: a heap with a C free list loads its blocks
// in list order, and the next insert takes the first block that fits.
func TestLoadLocalHeap_CFreeList(t *testing.T) {
	img := localHeapImage(64, 40, func(s []byte) {
		putFreeBlock(s, 40, 16, 24) // head: 24 bytes at the end
		putFreeBlock(s, 16, 1, 16)  // then 16 bytes in the middle
	})
	heap, err := LoadLocalHeap(&mockReaderAt{data: img}, 0, createMockSuperblock())
	require.NoError(t, err)
	require.Equal(t, []localHeapFreeBlock{{offset: 40, size: 24}, {offset: 16, size: 16}}, heap.free)

	// 15 chars need 16: the 24-byte head cannot split (remainder 8), the
	// 16-byte block fits exactly.
	off, err := heap.AddString("123456789012345")
	require.NoError(t, err)
	require.Equal(t, uint64(16), off)
	require.Equal(t, []localHeapFreeBlock{{offset: 40, size: 24}}, heap.free)
}

// TestLocalHeap_GrowAndAddString covers both branches of the growth in
// H5HL_insert (H5HL.c:592-700).
func TestLocalHeap_GrowAndAddString(t *testing.T) {
	t.Run("last free block at the end is extended", func(t *testing.T) {
		heap := NewLocalHeap(64) // free {8, 56}
		name := strings.Repeat("x", 60)
		off, oldSize := heap.GrowAndAddString(name) // need 64, segment doubles
		require.Equal(t, uint64(64), oldSize)
		require.Equal(t, uint64(8), off)
		require.Equal(t, uint64(128), heap.DataSegmentSize)
		require.Equal(t, []localHeapFreeBlock{{offset: 72, size: 56}}, heap.free)
		got, err := heap.GetString(off)
		require.NoError(t, err)
		require.Equal(t, name, got)
	})
	t.Run("no free block at the end: new block at the head", func(t *testing.T) {
		heap := NewLocalHeap(24) // free {8, 16}
		_, err := heap.AddString("123456789012345")
		require.NoError(t, err)
		require.Empty(t, heap.free)

		off, oldSize := heap.GrowAndAddString("abc") // need 8, grows by 24
		require.Equal(t, uint64(24), oldSize)
		require.Equal(t, uint64(24), off)
		require.Equal(t, uint64(48), heap.DataSegmentSize)
		require.Equal(t, []localHeapFreeBlock{{offset: 32, size: 16}}, heap.free)
	})
}

// TestLocalHeap_WriteToMovedSegment: after the data segment moves
// (H5HL__dblk_realloc sets only dblk_addr, H5HLdblk.c:190), the prefix stays
// where it was and points at the new segment.
func TestLocalHeap_WriteToMovedSegment(t *testing.T) {
	heap := NewLocalHeap(64)
	buf := make([]byte, 2048)
	w := &mockWriter{data: buf}
	require.NoError(t, heap.WriteTo(w, 0))

	off, _ := heap.GrowAndAddString(strings.Repeat("y", 100))
	heap.SetDataSegmentAddress(1000)
	require.NoError(t, heap.WriteTo(w, 0))

	require.Equal(t, uint64(1000), binary.LittleEndian.Uint64(buf[24:32]))
	require.False(t, heap.Contiguous(0))

	loaded, err := LoadLocalHeap(&mockReaderAt{data: buf}, 0, createMockSuperblock())
	require.NoError(t, err)
	require.Equal(t, uint64(1000), loaded.DataSegmentAddress)
	require.Equal(t, heap.free, loaded.free)
	got, err := loaded.GetString(off)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("y", 100), got)
}
