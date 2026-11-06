package core

import (
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// denseKind is what a dense storage index lists: attributes (H5Adense.c) or
// links (H5Gdense.c). It decides which v2 B-tree record types the index may
// have.
type denseKind int

const (
	denseAttributes denseKind = iota
	denseLinks
)

// Record sizes of the dense storage indexes and where each record keeps its
// fractal heap ID:
//
//	type 5 (H5G__dense_btree2_name_decode, H5Gbtree2.c:293-294):   hash(4) id(7)
//	type 6 (H5G__dense_btree2_corder_decode, H5Gbtree2.c:426-427): corder(8) id(7)
//	type 8 (H5A__dense_btree2_name_decode, H5Abtree2.c:312-326):   id(8) flags(1) corder(4) hash(4)
//	type 9 (H5A__dense_btree2_corder_decode, H5Abtree2.c:449-462): id(8) flags(1) corder(4)
//
// H5G_DENSE_FHEAP_ID_LEN is 7 (H5Gpkg.h:40) and H5O_FHEAP_ID_LEN 8
// (H5Oprivate.h:64).
var denseRecordLayout = map[uint8]struct{ size, idAt, idLen uint64 }{
	bt2TypeGroupName:   {11, 4, 7},
	bt2TypeGroupCorder: {15, 8, 7},
	bt2TypeAttrName:    {17, 0, 8},
	bt2TypeAttrCorder:  {13, 0, 8},
}

// denseIndexTypes are the record types C opens an index of each kind with
// (H5A__dense_*: H5A_BT2_NAME, H5A_BT2_CORDER; H5G__dense_*: H5G_BT2_NAME,
// H5G_BT2_CORDER). An attribute name index of type 5 is accepted too: this
// library up to v0.14.2 wrote dense attribute name indexes with link name
// records (structures.NewWritableBTreeV2 in internal/writer/
// dense_attribute_writer.go), hash(4) id(7), files the C library cannot open.
// Its readers keep reading those files.
var denseIndexTypes = map[denseKind][]uint8{
	denseAttributes: {bt2TypeAttrName, bt2TypeAttrCorder, bt2TypeGroupName},
	denseLinks:      {bt2TypeGroupName, bt2TypeGroupCorder},
}

// ReadDenseHeapObjects returns the link messages of a group's dense link
// storage, in the order of its index: the v2 B-tree at btreeAddr (link name
// or creation order records, types 5 and 6) and the fractal heap at heapAddr
// that holds the messages. The caller decodes each with
// structures.ParseLinkMessage.
func ReadDenseHeapObjects(r io.ReaderAt, btreeAddr, heapAddr uint64, sb *Superblock) ([][]byte, error) {
	return readDenseHeapObjects(r, sb, btreeAddr, heapAddr, denseLinks)
}

// readDenseHeapObjects walks the dense storage index at btreeAddr with the
// checked v2 B-tree reader (readBT2Header, walk: checksums, node counts,
// depth, cycles, record count) and returns the heap object each record names.
//
// Deviation, stricter than C: the record size is the one the record type
// defines (C uses the stored size for node arithmetic and the class's own
// decoder for the fields, so a lying size misreads every record).
func readDenseHeapObjects(r io.ReaderAt, sb *Superblock, btreeAddr, heapAddr uint64, kind denseKind) ([][]byte, error) {
	h, err := readBT2Header(r, sb, btreeAddr, denseIndexTypes[kind]...)
	if err != nil {
		return nil, err
	}
	layout := denseRecordLayout[h.typ]
	if h.rrecSize != layout.size {
		return nil, fmt.Errorf("%w: v2 B-tree at 0x%X has %d-byte records, type %d records are %d bytes",
			utils.ErrCorrupt, h.addr, h.rrecSize, h.typ, layout.size)
	}
	if h.allNrec == 0 {
		return nil, nil
	}

	heap, err := OpenFractalHeap(r, sb, heapAddr)
	if err != nil {
		return nil, err
	}

	// allNrec is bounded by the file (checkRoot), and each record is read
	// from it before its object is appended.
	var out [][]byte
	err = h.walk(r, sb, func(rec []byte) error {
		data, err := heap.Object(rec[layout.idAt : layout.idAt+layout.idLen])
		if err != nil {
			return fmt.Errorf("heap object %d: %w", len(out), err)
		}
		out = append(out, data)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}
