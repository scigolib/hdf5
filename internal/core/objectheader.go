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
	MsgAttribute    MessageType = 12
	MsgName         MessageType = 11
	MsgSymbolTable  MessageType = 17
)

func ReadObjectHeader(r io.ReaderAt, address uint64, sb *Superblock) (*ObjectHeader, error) {
	// Преобразуем адрес в int64
	offset := int64(address)
	if offset < 0 {
		return nil, fmt.Errorf("negative offset: %d", offset)
	}

	prefix := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(prefix)

	if _, err := r.ReadAt(prefix, offset); err != nil {
		return nil, utils.WrapError("object header read failed", err)
	}

	// Проверяем сигнатуру
	signature := string(prefix[0:4])
	if signature != "OHDR" {
		// Попробуем интерпретировать как little-endian
		leSignature := string([]byte{prefix[3], prefix[2], prefix[1], prefix[0]})
		if leSignature == "OHDR" {
			fmt.Println("Note: object header signature in little-endian format")
			// Переключаем порядок байт
			sb.Endianness = binary.LittleEndian
		} else {
			return nil, fmt.Errorf("invalid object header signature: %s (hex: % x)", signature, prefix[0:4])
		}
	}

	version := prefix[4]
	flags := prefix[5]

	header := &ObjectHeader{
		Version: version,
		Flags:   flags,
	}

	var err error
	switch version {
	case 1:
		return nil, errors.New("version 1 headers not supported")
	case 2:
		header.Messages, header.Name, err = parseV2Header(r, address+8, sb)
		if err != nil {
			return nil, utils.WrapError("v2 header parse failed", err)
		}
	default:
		return nil, fmt.Errorf("unsupported object header version: %d", version)
	}

	header.Type = determineObjectType(header.Messages)

	return header, nil
}

func determineObjectType(messages []*HeaderMessage) ObjectType {
	hasDataspace := false
	hasDatatype := false
	hasSymbolTable := false

	for _, msg := range messages {
		switch msg.Type {
		case MsgDataspace:
			hasDataspace = true
		case MsgDatatype:
			hasDatatype = true
		case MsgSymbolTable:
			hasSymbolTable = true
		}
	}

	if hasSymbolTable {
		return ObjectTypeGroup
	}
	if hasDataspace && hasDatatype {
		return ObjectTypeDataset
	}
	return ObjectTypeUnknown
}

func parseV2Header(r io.ReaderAt, offset uint64, sb *Superblock) ([]*HeaderMessage, string, error) {
	var messages []*HeaderMessage
	var name string

	sizeBuf := utils.GetBuffer(4)
	defer utils.ReleaseBuffer(sizeBuf)

	if _, err := r.ReadAt(sizeBuf, int64(offset)); err != nil {
		return nil, "", utils.WrapError("header size read failed", err)
	}
	headerSize := sb.Endianness.Uint32(sizeBuf)

	current := offset + 4
	end := offset + uint64(headerSize)

	for current < end {
		typeSizeBuf := utils.GetBuffer(4)
		if _, err := r.ReadAt(typeSizeBuf, int64(current)); err != nil {
			return nil, "", utils.WrapError("message header read failed", err)
		}

		msgType := MessageType(sb.Endianness.Uint16(typeSizeBuf[0:2]))
		msgSize := sb.Endianness.Uint16(typeSizeBuf[2:4])
		utils.ReleaseBuffer(typeSizeBuf)

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
