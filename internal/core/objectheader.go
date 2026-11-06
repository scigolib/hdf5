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
	MsgFillValueOld   MessageType = 4
	MsgFillValue      MessageType = 5  // Alias for FillValueOld
	MsgDataLayout     MessageType = 8  // Corrected: Data Layout is 0x0008
	MsgFilterPipeline MessageType = 11 // Filter Pipeline (compression, etc)
	MsgAttribute      MessageType = 12
	MsgName           MessageType = 13 // Corrected: Name is 0x000D
	MsgAttributeInfo  MessageType = 15 // Attribute Info (0x000F) - for dense attribute storage
	MsgContinuation   MessageType = 16 // Object header continuation (0x0010)
	MsgSymbolTable    MessageType = 17
	MsgLinkMessage    MessageType = 6
	MsgRefCount       MessageType = 22 // Reference Count (0x0016) - for hard links (v2 only)
)

// ReadObjectHeader reads and parses an HDF5 object header from the specified address.
// It supports both version 1 and version 2 object header formats.
func ReadObjectHeader(r io.ReaderAt, address uint64, sb *Superblock) (*ObjectHeader, error) {
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	offset := int64(address)
	if offset < 0 {
		return nil, fmt.Errorf("negative offset: %d", offset)
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
		return nil, fmt.Errorf("invalid object header signature: % x", prefix[0:4])
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

	// Parse attributes from messages (both compact and dense)
	attributes, err := ParseAttributesFromMessages(r, header.Messages, sb)
	if err != nil {
		// Don't fail the whole header read if attributes fail
		// Attributes are optional - continue without them
		_ = err
	} else {
		header.Attributes = attributes
	}

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

	// Determine message header size based on flags.
	// V2 message format: Type (1) + Size (2) + Flags (1) = 4 bytes
	// If H5O_HDR_ATTR_CRT_ORDER_TRACKED (bit 2 = 0x04) is set,
	// there's an additional 2-byte creation index, making it 6 bytes.
	msgHeaderSize := uint64(4)
	if flags&0x04 != 0 {
		msgHeaderSize = 6
	}

	for current < end {
		msgType, msgSize, msgFlags, err := readV2MessageHeader(r, current, isBE)
		if err != nil {
			return nil, "", utils.WrapError("message header read failed", err)
		}

		if msgSize == 0 {
			current += msgHeaderSize
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

		messages = append(messages, &HeaderMessage{
			Type:   msgType,
			Offset: current,
			Data:   data,
			Flags:  msgFlags,
		})

		current += msgHeaderSize + uint64(msgSize)
	}

	// Follow continuation messages (type 0x0010) to OCHK blocks.
	// V2 OCHK format: "OCHK"(4) + messages + Jenkins checksum(4).
	// We reuse the V1 continuation infrastructure for parsing the continuation
	// message data (address + size), but V2 OCHK blocks have a different layout.
	if sb != nil {
		continuations := findContinuations(messages, sb)
		for len(continuations) > 0 {
			cont := continuations[0]
			continuations = continuations[1:]

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
				newConts := findContinuations(contMessages, sb)
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
	msgStart := blockAddr + 4
	msgEnd := blockAddr + blockSize - 4

	// Determine message header size (same as main OHDR).
	msgHeaderSize := uint64(4)
	if flags&0x04 != 0 {
		msgHeaderSize = 6
	}

	var messages []*HeaderMessage
	var name string
	current := msgStart

	for current < msgEnd {
		msgType, msgSize, msgFlags, err := readV2MessageHeader(r, current, isBE)
		if err != nil {
			return nil, "", utils.WrapError("OCHK message header read failed", err)
		}

		if msgSize == 0 {
			current += msgHeaderSize
			continue
		}

		data := utils.GetBuffer(int(msgSize))
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(data, int64(current+msgHeaderSize)); err != nil {
			utils.ReleaseBuffer(data)
			return nil, "", utils.WrapError("OCHK message data read failed", err)
		}

		if msgType == MsgName && len(data) > 1 {
			name = string(data[1:])
		}

		messages = append(messages, &HeaderMessage{
			Type:   msgType,
			Offset: current,
			Data:   data,
			Flags:  msgFlags,
		})

		current += msgHeaderSize + uint64(msgSize)
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
// creation index the caller skips when tracked (H5Ocache.c:1258-1279). Six
// bytes are read, enough for either header size.
func readV2MessageHeader(r io.ReaderAt, addr uint64, isBE bool) (MessageType, uint16, uint8, error) {
	headerBuf := utils.GetBuffer(6)
	defer utils.ReleaseBuffer(headerBuf)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(headerBuf, int64(addr)); err != nil {
		return 0, 0, 0, err
	}
	order := binary.ByteOrder(binary.LittleEndian)
	if isBE {
		order = binary.BigEndian
	}
	return MessageType(headerBuf[0]), order.Uint16(headerBuf[1:3]), headerBuf[3], nil
}

// Optional version-2 object header prefix fields (H5Opkg.h H5O_HDR_* flags).
const (
	ohdrFlagStoreTimes       = 0x20 // H5O_HDR_STORE_TIMES: 4 x 4-byte times
	ohdrFlagAttrPhaseChange  = 0x10 // H5O_HDR_ATTR_STORE_PHASE_CHANGE: 2 x 2-byte counts
	ohdrFlagAttrCrtOrder     = 0x0C // H5O_HDR_ATTR_CRT_ORDER_TRACKED | _INDEXED
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
