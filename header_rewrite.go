package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// errHeaderFull reports a message an object header cannot hold at all: its
// data does not fit the 16-bit size field of a message header. C refuses such
// a message the same way (H5O_MESG_MAX_SIZE, H5Opkg.h); a compact attribute
// then goes to dense storage instead (H5O__attr_create, H5Oattribute.c).
var errHeaderFull = errors.New("object header full")

// Sizes of the object header layout (H5Opkg.h:30-139, H5Ocache.c:1012-1140).
const (
	ohdrV2FixedPrefix  = 6  // "OHDR", version, flags
	ohdrV2TimesSize    = 16 // access, modification, change, birth times
	ohdrV2PhaseSize    = 4  // max compact, min dense
	ohdrV1PrefixSize   = 16 // version 1 prefix, padded to 8 bytes
	ohdrChecksumSize   = 4
	ohdrV1MsgHeader    = 8      // type(2), size(2), flags(1), reserved(3)
	ohdrMinChunkData   = 22     // H5O_MIN_SIZE, H5Opkg.h:30
	ohdrV2ChunkMagic   = "OCHK" // continuation chunk signature (H5O_CHK_MAGIC)
	ohdrFlagTimes      = 0x20   // H5O_HDR_STORE_TIMES
	ohdrFlagPhase      = 0x10   // H5O_HDR_ATTR_STORE_PHASE_CHANGE
	ohdrFlagCrtTracked = 0x04   // H5O_HDR_ATTR_CRT_ORDER_TRACKED
	ohdrFlagChunk0Size = 0x03   // H5O_HDR_CHUNK0_SIZE
	maxNilDataSize     = 0xFFFF // the 16-bit message size field
)

// rewriteObjectHeader writes the object header at addr back with the
// messages of oh, which the caller read with core.ReadObjectHeader and then
// changed: data replaced, messages removed or appended. It is the one funnel
// through which this package changes the message list of an existing header,
// version 1 or 2.
//
// The header keeps its chunks. A message whose data keeps its size is
// rewritten where it is (H5O_msg_write). A removed message becomes free space
// and the header is condensed as H5O__condense_header does after a removal
// (H5Oalloc.c:2254-2309): messages move forward into free space of earlier
// chunks and a continuation chunk left empty is freed with its continuation
// message. A message that is new, or whose size changed (C removes and
// re-appends it, H5O__attr_rename_cb), is placed as H5O__alloc places it
// (H5Oalloc.c:1235-1300):
//
//  1. into the smallest free space of any chunk that holds it
//     (H5O__alloc_find_best_null, :1165-1222);
//  2. otherwise into a chunk extended in place, the first that the allocator
//     can grow at the end of the file or into free space after it
//     (H5O__alloc_extend_chunk, :476-672, through Allocator.TryExtend, the
//     counterpart of H5MF_try_extend);
//  3. otherwise into a new continuation chunk (H5O__alloc_new_chunk,
//     :1119-1160). Its continuation message takes free space of a chunk, or
//     the place of a message that moves into the new chunk
//     (H5O__alloc_find_best_nonnull, :699-830: any message but a
//     continuation message, attributes last), or, when no single message
//     frees enough room, the place of every message of the last chunk
//     (H5O__alloc_chunk, :925-985).
//
// Free space within a chunk is kept as one null message after the chunk's
// messages, or as a gap at the end of a version 2 chunk when it is shorter
// than a message header, which is where H5O__move_msgs_forward and
// H5O__merge_null leave it (H5Oalloc.c:1550-1700, 1924-2060). Every chunk is
// written with its checksum recomputed; chunks freed by the condensing go
// back to the allocator after the header is written.
//
// Deviation: C also replaces a continuation message by the messages of the
// chunk it names when they fit in its place (H5O__move_cont); this is not
// done, the chunk stays.
func (fw *FileWriter) rewriteObjectHeader(addr uint64, oh *core.ObjectHeader) error {
	if oh.Version != 1 && oh.Version != 2 {
		return fmt.Errorf("rewriting a version %d object header: %w", oh.Version, utils.ErrNotSupported)
	}
	disk, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("re-read object header at %d: %w", addr, err)
	}
	l, err := fw.loadHeaderLayout(addr, disk)
	if err != nil {
		return fmt.Errorf("object header at %d: %w", addr, err)
	}
	if err := l.apply(oh); err != nil {
		return fmt.Errorf("object header at %d: %w", addr, err)
	}
	if err := l.write(); err != nil {
		return fmt.Errorf("object header at %d: %w", addr, err)
	}
	allocator := fw.writer.Allocator()
	for _, c := range l.freed {
		if err := allocator.Free(c.addr, c.size); err != nil {
			return fmt.Errorf("object header at %d: free continuation chunk [0x%X, +%d): %w", addr, c.addr, c.size, err)
		}
	}
	fw.setHeaderAllocSize(addr, l.chunks[0].size)
	return nil
}

// ohSlot is one message of an object header chunk: a header message, or a
// continuation message naming another chunk (cont), whose data is encoded
// from that chunk when the header is written.
type ohSlot struct {
	typ   core.MessageType
	flags uint8
	crt   uint16
	data  []byte
	cont  *ohChunk
}

// ohChunk is one chunk of an object header: its file space, prefix or
// signature and checksum included, and its messages in order.
type ohChunk struct {
	addr, size uint64
	slots      []*ohSlot
}

// ohLayout is an object header as chunks of messages, the model H5O_t keeps
// (H5Opkg.h: chunk array, message array).
type ohLayout struct {
	fw      *FileWriter
	sb      *core.Superblock
	version uint8
	flags   uint8 // version 2 flags without the chunk 0 size bits
	width   uint64
	times   []byte
	phase   []byte
	refs    uint32
	chunks  []*ohChunk
	byAddr  map[uint64]*ohSlot // messages of the header on disk, by offset
	freed   []*ohChunk
}

// loadHeaderLayout builds the chunk layout of the header at addr from disk,
// the header as core.ReadObjectHeader read it: chunk 0, then the
// continuation chunks in the order the reader follows them (the order C
// numbers them in), each message in the chunk that holds its offset.
func (fw *FileWriter) loadHeaderLayout(addr uint64, disk *core.ObjectHeader) (*ohLayout, error) {
	space, err := fw.headerSpace(addr)
	if err != nil {
		return nil, err
	}
	l := &ohLayout{
		fw: fw, sb: fw.file.sb, version: disk.Version, refs: disk.ReferenceCount,
		times: disk.Times, phase: disk.AttrPhaseChange,
		chunks: []*ohChunk{{addr: addr, size: space}},
		byAddr: map[uint64]*ohSlot{},
	}
	if disk.Version == 2 {
		l.flags = disk.Flags &^ ohdrFlagChunk0Size
		l.width = uint64(1) << (disk.Flags & ohdrFlagChunk0Size)
		if err := l.fitChunk0Width(); err != nil {
			return nil, err
		}
	}

	osz, lsz := int(l.sb.OffsetSize), int(l.sb.LengthSize)
	for _, m := range disk.Messages {
		if m.Type == core.MsgNil {
			continue
		}
		c := l.chunkAt(m.Offset)
		if c == nil {
			return nil, fmt.Errorf("message at 0x%X is in no chunk of the header: %w", m.Offset, utils.ErrCorrupt)
		}
		s := &ohSlot{typ: m.Type, flags: m.Flags, crt: m.CreationIndex, data: m.Data}
		if m.Type == core.MsgContinuation {
			if len(m.Data) < osz+lsz {
				return nil, fmt.Errorf("continuation message of %d bytes: %w", len(m.Data), utils.ErrCorrupt)
			}
			s.cont = &ohChunk{addr: readAddressLE(m.Data[:osz]), size: readAddressLE(m.Data[osz : osz+lsz])}
			l.chunks = append(l.chunks, s.cont)
		}
		c.slots = append(c.slots, s)
		l.byAddr[m.Offset] = s
	}
	for i, c := range l.chunks {
		if l.used(c) > l.capacity(c, i) {
			return nil, fmt.Errorf("chunk at 0x%X holds %d bytes of messages in %d bytes: %w",
				c.addr, l.used(c), l.capacity(c, i), utils.ErrCorrupt)
		}
	}
	return l, nil
}

// chunkAt returns the chunk that holds the byte at off.
func (l *ohLayout) chunkAt(off uint64) *ohChunk {
	for _, c := range l.chunks {
		if off >= c.addr && off-c.addr < c.size {
			return c
		}
	}
	return nil
}

// msgHeaderSize is the size of a message header (H5O_SIZEOF_MSGHDR_OH,
// H5Opkg.h:112-122).
func (l *ohLayout) msgHeaderSize() uint64 {
	if l.version == 1 {
		return ohdrV1MsgHeader
	}
	return core.V2MessageHeaderSize(l.flags)
}

// align rounds n up as version 1 headers align messages to 8 bytes
// (H5O_ALIGN_OH, H5Opkg.h:57-59); version 2 does not align.
func (l *ohLayout) align(n uint64) uint64 {
	if l.version == 1 {
		return (n + 7) &^ 7
	}
	return n
}

// dataSize is the size of the data of s in the chunk.
func (l *ohLayout) dataSize(s *ohSlot) uint64 {
	if s.cont != nil {
		return l.align(uint64(l.sb.OffsetSize) + uint64(l.sb.LengthSize))
	}
	return l.align(uint64(len(s.data)))
}

// slotSize is the size of s in the chunk, message header included.
func (l *ohLayout) slotSize(s *ohSlot) uint64 {
	return l.msgHeaderSize() + l.dataSize(s)
}

// chunkOverhead is the part of chunk i that holds no messages: the prefix
// and checksum of chunk 0, the signature and checksum of a version 2
// continuation chunk (H5O_SIZEOF_HDR, H5O_SIZEOF_CHKHDR_OH, H5Opkg.h:85-139).
func (l *ohLayout) chunkOverhead(i int) uint64 {
	switch {
	case l.version == 1 && i == 0:
		return ohdrV1PrefixSize
	case l.version == 1:
		return 0
	case i == 0:
		return l.prefixSize() + ohdrChecksumSize
	default:
		return uint64(len(ohdrV2ChunkMagic)) + ohdrChecksumSize
	}
}

// prefixSize is the size of the version 2 prefix of chunk 0.
func (l *ohLayout) prefixSize() uint64 {
	n := uint64(ohdrV2FixedPrefix) + l.width
	if l.flags&ohdrFlagTimes != 0 {
		n += ohdrV2TimesSize
	}
	if l.flags&ohdrFlagPhase != 0 {
		n += ohdrV2PhaseSize
	}
	return n
}

// capacity is the space chunk c (number i) has for messages.
func (l *ohLayout) capacity(c *ohChunk, i int) uint64 {
	if o := l.chunkOverhead(i); c.size > o {
		return c.size - o
	}
	return 0
}

// used is the space the messages of c take.
func (l *ohLayout) used(c *ohChunk) uint64 {
	var n uint64
	for _, s := range c.slots {
		n += l.slotSize(s)
	}
	return n
}

// free is the space of chunk i that holds no message.
func (l *ohLayout) free(i int) uint64 {
	c := l.chunks[i]
	return l.capacity(c, i) - l.used(c)
}

// widthFor is the smallest chunk 0 size field width, at least the current
// one, that encodes n (H5Oalloc.c:524-548).
func widthFor(current, n uint64) uint64 {
	w := current
	for w < 8 && n > uint64(1)<<(8*w)-1 {
		w *= 2
	}
	return w
}

// fitChunk0Width widens the chunk 0 size field of a version 2 header until
// it encodes the data size of chunk 0. A header written in this session may
// own more space than its size field described when it was written.
func (l *ohLayout) fitChunk0Width() error {
	c := l.chunks[0]
	for {
		if c.size < l.chunkOverhead(0) {
			return fmt.Errorf("chunk 0 of %d bytes is shorter than its prefix: %w", c.size, utils.ErrCorrupt)
		}
		w := widthFor(l.width, l.capacity(c, 0))
		if w == l.width {
			return nil
		}
		l.width = w
	}
}

// apply changes the layout to hold the messages of oh, as described at
// rewriteObjectHeader.
func (l *ohLayout) apply(oh *core.ObjectHeader) error {
	if err := l.applyPrefix(oh); err != nil {
		return err
	}

	kept := map[*ohSlot]bool{}
	var appended []*ohSlot
	for _, m := range oh.Messages {
		if m.Type == core.MsgNil || m.Type == core.MsgContinuation {
			continue // the layout keeps free space and the chain itself
		}
		s := &ohSlot{typ: m.Type, flags: m.Flags, crt: m.CreationIndex, data: m.Data}
		if err := l.checkMessage(s); err != nil {
			return err
		}
		old, ok := l.byAddr[m.Offset]
		if ok && !kept[old] && old.cont == nil && l.dataSize(old) == l.dataSize(s) {
			// Same size: rewritten in place (H5O_msg_write).
			old.typ, old.flags, old.crt, old.data = s.typ, s.flags, s.crt, s.data
			kept[old] = true
			continue
		}
		appended = append(appended, s)
	}

	if l.removeUnkept(kept) {
		l.condense()
	}

	for _, s := range appended {
		if err := l.alloc(s); err != nil {
			return err
		}
	}
	return nil
}

// applyPrefix takes the prefix fields of oh: flags, times and phase change
// values of version 2, the reference count of version 1.
func (l *ohLayout) applyPrefix(oh *core.ObjectHeader) error {
	if oh.Version != l.version {
		return fmt.Errorf("header version changed from %d to %d: %w", l.version, oh.Version, utils.ErrNotSupported)
	}
	if l.version == 1 {
		l.refs = oh.ReferenceCount
		return nil
	}
	if oh.Flags&ohdrFlagCrtTracked != l.flags&ohdrFlagCrtTracked {
		return fmt.Errorf("attribute creation order tracking changed: %w", utils.ErrNotSupported)
	}
	l.flags = oh.Flags &^ ohdrFlagChunk0Size
	l.times, l.phase = oh.Times, oh.AttrPhaseChange
	if (l.flags&ohdrFlagTimes != 0) != (len(l.times) == ohdrV2TimesSize) ||
		(l.flags&ohdrFlagPhase != 0) != (len(l.phase) == ohdrV2PhaseSize) {
		return fmt.Errorf("prefix fields do not match flags 0x%02X: %w", oh.Flags, utils.ErrCorrupt)
	}
	return l.fitChunk0Width()
}

// removeUnkept removes every message but the continuation messages and
// those in kept, and reports whether it removed one.
func (l *ohLayout) removeUnkept(kept map[*ohSlot]bool) bool {
	removed := false
	for _, c := range l.chunks {
		slots := c.slots[:0]
		for _, s := range c.slots {
			if s.cont != nil || kept[s] {
				slots = append(slots, s)
			} else {
				removed = true
			}
		}
		c.slots = slots
	}
	return removed
}

// checkMessage refuses a message the header cannot encode.
func (l *ohLayout) checkMessage(s *ohSlot) error {
	if l.version == 2 && s.typ > 0xFF {
		return fmt.Errorf("message type %d in a version 2 header: %w", s.typ, utils.ErrNotSupported)
	}
	limit := uint64(maxNilDataSize)
	if l.version == 1 {
		limit &^= 7
	}
	if l.dataSize(s) > limit {
		return fmt.Errorf("message of type %d with %d bytes: %w", s.typ, len(s.data), errHeaderFull)
	}
	return nil
}

// condense moves messages forward into free space of earlier chunks and
// removes the continuation chunks left empty, until nothing changes
// (H5O__condense_header, H5Oalloc.c:2254-2309: H5O__move_msgs_forward,
// H5O__merge_null, H5O__remove_empty_chunks).
func (l *ohLayout) condense() {
	for changed := true; changed; {
		changed = l.moveForward() || l.removeEmptyChunk()
	}
}

// moveForward moves the first message of a continuation chunk that fits the
// free space of an earlier chunk there (H5Oalloc.c:1660-1700: a message moves
// to a null message of an earlier chunk at least its size), and reports
// whether it moved one.
func (l *ohLayout) moveForward() bool {
	for k := 1; k < len(l.chunks); k++ {
		c := l.chunks[k]
		for j, s := range c.slots {
			for i := range k {
				if l.free(i) >= l.slotSize(s) {
					l.chunks[i].slots = append(l.chunks[i].slots, s)
					c.slots = append(c.slots[:j], c.slots[j+1:]...)
					return true
				}
			}
		}
	}
	return false
}

// removeEmptyChunk removes one continuation chunk without messages and the
// continuation message that names it (H5O__remove_empty_chunks,
// H5Oalloc.c:2069-2240), and reports whether it removed one. Its space is
// freed after the header is written.
func (l *ohLayout) removeEmptyChunk() bool {
	for k := 1; k < len(l.chunks); k++ {
		empty := l.chunks[k]
		if len(empty.slots) != 0 {
			continue
		}
		for _, c := range l.chunks {
			for j, s := range c.slots {
				if s.cont == empty {
					c.slots = append(c.slots[:j], c.slots[j+1:]...)
				}
			}
		}
		l.chunks = append(l.chunks[:k], l.chunks[k+1:]...)
		l.freed = append(l.freed, empty)
		return true
	}
	return false
}

// alloc places the new message s (H5O__alloc, H5Oalloc.c:1235-1300).
func (l *ohLayout) alloc(s *ohSlot) error {
	need := l.slotSize(s)
	if i := l.bestFree(need); i >= 0 {
		l.chunks[i].slots = append(l.chunks[i].slots, s)
		return nil
	}
	for i := range l.chunks {
		if l.extend(i, need) {
			l.chunks[i].slots = append(l.chunks[i].slots, s)
			return nil
		}
	}
	return l.newChunk(s)
}

// bestFree returns the chunk with the smallest free space of at least need
// bytes, the earliest of equals, or -1 (H5O__alloc_find_best_null,
// H5Oalloc.c:1165-1222).
func (l *ohLayout) bestFree(need uint64) int {
	best := -1
	for i := range l.chunks {
		if f := l.free(i); f >= need && (best < 0 || f < l.free(best)) {
			best = i
		}
	}
	return best
}

// extend grows chunk i in place so that its free space holds need bytes,
// and reports whether the allocator could (H5O__alloc_extend_chunk,
// H5Oalloc.c:476-672). Chunk 0 of a version 2 header may need a wider size
// field; its extra bytes are part of the extension (:524-548).
func (l *ohLayout) extend(i int, need uint64) bool {
	c := l.chunks[i]
	delta := l.align(need - l.free(i))
	width := l.width
	var extra uint64
	if l.version == 2 && i == 0 {
		width = widthFor(l.width, l.capacity(c, 0)+delta)
		extra = width - l.width
	}
	if !l.fw.writer.Allocator().TryExtend(c.addr, c.size, delta+extra) {
		return false
	}
	c.size += delta + extra
	l.width = width
	return true
}

// newChunk places s in a new continuation chunk (H5O__alloc_new_chunk and
// H5O__alloc_chunk, H5Oalloc.c:845-1160).
func (l *ohLayout) newChunk(s *ohSlot) error {
	hdr := l.msgHeaderSize()
	cont := &ohSlot{typ: core.MsgContinuation}
	contSize := hdr + l.align(uint64(l.sb.OffsetSize)+uint64(l.sb.LengthSize))
	size := l.dataSize(s)

	// Where the continuation message goes: free space, or the place of a
	// message that moves into the new chunk.
	holder, moveChunk, moveAt := l.bestFree(contSize), -1, -1
	moveAll := false
	if holder < 0 {
		var multi uint64
		moveChunk, moveAt, multi = l.bestToMove(contSize)
		if moveAt < 0 {
			moveAll = true
			size += multi
		} else {
			size += l.slotSize(l.chunks[moveChunk].slots[moveAt])
		}
	}

	chunkSize := l.align(max(ohdrMinChunkData, size+hdr))
	if l.version == 2 {
		chunkSize += uint64(len(ohdrV2ChunkMagic)) + ohdrChecksumSize
	}
	addr, err := l.fw.writer.Allocator().Allocate(chunkSize)
	if err != nil {
		return fmt.Errorf("allocate object header continuation chunk: %w", err)
	}
	nc := &ohChunk{addr: addr, size: chunkSize}
	cont.cont = nc

	switch {
	case holder >= 0:
		l.chunks[holder].slots = append(l.chunks[holder].slots, cont)
	case moveAll:
		last := l.chunks[len(l.chunks)-1]
		nc.slots = last.slots
		last.slots = []*ohSlot{cont}
	default:
		from := l.chunks[moveChunk]
		nc.slots = []*ohSlot{from.slots[moveAt]}
		from.slots[moveAt] = cont
	}
	nc.slots = append(nc.slots, s)
	l.chunks = append(l.chunks, nc)
	return nil
}

// bestToMove returns the message whose place, with the free space after it,
// holds a continuation message of contSize bytes: the smallest such place,
// a message that is not an attribute before any attribute, the earliest
// chunk among equals; never a continuation message. Without one it returns
// -1 and the size of the smaller messages of the last chunk, which then all
// move (H5O__alloc_find_best_nonnull, H5Oalloc.c:699-830).
func (l *ohLayout) bestToMove(contSize uint64) (chunk, at int, multi uint64) {
	chunk, at = -1, -1
	var bestTotal uint64
	var bestAttr bool
	last := len(l.chunks) - 1
	for k, c := range l.chunks {
		for j, s := range c.slots {
			if s.cont != nil {
				continue
			}
			total := l.movableSpace(k, j)
			if total < contSize {
				if at < 0 && k == last {
					multi += l.slotSize(s)
				}
				continue
			}
			isAttr := s.typ == core.MsgAttribute
			if at < 0 || betterToMove(isAttr, total, bestAttr, bestTotal) {
				chunk, at, bestTotal, bestAttr = k, j, total, isAttr
			}
		}
	}
	return chunk, at, multi
}

// movableSpace is the space message j of chunk k frees when it moves: its
// own, and the free space after it when it is the chunk's last message.
func (l *ohLayout) movableSpace(k, j int) uint64 {
	c := l.chunks[k]
	n := l.slotSize(c.slots[j])
	if j == len(c.slots)-1 {
		n += l.free(k)
	}
	return n
}

// betterToMove reports whether a message (attribute or not, total bytes)
// is a better one to move than the best so far: a message that is not an
// attribute before an attribute, then the smaller (H5Oalloc.c:740-760).
func betterToMove(isAttr bool, total uint64, bestAttr bool, bestTotal uint64) bool {
	if isAttr != bestAttr {
		return !isAttr
	}
	return total < bestTotal
}

// write writes every chunk of the layout with its checksum.
func (l *ohLayout) write() error {
	images := make([][]byte, len(l.chunks))
	var nmesgs uint64
	for i := range l.chunks {
		var n uint64
		images[i], n = l.encodeChunk(i)
		nmesgs += n
	}

	if err := l.putPrefix(images[0], nmesgs); err != nil {
		return err
	}
	for i, c := range l.chunks {
		buf := images[i]
		if l.version == 2 {
			n := len(buf) - ohdrChecksumSize
			binary.LittleEndian.PutUint32(buf[n:], core.JenkinsChecksum(buf[:n]))
		}
		if err := l.fw.writer.WriteAtAddress(buf, c.addr); err != nil {
			return fmt.Errorf("write object header chunk at 0x%X: %w", c.addr, err)
		}
	}
	return nil
}

// encodeChunk encodes the messages of chunk i and its free space, without
// the chunk 0 prefix and the checksum, and returns the image and the number
// of messages written.
func (l *ohLayout) encodeChunk(i int) ([]byte, uint64) {
	c := l.chunks[i]
	buf := make([]byte, c.size)
	off, end := l.chunkOverhead(i), c.size
	if l.version == 2 {
		off -= ohdrChecksumSize
		end -= ohdrChecksumSize
		if i > 0 {
			copy(buf, ohdrV2ChunkMagic)
		}
	}
	var nmesgs uint64
	for _, s := range c.slots {
		data := s.data
		if s.cont != nil {
			data = core.EncodeContinuationMessage(s.cont.addr, s.cont.size, l.sb)
		}
		l.putMessage(buf[off:], s.typ, s.flags, s.crt, data, l.dataSize(s))
		off += l.slotSize(s)
		nmesgs++
	}
	return buf, nmesgs + l.putFree(buf[off:end])
}

// putFree fills buf, the free space at the end of a chunk, with null
// messages; a remainder shorter than a message header stays a gap, which
// only a version 2 chunk without null messages can have (H5O__add_gap;
// H5Ocache.c:1507-1521). It returns the number of null messages.
func (l *ohLayout) putFree(buf []byte) uint64 {
	hdr := l.msgHeaderSize()
	limit := uint64(maxNilDataSize)
	if l.version == 1 {
		limit &^= 7
	}
	var off, count uint64
	for f := uint64(len(buf)); f-off >= hdr; {
		n := f - off - hdr
		if n > limit {
			n = limit
			if r := f - off - hdr - n; r < hdr {
				n -= hdr - r
			}
		}
		l.putMessage(buf[off:], core.MsgNil, 0, 0, nil, n)
		off += hdr + n
		count++
	}
	return count
}

// putMessage encodes a message into buf: its header (version 1: type, size,
// flags, 3 reserved bytes; version 2: type, size, flags and the creation
// index when the header tracks it, H5Ocache.c:1270-1304), then data padded
// with zeros to size bytes.
func (l *ohLayout) putMessage(buf []byte, typ core.MessageType, flags uint8, crt uint16, data []byte, size uint64) {
	var at uint64
	if l.version == 1 {
		binary.LittleEndian.PutUint16(buf[0:], uint16(typ))
		binary.LittleEndian.PutUint16(buf[2:], uint16(size)) //nolint:gosec // G115: checked against maxNilDataSize
		buf[4] = flags
		at = ohdrV1MsgHeader
	} else {
		buf[0] = byte(typ)                                   //nolint:gosec // G115: checkMessage refuses types above 0xFF
		binary.LittleEndian.PutUint16(buf[1:], uint16(size)) //nolint:gosec // G115: checked against maxNilDataSize
		buf[3] = flags
		at = 4
		if l.flags&ohdrFlagCrtTracked != 0 {
			binary.LittleEndian.PutUint16(buf[4:], crt)
			at = 6
		}
	}
	copy(buf[at:at+size], data)
}

// putPrefix encodes the prefix of chunk 0 into buf: for version 1 the
// version, message count (every message of every chunk, null messages
// included, as C counts them), reference count and header data size
// (H5Ocache.c:1094-1140); for version 2 the signature, version, flags,
// optional times and phase change values and the chunk 0 data size
// (H5Ocache.c:1012-1092).
func (l *ohLayout) putPrefix(buf []byte, nmesgs uint64) error {
	c := l.chunks[0]
	if l.version == 1 {
		if nmesgs > 0xFFFF || c.size-ohdrV1PrefixSize > 0xFFFFFFFF {
			return fmt.Errorf("version 1 header of %d messages, %d bytes: %w", nmesgs, c.size, utils.ErrNotSupported)
		}
		buf[0] = 1
		binary.LittleEndian.PutUint16(buf[2:], uint16(nmesgs))
		binary.LittleEndian.PutUint32(buf[4:], l.refs)
		binary.LittleEndian.PutUint32(buf[8:], uint32(c.size-ohdrV1PrefixSize)) //nolint:gosec // G115: checked above
		return nil
	}
	copy(buf, "OHDR")
	buf[4] = 2
	var bits uint8
	for w := l.width; w > 1; w /= 2 {
		bits++
	}
	buf[5] = l.flags | bits
	at := ohdrV2FixedPrefix
	if l.flags&ohdrFlagTimes != 0 {
		at += copy(buf[at:], l.times)
	}
	if l.flags&ohdrFlagPhase != 0 {
		at += copy(buf[at:], l.phase)
	}
	n := l.capacity(c, 0)
	var field [8]byte
	binary.LittleEndian.PutUint64(field[:], n)
	copy(buf[at:at+int(l.width)], field[:l.width]) //nolint:gosec // G115: width is 1, 2, 4 or 8
	return nil
}

// headerSpace returns the size of chunk 0 of the object header at addr: the
// allocation when the header was written in this session, otherwise its size
// on disk (prefix, messages and checksum), which is then remembered.
func (fw *FileWriter) headerSpace(addr uint64) (uint64, error) {
	if n := fw.lookupHeaderAllocSize(addr); n > 0 {
		return n, nil
	}
	n, err := readChunk0Size(fw, addr)
	if err != nil {
		return 0, err
	}
	fw.setHeaderAllocSize(addr, n)
	return n, nil
}

// readChunk0Size reads the size of chunk 0 of the object header at addr
// from its prefix: for version 2 the signature, version, flags, the optional
// times and attribute phase change values and the chunk-size field, 1 to 8
// bytes wide by flags bits 0-1 (H5Ocache.c:1012-1092), plus the checksum;
// for version 1 the 16-byte prefix plus the header size field
// (H5Ocache.c:1094-1140).
func readChunk0Size(fw *FileWriter, addr uint64) (uint64, error) {
	r, sb := fw.writer.Reader(), fw.file.sb
	head, err := core.ReadBlock(r, sb, addr, ohdrV2FixedPrefix, ohdrV2FixedPrefix, "object header prefix")
	if err != nil {
		return 0, fmt.Errorf("object header at %d: %w", addr, err)
	}

	if string(head[:4]) != "OHDR" {
		if head[0] != 1 {
			return 0, fmt.Errorf("object header at %d: version %d: %w", addr, head[0], utils.ErrCorrupt)
		}
		v1, err := core.ReadBlock(r, sb, addr, ohdrV1PrefixSize, ohdrV1PrefixSize, "object header prefix")
		if err != nil {
			return 0, fmt.Errorf("object header at %d: %w", addr, err)
		}
		return ohdrV1PrefixSize + uint64(binary.LittleEndian.Uint32(v1[8:12])), nil
	}

	flags := head[5]
	fields := uint64(0)
	if flags&0x20 != 0 {
		fields += ohdrV2TimesSize
	}
	if flags&0x10 != 0 {
		fields += ohdrV2PhaseSize
	}
	width := uint64(1) << (flags & 0x03)
	prefixLen := ohdrV2FixedPrefix + fields + width
	prefix, err := core.ReadBlock(r, sb, addr, prefixLen, prefixLen, "object header prefix")
	if err != nil {
		return 0, fmt.Errorf("object header at %d: %w", addr, err)
	}

	field := prefix[ohdrV2FixedPrefix+fields:]
	var chunkSize uint64
	switch width {
	case 1:
		chunkSize = uint64(field[0])
	case 2:
		chunkSize = uint64(binary.LittleEndian.Uint16(field))
	case 4:
		chunkSize = uint64(binary.LittleEndian.Uint32(field))
	default:
		chunkSize = binary.LittleEndian.Uint64(field)
	}

	total := prefixLen + chunkSize + ohdrChecksumSize
	if total < chunkSize {
		return 0, fmt.Errorf("object header at %d: chunk 0 size %d: %w", addr, chunkSize, utils.ErrCorrupt)
	}
	return total, nil
}

// setHeaderAllocSize records n as the space of the object header at addr.
func (fw *FileWriter) setHeaderAllocSize(addr, n uint64) {
	if addr == fw.rootGroupAddr {
		fw.rootHeaderAllocSz = n
		return
	}
	for _, meta := range fw.groups {
		if meta.headerAddr == addr {
			meta.headerAllocSz = n
			return
		}
	}
	fw.headerAllocs[addr] = n
}
