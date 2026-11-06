package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// External File List storage (message 0x0007, H5Oefl.c; I/O in H5Defl.c): the
// raw data of a contiguous dataset lives in one or more files outside the
// HDF5 file. The message names the files through a local heap and gives, for
// each, where its part of the data starts and how many bytes it holds; the
// parts are concatenated in slot order to form the dataset's storage.

const (
	// eflVersion is H5O_EFL_VERSION (H5Oefl.c:58), the only version.
	eflVersion = 1

	// ExternalUnlimited is a slot size meaning "the rest of the data":
	// H5O_EFL_UNLIMITED = H5F_UNLIMITED (H5Oprivate.h:380, H5Fpublic.h:106).
	ExternalUnlimited uint64 = math.MaxUint64

	// localHeapVersion is H5HL_VERSION (H5HLcache.c:44).
	localHeapVersion = 0

	// localHeapFreeNull is H5HL_FREE_NULL (H5HLpkg.h:54).
	localHeapFreeNull uint64 = 1
)

// ExternalFileSlot is one entry of an External File List.
type ExternalFileSlot struct {
	// Name is the external file's name as stored in the file. It is untrusted
	// input; the opener decides how, and whether, it maps to a real file.
	Name string

	// Offset is where this slot's bytes start inside the external file.
	Offset uint64

	// Size is the number of the dataset's bytes the file holds from Offset,
	// or ExternalUnlimited for all remaining bytes.
	Size uint64
}

// ExternalFileList is a decoded External File List message: the slots in use,
// in the order their bytes form the dataset's storage.
type ExternalFileList struct {
	HeapAddress uint64             // Local heap holding the names.
	Allocated   uint16             // Slots allocated in the message (nalloc).
	Slots       []ExternalFileSlot // Slots in use (nused), never empty for a dataset.
}

// ExternalFile is an open external raw data file.
type ExternalFile interface {
	io.ReaderAt
	io.Closer
}

// ExternalOpener opens the external raw data file a slot names, read-only.
// It owns the policy that maps an untrusted name to a file; a nil opener
// refuses every name.
type ExternalOpener func(name string) (ExternalFile, error)

// ParseExternalFileList decodes an External File List message as
// H5O__efl_decode does (H5Oefl.c:66-197), with the same checks:
//   - the version is 1 and the number of allocated slots is positive;
//   - the slots in use do not exceed the slots allocated;
//   - the heap address is defined, and the heap is a valid local heap;
//   - every name offset lies inside the heap's data block and names a
//     non-empty string; the name runs to its NUL or to the end of the block,
//     as H5MM_strndup copies it.
//
// Violations wrap utils.ErrCorrupt. Only the slots in use are decoded, as in C.
func ParseExternalFileList(r io.ReaderAt, sb *Superblock, data []byte) (*ExternalFileList, error) {
	if sb == nil {
		return nil, errors.New("external file list: superblock is required")
	}

	c := &eflCursor{data: data}
	version, err := c.uint(1)
	if err != nil {
		return nil, err
	}
	if version != eflVersion {
		return nil, fmt.Errorf("%w: bad version number %d for external file list message (H5Oefl.c:87-88)",
			utils.ErrCorrupt, version)
	}
	if _, err := c.uint(3); err != nil { // Reserved.
		return nil, err
	}

	nalloc, err := c.uint(2)
	if err != nil {
		return nil, err
	}
	if nalloc == 0 {
		return nil, fmt.Errorf("%w: bad number of allocated slots when parsing efl msg (H5Oefl.c:98-99)",
			utils.ErrCorrupt)
	}
	nused, err := c.uint(2)
	if err != nil {
		return nil, err
	}
	if nused > nalloc {
		return nil, fmt.Errorf("%w: bad number of in-use slots when parsing efl msg: %d of %d (H5Oefl.c:103-104)",
			utils.ErrCorrupt, nused, nalloc)
	}

	heapAddr, err := c.address(int(sb.OffsetSize))
	if err != nil {
		return nil, err
	}
	if heapAddr == undefinedAddress {
		return nil, fmt.Errorf("%w: bad local heap address when parsing efl msg (H5Oefl.c:110-111)", utils.ErrCorrupt)
	}

	// Each slot takes three length fields; refusing a count the message
	// cannot hold keeps the allocation below bounded by the message size.
	slotBytes := 3 * uint64(sb.LengthSize)
	//nolint:gosec // G115: the cursor never passes the end, so the difference is non-negative.
	if uint64(len(c.data)-c.off) < nused*slotBytes {
		return nil, c.overflow(int(nused * slotBytes)) //nolint:gosec // G115: at most 65535 × 24.
	}

	heap, err := readLocalHeapBlock(r, sb, heapAddr)
	if err != nil {
		return nil, fmt.Errorf("external file list heap at 0x%X: %w", heapAddr, err)
	}

	slots, err := decodeEFLSlots(c, heap, nused, int(sb.LengthSize))
	if err != nil {
		return nil, err
	}

	return &ExternalFileList{
		HeapAddress: heapAddr,
		Allocated:   uint16(nalloc), //nolint:gosec // G115: decoded from 2 bytes.
		Slots:       slots,
	}, nil
}

// decodeEFLSlots decodes the nused slots of an External File List: a name
// offset into the heap block, a file offset and a size, each length bytes
// (H5Oefl.c:146-178). The caller checked that the message holds them.
func decodeEFLSlots(c *eflCursor, heap []byte, nused uint64, length int) ([]ExternalFileSlot, error) {
	slots := make([]ExternalFileSlot, nused)
	for i := range slots {
		nameOff, err := c.uint(length)
		if err != nil {
			return nil, err
		}
		name, err := externalName(heap, nameOff)
		if err != nil {
			return nil, fmt.Errorf("external file %d: %w", i, err)
		}
		offset, err := c.uint(length)
		if err != nil {
			return nil, err
		}
		size, err := c.uint(length)
		if err != nil {
			return nil, err
		}
		slots[i] = ExternalFileSlot{Name: name, Offset: offset, Size: size}
	}

	return slots, nil
}

// externalName returns the name at off in a local heap data block, as
// H5O__efl_decode reads it: H5HL_offset_into refuses an offset at or past the
// block size (H5HL.c:391-392), an empty name is invalid (H5Oefl.c:146-147),
// and H5MM_strndup copies up to the NUL or the end of the block.
func externalName(heap []byte, off uint64) (string, error) {
	if off >= uint64(len(heap)) {
		return "", fmt.Errorf("%w: name offset %d is outside the %d-byte local heap (H5HL.c:391-392)",
			utils.ErrCorrupt, off, len(heap))
	}

	s := heap[off:]
	for i, b := range s {
		if b == 0 {
			s = s[:i]
			break
		}
	}
	if len(s) == 0 {
		return "", fmt.Errorf("%w: invalid external file name: empty (H5Oefl.c:146-147)", utils.ErrCorrupt)
	}

	return string(s), nil
}

// TotalSize is H5O_efl_total_size (H5Oefl.c:405-430): the bytes the slots
// hold together, ExternalUnlimited when the last slot is unlimited, and
// utils.ErrCorrupt when the sum overflows.
func (l *ExternalFileList) TotalSize() (uint64, error) {
	if n := len(l.Slots); n > 0 && l.Slots[n-1].Size == ExternalUnlimited {
		return ExternalUnlimited, nil
	}

	var total uint64
	for _, s := range l.Slots {
		next, err := utils.SafeAdd(total, s.Size)
		if err != nil {
			return 0, fmt.Errorf("%w: total external storage size overflowed (H5Oefl.c:419-421)", utils.ErrCorrupt)
		}
		total = next
	}

	return total, nil
}

// Reader returns the dataset's external storage as one io.ReaderAt: offset 0
// is the first byte of the first slot. Each read opens the files it needs
// through open and closes them before returning, as H5D__efl_read does.
func (l *ExternalFileList) Reader(open ExternalOpener) io.ReaderAt {
	return &externalReader{list: l, open: open}
}

type externalReader struct {
	list *ExternalFileList
	open ExternalOpener
}

// ReadAt reads len(p) bytes of storage at off, following H5D__efl_read
// (H5Defl.c:277-402):
//   - the first slot is the one whose range holds off, or the first unlimited
//     slot;
//   - a read past the last slot is an error ("read past logical end of
//     file"), as is a file position that does not fit in an off_t;
//   - bytes past the end of an external file read as zeros, as C fills them
//     (H5Defl.c:376-381).
//
// It returns len(p) or an error, never a short count.
func (e *externalReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("external storage: negative offset %d", off)
	}

	slots := e.list.Slots
	addr := uint64(off)
	u, cur := 0, uint64(0)
	var skip uint64
	for ; u < len(slots); u++ {
		if slots[u].Size == ExternalUnlimited || addr-cur < slots[u].Size {
			skip = addr - cur
			break
		}
		cur += slots[u].Size // No overflow: TotalSize was checked by the descriptor.
	}

	done := 0
	for done < len(p) {
		if u >= len(slots) {
			return done, fmt.Errorf("%w: external storage: read past logical end of file (H5Defl.c:313-314)",
				utils.ErrCorrupt)
		}
		slot := slots[u]
		pos, err := utils.SafeAdd(slot.Offset, skip)
		if err != nil || pos > math.MaxInt64 {
			return done, fmt.Errorf("%w: external file %q: address overflowed (H5Defl.c:315-316)",
				utils.ErrCorrupt, slot.Name)
		}
		n := uint64(len(p) - done)
		if slot.Size != ExternalUnlimited {
			n = min(n, slot.Size-skip)
		}

		if err := e.readSlot(slot.Name, p[done:done+int(n)], int64(pos)); err != nil { //nolint:gosec // G115: n ≤ len(p); pos ≤ MaxInt64.
			return done, err
		}

		done += int(n) //nolint:gosec // G115: n ≤ len(p).
		skip = 0
		u++
	}

	return done, nil
}

// readSlot fills dst from one external file at pos; past its end, zeros.
func (e *externalReader) readSlot(name string, dst []byte, pos int64) (err error) {
	if e.open == nil {
		return fmt.Errorf("external file %q: no external file access for this file: %w", name, utils.ErrNotSupported)
	}

	f, err := e.open(name)
	if err != nil {
		return fmt.Errorf("external file %q: %w", name, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("external file %q: close: %w", name, cerr)
		}
	}()

	n, rerr := f.ReadAt(dst, pos)
	if rerr != nil && !errors.Is(rerr, io.EOF) {
		return fmt.Errorf("external file %q: read error in external raw data file: %w", name, rerr)
	}
	clear(dst[n:]) // H5Defl.c:376-381: end of file on disk reads as zeros.

	return nil
}

// readLocalHeapBlock reads the data block of the local heap at addr with the
// header checks of H5HL__hdr_deserialize (H5HLcache.c:140-205): signature,
// version 0, a free-list head inside the block, a defined block address. The
// block is read through ReadBlock.
//
// internal/structures.LoadLocalHeap reads the same structure for groups, but
// structures imports core, so core cannot call it.
func readLocalHeapBlock(r io.ReaderAt, sb *Superblock, addr uint64) ([]byte, error) {
	prefixSize := 8 + 2*uint64(sb.LengthSize) + uint64(sb.OffsetSize)
	prefix, err := ReadBlock(r, sb, addr, prefixSize, prefixSize, "local heap header")
	if err != nil {
		return nil, err
	}

	if string(prefix[:4]) != "HEAP" {
		return nil, fmt.Errorf("%w: bad local heap signature (H5HLcache.c:161-162)", utils.ErrCorrupt)
	}
	if prefix[4] != localHeapVersion {
		return nil, fmt.Errorf("%w: wrong version number %d in local heap (H5HLcache.c:168-169)",
			utils.ErrCorrupt, prefix[4])
	}

	c := &eflCursor{data: prefix, off: 8}
	length := int(sb.LengthSize)
	size, err := c.uint(length)
	if err != nil {
		return nil, err
	}
	free, err := c.uint(length)
	if err != nil {
		return nil, err
	}
	if free != localHeapFreeNull && free >= size {
		return nil, fmt.Errorf("%w: bad heap free list (H5HLcache.c:189-190)", utils.ErrCorrupt)
	}
	dataAddr, err := c.address(int(sb.OffsetSize))
	if err != nil {
		return nil, err
	}
	if dataAddr == undefinedAddress {
		return nil, fmt.Errorf("%w: bad datablock address (H5HLcache.c:200-201)", utils.ErrCorrupt)
	}

	return ReadBlock(r, sb, dataAddr, size, utils.MaxChunkSize, "local heap data")
}

// eflCursor reads sized little-endian fields with C's bounds check
// (H5_IS_BUFFER_OVERFLOW before every field).
type eflCursor struct {
	data []byte
	off  int
}

func (c *eflCursor) overflow(n int) error {
	return fmt.Errorf("%w: ran off end of input buffer while decoding: %d bytes at %d of %d",
		utils.ErrCorrupt, n, c.off, len(c.data))
}

// uint decodes an n-byte unsigned field in the file's byte order.
func (c *eflCursor) uint(n int) (uint64, error) {
	if n <= 0 || n > 8 || len(c.data)-c.off < n {
		return 0, c.overflow(n)
	}
	var buf [8]byte
	copy(buf[:], c.data[c.off:c.off+n])
	c.off += n

	return uint64(buf[0]) | uint64(buf[1])<<8 | uint64(buf[2])<<16 | uint64(buf[3])<<24 |
		uint64(buf[4])<<32 | uint64(buf[5])<<40 | uint64(buf[6])<<48 | uint64(buf[7])<<56, nil
}

// address decodes an address field; all ones is the undefined address at any
// width (H5F_addr_decode_len).
func (c *eflCursor) address(n int) (uint64, error) {
	if n <= 0 || n > 8 || len(c.data)-c.off < n {
		return 0, c.overflow(n)
	}
	v := readLayoutAddress(c.data[c.off:c.off+n], n, binary.LittleEndian)
	c.off += n

	return v, nil
}
