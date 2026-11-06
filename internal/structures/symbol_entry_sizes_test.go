package structures

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/stretchr/testify/require"
)

// The tests in this file pin symbol table entries and group B-tree keys to the
// C library: an entry's link name offset is sizeof_size wide (H5G_ent_decode,
// H5Gent.c:125; H5G_SIZEOF_ENTRY, H5Gprivate.h:38-43), and so is a group
// B-tree key (H5Gnode.c:1081, :165).

// putLE stores v little-endian in a field of the given width.
func putLE(b []byte, v uint64, width int) {
	for i := 0; i < width; i++ {
		b[i] = byte(v >> (8 * i))
	}
}

type testEntry struct {
	nameOff, header uint64
	cacheType       uint32
	btree, heap     uint64
	slink           uint32
}

// encodeEntry encodes one entry as H5G_ent_encode does.
func encodeEntry(e testEntry, a, s int) []byte {
	b := make([]byte, s+a+24)
	putLE(b, e.nameOff, s)
	putLE(b[s:], e.header, a)
	binary.LittleEndian.PutUint32(b[s+a:], e.cacheType)
	switch e.cacheType {
	case CacheTypeSymbolTable:
		putLE(b[s+a+8:], e.btree, a)
		putLE(b[s+a+8+a:], e.heap, a)
	case CacheTypeSoftLink:
		binary.LittleEndian.PutUint32(b[s+a+8:], e.slink)
	}
	return b
}

// encodeSNOD encodes a symbol table node with the given entries.
func encodeSNOD(entries []testEntry, a, s int) []byte {
	b := make([]byte, 8, 8+len(entries)*(s+a+24))
	copy(b, "SNOD\x01")
	binary.LittleEndian.PutUint16(b[6:], uint16(len(entries)))
	for _, e := range entries {
		b = append(b, encodeEntry(e, a, s)...)
	}
	return b
}

func testSB(a, s uint8) *core.Superblock {
	return &core.Superblock{Version: 0, OffsetSize: a, LengthSize: s, Endianness: binary.LittleEndian}
}

var sizePairs = []struct{ a, s int }{{8, 4}, {4, 8}, {8, 8}, {4, 4}, {2, 8}}

func sampleEntries() []testEntry {
	return []testEntry{
		{nameOff: 8, header: 0x1234, cacheType: CacheTypeNone},
		{nameOff: 16, header: 0x2345, cacheType: CacheTypeSymbolTable, btree: 0x3456, heap: 0x4567},
		{nameOff: 24, header: 0x5678, cacheType: CacheTypeSoftLink, slink: 40},
	}
}

func requireEntries(t *testing.T, want []testEntry, got []SymbolTableEntry, a, s int) {
	t.Helper()
	require.Len(t, got, len(want), "addr=%d size=%d", a, s)
	for i, w := range want {
		g := got[i]
		require.Equal(t, w.nameOff, g.LinkNameOffset, "entry %d addr=%d size=%d", i, a, s)
		require.Equal(t, w.header, g.ObjectAddress, "entry %d addr=%d size=%d", i, a, s)
		require.Equal(t, w.cacheType, g.CacheType, "entry %d", i)
		require.Equal(t, w.btree, g.CachedBTreeAddr, "entry %d", i)
		require.Equal(t, w.heap, g.CachedHeapAddr, "entry %d", i)
		require.Equal(t, w.slink, g.CachedSoftLinkOffset, "entry %d", i)
	}
}

func TestParseSymbolTableNode_LengthSizedNameOffset(t *testing.T) {
	for _, p := range sizePairs {
		want := sampleEntries()
		img := encodeSNOD(want, p.a, p.s)
		node, err := ParseSymbolTableNode(bytes.NewReader(img), 0, testSB(uint8(p.a), uint8(p.s)))
		require.NoError(t, err, "addr=%d size=%d", p.a, p.s)
		requireEntries(t, want, node.Entries, p.a, p.s)
	}
}

// TestParseSymbolTableNode_UnknownCacheType: C fails on a cache type it does
// not know (H5Gent.c:162-164).
func TestParseSymbolTableNode_UnknownCacheType(t *testing.T) {
	img := encodeSNOD([]testEntry{{nameOff: 8, header: 0x100, cacheType: 7}}, 8, 8)
	_, err := ParseSymbolTableNode(bytes.NewReader(img), 0, testSB(8, 8))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// encodeGroupLeaf encodes a leaf node of a group B-tree (H5B header, then
// key, child, key, ..., key) whose single child is the SNOD at snodAddr.
func encodeGroupLeaf(snodAddr uint64, a, s int) []byte {
	b := []byte{'T', 'R', 'E', 'E', 0, 0, 1, 0}
	sib := make([]byte, 2*a)
	putLE(sib, ^uint64(0), a)
	putLE(sib[a:], ^uint64(0), a)
	b = append(b, sib...)
	keyChildKey := make([]byte, s+a+s)
	putLE(keyChildKey, 0, s)
	putLE(keyChildKey[s:], snodAddr, a)
	putLE(keyChildKey[s+a:], 24, s)
	return append(b, keyChildKey...)
}

func TestReadGroupBTreeEntries_LengthSizedKeys(t *testing.T) {
	for _, p := range sizePairs {
		want := sampleEntries()
		const snodAddr = 0x100
		file := make([]byte, 0x400)
		copy(file, encodeGroupLeaf(snodAddr, p.a, p.s))
		copy(file[snodAddr:], encodeSNOD(want, p.a, p.s))

		got, err := ReadGroupBTreeEntries(bytes.NewReader(file), 0, testSB(uint8(p.a), uint8(p.s)))
		require.NoError(t, err, "addr=%d size=%d", p.a, p.s)
		require.Len(t, got, len(want), "addr=%d size=%d", p.a, p.s)
		for i, w := range want {
			require.Equal(t, w.nameOff, got[i].LinkNameOffset, "entry %d addr=%d size=%d", i, p.a, p.s)
			require.Equal(t, w.header, got[i].ObjectAddress, "entry %d addr=%d size=%d", i, p.a, p.s)
		}
	}
}

// TestReadGroupBTreeEntries_BadChildIsError: a child that is not a symbol
// table node fails the iteration in C (H5B__iterate_helper stops on the
// failed node load); it is never skipped.
func TestReadGroupBTreeEntries_BadChildIsError(t *testing.T) {
	const snodAddr = 0x100
	file := make([]byte, 0x400)
	copy(file, encodeGroupLeaf(snodAddr, 8, 8))
	copy(file[snodAddr:], "XXXX")

	_, err := ReadGroupBTreeEntries(bytes.NewReader(file), 0, testSB(8, 8))
	require.ErrorIs(t, err, utils.ErrCorrupt)
}
