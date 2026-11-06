package core

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"

	"github.com/scigolib/hdf5/internal/utils"
)

// Fractal heap format constants (H5HFpkg.h, H5HFhdr.c, H5HFtiny.c).
const (
	fheapHeaderSig       = "FRHP"
	fheapIBlockSig       = "FHIB"
	fheapDBlockSig       = "FHDB"
	fheapVersion         = 0
	fheapChecksumSize    = 4
	fheapFlagChecksumDBs = 0x02 // H5HF_HDR_FLAGS_CHECKSUM_DBLOCKS, H5HFpkg.h:61

	fheapIDVersionMask = 0xC0 // H5HF_ID_VERS_MASK, H5HFpkg.h:139
	fheapIDTypeMask    = 0x30 // H5HF_ID_TYPE_MASK, H5HFpkg.h:145
	fheapIDTypeManaged = 0x00
	fheapIDTypeHuge    = 0x10
	fheapIDTypeTiny    = 0x20

	fheapTinyLenShort = 16   // H5HF_TINY_LEN_SHORT, H5HFtiny.c:41
	fheapTinyMaskLen  = 0x0F // H5HF_TINY_MASK_SHORT, H5HFtiny.c:42

	fheapWidthLimit     = 64 * 1024 // H5HF_WIDTH_LIMIT, H5HFhdr.c:55
	fheapMaxDirectLimit = 2 << 30   // H5HF_MAX_DIRECT_SIZE_LIMIT, H5HFhdr.c:49

	// fheapPrefixSize is signature and version (H5HF_METADATA_PREFIX_SIZE
	// without the checksum, H5HFpkg.h:42-46).
	fheapPrefixSize = 5
)

// FractalHeap is an opened fractal heap (H5HF_hdr_t): its header, decoded and
// checked once, and the doubling table derived from it. Object reads the
// objects its heap IDs name, managed, huge and tiny, as H5HF_read does
// (H5HF.c:492-540).
//
// Every address, size and row count comes from the file and is checked
// before it is used (ADR-003): the header and every indirect and direct block
// are read through ReadBlock and their checksums verified, block offsets must
// be the ones the doubling table gives, and a descent through nested indirect
// blocks must lose rows at every level, so it ends after at most 64 levels.
// The blocks a FractalHeap has read are remembered; together they may not
// claim more bytes than the file holds.
//
// Legacy heaps: this library up to v0.14.2 wrote heaps whose managed object
// offsets count from the first byte after the direct block header, where C
// counts from the first byte of the block (H5HFman.c:388-399). Those heaps
// store 0 as the address of the huge object B-tree and of the free space
// manager (structures.NewWritableFractalHeap, v0.14.2), which C never writes:
// it creates both as the undefined address (H5HFhdr.c, H5HF__hdr_create) and
// address 0 is the superblock. Such a heap is read with the old offsets.
//
// A FractalHeap is not safe for concurrent use.
type FractalHeap struct {
	r    io.ReaderAt
	sb   *Superblock
	addr uint64

	idLen           uint64
	checksumDBlocks bool
	maxManSize      uint64
	manSize         uint64
	width           uint64
	startBlock      uint64
	maxDirect       uint64
	rootAddr        uint64
	currRootRows    uint64

	offSize, lenSize int
	firstRowBits     int
	maxDirectRows    uint64
	numIDFirstRow    uint64
	rowBlockSize     []uint64
	rowBlockOff      []uint64
	dblockOverhead   uint64
	hugeIDsDirect    bool
	tinyExtended     bool
	tinyMaxLen       uint64
	legacy           bool

	limit   uint64      // readable end of the file, 0 when unknown
	budget  blockBudget // bytes of the blocks read so far, at most limit
	iblocks map[uint64]*fheapIBlock
	dblocks map[uint64]fheapDBlock
}

// fheapIBlock is a decoded indirect block (H5HF_indirect_t): its row count,
// its offset in the heap's address space and its child addresses.
type fheapIBlock struct {
	nrows    uint64
	blockOff uint64
	ents     []uint64
}

// fheapDBlock is a direct block whose header was checked.
type fheapDBlock struct {
	size, blockOff uint64
}

// fheapFields reads the little-endian fields of a fractal heap block whose
// size was computed from the header, so every field lies inside it.
type fheapFields struct {
	b []byte
	p int
}

func (f *fheapFields) uint(n int) uint64 {
	v := fheapUint(f.b[f.p : f.p+n])
	f.p += n

	return v
}

// addr decodes an address; all ones is the undefined address at any width
// (H5F_addr_decode_len, H5Fint.c:3009-3049).
func (f *fheapFields) addr(n int) uint64 {
	v := readLayoutAddress(f.b[f.p:f.p+n], n, binary.LittleEndian)
	f.p += n

	return v
}

// fheapUint decodes an unsigned little-endian integer of 1 to 8 bytes
// (UINT64DECODE_VAR).
func fheapUint(b []byte) uint64 {
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}

	return v
}

// log2Gen is H5VM_log2_gen: the index of the highest set bit, 0 for 0.
func log2Gen(v uint64) int {
	if v == 0 {
		return 0
	}

	return bits.Len64(v) - 1
}

// ReadFractalHeapObject reads the object heapID names in the fractal heap
// whose header is at heapAddr. To read several objects of one heap, open it
// once with OpenFractalHeap.
func ReadFractalHeapObject(r io.ReaderAt, sb *Superblock, heapAddr uint64, heapID []byte) ([]byte, error) {
	h, err := OpenFractalHeap(r, sb, heapAddr)
	if err != nil {
		return nil, err
	}

	return h.Object(heapID)
}

// OpenFractalHeap reads and checks the fractal heap header at addr
// (H5HF__cache_hdr_deserialize, H5HFcache.c:445-560) and derives the doubling
// table (H5HF__hdr_finish_init_phase1, H5HFhdr.c:182-204). A heap with I/O
// filters is utils.ErrNotSupported: its direct blocks and huge objects are
// compressed.
func OpenFractalHeap(r io.ReaderAt, sb *Superblock, addr uint64) (*FractalHeap, error) {
	buf, err := readFractalHeapHeaderBlock(r, sb, addr)
	if err != nil {
		return nil, err
	}

	limit := dataLimit(r, sb)
	h := &FractalHeap{
		r: r, sb: sb, addr: addr, limit: limit, budget: blockBudget{limit: limit},
		iblocks: map[uint64]*fheapIBlock{}, dblocks: map[uint64]fheapDBlock{},
	}
	if err := h.decodeHeader(buf); err != nil {
		return nil, fmt.Errorf("fractal heap at 0x%X: %w", addr, err)
	}
	h.initTable()

	return h, nil
}

// readFractalHeapHeaderBlock reads the header block, filter information
// included, and verifies its signature, version and checksum
// (H5HFcache.c:206-211, :545-560).
func readFractalHeapHeaderBlock(r io.ReaderAt, sb *Superblock, addr uint64) ([]byte, error) {
	l, o := uint64(sb.LengthSize), uint64(sb.OffsetSize)
	// H5HF_HEADER_SIZE (H5HFpkg.h:67-100): prefix, flags(1), max_man_size(4),
	// two lengths and two addresses, eight lengths of statistics, and the
	// doubling table (2+l+l+2+2+o+2), then the checksum.
	size := fheapPrefixSize + 2 + 2 + 1 + 4 + 2*l + 2*o + 8*l + 2 + 2*l + 2 + 2 + o + 2 + fheapChecksumSize
	buf, err := ReadBlock(r, sb, addr, size, size, "fractal heap header")
	if err != nil {
		return nil, err
	}
	if string(buf[:4]) != fheapHeaderSig {
		return nil, fmt.Errorf("%w: wrong fractal heap header signature %q at 0x%X (H5HFcache.c:206)",
			utils.ErrCorrupt, buf[:4], addr)
	}
	if buf[4] != fheapVersion {
		return nil, fmt.Errorf("%w: wrong fractal heap header version %d at 0x%X (H5HFcache.c:211)",
			utils.ErrCorrupt, buf[4], addr)
	}
	if filterLen := uint64(binary.LittleEndian.Uint16(buf[7:9])); filterLen > 0 {
		// The filtered root direct block's size and mask, then the
		// pipeline (H5HFcache.c:509-532).
		size += l + 4 + filterLen
		if buf, err = ReadBlock(r, sb, addr, size, size, "fractal heap header"); err != nil {
			return nil, err
		}
	}
	if err := verifyFheapChecksum(sb, buf, addr, "header"); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint16(buf[7:9]) > 0 {
		return nil, fmt.Errorf("%w: fractal heap at 0x%X has I/O filters", utils.ErrNotSupported, addr)
	}

	return buf, nil
}

// verifyFheapChecksum checks the checksum in the last four bytes of a heap
// header or indirect block (H5F_get_checksums; H5HFcache.c:416-425, :877-881).
// Both are structures hdf5-go up to v0.13.4 wrote with CRC32-IEEE: such a
// checksum is accepted and recorded as a WarnLegacyMetadataChecksum warning on
// sb (VerifyMetadataChecksumLegacy); C accepts only lookup3.
func verifyFheapChecksum(sb *Superblock, buf []byte, addr uint64, what string) error {
	n := len(buf) - fheapChecksumSize
	stored := binary.LittleEndian.Uint32(buf[n:])
	legacy, err := VerifyMetadataChecksumLegacy(buf[:n], stored, "fractal heap "+what)
	if err != nil {
		return fmt.Errorf("%w: fractal heap %s at 0x%X has checksum 0x%08X, computed 0x%08X",
			utils.ErrCorrupt, what, addr, stored, JenkinsChecksum(buf[:n]))
	}
	if legacy {
		sb.warnLegacyChecksumf(WarnLegacyMetadataChecksum, addr, uint64(n), legacyMetadataChecksumMessage, "fractal heap "+what) //nolint:gosec // n >= 0
	}

	return nil
}

// decodeHeader decodes the header fields in C's order (H5HFcache.c:470-505,
// H5HF__dtable_decode).
func (h *FractalHeap) decodeHeader(buf []byte) error {
	l, o := int(h.sb.LengthSize), int(h.sb.OffsetSize)
	f := &fheapFields{b: buf, p: fheapPrefixSize}

	h.idLen = f.uint(2)
	f.uint(2) // filter_len, zero (readFractalHeapHeaderBlock)
	h.checksumDBlocks = f.uint(1)&fheapFlagChecksumDBs != 0
	h.maxManSize = f.uint(4)
	f.uint(l) // huge_next_id
	hugeBT2Addr := f.addr(o)
	f.uint(l) // total_man_free
	fsAddr := f.addr(o)
	h.manSize = f.uint(l)
	f.p += 7 * l // man_alloc_size, man_iter_off, man_nobjs, huge and tiny statistics
	h.width = f.uint(2)
	h.startBlock = f.uint(l)
	h.maxDirect = f.uint(l)
	maxIndex := f.uint(2)
	f.uint(2) // start_root_rows
	h.rootAddr = f.addr(o)
	h.currRootRows = f.uint(2)

	h.legacy = hugeBT2Addr == 0 && fsAddr == 0
	if h.legacy {
		h.sb.Warnf(WarnLegacyDenseLayout, h.addr, legacyDenseLayoutMessage,
			"fractal heap", h.addr, "object offsets counted from the end of the block prefix")
	}

	//nolint:gosec // G115: maxIndex is a 2-byte field
	offSize, err := FractalHeapOffsetSize(uint16(maxIndex), h.sb.LengthSize)
	if err != nil {
		return fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}
	h.offSize = int(offSize)
	if err := h.checkParams(); err != nil {
		return err
	}
	h.firstRowBits = log2Gen(h.startBlock) + log2Gen(h.width)
	// max_root_rows = max_index - first_row_bits + 1 (H5HFdtable.c:97), 0 for
	// a heap whose only block is its root direct block: this library up to
	// v0.14.2 wrote 16-bit heaps with 32 KiB blocks in 4 columns, 17 bits.
	if maxIndex+1 < uint64(h.firstRowBits) { //nolint:gosec // G115: at most 31 + 16
		return fmt.Errorf("%w: maximum heap size of %d bits is below the first row's %d bits",
			utils.ErrCorrupt, maxIndex, h.firstRowBits)
	}
	maxRootRows := maxIndex + 1 - uint64(h.firstRowBits) //nolint:gosec // G115: as above
	if h.currRootRows > maxRootRows {
		return fmt.Errorf("%w: root indirect block has %d rows, the heap at most %d",
			utils.ErrCorrupt, h.currRootRows, maxRootRows)
	}
	h.rowBlockSize = make([]uint64, maxRootRows) // maxRootRows <= 64
	h.rowBlockOff = make([]uint64, maxRootRows)

	return nil
}

// checkParams checks the creation parameters a header carries. C checks them
// only when it creates a heap (H5HF__hdr_create, H5HFhdr.c:318-345) and
// trusts a header it reads; here a corrupt value would size a table or a
// read, so it is utils.ErrCorrupt (ADR-003).
func (h *FractalHeap) checkParams() error {
	switch {
	case h.idLen == 0:
		return fmt.Errorf("%w: heap ID length is zero", utils.ErrCorrupt)
	case h.width == 0 || h.width > fheapWidthLimit || bits.OnesCount64(h.width) != 1:
		return fmt.Errorf("%w: doubling table width %d is not a power of two up to %d (H5HFhdr.c:318-321)",
			utils.ErrCorrupt, h.width, fheapWidthLimit)
	case h.startBlock == 0 || bits.OnesCount64(h.startBlock) != 1:
		return fmt.Errorf("%w: starting block size %d is not a power of two (H5HFhdr.c:322-325)",
			utils.ErrCorrupt, h.startBlock)
	case h.maxDirect == 0 || h.maxDirect > fheapMaxDirectLimit || bits.OnesCount64(h.maxDirect) != 1:
		return fmt.Errorf("%w: maximum direct block size %d is not a power of two up to %d (H5HFhdr.c:326-331)",
			utils.ErrCorrupt, h.maxDirect, uint64(fheapMaxDirectLimit))
	case h.maxDirect < h.startBlock:
		return fmt.Errorf("%w: maximum direct block size %d is below the starting block size %d",
			utils.ErrCorrupt, h.maxDirect, h.startBlock)
	case h.maxDirect < h.maxManSize:
		return fmt.Errorf("%w: maximum direct block size %d is below the maximum managed object size %d "+
			"(H5HFhdr.c:332-334)", utils.ErrCorrupt, h.maxDirect, h.maxManSize)
	}

	return nil
}

// initTable derives the doubling table (H5HF__dtable_init, H5HFdtable.c:75-120),
// the heap ID field widths (H5HFhdr.c:194-200), the 'huge' and 'tiny' ID
// forms (H5HFhuge.c:175-205, H5HFtiny.c:98-110) and the direct block header
// size (H5HF_MAN_ABS_DIRECT_OVERHEAD, H5HFpkg.h:102-110).
func (h *FractalHeap) initTable() {
	startBits := log2Gen(h.startBlock)
	maxDirectBits := log2Gen(h.maxDirect)
	h.maxDirectRows = uint64(maxDirectBits-startBits) + 2 //nolint:gosec // G115: maxDirect >= startBlock
	h.numIDFirstRow = h.startBlock * h.width              // < 2^47: both are checked
	blockSize, blockOff := h.startBlock, h.numIDFirstRow
	if len(h.rowBlockSize) > 0 {
		h.rowBlockSize[0] = h.startBlock
	}
	for u := 1; u < len(h.rowBlockSize); u++ {
		// The last row starts at 2^(maxIndex-1), which fits: maxIndex <= 64.
		h.rowBlockSize[u], h.rowBlockOff[u] = blockSize, blockOff
		blockSize *= 2
		blockOff *= 2
	}

	dirOffSize := (maxDirectBits + 7) / 8     // H5HF_SIZEOF_OFFSET_LEN
	manLenSize := log2Gen(h.maxManSize)/8 + 1 // H5VM_limit_enc_size
	h.lenSize = min(dirOffSize, manLenSize)

	o, l := uint64(h.sb.OffsetSize), uint64(h.sb.LengthSize)
	h.hugeIDsDirect = o+l < h.idLen // the flags byte, address and length; unfiltered heap (H5HFhuge.c:193-203)
	switch {
	case h.idLen-1 <= fheapTinyLenShort:
		h.tinyMaxLen = h.idLen - 1
	case h.idLen-1 == fheapTinyLenShort+1:
		h.tinyMaxLen = fheapTinyLenShort
	default:
		h.tinyMaxLen, h.tinyExtended = h.idLen-2, true
	}

	h.dblockOverhead = fheapPrefixSize + o + uint64(h.offSize) //nolint:gosec // G115: offSize <= 8
	if h.checksumDBlocks {
		h.dblockOverhead += fheapChecksumSize
	}
}

// Object returns the object heapID names (H5HF_read, H5HF.c:492-540).
func (h *FractalHeap) Object(heapID []byte) ([]byte, error) {
	if len(heapID) == 0 {
		return nil, fmt.Errorf("%w: empty fractal heap ID", utils.ErrCorrupt)
	}
	flags := heapID[0]
	if flags&fheapIDVersionMask != 0 {
		return nil, fmt.Errorf("%w: incorrect heap ID version %d (H5HF.c:511-512)",
			utils.ErrCorrupt, flags>>6)
	}
	switch flags & fheapIDTypeMask {
	case fheapIDTypeManaged:
		return h.managed(heapID)
	case fheapIDTypeHuge:
		return h.huge(heapID)
	case fheapIDTypeTiny:
		return h.tiny(heapID)
	default:
		// C: "heap ID type not supported yet" (H5HF.c:535); no library
		// writes this type.
		return nil, fmt.Errorf("%w: reserved heap ID type 3", utils.ErrCorrupt)
	}
}

// managed reads a managed object (H5HF__man_op_real, H5HFman.c:300-405).
func (h *FractalHeap) managed(id []byte) ([]byte, error) {
	if need := 1 + h.offSize + h.lenSize; len(id) < need {
		return nil, fmt.Errorf("%w: %d-byte heap ID cannot hold a %d-byte offset and a %d-byte length",
			utils.ErrCorrupt, len(id), h.offSize, h.lenSize)
	}
	off := fheapUint(id[1 : 1+h.offSize])
	length := fheapUint(id[1+h.offSize : 1+h.offSize+h.lenSize])
	if err := h.checkManaged(off, length); err != nil {
		return nil, err
	}

	blockAddr, blockSize, blockOff, err := h.locate(off)
	if err != nil {
		return nil, err
	}
	if err := h.checkDirectBlock(blockAddr, blockSize, blockOff); err != nil {
		return nil, err
	}

	at := off - blockOff // < blockSize: locate chose the block holding off
	if h.legacy {
		at += h.dblockOverhead
	} else if at < h.dblockOverhead {
		return nil, fmt.Errorf("%w: heap object at offset %d is located in the prefix of its direct block "+
			"(H5HFman.c:391-393)", utils.ErrCorrupt, off)
	}
	if at+length > blockSize {
		return nil, fmt.Errorf("%w: heap object at offset %d of %d bytes overruns its %d-byte direct block "+
			"(H5HFman.c:395-397)", utils.ErrCorrupt, off, length, blockSize)
	}

	return ReadBlock(h.r, h.sb, blockAddr+at, length, length, "fractal heap object")
}

// checkManaged applies C's checks of a managed object's offset and length
// (H5HFman.c:321-332). A legacy heap's first object has offset 0.
func (h *FractalHeap) checkManaged(off, length uint64) error {
	switch {
	case off == 0 && !h.legacy:
		return fmt.Errorf("%w: invalid fractal heap offset 0 (H5HFman.c:322-323)", utils.ErrCorrupt)
	case off > h.manSize:
		return fmt.Errorf("%w: fractal heap object offset %d is beyond the managed space of %d bytes "+
			"(H5HFman.c:324-325)", utils.ErrCorrupt, off, h.manSize)
	case length == 0:
		return fmt.Errorf("%w: invalid fractal heap object size 0 (H5HFman.c:326-327)", utils.ErrCorrupt)
	case length > h.maxDirect:
		return fmt.Errorf("%w: fractal heap object of %d bytes is too large for a direct block of %d "+
			"(H5HFman.c:328-329)", utils.ErrCorrupt, length, h.maxDirect)
	case length > h.maxManSize:
		return fmt.Errorf("%w: fractal heap object of %d bytes exceeds the managed maximum %d "+
			"(H5HFman.c:330-331)", utils.ErrCorrupt, length, h.maxManSize)
	}

	return nil
}

// lookup returns the row and column of the block holding heap offset off
// (H5HF__dtable_lookup, H5HFdtable.c:135-160).
func (h *FractalHeap) lookup(off uint64) (row, col uint64, err error) {
	if off < h.numIDFirstRow {
		return 0, off / h.startBlock, nil
	}
	highBit := log2Gen(off)                 // >= firstRowBits: off >= startBlock * width
	r := uint64(highBit-h.firstRowBits) + 1 //nolint:gosec // G115: highBit >= firstRowBits
	if r >= uint64(len(h.rowBlockSize)) {
		return 0, 0, fmt.Errorf("%w: heap offset %d is beyond the heap's %d rows",
			utils.ErrCorrupt, off, len(h.rowBlockSize))
	}

	return r, (off - 1<<highBit) / h.rowBlockSize[r], nil
}

// locate finds the direct block holding heap offset off: its address, size
// and offset in the heap (H5HF__man_dblock_locate, H5HFdblock.c:495-583, and
// H5HFman.c:337-385).
func (h *FractalHeap) locate(off uint64) (addr, size, blockOff uint64, err error) {
	if h.currRootRows == 0 {
		if h.rootAddr == undefinedAddress {
			return 0, 0, 0, fmt.Errorf("%w: fractal heap has no blocks for offset %d", utils.ErrCorrupt, off)
		}
		if off >= h.startBlock {
			return 0, 0, 0, fmt.Errorf("%w: heap offset %d is beyond the root direct block of %d bytes",
				utils.ErrCorrupt, off, h.startBlock)
		}

		return h.rootAddr, h.startBlock, 0, nil
	}

	ib, err := h.indirectBlock(h.rootAddr, h.currRootRows, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	row, col, err := h.lookup(off)
	for err == nil && row >= h.maxDirectRows {
		ib, err = h.childIndirectBlock(ib, row, col)
		if err == nil {
			row, col, err = h.lookup(off - ib.blockOff)
		}
	}
	if err != nil {
		return 0, 0, 0, err
	}
	if row >= ib.nrows {
		return 0, 0, 0, fmt.Errorf("%w: heap offset %d is in row %d of an indirect block of %d rows",
			utils.ErrCorrupt, off, row, ib.nrows)
	}
	addr = ib.ents[row*h.width+col]
	if addr == undefinedAddress {
		return 0, 0, 0, fmt.Errorf("%w: fractal heap ID not in allocated direct block (H5HFman.c:368-374)",
			utils.ErrCorrupt)
	}

	return addr, h.rowBlockSize[row], ib.blockOff + h.rowBlockOff[row] + col*h.rowBlockSize[row], nil
}

// childIndirectBlock descends from ib to the indirect block of row and col
// (H5HFdblock.c:530-575). The child's rows come from its block size
// (H5HFdblock.c:534); a row whose blocks are smaller than the first row cannot
// hold an indirect block, where C's unsigned arithmetic wraps. Otherwise the
// child has row - log2(width) rows, fewer than its parent (row < ib.nrows),
// which C asserts (H5HFdblock.c:537) and which bounds the descent.
func (h *FractalHeap) childIndirectBlock(ib *fheapIBlock, row, col uint64) (*fheapIBlock, error) {
	if row >= ib.nrows {
		return nil, fmt.Errorf("%w: row %d is beyond an indirect block of %d rows", utils.ErrCorrupt, row, ib.nrows)
	}
	blockBits := log2Gen(h.rowBlockSize[row])
	if blockBits < h.firstRowBits {
		return nil, fmt.Errorf("%w: row %d's blocks of %d bytes are too small for an indirect block, "+
			"the first row spans 2^%d bytes", utils.ErrCorrupt, row, h.rowBlockSize[row], h.firstRowBits)
	}
	nrows := uint64(blockBits-h.firstRowBits) + 1 //nolint:gosec // G115: checked above
	addr := ib.ents[row*h.width+col]
	if addr == undefinedAddress {
		return nil, fmt.Errorf("%w: fractal heap ID not in allocated indirect block", utils.ErrCorrupt)
	}

	return h.indirectBlock(addr, nrows, ib.blockOff+h.rowBlockOff[row]+col*h.rowBlockSize[row])
}

// spend counts the bytes of a block about to be read against the file's
// size: the distinct blocks of a heap lie in the file, so together they
// cannot be larger, and a corrupt heap cannot make the reader read more.
// The budget lives as long as the heap; its block caches read each block once.
func (h *FractalHeap) spend(n, addr uint64) error {
	return h.budget.charge(n, addr, "fractal heap")
}

// indirectBlock reads the indirect block at addr with nrows rows, which
// must sit at heap offset blockOff (H5HF__cache_iblock_deserialize,
// H5HFcache.c:904-1050). C does not check the stored block offset; it is
// checked here because the descent subtracts it from the object's offset.
func (h *FractalHeap) indirectBlock(addr, nrows, blockOff uint64) (*fheapIBlock, error) {
	if ib, ok := h.iblocks[addr]; ok {
		if ib.nrows != nrows || ib.blockOff != blockOff {
			return nil, fmt.Errorf("%w: fractal heap indirect block at 0x%X is reached as two blocks",
				utils.ErrCorrupt, addr)
		}

		return ib, nil
	}

	o := int(h.sb.OffsetSize)
	nents := nrows * h.width // nrows <= 64, width <= 2^16
	// H5HF_MAN_INDIRECT_SIZE (H5HFpkg.h:119-129), unfiltered entries.
	size := uint64(fheapPrefixSize+fheapChecksumSize+o+h.offSize) + nents*uint64(o) //nolint:gosec // small
	if err := h.spend(size, addr); err != nil {
		return nil, err
	}
	buf, err := ReadBlock(h.r, h.sb, addr, size, size, "fractal heap indirect block")
	if err != nil {
		return nil, err
	}
	if err := h.checkBlockPrefix(buf, addr, fheapIBlockSig, "indirect"); err != nil {
		return nil, err
	}
	if err := verifyFheapChecksum(h.sb, buf, addr, "indirect block"); err != nil {
		return nil, err
	}

	f := &fheapFields{b: buf, p: fheapPrefixSize + o}
	if got := f.uint(h.offSize); got != blockOff {
		return nil, fmt.Errorf("%w: fractal heap indirect block at 0x%X has heap offset %d, its parent places it at %d",
			utils.ErrCorrupt, addr, got, blockOff)
	}
	ib := &fheapIBlock{nrows: nrows, blockOff: blockOff, ents: make([]uint64, nents)}
	for u := range ib.ents {
		ib.ents[u] = f.addr(o)
	}
	h.iblocks[addr] = ib

	return ib, nil
}

// checkBlockPrefix checks the signature, version and owning heap of an
// indirect or direct block (H5HFcache.c:950-960, :1763-1773).
func (h *FractalHeap) checkBlockPrefix(buf []byte, addr uint64, sig, what string) error {
	if string(buf[:4]) != sig {
		return fmt.Errorf("%w: wrong fractal heap %s block signature %q at 0x%X",
			utils.ErrCorrupt, what, buf[:4], addr)
	}
	if buf[4] != fheapVersion {
		return fmt.Errorf("%w: wrong fractal heap %s block version %d at 0x%X", utils.ErrCorrupt, what, buf[4], addr)
	}
	o := int(h.sb.OffsetSize)
	if heap := readLayoutAddress(buf[fheapPrefixSize:fheapPrefixSize+o], o, binary.LittleEndian); heap != h.addr {
		return fmt.Errorf("%w: fractal heap %s block at 0x%X belongs to the heap at 0x%X, not 0x%X",
			utils.ErrCorrupt, what, addr, heap, h.addr)
	}

	return nil
}

// checkDirectBlock checks the direct block at addr of size bytes, which must
// sit at heap offset blockOff: its header and, when the heap checksums direct
// blocks, the checksum over the whole block with the checksum field zeroed
// (H5HF__cache_dblock_verify_chksum, H5HFcache.c:1594-1608). Without
// checksums only the header is read, and the block must lie in the file.
func (h *FractalHeap) checkDirectBlock(addr, size, blockOff uint64) error {
	if db, ok := h.dblocks[addr]; ok {
		if db.size != size || db.blockOff != blockOff {
			return fmt.Errorf("%w: fractal heap direct block at 0x%X is reached as two blocks",
				utils.ErrCorrupt, addr)
		}

		return nil
	}

	read := h.dblockOverhead
	if h.checksumDBlocks {
		read = size
	}
	if h.dblockOverhead > size {
		return fmt.Errorf("%w: fractal heap direct block of %d bytes is smaller than its %d-byte header",
			utils.ErrCorrupt, size, h.dblockOverhead)
	}
	if err := h.spend(read, addr); err != nil {
		return err
	}
	if end, err := utils.SafeAdd(addr, size); err != nil || (h.limit > 0 && end > h.limit) {
		return fmt.Errorf("%w: fractal heap direct block at 0x%X of %d bytes is beyond the file's %d bytes",
			utils.ErrCorrupt, addr, size, h.limit)
	}
	buf, err := ReadBlock(h.r, h.sb, addr, read, read, "fractal heap direct block")
	if err != nil {
		return err
	}
	if err := h.checkBlockPrefix(buf, addr, fheapDBlockSig, "direct"); err != nil {
		return err
	}
	o := int(h.sb.OffsetSize)
	if got := fheapUint(buf[fheapPrefixSize+o : fheapPrefixSize+o+h.offSize]); got != blockOff {
		return fmt.Errorf("%w: fractal heap direct block at 0x%X has heap offset %d, its parent places it at %d",
			utils.ErrCorrupt, addr, got, blockOff)
	}
	if h.checksumDBlocks {
		at := h.dblockOverhead - fheapChecksumSize
		stored := binary.LittleEndian.Uint32(buf[at:])
		clear(buf[at : at+fheapChecksumSize])
		if computed := JenkinsChecksum(buf); computed != stored {
			return fmt.Errorf("%w: fractal heap direct block at 0x%X has checksum 0x%08X, computed 0x%08X",
				utils.ErrCorrupt, addr, stored, computed)
		}
	}
	h.dblocks[addr] = fheapDBlock{size: size, blockOff: blockOff}

	return nil
}

// huge reads a 'huge' object, stored outside the heap's blocks
// (H5HF__huge_op_real, H5HFhuge.c:478-560). Only IDs that hold the object's
// address and length are read; the others name a record of the v2 B-tree
// that tracks huge objects, which is utils.ErrNotSupported.
func (h *FractalHeap) huge(id []byte) ([]byte, error) {
	if !h.hugeIDsDirect {
		return nil, fmt.Errorf("%w: huge fractal heap objects tracked by a v2 B-tree", utils.ErrNotSupported)
	}
	o, l := int(h.sb.OffsetSize), int(h.sb.LengthSize)
	if len(id) < 1+o+l {
		return nil, fmt.Errorf("%w: %d-byte huge object heap ID cannot hold an address and a length",
			utils.ErrCorrupt, len(id))
	}
	f := &fheapFields{b: id, p: 1}
	addr := f.addr(o)
	length := f.uint(l)

	return ReadBlock(h.r, h.sb, addr, length, utils.MaxChunkSize, "fractal heap huge object")
}

// tiny returns a 'tiny' object, stored in its heap ID (H5HF__tiny_op_real,
// H5HFtiny.c:185-245). The extended length is the low nibble of the flags
// byte and the next byte, as H5HF__tiny_insert encodes it
// (H5HFtiny.c:149-151). Deviation: C decodes the nibble with the mask
// 0x0F00 on a byte (H5HFtiny.c:199), which drops it; both agree for objects
// of up to 256 bytes, and only a heap ID of 258 bytes or more can hold a
// longer one.
func (h *FractalHeap) tiny(id []byte) ([]byte, error) {
	n, at := uint64(id[0]&fheapTinyMaskLen)+1, uint64(1)
	if h.tinyExtended {
		if len(id) < 2 {
			return nil, fmt.Errorf("%w: 1-byte extended tiny object heap ID", utils.ErrCorrupt)
		}
		n, at = (uint64(id[0]&fheapTinyMaskLen)<<8|uint64(id[1]))+1, 2
	}
	if n > h.tinyMaxLen || at+n > uint64(len(id)) {
		return nil, fmt.Errorf("%w: tiny object of %d bytes does not fit its %d-byte heap ID",
			utils.ErrCorrupt, n, len(id))
	}

	return append([]byte(nil), id[at:at+n]...), nil
}
