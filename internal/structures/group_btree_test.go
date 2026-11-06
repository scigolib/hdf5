package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// memFile is an in-memory file for node writes.
type memFile []byte

func (m memFile) WriteAt(p []byte, off int64) (int, error) {
	return copy(m[off:], p), nil
}

// TestGroupBTreeNode_EncodeLayout: a group B-tree node is NodeSize bytes,
// its keys are sizeof_size wide and its children sizeof_addr wide
// (H5B__cache_serialize, H5Bcache.c:265-320; H5G__node_encode_key,
// H5Gnode.c:165), and it decodes back unchanged.
func TestGroupBTreeNode_EncodeLayout(t *testing.T) {
	for _, p := range sizePairs {
		sb := testSB(uint8(p.a), uint8(p.s))
		l := NewGroupBTreeLayout(sb)
		require.Equal(t, 8+2*p.a+32*p.a+33*p.s, int(l.NodeSize()), "addr=%d size=%d", p.a, p.s)

		n := NewGroupBTreeNode(l, 1)
		n.N = 2
		n.Left = 0x40
		copy(n.Keys, []uint64{0, 24, 48})
		copy(n.Children, []uint64{0x100, 0x200})
		img := n.Encode(l)
		require.Len(t, img, int(l.NodeSize()))

		want := []byte{'T', 'R', 'E', 'E', 0, 1, 2, 0}
		sib := make([]byte, 2*p.a)
		putLE(sib, 0x40, p.a)
		putLE(sib[p.a:], ^uint64(0), p.a)
		want = append(want, sib...)
		for i := 0; i < 2; i++ {
			kc := make([]byte, p.s+p.a)
			putLE(kc, n.Keys[i], p.s)
			putLE(kc[p.s:], n.Children[i], p.a)
			want = append(want, kc...)
		}
		last := make([]byte, p.s)
		putLE(last, 48, p.s)
		want = append(want, last...)
		require.Equal(t, want, img[:len(want)], "addr=%d size=%d", p.a, p.s)
		require.Equal(t, make([]byte, len(img)-len(want)), img[len(want):], "unused space is zero")

		file := make(memFile, 2*l.NodeSize())
		require.NoError(t, n.WriteAt(file, l, 8))
		got, err := ReadGroupBTreeNode(bytes.NewReader(file), sb, l, 8, 1)
		require.NoError(t, err)
		require.Equal(t, n, got)
	}
}

// TestGroupBTreeNode_EmptyNodeHasNoKeys: a node without children carries no
// key (H5B__cache_serialize writes the final key only for nchildren > 0).
func TestGroupBTreeNode_EmptyNodeHasNoKeys(t *testing.T) {
	l := NewGroupBTreeLayout(testSB(8, 8))
	img := NewGroupBTreeNode(l, 0).Encode(l)
	require.Equal(t, make([]byte, len(img)-24), img[24:])
}

// encodeGroupNode encodes a group B-tree node of the given level.
func encodeGroupNode(level uint8, keys, children []uint64, a, s int) []byte {
	b := []byte{'T', 'R', 'E', 'E', 0, level, 0, 0}
	binary.LittleEndian.PutUint16(b[6:], uint16(len(children)))
	sib := make([]byte, 2*a)
	putLE(sib, ^uint64(0), a)
	putLE(sib[a:], ^uint64(0), a)
	b = append(b, sib...)
	for i, c := range children {
		kc := make([]byte, s+a)
		putLE(kc, keys[i], s)
		putLE(kc[s:], c, a)
		b = append(b, kc...)
	}
	k := make([]byte, s)
	putLE(k, keys[len(children)], s)
	return append(b, k...)
}

// TestReadGroupBTreeEntries_MultiLevel: entries of a tree of two levels come
// back in key order from every leaf (H5B__iterate_helper, H5B.c:1105-1190).
func TestReadGroupBTreeEntries_MultiLevel(t *testing.T) {
	for _, p := range sizePairs {
		file := make([]byte, 0x2000)
		copy(file, encodeGroupNode(1, []uint64{0, 16, 24}, []uint64{0x400, 0x800}, p.a, p.s))
		copy(file[0x400:], encodeGroupLeaf(0x1000, p.a, p.s))
		copy(file[0x800:], encodeGroupLeaf(0x1400, p.a, p.s))
		all := sampleEntries()
		copy(file[0x1000:], encodeSNOD(all[:2], p.a, p.s))
		copy(file[0x1400:], encodeSNOD(all[2:], p.a, p.s))

		got, err := ReadGroupBTreeEntries(bytes.NewReader(file), 0, testSB(uint8(p.a), uint8(p.s)))
		require.NoError(t, err, "addr=%d size=%d", p.a, p.s)
		require.Len(t, got, 3)
		for i, w := range all {
			require.Equal(t, w.nameOff, got[i].LinkNameOffset, "entry %d addr=%d size=%d", i, p.a, p.s)
			require.Equal(t, w.header, got[i].ObjectAddress, "entry %d addr=%d size=%d", i, p.a, p.s)
		}
	}
}

// TestReadGroupBTreeEntries_ChildLevelMismatch: a child must be one level
// below its parent (H5B__cache_deserialize checks the expected level).
func TestReadGroupBTreeEntries_ChildLevelMismatch(t *testing.T) {
	file := make([]byte, 0x1000)
	copy(file, encodeGroupNode(1, []uint64{0, 24}, []uint64{0x400}, 8, 8))
	copy(file[0x400:], encodeGroupNode(1, []uint64{0, 24}, []uint64{0x800}, 8, 8))
	_, err := ReadGroupBTreeEntries(bytes.NewReader(file), 0, testSB(8, 8))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestReadGroupBTreeNode_TooManyChildren: more than 2K children is corrupt
// (H5Bcache.c:183-184).
func TestReadGroupBTreeNode_TooManyChildren(t *testing.T) {
	sb := testSB(8, 8)
	l := NewGroupBTreeLayout(sb)
	keys := make([]uint64, l.TwoK()+2)
	children := make([]uint64, l.TwoK()+1)
	file := make([]byte, 0x1000)
	copy(file, encodeGroupNode(0, keys, children, 8, 8))
	_, err := ReadGroupBTreeNode(bytes.NewReader(file), sb, l, 0, GroupBTreeLevelUnknown)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestReadGroupSNOD_OverCapacity: a symbol table node holds at most
// 2*sym_leaf_k entries (H5G_NODE_SIZE, H5Gcache.c:150-200).
func TestReadGroupSNOD_OverCapacity(t *testing.T) {
	sb := testSB(8, 8)
	l := NewGroupBTreeLayout(sb)
	entries := make([]testEntry, l.SNODCapacity()+1)
	for i := range entries {
		entries[i] = testEntry{nameOff: uint64(8 * (i + 1)), header: 0x100}
	}
	file := make([]byte, 0x1000)
	copy(file, encodeSNOD(entries, 8, 8))
	_, err := ReadGroupSNOD(bytes.NewReader(file), sb, l, 0)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestGroupBTreeLayout_FileK: K values come from the superblock, the
// defaults only when it has none.
func TestGroupBTreeLayout_FileK(t *testing.T) {
	sb := testSB(4, 8)
	sb.SymLeafK, sb.SymInternalK = 2, 3
	l := NewGroupBTreeLayout(sb)
	require.Equal(t, 6, l.TwoK())
	require.Equal(t, 4, l.SNODCapacity())
	require.Equal(t, uint64(8+2*4+6*4+7*8), l.NodeSize())
	require.Equal(t, uint64(8+4*(8+4+24)), l.SNODSize())

	d := NewGroupBTreeLayout(testSB(8, 8))
	require.Equal(t, 32, d.TwoK())
	require.Equal(t, 8, d.SNODCapacity())
}

// TestLocalHeapRemove: freed space joins a free neighbor on either side,
// fills the hole between two, and is dropped when smaller than a free-list
// entry with no neighbor (H5HL_remove, H5HL.c:719-830).
func TestLocalHeapRemove(t *testing.T) {
	h := NewLocalHeap(256)
	const name = "fifteen_chars__" // 15 bytes and its terminator: one 16-byte object
	var offs []uint64
	for i := 0; i < 3; i++ {
		off, err := h.AddString(name)
		require.NoError(t, err)
		offs = append(offs, off)
	}
	require.Equal(t, []uint64{8, 24, 40}, offs)
	require.Equal(t, []localHeapFreeBlock{{offset: 56, size: 200}}, h.free)

	require.NoError(t, h.Remove(8, 16))
	require.Equal(t, []localHeapFreeBlock{{offset: 8, size: 16}, {offset: 56, size: 200}}, h.free)

	require.NoError(t, h.Remove(40, 16))
	require.Equal(t, []localHeapFreeBlock{{offset: 8, size: 16}, {offset: 40, size: 216}}, h.free)

	require.NoError(t, h.Remove(24, 16))
	require.Equal(t, []localHeapFreeBlock{{offset: 8, size: 248}}, h.free)

	small := NewLocalHeap(256)
	a, err := small.AddString("a")
	require.NoError(t, err)
	_, err = small.AddString("b")
	require.NoError(t, err)
	require.NoError(t, small.Remove(a, 2))
	require.Equal(t, []localHeapFreeBlock{{offset: 24, size: 232}}, small.free, "an 8-byte hole with no free neighbor is not tracked")

	require.ErrorIs(t, h.Remove(250, 16), utils.ErrCorrupt)
	require.ErrorIs(t, h.Remove(8, 0), utils.ErrCorrupt)
}
