package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"slices"

	"github.com/scigolib/hdf5/internal/utils"
)

// Version 2 B-tree signatures and record types (H5B2pkg.h, H5B2private.h:38-57).
const (
	bt2HeaderSig   = "BTHD"
	bt2InternalSig = "BTIN"
	bt2LeafSig     = "BTLF"

	bt2TypeGroupName      = 5         // H5B2_GRP_DENSE_NAME_ID.
	bt2TypeGroupCorder    = 6         // H5B2_GRP_DENSE_CORDER_ID.
	bt2TypeAttrName       = 8         // H5B2_ATTR_DENSE_NAME_ID.
	bt2TypeAttrCorder     = 9         // H5B2_ATTR_DENSE_CORDER_ID.
	bt2TypeChunk          = 10        // H5B2_CDSET_ID.
	bt2TypeFilteredChunk  = 11        // H5B2_CDSET_FILT_ID.
	bt2NumTypes           = 13        // H5B2_NUM_BTREE_ID.
	bt2NodePrefix         = 4 + 1 + 1 // Signature, version, type before the records.
	bt2MaxRecordsPerField = 2         // H5B2_SIZEOF_RECORDS_PER_NODE.
)

// bt2NodeInfo is what H5B2__hdr_init computes per depth (H5B2hdr.c:138-190).
type bt2NodeInfo struct {
	maxNrec        uint64
	cumMaxNrec     uint64
	cumMaxNrecSize int
}

// bt2Header is a decoded, checked version 2 B-tree header
// (H5B2__cache_hdr_deserialize, H5B2cache.c:224-320).
type bt2Header struct {
	addr        uint64
	addrSize    uint64
	typ         uint8
	nodeSize    uint64
	rrecSize    uint64
	depth       int
	rootAddr    uint64
	rootNrec    uint64
	allNrec     uint64
	maxNrecSize int
	info        []bt2NodeInfo // One per depth, 0 = leaves.
}

// readBT2Header reads the header at addr:
//
//	"BTHD" version(1)=0 type(1) node_size(4) rrec_size(2) depth(2)
//	split_percent(1) merge_percent(1) root_addr(sizeof_addr)
//	root_nrec(2) root_all_nrec(sizeof_size) checksum(4)
//
// Checks as C (H5B2cache.c:224-320, H5B2hdr.c:95-190): signature, version,
// type below H5B2_NUM_BTREE_ID, checksum. Deviations, stricter, because C
// asserts them or would compute with wrapped values:
//   - the type is one the caller expects (C opens each index with the record
//     class of its slot, and H5B2__cache_hdr_deserialize refuses another type);
//   - a leaf holds at least one record and the record count of a node fits the
//     two bytes a pointer stores it in (H5B2hdr.c:165 asserts it);
//   - every internal depth holds at least one record and the cumulative counts
//     do not overflow, which bounds the depth at about 64;
//   - the root's counts are within what its depth can hold, and its records
//     fit in the file.
func readBT2Header(r io.ReaderAt, sb *Superblock, addr uint64, wantTypes ...uint8) (*bt2Header, error) {
	ls, os := int(sb.LengthSize), int(sb.OffsetSize)
	size := uint64(bt2NodePrefix+4+2+2+1+1+2+4) + uint64(sb.OffsetSize) + uint64(sb.LengthSize)

	buf, err := readChecksummedBlock(r, sb, addr, size, "v2 B-tree header")
	if err != nil {
		return nil, err
	}
	if string(buf[:4]) != bt2HeaderSig {
		return nil, fmt.Errorf("%w: v2 B-tree header at 0x%X: wrong signature %q", utils.ErrCorrupt, addr, buf[:4])
	}
	if buf[4] != 0 {
		return nil, fmt.Errorf("%w: v2 B-tree header at 0x%X: unsupported version %d", utils.ErrCorrupt, addr, buf[4])
	}
	if buf[5] >= bt2NumTypes || !slices.Contains(wantTypes, buf[5]) {
		return nil, fmt.Errorf("%w: v2 B-tree header at 0x%X: type %d, want one of %d",
			utils.ErrCorrupt, addr, buf[5], wantTypes)
	}

	h := &bt2Header{
		addr:     addr,
		addrSize: uint64(sb.OffsetSize),
		typ:      buf[5],
		nodeSize: uint64(binary.LittleEndian.Uint32(buf[6:])),
		rrecSize: uint64(binary.LittleEndian.Uint16(buf[10:])),
		depth:    int(binary.LittleEndian.Uint16(buf[12:])),
	}
	// buf[14], buf[15]: split and merge percentages, used only when writing.
	p := 16
	h.rootAddr = readLayoutAddress(buf[p:], os, sb.Endianness)
	p += os
	h.rootNrec = uint64(binary.LittleEndian.Uint16(buf[p:]))
	h.allNrec = readUint64(buf[p+2:], ls, sb.Endianness)

	if err := h.init(); err != nil {
		return nil, fmt.Errorf("%w: v2 B-tree header at 0x%X: %w", utils.ErrCorrupt, addr, err)
	}
	if err := h.checkRoot(r, sb); err != nil {
		return nil, fmt.Errorf("%w: v2 B-tree header at 0x%X: %w", utils.ErrCorrupt, addr, err)
	}

	return h, nil
}

// init computes the per-depth node information (H5B2__hdr_init,
// H5B2hdr.c:138-190).
func (h *bt2Header) init() error {
	if h.rrecSize == 0 {
		return fmt.Errorf("record size 0")
	}
	if h.nodeSize <= bt2NodePrefix+4 {
		return fmt.Errorf("node size %d holds no record", h.nodeSize)
	}

	leafMax := (h.nodeSize - (bt2NodePrefix + 4)) / h.rrecSize // H5B2_NUM_LEAF_REC.
	if leafMax == 0 {
		return fmt.Errorf("node size %d holds no %d-byte record", h.nodeSize, h.rrecSize)
	}
	h.maxNrecSize = limitEncSize(leafMax)
	if h.maxNrecSize > bt2MaxRecordsPerField {
		return fmt.Errorf("%d records per node do not fit the %d-byte record count", leafMax, bt2MaxRecordsPerField)
	}

	h.info = make([]bt2NodeInfo, 1, min(h.depth+1, 65)) // The checks below end deeper trees.
	h.info[0] = bt2NodeInfo{maxNrec: leafMax, cumMaxNrec: leafMax}
	for d := 1; d <= h.depth; d++ {
		prev := h.info[d-1]
		ptr := h.pointerSize(d)
		if h.nodeSize < bt2NodePrefix+4+ptr {
			return fmt.Errorf("node size %d holds no child pointer at depth %d", h.nodeSize, d)
		}
		maxNrec := (h.nodeSize - (bt2NodePrefix + 4 + ptr)) / (h.rrecSize + ptr) // H5B2_NUM_INT_REC.
		if maxNrec == 0 {
			return fmt.Errorf("node size %d holds no record at depth %d", h.nodeSize, d)
		}

		// cum_max_nrec = (max_nrec + 1) × cum_max_nrec[d-1] + max_nrec.
		cum, err := utils.SafeMultiply(maxNrec+1, prev.cumMaxNrec)
		if err == nil {
			cum, err = utils.SafeAdd(cum, maxNrec)
		}
		if err != nil {
			return fmt.Errorf("depth %d: a tree this deep would hold more than 2^64 records", h.depth)
		}
		h.info = append(h.info, bt2NodeInfo{maxNrec: maxNrec, cumMaxNrec: cum, cumMaxNrecSize: limitEncSize(cum)})
	}

	return nil
}

// pointerSize is H5B2_INT_POINTER_SIZE for a node at depth d >= 1: the child's
// address, its record count and, below depth 1, its total record count.
func (h *bt2Header) pointerSize(d int) uint64 {
	return h.addrSize + uint64(h.maxNrecSize+h.info[d-1].cumMaxNrecSize) //nolint:gosec // G115: two counts of at most 8 bytes
}

// checkRoot checks the root's counts against the tree's shape and the file.
func (h *bt2Header) checkRoot(r io.ReaderAt, sb *Superblock) error {
	if h.rootAddr == undefinedAddress {
		if h.allNrec != 0 || h.rootNrec != 0 {
			return fmt.Errorf("no root node, but %d records", h.allNrec)
		}

		return nil
	}
	if h.allNrec == 0 && h.rootNrec == 0 {
		// A root node with no records: C frees the root when its last
		// record goes (H5B2leaf.c:811-818), but this library's attribute
		// deletion up to v0.14.2 kept it. Such a tree is empty; its node is
		// not read.
		h.rootAddr = undefinedAddress

		return nil
	}

	top := h.info[h.depth]
	if h.rootNrec == 0 || h.rootNrec > top.maxNrec {
		return fmt.Errorf("root has %d records, a node at depth %d holds 1..%d", h.rootNrec, h.depth, top.maxNrec)
	}
	if h.allNrec < h.rootNrec || h.allNrec > top.cumMaxNrec {
		return fmt.Errorf("tree claims %d records, a root of %d at depth %d holds %d..%d",
			h.allNrec, h.rootNrec, h.depth, h.rootNrec, top.cumMaxNrec)
	}

	// Every record occupies rrec_size bytes in some node of the file.
	if limit := dataLimit(r, sb); limit > 0 && h.allNrec > limit/h.rrecSize {
		return fmt.Errorf("tree claims %d records of %d bytes, more than the file holds", h.allNrec, h.rrecSize)
	}

	return nil
}

// bt2Walk is the state of one in-order walk of a version 2 B-tree.
type bt2Walk struct {
	r       io.ReaderAt
	sb      *Superblock
	h       *bt2Header
	visited map[uint64]bool
	fn      func(rec []byte) error
}

// walk passes every record of the tree to fn in key order, stopping at the
// first error. Each node is checked as C checks it when loading
// (H5B2cache.c:524-700, :900-1050): signature, version, type, checksum over
// the bytes the node's record count implies. Deviations, stricter:
//   - a node is reached once (a visited set) and lies at the depth its parent
//     implies; the walk ends after depth+1 levels;
//   - a node's record count is at most the maximum for its depth, and an
//     internal node's total equals its records plus its children's totals,
//     as C maintains them (H5B2int.c); the root's total is the header's;
//   - the whole node_size bytes of each node lie in the file, as C reads
//     them (the image length is node_size).
func (h *bt2Header) walk(r io.ReaderAt, sb *Superblock, fn func(rec []byte) error) error {
	if h.rootAddr == undefinedAddress {
		return nil
	}

	w := &bt2Walk{r: r, sb: sb, h: h, visited: map[uint64]bool{}, fn: fn}
	total, err := w.node(h.rootAddr, h.depth, h.rootNrec)
	if err != nil {
		return err
	}
	if total != h.allNrec {
		return fmt.Errorf("%w: v2 B-tree at 0x%X holds %d records, its header says %d", utils.ErrCorrupt, h.addr, total, h.allNrec)
	}

	return nil
}

// node reads the node at addr, at depth with nrec records, passes its records
// (and its children's, in order) to fn and returns how many it passed.
func (w *bt2Walk) node(addr uint64, depth int, nrec uint64) (uint64, error) {
	h := w.h
	what := "v2 B-tree leaf"
	sig := bt2LeafSig
	if depth > 0 {
		what, sig = "v2 B-tree internal node", bt2InternalSig
	}

	if w.visited[addr] {
		return 0, fmt.Errorf("%w: %s at 0x%X is reached twice", utils.ErrCorrupt, what, addr)
	}
	w.visited[addr] = true

	if nrec > h.info[depth].maxNrec {
		return 0, fmt.Errorf("%w: %s at 0x%X has %d records, the most is %d",
			utils.ErrCorrupt, what, addr, nrec, h.info[depth].maxNrec)
	}
	if err := checkRegion(w.r, w.sb, addr, h.nodeSize, what); err != nil {
		return 0, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}

	var ptr uint64
	if depth > 0 {
		ptr = h.pointerSize(depth)
	}
	// Both terms are below node_size: nrec <= max_nrec for this depth.
	span := bt2NodePrefix + nrec*h.rrecSize + 4
	if depth > 0 {
		span += (nrec + 1) * ptr
	}

	buf, err := readChecksummedBlock(w.r, w.sb, addr, span, what)
	if err != nil {
		return 0, err
	}
	if string(buf[:4]) != sig {
		return 0, fmt.Errorf("%w: %s at 0x%X: wrong signature %q", utils.ErrCorrupt, what, addr, buf[:4])
	}
	if buf[4] != 0 {
		return 0, fmt.Errorf("%w: %s at 0x%X: unsupported version %d", utils.ErrCorrupt, what, addr, buf[4])
	}
	if buf[5] != h.typ {
		return 0, fmt.Errorf("%w: %s at 0x%X: type %d, the header says %d", utils.ErrCorrupt, what, addr, buf[5], h.typ)
	}

	recs := buf[bt2NodePrefix:]
	if depth == 0 {
		for i := uint64(0); i < nrec; i++ {
			if err := w.fn(recs[i*h.rrecSize : (i+1)*h.rrecSize]); err != nil {
				return 0, err
			}
		}

		return nrec, nil
	}

	return w.children(addr, depth, nrec, recs, buf[bt2NodePrefix+nrec*h.rrecSize:], ptr)
}

// children walks the nrec+1 children of an internal node in order, passing
// the node's own records between them.
func (w *bt2Walk) children(addr uint64, depth int, nrec uint64, recs, ptrs []byte, ptr uint64) (uint64, error) {
	h := w.h
	os := int(w.sb.OffsetSize)
	total := nrec
	for i := uint64(0); i <= nrec; i++ {
		p := ptrs[i*ptr:]
		child := readLayoutAddress(p, os, w.sb.Endianness)
		childNrec := readUint64(p[os:], h.maxNrecSize, binary.LittleEndian)
		childAll := childNrec
		if depth > 1 {
			childAll = readUint64(p[os+h.maxNrecSize:], h.info[depth-1].cumMaxNrecSize, binary.LittleEndian)
		}
		if child == undefinedAddress || childNrec == 0 {
			return 0, fmt.Errorf("%w: v2 B-tree internal node at 0x%X: child %d is empty", utils.ErrCorrupt, addr, i)
		}

		got, err := w.node(child, depth-1, childNrec)
		if err != nil {
			return 0, err
		}
		if got != childAll {
			return 0, fmt.Errorf("%w: v2 B-tree node at 0x%X holds %d records, its parent says %d",
				utils.ErrCorrupt, child, got, childAll)
		}
		total += got // Bounded by the visited nodes' records, each in the file.

		if i < nrec {
			if err := w.fn(recs[i*h.rrecSize : (i+1)*h.rrecSize]); err != nil {
				return 0, err
			}
		}
	}

	return total, nil
}
