// Copyright (c) 2025 SciGo HDF5 Library Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.

package structures

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// flatFile is a file image that, unlike mockFileBuffer, lets a later write
// replace earlier bytes, as an in-place heap rewrite needs.
type flatFile struct{ data []byte }

func newFlatFile() *flatFile { return &flatFile{} }

func (f *flatFile) WriteAtAddress(data []byte, address uint64) error {
	if end := address + uint64(len(data)); end > uint64(len(f.data)) {
		f.data = append(f.data, make([]byte, end-uint64(len(f.data)))...)
	}
	copy(f.data[address:], data)
	return nil
}

func (f *flatFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func testSuperblock8() *core.Superblock {
	return &core.Superblock{LengthSize: 8, OffsetSize: 8, Endianness: binary.LittleEndian}
}

// managedIDOffset decodes the offset field of a managed heap ID
// (H5HF_MAN_ID_DECODE, H5HFpkg.h:156).
func managedIDOffset(id []byte, offSize uint8) uint64 {
	var b [8]byte
	copy(b[:], id[1:1+int(offSize)])
	return binary.LittleEndian.Uint64(b[:])
}

// requireCHeapHeader decodes the heap header at headerAddr and checks what a
// heap this package writes always has, as C creates it (H5HFhdr.c:357, 360):
// the checksum flag and HADDR_UNDEF for both the huge-object B-tree and the
// free-space addresses, so the heap is never classified legacy.
func requireCHeapHeader(t *testing.T, buf io.ReaderAt, headerAddr uint64, sb *core.Superblock) *FractalHeapHeader {
	t.Helper()
	h, err := parseFractalHeapHeader(buf, headerAddr, sb.LengthSize, sb.OffsetSize, sb.Endianness)
	if err != nil {
		t.Fatalf("decode the heap header: %v", err)
	}
	if isLegacyHeapHeader(h.HugeObjBTreeAddr, h.FreeSpaceSectionAddr) {
		t.Error("a heap written by this package is classified legacy")
	}
	if h.Flags != 0x02 || !h.ChecksumDirectBlocks {
		t.Errorf("flags 0x%02X, want 0x02 (checksum direct blocks)", h.Flags)
	}
	if h.HugeObjBTreeAddr != ^uint64(0) || h.FreeSpaceSectionAddr != ^uint64(0) {
		t.Errorf("huge B-tree address 0x%X, free-space address 0x%X, want HADDR_UNDEF for both",
			h.HugeObjBTreeAddr, h.FreeSpaceSectionAddr)
	}
	return h
}

// TestAttributeFractalHeapCLayout checks the heap a dense attribute writer
// creates against what H5HF__hdr_create and H5HF__man_dblock_create produce
// for the C attribute heap parameters (H5Oprivate.h:57-64): the checksum
// flag, HADDR_UNDEF for the huge-object B-tree and free-space addresses
// (H5HFhdr.c:357, 360), object offsets that count the block prefix
// (H5HFdblock.c:163) and the checksum in the prefix, over the whole block
// with that field zeroed (H5HFcache.c:2047-2058).
func TestAttributeFractalHeapCLayout(t *testing.T) {
	sb := testSuperblock8()
	heap := NewAttributeFractalHeap()
	if err := heap.SetAddressSize(sb.OffsetSize); err != nil {
		t.Fatal(err)
	}
	objs := [][]byte{[]byte("first"), []byte("second object")}
	ids := make([][]byte, 0, len(objs))
	for _, o := range objs {
		id, err := heap.InsertObject(o)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	buf := newFlatFile()
	headerAddr, err := heap.WriteToFile(buf, &mockAllocator{nextAddr: 1024}, sb)
	if err != nil {
		t.Fatal(err)
	}

	rh, err := core.OpenFractalHeap(buf, sb, headerAddr)
	if err != nil {
		t.Fatalf("open the heap with the core reader: %v", err)
	}
	h := requireCHeapHeader(t, buf, headerAddr, sb)
	if h.HeapOffsetSize != 5 || h.HeapIDLen != 8 {
		t.Errorf("heap offset size %d, ID length %d, want 5 and 8 (max_index 40)", h.HeapOffsetSize, h.HeapIDLen)
	}

	// H5HF_MAN_ABS_DIRECT_OVERHEAD: signature, version, heap address, block
	// offset, checksum.
	prefix := uint64(4 + 1 + 8 + 5 + 4)
	if got := managedIDOffset(ids[0], h.HeapOffsetSize); got != prefix {
		t.Errorf("first object at heap offset %d, want %d (just after the block prefix)", got, prefix)
	}

	block := make([]byte, h.StartingBlockSize)
	if _, err := buf.ReadAt(block, int64(h.RootBlockAddr)); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		off := managedIDOffset(id, h.HeapOffsetSize)
		if got := block[off : off+uint64(len(objs[i]))]; !bytes.Equal(got, objs[i]) {
			t.Errorf("object %d: block bytes at offset %d are %.32q, want %q", i, off, got, objs[i])
		}
		got, err := rh.Object(id)
		if err != nil {
			t.Fatalf("reader: object %d: %v", i, err)
		}
		if !bytes.Equal(got, objs[i]) {
			t.Errorf("reader: object %d is %.32q, want %q", i, got, objs[i])
		}
	}

	csOff := prefix - 4
	stored := binary.LittleEndian.Uint32(block[csOff:])
	zeroed := bytes.Clone(block)
	clear(zeroed[csOff : csOff+4])
	if want := core.JenkinsChecksum(zeroed); stored != want {
		t.Errorf("direct block checksum 0x%08X, want 0x%08X", stored, want)
	}
}

// TestFractalHeapReaderRefusesPrefixOffset checks that a C heap ID pointing
// into the block prefix is corrupt (H5HFman.c:386-399), not prefix bytes.
func TestFractalHeapReaderRefusesPrefixOffset(t *testing.T) {
	sb := testSuperblock8()
	heap := NewAttributeFractalHeap()
	id, err := heap.InsertObject([]byte("object"))
	if err != nil {
		t.Fatal(err)
	}
	buf := newFlatFile()
	headerAddr, err := heap.WriteToFile(buf, &mockAllocator{nextAddr: 1024}, sb)
	if err != nil {
		t.Fatal(err)
	}
	rh, err := core.OpenFractalHeap(buf, sb, headerAddr)
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(id)
	clear(bad[1:6]) // offset 0: the block signature
	if _, err := rh.Object(bad); !errors.Is(err, utils.ErrCorrupt) {
		t.Errorf("Object at offset 0: %v, want ErrCorrupt", err)
	}
}

// writeLegacyHeap writes a heap in the layout hdf5-go v0.14.2 and earlier
// wrote: no checksum flag, both the huge-object B-tree and the free-space
// addresses 0, and object offsets counted from the end of the block prefix.
// With no checksum the prefix is the same as C's, so the objects lie where a
// C heap would put them and only the IDs differ. It returns the header
// address and the legacy IDs.
func writeLegacyHeap(t *testing.T, sb *core.Superblock, objs [][]byte) (*flatFile, uint64, [][]byte) {
	t.Helper()
	heap := NewWritableFractalHeap(DenseHeapBlockSize)
	heap.Header.Flags = 0
	heap.Header.HugeObjectBTreeAddr = 0
	heap.Header.FreeSectionAddress = 0
	heap.resetDirectBlock()
	prefix := heap.directBlockOverhead()

	ids := make([][]byte, 0, len(objs))
	for _, o := range objs {
		id, err := heap.InsertObject(o)
		if err != nil {
			t.Fatal(err)
		}
		off := managedIDOffset(id, heap.Header.HeapOffsetSize) - prefix
		legacy := bytes.Clone(id)
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], off)
		copy(legacy[1:1+int(heap.Header.HeapOffsetSize)], b[:])
		ids = append(ids, legacy)
	}
	buf := newFlatFile()
	headerAddr, err := heap.WriteToFile(buf, &mockAllocator{nextAddr: 1024}, sb)
	if err != nil {
		t.Fatal(err)
	}
	return buf, headerAddr, ids
}

// TestFractalHeapReaderLegacyOffsets checks that a heap written by v0.14.2
// or earlier, recognized by both addresses being 0, is read with its own
// offset rule.
func TestFractalHeapReaderLegacyOffsets(t *testing.T) {
	sb := testSuperblock8()
	objs := [][]byte{[]byte("alpha"), []byte("beta-object")}
	buf, headerAddr, ids := writeLegacyHeap(t, sb, objs)

	rh, err := core.OpenFractalHeap(buf, sb, headerAddr)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		got, err := rh.Object(id)
		if err != nil {
			t.Fatalf("legacy object %d: %v", i, err)
		}
		if !bytes.Equal(got, objs[i]) {
			t.Errorf("legacy object %d is %.32q, want %q", i, got, objs[i])
		}
	}
}

// TestFractalHeapAdoptLegacyMigrates checks the legacy migration on a
// rewrite: objects keep their bytes, every ID is remapped to the C layout,
// both addresses become HADDR_UNDEF and the heap is checksummed, so the heap
// is never left with mixed offsets.
func TestFractalHeapAdoptLegacyMigrates(t *testing.T) {
	sb := testSuperblock8()
	objs := [][]byte{[]byte("alpha"), []byte("beta-object"), []byte("gamma")}
	buf, headerAddr, ids := writeLegacyHeap(t, sb, objs)

	heap := NewAttributeFractalHeap()
	if err := heap.LoadFromFile(buf, headerAddr, sb); err != nil {
		t.Fatal(err)
	}
	// The legacy objects are readable before adoption (the name index
	// migration resolves names through them).
	if got, err := heap.GetObject(ids[1]); err != nil || !bytes.Equal(got, objs[1]) {
		t.Fatalf("legacy GetObject before adoption: %.32q, %v", got, err)
	}
	remap, err := heap.AdoptObjects(append(ids, ids[0])) // a duplicate ID is one object
	if err != nil {
		t.Fatal(err)
	}
	if len(remap) != len(ids) {
		t.Fatalf("remap has %d entries, want %d", len(remap), len(ids))
	}
	newObj := []byte("delta")
	newID, err := heap.InsertObject(newObj)
	if err != nil {
		t.Fatal(err)
	}
	if err := heap.WriteAt(buf, sb); err != nil {
		t.Fatal(err)
	}

	rh, err := core.OpenFractalHeap(buf, sb, headerAddr)
	if err != nil {
		t.Fatal(err)
	}
	mh := requireCHeapHeader(t, buf, headerAddr, sb)
	prefix := 4 + 1 + 8 + uint64(mh.HeapOffsetSize) + 4
	for i, id := range ids {
		nid, ok := remap[string(id)]
		if !ok {
			t.Fatalf("legacy ID %d missing from the remap", i)
		}
		if off := managedIDOffset(nid, mh.HeapOffsetSize); off < prefix {
			t.Errorf("object %d at offset %d, inside the %d-byte prefix", i, off, prefix)
		}
		got, err := rh.Object(nid)
		if err != nil {
			t.Fatalf("migrated object %d: %v", i, err)
		}
		if !bytes.Equal(got, objs[i]) {
			t.Errorf("migrated object %d is %.32q, want %q", i, got, objs[i])
		}
	}
	if got, err := rh.Object(newID); err != nil || !bytes.Equal(got, newObj) {
		t.Errorf("object inserted after migration: %.32q, %v", got, err)
	}
}

// TestFractalHeapAdoptCHeapAppendsAfterLiveObjects checks the C-heap RMW
// position: a heap whose header iterator offset (0 for a root direct block,
// H5HFdblock.c:347-366) says nothing about the used space gets new objects
// after the last live one, never over the prefix or a live object.
func TestFractalHeapAdoptCHeapAppendsAfterLiveObjects(t *testing.T) {
	sb := testSuperblock8()
	heap := NewAttributeFractalHeap()
	objs := [][]byte{[]byte("one"), []byte("two-two")}
	ids := make([][]byte, 0, len(objs))
	for _, o := range objs {
		id, err := heap.InsertObject(o)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	buf := newFlatFile()
	headerAddr, err := heap.WriteToFile(buf, &mockAllocator{nextAddr: 1024}, sb)
	if err != nil {
		t.Fatal(err)
	}

	loaded := NewAttributeFractalHeap()
	if err := loaded.LoadFromFile(buf, headerAddr, sb); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.InsertObject([]byte("x")); err == nil {
		t.Fatal("insert before AdoptObjects succeeded")
	}
	if _, err := loaded.AdoptObjects(ids); err != nil {
		t.Fatal(err)
	}
	id3, err := loaded.InsertObject([]byte("three"))
	if err != nil {
		t.Fatal(err)
	}
	last := managedIDOffset(ids[1], loaded.Header.HeapOffsetSize) + uint64(len(objs[1]))
	if got := managedIDOffset(id3, loaded.Header.HeapOffsetSize); got != last {
		t.Errorf("new object at offset %d, want %d (after the last live object)", got, last)
	}
	if err := loaded.WriteAt(buf, sb); err != nil {
		t.Fatal(err)
	}
	rh, err := core.OpenFractalHeap(buf, sb, headerAddr)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if got, err := rh.Object(id); err != nil || !bytes.Equal(got, objs[i]) {
			t.Errorf("object %d after RMW: %.32q, %v; want %q", i, got, err, objs[i])
		}
	}
	if got, err := rh.Object(id3); err != nil || string(got) != "three" {
		t.Errorf("object inserted after RMW: %.32q, %v; want \"three\"", got, err)
	}
}
