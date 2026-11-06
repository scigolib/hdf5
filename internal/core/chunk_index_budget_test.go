package core

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/utils"
)

// byteCountingReader counts the bytes read through it. The embedded
// *bytes.Reader keeps Size, so dataLimit sees the image's end.
type byteCountingReader struct {
	*bytes.Reader
	n uint64
}

func (c *byteCountingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.Reader.ReadAt(p, off)
	c.n += uint64(n)

	return n, err
}

// tinyChunkedInt8 is a chunked int8 dataset of one one-byte chunk whose index
// of the given type lives at addr. Its current grid holds one chunk, so the
// entries of a crafted index past index 0 are out of the grid and skipped.
func tinyChunkedInt8(version, index uint8, maxDim, addr uint64) *DatasetDescriptor {
	return &DatasetDescriptor{
		datatype:  &DatatypeMessage{Class: DatatypeFixed, Size: 1},
		dataspace: &DataspaceMessage{Type: DataspaceSimple, Dimensions: []uint64{1}, MaxDims: []uint64{maxDim}},
		layout: &DataLayoutMessage{
			Version: version, Class: LayoutChunked, ChunkIndexType: index,
			ChunkSize: []uint64{1, 1}, DataAddress: addr,
		},
		fill:       &FillValueMessage{},
		nelmts:     1,
		elemSize:   1,
		dataBytes:  1,
		chunkBytes: 1,
	}
}

// Geometry of overlappingEArray: 128 data block pointers in the index block
// give 14 super blocks of data blocks, all addressed from the index block
// (2 × (log2 128) = 14 = nsblks for 64-element minimum blocks and 19 bits),
// 254 data blocks of 64 to 8192 eight-byte records. A page of 2^13 elements
// keeps every block unpaged, as C requires (H5EA__dblock_alloc).
const (
	eaTestSupBlkMin  = 128
	eaTestDataBlkMin = 64
	eaTestMaxBits    = 19
	eaTestPageBits   = 13
	eaTestHeaderSize = arrayPrefixSize + 6 + 6*8 + 8                // 72.
	eaTestDblkPrefix = 4 + 1 + 1 + 8 + (eaTestMaxBits+7)/8          // Signature to block offset: 17.
	eaTestIdxSize    = arrayPrefixSize + 8 + 8 + 2*(128-1)*8        // One record, 254 block addresses.
	eaTestDataBase   = eaTestHeaderSize + eaTestIdxSize             // First byte after the index block.
	eaTestNsblks     = 1 + eaTestMaxBits - 6                        // log2(64) = 6.
	eaTestMaxIdxSet  = uint64(1) << 20                              // Past every record of the blocks.
	eaTestUndefined  = math.MaxUint64                               // An unwritten record or block.
	eaTestRecordSize = 8                                            // An unfiltered record: the address.
	eaTestSealedSpan = 4                                            // The checksum closing each block.
	eaTestStride     = 24                                           // Distance between the data blocks.
	eaTestPadding    = 8                                            // Step when a placement collides.
	eaTestDblkMax    = eaTestDblkPrefix + 8192*eaTestRecordSize + 4 // The largest data block.
)

// overlappingEArray builds an extensible array whose 254 data blocks lie at
// distinct addresses 24 bytes apart, so each overlaps the next ones, every
// block a valid one: its prefix and checksum fall where no other block's do,
// and the blocks are sealed in the order of their checksums, so sealing one
// changes only bytes of blocks sealed later. The blocks claim about 8 MB of a
// file of about 74 KB; the records are undefined or out of the grid.
func overlappingEArray(t *testing.T) []byte {
	t.Helper()

	var sizes []uint64
	for u := range eaTestNsblks {
		n := sblkDblkNelmts(uint64(u), eaTestDataBlkMin)
		for range 1 << (u / 2) {
			sizes = append(sizes, eaTestDblkPrefix+n*eaTestRecordSize+eaTestSealedSpan)
		}
	}
	require.Len(t, sizes, 2*(eaTestSupBlkMin-1))

	// Place each block where its prefix and checksum touch no other block's.
	used := make([]bool, eaTestDataBase+len(sizes)*(eaTestStride+eaTestPadding*8)+eaTestDblkMax)
	free := func(from, n uint64) bool { return !slices.Contains(used[from:from+n], true) }
	mark := func(from, n uint64) {
		for i := from; i < from+n; i++ {
			used[i] = true
		}
	}
	addrs := make([]uint64, len(sizes))
	next := uint64(eaTestDataBase)
	for i, size := range sizes {
		a := next
		for !free(a, eaTestDblkPrefix) || !free(a+size-eaTestSealedSpan, eaTestSealedSpan) {
			a += eaTestPadding
		}
		mark(a, eaTestDblkPrefix)
		mark(a+size-eaTestSealedSpan, eaTestSealedSpan)
		addrs[i] = a
		next = a + eaTestStride
	}

	end := uint64(0)
	for i, a := range addrs {
		end = max(end, a+sizes[i])
	}
	img := bytes.Repeat([]byte{0xFF}, int(end))
	le := binary.LittleEndian

	// Header (H5EA__cache_hdr_deserialize): parameters, statistics, index block.
	h := img[:eaTestHeaderSize]
	copy(h, earrayHeaderSig)
	h[4], h[5], h[6], h[7] = 0, arrayClassChunk, eaTestRecordSize, eaTestMaxBits
	h[8], h[9], h[10], h[11] = 1, eaTestDataBlkMin, eaTestSupBlkMin, eaTestPageBits
	clear(h[12 : 12+6*8])
	le.PutUint64(h[12+4*8:], eaTestMaxIdxSet)
	le.PutUint64(h[12+6*8:], eaTestHeaderSize)
	le.PutUint32(h[eaTestHeaderSize-4:], JenkinsChecksum(h[:eaTestHeaderSize-4]))

	// Index block: prefix, one undefined record, the data block addresses.
	ib := img[eaTestHeaderSize:eaTestDataBase]
	copy(ib, earrayIndexBlockSig)
	ib[4], ib[5] = 0, arrayClassChunk
	le.PutUint64(ib[6:], 0) // Owning header.
	for i, a := range addrs {
		le.PutUint64(ib[6+8+eaTestRecordSize+8*i:], a)
	}
	le.PutUint32(ib[eaTestIdxSize-4:], JenkinsChecksum(ib[:eaTestIdxSize-4]))

	// Data block prefixes, then the checksums in increasing position.
	for _, a := range addrs {
		copy(img[a:], earrayDataBlockSig)
		img[a+4], img[a+5] = 0, arrayClassChunk
		clear(img[a+6 : a+eaTestDblkPrefix]) // Owning header 0, block offset 0.
	}
	order := make([]int, len(addrs))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(x, y int) int {
		return int(addrs[x]+sizes[x]) - int(addrs[y]+sizes[y])
	})
	for _, i := range order {
		cs := addrs[i] + sizes[i] - eaTestSealedSpan
		le.PutUint32(img[cs:], JenkinsChecksum(img[addrs[i]:cs]))
	}
	for i, a := range addrs { // Every block is valid as built.
		_, err := readChecksummedBlock(bytes.NewReader(img), &Superblock{OffsetSize: 8}, a, sizes[i], "test data block")
		require.NoError(t, err, "data block %d", i)
	}

	return img
}

// TestEArray_OverlappingBlocksBudget (review F1): the walk refused a block
// reached twice but not distinct blocks that overlap, so 254 data blocks 24
// bytes apart read about 8 MB from a 74 KB file; the overlap factor grows
// with the block size, up to the 128 MB a block may claim. C has no overlap
// check either (its cache keys blocks by address, H5C__verify_len_eoa,
// H5Centry.c:949-988); the walk now reads at most the file's size in blocks,
// and more is utils.ErrCorrupt.
//
// Red on edb53b1: no error, 8,395,560 bytes read (67,616 with the budget).
func TestEArray_OverlappingBlocksBudget(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	img := overlappingEArray(t)
	d := tinyChunkedInt8(4, ChunkIndexEArray, eaTestUndefined, 0)

	r := &byteCountingReader{Reader: bytes.NewReader(img)}
	_, err := d.CollectChunks(r, sb, 1<<20)
	t.Logf("file %d bytes, read %d bytes", len(img), r.n)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "more than the file")
	require.LessOrEqual(t, r.n, uint64(len(img))+eaTestHeaderSize, "the walk read more than the file")
}

// overlappingBTreeV1 builds a level-2 chunk B-tree root at address 0 whose n
// children are level-1 nodes with no entries at distinct addresses 8 bytes
// apart: a run of identical "TREE" words, each the first word of one node and
// a sibling address of the two before it. Each child is read in full (24
// bytes) from 8 bytes of the file.
func overlappingBTreeV1(n int) []byte {
	node := []byte("TREE")
	node = append(node, btreeTypeChunk, 2)
	node = binary.LittleEndian.AppendUint16(node, uint16(n))
	node = binary.LittleEndian.AppendUint64(node, math.MaxUint64)
	node = binary.LittleEndian.AppendUint64(node, math.MaxUint64)

	key := make([]byte, 4+4+2*8) // nbytes, filter mask, two coordinates: all 0.
	run := uint64(len(node) + n*(len(key)+8) + len(key))
	for i := range n {
		node = append(node, key...)
		node = binary.LittleEndian.AppendUint64(node, run+8*uint64(i))
	}
	node = append(node, key...)

	word := []byte("TREE")
	word = append(word, btreeTypeChunk, 1, 0, 0) // Level 1, no entries.
	for range n + 2 {
		node = append(node, word...)
	}

	return node
}

// TestBTreeV1_OverlappingNodesBudget (review F1): the version 1 walk refused
// a node reached twice but read distinct nodes that overlap as often as they
// were listed, so a root listing 4096 internal nodes 8 bytes apart read 229
// KB from a 164 KB file (up to about 38 times the file for rank-32 keys).
// Every parsed node, internal ones included, is now charged against the
// file's size, and more is utils.ErrCorrupt.
//
// Red on edb53b1: no error, 229,424 bytes read (163,928 with the budget).
func TestBTreeV1_OverlappingNodesBudget(t *testing.T) {
	sb := &Superblock{Version: 0, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	const children = 4096
	img := overlappingBTreeV1(children)
	d := tinyChunkedInt8(3, ChunkIndexBTREE, 1, 0)

	r := &byteCountingReader{Reader: bytes.NewReader(img)}
	_, err := d.CollectChunks(r, sb, 1<<20)
	t.Logf("file %d bytes, read %d bytes", len(img), r.n)
	require.ErrorIs(t, err, utils.ErrCorrupt)
	require.Contains(t, err.Error(), "more than the file")
	// A node's 24-byte header is read before its size is known and charged.
	require.LessOrEqual(t, r.n, uint64(len(img))+24, "the walk read more than the file")

	// The budget bounds the bytes, it is no overlap check: two overlapping
	// children whose reads fit in the file are read.
	small := append(overlappingBTreeV1(2), make([]byte, 64)...)
	_, err = d.CollectChunks(bytes.NewReader(small), sb, 1<<20)
	require.NoError(t, err)
}

// TestCollectChunks_UnallocatedHugeGrid (review F2): listing the chunks of a
// fixed array index looked up every chunk of the grid one at a time, so a
// dataset of 2^40 one-byte chunks with no chunk written ran for hours. C
// lists what the index stores (H5D__chunk_iter, H5Dchunk.c:8372-8415), and an
// index never allocated stores nothing (H5Dchunk.c:8041-8047): the walk now
// does the same and returns at once.
//
// Red on edb53b1: the call does not return within the 10 s deadline.
func TestCollectChunks_UnallocatedHugeGrid(t *testing.T) {
	sb := &Superblock{Version: 3, OffsetSize: 8, LengthSize: 8, Endianness: binary.LittleEndian}
	const grid = uint64(1) << 40
	d := tinyChunkedInt8(4, ChunkIndexFArray, grid, undefinedAddress)
	d.dataspace.Dimensions = []uint64{grid}
	d.nelmts, d.dataBytes = grid, grid

	type result struct {
		entries []ChunkEntry
		err     error
	}
	done := make(chan result, 1)
	go func() {
		entries, err := d.CollectChunks(bytes.NewReader(nil), sb, 1<<20)
		done <- result{entries, err}
	}()

	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Empty(t, got.entries)
	case <-time.After(10 * time.Second):
		t.Fatal("listing the chunks of an unallocated index walked the whole 2^40 grid")
	}
}
