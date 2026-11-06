package hdf5

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// Groups of the 1.8 format keep their links as link messages in the group's
// object header ("compact" storage, H5Gcompact.c) while they have fewer than
// max_compact links, and in a fractal heap with a v2 B-tree name index
// ("dense" storage, H5Gdense.c) beyond that; they fall back to compact
// storage when fewer than min_dense links remain. This file adds, removes
// and looks up links in such groups of an existing file as H5G_obj_insert,
// H5G_obj_remove and H5G_obj_lookup do (H5Gobj.c), switching between the two
// forms at the same thresholds. The writer itself creates symbol-table
// groups only.

// msgGroupInfo is the group info message (H5O_GINFO_ID, H5Oprivate.h:217).
const msgGroupInfo core.MessageType = 10

// Group info message layout (H5Oginfo.c:85-140).
const (
	groupInfoVersion         = 0
	groupInfoPhaseChangeFlag = 0x01 // H5O_GINFO_STORE_PHASE_CHANGE
	groupInfoEstEntryFlag    = 0x02 // H5O_GINFO_STORE_EST_ENTRY_INFO
	groupInfoMaxCompactDef   = 8    // H5G_CRT_GINFO_MAX_COMPACT, H5Gprivate.h:74
	groupInfoMinDenseDef     = 6    // H5G_CRT_GINFO_MIN_DENSE, H5Gprivate.h:75
)

// maxHeaderMessageSize is H5O_MESG_MAX_SIZE (H5Oprivate.h:67): a link message
// this large goes to dense storage in C (H5Gobj.c:474) and keeps a group in
// dense storage (H5Gobj.c:870).
const maxHeaderMessageSize = 65536

// v1MessageHeaderSize is the size of a message header in a version 1 object
// header: type, size, flags and three reserved bytes (H5O_SIZEOF_MSGHDR_VERS,
// H5Opkg.h:85-107).
const v1MessageHeaderSize = 8

// rootHasSymbolTable reports whether the root group stores its links in a
// symbol table. OpenForWrite reads the table's addresses from the superblock
// or from the root's symbol table message; neither exists for a group of the
// 1.8 format.
func (fw *FileWriter) rootHasSymbolTable() bool {
	return fw.rootHeapAddr != 0 || fw.rootBTreeAddr != 0
}

// hasSymbolTable reports whether the group stores its links in a symbol
// table. A group of the 1.8 format is registered with neither a local heap
// nor a B-tree: its links live in its object header or in dense storage.
func (m *GroupMetadata) hasSymbolTable() bool {
	return m.heapAddr != 0 || m.btreeAddr != 0
}

// linkMessageGroup returns the object header address of the group at path
// and true when that group keeps its links as link messages or in dense
// storage rather than in a symbol table. It returns false for a symbol-table
// group and for a path that names no registered group; the caller reports
// the latter as it does for symbol-table groups.
func (fw *FileWriter) linkMessageGroup(path string) (uint64, bool) {
	if path == "" || path == "/" {
		return fw.rootGroupAddr, !fw.rootHasSymbolTable()
	}
	meta, ok := fw.groups[path]
	if !ok || meta.hasSymbolTable() {
		return 0, false
	}
	return meta.headerAddr, true
}

// parseGroupInfo returns max_compact and min_dense from a group info message
// as H5O__ginfo_decode reads them (H5Oginfo.c:85-140): stored when the
// phase-change flag is set, the library defaults otherwise.
func parseGroupInfo(data []byte) (maxCompact, minDense int, err error) {
	if len(data) < 2 {
		return 0, 0, fmt.Errorf("%w: group info message of %d bytes", utils.ErrCorrupt, len(data))
	}
	if data[0] != groupInfoVersion {
		return 0, 0, fmt.Errorf("%w: group info message version %d", utils.ErrCorrupt, data[0])
	}
	flags := data[1]
	if flags&^(groupInfoPhaseChangeFlag|groupInfoEstEntryFlag) != 0 {
		return 0, 0, fmt.Errorf("%w: group info message flags 0x%02X", utils.ErrCorrupt, flags)
	}
	need := 2
	if flags&groupInfoPhaseChangeFlag != 0 {
		need += 4
	}
	if flags&groupInfoEstEntryFlag != 0 {
		need += 4
	}
	if len(data) < need {
		return 0, 0, fmt.Errorf("%w: group info message of %d bytes, flags 0x%02X need %d",
			utils.ErrCorrupt, len(data), flags, need)
	}
	if flags&groupInfoPhaseChangeFlag == 0 {
		return groupInfoMaxCompactDef, groupInfoMinDenseDef, nil
	}
	return int(binary.LittleEndian.Uint16(data[2:4])), int(binary.LittleEndian.Uint16(data[4:6])), nil
}

// addressDefined reports whether addr, read with the file's offset size, is
// a defined address (H5_addr_defined): all bits set is HADDR_UNDEF.
func addressDefined(addr uint64, offsetSize uint8) bool {
	return addr != undefinedAddress>>(64-8*uint(offsetSize))
}

// newLinkTarget is what a new link points at: the object at addr (a hard
// link), the path soft (a soft link, when not empty), or the object extPath
// in the file extFile (an external link, when extFile is not empty).
type newLinkTarget struct {
	addr             uint64
	soft             string
	extFile, extPath string
}

// newLinkMessage builds the link message H5O__link_encode writes for a hard,
// soft or external link to t (H5Olink.c:296-391): an ASCII name (the default
// link creation property list), the narrowest name length field, the link
// type stored only for a soft or external link, and the creation order when
// the group tracks it. The value of an external link is the user data
// H5Lcreate_external builds (H5L.c:644-656) behind its 16-bit length.
func newLinkMessage(sb *core.Superblock, name string, t newLinkTarget, corder int64, corderValid bool) (*core.LinkMessage, error) {
	lm := &core.LinkMessage{Version: 1, Type: core.LinkTypeHard, Name: name}
	switch n := uint64(len(name)); {
	case n == 0:
		return nil, fmt.Errorf("link name is empty")
	case n <= math.MaxUint8:
		// Flags bits 0-1 stay 0: one-byte length.
	case n <= math.MaxUint16:
		lm.Flags |= 1
	default:
		lm.Flags |= 2
	}
	if corderValid {
		lm.Flags |= core.LinkFlagCreationOrderBit
		lm.CreationOrder = uint64(corder) //nolint:gosec // G115: C stores the int64 creation order as 8 raw bytes (INT64ENCODE)
	}
	switch {
	case t.extFile != "":
		v, err := core.EncodeExternalLinkValue(t.extFile, t.extPath)
		if err != nil {
			return nil, err
		}
		lm.Type = core.LinkTypeExternal
		lm.Flags |= core.LinkFlagLinkTypeFieldBit
		lm.LinkValue = v
	case t.soft != "":
		if len(t.soft) > math.MaxUint16 {
			return nil, fmt.Errorf("soft link target of %d bytes exceeds the 16-bit length field", len(t.soft))
		}
		lm.Type = core.LinkTypeSoft
		lm.Flags |= core.LinkFlagLinkTypeFieldBit
		lm.LinkValue = binary.LittleEndian.AppendUint16(nil, uint16(len(t.soft))) //nolint:gosec // G115: checked above
		lm.LinkValue = append(lm.LinkValue, t.soft...)
	default:
		lm.LinkValue = make([]byte, sb.OffsetSize)
		writeAddressLE(lm.LinkValue, t.addr)
	}
	return lm, nil
}

// writeAddressLE writes addr into buf, whose length is the file's offset
// size, little-endian as H5F_addr_encode does.
func writeAddressLE(buf []byte, addr uint64) {
	var full [8]byte
	binary.LittleEndian.PutUint64(full[:], addr)
	copy(buf, full[:])
}

// readAddressLE reads a little-endian address of len(buf) bytes, the file's
// offset size (H5F_addr_decode).
func readAddressLE(buf []byte) uint64 {
	var addr uint64
	for i := len(buf) - 1; i >= 0; i-- {
		addr = addr<<8 | uint64(buf[i])
	}
	return addr
}

// linkTarget returns the object address a hard link points at, and
// undefinedAddress for a soft or external link, which names no object (as a
// symbol table entry of a soft link has an undefined address).
func linkTarget(lm *core.LinkMessage, sb *core.Superblock) (uint64, error) {
	if lm.Type != core.LinkTypeHard {
		return undefinedAddress, nil
	}
	if len(lm.LinkValue) != int(sb.OffsetSize) {
		return 0, fmt.Errorf("%w: hard link %q holds %d address bytes, the file's offset size is %d",
			utils.ErrCorrupt, lm.Name, len(lm.LinkValue), sb.OffsetSize)
	}
	return readAddressLE(lm.LinkValue), nil
}

// linkGroup is the object header of a group of the 1.8 format, read for one
// insert, remove or lookup: the link info message, which says whether the
// links are compact or dense (H5G__obj_get_linfo, H5Gobj.c:297-343), and the
// thresholds of its group info message.
type linkGroup struct {
	addr     uint64
	oh       *core.ObjectHeader
	linfoMsg *core.HeaderMessage
	linfo    *core.LinkInfoMessage

	maxCompact int
	minDense   int

	// hasPipeline is set when the group has a filter pipeline message: C
	// creates its dense storage heap with those filters (H5Gobj.c:482-491).
	hasPipeline bool
}

// compactLink is one link message of a group's object header.
type compactLink struct {
	msg  *core.HeaderMessage
	link *core.LinkMessage
}

// loadLinkGroup reads the object header of the group at addr. A group
// without a group info message gets the default thresholds; C would refuse
// the change (H5Gobj.c:465), but the thresholds are all the message adds.
func (fw *FileWriter) loadLinkGroup(addr uint64) (*linkGroup, error) {
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.sb)
	if err != nil {
		return nil, fmt.Errorf("read group object header at 0x%X: %w", addr, err)
	}
	return fw.linkGroupOf(addr, oh)
}

// linkGroupOf is loadLinkGroup for the header oh already read from addr.
func (fw *FileWriter) linkGroupOf(addr uint64, oh *core.ObjectHeader) (*linkGroup, error) {
	sb := fw.file.sb
	var err error
	g := &linkGroup{addr: addr, oh: oh, maxCompact: groupInfoMaxCompactDef, minDense: groupInfoMinDenseDef}
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgLinkInfo:
			if g.linfoMsg == nil {
				g.linfoMsg = msg
			}
		case msgGroupInfo:
			if g.maxCompact, g.minDense, err = parseGroupInfo(msg.Data); err != nil {
				return nil, fmt.Errorf("group at 0x%X: %w", addr, err)
			}
		case core.MsgFilterPipeline:
			g.hasPipeline = true
		}
	}
	if g.linfoMsg == nil {
		return nil, fmt.Errorf("%w: group at 0x%X has neither a symbol table nor a link info message",
			utils.ErrCorrupt, addr)
	}
	if g.linfo, err = core.ParseLinkInfoMessage(g.linfoMsg.Data, sb); err != nil {
		return nil, fmt.Errorf("%w: group at 0x%X: link info message: %w", utils.ErrCorrupt, addr, err)
	}
	return g, nil
}

// dense reports whether the group's links are in dense storage: the link
// info message names a fractal heap (H5Gobj.c:472).
func (g *linkGroup) dense() bool {
	return g.linfo.HasFractalHeap()
}

// compactLinks returns the link messages of the group's header in header
// order.
func (g *linkGroup) compactLinks(sb *core.Superblock) ([]compactLink, error) {
	var links []compactLink
	for _, msg := range g.oh.Messages {
		if msg.Type != core.MsgLinkMessage {
			continue
		}
		lm, err := core.ParseLinkMessage(msg.Data, sb)
		if err != nil {
			return nil, fmt.Errorf("%w: group at 0x%X: link message: %w", utils.ErrCorrupt, g.addr, err)
		}
		links = append(links, compactLink{msg: msg, link: lm})
	}
	return links, nil
}

// newLink encodes the link message of a new link, with the next creation
// order of the group when it tracks creation order; the link info message
// then counts one more (H5Gobj.c:451-458).
func (g *linkGroup) newLink(sb *core.Superblock, name string, t newLinkTarget) ([]byte, error) {
	track := g.linfo.HasCreationOrderTracking()
	lm, err := newLinkMessage(sb, name, t, g.linfo.MaxCreationOrder, track)
	if err != nil {
		return nil, err
	}
	data, err := core.EncodeLinkMessage(lm, sb)
	if err != nil {
		return nil, fmt.Errorf("encode link message: %w", err)
	}
	if track {
		if g.linfo.MaxCreationOrder == math.MaxInt64 {
			return nil, fmt.Errorf("group at 0x%X: link creation order overflows", g.addr)
		}
		g.linfo.MaxCreationOrder++
	}
	return data, nil
}

// encodeLinkInfo stores the link info message, changed in memory, in the
// header's message list. Only its maximum creation order and its addresses
// change, so its size stays the same. A version 1 header keeps each message
// padded to a multiple of eight bytes (H5O_ALIGN_OLD, H5Opkg.h), and the
// message read from it carries the padding; the encoding is padded the same.
func (g *linkGroup) encodeLinkInfo(sb *core.Superblock) ([]byte, error) {
	data, err := core.EncodeLinkInfoMessage(g.linfo, sb)
	if err != nil {
		return nil, fmt.Errorf("encode link info message: %w", err)
	}
	if old := len(g.linfoMsg.Data); len(data) != old {
		if g.oh.Version != 1 || len(data) > old || (len(data)+7)&^7 != old {
			return nil, fmt.Errorf("link info message re-encoded as %d bytes, was %d", len(data), old)
		}
		data = append(data, make([]byte, old-len(data))...)
	}
	return data, nil
}

// messageHeaderSize is the size of the header of one message in the group's
// object header (H5O_SIZEOF_MSGHDR_OH, H5Opkg.h:85-107).
func (g *linkGroup) messageHeaderSize() uint64 {
	if g.oh.Version == 1 {
		return v1MessageHeaderSize
	}
	return core.V2MessageHeaderSize(g.oh.Flags)
}

// rewriteWithLinks writes the group's header back with its link info message
// re-encoded, without the link messages in drop and with the encoded link
// messages of add appended (H5O_msg_remove, H5O_msg_append_oh). The messages
// are appended to the list; rewriteObjectHeader lays them out for a version 1
// or 2 header alike.
func (fw *FileWriter) rewriteWithLinks(g *linkGroup, drop map[*core.HeaderMessage]bool, add [][]byte) error {
	sb := fw.file.sb
	linfoData, err := g.encodeLinkInfo(sb)
	if err != nil {
		return err
	}
	if len(drop) > 0 {
		kept := g.oh.Messages[:0:0]
		for _, msg := range g.oh.Messages {
			if !drop[msg] {
				kept = append(kept, msg)
			}
		}
		g.oh.Messages = kept
	}
	g.linfoMsg.Data = linfoData
	for _, data := range add {
		g.oh.Messages = append(g.oh.Messages, &core.HeaderMessage{Type: core.MsgLinkMessage, Data: data})
	}
	return fw.rewriteObjectHeader(g.addr, g.oh)
}

// groupLinkExistsError is the error for a name the group at addr already
// holds (errGroupEntryExists, as linkExistsError gives it by path).
func groupLinkExistsError(addr uint64, name string) error {
	return fmt.Errorf("%w: %q in the group at 0x%X", errGroupEntryExists, name, addr)
}

// insertGroupLink adds a link named name to the group of the 1.8 format whose
// object header is at groupAddr: a hard link to childAddr, or a soft link to
// softTarget when it is not empty (insertLink).
func (fw *FileWriter) insertGroupLink(groupAddr uint64, name string, childAddr uint64, softTarget string) error {
	return fw.insertLink(groupAddr, name, newLinkTarget{addr: childAddr, soft: softTarget})
}

// insertLink adds a link named name to t to the group of the 1.8 format
// whose object header is at groupAddr. As H5G_obj_insert does
// (H5Gobj.c:422-580), the link goes to dense storage when the group has it,
// into the header as a link message while the group holds fewer than
// max_compact links, and otherwise the group's links move to new dense
// storage first.
func (fw *FileWriter) insertLink(groupAddr uint64, name string, t newLinkTarget) error {
	sb := fw.file.sb
	g, err := fw.loadLinkGroup(groupAddr)
	if err != nil {
		return err
	}
	if g.dense() {
		return fw.insertDenseLink(g, name, t)
	}

	links, err := g.compactLinks(sb)
	if err != nil {
		return err
	}
	for _, l := range links {
		if l.link.Name == name {
			return groupLinkExistsError(g.addr, name)
		}
	}
	data, err := g.newLink(sb, name, t)
	if err != nil {
		return err
	}
	// The size limit counts the message header too, as C does
	// (H5O_msg_size_oh, H5Gobj.c), and as removeDenseLink checks it.
	if len(links) < g.maxCompact && uint64(len(data))+g.messageHeaderSize() < maxHeaderMessageSize {
		// H5G__compact_insert (H5Gcompact.c:168-184): one more link message.
		return fw.rewriteWithLinks(g, nil, [][]byte{data})
	}
	return fw.compactToDense(g, links, name, data)
}

// insertDenseLink adds a link to a group in dense storage (H5G__dense_insert,
// H5Gdense.c:344-431): the encoded link message goes into the fractal heap
// and its heap ID into the name index. The link info message changes only
// when the group tracks creation order; it keeps its size, so it is patched
// in place.
func (fw *FileWriter) insertDenseLink(g *linkGroup, name string, t newLinkTarget) error {
	sb := fw.file.sb
	st, err := fw.loadDenseLinkStorage(g.linfo)
	if err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	if _, found, err := st.names.FindRecord(name); err != nil {
		return fmt.Errorf("group at 0x%X: look up %q: %w", g.addr, name, err)
	} else if found {
		return groupLinkExistsError(g.addr, name)
	}
	data, err := g.newLink(sb, name, t)
	if err != nil {
		return err
	}
	if err := st.insert(name, data); err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	var linfoData []byte
	if g.linfo.HasCreationOrderTracking() {
		if linfoData, err = g.encodeLinkInfo(sb); err != nil {
			return err
		}
	}
	if err := st.flush(fw); err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	if linfoData == nil {
		return nil
	}
	return fw.patchHeaderMessage(g.addr, g.oh, g.linfoMsg, core.MsgLinkInfo, linfoData)
}

// compactToDense moves the links of a compact group into new dense storage
// and adds the new link (encoded as data) there, as H5G_obj_insert does when
// the group reaches max_compact links or the new link message is too large
// for a header (H5Gobj.c:476-509): H5G__dense_create, each link message
// inserted in header order (H5G__obj_compact_to_dense_cb, H5Gobj.c:356-374),
// the link messages removed from the header, then the new link inserted.
//
// The heap and the name index are written before the header names them, so
// a failure leaves the group as it was (with unreferenced space at the end of
// the file).
func (fw *FileWriter) compactToDense(g *linkGroup, links []compactLink, name string, data []byte) error {
	sb := fw.file.sb
	if g.hasPipeline {
		return fmt.Errorf("%w: dense link storage with a filtered fractal heap (group at 0x%X)", ErrNotSupported, g.addr)
	}
	if g.linfo.HasCreationOrderIndex() {
		return fmt.Errorf("%w: group at 0x%X indexes link creation order (v2 B-tree type 6)", ErrNotSupported, g.addr)
	}
	st, err := newDenseLinkStorage(sb)
	if err != nil {
		return err
	}
	drop := make(map[*core.HeaderMessage]bool, len(links))
	for _, l := range links {
		if err := st.insert(l.link.Name, l.msg.Data); err != nil {
			return fmt.Errorf("group at 0x%X: %w", g.addr, err)
		}
		drop[l.msg] = true
	}
	if err := st.insert(name, data); err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	heapAddr, nameAddr, err := st.create(fw)
	if err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	g.linfo.FractalHeapAddress = heapAddr
	g.linfo.NameBTreeAddress = nameAddr
	return fw.rewriteWithLinks(g, drop, nil)
}

// removeGroupLink removes the link named name from the group of the 1.8
// format whose object header is at groupAddr and returns the address of the
// object it pointed at (undefinedAddress for a soft link), as H5G_obj_remove
// does (H5Gobj.c:925-975) followed by H5G__obj_remove_update_linfo
// (H5Gobj.c:817-910). A name the group does not hold is errGroupEntryNotFound.
func (fw *FileWriter) removeGroupLink(groupAddr uint64, name string) (uint64, error) {
	sb := fw.file.sb
	g, err := fw.loadLinkGroup(groupAddr)
	if err != nil {
		return 0, err
	}
	if g.dense() {
		return fw.removeDenseLink(g, name)
	}

	links, err := g.compactLinks(sb)
	if err != nil {
		return 0, err
	}
	for _, l := range links {
		if l.link.Name != name {
			continue
		}
		target, err := linkTarget(l.link, sb)
		if err != nil {
			return 0, err
		}
		// H5G__compact_remove (H5Gcompact.c:281-300); with no link left
		// the creation order starts over (H5Gobj.c:828-832).
		if len(links) == 1 {
			g.linfo.MaxCreationOrder = 0
		}
		if err := fw.rewriteWithLinks(g, map[*core.HeaderMessage]bool{l.msg: true}, nil); err != nil {
			return 0, err
		}
		return target, nil
	}
	return 0, fmt.Errorf("link %q: %w", name, errGroupEntryNotFound)
}

// removeDenseLink removes a link from a group in dense storage
// (H5G__dense_remove, H5Gdense.c:1285-1330: the name index record and the
// heap object). With no link left the dense storage is deleted
// (H5Gobj.c:836-841); with fewer than min_dense left the links go back into
// the header as link messages, in name index order, and the dense storage is
// deleted, unless a link message would be too large for a header
// (H5Gobj.c:851-895).
func (fw *FileWriter) removeDenseLink(g *linkGroup, name string) (uint64, error) {
	sb := fw.file.sb
	st, err := fw.loadDenseLinkStorage(g.linfo)
	if err != nil {
		return 0, fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	lm, err := st.remove(name)
	if err != nil {
		return 0, fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	target, err := linkTarget(lm, sb)
	if err != nil {
		return 0, err
	}

	n := st.count()
	if n == 0 {
		g.linfo.MaxCreationOrder = 0
		return target, fw.dropDenseLinks(g, nil)
	}
	if n < g.minDense {
		remaining, err := st.linkMessages()
		if err != nil {
			return 0, fmt.Errorf("group at 0x%X: %w", g.addr, err)
		}
		fits := true
		for _, data := range remaining {
			if uint64(len(data))+g.messageHeaderSize() >= maxHeaderMessageSize {
				fits = false
				break
			}
		}
		if fits {
			return target, fw.dropDenseLinks(g, remaining)
		}
	}
	if err := st.flush(fw); err != nil {
		return 0, fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	return target, nil
}

// dropDenseLinks deletes the dense link storage of g as H5G__dense_delete
// does (H5Gdense.c:1581-1650): the link info message gets undefined
// addresses and the space of the name index, the creation order index and
// the fractal heap is freed. The encoded link messages of links are appended
// to the header first. The space is checked against the allocator before the
// header is written, so a range that cannot be freed refuses the change with
// the file as it was.
func (fw *FileWriter) dropDenseLinks(g *linkGroup, links [][]byte) error {
	ranges, err := fw.denseLinkStorageRanges(g.linfo)
	if err != nil {
		return fmt.Errorf("group at 0x%X: %w", g.addr, err)
	}
	g.linfo.FractalHeapAddress = undefinedAddress
	g.linfo.NameBTreeAddress = undefinedAddress
	if g.linfo.HasCreationOrderIndex() {
		g.linfo.CreationOrderBTreeAddress = undefinedAddress
	}
	if err := fw.rewriteWithLinks(g, nil, links); err != nil {
		return err
	}
	alloc := fw.writer.Allocator()
	for _, r := range ranges {
		if err := alloc.Free(r.Offset, r.Size); err != nil {
			return fmt.Errorf("free dense link storage at %d: %w", r.Offset, err)
		}
	}
	return nil
}

// lookupGroupLink returns the address of the object the link named name of
// the group at groupAddr points at (undefinedAddress for a soft link), as
// H5G__obj_lookup does (H5Gobj.c:1056-1100) through H5G__compact_lookup or
// H5G__dense_lookup. A name the group does not hold is errGroupEntryNotFound.
func (fw *FileWriter) lookupGroupLink(groupAddr uint64, name string) (uint64, error) {
	sb := fw.file.sb
	g, err := fw.loadLinkGroup(groupAddr)
	if err != nil {
		return 0, err
	}
	links, err := fw.groupLinks(g)
	if err != nil {
		return 0, err
	}
	for _, lm := range links {
		if lm.Name == name {
			return linkTarget(lm, sb)
		}
	}
	return 0, fmt.Errorf("link %q: %w", name, errGroupEntryNotFound)
}

// groupLinks returns every link of g: its link messages, or the links of its
// dense storage read through the checked reader (any depth of name index).
func (fw *FileWriter) groupLinks(g *linkGroup) ([]*core.LinkMessage, error) {
	sb := fw.file.sb
	if !g.dense() {
		compact, err := g.compactLinks(sb)
		if err != nil {
			return nil, err
		}
		links := make([]*core.LinkMessage, len(compact))
		for i, l := range compact {
			links[i] = l.link
		}
		return links, nil
	}
	objs, err := core.ReadDenseHeapObjects(fw.writer.Reader(), g.linfo.NameBTreeAddress, g.linfo.FractalHeapAddress, sb)
	if err != nil {
		return nil, fmt.Errorf("group at 0x%X: dense link storage: %w", g.addr, err)
	}
	links := make([]*core.LinkMessage, 0, len(objs))
	for _, data := range objs {
		lm, err := core.ParseLinkMessage(data, sb)
		if err != nil {
			return nil, fmt.Errorf("%w: group at 0x%X: dense link message: %w", utils.ErrCorrupt, g.addr, err)
		}
		links = append(links, lm)
	}
	return links, nil
}
