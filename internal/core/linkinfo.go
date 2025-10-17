package core

import (
	"errors"
)

// LinkInfoMessage represents HDF5 link information message for modern groups.
type LinkInfoMessage struct {
	Version              uint8
	Flags                uint8
	MaxLinkCreationOrder uint8
	BTreeAddress         uint64
	HeapAddress          uint64
}

// ParseLinkInfoMessage parses a link information message from header message data.
func ParseLinkInfoMessage(data []byte, sb *Superblock) (*LinkInfoMessage, error) {
	if len(data) < 18 {
		return nil, errors.New("link info message too short")
	}

	msg := &LinkInfoMessage{
		Version: data[0],
		Flags:   data[1],
	}

	offset := 2
	if msg.Flags&0x01 != 0 {
		if len(data) < offset+1 {
			return nil, errors.New("link info message missing max creation order")
		}
		msg.MaxLinkCreationOrder = data[offset]
		offset++
	}

	msg.BTreeAddress = sb.Endianness.Uint64(data[offset : offset+8])
	offset += 8
	msg.HeapAddress = sb.Endianness.Uint64(data[offset : offset+8])

	return msg, nil
}
