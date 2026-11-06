package core

import (
	"encoding/binary"
	"fmt"

	"github.com/scigolib/hdf5/internal/utils"
)

// Free-space manager header constants (H5FSpkg.h, H5FScache.c).
const (
	fsHeaderSig         = "FSHD"
	fsVersion           = 0 // H5FS_HDR_VERSION, H5FSpkg.h:44
	fsClientFractalHeap = 0 // H5FS_CLIENT_FHEAP_ID, H5FSprivate.h:145
)

// indirectBlockSize is H5HF_MAN_INDIRECT_SIZE (H5HFpkg.h:119-129) for an
// unfiltered indirect block of nrows rows: prefix, checksum, heap header
// address, block offset and one child address per entry. It is what
// H5HF__man_iblock_create allocates and H5HF__man_iblock_delete frees.
func (h *FractalHeap) indirectBlockSize(nrows uint64) uint64 {
	o := uint64(h.sb.OffsetSize)
	//nolint:gosec // G115: offSize is at most 8
	return fheapPrefixSize + fheapChecksumSize + o + uint64(h.offSize) + nrows*h.width*o
}

// Blocks passes to block every piece of file space the heap owns, each at
// the size C allocated for it, in the order H5HF__hdr_delete frees them
// (H5HFhdr.c:1373-1420):
//   - the free-space manager: its header and, when it has serialized
//     sections, their allocation (H5HF__space_delete → H5FS_delete,
//     H5FS.c:315-370);
//   - the root direct block of start_block_size bytes, or the root indirect
//     block with every direct and indirect block below it
//     (H5HF__man_iblock_delete, H5HFiblock.c:1580-1617);
//   - the v2 B-tree of 'huge' objects and every huge object it records
//     (H5HF__huge_delete, H5HFhuge.c:1097-1136; the remove callbacks free
//     each object's address and length, H5HFbtree2.c);
//   - the header itself, H5HF_HEADER_SIZE bytes.
//
// Every block is read and checked as Object reads it (signatures, owning
// heap, block offsets, checksums), so a heap that cannot be read is never
// freed. A legacy heap (FractalHeap) stores 0 for the huge object B-tree and
// the free-space manager: it has neither.
func (h *FractalHeap) Blocks(block func(addr, size uint64) error) error {
	if !h.legacy && h.fsAddr != undefinedAddress {
		if err := h.freeSpaceBlocks(block); err != nil {
			return fmt.Errorf("fractal heap at 0x%X: free-space manager: %w", h.addr, err)
		}
	}
	if err := h.managedBlocks(block); err != nil {
		return fmt.Errorf("fractal heap at 0x%X: %w", h.addr, err)
	}
	if !h.legacy && h.hugeBT2Addr != undefinedAddress {
		if err := h.hugeBlocks(block); err != nil {
			return fmt.Errorf("fractal heap at 0x%X: huge objects: %w", h.addr, err)
		}
	}

	return block(h.addr, h.hdrSize)
}

// managedBlocks reports the root direct block, or the tree of indirect and
// direct blocks under the root indirect block.
func (h *FractalHeap) managedBlocks(block func(addr, size uint64) error) error {
	if h.rootAddr == undefinedAddress {
		return nil
	}
	if h.currRootRows == 0 {
		if err := h.checkDirectBlock(h.rootAddr, h.startBlock, 0); err != nil {
			return err
		}

		return block(h.rootAddr, h.startBlock)
	}
	root, err := h.indirectBlock(h.rootAddr, h.currRootRows, 0)
	if err != nil {
		return err
	}

	return h.indirectBlocks(h.rootAddr, root, block)
}

// indirectBlocks reports the children of the indirect block ib at addr, then
// ib itself, as H5HF__man_iblock_delete frees them (H5HFiblock.c:1580-1650):
// a defined entry of a row below max_direct_rows is a direct block of the
// row's block size, a deeper one an indirect block of
// H5HF__dtable_size_to_rows rows (childIndirectBlock). Every child has fewer
// rows than its parent, so the recursion ends.
func (h *FractalHeap) indirectBlocks(addr uint64, ib *fheapIBlock, block func(addr, size uint64) error) error {
	for row := uint64(0); row < ib.nrows; row++ {
		for col := uint64(0); col < h.width; col++ {
			if err := h.childBlocks(ib, row, col, block); err != nil {
				return err
			}
		}
	}

	return block(addr, h.indirectBlockSize(ib.nrows))
}

// childBlocks reports the child of ib at (row, col), if defined, with
// everything under it (indirectBlocks).
func (h *FractalHeap) childBlocks(ib *fheapIBlock, row, col uint64, block func(addr, size uint64) error) error {
	child := ib.ents[row*h.width+col]
	if child == undefinedAddress {
		return nil
	}
	if row < h.maxDirectRows {
		size := h.rowBlockSize[row]
		if err := h.checkDirectBlock(child, size, ib.blockOff+h.rowBlockOff[row]+col*size); err != nil {
			return err
		}

		return block(child, size)
	}
	sub, err := h.childIndirectBlock(ib, row, col)
	if err != nil {
		return err
	}

	return h.indirectBlocks(child, sub, block)
}

// freeSpaceBlocks reads the heap's free-space manager header
// (H5FS__cache_hdr_deserialize, H5FScache.c) and reports it at
// H5FS_HEADER_SIZE (H5FSpkg.h:56-75) and, when it has serialized sections,
// their allocation of alloc_sect_size bytes, as H5FS_delete frees them
// (H5FS.c:315-370).
//
//	"FSHD" version(1) client(1) tot_space(L) tot_sect_count(L)
//	serial_sect_count(L) ghost_sect_count(L) nclasses(2) shrink(2)
//	expand(2) max_sect_addr(2) max_sect_size(L) sect_addr(O)
//	sect_size(L) alloc_sect_size(L) checksum(4)
func (h *FractalHeap) freeSpaceBlocks(block func(addr, size uint64) error) error {
	l, o := int(h.sb.LengthSize), int(h.sb.OffsetSize)
	size := uint64(fheapPrefixSize + 1 + 4*l + 4*2 + l + o + 2*l + fheapChecksumSize) //nolint:gosec // small
	buf, err := readChecksummedBlock(h.r, h.sb, h.fsAddr, size, "free-space manager header")
	if err != nil {
		return err
	}
	if string(buf[:4]) != fsHeaderSig {
		return fmt.Errorf("%w: wrong free-space manager header signature %q at 0x%X (H5FScache.c)",
			utils.ErrCorrupt, buf[:4], h.fsAddr)
	}
	if buf[4] != fsVersion {
		return fmt.Errorf("%w: wrong free-space manager header version %d at 0x%X (H5FScache.c)",
			utils.ErrCorrupt, buf[4], h.fsAddr)
	}
	if buf[5] != fsClientFractalHeap {
		return fmt.Errorf("%w: free-space manager at 0x%X serves client %d, not a fractal heap",
			utils.ErrCorrupt, h.fsAddr, buf[5])
	}

	f := &fheapFields{b: buf, p: fheapPrefixSize + 1}
	f.uint(l) // tot_space
	f.uint(l) // tot_sect_count
	serial := f.uint(l)
	f.p += l + 4*2 + l // ghost_sect_count, four 2-byte fields, max_sect_size
	sectAddr := f.addr(o)
	sectSize := f.uint(l)
	allocSectSize := f.uint(l)

	if err := block(h.fsAddr, size); err != nil {
		return err
	}
	if serial == 0 {
		return nil
	}
	if sectAddr == undefinedAddress || allocSectSize == 0 || allocSectSize < sectSize {
		return fmt.Errorf("%w: free-space manager at 0x%X has %d sections at 0x%X in %d of %d allocated bytes",
			utils.ErrCorrupt, h.fsAddr, serial, sectAddr, sectSize, allocSectSize)
	}

	return block(sectAddr, allocSectSize)
}

// hugeBlocks reports the v2 B-tree that tracks the heap's 'huge' objects and
// every object it records. Its record type follows from the heap ID form
// (H5HF__huge_bt2_create, H5HFhuge.c:100-139): directly accessed objects
// (huge_ids_direct) are type 3, address(O) length(L); the others type 1,
// address(O) length(L) id(L). Filtered heaps are refused by OpenFractalHeap.
func (h *FractalHeap) hugeBlocks(block func(addr, size uint64) error) error {
	o, l := uint64(h.sb.OffsetSize), uint64(h.sb.LengthSize)
	typ, rrec := uint8(bt2TypeHugeIndirect), o+2*l
	if h.hugeIDsDirect {
		typ, rrec = bt2TypeHugeDirect, o+l
	}
	bt, err := readBT2Header(h.r, h.sb, h.hugeBT2Addr, typ)
	if err != nil {
		return err
	}
	if bt.rrecSize != rrec {
		return fmt.Errorf("%w: v2 B-tree at 0x%X has %d-byte records, type %d records are %d bytes",
			utils.ErrCorrupt, bt.addr, bt.rrecSize, typ, rrec)
	}

	return bt.walkBlocks(h.r, h.sb, block, func(rec []byte) error {
		addr := readLayoutAddress(rec[:o], int(h.sb.OffsetSize), binary.LittleEndian)
		length := fheapUint(rec[o : o+l])
		if addr == undefinedAddress || length == 0 {
			return fmt.Errorf("%w: huge object record at 0x%X of %d bytes", utils.ErrCorrupt, addr, length)
		}

		return block(addr, length)
	})
}
