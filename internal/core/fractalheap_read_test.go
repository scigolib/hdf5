package core

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// The tests below lay out fractal heaps byte by byte as the C library writes
// them (H5HFcache.c: header, indirect and direct block serializers), with
// 8-byte addresses and lengths, the heap header at address 0.

// fheapTestSB is the superblock every test heap is read with.
var fheapTestSB = &Superblock{OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}

const (
	fheapTestHeaderSize = 146 // H5HF_HEADER_SIZE with 8-byte fields
	fheapTestOffSize    = 3   // heap offsets of an 18-bit heap
	fheapTestLenSize    = 2   // lengths of 512-byte objects in 1 KiB blocks
	fheapTestOverhead   = 16  // direct block header: 5 + 8 + 3
)

// fheapSpec holds the header fields the tests vary.
type fheapSpec struct {
	idLen     uint16
	flags     byte
	maxMan    uint32
	hugeBT2   uint64
	fsAddr    uint64
	manSize   uint64
	width     uint16
	start     uint64
	maxDirect uint64
	maxIndex  uint16
	root      uint64
	currRows  uint16
}

// defaultFheapSpec is a heap as C creates one for dense storage, scaled down:
// 4 columns of 512-byte blocks, direct blocks up to 1 KiB (rows 0-2), an
// 18-bit address space (3-byte offsets, 8 root rows).
func defaultFheapSpec() fheapSpec {
	return fheapSpec{
		idLen: 8, maxMan: 512, hugeBT2: undefinedAddress, fsAddr: undefinedAddress, manSize: 1 << 17,
		width: 4, start: 512, maxDirect: 1024, maxIndex: 18, root: undefinedAddress,
	}
}

// bytes encodes the header (H5HF__cache_hdr_serialize) with its checksum.
func (s fheapSpec) bytes() []byte {
	le := binary.LittleEndian
	b := make([]byte, fheapTestHeaderSize)
	copy(b, fheapHeaderSig)
	le.PutUint16(b[5:], s.idLen)
	b[9] = s.flags
	le.PutUint32(b[10:], s.maxMan)
	le.PutUint64(b[22:], s.hugeBT2)
	le.PutUint64(b[38:], s.fsAddr)
	le.PutUint64(b[46:], s.manSize)
	le.PutUint16(b[110:], s.width)
	le.PutUint64(b[112:], s.start)
	le.PutUint64(b[120:], s.maxDirect)
	le.PutUint16(b[128:], s.maxIndex)
	le.PutUint64(b[132:], s.root)
	le.PutUint16(b[140:], s.currRows)
	resealFheap(b)

	return b
}

// resealFheap recomputes the checksum in the last four bytes of a header or
// an indirect block.
func resealFheap(b []byte) {
	n := len(b) - fheapChecksumSize
	binary.LittleEndian.PutUint32(b[n:], JenkinsChecksum(b[:n]))
}

func putFheapUint(b []byte, v uint64) {
	for i := range b {
		b[i] = byte(v >> (8 * i))
	}
}

// fheapDirectBlock returns a size-byte direct block of the heap at address 0
// that sits at heap offset blockOff, with objs copied in at their offsets in
// the block. A checksummed block is sealed with sealFheapDirectBlock.
func fheapDirectBlock(blockOff uint64, size int, objs map[int][]byte) []byte {
	b := make([]byte, size)
	copy(b, fheapDBlockSig)
	putFheapUint(b[13:13+fheapTestOffSize], blockOff)
	for at, o := range objs {
		copy(b[at:], o)
	}

	return b
}

// sealFheapDirectBlock stores the checksum of a direct block, computed over
// the whole block with the checksum field zeroed (H5HFcache.c:1594-1608).
func sealFheapDirectBlock(b []byte) {
	at := 13 + fheapTestOffSize
	clear(b[at : at+fheapChecksumSize])
	binary.LittleEndian.PutUint32(b[at:], JenkinsChecksum(b))
}

// fheapIndirectBlock returns an indirect block of the heap at address 0 that
// sits at heap offset blockOff, with child addresses ents (row by row).
func fheapIndirectBlock(blockOff uint64, ents []uint64) []byte {
	b := make([]byte, 13+fheapTestOffSize, 13+fheapTestOffSize+8*len(ents)+fheapChecksumSize)
	copy(b, fheapIBlockSig)
	putFheapUint(b[13:], blockOff)
	for _, e := range ents {
		b = binary.LittleEndian.AppendUint64(b, e)
	}
	b = append(b, 0, 0, 0, 0)
	resealFheap(b)

	return b
}

// undefinedEnts returns n child entries, all unallocated.
func undefinedEnts(n int) []uint64 {
	ents := make([]uint64, n)
	for i := range ents {
		ents[i] = undefinedAddress
	}

	return ents
}

// managedFheapID returns an 8-byte managed object heap ID (H5HF_MAN_ID_ENCODE).
func managedFheapID(off, length uint64) []byte {
	id := make([]byte, 8)
	putFheapUint(id[1:1+fheapTestOffSize], off)
	putFheapUint(id[1+fheapTestOffSize:1+fheapTestOffSize+fheapTestLenSize], length)

	return id
}

// fheapRootDirectAddr is where rootDirectFheap puts its only block.
const fheapRootDirectAddr = 256

// rootDirectFheap lays out a heap whose root is one direct block of s.start
// bytes holding objs, with 512 bytes of managed space; mutate edits the image
// before it is returned.
func rootDirectFheap(s fheapSpec, objs map[int][]byte, mutate func(img []byte)) []byte {
	s.root, s.manSize = fheapRootDirectAddr, s.start
	img := make([]byte, fheapRootDirectAddr+int(s.start))
	copy(img, s.bytes())
	db := fheapDirectBlock(0, int(s.start), objs)
	if s.flags&fheapFlagChecksumDBs != 0 {
		sealFheapDirectBlock(db)
	}
	copy(img[fheapRootDirectAddr:], db)
	if mutate != nil {
		mutate(img)
	}

	return img
}

func openTestFheap(t *testing.T, img []byte) *FractalHeap {
	t.Helper()
	h, err := OpenFractalHeap(bytes.NewReader(img), fheapTestSB, 0)
	require.NoError(t, err)

	return h
}

// TestFractalHeap_RootDirectBlock reads objects of a heap whose root is a
// direct block, with and without direct block checksums. C counts an object's
// offset from the first byte of its block (H5HFman.c:388-393), so the first
// object follows the block header. Replaces the readHeapObject tests
// (TestReadHeapObject_Valid, _WithOffset, TestReadHeapObjectValidObjectUnchanged).
func TestFractalHeap_RootDirectBlock(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    byte
		overhead int
	}{
		{"no checksum", 0, fheapTestOverhead},
		{"checksummed", fheapFlagChecksumDBs, fheapTestOverhead + fheapChecksumSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultFheapSpec()
			s.flags = tc.flags
			first, second := tc.overhead, tc.overhead+15+5
			img := rootDirectFheap(s, map[int][]byte{first: []byte("dense-attribute"), second: []byte("data")}, nil)

			h := openTestFheap(t, img)
			got, err := h.Object(managedFheapID(uint64(first), 15))
			require.NoError(t, err)
			require.Equal(t, "dense-attribute", string(got))
			got, err = h.Object(managedFheapID(uint64(second), 4))
			require.NoError(t, err)
			require.Equal(t, "data", string(got))

			got, err = ReadFractalHeapObject(bytes.NewReader(img), fheapTestSB, 0, managedFheapID(uint64(first), 5))
			require.NoError(t, err)
			require.Equal(t, "dense", string(got))
		})
	}
}

// TestFractalHeap_HeaderRejected: a header that is not one, or whose
// parameters would size a table or a read wrongly, is utils.ErrCorrupt; a
// heap with I/O filters is utils.ErrNotSupported. Replaces
// TestReadFractalHeapHeaderRaw_InvalidSignature, _TooShort and
// TestReadFractalHeapHeaderRawRejectsInvalidMaxHeapSize (bits 0, 65, 65535),
// which only required an error.
func TestFractalHeap_HeaderRejected(t *testing.T) {
	spec := func(edit func(s *fheapSpec)) []byte {
		s := defaultFheapSpec()
		edit(&s)

		return s.bytes()
	}
	raw := func(edit func(b []byte), reseal bool) []byte {
		b := defaultFheapSpec().bytes()
		edit(b)
		if reseal {
			resealFheap(b)
		}

		return b
	}
	for _, tc := range []struct {
		name, want string
		img        []byte
	}{
		{"signature", "header signature", raw(func(b []byte) { b[0] = 'X' }, true)},
		{"version", "header version", raw(func(b []byte) { b[4] = 1 }, true)},
		{"checksum", "header at 0x0 has checksum", raw(func(b []byte) { b[10]++ }, false)},
		{"heap ID length 0", "heap ID length is zero", spec(func(s *fheapSpec) { s.idLen = 0 })},
		{"width 0", "table width 0", spec(func(s *fheapSpec) { s.width = 0 })},
		{"width 3", "table width 3", spec(func(s *fheapSpec) { s.width = 3 })},
		{"starting block 0", "starting block size 0", spec(func(s *fheapSpec) { s.start = 0 })},
		{"starting block 768", "starting block size 768", spec(func(s *fheapSpec) { s.start = 768 })},
		{"direct block 0", "direct block size 0 is not", spec(func(s *fheapSpec) { s.maxDirect = 0 })},
		{"direct block 1536", "direct block size 1536 is not", spec(func(s *fheapSpec) { s.maxDirect = 1536 })},
		{"direct block 4 GiB", "direct block size 4294967296 is not",
			spec(func(s *fheapSpec) { s.maxDirect = 4 << 30 })},
		{"direct block below start", "below the starting block size",
			spec(func(s *fheapSpec) { s.start, s.maxDirect = 2048, 1024 })},
		{"direct block below managed max", "below the maximum managed object size",
			spec(func(s *fheapSpec) { s.maxMan = 2048 })},
		{"max heap size 0", "max heap size is zero", spec(func(s *fheapSpec) { s.maxIndex = 0 })},
		{"max heap size 65", "max heap size 65 bits", spec(func(s *fheapSpec) { s.maxIndex = 65 })},
		{"max heap size 65535", "max heap size 65535 bits", spec(func(s *fheapSpec) { s.maxIndex = 65535 })},
		{"max heap size below the first row", "below the first row's 11 bits",
			spec(func(s *fheapSpec) { s.maxIndex = 9 })},
		{"root rows above the maximum", "root indirect block has 9 rows, the heap at most 8",
			spec(func(s *fheapSpec) { s.currRows = 9 })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenFractalHeap(bytes.NewReader(tc.img), fheapTestSB, 0)
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("truncated", func(t *testing.T) {
		_, err := OpenFractalHeap(bytes.NewReader(defaultFheapSpec().bytes()[:100]), fheapTestSB, 0)
		require.Error(t, err)
	})

	t.Run("filters", func(t *testing.T) {
		// Root direct block size (8), filter mask (4) and a 4-byte pipeline
		// precede the checksum (H5HFcache.c:509-532).
		b := defaultFheapSpec().bytes()
		b = append(b[:fheapTestHeaderSize-fheapChecksumSize], make([]byte, 8+4+4+fheapChecksumSize)...)
		binary.LittleEndian.PutUint16(b[7:], 4)
		resealFheap(b)
		_, err := OpenFractalHeap(bytes.NewReader(b), fheapTestSB, 0)
		require.ErrorIs(t, err, utils.ErrNotSupported)
	})
}

// TestFractalHeap_FieldSizes pins the widths of a managed heap ID's fields:
// the offset takes ceil(max_index/8) bytes (H5HF_SIZEOF_OFFSET_BITS,
// H5HFhdr.c:194) and the length the smaller of the bytes of a direct block
// offset and of the managed object size (H5HFhdr.c:196-198). Replaces
// TestReadFractalHeapHeaderRawOffsetSize and the computeOffsetSize tests
// (TestComputeOffsetSize, _Coverage, _ExtendedCases): computeOffsetSize took
// the byte width of a value, which C does not use for the length; 65536-byte
// blocks with 65536-byte objects have 2-byte lengths in C, 3 by it.
func TestFractalHeap_FieldSizes(t *testing.T) {
	for _, tc := range []struct {
		bits uint16
		want int
	}{{1, 1}, {8, 1}, {9, 2}, {16, 2}, {24, 3}, {25, 4}, {64, 8}} {
		s := defaultFheapSpec()
		// One 1-byte block a row: every maximum heap size has a first row.
		s.width, s.start, s.maxDirect, s.maxMan, s.maxIndex = 1, 1, 1, 1, tc.bits
		h := openTestFheap(t, s.bytes())
		require.Equal(t, tc.want, h.offSize, "max heap size %d bits", tc.bits)
	}

	for _, tc := range []struct {
		maxDirect, start uint64
		maxMan           uint32
		want             int
	}{
		{65536, 512, 65536, 2},
		{512, 512, 512, 2},
		{256, 256, 255, 1},
		{1 << 20, 512, 65535, 2},
	} {
		s := defaultFheapSpec()
		s.maxDirect, s.start, s.maxMan = tc.maxDirect, tc.start, tc.maxMan
		h := openTestFheap(t, s.bytes())
		require.Equal(t, tc.want, h.lenSize, "blocks %d, objects %d", tc.maxDirect, tc.maxMan)
	}
}

// TestFractalHeap_IDRejected covers the heap ID's flags byte (H5HF.c:505-536)
// and a managed ID too short for its fields. Replaces TestParseHeapID,
// _EdgeCases, TestParseHeapID_UnsupportedType and _Managed: the flags byte's
// low bits, which those ignored, are the length of a tiny object, and a
// version other than 0 is an error (H5HF.c:511-512).
func TestFractalHeap_IDRejected(t *testing.T) {
	h := openTestFheap(t, defaultFheapSpec().bytes())
	for name, id := range map[string][]byte{
		"empty":         nil,
		"version 1":     {0x40, 1, 0, 0, 1, 0, 0, 0},
		"reserved type": {0x30, 1, 0, 0, 1, 0, 0, 0},
		"short managed": {0x00, 1, 0, 0, 1},
	} {
		_, err := h.Object(id)
		require.ErrorIs(t, err, utils.ErrCorrupt, name)
	}
}

// TestFractalHeap_ManagedRejected: C's checks of a managed object's offset
// and length (H5HFman.c:321-332, :388-397) and the checks of the direct block
// that holds it. Replaces TestReadHeapObjectRejectsLengthBeyondMaxManagedObject,
// _BeyondBlock, _BeyondReader, TestReadHeapObject_InvalidSignature and
// _OffsetBeforeBlock.
func TestFractalHeap_ManagedRejected(t *testing.T) {
	objs := map[int][]byte{fheapTestOverhead: []byte("object")}
	putAt := func(at int, b []byte) func([]byte) { return func(img []byte) { copy(img[at:], b) } }
	db := fheapRootDirectAddr
	for _, tc := range []struct {
		name, want  string
		flags       byte
		mutate      func(img []byte)
		off, length uint64
	}{
		{name: "offset 0", want: "invalid fractal heap offset 0", off: 0, length: 6},
		{name: "offset beyond the managed space", want: "beyond the managed space of 512", off: 513, length: 6},
		{name: "length 0", want: "object size 0", off: fheapTestOverhead, length: 0},
		{name: "longer than a direct block", want: "too large for a direct block of 1024",
			off: fheapTestOverhead, length: 1025},
		{name: "longer than the managed maximum", want: "exceeds the managed maximum 512",
			off: fheapTestOverhead, length: 600},
		{name: "in the block header", want: "in the prefix of its direct block", off: 8, length: 6},
		{name: "overruns the block", want: "overruns its 512-byte direct block", off: 500, length: 20},
		{name: "beyond the root block", want: "beyond the root direct block", off: 512, length: 6},
		{name: "block outside the file", want: "beyond the file's", mutate: func(img []byte) {
			binary.LittleEndian.PutUint64(img[132:], 1<<20)
			resealFheap(img[:fheapTestHeaderSize])
		}, off: fheapTestOverhead, length: 6},
		{name: "block signature", want: "direct block signature \"FHIB\"",
			mutate: putAt(db, []byte("FHIB")), off: fheapTestOverhead, length: 6},
		{name: "block version", want: "direct block version 1",
			mutate: putAt(db+4, []byte{1}), off: fheapTestOverhead, length: 6},
		{name: "block of another heap", want: "belongs to the heap at 0x40",
			mutate: putAt(db+5, []byte{0x40}), off: fheapTestOverhead, length: 6},
		{name: "block at another offset", want: "has heap offset 2, its parent places it at 0",
			mutate: putAt(db+13, []byte{2}), off: fheapTestOverhead, length: 6},
		{name: "block checksum", want: "direct block at 0x100 has checksum", flags: fheapFlagChecksumDBs,
			mutate: putAt(db+100, []byte{1}), off: fheapTestOverhead + fheapChecksumSize, length: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultFheapSpec()
			s.flags = tc.flags
			h := openTestFheap(t, rootDirectFheap(s, objs, tc.mutate))
			_, err := h.Object(managedFheapID(tc.off, tc.length))
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestFractalHeap_LegacyOffsets reads a heap this library wrote up to
// v0.14.2: object offsets count from the first byte after the direct block
// header, the first object at offset 0, and the huge object B-tree and free
// space manager addresses are 0. The same image with C's undefined addresses
// is read with C's offsets.
//
// Those heaps also have a 16-bit address space smaller than their first row
// (32 KiB blocks in 4 columns span 17 bits): no root indirect block can have
// a row, so the root direct block is the heap's only block. Such a header
// opens; here 512-byte blocks (11 bits) in a 10-bit heap.
func TestFractalHeap_LegacyOffsets(t *testing.T) {
	s := defaultFheapSpec()
	s.maxIndex = 10
	require.Empty(t, openTestFheap(t, s.bytes()).rowBlockSize)

	s = defaultFheapSpec()
	s.hugeBT2, s.fsAddr = 0, 0
	img := rootDirectFheap(s, map[int][]byte{fheapTestOverhead: []byte("legacy"), fheapTestOverhead + 6: []byte("heap")}, nil)

	h := openTestFheap(t, img)
	got, err := h.Object(managedFheapID(0, 6))
	require.NoError(t, err)
	require.Equal(t, "legacy", string(got))
	got, err = h.Object(managedFheapID(6, 4))
	require.NoError(t, err)
	require.Equal(t, "heap", string(got))

	s.hugeBT2, s.fsAddr = undefinedAddress, undefinedAddress
	h = openTestFheap(t, rootDirectFheap(s, map[int][]byte{fheapTestOverhead: []byte("legacy")}, nil))
	_, err = h.Object(managedFheapID(0, 6))
	require.ErrorIs(t, err, utils.ErrCorrupt, "C heaps have no object at offset 0")
	got, err = h.Object(managedFheapID(fheapTestOverhead, 6))
	require.NoError(t, err)
	require.Equal(t, "legacy", string(got))
}

// Where nestedFheap lays out its blocks.
const (
	nestedRootAddr       = 256
	nestedChildAddr      = 512
	nestedGrandchildAddr = 768
	nestedDBlockAddr     = 1024
	nestedChildOff       = 65536 // row 6, column 0 of the root: 2048 << 5
	nestedGrandchildOff  = 73728 // row 3, column 0 of the child: 65536 + 8192
	nestedDBlockOff      = 74752 // row 0, column 2 of the grandchild: 73728 + 2 * 512
	nestedObjectOff      = nestedDBlockOff + fheapTestOverhead
)

// nestedFheap lays out a heap of three levels of indirect blocks: a root of
// 7 rows whose row 6 (16 KiB blocks, so indirect: direct blocks stop at row 2)
// holds a child of 4 rows, whose row 3 (2 KiB) holds a grandchild of 1 row
// with a direct block in column 2, and the object "deep!" first in that block.
// edit may change the child entries before the blocks are encoded.
func nestedFheap(edit func(root, child, grandchild []uint64)) []byte {
	s := defaultFheapSpec()
	s.root, s.currRows = nestedRootAddr, 7
	root, child, grandchild := undefinedEnts(7*4), undefinedEnts(4*4), undefinedEnts(4)
	root[6*4], child[3*4], grandchild[2] = nestedChildAddr, nestedGrandchildAddr, nestedDBlockAddr
	if edit != nil {
		edit(root, child, grandchild)
	}

	img := make([]byte, nestedDBlockAddr+512)
	copy(img, s.bytes())
	copy(img[nestedRootAddr:], fheapIndirectBlock(0, root))
	copy(img[nestedChildAddr:], fheapIndirectBlock(nestedChildOff, child))
	copy(img[nestedGrandchildAddr:], fheapIndirectBlock(nestedGrandchildOff, grandchild))
	copy(img[nestedDBlockAddr:], fheapDirectBlock(nestedDBlockOff, 512, map[int][]byte{fheapTestOverhead: []byte("deep!")}))

	return img
}

// TestFractalHeap_NestedIndirectBlocks finds an object through three levels
// of indirect blocks as H5HF__man_dblock_locate does (H5HFdblock.c:495-583),
// and rejects heaps whose blocks are missing, corrupt, misplaced or cyclic.
func TestFractalHeap_NestedIndirectBlocks(t *testing.T) {
	h := openTestFheap(t, nestedFheap(nil))
	got, err := h.Object(managedFheapID(nestedObjectOff, 5))
	require.NoError(t, err)
	require.Equal(t, "deep!", string(got))
	got, err = h.Object(managedFheapID(nestedObjectOff+1, 4))
	require.NoError(t, err, "a second read reuses the cached blocks")
	require.Equal(t, "eep!", string(got))

	flip := func(at int) func([]byte) { return func(img []byte) { img[at] ^= 0xFF } }
	for _, tc := range []struct {
		name, want string
		edit       func(root, child, grandchild []uint64)
		mutate     func(img []byte)
		off        uint64
	}{
		{name: "unallocated direct block", want: "not in allocated direct block", off: nestedObjectOff - 512},
		{name: "unallocated root direct block", want: "not in allocated direct block", off: fheapTestOverhead},
		{name: "unallocated indirect block", want: "not in allocated indirect block",
			off: nestedChildOff + 16384 + fheapTestOverhead},
		{name: "row beyond the root", want: "row 7 is beyond an indirect block of 7 rows", mutate: func(img []byte) {
			s := defaultFheapSpec() // room for an offset in row 7
			s.root, s.currRows, s.manSize = nestedRootAddr, 7, 1<<18
			copy(img, s.bytes())
		}, off: 1<<17 + fheapTestOverhead},
		{name: "child checksum", want: "indirect block at 0x200 has checksum",
			mutate: flip(nestedChildAddr + 20), off: nestedObjectOff},
		{name: "grandchild signature", want: "indirect block signature \"XHIB\"", mutate: func(img []byte) {
			img[nestedGrandchildAddr] = 'X'
			resealFheap(img[nestedGrandchildAddr : nestedGrandchildAddr+52])
		}, off: nestedObjectOff},
		{name: "child at another heap offset", want: "has heap offset 81920, its parent places it at 65536",
			mutate: func(img []byte) {
				copy(img[nestedChildAddr:], fheapIndirectBlock(nestedChildOff+16384, undefinedEnts(16)))
			}, off: nestedObjectOff},
		{name: "child pointing back at the root", want: "at 0x100 is reached as two blocks",
			edit: func(_, child, _ []uint64) { child[3*4] = nestedRootAddr }, off: nestedObjectOff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := nestedFheap(tc.edit)
			if tc.mutate != nil {
				tc.mutate(img)
			}
			h := openTestFheap(t, img)
			_, err := h.Object(managedFheapID(tc.off, 5))
			require.ErrorIs(t, err, utils.ErrCorrupt)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestFractalHeap_RowTooSmallForIndirectBlock: with 512-byte direct blocks in
// 4 columns, row 2 holds 1 KiB blocks, less than the 2 KiB of a first row, so
// it cannot hold indirect blocks. C computes the child's row count in unsigned
// arithmetic, which wraps (H5HFdblock.c:534); here it is utils.ErrCorrupt.
func TestFractalHeap_RowTooSmallForIndirectBlock(t *testing.T) {
	s := defaultFheapSpec()
	s.maxDirect, s.root, s.currRows = 512, 256, 3
	ents := undefinedEnts(3 * 4)
	ents[2*4] = 1024
	img := make([]byte, 1024+512)
	copy(img, s.bytes())
	copy(img[256:], fheapIndirectBlock(0, ents))

	h := openTestFheap(t, img)
	_, err := h.Object(managedFheapID(4096+fheapTestOverhead, 5))
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "too small for an indirect block")
}

// TestFractalHeap_DirectBlockReachedTwice: two entries of an indirect block
// naming one direct block place it at two heap offsets; the second is
// utils.ErrCorrupt.
func TestFractalHeap_DirectBlockReachedTwice(t *testing.T) {
	s := defaultFheapSpec()
	s.maxDirect, s.root, s.currRows = 512, 256, 1
	ents := undefinedEnts(4)
	ents[0], ents[1] = 512, 512
	img := make([]byte, 1024)
	copy(img, s.bytes())
	copy(img[256:], fheapIndirectBlock(0, ents))
	copy(img[512:], fheapDirectBlock(0, 512, map[int][]byte{fheapTestOverhead: []byte("once")}))

	h := openTestFheap(t, img)
	got, err := h.Object(managedFheapID(fheapTestOverhead, 4))
	require.NoError(t, err)
	require.Equal(t, "once", string(got))
	_, err = h.Object(managedFheapID(512+fheapTestOverhead, 4))
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "reached as two blocks")
}

// TestFractalHeap_BlockBudget: four checksummed 512-byte direct blocks that
// overlap, 32 bytes apart, each valid (sealed last to first, so each checksum
// covers the sealed blocks inside it), claim more bytes than the 864-byte
// file. The blocks a heap reads may not, so the second block is refused.
func TestFractalHeap_BlockBudget(t *testing.T) {
	const blocks, spacing, first = 4, 32, 256
	overhead := fheapTestOverhead + fheapChecksumSize
	s := defaultFheapSpec()
	s.flags, s.maxDirect, s.root, s.currRows = fheapFlagChecksumDBs, 512, 160, 1
	ents := make([]uint64, blocks)
	img := make([]byte, first+(blocks-1)*spacing+512)
	copy(img, s.bytes())
	for i := range blocks {
		at := first + i*spacing
		ents[i] = uint64(at)
		hdr := fheapDirectBlock(uint64(i)*512, overhead, map[int][]byte{})
		copy(img[at:], hdr)
		copy(img[at+overhead:], "blk")
	}
	copy(img[160:], fheapIndirectBlock(0, ents))
	for i := blocks - 1; i >= 0; i-- {
		at := first + i*spacing
		sealFheapDirectBlock(img[at : at+512])
	}

	h := openTestFheap(t, img)
	got, err := h.Object(managedFheapID(uint64(overhead), 3))
	require.NoError(t, err)
	require.Equal(t, "blk", string(got))
	_, err = h.Object(managedFheapID(512+uint64(overhead), 3))
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "more than the file")
}

// TestFractalHeap_Tiny reads objects stored in their heap IDs
// (H5HF__tiny_op_real, H5HFtiny.c:185-245): up to 16 bytes the low nibble of
// the flags byte is the length - 1; longer IDs carry 12 bits of length in the
// nibble and the next byte (H5HFtiny.c:98-110, :149-151).
func TestFractalHeap_Tiny(t *testing.T) {
	heap := func(idLen uint16) *FractalHeap {
		s := defaultFheapSpec()
		s.idLen = idLen

		return openTestFheap(t, s.bytes())
	}

	h := heap(8)
	got, err := h.Object([]byte{0x22, 'a', 'b', 'c', 0, 0, 0, 0})
	require.NoError(t, err)
	require.Equal(t, "abc", string(got))
	_, err = h.Object([]byte{0x27, 1, 2, 3, 4, 5, 6, 7})
	require.ErrorIs(t, err, utils.ErrCorrupt, "8 bytes do not fit an 8-byte ID")
	_, err = h.Object([]byte{0x23, 'a', 'b'})
	require.ErrorIs(t, err, utils.ErrCorrupt, "a 4-byte object in a 3-byte ID")

	h = heap(18) // 17 bytes after the flags: still the short form, 16 at most
	got, err = h.Object(append([]byte{0x2F}, bytes.Repeat([]byte{'s'}, 17)...))
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'s'}, 16), got)

	h = heap(20) // extended: 18 bytes at most
	id := append([]byte{0x20, 17}, bytes.Repeat([]byte{'x'}, 18)...)
	got, err = h.Object(id)
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'x'}, 18), got)
	id[1] = 18
	_, err = h.Object(id)
	require.ErrorIs(t, err, utils.ErrCorrupt)
}

// TestFractalHeap_Huge: a huge object whose heap ID holds its address and
// length (H5HFhuge.c:193-203, unfiltered) is read from the file; one tracked
// by the heap's v2 B-tree is utils.ErrNotSupported.
func TestFractalHeap_Huge(t *testing.T) {
	s := defaultFheapSpec()
	s.idLen = 17
	img := append(s.bytes(), []byte("a huge object")...)
	h := openTestFheap(t, img)

	id := make([]byte, 17)
	id[0] = fheapIDTypeHuge
	binary.LittleEndian.PutUint64(id[1:], fheapTestHeaderSize)
	binary.LittleEndian.PutUint64(id[9:], 13)
	got, err := h.Object(id)
	require.NoError(t, err)
	require.Equal(t, "a huge object", string(got))

	binary.LittleEndian.PutUint64(id[9:], 1<<20)
	_, err = h.Object(id)
	require.Error(t, err, "an object beyond the file")

	h = openTestFheap(t, defaultFheapSpec().bytes()) // 8-byte IDs
	_, err = h.Object([]byte{fheapIDTypeHuge, 1, 0, 0, 0, 0, 0, 0})
	require.ErrorIs(t, err, utils.ErrNotSupported)
}

// FuzzFractalHeapObject opens arbitrary heap images and reads arbitrary IDs:
// the reader must return an error, never panic or allocate beyond the input.
func FuzzFractalHeapObject(f *testing.F) {
	f.Add(nestedFheap(nil), managedFheapID(nestedObjectOff, 5))
	f.Add(rootDirectFheap(defaultFheapSpec(), map[int][]byte{fheapTestOverhead: []byte("x")}, nil),
		managedFheapID(fheapTestOverhead, 1))
	f.Add(defaultFheapSpec().bytes(), []byte{0x22, 'a', 'b', 'c'})

	f.Fuzz(func(t *testing.T, img, id []byte) {
		h, err := OpenFractalHeap(bytes.NewReader(img), fheapTestSB, 0)
		if err != nil {
			return
		}
		data, err := h.Object(id)
		if err != nil {
			return
		}
		if len(data) > max(len(img), len(id)) {
			t.Fatalf("%d-byte object from a %d-byte image and a %d-byte ID", len(data), len(img), len(id))
		}
	})
}
