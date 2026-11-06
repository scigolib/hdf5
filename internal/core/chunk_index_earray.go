package core

import (
	"fmt"
	"io"
	"math"
	"math/bits"

	"github.com/scigolib/hdf5/internal/utils"
)

// Extensible array signatures (H5EApkg.h).
const (
	earrayHeaderSig     = "EAHD"
	earrayIndexBlockSig = "EAIB"
	earraySuperBlockSig = "EASB"
	earrayDataBlockSig  = "EADB"

	// earrayMaxNelmtsBits is H5EA_MAX_NELMTS_IDX_MAX (H5EAhdr.c:49).
	earrayMaxNelmtsBits = 64
)

// earraySblkInfo describes one super block of an extensible array: how many
// data blocks it has, their size in elements and the index of its first
// element after the index block's own (H5EA_sblk_info_t, H5EApkg.h:152-157).
type earraySblkInfo struct {
	ndblks     uint64
	dblkNelmts uint64
	startIdx   uint64 // math.MaxUint64 when the index space ends before it.
	startDblk  uint64
}

// earrayHeader is the decoded extensible array header
// (H5EA__cache_hdr_deserialize, H5EAcache.c:290-380).
type earrayHeader struct {
	addr         uint64
	class        uint8
	rawSize      uint64
	maxNelmtsBit uint8
	idxBlkElmts  uint64
	dataBlkMin   uint64
	supBlkMin    uint64
	pageElmts    uint64
	maxIdxSet    uint64
	idxBlkAddr   uint64
	arrOffSize   uint64
	sblks        []earraySblkInfo
	iblkNsblks   uint64 // Super blocks whose data blocks the index block holds.
}

// iterateEArray walks the extensible array index (H5Dearray.c, H5EA*.c), used
// when exactly one dimension is unlimited (H5Dchunk.c:966-990). Chunks are
// numbered row-major over the maximum grid with the unlimited dimension moved
// to the front (H5Dearray.c:1158-1176). The first records live in the index
// block; the rest live in data blocks, which the index block points at
// directly for the first super blocks and through super blocks after that
// (H5EA__lookup_elmt, H5EA.c:309-600).
//
// Unlike H5EA_iterate, which looks up every index below max_idx_set one at a
// time (H5EA.c), the walk visits only the blocks that exist and skips records
// at or above max_idx_set, which C reads as the fill value (H5EA_get). The
// work is then bounded by the blocks in the file, not by a stored count.
//
// Checks, as C makes them while loading each block (H5EAcache.c): signature,
// version 0, class, owning header address, checksum. Deviations, stricter:
//   - the class and record size match the dataset (as for the fixed array);
//   - the header passes the parameter checks C makes when it creates an array
//     (H5EA__hdr_create, H5EAhdr.c:350-385, compiled only in debug builds):
//     every later size computation relies on them;
//   - a block is reached once: two pointers to one block are utils.ErrCorrupt.
//
// The block_off fields of super and data blocks are not checked: C never reads
// them back, and it stores a different value for data blocks of the index
// block (H5EA.c:399-400) than for those of super blocks (H5EA.c:486-487).
func (d *DatasetDescriptor) iterateEArray(r io.ReaderAt, sb *Superblock, s *chunkSink) error {
	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return err
	}
	if unlim < 0 {
		return fmt.Errorf("%w: extensible array chunk index needs exactly one unlimited dimension", utils.ErrCorrupt)
	}

	lin, err := newChunkLinearizer(maxChunks, unlim)
	if err != nil {
		return err
	}

	if d.layout.DataAddress == undefinedAddress {
		return nil
	}

	codec := newChunkElemCodec(d, sb)
	hdr, err := readEArrayHeader(r, sb, d.layout.DataAddress, codec)
	if err != nil {
		return err
	}

	if hdr.idxBlkAddr == undefinedAddress || hdr.maxIdxSet == 0 {
		return nil
	}

	w := &earrayWalk{s: s, r: r, sb: sb, hdr: hdr, codec: codec, lin: lin, visited: map[uint64]bool{}}

	return w.indexBlock()
}

// readEArrayHeader decodes and checks the header at addr:
//
//	"EAHD" version(1) class(1) raw_elmt_size(1) max_nelmts_bits(1)
//	idx_blk_elmts(1) data_blk_min_elmts(1) sup_blk_min_data_ptrs(1)
//	max_dblk_page_nelmts_bits(1) six statistics(sizeof_size each)
//	idx_blk_addr(sizeof_addr) checksum(4)
func readEArrayHeader(r io.ReaderAt, sb *Superblock, addr uint64, codec chunkElemCodec) (*earrayHeader, error) {
	ls, os := int(sb.LengthSize), int(sb.OffsetSize)
	size := uint64(arrayPrefixSize+6) + 6*uint64(sb.LengthSize) + uint64(sb.OffsetSize)

	buf, err := readChecksummedBlock(r, sb, addr, size, "extensible array header")
	if err != nil {
		return nil, err
	}
	if string(buf[:4]) != earrayHeaderSig {
		return nil, fmt.Errorf("%w: extensible array header at 0x%X: wrong signature %q", utils.ErrCorrupt, addr, buf[:4])
	}
	if buf[4] != 0 {
		return nil, fmt.Errorf("%w: extensible array header at 0x%X: unsupported version %d", utils.ErrCorrupt, addr, buf[4])
	}

	hdr := &earrayHeader{
		addr:         addr,
		class:        buf[5],
		rawSize:      uint64(buf[6]),
		maxNelmtsBit: buf[7],
		idxBlkElmts:  uint64(buf[8]),
		dataBlkMin:   uint64(buf[9]),
		supBlkMin:    uint64(buf[10]),
	}
	pageBits := buf[11]
	// Statistics: nsuper_blks, super_blk_size, ndata_blks, data_blk_size,
	// max_idx_set, nelmts. Only max_idx_set (the highest index set, plus one)
	// changes what a read sees.
	hdr.maxIdxSet = readUint64(buf[12+4*ls:], ls, sb.Endianness)
	hdr.idxBlkAddr = readLayoutAddress(buf[12+6*ls:], os, sb.Endianness)

	if err := checkArrayClass(hdr.class, hdr.rawSize, codec, "extensible array", addr); err != nil {
		return nil, err
	}
	if err := hdr.init(pageBits); err != nil {
		return nil, fmt.Errorf("%w: extensible array header at 0x%X: %w", utils.ErrCorrupt, addr, err)
	}

	return hdr, nil
}

// checkParams applies the creation checks of H5EA__hdr_create
// (H5EAhdr.c:350-385) to the parameters a header carries.
func (h *earrayHeader) checkParams(pageBits uint8) error {
	switch {
	case h.maxNelmtsBit == 0 || h.maxNelmtsBit > earrayMaxNelmtsBits:
		return fmt.Errorf("max. elements bits %d out of 1..%d", h.maxNelmtsBit, earrayMaxNelmtsBits)
	case h.supBlkMin < 2 || h.supBlkMin&(h.supBlkMin-1) != 0:
		return fmt.Errorf("min. data block pointers %d is not a power of two >= 2", h.supBlkMin)
	case h.dataBlkMin == 0 || h.dataBlkMin&(h.dataBlkMin-1) != 0:
		return fmt.Errorf("min. data block elements %d is not a power of two", h.dataBlkMin)
	case pageBits > h.maxNelmtsBit || pageBits > maxPageBits:
		return fmt.Errorf("page bits %d above max. elements bits %d", pageBits, h.maxNelmtsBit)
	}

	return nil
}

// init applies checkParams and computes the super block table as
// H5EA__hdr_init does (H5EAhdr.c:120-160).
func (h *earrayHeader) init(pageBits uint8) error {
	if err := h.checkParams(pageBits); err != nil {
		return err
	}
	h.pageElmts = uint64(1) << pageBits
	if h.pageElmts < h.idxBlkElmts {
		return fmt.Errorf("page of %d elements is smaller than the index block's %d", h.pageElmts, h.idxBlkElmts)
	}

	// H5VM_log2_of2 of a power of two; bits.Len64 is 0..64.
	minBits := uint64(bits.Len64(h.dataBlkMin)) - 1 //nolint:gosec // G115: see above.
	if minBits > uint64(h.maxNelmtsBit) {
		return fmt.Errorf("min. data block elements %d exceed the array's %d bits", h.dataBlkMin, h.maxNelmtsBit)
	}
	nsblks := 1 + uint64(h.maxNelmtsBit) - minBits

	// H5EA_SBLK_FIRST_IDX: super blocks below it keep their data block
	// addresses in the index block (H5EApkg.h:145, H5EAiblock.c:113-115).
	h.iblkNsblks = 2 * (uint64(bits.Len64(h.supBlkMin)) - 1) //nolint:gosec // G115: bits.Len64 is 0..64.
	if h.iblkNsblks > nsblks {
		return fmt.Errorf("index block covers %d super blocks of %d", h.iblkNsblks, nsblks)
	}
	if first := sblkDblkNelmts(h.iblkNsblks, h.dataBlkMin); h.iblkNsblks < nsblks && h.pageElmts < first {
		return fmt.Errorf("page of %d elements is smaller than the first super block's data blocks of %d",
			h.pageElmts, first)
	}

	h.arrOffSize = (uint64(h.maxNelmtsBit) + 7) / 8 // H5EA_SIZEOF_OFFSET_BITS.
	h.sblks = make([]earraySblkInfo, nsblks)        // At most 65 entries.
	startIdx, startDblk := uint64(0), uint64(0)
	for u := range h.sblks {
		s := &h.sblks[u]
		s.ndblks = uint64(1) << (uint(u) / 2)                  // H5_EXP2(u / 2).
		s.dblkNelmts = sblkDblkNelmts(uint64(u), h.dataBlkMin) // H5EA_SBLK_DBLK_NELMTS.
		s.startIdx, s.startDblk = startIdx, startDblk

		span, err := utils.SafeMultiply(s.ndblks, s.dblkNelmts)
		if err == nil {
			startIdx, err = utils.SafeAdd(startIdx, span)
		}
		if err != nil {
			startIdx = math.MaxUint64 // Later super blocks start past any index.
		}
		startDblk += s.ndblks
	}

	return nil
}

// sblkDblkNelmts is H5EA_SBLK_DBLK_NELMTS (H5EAhdr.c:53): 2^((u+1)/2) × min.
// With u <= 64 and min <= 128 the product fits in 64 bits.
func sblkDblkNelmts(u, dataBlkMin uint64) uint64 {
	return (uint64(1) << ((u + 1) / 2)) * dataBlkMin
}

// earrayWalk is the state of one walk over an extensible array's blocks.
type earrayWalk struct {
	s       *chunkSink
	r       io.ReaderAt
	sb      *Superblock
	hdr     *earrayHeader
	codec   chunkElemCodec
	lin     *chunkLinearizer
	visited map[uint64]bool
}

// visit refuses a block reached a second time.
func (w *earrayWalk) visit(addr uint64, what string) error {
	if w.visited[addr] {
		return fmt.Errorf("%w: extensible array %s at 0x%X is reached twice", utils.ErrCorrupt, what, addr)
	}
	w.visited[addr] = true

	return nil
}

// indexBlock reads the index block:
//
//	"EAIB" version(1) class(1) header_addr(sizeof_addr)
//	records[idx_blk_elmts] dblk_addrs[2 × (sup_blk_min_data_ptrs - 1)]
//	sblk_addrs[nsblks - iblock nsblks] checksum(4)
//
// (H5EA__cache_iblock_deserialize, H5EAcache.c:680-760; sizes from
// H5EA__iblock_alloc, H5EAiblock.c:113-115).
func (w *earrayWalk) indexBlock() error {
	h := w.hdr
	os := uint64(w.sb.OffsetSize)
	ndblkAddrs := 2 * (h.supBlkMin - 1)
	nsblkAddrs := uint64(len(h.sblks)) - h.iblkNsblks
	size := arrayPrefixSize + os + h.idxBlkElmts*h.rawSize + (ndblkAddrs+nsblkAddrs)*os // Small: every factor is a byte.

	if err := w.visit(h.idxBlkAddr, "index block"); err != nil {
		return err
	}
	buf, err := readChecksummedBlock(w.r, w.sb, h.idxBlkAddr, size, "extensible array index block")
	if err != nil {
		return err
	}
	if err := checkBlockPrefix(buf, earrayIndexBlockSig, h.class, h.addr, w.sb, "extensible array index block"); err != nil {
		return err
	}

	p := arrayPrefixSize - 4 + os
	if err := w.addRecords(buf[p:], 0, h.idxBlkElmts); err != nil {
		return err
	}
	p += h.idxBlkElmts * h.rawSize

	// Data blocks of the first super blocks, numbered by start_dblk.
	for u := uint64(0); u < h.iblkNsblks; u++ {
		s := h.sblks[u]
		for k := uint64(0); k < s.ndblks; k++ {
			addr := readLayoutAddress(buf[p+(s.startDblk+k)*os:], int(w.sb.OffsetSize), w.sb.Endianness)
			first, ok := w.start(h.idxBlkElmts+s.startIdx, k, s.dblkNelmts) // Small: u < iblkNsblks <= 14.
			if !ok {
				return nil // This block and every later one start past max_idx_set.
			}
			if err := w.dataBlock(addr, first, s.dblkNelmts); err != nil {
				return err
			}
		}
	}
	p += ndblkAddrs * os

	for j := uint64(0); j < nsblkAddrs; j++ {
		addr := readLayoutAddress(buf[p+j*os:], int(w.sb.OffsetSize), w.sb.Endianness)
		if err := w.superBlock(addr, h.iblkNsblks+j); err != nil {
			return err
		}
	}

	return nil
}

// earraySuperBlockSize returns the bytes of a super block's page bitmaps and
// of the whole block: prefix, header address and block offset, ndblks bitmaps
// of initSize bytes and ndblks data block addresses. ndblks <= 2^32 and
// initSize <= 2^61: both terms are bounded by the file before any read, so
// the products are checked.
func earraySuperBlockSize(ndblks, initSize, os, arrOffSize uint64) (bitmapBytes, size uint64, err error) {
	bitmapBytes, err = utils.SafeMultiply(ndblks, initSize)
	var addrBytes uint64
	if err == nil {
		addrBytes, err = utils.SafeMultiply(ndblks, os)
	}
	size = arrayPrefixSize + os + arrOffSize
	if err == nil {
		size, err = utils.SafeAdd(size, bitmapBytes)
	}
	if err == nil {
		size, err = utils.SafeAdd(size, addrBytes)
	}

	return bitmapBytes, size, err //nolint:wrapcheck // the caller wraps it with the block's index
}

// start returns base + k × n, the first array index of a block or page, and
// whether it is below max_idx_set. C never reads an index at or above it
// (H5EA_get returns the fill value there), and the blocks are walked in index
// order, so a false answer ends the walk of the enclosing block. Overflow is
// past every index.
func (w *earrayWalk) start(base, k, n uint64) (uint64, bool) {
	off, err := utils.SafeMultiply(k, n)
	if err == nil {
		off, err = utils.SafeAdd(base, off)
	}
	return off, err == nil && off < w.hdr.maxIdxSet
}

// superBlock reads super block u:
//
//	"EASB" version(1) class(1) header_addr(sizeof_addr) block_off(arr_off_size)
//	page_init[ndblks × ((npages+7)/8)]   when its data blocks are paged
//	dblk_addrs[ndblks] checksum(4)
//
// (H5EA__cache_sblock_deserialize, H5EAcache.c:1085-1150; paging from
// H5EA__sblock_alloc, H5EAsblock.c:120-146).
func (w *earrayWalk) superBlock(addr, u uint64) error {
	h := w.hdr
	s := h.sblks[u]
	base, ok := w.start(h.idxBlkElmts, 1, s.startIdx)
	if addr == undefinedAddress || !ok {
		return nil
	}
	if err := w.visit(addr, "super block"); err != nil {
		return err
	}

	os := uint64(w.sb.OffsetSize)
	var npages, initSize uint64
	if s.dblkNelmts > h.pageElmts {
		npages = s.dblkNelmts / h.pageElmts // Both powers of two (H5EAsblock.c:127-133).
		initSize = (npages + 7) / 8
	}

	bitmapBytes, size, err := earraySuperBlockSize(s.ndblks, initSize, os, h.arrOffSize)
	if err != nil {
		return fmt.Errorf("%w: extensible array super block %d size overflows", utils.ErrCorrupt, u)
	}

	buf, err := readChecksummedBlock(w.r, w.sb, addr, size, "extensible array super block")
	if err != nil {
		return err
	}
	if err := checkBlockPrefix(buf, earraySuperBlockSig, h.class, h.addr, w.sb, "extensible array super block"); err != nil {
		return err
	}

	p := arrayPrefixSize - 4 + os + h.arrOffSize
	bitmap := buf[p : p+bitmapBytes]
	p += bitmapBytes

	for k := uint64(0); k < s.ndblks; k++ {
		dblk := readLayoutAddress(buf[p+k*os:], int(w.sb.OffsetSize), w.sb.Endianness)
		first, ok := w.start(base, k, s.dblkNelmts)
		if !ok {
			return nil
		}
		if npages == 0 {
			if err := w.dataBlock(dblk, first, s.dblkNelmts); err != nil {
				return err
			}
			continue
		}
		if err := w.pagedDataBlock(dblk, first, k, npages, bitmap); err != nil {
			return err
		}
	}

	return nil
}

// dataBlock reads an unpaged data block of n records whose first has index
// first:
//
//	"EADB" version(1) class(1) header_addr(sizeof_addr) block_off(arr_off_size)
//	records[n] checksum(4)
//
// (H5EA__cache_dblock_deserialize, H5EAcache.c:1495-1560).
func (w *earrayWalk) dataBlock(addr, first, n uint64) error {
	if addr == undefinedAddress {
		return nil
	}
	if err := w.visit(addr, "data block"); err != nil {
		return err
	}

	h := w.hdr
	os := uint64(w.sb.OffsetSize)
	recs, err := utils.SafeMultiply(n, h.rawSize)
	if err != nil {
		return fmt.Errorf("%w: extensible array data block of %d records overflows", utils.ErrCorrupt, n)
	}
	buf, err := readChecksummedBlock(w.r, w.sb, addr, arrayPrefixSize+os+h.arrOffSize+recs, "extensible array data block")
	if err != nil {
		return err
	}
	if err := checkBlockPrefix(buf, earrayDataBlockSig, h.class, h.addr, w.sb, "extensible array data block"); err != nil {
		return err
	}

	return w.addRecords(buf[arrayPrefixSize-4+os+h.arrOffSize:], first, n)
}

// pagedDataBlock reads the written pages of paged data block k of a super
// block. As in C, the block's prefix is not read: its pages are found from
// its address (H5EA.c:516-560). Page i of block k is written when bit
// k × npages + i of the super block's bitmap is set (H5EA.c:527).
func (w *earrayWalk) pagedDataBlock(addr, first, k, npages uint64, bitmap []byte) error {
	if addr == undefinedAddress {
		return nil
	}
	if err := w.visit(addr, "data block"); err != nil {
		return err
	}

	h := w.hdr
	prefix := arrayPrefixSize + uint64(w.sb.OffsetSize) + h.arrOffSize // H5EA_DBLOCK_PREFIX_SIZE.
	for i := uint64(0); i < npages; i++ {
		start, ok := w.start(first, i, h.pageElmts)
		if !ok {
			return nil
		}
		// The bitmap holds ndblks × ((npages+7)/8) bytes, so bit k × npages + i
		// is always inside it.
		if !pageInitBit(bitmap, k*npages+i) {
			continue
		}

		pageAddr, pageBytes, err := arrayPageRegion(addr, prefix, i, h.pageElmts, h.pageElmts, h.rawSize)
		if err != nil {
			return fmt.Errorf("%w: extensible array page %d: %w", utils.ErrCorrupt, i, err)
		}
		page, err := readChecksummedBlock(w.r, w.sb, pageAddr, pageBytes, "extensible array data block page")
		if err != nil {
			return err
		}
		if err := w.addRecords(page, start, h.pageElmts); err != nil {
			return err
		}
	}

	return nil
}

// addRecords adds the records of raw, the first with array index first,
// skipping undefined addresses and indexes at or above max_idx_set.
func (w *earrayWalk) addRecords(raw []byte, first, n uint64) error {
	if first >= w.hdr.maxIdxSet {
		return nil
	}

	return w.s.addArrayRecords(raw, first, min(n, w.hdr.maxIdxSet-first), w.codec, w.lin)
}
