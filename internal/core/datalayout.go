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

// Chunk index types stored in a version 4 chunked layout message.
// Reference: H5Dpublic.h:57-64 (H5D_chunk_index_t).
const (
	ChunkIndexBTREE  uint8 = 0 // v1 B-tree (version 3 layout only).
	ChunkIndexSingle uint8 = 1 // Single chunk.
	ChunkIndexNone   uint8 = 2 // Implicit: no index.
	ChunkIndexFArray uint8 = 3 // Fixed array.
	ChunkIndexEArray uint8 = 4 // Extensible array.
	ChunkIndexBT2    uint8 = 5 // v2 B-tree.
)

// Chunk layout flags and the rank ceiling, from H5Oprivate.h:400-406.
const (
	// chunkLayoutFlagDontFilterPartial marks chunks that must not be filtered
	// when only part of them is written (H5Oprivate.h:403).
	chunkLayoutFlagDontFilterPartial = 0x01

	// chunkLayoutFlagSingleWithFilter marks a single-chunk index that stores the
	// chunk size and filter mask in the layout message (H5Oprivate.h:404).
	chunkLayoutFlagSingleWithFilter = 0x02

	// chunkLayoutFlagAll is the mask of flags the format defines; the C decoder
	// rejects any other bit (H5Olayout.c:321).
	chunkLayoutFlagAll = chunkLayoutFlagDontFilterPartial | chunkLayoutFlagSingleWithFilter

	// maxChunkLayoutDims is H5O_LAYOUT_NDIMS = H5S_MAX_RANK + 1 (H5Oprivate.h:400).
	// The chunk grid has one more dimension than the dataspace: the last one is
	// the element size in bytes.
	maxChunkLayoutDims = 33
)

// DataLayoutMessage represents HDF5 data layout message.
type DataLayoutMessage struct {
	Version        uint8
	Class          DataLayoutClass
	DataAddress    uint64   // Address where data is stored (for contiguous/chunked).
	DataSize       uint64   // Size of data (for contiguous).
	CompactData    []byte   // Data itself (for compact layout).
	ChunkSize      []uint64 // Chunk dimensions (for chunked layout) - uint64 for HDF5 2.0.0+ support.
	ChunkKeySize   uint8    // Size of chunk keys in bytes: 4 (uint32) or 8 (uint64).
	ChunkIndexType uint8    // Chunk index type (version 4 only); ChunkIndexBTREE otherwise.
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
		Version:      version,
		ChunkKeySize: determineChunkKeySize(sb.Version),
	}

	switch version {
	case 3:
		return parseLayoutV3(data, sb, msg)
	case 4:
		return parseLayoutV4(data, sb, msg)
	}

	return nil, fmt.Errorf("layout version %d not implemented", version)
}

// determineChunkKeySize determines the chunk key size based on file format version.
// HDF5 < 2.0.0 (superblock v0-v2) uses 32-bit chunk dimensions.
// Future versions may use 64-bit chunk dimensions.
func determineChunkKeySize(superblockVersion uint8) uint8 {
	// Conservative approach: use 32-bit for all current versions (0, 2, 3).
	// All tested files (including HDF5 2.0.0) work correctly with 32-bit.
	// This condition (>= 4) is prepared for potential future versions.
	if superblockVersion >= 4 {
		return 8
	}
	return 4
}

// parseLayoutV3 parses HDF5 Data Layout Message version 3.
// Cognitive complexity is high due to handling 3 distinct layout types
// (Compact, Contiguous, Chunked) with different binary formats and
// HDF5 version differences (32-bit vs 64-bit chunk dimensions).
// This complexity is inherent to the HDF5 format specification.
//
//nolint:gocognit // Binary format parsing requires handling multiple layout types
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
		if err := validateChunkDimensionality(dimensionality); err != nil {
			return nil, err
		}

		offset := 3
		// For v3, B-tree address comes BEFORE chunk dimensions.
		// Read B-tree address (where chunk index is stored).
		if offset+int(sb.OffsetSize) > len(data) {
			return nil, errors.New("chunked layout address truncated")
		}
		msg.DataAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read chunk dimensions.
		// Current HDF5 formats (superblock v0-v3) use 32-bit chunk dimensions.
		// Future formats may use 64-bit chunk dimensions.
		msg.ChunkSize = make([]uint64, dimensionality)

		if err := readChunkDimensions(msg, data, offset, dimensionality); err != nil {
			return nil, err
		}

		// A zero chunk dimension divides by zero in every reader that maps
		// element coordinates to chunk coordinates, so it is rejected here, at
		// the only place the value enters the library.
		for i, dim := range msg.ChunkSize {
			if dim == 0 {
				return nil, fmt.Errorf("chunked layout dimension %d is zero", i)
			}
		}

	default:
		return nil, fmt.Errorf("unsupported layout class: %d", msg.Class)
	}

	return msg, nil
}

// readChunkDimensions fills msg.ChunkSize from the message body, reading 64-bit
// dimensions for HDF5 2.0.0+ and 32-bit dimensions for earlier formats.
func readChunkDimensions(msg *DataLayoutMessage, data []byte, offset int, dimensionality uint8) error {
	if msg.ChunkKeySize == 8 {
		for i := 0; i < int(dimensionality); i++ {
			if offset+8 > len(data) {
				return fmt.Errorf("chunked layout dimension %d truncated (64-bit)", i)
			}
			msg.ChunkSize[i] = binary.LittleEndian.Uint64(data[offset : offset+8])
			offset += 8
		}

		return nil
	}

	// Read as uint32 (HDF5 < 2.0.0) and convert to uint64 for internal consistency.
	for i := 0; i < int(dimensionality); i++ {
		if offset+4 > len(data) {
			return fmt.Errorf("chunked layout dimension %d truncated (32-bit)", i)
		}
		msg.ChunkSize[i] = uint64(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	return nil
}

func parseLayoutV4(data []byte, sb *Superblock, msg *DataLayoutMessage) (*DataLayoutMessage, error) {
	if len(data) < 2 {
		return nil, errors.New("data layout message too short")
	}

	// Version 4 compact and contiguous bodies are identical to version 3. A
	// chunked body is not: it carries flags, a per-dimension encoded width and
	// an index type before the address (H5Olayout.c:305-520). Treating it as
	// version 3 silently mis-read the chunk grid of real files.
	if DataLayoutClass(data[1]) == LayoutChunked {
		return parseChunkedLayoutV4(data, sb, msg)
	}

	return parseLayoutV3(data, sb, msg)
}

// validateChunkDimensionality rejects a chunk grid that cannot describe a
// dataset. Zero dimensions never occur in a valid file, and the format caps the
// grid at H5O_LAYOUT_NDIMS; the C decoder applies both checks to version 3 and
// version 4 messages (H5Olayout.c:276, :335).
func validateChunkDimensionality(ndims uint8) error {
	if ndims == 0 {
		return errors.New("chunked layout has zero dimensionality")
	}

	if ndims > maxChunkLayoutDims {
		return fmt.Errorf("chunked layout dimensionality %d exceeds the maximum %d",
			ndims, maxChunkLayoutDims)
	}

	return nil
}

// parseChunkedLayoutV4 parses a version 4 chunked layout body.
//
// Layout: version(1) class(1) flags(1) ndims(1) enc_bytes_per_dim(1)
// dims[ndims × enc_bytes_per_dim], index type(1), index creation parameters,
// index address(sizeof_addr).
// Reference: H5Olayout.c:305-520.
func parseChunkedLayoutV4(data []byte, sb *Superblock, msg *DataLayoutMessage) (*DataLayoutMessage, error) {
	if len(data) < 5 {
		return nil, errors.New("chunked layout message too short")
	}

	flags := data[2]
	ndims := data[3]
	encBytes := int(data[4])

	// The C decoder rejects any bit outside the two defined flags
	// (H5Olayout.c:321): an unknown flag means the body is corrupt or a future
	// feature this parser would silently misread.
	if flags&^uint8(chunkLayoutFlagAll) != 0 {
		return nil, fmt.Errorf("chunked layout has unknown flags: 0x%02x", flags)
	}

	if err := validateChunkDimensionality(ndims); err != nil {
		return nil, err
	}

	if encBytes == 0 || encBytes > 8 {
		return nil, fmt.Errorf("chunked layout has invalid dimension width: %d", encBytes)
	}

	offset := 5
	if offset+int(ndims)*encBytes > len(data) {
		return nil, errors.New("chunked layout dimensions truncated")
	}

	msg.ChunkSize = make([]uint64, ndims)
	for i := 0; i < int(ndims); i++ {
		msg.ChunkSize[i] = readUint64(data[offset:], encBytes, binary.LittleEndian)
		if msg.ChunkSize[i] == 0 {
			return nil, fmt.Errorf("chunked layout dimension %d is zero", i)
		}
		offset += encBytes
	}

	if offset >= len(data) {
		return nil, errors.New("chunked layout missing chunk index type")
	}

	msg.ChunkIndexType = data[offset]
	offset++

	skip, err := chunkIndexParamSize(msg.ChunkIndexType, flags, sb)
	if err != nil {
		return nil, err
	}

	if offset+skip+int(sb.OffsetSize) > len(data) {
		return nil, errors.New("chunked layout index data truncated")
	}

	offset += skip
	msg.DataAddress = readUint64(data[offset:], int(sb.OffsetSize), sb.Endianness)

	return msg, nil
}

// chunkIndexParamSize returns the size of the index creation parameters that
// follow the index type in a version 4 chunked layout message.
// Reference: H5Olayout.c:384-516.
func chunkIndexParamSize(indexType, flags uint8, sb *Superblock) (int, error) {
	switch indexType {
	case ChunkIndexBTREE:
		return 0, errors.New("chunked layout version 4 cannot use the v1 B-tree index")
	case ChunkIndexNone:
		return 0, nil
	case ChunkIndexSingle:
		if flags&chunkLayoutFlagSingleWithFilter != 0 {
			return int(sb.LengthSize) + 4, nil // Chunk size + filter mask.
		}

		return 0, nil
	case ChunkIndexFArray:
		return 1, nil // max_dblk_page_nelmts_bits.
	case ChunkIndexEArray:
		return 5, nil // Five one-byte creation parameters.
	case ChunkIndexBT2:
		return 6, nil // node_size(4) + split_percent(1) + merge_percent(1).
	default:
		return 0, fmt.Errorf("unsupported chunk index type: %d", indexType)
	}
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
