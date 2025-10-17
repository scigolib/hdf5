package structures

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// SymbolTableNode represents a Symbol Table Node (SNOD structure).
// This is different from SymbolTable - it contains actual entries, not addresses.
type SymbolTableNode struct {
	Version    uint8
	NumSymbols uint16
	Entries    []SymbolTableEntry
}

// ParseSymbolTableNode parses a Symbol Table Node (SNOD).
// Format:
// - 4 bytes: Signature ("SNOD").
// - 1 byte: Version (1).
// - 1 byte: Reserved (0).
// - 2 bytes: Number of symbols.
// - Then symbol table entries follow (each entry is offsetSize*2 + 8 + 16 bytes).
func ParseSymbolTableNode(r io.ReaderAt, address uint64, sb *core.Superblock) (*SymbolTableNode, error) {
	// Read header (8 bytes).
	header := utils.GetBuffer(8)
	defer utils.ReleaseBuffer(header)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(header, int64(address)); err != nil {
		return nil, utils.WrapError("SNOD header read failed", err)
	}

	// Check signature.
	sig := string(header[0:4])
	if sig != "SNOD" {
		return nil, fmt.Errorf("invalid SNOD signature: %q", sig)
	}

	version := header[4]
	if version != 1 {
		return nil, fmt.Errorf("unsupported SNOD version: %d", version)
	}

	numSymbols := sb.Endianness.Uint16(header[6:8])

	node := &SymbolTableNode{
		Version:    version,
		NumSymbols: numSymbols,
		Entries:    make([]SymbolTableEntry, 0, numSymbols),
	}

	if numSymbols == 0 {
		return node, nil
	}

	// Each symbol table entry format:
	// - offsetSize bytes: Link name offset in local heap.
	// - offsetSize bytes: Object header address.
	// - 4 bytes: Cache type.
	// - 4 bytes: Reserved.
	// - 16 bytes: Scratch-pad (cache-type specific).
	entrySize := int(sb.OffsetSize)*2 + 4 + 4 + 16

	// Read all entries.
	dataSize := int(numSymbols) * entrySize
	data := utils.GetBuffer(dataSize)
	defer utils.ReleaseBuffer(data)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	entryOffset := int64(address) + 8 // After header.
	if _, err := r.ReadAt(data, entryOffset); err != nil {
		return nil, utils.WrapError("SNOD entries read failed", err)
	}

	// Parse entries.
	offset := 0
	for i := uint16(0); i < numSymbols; i++ {
		if offset+entrySize > len(data) {
			return nil, fmt.Errorf("SNOD data truncated at entry %d", i)
		}

		// Read link name offset.
		linkOffset := readAddressFromBytes(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read object header address.
		objAddr := readAddressFromBytes(data[offset:], int(sb.OffsetSize), sb.Endianness)
		offset += int(sb.OffsetSize)

		// Read cache type.
		cacheType := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		// Read reserved.
		reserved := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		// Skip scratch-pad (16 bytes).
		offset += 16

		node.Entries = append(node.Entries, SymbolTableEntry{
			LinkNameOffset: linkOffset,
			ObjectAddress:  objAddr,
			CacheType:      cacheType,
			Reserved:       reserved,
		})
	}

	return node, nil
}

// readAddressFromBytes reads a variable-sized address from byte slice.
func readAddressFromBytes(data []byte, size int, endianness binary.ByteOrder) uint64 {
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
		// Pad to 8 bytes.
		var buf [8]byte
		copy(buf[:], data[:size])
		return endianness.Uint64(buf[:])
	}
}
