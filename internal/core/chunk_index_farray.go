package core

import (
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// Fixed array signatures and classes (H5FApkg.h, H5FAprivate.h).
const (
	farrayHeaderSig    = "FAHD"
	farrayDataBlockSig = "FADB"

	arrayClassChunk         = 0 // H5FA_CLS_CHUNK_ID / H5EA_CLS_CHUNK_ID.
	arrayClassFilteredChunk = 1 // H5FA_CLS_FILT_CHUNK_ID / H5EA_CLS_FILT_CHUNK_ID.

	// arrayPrefixSize is signature, version, class and checksum, the
	// H5FA_METADATA_PREFIX_SIZE(true) / H5EA_METADATA_PREFIX_SIZE(true) every
	// block carries (H5FApkg.h:48-53, H5EApkg.h).
	arrayPrefixSize = 4 + 1 + 1 + 4

	// maxPageBits keeps 1 << bits representable. C shifts a size_t by the
	// stored value (H5FAdblock.c:109, H5EAhdr.c:136), which is undefined from
	// 64 on, so no valid file has a larger value.
	maxPageBits = 63
)

// farrayHeader is the decoded fixed array header (H5FA__cache_hdr_deserialize,
// H5FAcache.c).
type farrayHeader struct {
	addr      uint64
	class     uint8
	rawSize   uint64
	pageBits  uint8
	nelmts    uint64
	dblkAddr  uint64
	pageElmts uint64
}

// openFArray opens the fixed array index (H5Dfarray.c, H5FA*.c). The header
// names a data block holding one record per chunk of the maximum grid, in
// row-major order (H5D__farray_idx_get_addr, H5Dfarray.c:1105-1106). A data
// block with more records than a page holds is paged: a bitmap in the block
// marks which pages exist, and each page carries its own checksum
// (H5FAdblock.c:109-131, H5FA.c:330-380).
//
// Checks, as C makes them while loading each block (H5FAcache.c):
// signature, version 0, class, owning header address, checksum.
//
// Deviations, stricter than C, which trusts these values from the dataset's
// own creation (H5Dfarray.c:838-850):
//   - the header's class matches the dataset's filter pipeline;
//   - the record size is the one the dataset implies;
//   - the element count is the number of chunks in the maximum grid;
//   - every maximum dimension is fixed.
func (d *DatasetDescriptor) openFArray(r io.ReaderAt, sb *Superblock) (chunkIndexOps, error) {
	maxChunks, unlim, err := d.maxChunkGrid()
	if err != nil {
		return chunkIndexOps{}, err
	}
	if unlim != -1 {
		return chunkIndexOps{}, fmt.Errorf("%w: fixed array chunk index on a dataspace with an unlimited dimension", utils.ErrCorrupt)
	}

	lin, err := newChunkLinearizer(maxChunks, 0)
	if err != nil {
		return chunkIndexOps{}, err
	}

	if d.layout.DataAddress == undefinedAddress {
		return unallocatedIndex(), nil
	}

	codec := newChunkElemCodec(d, sb)
	hdr, err := readFArrayHeader(r, sb, d.layout.DataAddress, codec)
	if err != nil {
		return chunkIndexOps{}, err
	}

	if hdr.nelmts != lin.total {
		return chunkIndexOps{}, fmt.Errorf("%w: fixed array at 0x%X has %d elements, the chunk grid needs %d",
			utils.ErrCorrupt, hdr.addr, hdr.nelmts, lin.total)
	}

	if hdr.dblkAddr == undefinedAddress {
		// No chunk was written; the header is allocated all the same.
		return chunkIndexOps{
			lookup:  noChunks,
			iterate: func(s *chunkSink) error { return s.indexBlock(hdr.addr, farrayHeaderSize(sb)) },
		}, nil
	}

	fa, err := openFArrayDataBlock(r, sb, hdr, codec, newBlockBudget(r, sb))
	if err != nil {
		return chunkIndexOps{}, err
	}

	lookup := func(scaled []uint64) (uint64, uint64, uint32, error) {
		raw, err := fa.record(lin.index(scaled))
		if err != nil || raw == nil {
			return undefinedAddress, 0, 0, err
		}
		addr, nbytes, mask := codec.decode(raw)

		return addr, nbytes, mask, nil
	}

	return chunkIndexOps{
		lookup: lookup,
		iterate: func(s *chunkSink) error {
			if err := fa.reportBlocks(s); err != nil {
				return err
			}

			return fa.iterate(s, codec, lin)
		},
	}, nil
}

// farrayHeaderSize is H5FA_HEADER_SIZE (H5FApkg.h:56-70): prefix, element
// size, page bits, element count and data block address.
func farrayHeaderSize(sb *Superblock) uint64 {
	return arrayPrefixSize + 2 + uint64(sb.LengthSize) + uint64(sb.OffsetSize)
}

// readFArrayHeader decodes and checks the header at addr:
//
//	"FAHD" version(1) class(1) raw_elmt_size(1) page_bits(1)
//	nelmts(sizeof_size) dblk_addr(sizeof_addr) checksum(4)
func readFArrayHeader(r io.ReaderAt, sb *Superblock, addr uint64, codec chunkElemCodec) (*farrayHeader, error) {
	ls, os := int(sb.LengthSize), int(sb.OffsetSize)

	buf, err := readChecksummedBlock(r, sb, addr, farrayHeaderSize(sb), "fixed array header")
	if err != nil {
		return nil, err
	}

	if string(buf[:4]) != farrayHeaderSig {
		return nil, fmt.Errorf("%w: fixed array header at 0x%X: wrong signature %q", utils.ErrCorrupt, addr, buf[:4])
	}
	if buf[4] != 0 {
		return nil, fmt.Errorf("%w: fixed array header at 0x%X: unsupported version %d", utils.ErrCorrupt, addr, buf[4])
	}

	hdr := &farrayHeader{addr: addr, class: buf[5], rawSize: uint64(buf[6]), pageBits: buf[7]}
	hdr.nelmts = readUint64(buf[8:], ls, sb.Endianness)
	hdr.dblkAddr = readLayoutAddress(buf[8+ls:], os, sb.Endianness)

	if err := checkArrayClass(hdr.class, hdr.rawSize, codec, "fixed array", addr); err != nil {
		return nil, err
	}
	if hdr.pageBits == 0 || hdr.pageBits > maxPageBits {
		return nil, fmt.Errorf("%w: fixed array header at 0x%X: page bits %d out of range", utils.ErrCorrupt, addr, hdr.pageBits)
	}
	hdr.pageElmts = uint64(1) << hdr.pageBits

	return hdr, nil
}

// checkArrayClass checks a fixed or extensible array's class and record size
// against the dataset (H5Dfarray.c:838-846, H5Dearray.c:879-887).
func checkArrayClass(class uint8, rawSize uint64, codec chunkElemCodec, what string, addr uint64) error {
	want := uint8(arrayClassChunk)
	if codec.filtered {
		want = arrayClassFilteredChunk
	}
	if class != want {
		return fmt.Errorf("%w: %s at 0x%X has class %d, the dataset's filter pipeline needs %d",
			utils.ErrCorrupt, what, addr, class, want)
	}
	if rawSize != codec.rawSize() {
		return fmt.Errorf("%w: %s at 0x%X has %d-byte records, the dataset needs %d",
			utils.ErrCorrupt, what, addr, rawSize, codec.rawSize())
	}

	return nil
}

// farrayDataBlock is an opened fixed array data block. An unpaged block keeps
// its records; a paged one keeps its page bitmap and the page read last. The
// readers look chunks up in increasing array index, so each page is read once.
type farrayDataBlock struct {
	r       io.ReaderAt
	sb      *Superblock
	hdr     *farrayHeader
	recSize uint64
	records []byte // Unpaged: every record.
	bitmap  []byte // Paged: one bit per page, most significant first.
	npages  uint64
	prefix  uint64
	page    []byte // Paged: the records of page pageIdx.
	pageIdx uint64
	budget  *blockBudget // Bytes of the data block and of every page read.
}

// openFArrayDataBlock reads and checks the data block:
//
//	"FADB" version(1) class(1) header_addr(sizeof_addr)
//	page_init[(npages+7)/8]   paged only
//	records[nelmts]           not paged only
//	checksum(4)
//	pages: records[page_nelmts] checksum(4) each, the last one short
func openFArrayDataBlock(r io.ReaderAt, sb *Superblock, hdr *farrayHeader, codec chunkElemCodec, budget *blockBudget) (*farrayDataBlock, error) {
	os := uint64(sb.OffsetSize)
	fa := &farrayDataBlock{r: r, sb: sb, hdr: hdr, recSize: codec.rawSize(), pageIdx: math.MaxUint64, budget: budget}
	paged := hdr.nelmts > hdr.pageElmts

	var initSize uint64
	if paged {
		fa.npages = (hdr.nelmts-1)/hdr.pageElmts + 1 // Overflow-free ceil.
		initSize = (fa.npages + 7) / 8
	}

	fa.prefix = arrayPrefixSize + os + initSize
	size := fa.prefix
	if !paged {
		recs, err := utils.SafeMultiply(hdr.nelmts, hdr.rawSize)
		if err == nil {
			size, err = utils.SafeAdd(size, recs)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: fixed array data block of %d records overflows", utils.ErrCorrupt, hdr.nelmts)
		}
	}

	if err := budget.charge(size, hdr.dblkAddr, "fixed array"); err != nil {
		return nil, err
	}
	buf, err := readChecksummedBlock(r, sb, hdr.dblkAddr, size, "fixed array data block")
	if err != nil {
		return nil, err
	}
	if err := checkBlockPrefix(buf, farrayDataBlockSig, hdr.class, hdr.addr, sb, "fixed array data block"); err != nil {
		return nil, err
	}

	body := buf[arrayPrefixSize-4+os : len(buf)-4]
	if paged {
		fa.bitmap = body
	} else {
		fa.records = body
	}

	return fa, nil
}

// record returns the raw record of array index idx, or nil when its page was
// never written (every chunk on it reads as the fill value, H5FA_get, H5FA.c:450-454).
func (fa *farrayDataBlock) record(idx uint64) ([]byte, error) {
	if fa.records != nil {
		return fa.records[idx*fa.recSize:], nil
	}

	hdr := fa.hdr
	p := idx / hdr.pageElmts
	if !pageInitBit(fa.bitmap, p) {
		return nil, nil
	}

	if p != fa.pageIdx {
		if err := fa.loadPage(p); err != nil {
			return nil, err
		}
	}

	return fa.page[(idx-p*hdr.pageElmts)*fa.recSize:], nil
}

// pageRecords is the number of records on page p; the last page is short.
func (fa *farrayDataBlock) pageRecords(p uint64) uint64 {
	if p == fa.npages-1 {
		return fa.hdr.nelmts - p*fa.hdr.pageElmts
	}

	return fa.hdr.pageElmts
}

// loadPage reads and checks page p, which becomes the page kept.
func (fa *farrayDataBlock) loadPage(p uint64) error {
	hdr := fa.hdr

	// Pages follow the prefix at a stride of H5FA_DBLK_PAGE_SIZE
	// (H5FApkg.h:101-105, H5FA.c:350-351); the last page is short.
	addr, pageBytes, err := arrayPageRegion(hdr.dblkAddr, fa.prefix, p, hdr.pageElmts, fa.pageRecords(p), hdr.rawSize)
	if err != nil {
		return fmt.Errorf("%w: fixed array page %d: %w", utils.ErrCorrupt, p, err)
	}
	// Pages lie at a fixed stride and are read once each (the lookups and
	// iterate go in increasing index), so they cannot overlap; the charge
	// keeps every read of the walk under the one budget all the same.
	if err := fa.budget.charge(pageBytes, addr, "fixed array"); err != nil {
		return err
	}
	page, err := readChecksummedBlock(fa.r, fa.sb, addr, pageBytes, "fixed array data block page")
	if err != nil {
		return err
	}
	fa.page, fa.pageIdx = page, p

	return nil
}

// iterate passes every written record to the sink in array index order, as
// H5D__farray_idx_iterate does through H5FA_iterate (H5Dfarray.c:1251-1305,
// H5FA.c:658-700). C visits every one of the nelmts elements and gets the
// fill value for those on unwritten pages; here a page whose bit is clear is
// skipped whole, so the work is bounded by the bytes of the data block and
// its written pages, all in the file, never by the element count. The
// records of an index are each listed once by construction.
func (fa *farrayDataBlock) iterate(s *chunkSink, codec chunkElemCodec, lin *chunkLinearizer) error {
	hdr := fa.hdr
	if fa.records != nil {
		return s.addArrayRecords(fa.records, 0, hdr.nelmts, codec, lin)
	}

	for p := uint64(0); p < fa.npages; p++ {
		if err := s.tick(); err != nil {
			return err
		}
		if !pageInitBit(fa.bitmap, p) {
			continue
		}

		raw, err := fa.record(p * hdr.pageElmts) // Reads page p.
		if err != nil {
			return err
		}
		if err := s.addArrayRecords(raw, p*hdr.pageElmts, fa.pageRecords(p), codec, lin); err != nil {
			return err
		}
	}

	return nil
}

// reportBlocks reports the header and the data block to the sink's Block
// hook, each of the size C allocated: the header H5FA_HEADER_SIZE
// (H5FAhdr.c:203), the data block H5FA_DBLOCK_SIZE, its prefix with the page
// bitmap, every record, and one checksum per page when paged (H5FApkg.h:79-99,
// H5FAdblock.c:189-192), whether or not a page was ever written. H5FA_delete
// frees both (H5FAhdr.c:236, H5FAdblock.c:235).
func (fa *farrayDataBlock) reportBlocks(s *chunkSink) error {
	if s.block == nil {
		return nil
	}
	if err := s.indexBlock(fa.hdr.addr, farrayHeaderSize(fa.sb)); err != nil {
		return err
	}

	size, err := utils.SafeMultiply(fa.hdr.nelmts, fa.hdr.rawSize)
	if err == nil {
		size, err = utils.SafeAdd(size, fa.prefix+4*fa.npages)
	}
	if err != nil {
		return fmt.Errorf("%w: fixed array data block of %d records overflows", utils.ErrCorrupt, fa.hdr.nelmts)
	}

	return s.indexBlock(fa.hdr.dblkAddr, size)
}

// arrayPageRegion returns the address and size of page p of a paged fixed or
// extensible array data block: base + prefix + p × (pageElmts × rawSize + 4),
// holding n records and a checksum. Overflow is an error.
func arrayPageRegion(base, prefix, p, pageElmts, n, rawSize uint64) (addr, size uint64, err error) {
	stride, err := utils.SafeMultiply(pageElmts, rawSize)
	if err == nil {
		stride, err = utils.SafeAdd(stride, 4)
	}
	if err == nil {
		stride, err = utils.SafeMultiply(stride, p)
	}
	if err == nil {
		addr, err = utils.SafeAdd(base, prefix)
	}
	if err == nil {
		addr, err = utils.SafeAdd(addr, stride)
	}
	if err == nil {
		size, err = utils.SafeMultiply(n, rawSize)
	}
	if err == nil {
		size, err = utils.SafeAdd(size, 4)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("page region overflows: %w", err)
	}

	return addr, size, nil
}
