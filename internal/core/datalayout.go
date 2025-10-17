package core

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DataLayoutClass represents the storage layout type.
type DataLayoutClass uint8

// Data layout class constants define how dataset data is stored.
const (
	LayoutCompact    DataLayoutClass = 0 // Data stored in message.
	LayoutContiguous DataLayoutClass = 1 // Data stored contiguously in file.
	LayoutChunked    DataLayoutClass = 2 // Data stored in chunks.
	LayoutVirtual    DataLayoutClass = 3 // Virtual dataset (HDF5 1.10+).

	layoutUnknown = "unknown" // String representation for unknown layout class.
)

// DataLayoutMessage represents HDF5 data layout message.
type DataLayoutMessage struct {
	Version     uint8
	Class       DataLayoutClass
	DataAddress uint64   // Address where data is stored (for contiguous/chunked).
	DataSize    uint64   // Size of data (for contiguous).
	CompactData []byte   // Data itself (for compact layout).
	ChunkSize   []uint32 // Chunk dimensions (for chunked layout).
}

// ParseDataLayoutMessage parses a data layout message from header message data.
func ParseDataLayoutMessage(data []byte, sb *Superblock) (*DataLayoutMessage, error) {
	if len(data) < 1 {
		return nil, errors.New("data layout message too short")
	}

	version := data[0]

	// Version 3 and 4 are most common (HDF5 1.8+).
	if version < 3 || version > 4 {
		return nil, fmt.Errorf("unsupported data layout version: %d", version)
	}

	msg := &DataLayoutMessage{
		Version: version,
	}

	switch version {
	case 3:
		return parseLayoutV3(data, sb, msg)
	case 4:
		return parseLayoutV4(data, sb, msg)
	}

	return nil, fmt.Errorf("layout version %d not implemented", version)
}

func parseLayoutV3(data []byte, sb *Superblock, msg *DataLayoutMessage) (*DataLayoutMessage, error) {
	if len(data) < 2 {
		return nil, errors.New("layout v3 message too short")
	}

	msg.Class = DataLayoutClass(data[1])

	switch msg.Class {
	case LayoutCompact:
		// Compact layout: data is stored in the message itself.
		if len(data) < 4 {
			return nil, errors.New("compact layout message too short")
		}
		size := binary.LittleEndian.Uint16(data[2:4])
		if len(data) < 4+int(size) {
			return nil, errors.New("compact layout data truncated")
		}
		msg.CompactData = data[4 : 4+size]
		msg.DataSize = uint64(size)

	case LayoutContiguous:
		// Contiguous layout: data stored sequentially in file.
		if len(data) < 2+int(sb.OffsetSize)+int(sb.LengthSize) {
			return nil, errors.New("contiguous layout message too short")
		}

		offset := 2
		// Read data address.
		msg.DataAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read data size.
		msg.DataSize = readUint64(data[offset:], int(sb.LengthSize), sb.Endianness)

	case LayoutChunked:
		// Chunked layout v3: dimensionality + B-tree address + chunk dimensions.
		// Reference: H5Olayout.c - H5D_CHUNKED case.
		if len(data) < 3 {
			return nil, errors.New("chunked layout message too short")
		}

		dimensionality := data[2]
		offset := 3

		// For v3, B-tree address comes BEFORE chunk dimensions.
		// Read B-tree address (where chunk index is stored).
		if offset+int(sb.OffsetSize) > len(data) {
			return nil, errors.New("chunked layout address truncated")
		}
		msg.DataAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Now read chunk dimensions.
		msg.ChunkSize = make([]uint32, dimensionality)
		for i := 0; i < int(dimensionality); i++ {
			if offset+4 > len(data) {
				return nil, errors.New("chunked layout dimensions truncated")
			}
			msg.ChunkSize[i] = binary.LittleEndian.Uint32(data[offset : offset+4])
			offset += 4
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", msg.Class)
	}

	return msg, nil
}

func parseLayoutV4(data []byte, sb *Superblock, msg *DataLayoutMessage) (*DataLayoutMessage, error) {
	// Version 4 is similar to v3 but with some differences.
	// For now, delegate to v3 parser (they're very similar for contiguous layout).
	return parseLayoutV3(data, sb, msg)
}

// Helper function to read variable-sized unsigned integers.
func readUint64(data []byte, size int, endianness binary.ByteOrder) uint64 {
	if size > len(data) {
		size = len(data)
	}

	switch size {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(endianness.Uint16(data[:2]))
	case 4:
		return uint64(endianness.Uint32(data[:4]))
	case 8:
		return endianness.Uint64(data[:8])
	default:
		// Pad to 8 bytes and read.
		var buf [8]byte
		copy(buf[:], data[:size])
		return endianness.Uint64(buf[:])
	}
}

// IsContiguous returns true if layout is contiguous.
func (dl *DataLayoutMessage) IsContiguous() bool {
	return dl.Class == LayoutContiguous
}

// IsCompact returns true if layout is compact (data in message).
func (dl *DataLayoutMessage) IsCompact() bool {
	return dl.Class == LayoutCompact
}

// IsChunked returns true if layout is chunked.
func (dl *DataLayoutMessage) IsChunked() bool {
	return dl.Class == LayoutChunked
}

// String returns human-readable layout description.
func (dl *DataLayoutMessage) String() string {
	switch dl.Class {
	case LayoutCompact:
		return fmt.Sprintf("compact (size=%d)", dl.DataSize)
	case LayoutContiguous:
		return fmt.Sprintf("contiguous (address=0x%X, size=%d)", dl.DataAddress, dl.DataSize)
	case LayoutChunked:
		return fmt.Sprintf("chunked (chunks=%v)", dl.ChunkSize)
	case LayoutVirtual:
		return "virtual"
	default:
		return layoutUnknown
	}
}
