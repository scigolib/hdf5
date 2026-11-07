package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// recordingWriter keeps every write by address.
type recordingWriter struct{ writes map[uint64][]byte }

func (w *recordingWriter) WriteAtAddress(data []byte, addr uint64) error {
	w.writes[addr] = append([]byte(nil), data...)
	return nil
}

// countingAllocator hands out consecutive addresses.
type countingAllocator struct{ next uint64 }

func (a *countingAllocator) Allocate(size uint64) (uint64, error) {
	addr := a.next
	a.next += size
	return addr, nil
}

// growFile is an in-memory file: WriteAtAddress grows it, ReadAt and Size
// read it like a file of its current length.
type growFile struct{ b []byte }

func (m *growFile) WriteAtAddress(data []byte, addr uint64) error {
	if end := int(addr) + len(data); end > len(m.b) {
		m.b = append(m.b, make([]byte, end-len(m.b))...)
	}
	copy(m.b[addr:], data)
	return nil
}

func (m *growFile) ReadAt(p []byte, off int64) (int, error) {
	return bytes.NewReader(m.b).ReadAt(p, off)
}

func (m *growFile) Size() int64 { return int64(len(m.b)) }

// TestBTreeV2_LoadRefusesNodePastFile: a header whose node size puts the
// leaf's node past the end of the file is refused when the tree is loaded,
// as the C library refuses to read the node (H5F_block_read past the EOA).
// Red before the fix (deep review F-A): only the used part of the leaf was
// read, and since leaves are written as whole nodes, a crafted node_size of
// up to 4 GiB became an allocation and an in-place write of that size over
// everything after the leaf.
func TestBTreeV2_LoadRefusesNodePastFile(t *testing.T) {
	sb := &core.Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	for _, nodeSize := range []uint32{1 << 20, 0xFFFFFFFF} {
		bt := NewWritableBTreeV2(512)
		require.NoError(t, bt.InsertRecord("alpha", 0x1234))
		f := &growFile{}
		headerAddr, err := bt.WriteToFile(f, &countingAllocator{next: 64}, sb)
		require.NoError(t, err)

		bt.header.NodeSize = nodeSize // what a crafted file says
		header, err := bt.encodeHeader(sb)
		require.NoError(t, err)
		require.NoError(t, f.WriteAtAddress(header, headerAddr))

		loaded := NewWritableBTreeV2(512)
		err = loaded.LoadFromFile(f, headerAddr, sb)
		require.ErrorIs(t, err, utils.ErrCorrupt, "node size %d in a %d-byte file", nodeSize, len(f.b))
	}
}

// TestBTreeV2_LeafWrittenAtFullNodeSize: a v2 B-tree leaf is written as one
// node-size image, zero past its checksum, as H5B2__cache_leaf_serialize
// writes it (H5B2cache.c: "Clear rest of leaf node"). Red before the fix:
// only the used bytes were written while the full node was allocated, so a
// leaf that ended the file left the file shorter than its end-of-file
// address and the C library refused to open it (dense link storage created
// while writing).
func TestBTreeV2_LeafWrittenAtFullNodeSize(t *testing.T) {
	sb := &core.Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	bt := NewWritableBTreeV2(4096)
	require.NoError(t, bt.InsertRecord("alpha", 0x1234))

	w := &recordingWriter{writes: map[uint64][]byte{}}
	_, err := bt.WriteToFile(w, &countingAllocator{next: 4096}, sb)
	require.NoError(t, err)

	leaf := w.writes[4096]
	require.Len(t, leaf, 4096, "the leaf image is one full node")
	require.Equal(t, "BTLF", string(leaf[:4]))
	used := int(bt.calculateLeafSize(sb))
	require.Equal(t, make([]byte, 4096-used), leaf[used:], "the rest of the node is zero")
}
