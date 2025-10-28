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
}

// HeaderMessage represents a single message within an object header.
type HeaderMessage struct {
	Type   MessageType
	Offset uint64
	Data   []byte
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
		header.Messages, header.Name, err = parseV1Header(r, address, sb)
		if err != nil {
			return nil, utils.WrapError("v1 header parse failed", err)
		}
	case 2:
		header.Messages, header.Name, err = parseV2Header(r, address, header.Flags, sb, isBE)
		if err != nil {
			return nil, utils.WrapError("v2 header parse failed", err)
		}
	default:
		return nil, fmt.Errorf("unsupported object header version: %d", header.Version)
	}

	header.Type = determineObjectType(header.Messages)

	// Parse attributes from messages (both compact and dense)
	attributes, err := ParseAttributesFromMessages(r, header.Messages, sb)
	//nolint:revive // empty-block: Error intentionally ignored, attributes are optional
	if err != nil {
		// Don't fail the whole header read if attributes fail
		// Just log and continue
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

func parseV2Header(r io.ReaderAt, headerAddr uint64, flags uint8, _ *Superblock, isBE bool) ([]*HeaderMessage, string, error) {
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

	//nolint:gosec // G115: Safe conversion for HDF5 structure sizes
	current += uint64(chunkSizeBytes)
	end := current + chunkSize

	for current < end {
		// V2 message format: Type (1 byte) + Size (2 bytes) + Flags (1 byte) = 4 bytes header
		typeSizeBuf := utils.GetBuffer(4)
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(typeSizeBuf, int64(current)); err != nil {
			utils.ReleaseBuffer(typeSizeBuf)
			return nil, "", utils.WrapError("message header read failed", err)
		}

		// Type is 1 byte, size is 2 bytes, flags is 1 byte
		msgType := MessageType(typeSizeBuf[0])
		var msgSize uint16
		if isBE {
			msgSize = binary.BigEndian.Uint16(typeSizeBuf[1:3])
		} else {
			msgSize = binary.LittleEndian.Uint16(typeSizeBuf[1:3])
		}
		msgFlags := typeSizeBuf[3]
		_ = msgFlags // Unused for now
		utils.ReleaseBuffer(typeSizeBuf)

		if msgSize == 0 {
			current += 4
			continue
		}

		data := utils.GetBuffer(int(msgSize))
		//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
		if _, err := r.ReadAt(data, int64(current+4)); err != nil {
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
		})

		current += 4 + uint64(msgSize)
	}

	return messages, name, nil
}
