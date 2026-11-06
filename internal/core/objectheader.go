package core

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

// ObjectType identifies the type of HDF5 object (group, dataset, datatype).
type ObjectType uint8

// Object type constants identify different HDF5 object types.
const (
	ObjectTypeGroup ObjectType = iota
	ObjectTypeDataset
	ObjectTypeDatatype
	ObjectTypeUnknown
)

// ObjectHeader represents an HDF5 object header containing metadata messages.
type ObjectHeader struct {
	Version    uint8
	Flags      uint8
	Type       ObjectType
	Messages   []*HeaderMessage
	Name       string
	Attributes []*Attribute

	// AttributesErr is the error that stopped the attributes from being read;
	// Attributes is then nil. The header itself is valid without them.
	AttributesErr error

	// Times and AttrPhaseChange are the optional version-2 prefix fields,
	// kept verbatim so a rewritten header keeps them (decoded at
	// H5Ocache.c:404-418, encoded at :1030-1055): the access, modification,
	// change and birth times (flag 0x20, 16 bytes) and the max compact / min
	// dense attribute counts (flag 0x10, 4 bytes).
	Times           []byte
	AttrPhaseChange []byte

	// ReferenceCount tracks the number of hard links to this object.
	// For V1 headers: Stored directly in header (bytes 4-7).
	// For V2 headers: Stored in RefCount message (type 0x0016) if >1.
	// Default value is 1 (single link). Incremented when hard links are created.
	ReferenceCount uint32
}

// HeaderMessage represents a single message within an object header.
type HeaderMessage struct {
	Type   MessageType
	Offset uint64
	Data   []byte

	// Flags are the message flags of the header message (format spec
	// IV.A.1, H5Ocache.c:1277-1279). MsgFlagShared means Data is a
	// shared-message reference (H5Oshared.h), not the message itself.
	Flags uint8

	// CreationIndex is the creation index of the message header, present
	// when the version 2 object header tracks attribute creation order
	// (H5Ocache.c:1301-1304). It orders compact attributes; C writes 0 for
	// every other message (H5Omessage.c:1969-1974).
	CreationIndex uint16

	// FromContinuation is true if this message was read from an OCHK
	// continuation block rather than the main OHDR chunk. Used by the
	// write path to avoid rewriting continuation messages into the main header.
	FromContinuation bool
}

// MessageType identifies the type of message in an object header.
type MessageType uint16

// Message type constants identify different types of header messages.
const (
	MsgNil            MessageType = 0
	MsgDataspace      MessageType = 1
	MsgLinkInfo       MessageType = 2
	MsgDatatype       MessageType = 3
	MsgFillValueOld   MessageType = 4  // Old fill value message (H5O_FILL_ID, H5Oprivate.h:211)
	MsgFillValue      MessageType = 5  // Fill value message (H5O_FILL_NEW_ID, H5Oprivate.h:212)
	MsgDataLayout     MessageType = 8  // Corrected: Data Layout is 0x0008
	MsgExternalFiles  MessageType = 7  // External File List (H5O_EFL_ID, H5Oprivate.h:214)
	MsgFilterPipeline MessageType = 11 // Filter Pipeline (compression, etc)
	MsgAttribute      MessageType = 12
	MsgName           MessageType = 13 // Corrected: Name is 0x000D
	MsgContinuation   MessageType = 16 // Object header continuation (0x0010)
	MsgSymbolTable    MessageType = 17
	MsgLinkMessage    MessageType = 6
	MsgRefCount       MessageType = 22 // Reference Count (0x0016) - for hard links (v2 only)

	// MsgAttributeInfo is the attribute info message of dense attribute
	// storage (H5O_AINFO_ID 0x0015, H5Oprivate.h:228).
	MsgAttributeInfo MessageType = 21

	// MsgSharedMessageTable is the shared object header message table
	// (H5O_SHMESG_ID 0x000F, H5Oprivate.h:222). C writes it only in the
	// superblock extension's header. This library up to v0.14.2 wrote its
	// attribute info messages with this ID in ordinary object headers; the
	// readers accept that legacy layout there (attributeInfoMessage).
	MsgSharedMessageTable MessageType = 15
)

// ReadObjectHeader reads and parses an HDF5 object header from the specified address.
// It supports both version 1 and version 2 object header formats.
func ReadObjectHeader(r io.ReaderAt, address uint64, sb *Superblock) (*ObjectHeader, error) {
	return readObjectHeader(r, address, sb, true)
}

// readObjectHeader reads the object header at address. withAttributes parses
// the attribute messages into ObjectHeader.Attributes; resolving a shared
// message reads its target header without them (resolveSharedMessage), so an
// attribute typed by the committed datatype it is attached to cannot start a
// resolution loop.
func readObjectHeader(r io.ReaderAt, address uint64, sb *Superblock, withAttributes bool) (*ObjectHeader, error) {
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	offset := int64(address)
	if offset < 0 {
		return nil, fmt.Errorf("%w: object header address 0x%X is out of range", utils.ErrCorrupt, address)
	}

	prefix := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(prefix)

	if _, err := r.ReadAt(prefix, offset); err != nil {
		return nil, utils.WrapError("object header read failed", err)
	}

	header := &ObjectHeader{}

	// Determine version and format
	// V1: No signature, version byte at offset 0
	// V2: "OHDR" signature at offset 0-3, version at offset 4 or 7
	isBE := false
	var version uint8

	//nolint:gocritic // ifElseChain: complex signature detection, not suitable for switch
	if string(prefix[0:4]) == "OHDR" {
		// V2 little-endian
		version = prefix[4]
		header.Flags = prefix[5]
	} else if string([]byte{prefix[3], prefix[2], prefix[1], prefix[0]}) == "OHDR" {
		// V2 big-endian
		isBE = true
		version = prefix[7]
		header.Flags = prefix[6]
	} else if prefix[0] == 1 && prefix[1] == 0 {
		// V1: version=1, reserved=0
		version = 1
		header.Flags = 0
	} else {
		// C: "bad object header version number" (H5Ocache.c:1017, :1101,
		// H5O__prefix_deserialize).
		return nil, fmt.Errorf("%w: invalid object header signature: % x", utils.ErrCorrupt, prefix[0:4])
	}

	header.Version = version

	var err error
	switch header.Version {
	case 1:
		header.Messages, header.Name, header.ReferenceCount, err = parseV1Header(r, address, sb)
		if err != nil {
			return nil, utils.WrapError("v1 header parse failed", err)
		}
	case 2:
		header.Messages, header.Name, err = parseV2Header(r, address, header.Flags, sb, isBE)
		if err != nil {
			return nil, utils.WrapError("v2 header parse failed", err)
		}
		header.Times, header.AttrPhaseChange, err = readV2PrefixFields(r, address, header.Flags)
		if err != nil {
			return nil, utils.WrapError("v2 header prefix read failed", err)
		}
		// For V2, reference count defaults to 1 (may be overridden by RefCount message)
		header.ReferenceCount = 1
	default:
		return nil, fmt.Errorf("unsupported object header version: %d", header.Version)
	}

	header.Type = determineObjectType(header.Messages)

	// Check for RefCount message (V2 only) - overrides default
	if header.Version == 2 {
		for _, msg := range header.Messages {
			// RefCount message: version byte (0) + uint32 count.
			if msg.Type == MsgRefCount && len(msg.Data) >= 5 && msg.Data[0] == 0 {
				header.ReferenceCount = sb.Endianness.Uint32(msg.Data[1:5])
				break
			}
			if msg.Type == MsgRefCount && len(msg.Data) == 4 {
				// Files written by this library before v0.15 omitted the
				// version byte; accept the bare uint32 layout.
				header.ReferenceCount = sb.Endianness.Uint32(msg.Data[0:4])
				break
			}
		}
	}

	if !withAttributes {
		return header, nil
	}

	// Parse attributes from messages (both compact and dense). A header whose
	// attributes cannot be read still opens: C decodes attributes only when
	// they are accessed (H5O__attr_decode runs from H5A iteration), so a bad
	// attribute fails attribute access, not the object. The error is kept for
	// the attribute accessors to return.
	header.Attributes, header.AttributesErr = ParseAttributesFromMessages(r, header.Messages, sb)

	return header, nil
}

func determineObjectType(messages []*HeaderMessage) ObjectType {
	// First pass: look for definitive type indicators
	// Dataspace message indicates a dataset (datasets also have Datatype messages)
	for _, msg := range messages {
		switch msg.Type {
		case MsgSymbolTable, MsgLinkInfo, MsgLinkMessage:
			return ObjectTypeGroup
		case MsgDataspace:
			// Dataspace is definitive - this is a dataset
			// (even though it may also have a Datatype message)
			return ObjectTypeDataset
		}
	}

	// Second pass: check for standalone datatype
	for _, msg := range messages {
		if msg.Type == MsgDatatype {
			return ObjectTypeDatatype
		}
	}

	return ObjectTypeUnknown
}

func parseV2Header(r io.ReaderAt, headerAddr uint64, flags uint8, sb *Superblock, isBE bool) ([]*HeaderMessage, string, error) {
	var messages []*HeaderMessage
	var name string

	// Start after signature (4) + version (1) + flags (1) = 6 bytes
	current := headerAddr + 6

	// According to H5Opublic.h flag definitions:
	// Bit 0-1 (0x03): H5O_HDR_CHUNK0_SIZE - determines chunk size field size
	// Bit 2 (0x04): H5O_HDR_ATTR_CRT_ORDER_TRACKED
	// Bit 3 (0x08): H5O_HDR_ATTR_CRT_ORDER_INDEXED
	// Bit 4 (0x10): H5O_HDR_ATTR_STORE_PHASE_CHANGE
	// Bit 5 (0x20): H5O_HDR_STORE_TIMES - Store access/modification/change/birth times

	// Check for time fields (bit 5 = 0x20)
	if flags&0x20 != 0 {
		// Skip 4 time fields (4 bytes each = 16 bytes total)
		current += 16
	}

	// Check for non-default attribute phase change (bit 4 = 0x10)
	if flags&0x10 != 0 {
		// Skip max compact attributes (2 bytes) and min dense attributes (2 bytes)
		current += 4
	}

	// Determine chunk size encoding from bits 0-1 (H5O_HDR_CHUNK0_SIZE = 0x03)
	// 00 (0) = 1 byte, 01 (1) = 2 bytes, 10 (2) = 4 bytes, 11 (3) = 8 bytes
	sizeFieldType := flags & 0x03
	chunkSizeBytes := 1 << sizeFieldType // 1, 2, 4, or 8

	// Read chunk size
	sizeBuf := utils.GetBuffer(chunkSizeBytes)
	defer utils.ReleaseBuffer(sizeBuf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(sizeBuf, int64(current)); err != nil {
		return nil, "", utils.WrapError("chunk size read failed", err)
	}

	var chunkSize uint64
	switch chunkSizeBytes {
	case 1:
		chunkSize = uint64(sizeBuf[0])
	case 2:
		if isBE {
			chunkSize = uint64(binary.BigEndian.Uint16(sizeBuf))
		} else {
			chunkSize = uint64(binary.LittleEndian.Uint16(sizeBuf))
		}
	case 4:
		if isBE {
			chunkSize = uint64(binary.BigEndian.Uint32(sizeBuf))
		} else {
			chunkSize = uint64(binary.LittleEndian.Uint32(sizeBuf))
		}
	case 8:
		if isBE {
			chunkSize = binary.BigEndian.Uint64(sizeBuf)
		} else {
			chunkSize = binary.LittleEndian.Uint64(sizeBuf)
		}
	}

	current += uint64(chunkSizeBytes)
	// Per HDF5 C reference (H5Ocache.c:1207, H5Opkg.h:85-107):
	// chunk_size in file = message data ONLY (does not include checksum).
	// The 4-byte Jenkins lookup3 checksum follows immediately after the messages.
	end := current + chunkSize

	messages, name, err := parseV2ChunkMessages(r, current, end, flags, isBE)
	if err != nil {
		return nil, "", err
	}

	// Follow continuation messages (type 0x0010) to OCHK blocks.
	// V2 OCHK format: "OCHK"(4) + messages + Jenkins checksum(4).
	// We reuse the V1 continuation infrastructure for parsing the continuation
	// message data (address + size), but V2 OCHK blocks have a different layout.
	if sb != nil {
		continuations, err := findContinuations(messages, sb)
		if err != nil {
			return nil, "", err
		}
		walk := newContinuationWalk(r, sb, headerAddr)
		for len(continuations) > 0 {
			cont := continuations[0]
			continuations = continuations[1:]
			if err := walk.admit(cont); err != nil {
				return nil, "", err
			}

			contMessages, contName, err := parseV2ContinuationBlock(r, cont.Address, cont.Size, flags, isBE)
			if err != nil {
				return nil, "", utils.WrapError("V2 continuation block parse failed", err)
			}

			// Mark continuation messages so the write path can exclude them.
			for _, cm := range contMessages {
				cm.FromContinuation = true
			}
			messages = append(messages, contMessages...)
			if contName != "" && name == "" {
				name = contName
			}

			// Check for chained continuations inside the OCHK block.
			if sb != nil {
				newConts, err := findContinuations(contMessages, sb)
				if err != nil {
					return nil, "", err
				}
				continuations = append(continuations, newConts...)
			}
		}
	}

	return messages, name, nil
}

// parseV2ContinuationBlock parses messages from a V2 OCHK continuation block.
//
// OCHK format (per H5Opkg.h):
//   - "OCHK" signature (4 bytes)
//   - Messages (same format as main OHDR v2)
//   - Jenkins lookup3 checksum (4 bytes)
func parseV2ContinuationBlock(r io.ReaderAt, blockAddr, blockSize uint64, flags uint8, isBE bool) ([]*HeaderMessage, string, error) {
	if blockSize < 8 {
		return nil, "", fmt.Errorf("OCHK block too small: %d bytes (minimum 8)", blockSize)
	}

	// Read signature.
	sigBuf := utils.GetBuffer(4)
	defer utils.ReleaseBuffer(sigBuf)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(sigBuf, int64(blockAddr)); err != nil {
		return nil, "", utils.WrapError("OCHK signature read failed", err)
	}
	if string(sigBuf) != "OCHK" {
		return nil, "", fmt.Errorf("invalid OCHK signature: % x", sigBuf)
	}

	// Messages start after "OCHK" (4 bytes) and end before checksum (4 bytes).
	return parseV2ChunkMessages(r, blockAddr+4, blockAddr+blockSize-4, flags, isBE)
}

// parseV2ChunkMessages decodes the messages of one version-2 object header
// chunk, [start, end) without the checksum, as H5O__chunk_deserialize does
// (H5Ocache.c:1246-1522). A message header is 4 bytes, 6 when the header
// tracks attribute creation order (H5O_SIZEOF_MSGHDR_VERS, H5Opkg.h:112-122).
//
// A tail shorter than a message header is the chunk's gap, which C skips
// (H5Ocache.c:1507-1521): the writer leaves one when a message shrinks and
// the remainder cannot hold a null message. C refuses a gap in a chunk that
// has null messages (the space would have gone into one) and a chunk that is
// a gap only. A message running past the chunk end is corrupt: it would be
// read from the checksum or the bytes after the chunk.
//
// The gap length is not kept: callers get the messages only.
func parseV2ChunkMessages(r io.ReaderAt, start, end uint64, flags uint8, isBE bool) ([]*HeaderMessage, string, error) {
	msgHeaderSize := V2MessageHeaderSize(flags)
	tracked := flags&ohdrFlagAttrCrtTracked != 0

	var messages []*HeaderMessage
	var name string
	decoded, nulls := 0, 0
	for current := start; current < end; {
		if gap := end - current; gap < msgHeaderSize {
			if decoded == 0 || nulls != 0 {
				return nil, "", fmt.Errorf("%w: object header chunk at 0x%x has a %d-byte gap after %d messages, %d of them null",
					utils.ErrCorrupt, start, gap, decoded, nulls)
			}
			break
		}

		msgType, msgSize, msgFlags, crtIdx, err := readV2MessageHeader(r, current, isBE)
		if err != nil {
			return nil, "", utils.WrapError("message header read failed", err)
		}
		next := current + msgHeaderSize + uint64(msgSize)
		if next > end {
			return nil, "", fmt.Errorf("%w: object header message at 0x%x (%d bytes) runs past its chunk's end 0x%x",
				utils.ErrCorrupt, current, msgSize, end)
		}
		decoded++
		if msgType == MsgNil {
			nulls++
		}

		if msgSize == 0 {
			current = next
			continue
		}

		data := utils.GetBuffer(int(msgSize))
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(data, int64(current+msgHeaderSize)); err != nil {
			utils.ReleaseBuffer(data)
			return nil, "", utils.WrapError("message data read failed", err)
		}

		if msgType == MsgName && len(data) > 1 {
			name = string(data[1:])
		}

		msg := &HeaderMessage{
			Type:   msgType,
			Offset: current,
			Data:   data,
			Flags:  msgFlags,
		}
		if tracked {
			msg.CreationIndex = crtIdx
		}
		messages = append(messages, msg)
		current = next
	}

	return messages, name, nil
}

// IncrementReferenceCount increments the reference count for this object header.
// This should be called when creating a new hard link to the object.
//
// Reference counting behavior:
//   - Each object starts with refcount = 1 (original link)
//   - Each additional hard link increments the count
//   - When links are deleted, the count is decremented
//   - Object is deleted when refcount reaches 0
//
// Returns the new reference count after increment.
func (oh *ObjectHeader) IncrementReferenceCount() uint32 {
	oh.ReferenceCount++
	return oh.ReferenceCount
}

// DecrementReferenceCount decrements the reference count for this object header.
// This should be called when removing a hard link to the object.
//
// Returns the new reference count after decrement, or 0 if already at 0.
func (oh *ObjectHeader) DecrementReferenceCount() uint32 {
	if oh.ReferenceCount > 0 {
		oh.ReferenceCount--
	}
	return oh.ReferenceCount
}

// GetReferenceCount returns the current reference count for this object header.
// The reference count indicates how many hard links point to this object.
//
// Returns:
//   - 0: Object has no links (should be deleted)
//   - 1: Object has one link (normal case)
//   - >1: Object has multiple hard links
func (oh *ObjectHeader) GetReferenceCount() uint32 {
	return oh.ReferenceCount
}

// readV2MessageHeader reads the header of the version-2 object header message
// at addr: type (1 byte), size (2 bytes) and flags (1 byte), followed by a
// 2-byte creation index when the header tracks attribute creation order
// (H5Ocache.c:1258-1304). Six bytes are read, enough for either header size;
// the caller ignores the creation index of an untracked header.
func readV2MessageHeader(r io.ReaderAt, addr uint64, isBE bool) (MessageType, uint16, uint8, uint16, error) {
	headerBuf := utils.GetBuffer(6)
	defer utils.ReleaseBuffer(headerBuf)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(headerBuf, int64(addr)); err != nil {
		return 0, 0, 0, 0, err
	}
	order := binary.ByteOrder(binary.LittleEndian)
	if isBE {
		order = binary.BigEndian
	}
	return MessageType(headerBuf[0]), order.Uint16(headerBuf[1:3]), headerBuf[3], order.Uint16(headerBuf[4:6]), nil
}

// V2MessageHeaderSize is the size of a message header in a version 2 object
// header with the given flags: type, size and flags, plus a 2-byte creation
// index when the header tracks attribute creation order (H5O_SIZEOF_MSGHDR_VERS,
// H5Opkg.h:112-122).
func V2MessageHeaderSize(ohFlags uint8) uint64 {
	if ohFlags&ohdrFlagAttrCrtTracked != 0 {
		return 6
	}
	return 4
}

// TracksAttrCreationOrder reports whether a version 2 object header tracks
// attribute creation order (H5O_HDR_ATTR_CRT_ORDER_TRACKED).
func (oh *ObjectHeader) TracksAttrCreationOrder() bool {
	return oh.Version == 2 && oh.Flags&ohdrFlagAttrCrtTracked != 0
}

// IndexesAttrCreationOrder reports whether a version 2 object header indexes
// attribute creation order (H5O_HDR_ATTR_CRT_ORDER_INDEXED).
func (oh *ObjectHeader) IndexesAttrCreationOrder() bool {
	return oh.Version == 2 && oh.Flags&ohdrFlagAttrCrtIndexed != 0
}

// Optional version-2 object header prefix fields (H5Opkg.h H5O_HDR_* flags).
const (
	ohdrFlagStoreTimes       = 0x20 // H5O_HDR_STORE_TIMES: 4 x 4-byte times
	ohdrFlagAttrPhaseChange  = 0x10 // H5O_HDR_ATTR_STORE_PHASE_CHANGE: 2 x 2-byte counts
	ohdrFlagAttrCrtTracked   = 0x04 // H5O_HDR_ATTR_CRT_ORDER_TRACKED: messages carry a 2-byte creation index
	ohdrFlagAttrCrtIndexed   = 0x08 // H5O_HDR_ATTR_CRT_ORDER_INDEXED
	ohdrTimesSize            = 16
	ohdrAttrPhaseChangeSize  = 4
	ohdrPrefixFieldsStartOff = 6 // after "OHDR", version and flags
)

// readV2PrefixFields reads the optional times and attribute phase change
// fields of the version-2 header at addr, which follow the flags byte
// (H5Ocache.c:404-418).
func readV2PrefixFields(r io.ReaderAt, addr uint64, flags uint8) (times, phase []byte, err error) {
	off := addr + ohdrPrefixFieldsStartOff
	if flags&ohdrFlagStoreTimes != 0 {
		times = make([]byte, ohdrTimesSize)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(times, int64(off)); err != nil {
			return nil, nil, err
		}
		off += ohdrTimesSize
	}
	if flags&ohdrFlagAttrPhaseChange != 0 {
		phase = make([]byte, ohdrAttrPhaseChangeSize)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(phase, int64(off)); err != nil {
			return nil, nil, err
		}
	}
	return times, phase, nil
}
