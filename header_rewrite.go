package hdf5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// errHeaderFull reports that an object header cannot take its messages even
// after the movable ones went to a continuation chunk.
var errHeaderFull = errors.New("object header full")

// Sizes of the version 2 object header layout (H5Opkg.h:85-107,
// H5Ocache.c:1012-1092).
const (
	ohdrV2FixedPrefix = 6  // "OHDR", version, flags
	ohdrV2TimesSize   = 16 // access, modification, change, birth times
	ohdrV2PhaseSize   = 4  // max compact, min dense
	ohdrV1PrefixSize  = 16 // version 1 prefix, padded to 8 bytes
	ohdrChecksumSize  = 4
	maxNilDataSize    = 0xFFFF // the 16-bit message size field
)

// rewriteObjectHeader writes the object header at addr back with the
// messages of oh, which the caller read with core.ReadObjectHeader and then
// changed (data replaced, messages removed or appended).
//
// The header never writes past the space its chunk 0 has on disk, as C keeps
// an object header inside the chunks it allocated (H5Oalloc.c:1235-1300):
//   - the messages fit: they are written with a null message over the rest
//     of the space, as C turns freed space into null messages;
//   - the header is the last block of the file: it grows in place;
//   - otherwise trailing movable messages go to a new continuation chunk
//     (OCHK) at the end of the file, as H5O__alloc_new_chunk does
//     (H5Oalloc.c:845-1000); errHeaderFull when even that cannot fit.
//
// Messages read from existing continuation chunks stay there when the caller
// left them as they are on disk; when the caller changed or removed one of
// them, the remaining ones are written anew and the old chain is dropped (its
// space is left unused).
func (fw *FileWriter) rewriteObjectHeader(addr uint64, oh *core.ObjectHeader) error {
	if oh.Version != 2 {
		return fmt.Errorf("rewriting a version %d object header: %w", oh.Version, utils.ErrNotSupported)
	}

	space, err := fw.headerSpace(addr)
	if err != nil {
		return err
	}

	disk, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("re-read object header at %d: %w", addr, err)
	}

	return fw.writeHeaderChunk0(addr, oh, space, headerCandidates(oh, disk))
}

// headerCandidates returns the messages of oh for chunk 0: its own messages
// without padding and continuation messages, then either the continuation
// messages of the header on disk (disk), when the messages oh holds from
// continuation chunks are those on disk, or those messages, to be moved.
func headerCandidates(oh, disk *core.ObjectHeader) []headerCandidate {
	var mainMsgs, chained []*core.HeaderMessage
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgNil || msg.Type == core.MsgContinuation {
			continue // padding and the chain are rebuilt below
		}
		if msg.FromContinuation {
			chained = append(chained, msg)
		} else {
			mainMsgs = append(mainMsgs, msg)
		}
	}

	var diskChained, diskConts []*core.HeaderMessage
	for _, msg := range disk.Messages {
		switch {
		case msg.Type == core.MsgContinuation && !msg.FromContinuation:
			diskConts = append(diskConts, msg)
		case msg.FromContinuation && msg.Type != core.MsgNil && msg.Type != core.MsgContinuation:
			diskChained = append(diskChained, msg)
		}
	}

	candidates := make([]headerCandidate, 0, len(mainMsgs)+len(chained)+len(diskConts))
	for _, msg := range mainMsgs {
		candidates = append(candidates, headerCandidate{msg: msg, movable: movableMessage(msg.Type)})
	}
	if sameMessages(chained, diskChained) {
		// The existing continuation chunks stay as they are.
		for _, msg := range diskConts {
			candidates = append(candidates, headerCandidate{msg: msg})
		}
	} else {
		for _, msg := range chained {
			candidates = append(candidates, headerCandidate{msg: msg, movable: true})
		}
	}
	return candidates
}

// headerCandidate is a message for chunk 0 and whether it may move to a new
// continuation chunk.
type headerCandidate struct {
	msg     *core.HeaderMessage
	movable bool
}

// movableMessage reports whether a message of type typ may move from chunk 0
// to a continuation chunk. C may move any message but a continuation message
// (H5Oalloc.c:714-720); here the messages that grow or appear after an object
// is created move, while the dataset and group description stays in chunk 0.
func movableMessage(typ core.MessageType) bool {
	switch typ {
	case core.MsgAttribute, core.MsgAttributeInfo, core.MsgRefCount, core.MsgLinkMessage, core.MsgName:
		return true
	default:
		return false
	}
}

// sameMessages reports whether a and b hold the same messages in order.
func sameMessages(a, b []*core.HeaderMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Flags != b[i].Flags || a[i].CreationIndex != b[i].CreationIndex ||
			!bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

// writeHeaderChunk0 writes chunk 0 of the object header at addr, whose space
// on disk is space bytes, from the candidate messages.
func (fw *FileWriter) writeHeaderChunk0(addr uint64, oh *core.ObjectHeader, space uint64, candidates []headerCandidate) error {
	ohw := headerWriterFor(oh, candidates)
	size := ohw.Size()

	mustMove := false
	for _, c := range candidates {
		if c.msg.FromContinuation {
			mustMove = true // the old chain is gone: these go to a new chunk
			break
		}
	}

	if !mustMove && size <= space {
		padHeaderWriter(ohw, space)
		return writeHeader(fw, ohw, addr)
	}

	allocator := fw.writer.Allocator()
	if !mustMove && addr+space >= allocator.EndOfFile() {
		// The header is the last block of the file: extend it in place.
		return fw.growHeaderAtEnd(addr, ohw, size)
	}

	return fw.spillHeader(addr, oh, space, candidates)
}

// growHeaderAtEnd writes the header at addr, the last block of the file, at
// its new size and extends the allocated space to cover it.
func (fw *FileWriter) growHeaderAtEnd(addr uint64, ohw *core.ObjectHeaderWriter, size uint64) error {
	if err := writeHeader(fw, ohw, addr); err != nil {
		return err
	}
	allocator := fw.writer.Allocator()
	if end := addr + size; end > allocator.EndOfFile() {
		if _, err := allocator.Allocate(end - allocator.EndOfFile()); err != nil {
			return fmt.Errorf("extend object header at %d: %w", addr, err)
		}
	}
	fw.setHeaderAllocSize(addr, size)
	return nil
}

// spillHeader moves movable messages, last first, to a new continuation
// chunk until chunk 0 with the continuation message fits in space bytes.
// Messages of the dropped chain always move.
func (fw *FileWriter) spillHeader(addr uint64, oh *core.ObjectHeader, space uint64, candidates []headerCandidate) error {
	sb := fw.file.sb
	contMsgSize := core.V2MessageHeaderSize(oh.Flags) + uint64(sb.OffsetSize) + uint64(sb.LengthSize)

	kept := make([]headerCandidate, 0, len(candidates))
	var moved []core.MessageWriter
	for _, c := range candidates {
		if c.msg.FromContinuation {
			moved = append(moved, messageWriterOf(c.msg))
			continue
		}
		kept = append(kept, c)
	}

	var movedFromMain []core.MessageWriter
	for headerWriterFor(oh, kept).Size()+contMsgSize > space {
		i := len(kept) - 1
		for i >= 0 && !kept[i].movable {
			i--
		}
		if i < 0 {
			return fmt.Errorf("object header at %d (%d bytes): %w", addr, space, errHeaderFull)
		}
		m := kept[i].msg
		movedFromMain = append([]core.MessageWriter{messageWriterOf(m)}, movedFromMain...)
		kept = append(kept[:i], kept[i+1:]...)
	}
	moved = append(moved, movedFromMain...)

	if len(moved) > 0 {
		for _, m := range moved {
			if len(m.Data) > maxNilDataSize {
				return fmt.Errorf("object header message of %d bytes: %w", len(m.Data), errHeaderFull)
			}
		}
		ochkSize := core.ContinuationChunkSizeV2(moved, oh.Flags)
		ochkAddr, err := fw.writer.Allocator().Allocate(ochkSize)
		if err != nil {
			return fmt.Errorf("allocate continuation chunk: %w", err)
		}
		if _, err := core.WriteContinuationChunkV2(fw.writer, ochkAddr, moved, oh.Flags); err != nil {
			return fmt.Errorf("write continuation chunk: %w", err)
		}
		kept = append(kept, headerCandidate{msg: &core.HeaderMessage{
			Type: core.MsgContinuation,
			Data: core.EncodeContinuationMessage(ochkAddr, ochkSize, sb),
		}})
	}

	ohw := headerWriterFor(oh, kept)
	padHeaderWriter(ohw, space)
	return writeHeader(fw, ohw, addr)
}

// headerWriterFor returns a writer of chunk 0 of oh holding the messages of
// candidates.
func headerWriterFor(oh *core.ObjectHeader, candidates []headerCandidate) *core.ObjectHeaderWriter {
	ohw := &core.ObjectHeaderWriter{
		Version:         oh.Version,
		Flags:           oh.Flags,
		Times:           oh.Times,
		AttrPhaseChange: oh.AttrPhaseChange,
		Messages:        make([]core.MessageWriter, len(candidates)),
	}
	for i, c := range candidates {
		ohw.Messages[i] = messageWriterOf(c.msg)
	}
	return ohw
}

// messageWriterOf returns the writer of a message read from a header, with
// its flags and creation index.
func messageWriterOf(m *core.HeaderMessage) core.MessageWriter {
	return core.MessageWriter{Type: m.Type, Flags: m.Flags, Data: m.Data, CreationIndex: m.CreationIndex}
}

// padHeaderWriter appends null messages so that the header fills space
// bytes. Fewer than a message header's bytes (4, or 6 with a creation index)
// cannot be filled and stay unused after the checksum, outside the chunk; a
// header never ends up larger than space.
func padHeaderWriter(ohw *core.ObjectHeaderWriter, space uint64) {
	msgHeaderSize := core.V2MessageHeaderSize(ohw.Flags)
	for {
		size := ohw.Size()
		if size+msgHeaderSize > space {
			break
		}
		n := min(space-size-msgHeaderSize, maxNilDataSize)
		ohw.Messages = append(ohw.Messages, core.MessageWriter{Type: core.MsgNil, Data: make([]byte, n)})
	}

	// A longer chunk may need a wider chunk-size field (H5Ocache.c:418-437):
	// take the extra bytes back from the last null message.
	for ohw.Size() > space {
		last := &ohw.Messages[len(ohw.Messages)-1]
		if last.Type != core.MsgNil {
			return
		}
		over := ohw.Size() - space
		if over >= uint64(len(last.Data)) {
			ohw.Messages = ohw.Messages[:len(ohw.Messages)-1]
			continue
		}
		last.Data = last.Data[:uint64(len(last.Data))-over]
	}
}

// writeHeader writes ohw at addr.
func writeHeader(fw *FileWriter, ohw *core.ObjectHeaderWriter, addr uint64) error {
	if _, err := ohw.WriteTo(fw.writer, addr); err != nil {
		return fmt.Errorf("write object header at %d: %w", addr, err)
	}
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
