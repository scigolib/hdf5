package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/utils"
)

type ObjectType uint8

const (
	ObjectTypeGroup ObjectType = iota
	ObjectTypeDataset
	ObjectTypeDatatype
	ObjectTypeUnknown
)

type ObjectHeader struct {
	Version  uint8
	Flags    uint8
	Type     ObjectType
	Messages []*HeaderMessage
	Name     string
}

type HeaderMessage struct {
	Type   MessageType
	Offset uint64
	Data   []byte
}

type MessageType uint16

const (
	MsgNil          MessageType = 0
	MsgDataspace    MessageType = 1
	MsgLinkInfo     MessageType = 2
	MsgDatatype     MessageType = 3
	MsgFillValueOld MessageType = 4
	MsgDataLayout   MessageType = 5
	MsgAttribute    MessageType = 12
	MsgName         MessageType = 11
	MsgSymbolTable  MessageType = 17
	MsgLinkMessage  MessageType = 18
)

func ReadObjectHeader(r io.ReaderAt, address uint64, sb *Superblock) (*ObjectHeader, error) {
	
	offset := int64(address)
	if offset < 0 {
		return nil, fmt.Errorf("negative offset: %d", offset)
	}

	prefix := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(prefix)

	if _, err := r.ReadAt(prefix, offset); err != nil {
		return nil, utils.WrapError("object header read failed", err)
	}

	// Улучшенное определение порядка байт
	isBE := false
	if string(prefix[0:4]) == "OHDR" {
		// Little-endian
	} else if string([]byte{prefix[3], prefix[2], prefix[1], prefix[0]}) == "OHDR" {
		isBE = true
	} else {
		return nil, fmt.Errorf("invalid object header signature: % x", prefix[0:4])
	}

	header := &ObjectHeader{}
	if isBE {
		header.Version = prefix[7]
		header.Flags = prefix[6]
	} else {
		header.Version = prefix[4]
		header.Flags = prefix[5]
	}

	var err error
	switch header.Version {
	case 1:
		return nil, errors.New("version 1 headers not supported")
	case 2:
		header.Messages, header.Name, err = parseV2Header(r, address+8, sb, isBE)
		if err != nil {
			return nil, utils.WrapError("v2 header parse failed", err)
		}
	default:
		return nil, fmt.Errorf("unsupported object header version: %d", header.Version)
	}

	header.Type = determineObjectType(header.Messages)

	return header, nil
}

func determineObjectType(messages []*HeaderMessage) ObjectType {
	for _, msg := range messages {
		switch msg.Type {
		case MsgSymbolTable, MsgLinkInfo, MsgLinkMessage:
			return ObjectTypeGroup
		case MsgDataspace:
			return ObjectTypeDataset
		case MsgDatatype:
			return ObjectTypeDatatype
		}
	}
	return ObjectTypeUnknown
}

func parseV2Header(r io.ReaderAt, offset uint64, sb *Superblock, isBE bool) ([]*HeaderMessage, string, error) {
	var messages []*HeaderMessage
	var name string

	sizeBuf := utils.GetBuffer(4)
	defer utils.ReleaseBuffer(sizeBuf)

	if _, err := r.ReadAt(sizeBuf, int64(offset)); err != nil {
		return nil, "", utils.WrapError("header size read failed", err)
	}

	var headerSize uint32
	if isBE {
		headerSize = binary.BigEndian.Uint32(sizeBuf)
	} else {
		headerSize = binary.LittleEndian.Uint32(sizeBuf)
	}

	current := offset + 4
	end := offset + uint64(headerSize)

	for current < end {
		typeSizeBuf := utils.GetBuffer(4)
		if _, err := r.ReadAt(typeSizeBuf, int64(current)); err != nil {
			utils.ReleaseBuffer(typeSizeBuf)
			return nil, "", utils.WrapError("message header read failed", err)
		}

		var msgType MessageType
		var msgSize uint16
		if isBE {
			msgType = MessageType(binary.BigEndian.Uint16(typeSizeBuf[0:2]))
			msgSize = binary.BigEndian.Uint16(typeSizeBuf[2:4])
		} else {
			msgType = MessageType(binary.LittleEndian.Uint16(typeSizeBuf[0:2]))
			msgSize = binary.LittleEndian.Uint16(typeSizeBuf[2:4])
		}
		utils.ReleaseBuffer(typeSizeBuf)

		if msgSize == 0 {
			current += 4
			continue
		}

		data := utils.GetBuffer(int(msgSize))
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
