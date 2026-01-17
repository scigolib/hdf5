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
//
// For group B-trees, the leaf nodes contain:
// - Keys: heap offsets (for sorting/searching)
// - Children: addresses of Symbol Table Nodes (SNODs)
//
// The function follows child pointers to SNODs and collects all entries from them.
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

	// Read number of entries (this is the number of keys used).
	entriesUsed := sb.Endianness.Uint16(header[6:8])
	if entriesUsed == 0 {
		return nil, nil
	}

	// For group B-trees (type 0), the data after header is:
	// - Keys and children interleaved: Key[0], Child[0], Key[1], Child[1], ..., Key[N]
	// - Keys are heap offsets (offsetSize bytes each)
	// - Children are SNOD addresses (offsetSize bytes each)
	// - There are (entriesUsed) children and (entriesUsed+1) keys (but last key might be empty)
	//
	// For leaf nodes, children point to Symbol Table Nodes (SNODs).
	// We need to parse each SNOD to get the actual entries.

	// Calculate data size: interleaved keys and children
	// Pattern: Key[0], Child[0], Key[1], Child[1], ..., Key[entriesUsed], (last child if internal node)
	// For leaf nodes with N children: N+1 keys and N children
	// But we read as pairs: (key, child) repeated
	dataSize := int(entriesUsed) * 2 * int(sb.OffsetSize)  // entriesUsed children + keys interleaved
	data := utils.GetBuffer(dataSize + int(sb.OffsetSize)) // +1 key at end
	defer utils.ReleaseBuffer(data)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	dataOffset := int64(address) + int64(headerSize)
	if _, err := r.ReadAt(data[:dataSize+int(sb.OffsetSize)], dataOffset); err != nil {
		return nil, utils.WrapError("B-tree data read failed", err)
	}

	// Collect all SNOD addresses (children)
	var snodAddresses []uint64
	pos := 0
	for i := uint16(0); i < entriesUsed; i++ {
		// Skip key (heap offset) - we don't need it for enumeration
		pos += int(sb.OffsetSize)

		// Read child address (SNOD) using file's endianness
		childAddr := readAddress(data[pos:], int(sb.OffsetSize), sb.Endianness)
		pos += int(sb.OffsetSize)

		if childAddr != 0 && childAddr != 0xFFFFFFFFFFFFFFFF {
			snodAddresses = append(snodAddresses, childAddr)
		}
	}

	// Parse each SNOD to collect entries
	var allEntries []BTreeEntry
	for _, snodAddr := range snodAddresses {
		snodNode, err := ParseSymbolTableNode(r, snodAddr, sb)
		if err != nil {
			// Skip invalid SNODs
			continue
		}

		// Convert SNOD entries to BTreeEntry format
		for _, entry := range snodNode.Entries {
			allEntries = append(allEntries, BTreeEntry{
				LinkNameOffset:  entry.LinkNameOffset,
				ObjectAddress:   entry.ObjectAddress,
				CacheType:       entry.CacheType,
				Reserved:        0,
				CachedBTreeAddr: entry.CachedBTreeAddr,
				CachedHeapAddr:  entry.CachedHeapAddr,
			})
		}
	}

	return allEntries, nil
}

// readAddress reads a variable-sized address from byte slice using the specified endianness.
//
//nolint:gosec // G602: bounds are checked by clamping size to len(data) before switch
func readAddress(data []byte, size int, endianness binary.ByteOrder) uint64 {
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

// BTreeNodeV1 represents a B-tree version 1 node for group symbol tables.
// This is the "TREE" format used for indexing symbol table nodes.
//
// Format:
// - 4 bytes: Signature ("TREE")
// - 1 byte: Node type (0 = group B-tree)
// - 1 byte: Node level (0 = leaf, 1+ = internal)
// - 2 bytes: Number of entries used
// - offsetSize bytes: Left sibling address (0xFFFFFFFFFFFFFFFF for none)
// - offsetSize bytes: Right sibling address (0xFFFFFFFFFFFFFFFF for none)
// - Then: 2K+1 keys (each offsetSize bytes) alternating with 2K child addresses
//
// For MVP (single node), we simplify:
// - Only leaf nodes (level = 0)
// - Only one child pointer (to symbol table node)
// - Left/right siblings are undefined.
type BTreeNodeV1 struct {
	Signature     [4]byte  // "TREE"
	NodeType      uint8    // 0 = group symbol table
	NodeLevel     uint8    // 0 = leaf
	EntriesUsed   uint16   // Number of entries
	LeftSibling   uint64   // Address of left sibling (UNDEF for none)
	RightSibling  uint64   // Address of right sibling (UNDEF for none)
	Keys          []uint64 // Link name offsets in heap (2K+1 for full node)
	ChildPointers []uint64 // Child node addresses (2K for full node, or symbol table node addresses for leaf)
}

// NewBTreeNodeV1 creates a new B-tree v1 node for group symbol tables.
// For MVP, this is always a leaf node pointing to a single symbol table node.
func NewBTreeNodeV1(nodeType uint8, k uint16) *BTreeNodeV1 {
	return &BTreeNodeV1{
		Signature:     [4]byte{'T', 'R', 'E', 'E'},
		NodeType:      nodeType,
		NodeLevel:     0, // Leaf node for MVP
		EntriesUsed:   0,
		LeftSibling:   0xFFFFFFFFFFFFFFFF,            // Undefined
		RightSibling:  0xFFFFFFFFFFFFFFFF,            // Undefined
		Keys:          make([]uint64, 0, 2*int(k)+1), // 2K+1 keys
		ChildPointers: make([]uint64, 0, 2*int(k)),   // 2K children
	}
}

// AddKey adds a key and child pointer to the B-tree node.
// For leaf nodes in groups, keys are link name offsets in the local heap,
// and child pointers are addresses of symbol table nodes.
func (btn *BTreeNodeV1) AddKey(key, childAddr uint64) error {
	maxKeys := cap(btn.Keys)
	if len(btn.Keys) >= maxKeys {
		return fmt.Errorf("b-tree node is full (%d/%d keys)", len(btn.Keys), maxKeys)
	}

	// For MVP: simple append (no balancing)
	btn.Keys = append(btn.Keys, key)
	btn.ChildPointers = append(btn.ChildPointers, childAddr)
	btn.EntriesUsed++

	return nil
}

// WriteAt writes the B-tree node to w at the specified address.
// offsetSize determines the size of addresses in the file (typically 8).
// K is the B-tree order (default 16, so 2K+1 = 33 keys).
func (btn *BTreeNodeV1) WriteAt(w io.WriterAt, address uint64, offsetSize uint8, k uint16, endianness binary.ByteOrder) error {
	// Calculate sizes
	maxKeys := 2*int(k) + 1
	maxChildren := 2 * int(k)

	// Header size: 4 (sig) + 1 (type) + 1 (level) + 2 (entries) + 2*offsetSize (siblings)
	headerSize := 8 + 2*int(offsetSize)

	// Keys and children are interleaved:
	// Key[0], Child[0], Key[1], Child[1], ..., Key[2K], Child[2K-1], Key[2K]
	// Total: (2K+1) keys * offsetSize + 2K children * offsetSize
	keysSize := maxKeys * int(offsetSize)
	childrenSize := maxChildren * int(offsetSize)

	totalSize := headerSize + keysSize + childrenSize
	buf := make([]byte, totalSize)

	// Write header
	pos := 0
	copy(buf[pos:], btn.Signature[:])
	pos += 4
	buf[pos] = btn.NodeType
	pos++
	buf[pos] = btn.NodeLevel
	pos++
	endianness.PutUint16(buf[pos:], btn.EntriesUsed)
	pos += 2

	// Write left sibling
	writeAddr(buf[pos:], btn.LeftSibling, int(offsetSize), endianness)
	pos += int(offsetSize)

	// Write right sibling
	writeAddr(buf[pos:], btn.RightSibling, int(offsetSize), endianness)
	pos += int(offsetSize)

	// Write keys and children (interleaved)
	// For a leaf B-tree in groups: keys are heap offsets, children are symbol table node addresses
	for i := 0; i < maxKeys; i++ {
		var key uint64
		if i < len(btn.Keys) {
			key = btn.Keys[i]
		}
		// If i >= len(Keys), key is 0 (padding)

		writeAddr(buf[pos:], key, int(offsetSize), endianness)
		pos += int(offsetSize)

		// After each key (except the last), write a child pointer
		if i < maxChildren {
			var child uint64
			if i < len(btn.ChildPointers) {
				child = btn.ChildPointers[i]
			}

			writeAddr(buf[pos:], child, int(offsetSize), endianness)
			pos += int(offsetSize)
		}
	}

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.WriterAt interface
	_, err := w.WriteAt(buf, int64(address))
	return err
}

// writeAddr writes a variable-sized address to byte slice.
func writeAddr(data []byte, addr uint64, size int, endianness binary.ByteOrder) {
	if size > len(data) {
		size = len(data)
	}

	switch size {
	case 1:
		data[0] = byte(addr)
	case 2:
		endianness.PutUint16(data[:2], uint16(addr)) //nolint:gosec // Safe: address size matches offset size
	case 4:
		endianness.PutUint32(data[:4], uint32(addr)) //nolint:gosec // Safe: address size matches offset size
	case 8:
		endianness.PutUint64(data[:8], addr)
	default:
		// Pad to requested size
		var buf [8]byte
		endianness.PutUint64(buf[:], addr)
		copy(data[:size], buf[:size])
	}
}
