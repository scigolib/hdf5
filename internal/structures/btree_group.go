package structures

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// ReadGroupBTreeEntries reads entries from a "TREE" format B-tree (type 0 - group symbol table).
// This is the v1 B-tree format used in v0 and some v1 HDF5 files for indexing group entries.
func ReadGroupBTreeEntries(r io.ReaderAt, address uint64, sb *core.Superblock) ([]BTreeEntry, error) {
	// Read B-tree node header.
	// Format:
	// - 4 bytes: Signature ("TREE").
	// - 1 byte: Node type (0 = group B-tree).
	// - 1 byte: Node level (0 = leaf).
	// - 2 bytes: Number of entries used.
	// - offsetSize bytes: Left sibling address.
	// - offsetSize bytes: Right sibling address.

	headerSize := 4 + 1 + 1 + 2 + int(sb.OffsetSize)*2
	header := utils.GetBuffer(headerSize)
	defer utils.ReleaseBuffer(header)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(header, int64(address)); err != nil {
		return nil, utils.WrapError("B-tree node header read failed", err)
	}

	// Check signature.
	sig := string(header[0:4])
	if sig != "TREE" {
		return nil, fmt.Errorf("invalid B-tree signature: %q (expected TREE)", sig)
	}

	// Check node type (should be 0 for groups).
	nodeType := header[4]
	if nodeType != 0 {
		return nil, fmt.Errorf("expected group B-tree (type 0), got type %d", nodeType)
	}

	// Check node level (we only support leaf nodes for now).
	nodeLevel := header[5]
	if nodeLevel != 0 {
		return nil, errors.New("non-leaf B-tree nodes not supported yet")
	}

	// Read number of entries.
	entriesUsed := sb.Endianness.Uint16(header[6:8])
	if entriesUsed == 0 {
		return nil, nil
	}

	// Skip left/right sibling addresses (we don't need them for simple traversal).
	// Now read the symbol table entries.
	// Each entry has:
	// - offsetSize bytes: Link name offset in local heap.
	// - offsetSize bytes: Object header address.
	// - 4 bytes: Cache type.
	// - 4 bytes: Reserved.
	// Plus there are 2*K keys separating the entries (for non-leaf nodes).
	// For leaf nodes (level 0), entries follow directly after header.

	entrySize := int(sb.OffsetSize)*2 + 4 + 4 // link offset + obj addr + cache + reserved.
	dataSize := int(entriesUsed) * entrySize

	data := utils.GetBuffer(dataSize)
	defer utils.ReleaseBuffer(data)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	dataOffset := int64(address) + int64(headerSize)
	if _, err := r.ReadAt(data, dataOffset); err != nil {
		return nil, utils.WrapError("B-tree entries read failed", err)
	}

	// Parse entries.
	var entries []BTreeEntry
	offset := 0

	for i := uint16(0); i < entriesUsed; i++ {
		if offset+entrySize > len(data) {
			return nil, fmt.Errorf("b-tree data truncated at entry %d", i)
		}

		// Read link name offset.
		linkOffset := readAddress(data[offset:], int(sb.OffsetSize))
		offset += int(sb.OffsetSize)

		// Read object header address.
		objAddr := readAddress(data[offset:], int(sb.OffsetSize))
		offset += int(sb.OffsetSize)

		// Read cache type.
		cacheType := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		// Read reserved.
		reserved := sb.Endianness.Uint32(data[offset : offset+4])
		offset += 4

		entries = append(entries, BTreeEntry{
			LinkNameOffset: linkOffset,
			ObjectAddress:  objAddr,
			CacheType:      cacheType,
			Reserved:       reserved,
		})
	}

	return entries, nil
}

// readAddress reads a variable-sized address from byte slice.
func readAddress(data []byte, size int) uint64 {
	if size > len(data) {
		size = len(data)
	}

	switch size {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(data[:2]))
	case 4:
		return uint64(binary.LittleEndian.Uint32(data[:4]))
	case 8:
		return binary.LittleEndian.Uint64(data[:8])
	default:
		// Pad to 8 bytes.
		var buf [8]byte
		copy(buf[:], data[:size])
		return binary.LittleEndian.Uint64(buf[:])
	}
}
