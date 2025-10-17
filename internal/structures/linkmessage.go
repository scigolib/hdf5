package structures

import (
	"encoding/binary"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
)

// LinkType represents the type of link.
type LinkType uint8

// Link type constants define different kinds of links between HDF5 objects.
const (
	LinkTypeHard     LinkType = 0  // Hard link (object address).
	LinkTypeSoft     LinkType = 1  // Soft link (path string).
	LinkTypeExternal LinkType = 64 // External link (user-defined >= 64).
)

// LinkMessage represents an HDF5 Link message (type 6).
type LinkMessage struct {
	Version            uint8
	Flags              uint8
	Type               LinkType
	Name               string
	CreationOrder      int64
	CreationOrderValid bool
	CharacterSet       uint8

	// For hard links.
	ObjectAddress uint64

	// For soft links.
	TargetPath string
}

// Link message flag bits.
const (
	flagNameSize0          = 0x00 // Name size: 1 byte.
	flagNameSize1          = 0x01 // Name size: 2 bytes.
	flagNameSize2          = 0x02 // Name size: 4 bytes.
	flagNameSize3          = 0x03 // Name size: 8 bytes.
	flagNameSizeMask       = 0x03 // Mask for name size bits.
	flagStoreCreationOrder = 0x04 // Store creation order.
	flagStoreLinkType      = 0x08 // Store link type.
	flagStoreCharset       = 0x10 // Store character set.
)

// ParseLinkMessage parses a Link message from raw data.
func ParseLinkMessage(data []byte, sb *core.Superblock) (*LinkMessage, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("link message too short: %d bytes", len(data))
	}

	msg := &LinkMessage{}
	current := 0

	// Version (1 byte).
	msg.Version = data[current]
	current++

	if msg.Version != 1 {
		return nil, fmt.Errorf("unsupported link message version: %d", msg.Version)
	}

	// Flags (1 byte).
	msg.Flags = data[current]
	current++

	// Optional: Link type (1 byte if flag bit 3 is set).
	if msg.Flags&flagStoreLinkType != 0 {
		if current >= len(data) {
			return nil, fmt.Errorf("unexpected end of data reading link type")
		}
		msg.Type = LinkType(data[current])
		current++
	} else {
		// Default is hard link.
		msg.Type = LinkTypeHard
	}

	// Optional: Creation order (8 bytes if flag bit 2 is set).
	if msg.Flags&flagStoreCreationOrder != 0 {
		if current+8 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading creation order")
		}
		//nolint:gosec // G115: HDF5 binary format requires uint64 to int64 conversion
		msg.CreationOrder = int64(binary.LittleEndian.Uint64(data[current : current+8]))
		msg.CreationOrderValid = true
		current += 8
	}

	// Optional: Character set (1 byte if flag bit 4 is set).
	if msg.Flags&flagStoreCharset != 0 {
		if current >= len(data) {
			return nil, fmt.Errorf("unexpected end of data reading charset")
		}
		msg.CharacterSet = data[current]
		current++
	} else {
		msg.CharacterSet = 0 // ASCII.
	}

	// Name length (variable: 1, 2, 4, or 8 bytes based on flags bits 0-1).
	nameSizeType := msg.Flags & flagNameSizeMask
	var nameLen uint64

	switch nameSizeType {
	case flagNameSize0: // 1 byte.
		if current >= len(data) {
			return nil, fmt.Errorf("unexpected end of data reading name length")
		}
		nameLen = uint64(data[current])
		current++

	case flagNameSize1: // 2 bytes.
		if current+2 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading name length")
		}
		nameLen = uint64(binary.LittleEndian.Uint16(data[current : current+2]))
		current += 2

	case flagNameSize2: // 4 bytes.
		if current+4 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading name length")
		}
		nameLen = uint64(binary.LittleEndian.Uint32(data[current : current+4]))
		current += 4

	case flagNameSize3: // 8 bytes.
		if current+8 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading name length")
		}
		nameLen = binary.LittleEndian.Uint64(data[current : current+8])
		current += 8
	}

	if nameLen == 0 {
		return nil, fmt.Errorf("invalid name length: 0")
	}

	// Name (N bytes).
	//nolint:gosec // G115: Safe conversion for HDF5 string lengths
	if current+int(nameLen) > len(data) {
		return nil, fmt.Errorf("unexpected end of data reading name (need %d bytes, have %d)",
			nameLen, len(data)-current)
	}
	//nolint:gosec // G115: Safe conversion for HDF5 string lengths
	msg.Name = string(data[current : current+int(nameLen)])
	//nolint:gosec // G115: Safe conversion for HDF5 string lengths
	current += int(nameLen)

	// Link-type specific data.
	switch msg.Type {
	case LinkTypeHard:
		// Object address (offset size bytes from superblock).
		offsetSize := int(sb.OffsetSize)
		if current+offsetSize > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading object address")
		}

		// Read address with proper size.
		switch offsetSize {
		case 1:
			msg.ObjectAddress = uint64(data[current])
		case 2:
			msg.ObjectAddress = uint64(sb.Endianness.Uint16(data[current : current+2]))
		case 4:
			msg.ObjectAddress = uint64(sb.Endianness.Uint32(data[current : current+4]))
		case 8:
			msg.ObjectAddress = sb.Endianness.Uint64(data[current : current+8])
		default:
			return nil, fmt.Errorf("invalid offset size: %d", offsetSize)
		}
		// current is not used after this point.

	case LinkTypeSoft:
		// Soft link value length (2 bytes).
		if current+2 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading soft link length")
		}
		targetLen := binary.LittleEndian.Uint16(data[current : current+2])
		current += 2

		if targetLen == 0 {
			return nil, fmt.Errorf("invalid soft link length: 0")
		}

		// Target path.
		if current+int(targetLen) > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading soft link path")
		}
		msg.TargetPath = string(data[current : current+int(targetLen)])
		// current is not used after this point.

	default:
		// External or user-defined links (type >= 64).
		// For now, just skip the data.
		// User-defined link data: 2 bytes length + data.
		if current+2 > len(data) {
			return nil, fmt.Errorf("unexpected end of data reading user-defined link length")
		}
		udLen := binary.LittleEndian.Uint16(data[current : current+2])
		// Skip user-defined link data: current += 2 + int(udLen).
		// Not implemented yet, so we don't need to track current.
		_ = udLen // Acknowledge we read it but don't use it.
	}

	return msg, nil
}

// IsHardLink returns true if this is a hard link.
func (lm *LinkMessage) IsHardLink() bool {
	return lm.Type == LinkTypeHard
}

// IsSoftLink returns true if this is a soft link.
func (lm *LinkMessage) IsSoftLink() bool {
	return lm.Type == LinkTypeSoft
}

// String returns a string representation of the link.
func (lm *LinkMessage) String() string {
	switch lm.Type {
	case LinkTypeHard:
		return fmt.Sprintf("Hard link '%s' -> address 0x%x", lm.Name, lm.ObjectAddress)
	case LinkTypeSoft:
		return fmt.Sprintf("Soft link '%s' -> '%s'", lm.Name, lm.TargetPath)
	default:
		return fmt.Sprintf("Link '%s' (type %d)", lm.Name, lm.Type)
	}
}
