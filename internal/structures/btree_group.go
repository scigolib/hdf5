package structures

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
)

// ReadGroupBTreeEntries reads the entries of a group from its version 1
// B-tree ("TREE", type 0) and the symbol table nodes the tree indexes, left to
// right, at every tree depth (WalkGroupBTree, as H5B__iterate_helper,
// H5B.c:1108-1160). Keys are length-sized (H5Gnode.c:165) and children are
// addresses.
//
// A child that does not load fails the whole iteration, as in C
// (H5B__iterate_helper stops on the failed node load): skipping it would
// silently hide links. Such a failure is reported as utils.ErrCorrupt.
func ReadGroupBTreeEntries(r io.ReaderAt, address uint64, sb *core.Superblock) ([]BTreeEntry, error) {
	l := NewGroupBTreeLayout(sb)
	var allEntries []BTreeEntry
	err := WalkGroupBTree(r, sb, address, func(snodAddr uint64) error {
		snodNode, err := ReadGroupSNOD(r, sb, l, snodAddr)
		if err != nil {
			return fmt.Errorf("group B-tree at 0x%x: %w", address, err)
		}
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allEntries, nil
}

// readAddress reads a file address of size bytes; an all-ones address is
// HADDR_UNDEF at any width (core.DecodeAddress, H5Fint.c:3046-3050).
func readAddress(data []byte, size int, endianness binary.ByteOrder) uint64 {
	return core.DecodeAddress(data, size, endianness)
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

	// Use data[:size] for all slicing so gosec can verify bounds statically.
	d := data[:size]

	switch size {
	case 1:
		d[0] = byte(addr) //nolint:gosec // G115: variable-size address encoding
	case 2:
		endianness.PutUint16(d, uint16(addr)) //nolint:gosec // Safe: address size matches offset size
	case 4:
		endianness.PutUint32(d, uint32(addr)) //nolint:gosec // Safe: address size matches offset size
	case 8:
		endianness.PutUint64(d, addr)
	default:
		// Pad to requested size.
		var buf [8]byte
		endianness.PutUint64(buf[:], addr)
		copy(d, buf[:size])
	}
}
