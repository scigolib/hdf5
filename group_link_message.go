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
// max_compact links, and in a fractal heap with a v2 B-tree index ("dense"
// storage, H5Gdense.c) beyond that. The writer creates symbol-table groups
// only; this file adds a link to a compact group of an existing file, as
// H5G_obj_insert does (H5Gobj.c:422-580). The switch to dense storage is not
// implemented.

// msgGroupInfo is the group info message (H5O_GINFO_ID, H5Oprivate.h:217).
const msgGroupInfo core.MessageType = 10

// Group info message layout (H5Oginfo.c:85-140).
const (
	groupInfoVersion         = 0
	groupInfoPhaseChangeFlag = 0x01 // H5O_GINFO_STORE_PHASE_CHANGE
	groupInfoEstEntryFlag    = 0x02 // H5O_GINFO_STORE_EST_ENTRY_INFO
	groupInfoMaxCompactDef   = 8    // H5G_CRT_GINFO_MAX_COMPACT, H5Gprivate.h:74
)

// maxHeaderMessageSize is H5O_MESG_MAX_SIZE (H5Oprivate.h:67): a link message
// this large goes to dense storage in C (H5Gobj.c:474).
const maxHeaderMessageSize = 65536

// rootHasSymbolTable reports whether the root group stores its links in a
// symbol table. OpenForWrite reads the table's addresses from the superblock
// or from the root's symbol table message; neither exists for a group of the
// 1.8 format.
func (fw *FileWriter) rootHasSymbolTable() bool {
	return fw.rootHeapAddr != 0 || fw.rootBTreeAddr != 0
}

// parseGroupInfoMaxCompact returns max_compact from a group info message as
// H5O__ginfo_decode reads it (H5Oginfo.c:85-140): stored when the
// phase-change flag is set, the library default otherwise.
func parseGroupInfoMaxCompact(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("%w: group info message of %d bytes", utils.ErrCorrupt, len(data))
	}
	if data[0] != groupInfoVersion {
		return 0, fmt.Errorf("%w: group info message version %d", utils.ErrCorrupt, data[0])
	}
	flags := data[1]
	if flags&^(groupInfoPhaseChangeFlag|groupInfoEstEntryFlag) != 0 {
		return 0, fmt.Errorf("%w: group info message flags 0x%02X", utils.ErrCorrupt, flags)
	}
	if flags&groupInfoPhaseChangeFlag == 0 {
		return groupInfoMaxCompactDef, nil
	}
	if len(data) < 4 {
		return 0, fmt.Errorf("%w: group info message of %d bytes has no max_compact", utils.ErrCorrupt, len(data))
	}
	return int(binary.LittleEndian.Uint16(data[2:4])), nil
}

// addressDefined reports whether addr, read with the file's offset size, is
// a defined address (H5_addr_defined): all bits set is HADDR_UNDEF.
func addressDefined(addr uint64, offsetSize uint8) bool {
	return addr != undefinedAddress>>(64-8*uint(offsetSize))
}

// newLinkMessage builds the link message H5O__link_encode writes for a hard
// link to childAddr, or a soft link to softTarget when it is not empty
// (H5Olink.c:296-380): an ASCII name (the default link creation property
// list), the narrowest name length field, the link type stored only for a
// soft link, and the creation order when the group tracks it.
func newLinkMessage(sb *core.Superblock, name string, childAddr uint64, softTarget string, corder int64, corderValid bool) (*core.LinkMessage, error) {
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
	if softTarget == "" {
		lm.LinkValue = make([]byte, sb.OffsetSize)
		writeAddressLE(lm.LinkValue, childAddr)
		return lm, nil
	}
	if len(softTarget) > math.MaxUint16 {
		return nil, fmt.Errorf("soft link target of %d bytes exceeds the 16-bit length field", len(softTarget))
	}
	lm.Type = core.LinkTypeSoft
	lm.Flags |= core.LinkFlagLinkTypeFieldBit
	lm.LinkValue = binary.LittleEndian.AppendUint16(nil, uint16(len(softTarget))) //nolint:gosec // G115: checked above
	lm.LinkValue = append(lm.LinkValue, softTarget...)
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

// linkMessageToGroup adds a link named childName to the compact group whose
// object header is at groupAddr, as H5G_obj_insert does for a group with a
// link info message (H5Gobj.c:440-475, 568-579): the creation order comes
// from the link info message, which is rewritten with the next value; the
// link message is appended to the header. A group already in dense storage,
// or one that would exceed max_compact, needs the conversion to dense
// storage, which is not implemented.
//
//nolint:gocognit,gocyclo,cyclop // One pass over the header collects what H5G_obj_insert reads with three lookups.
func (fw *FileWriter) linkMessageToGroup(groupAddr uint64, childName string, childAddr uint64, softTarget string) error {
	sb := fw.file.sb
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), groupAddr, sb)
	if err != nil {
		return fmt.Errorf("read group object header at 0x%X: %w", groupAddr, err)
	}

	var linfoMsg *core.HeaderMessage
	maxCompact := groupInfoMaxCompactDef
	nlinks := 0
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgLinkInfo:
			linfoMsg = msg
		case msgGroupInfo:
			if maxCompact, err = parseGroupInfoMaxCompact(msg.Data); err != nil {
				return err
			}
		case core.MsgLinkMessage:
			lm, parseErr := core.ParseLinkMessage(msg.Data, sb)
			if parseErr != nil {
				return fmt.Errorf("parse link message: %w", parseErr)
			}
			if lm.Name == childName {
				return fmt.Errorf("link %q already exists in the group", childName)
			}
			nlinks++
		}
	}
	if linfoMsg == nil {
		return fmt.Errorf("%w: group at 0x%X has neither a symbol table nor a link info message",
			utils.ErrCorrupt, groupAddr)
	}
	linfo, err := core.ParseLinkInfoMessage(linfoMsg.Data, sb)
	if err != nil {
		return fmt.Errorf("parse link info message: %w", err)
	}
	if addressDefined(linfo.FractalHeapAddress, sb.OffsetSize) {
		return fmt.Errorf("%w: adding a link to a group with dense link storage", ErrNotSupported)
	}
	if nlinks >= maxCompact {
		return fmt.Errorf("%w: group holds %d links (max_compact %d); converting it to dense link storage",
			ErrNotSupported, nlinks, maxCompact)
	}

	trackCorder := linfo.HasCreationOrderTracking()
	lm, err := newLinkMessage(sb, childName, childAddr, softTarget, linfo.MaxCreationOrder, trackCorder)
	if err != nil {
		return err
	}
	linkData, err := core.EncodeLinkMessage(lm, sb)
	if err != nil {
		return fmt.Errorf("encode link message: %w", err)
	}
	if len(linkData) >= maxHeaderMessageSize {
		return fmt.Errorf("%w: link message of %d bytes needs dense link storage", ErrNotSupported, len(linkData))
	}

	if trackCorder {
		linfo.MaxCreationOrder++
		linfoData, encErr := core.EncodeLinkInfoMessage(linfo, sb)
		if encErr != nil {
			return fmt.Errorf("encode link info message: %w", encErr)
		}
		linfoMsg.Data = linfoData
	}
	if err := core.AddMessageToObjectHeader(oh, core.MsgLinkMessage, linkData); err != nil {
		return fmt.Errorf("add link message: %w", err)
	}
	return fw.rewriteObjectHeader(groupAddr, oh)
}
