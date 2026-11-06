package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// LocalHeap represents an HDF5 local heap for storing short strings.
// Used by symbol tables to store object names.
//
// Format (HDF5 specification, H5HLcache.c:140-205 and :535-600):
//
//	Prefix (H5HL_SIZEOF_HDR: 32 bytes for 8-byte sizes and addresses):
//	  - Signature: "HEAP" (4 bytes)
//	  - Version: 0 (1 byte)
//	  - Reserved: 0 (3 bytes)
//	  - Data segment size (size_t)
//	  - Offset to head of free list (size_t, H5HL_FREE_NULL = 1 when empty)
//	  - Data segment address (address_t)
//	Data segment (anywhere in the file; right after the prefix when created):
//	  - Objects (null-terminated names) at 8-byte aligned offsets
//	  - Free blocks, each starting with the offset of the next free block and
//	    its own size (H5HL__fl_serialize, H5HLcache.c:289-315)
//
// The writer follows the C library: names are inserted into the free list
// (H5HL_insert, H5HL.c:527-707), and when the data segment must grow only the
// segment moves (H5HL__dblk_realloc, H5HLdblk.c:163-257); the prefix keeps its
// address, so nothing that points at the heap changes.
type LocalHeap struct {
	// Data is the data segment image (C dblk_image), DataSegmentSize bytes.
	Data []byte
	// FreeList is the free-list head offset as read from the prefix.
	FreeList uint64
	// HeaderSize is the size of the prefix (H5HL_SIZEOF_HDR).
	HeaderSize uint64

	DataSegmentSize      uint64 // Size of the data segment
	OffsetToHeadFreeList uint64 // Free-list head written to the prefix
	DataSegmentAddress   uint64 // File address of the data segment

	sizeofSize int                  // H5F_SIZEOF_SIZE
	sizeofAddr int                  // H5F_SIZEOF_ADDR
	free       []localHeapFreeBlock // C heap->freelist, head first
	dblkPlaced bool                 // DataSegmentAddress is a file address
}

// localHeapFreeBlock is one entry of the heap's free list (H5HL_free_t).
type localHeapFreeBlock struct {
	offset uint64
	size   uint64
}

// ErrLocalHeapFull is returned by AddString when no free block of the data
// segment can hold the name: the segment must grow (GrowAndAddString).
var ErrLocalHeapFull = errors.New("local heap is full")

// localHeapAlign is H5HL_ALIGN (H5HLprivate.h:29): heap objects and free
// blocks sit at 8-byte boundaries.
func localHeapAlign(x uint64) uint64 {
	return (x + 7) &^ 7
}

// sizeofFree is H5HL_SIZEOF_FREE (H5HLprivate.h:31-33): the smallest free
// block, which holds the next-block offset and the block size.
func (h *LocalHeap) sizeofFree() uint64 {
	return localHeapAlign(uint64(2 * h.sizeofSize)) //nolint:gosec // G115: sizeofSize is 2, 4 or 8
}

// PrefixSize is H5HL_SIZEOF_HDR (H5HLpkg.h:45-51): the size of the heap
// prefix in the file.
func (h *LocalHeap) PrefixSize() uint64 {
	return localHeapAlign(uint64(8 + 2*h.sizeofSize + h.sizeofAddr)) //nolint:gosec // G115: small sizes
}

// LoadLocalHeap loads a local heap from the specified file address: the
// prefix, the data segment and its free list (H5HL__hdr_deserialize and
// H5HL__fl_deserialize, H5HLcache.c:147-280).
func LoadLocalHeap(r io.ReaderAt, address uint64, sb *core.Superblock) (*LocalHeap, error) {
	// Calculate header size based on offset/length sizes
	// Format: Signature(4) + Version(1) + Reserved(3) + DataSegmentSize(lengthSize) +
	//         FreeListOffset(lengthSize) + DataSegmentAddress(offsetSize)
	headerSize := 8 + int(sb.LengthSize)*2 + int(sb.OffsetSize)

	headerBuf := utils.GetBuffer(headerSize)
	defer utils.ReleaseBuffer(headerBuf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(headerBuf, int64(address)); err != nil {
		return nil, utils.WrapError("local heap header read failed", err)
	}

	if string(headerBuf[0:4]) != "HEAP" {
		return nil, errors.New("invalid local heap signature")
	}

	// Parse header fields using file's endianness
	pos := 8 // After signature, version, reserved

	// Data segment size (lengthSize bytes)
	dataSegmentSize := decodeHeapLength(headerBuf[pos:], int(sb.LengthSize), sb.Endianness)
	pos += int(sb.LengthSize)

	// Free list head (lengthSize bytes): H5HL_FREE_NULL when the segment has no
	// free blocks, otherwise an offset inside the data segment.
	freeListHead := decodeHeapLength(headerBuf[pos:], int(sb.LengthSize), sb.Endianness)
	pos += int(sb.LengthSize)

	// Data segment address (offsetSize bytes); all ones is the undefined
	// address at any width (H5Fint.c:3046-3050).
	dataSegmentAddr := core.DecodeAddress(headerBuf[pos:], int(sb.OffsetSize), sb.Endianness)

	if err := validateLocalHeapHeader(freeListHead, dataSegmentSize, dataSegmentAddr); err != nil {
		return nil, err
	}

	heap := &LocalHeap{
		FreeList:             freeListHead,
		OffsetToHeadFreeList: freeListHead,
		DataSegmentSize:      dataSegmentSize,
		DataSegmentAddress:   dataSegmentAddr,
		sizeofSize:           int(sb.LengthSize),
		sizeofAddr:           int(sb.OffsetSize),
		dblkPlaced:           true,
	}
	heap.HeaderSize = heap.PrefixSize()

	// The data segment size comes straight from the file, so the read goes
	// through core.ReadBlock, which validates the size against the reader and
	// applies the hard cap before allocating. The cap has no C analog: C
	// survives a corrupt size because malloc returns NULL, while a failed Go
	// allocation kills the process (ADR-003, "Documented deviation").
	data, err := core.ReadBlock(r, sb, dataSegmentAddr, dataSegmentSize, utils.MaxChunkSize, "local heap data")
	if err != nil {
		return nil, utils.WrapError("local heap data read failed", err)
	}
	heap.Data = data

	if err := heap.parseFreeList(sb.Endianness); err != nil {
		return nil, err
	}

	return heap, nil
}

// decodeHeapLength decodes a size_t field of width n (2, 4 or 8 bytes).
func decodeHeapLength(b []byte, n int, order binary.ByteOrder) uint64 {
	switch n {
	case 2:
		return uint64(order.Uint16(b[:2]))
	case 4:
		return uint64(order.Uint32(b[:4]))
	case 8:
		return order.Uint64(b[:8])
	}
	return 0
}

// parseFreeList builds the free list from the data segment as
// H5HL__fl_deserialize does (H5HLcache.c:217-280): every block must hold its
// two fields inside the segment (:239-240), a next offset of zero is invalid
// (:250-251), and a block must end inside the segment (:255-256). The C loop
// has no bound; a cyclic list would grow its node list without end, so the
// walk here stops after the most blocks the segment can hold.
func (h *LocalHeap) parseFreeList(order binary.ByteOrder) error {
	h.free = nil
	fields := uint64(2 * h.sizeofSize) //nolint:gosec // G115: sizeofSize is 2, 4 or 8
	maxBlocks := h.DataSegmentSize/fields + 1
	for next := h.FreeList; next != h5hlFreeNull; {
		if uint64(len(h.free)) >= maxBlocks {
			return fmt.Errorf("%w: local heap free list does not end (cycle)", utils.ErrCorrupt)
		}
		if next > math.MaxUint64-fields || next+fields > h.DataSegmentSize {
			return fmt.Errorf("%w: local heap free block at %d outside data segment (size %d)",
				utils.ErrCorrupt, next, h.DataSegmentSize)
		}
		block := localHeapFreeBlock{offset: next}
		next = decodeHeapLength(h.Data[block.offset:], h.sizeofSize, order)
		if next == 0 {
			return fmt.Errorf("%w: local heap free block at %d links to offset 0", utils.ErrCorrupt, block.offset)
		}
		block.size = decodeHeapLength(h.Data[block.offset+uint64(h.sizeofSize):], h.sizeofSize, order) //nolint:gosec // G115: small size
		if block.size > h.DataSegmentSize-block.offset {
			return fmt.Errorf("%w: local heap free block at %d of size %d outside data segment (size %d)",
				utils.ErrCorrupt, block.offset, block.size, h.DataSegmentSize)
		}
		h.free = append(h.free, block)
	}
	return nil
}

// validateLocalHeapHeader enforces the two header invariants of
// H5HL__hdr_deserialize: a free-list head other than H5HL_FREE_NULL must point
// inside the data segment (H5HLcache.c:189-190), and the data block address
// must be defined (H5HLcache.c:200-201). ReadBlock rejects the same sentinel
// address, but this error names which structure is corrupt.
func validateLocalHeapHeader(freeListHead, dataSegmentSize, dataSegmentAddr uint64) error {
	if freeListHead != h5hlFreeNull && freeListHead >= dataSegmentSize {
		return fmt.Errorf("invalid local heap: free list head %d outside data segment (size %d)",
			freeListHead, dataSegmentSize)
	}

	if dataSegmentAddr == undefinedAddress {
		return errors.New("invalid local heap: undefined data segment address")
	}

	return nil
}

// h5hlFreeNull is H5HL_FREE_NULL (H5HLpkg.h:54): the free-list head value that
// means the data segment has no free blocks.
const h5hlFreeNull uint64 = 1

// undefinedAddress is HDF5's undefined-address sentinel: HADDR_UNDEF, the
// 64-bit all-ones value that the C check H5_addr_defined rejects.
const undefinedAddress uint64 = 0xFFFFFFFFFFFFFFFF

// GetString retrieves a null-terminated string from the heap at the given offset.
// The offset is relative to the start of the data segment (after the 32-byte header).
func (h *LocalHeap) GetString(offset uint64) (string, error) {
	if offset >= uint64(len(h.Data)) {
		return "", errors.New("offset beyond heap data")
	}

	end := offset
	for end < uint64(len(h.Data)) && h.Data[end] != 0 {
		end++
	}

	if end >= uint64(len(h.Data)) {
		return "", errors.New("string not null-terminated")
	}

	return string(h.Data[offset:end]), nil
}

// --- Write Support Functions ---

// NewLocalHeap creates the local heap of a new group, as H5HL_create
// (H5HL.c:99-177) followed by the insert of the empty name at offset 0
// (H5Gstab.c:144): the size hint is raised to H5HL_SIZEOF_FREE and aligned
// to 8 bytes, the whole data segment starts as one free block, and the empty
// name then takes the first 8 bytes.
//
// The heap uses 8-byte sizes and addresses, the only ones the writer
// creates. A heap read from a file keeps the file's sizes (LoadLocalHeap).
func NewLocalHeap(initialSize uint64) *LocalHeap {
	heap := &LocalHeap{
		OffsetToHeadFreeList: h5hlFreeNull,
		sizeofSize:           8,
		sizeofAddr:           8,
	}
	heap.HeaderSize = heap.PrefixSize()

	if initialSize < heap.sizeofFree() {
		initialSize = heap.sizeofFree()
	}
	initialSize = localHeapAlign(initialSize)

	heap.DataSegmentSize = initialSize
	heap.Data = make([]byte, initialSize)
	heap.free = []localHeapFreeBlock{{offset: 0, size: initialSize}}

	// A segment too small to split leaves no room for the empty name
	// (H5HL_insert keeps a split remainder of at least H5HL_SIZEOF_FREE), so
	// it grows as H5HL_insert grows it; the segment is not in the file yet.
	if _, err := heap.AddString(""); err != nil {
		heap.GrowAndAddString("")
	}

	return heap
}

// AddString stores s with its null terminator in a free block of the data
// segment and returns its offset, as the search loop of H5HL_insert does
// (H5HL.c:557-590): the size is aligned to 8 bytes (H5HL_ALIGN), and a block
// is used when it fits exactly or leaves at least H5HL_SIZEOF_FREE bytes.
// When no block qualifies it returns ErrLocalHeapFull and changes nothing;
// the caller then grows the segment with GrowAndAddString.
//
// Thread safety: Not thread-safe, caller must synchronize.
func (h *LocalHeap) AddString(s string) (offset uint64, err error) {
	need := localHeapAlign(uint64(len(s)) + 1)
	for i, fl := range h.free {
		switch {
		case fl.size > need && fl.size-need >= h.sizeofFree():
			offset = fl.offset
			h.free[i].offset += need
			h.free[i].size -= need
		case fl.size == need:
			offset = fl.offset
			h.free = append(h.free[:i], h.free[i+1:]...)
		default:
			continue
		}
		h.putObject(offset, need, s)
		return offset, nil
	}
	return 0, ErrLocalHeapFull
}

// GrowAndAddString enlarges the data segment and stores s in the new space,
// as the not-found branch of H5HL_insert does (H5HL.c:592-700): the segment
// at least doubles; a free block that ends the segment is extended and used,
// otherwise s goes at the old end and the rest becomes a new free block at
// the head of the list. It returns the offset of s and the segment size
// before the growth.
//
// Only the in-memory image changes. The caller places the larger segment in
// the file: in place when the space after it can be claimed (H5MF_try_extend,
// H5HL.c:617-640), otherwise at a new address recorded with
// SetDataSegmentAddress after the old segment is freed (H5HL__dblk_realloc,
// H5HLdblk.c:163-257). A segment not yet placed simply becomes larger.
func (h *LocalHeap) GrowAndAddString(s string) (offset, oldSize uint64) {
	need := localHeapAlign(uint64(len(s)) + 1)
	oldSize = h.DataSegmentSize

	// The free block closest to the end of the segment (H5HL.c:584-587).
	last := -1
	for i, fl := range h.free {
		if last < 0 || h.free[last].offset < fl.offset {
			last = i
		}
	}
	lastAtEnd := last >= 0 && h.free[last].offset+h.free[last].size == oldSize

	needMore := max(need, oldSize)
	if !lastAtEnd && needMore < need+h.sizeofFree() {
		needMore = need
	}
	newSize := oldSize + needMore

	grown := make([]byte, newSize)
	copy(grown, h.Data)
	h.Data = grown
	h.DataSegmentSize = newSize

	if lastAtEnd {
		offset = h.free[last].offset
		h.free[last].offset += need
		h.free[last].size += needMore - need
		if h.free[last].size < h.sizeofFree() {
			h.free = append(h.free[:last], h.free[last+1:]...)
		}
	} else {
		offset = oldSize
		if needMore-need >= h.sizeofFree() {
			h.free = append([]localHeapFreeBlock{{offset: oldSize + need, size: needMore - need}}, h.free...)
		}
	}

	h.putObject(offset, need, s)
	return offset, oldSize
}

// Remove returns the object of size bytes at offset to the free list, as
// H5HL_remove does (H5HL.c:719-830): the size is aligned to 8 bytes, a block
// that touches an existing free block on either side is merged with it (and
// with a second neighbor the merge then touches), and a block smaller than
// H5HL_SIZEOF_FREE that touches nothing is not tracked (H5HL.c:806-807).
//
// Deviation: C then shrinks the data segment when a free block ending it
// covers more than half of it (H5HL__minimize_heap_space, H5HL.c:190-330).
// The segment keeps its size here; a heap with a large trailing free block is
// valid and its space is reused by the next insert.
func (h *LocalHeap) Remove(offset, size uint64) error {
	size = localHeapAlign(size)
	if size == 0 || offset > h.DataSegmentSize || size > h.DataSegmentSize-offset {
		return fmt.Errorf("local heap object at %d size %d outside data segment (size %d): %w",
			offset, size, h.DataSegmentSize, utils.ErrCorrupt)
	}

	for i := range h.free {
		fl := &h.free[i]
		switch {
		case offset+size == fl.offset:
			// The new block ends where fl starts (H5HL.c:744-768).
			fl.offset = offset
			fl.size += size
			for j := i + 1; j < len(h.free); j++ {
				if h.free[j].offset+h.free[j].size == fl.offset {
					fl.offset = h.free[j].offset
					fl.size += h.free[j].size
					h.free = append(h.free[:j], h.free[j+1:]...)
					break
				}
			}
			return nil
		case fl.offset+fl.size == offset:
			// The new block starts where fl ends (H5HL.c:769-796).
			fl.size += size
			for j := i + 1; j < len(h.free); j++ {
				if fl.offset+fl.size == h.free[j].offset {
					fl.size += h.free[j].size
					h.free = append(h.free[:j], h.free[j+1:]...)
					break
				}
			}
			return nil
		}
	}

	// No neighbor: a block too small to hold the free-block fields is lost
	// to the heap until the segment is rebuilt, as in C.
	if size < h.sizeofFree() {
		return nil
	}
	h.free = append([]localHeapFreeBlock{{offset: offset, size: size}}, h.free...)
	return nil
}

// putObject writes s and its terminator at offset and clears the rest of the
// aligned slot, which may still hold the fields of a free block.
func (h *LocalHeap) putObject(offset, slot uint64, s string) {
	dst := h.Data[offset : offset+slot]
	n := copy(dst, s)
	clear(dst[n:])
}

// SetDataSegmentAddress records the file address of a data segment that
// moved (H5HLdblk.c:190).
func (h *LocalHeap) SetDataSegmentAddress(addr uint64) {
	h.DataSegmentAddress = addr
	h.dblkPlaced = true
}

// WriteTo writes the heap prefix at address and the data segment at its own
// address, as H5HL__cache_prefix_serialize and H5HL__cache_datablock_serialize
// do (H5HLcache.c:535-600). The free list is first encoded into the data
// segment (H5HL__fl_serialize, H5HLcache.c:289-315) and its head into the
// prefix (:563). A segment that was never placed goes right after the prefix,
// as H5HL_create allocates it (H5HL.c:123-129).
//
// Parameters:
//   - w: Writer to write to (must support WriteAt)
//   - address: Address of the heap prefix
//
// Returns:
//   - error: If write fails
func (h *LocalHeap) WriteTo(w io.WriterAt, address uint64) error {
	prefixSize := h.PrefixSize()
	if !h.dblkPlaced {
		h.SetDataSegmentAddress(address + prefixSize)
	}

	// Free list into the segment image, head offset into the prefix.
	h.OffsetToHeadFreeList = h5hlFreeNull
	if len(h.free) > 0 {
		h.OffsetToHeadFreeList = h.free[0].offset
	}
	for i, fl := range h.free {
		next := h5hlFreeNull
		if i+1 < len(h.free) {
			next = h.free[i+1].offset
		}
		putHeapLength(h.Data[fl.offset:], h.sizeofSize, next)
		putHeapLength(h.Data[fl.offset+uint64(h.sizeofSize):], h.sizeofSize, fl.size) //nolint:gosec // G115: small size
	}

	// The prefix is padded with zeros to H5HL_SIZEOF_HDR (H5HLcache.c:574-584).
	prefix := make([]byte, prefixSize)
	copy(prefix, "HEAP") // Signature; version 0 and the reserved bytes stay zero.
	pos := 8
	putHeapLength(prefix[pos:], h.sizeofSize, h.DataSegmentSize)
	pos += h.sizeofSize
	putHeapLength(prefix[pos:], h.sizeofSize, h.OffsetToHeadFreeList)
	pos += h.sizeofSize
	putHeapLength(prefix[pos:], h.sizeofAddr, h.DataSegmentAddress)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	if _, err := w.WriteAt(prefix, int64(address)); err != nil {
		return utils.WrapError("failed to write local heap header", err)
	}

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	if _, err := w.WriteAt(h.Data, int64(h.DataSegmentAddress)); err != nil {
		return utils.WrapError("failed to write local heap data", err)
	}

	return nil
}

// putHeapLength encodes v as a little-endian field of width n (2, 4 or 8):
// the file format stores all metadata little-endian.
func putHeapLength(b []byte, n int, v uint64) {
	var full [8]byte
	binary.LittleEndian.PutUint64(full[:], v)
	copy(b[:n], full[:n])
}

// Size returns the size of the heap when its data segment follows the prefix
// (prefix + data segment), the space H5HL_create allocates (H5HL.c:122).
func (h *LocalHeap) Size() uint64 {
	return h.PrefixSize() + h.DataSegmentSize
}

// Contiguous reports whether the data segment directly follows the prefix
// at prefixAddr, which C calls a single cache object (H5HLcache.c,
// H5HL__cache_prefix_deserialize). Such a heap is one block in the file.
func (h *LocalHeap) Contiguous(prefixAddr uint64) bool {
	return h.DataSegmentAddress == prefixAddr+h.PrefixSize()
}
