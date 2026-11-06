package core

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/utils"
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
	ChunkIndexType uint8    // Chunk index type (versions 4 and 5); ChunkIndexBTREE otherwise.
	ChunkFlags     uint8    // Chunked layout flags (versions 4 and 5): chunkLayoutFlag*.

	// Index holds the chunk index creation parameters of a version 4 or 5
	// chunked layout (H5O_layout_chunk_t, H5Oprivate.h:455-488).
	Index ChunkIndexParams
}

// ChunkIndexParams holds the creation parameters a version 4 or 5 chunked
// layout stores after the index type (H5Olayout.c:383-516). Only the fields of
// the message's index type are set.
type ChunkIndexParams struct {
	// SingleNbytes and SingleFilterMask are the stored size and filter mask of
	// a filtered single chunk (H5O_storage_chunk_single_filt_t).
	SingleNbytes     uint64
	SingleFilterMask uint32

	// FArrayPageBits is log2 of the elements in a fixed array data block page.
	FArrayPageBits uint8

	// EArray holds the extensible array parameters.
	EArray EArrayParams

	// BT2 holds the v2 B-tree parameters.
	BT2 BT2Params
}

// EArrayParams are the extensible array creation parameters (H5EA_create_t,
// H5EAprivate.h), one byte each in the layout message.
type EArrayParams struct {
	MaxNelmtsBits         uint8 // log2 of the most elements the array can hold.
	IdxBlkElmts           uint8 // Elements stored in the index block.
	SupBlkMinDataPtrs     uint8 // Least data block pointers in a super block.
	DataBlkMinElmts       uint8 // Least elements in a data block.
	MaxDblkPageNelmtsBits uint8 // log2 of the elements in a data block page.
}

// BT2Params are the v2 B-tree creation parameters stored in the layout
// message (H5O_layout_chunk_bt2_t).
type BT2Params struct {
	NodeSize     uint32
	SplitPercent uint8
	MergePercent uint8
}

// ParseDataLayoutMessage parses a data layout message from header message data.
func ParseDataLayoutMessage(data []byte, sb *Superblock) (*DataLayoutMessage, error) {
	if len(data) < 1 {
		return nil, errors.New("data layout message too short")
	}

	version := data[0]

	// Versions 1-5 are defined (H5O_LAYOUT_VERSION_1..5, H5Oprivate.h:442-455);
	// C rejects any other (H5Olayout.c:103-104).
	if version < 1 || version > 5 {
		return nil, fmt.Errorf("unsupported data layout version: %d: %w", version, utils.ErrCorrupt)
	}

	msg := &DataLayoutMessage{
		Version:      version,
		ChunkKeySize: determineChunkKeySize(sb.Version),
	}

	switch version {
	case 1, 2:
		return parseLayoutV1V2(data, sb, msg)
	case 3:
		return parseLayoutV3(data, sb, msg)
	case 4, 5:
		// Version 5 (HDF5 2.0) has the version 4 body; only the width of a
		// filtered chunk's stored size in the index differs (H5Dfarray.c:55-63).
		return parseLayoutV4(data, sb, msg)
	}

	return nil, fmt.Errorf("layout version %d not implemented", version)
}

// parseLayoutV1V2 parses a version 1 or 2 data layout message as
// H5O__layout_decode does (H5Olayout.c:106-216):
//
//	version(1) ndims(1) class(1) reserved(5)
//	address(sizeof_addr)            contiguous and chunked only
//	dims[ndims] × uint32            chunk dims (chunked); skipped otherwise
//	size(4) data[size]              compact only
//
// ndims is 1..H5O_LAYOUT_NDIMS, a chunked message needs ndims ≥ 2 (the last
// dimension is the element size) and every chunk dimension is positive. The
// stored contiguous dimensions are skipped as C skips them: the storage size
// is computed from the dataspace and datatype, which the descriptor does
// (H5Olayout.c:165-170). DataSize stays 0 for a contiguous layout. Only the
// version 1 B-tree chunk index exists in these versions (H5Olayout.c:150-155).
// Errors wrap utils.ErrCorrupt.
func parseLayoutV1V2(data []byte, sb *Superblock, msg *DataLayoutMessage) (*DataLayoutMessage, error) {
	const fixedPart = 8 // Version, ndims, class and 5 reserved bytes.

	if len(data) < fixedPart {
		return nil, fmt.Errorf("%w: version %d layout message of %d bytes is too short", utils.ErrCorrupt, msg.Version, len(data))
	}

	ndims := int(data[1])
	if ndims == 0 || ndims > maxChunkLayoutDims {
		return nil, fmt.Errorf("%w: layout dimensionality %d is out of range (H5Olayout.c:114-115)", utils.ErrCorrupt, ndims)
	}

	msg.Class = DataLayoutClass(data[2])
	if msg.Class != LayoutContiguous && msg.Class != LayoutChunked && msg.Class != LayoutCompact {
		return nil, fmt.Errorf("%w: bad layout type %d for layout message (H5Olayout.c:122-123)", utils.ErrCorrupt, msg.Class)
	}

	offset := fixedPart
	addrSize := int(sb.OffsetSize)
	if msg.Class != LayoutCompact {
		if len(data) < offset+addrSize {
			return nil, fmt.Errorf("%w: layout message address runs off the end of the message", utils.ErrCorrupt)
		}
		msg.DataAddress = readLayoutAddress(data[offset:offset+addrSize], addrSize, sb.Endianness)
		offset += addrSize
	}

	dimsSize := ndims * 4
	if len(data) < offset+dimsSize {
		return nil, fmt.Errorf("%w: layout message dimensions run off the end of the message", utils.ErrCorrupt)
	}

	if msg.Class == LayoutChunked {
		chunks, err := parseLayoutV1V2ChunkDims(data[offset:offset+dimsSize], ndims)
		if err != nil {
			return nil, err
		}
		msg.ChunkIndexType = ChunkIndexBTREE
		msg.ChunkSize = chunks
	}
	offset += dimsSize

	if msg.Class == LayoutCompact {
		compact, err := parseLayoutV1V2Compact(data[offset:])
		if err != nil {
			return nil, err
		}
		msg.CompactData = compact
		msg.DataSize = uint64(len(compact))
	}

	return msg, nil
}

// parseLayoutV1V2ChunkDims decodes the chunk dimensions of a version 1/2
// layout: at least two (the last is the element size), none of them zero
// (H5Olayout.c:176-191).
func parseLayoutV1V2ChunkDims(dims []byte, ndims int) ([]uint64, error) {
	if ndims < 2 {
		return nil, fmt.Errorf("%w: bad dimensions for chunked storage (H5Olayout.c:176-177)", utils.ErrCorrupt)
	}
	chunks := make([]uint64, ndims)
	for i := range chunks {
		dim := binary.LittleEndian.Uint32(dims[4*i:])
		if dim == 0 {
			return nil, fmt.Errorf("%w: chunk dimension %d is zero (H5Olayout.c:188-191)", utils.ErrCorrupt, i)
		}
		chunks[i] = uint64(dim)
	}
	return chunks, nil
}

// parseLayoutV1V2Compact returns the raw data of a version 1/2 compact
// layout: a 4-byte size and that many bytes (H5Olayout.c:207-208).
func parseLayoutV1V2Compact(rest []byte) ([]byte, error) {
	if len(rest) < 4 {
		return nil, fmt.Errorf("%w: compact layout size runs off the end of the message", utils.ErrCorrupt)
	}
	size := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	if uint64(len(rest)) < uint64(size) {
		return nil, fmt.Errorf("%w: compact layout data of %d bytes runs off the end of the message (H5Olayout.c:207-208)",
			utils.ErrCorrupt, size)
	}
	return rest[:size], nil
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
		msg.DataAddress = readLayoutAddress(data[offset:], int(sb.OffsetSize), sb.Endianness)
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
		msg.DataAddress = readLayoutAddress(data[offset:], int(sb.OffsetSize), sb.Endianness)
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

	// The class byte is decoded for every class (H5Olayout.c:222); without it
	// the message kept the zero value, LayoutCompact.
	msg.Class = LayoutChunked

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
	msg.ChunkFlags = flags
	offset++

	params, err := decodeChunkIndexParams(data[offset:], msg.ChunkIndexType, flags, sb)
	if err != nil {
		return nil, fmt.Errorf("%w: chunked layout: %w", utils.ErrCorrupt, err)
	}
	msg.Index = params.ChunkIndexParams

	offset += params.size
	if offset+int(sb.OffsetSize) > len(data) {
		return nil, fmt.Errorf("%w: chunked layout index data truncated: no room for the index address", utils.ErrCorrupt)
	}

	msg.DataAddress = readLayoutAddress(data[offset:], int(sb.OffsetSize), sb.Endianness)

	return msg, nil
}

// decodedIndexParams is a decoded parameter block and its encoded size.
type decodedIndexParams struct {
	ChunkIndexParams
	size int
}

// decodeChunkIndexParams decodes the index creation parameters that follow the
// index type in a version 4 or 5 chunked layout message, with the checks
// H5O__layout_decode makes (H5Olayout.c:383-516):
//   - the v1 B-tree cannot appear in these versions (:383-386);
//   - a single chunk index stores the chunk's size and filter mask only when
//     the "single index with filter" flag is set (:392-409);
//   - the fixed array page bits and the five extensible array parameters are
//     nonzero (:416-418, :431-469);
//   - the v2 B-tree split and merge percentages are in 1..100 (:481-505);
//   - any other index type is invalid (:511-512).
func decodeChunkIndexParams(data []byte, indexType, flags uint8, sb *Superblock) (decodedIndexParams, error) {
	var p decodedIndexParams

	n, err := chunkIndexParamsSize(indexType, flags, sb)
	if err != nil {
		return p, err
	}
	if n > len(data) {
		return p, fmt.Errorf("chunk index data truncated: type %d parameters need %d bytes, have %d", indexType, n, len(data))
	}
	p.size = n
	data = data[:n]

	switch indexType {
	case ChunkIndexSingle:
		if n > 0 {
			ls := int(sb.LengthSize)
			p.SingleNbytes = readUint64(data, ls, sb.Endianness)
			p.SingleFilterMask = binary.LittleEndian.Uint32(data[ls:])
		}
	case ChunkIndexFArray:
		if data[0] == 0 {
			return p, errors.New("invalid fixed array creation parameter: page bits are zero")
		}
		p.FArrayPageBits = data[0]
	case ChunkIndexEArray:
		p.EArray, err = decodeEArrayParams(data)
	case ChunkIndexBT2:
		p.BT2, err = decodeBT2Params(data)
	}

	return p, err
}

// chunkIndexParamsSize returns the encoded size of the creation parameters of
// a version 4 or 5 chunk index type, or an error for a type these versions
// cannot hold.
func chunkIndexParamsSize(indexType, flags uint8, sb *Superblock) (int, error) {
	switch indexType {
	case ChunkIndexBTREE:
		return 0, errors.New("version 4 cannot use the v1 B-tree index")
	case ChunkIndexNone:
		return 0, nil
	case ChunkIndexSingle:
		if flags&chunkLayoutFlagSingleWithFilter == 0 {
			return 0, nil
		}

		return int(sb.LengthSize) + 4, nil
	case ChunkIndexFArray:
		return 1, nil
	case ChunkIndexEArray:
		return 5, nil
	case ChunkIndexBT2:
		return 6, nil
	default:
		return 0, fmt.Errorf("unsupported chunk index type %d", indexType)
	}
}

// decodeEArrayParams decodes the five extensible array creation parameters,
// each of which must be nonzero (H5Olayout.c:431-469).
func decodeEArrayParams(data []byte) (EArrayParams, error) {
	for i, b := range data {
		if b == 0 {
			return EArrayParams{}, fmt.Errorf("invalid extensible array creation parameter %d: zero", i)
		}
	}

	return EArrayParams{
		MaxNelmtsBits:         data[0],
		IdxBlkElmts:           data[1],
		SupBlkMinDataPtrs:     data[2],
		DataBlkMinElmts:       data[3],
		MaxDblkPageNelmtsBits: data[4],
	}, nil
}

// decodeBT2Params decodes the v2 B-tree creation parameters: node size, then
// split and merge percentages in 1..100 (H5Olayout.c:481-505).
func decodeBT2Params(data []byte) (BT2Params, error) {
	p := BT2Params{NodeSize: binary.LittleEndian.Uint32(data), SplitPercent: data[4], MergePercent: data[5]}
	if p.SplitPercent == 0 || p.SplitPercent > 100 {
		return p, fmt.Errorf("bad v2 B-tree split percent %d: must be in 1..100", p.SplitPercent)
	}
	if p.MergePercent == 0 || p.MergePercent > 100 {
		return p, fmt.Errorf("bad v2 B-tree merge percent %d: must be in 1..100", p.MergePercent)
	}

	return p, nil
}

// readLayoutAddress decodes a file address of the given width. An address made of
// 0xff bytes only is the encoded "undefined address" at any width and decodes
// to undefinedAddress, as H5F_addr_decode_len does (H5Fint.c:3009-3049): with
// 4-byte offsets an unallocated dataset stores 0xffffffff, which must not be
// taken for a location inside the file.
func readLayoutAddress(data []byte, size int, endianness binary.ByteOrder) uint64 {
	return DecodeAddress(data, size, endianness)
}

// DecodeAddress decodes a file address of size bytes (the superblock's
// offset size). An address made of 0xff bytes only is HADDR_UNDEF at any
// width and decodes to the 64-bit undefined value, as H5F_addr_decode_len
// does (H5Fint.c:3046-3050), so callers compare with a single sentinel.
func DecodeAddress(data []byte, size int, endianness binary.ByteOrder) uint64 {
	n := min(size, len(data))
	allOnes := n > 0
	for _, b := range data[:n] {
		if b != 0xff {
			allOnes = false
			break
		}
	}

	if allOnes {
		return undefinedAddress
	}

	return readUint64(data, size, endianness)
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
