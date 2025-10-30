package structures

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// ChunkBTreeNode represents B-tree v1 node for chunk indexing.
// Format matches HDF5 specification for raw data chunk B-tree.
//
// HDF5 uses B-tree v1 (type 1) to index chunked dataset chunks.
// Each chunk is identified by its N-dimensional coordinate (scaled chunk index).
//
// Format specification (HDF5 Format Spec III.A.2):
// - Signature: "TREE" (4 bytes)
// - Node Type: 1 (Raw Data Chunk)
// - Node Level: 0 (leaf) or 1+ (internal)
// - Entries Used: Number of children/keys
// - Left Sibling: Address of left sibling node (or 0xFFFFFFFFFFFFFFFF)
// - Right Sibling: Address of right sibling node (or 0xFFFFFFFFFFFFFFFF)
// - Keys: N-dimensional chunk coordinates (2K+1 keys)
// - Children: Addresses of child nodes or raw data chunks (2K children)
//
// For MVP (Phase 1):
// - Single leaf node only (no splits)
// - N-dimensional chunk coordinates as keys
// - Row-major sorting of coordinates.
type ChunkBTreeNode struct {
	Signature    [4]byte    // "TREE"
	NodeType     uint8      // 1 = Raw Data Chunk (NOT 0 like groups!)
	NodeLevel    uint8      // 0 = leaf
	EntriesUsed  uint16     // Number of chunks
	LeftSibling  uint64     // 0xFFFFFFFFFFFFFFFF (no siblings in MVP)
	RightSibling uint64     // 0xFFFFFFFFFFFFFFFF
	Keys         []ChunkKey // Chunk coordinates (2K+1 for full node)
	ChildAddrs   []uint64   // Chunk file addresses (2K for full node)
}

// ChunkKey represents N-dimensional chunk coordinate.
//
// Format (per HDF5 spec):
// - Chunk size (bytes): uint32 or uint64 (depends on dataset size)
// - Filter mask: uint32 (0 for no filters)
// - Chunk scaled coordinates: uint64[dimensionality] (row-major)
//
// For MVP (Phase 1):
// - No filters (FilterMask = 0)
// - Chunk size not stored in key (stored in layout message)
// - Only coordinates are used for indexing.
type ChunkKey struct {
	Coords     []uint64 // [dim0, dim1, ..., dimN] (scaled chunk indices)
	FilterMask uint32   // Always 0 for Phase 1 (no compression)
}

// ChunkBTreeWriter builds B-tree for chunk indexing.
//
// This writer constructs a B-tree v1 index for chunked datasets.
// It collects chunk coordinates and addresses, sorts them in row-major
// order, and writes a single leaf node to the file.
//
// Usage:
//
//	writer := NewChunkBTreeWriter(2) // 2D dataset
//	writer.AddChunk([]uint64{0, 0}, chunkAddr1)
//	writer.AddChunk([]uint64{0, 1}, chunkAddr2)
//	writer.AddChunk([]uint64{1, 0}, chunkAddr3)
//	btreeAddr, err := writer.WriteToFile(fileWriter, allocator)
type ChunkBTreeWriter struct {
	dimensionality int
	entries        []ChunkBTreeEntry
}

// ChunkBTreeEntry represents a single chunk in the index.
type ChunkBTreeEntry struct {
	Coordinate []uint64 // Scaled chunk coordinate
	Address    uint64   // File address of raw chunk data
}

// NewChunkBTreeWriter creates new chunk B-tree writer.
//
// Parameters:
//   - dimensionality: Number of dimensions in dataset (1, 2, 3, etc.)
//
// Returns:
//   - ChunkBTreeWriter ready to accept chunks
func NewChunkBTreeWriter(dimensionality int) *ChunkBTreeWriter {
	return &ChunkBTreeWriter{
		dimensionality: dimensionality,
		entries:        make([]ChunkBTreeEntry, 0),
	}
}

// AddChunk adds chunk to index.
//
// Chunks must be added before WriteToFile is called.
// The order of addition does not matter - chunks will be sorted
// in row-major order before writing.
//
// Parameters:
//   - coord: Scaled chunk coordinate [dim0, dim1, ..., dimN]
//   - address: File address where chunk data is written
//
// Example:
//
//	// For 2D dataset with chunk size [10, 20]
//	// Dataset element [5, 15] is in chunk [0, 0]
//	// Dataset element [15, 25] is in chunk [1, 1]
//	writer.AddChunk([]uint64{0, 0}, 1000) // First chunk at address 1000
//	writer.AddChunk([]uint64{1, 1}, 2000) // Second chunk at address 2000
func (w *ChunkBTreeWriter) AddChunk(coord []uint64, address uint64) error {
	if len(coord) != w.dimensionality {
		return fmt.Errorf("coordinate dimensionality mismatch: expected %d, got %d",
			w.dimensionality, len(coord))
	}

	// Copy coordinate to prevent external modification
	coordCopy := make([]uint64, w.dimensionality)
	copy(coordCopy, coord)

	w.entries = append(w.entries, ChunkBTreeEntry{
		Coordinate: coordCopy,
		Address:    address,
	})

	return nil
}

// WriteToFile writes B-tree to file, returns root address.
//
// This method:
// 1. Sorts entries by coordinate (row-major order)
// 2. Builds single leaf node with all entries
// 3. Adds sentinel max key (required by B-tree spec)
// 4. Serializes node to bytes
// 5. Allocates space and writes to file
//
// Parameters:
//   - writer: FileWriter for write operations
//   - allocator: Space allocator
//
// Returns:
//   - uint64: File address of B-tree root node
//   - error: Non-nil if write fails
//
// The returned address should be stored in the Data Layout Message
// (chunked layout v3) as the B-tree address.
func (w *ChunkBTreeWriter) WriteToFile(writer Writer, allocator Allocator) (uint64, error) {
	if len(w.entries) == 0 {
		return 0, fmt.Errorf("no chunks to write (empty B-tree)")
	}

	// 1. Sort entries by coordinate (row-major)
	sort.Slice(w.entries, func(i, j int) bool {
		return compareChunkCoords(w.entries[i].Coordinate, w.entries[j].Coordinate) < 0
	})

	// 2. Build node
	node := &ChunkBTreeNode{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,  // Raw Data Chunk (NOT 0 like groups!)
		NodeLevel:    0,  // Leaf
		EntriesUsed:  uint16(len(w.entries)),
		LeftSibling:  0xFFFFFFFFFFFFFFFF, // Undefined (no siblings)
		RightSibling: 0xFFFFFFFFFFFFFFFF, // Undefined (no siblings)
	}

	// 3. Add keys and addresses
	for _, entry := range w.entries {
		node.Keys = append(node.Keys, ChunkKey{
			Coords:     entry.Coordinate,
			FilterMask: 0, // No filters in Phase 1
		})
		node.ChildAddrs = append(node.ChildAddrs, entry.Address)
	}

	// 4. Add max key (B-tree requirement)
	// The B-tree must have 2K+1 keys for 2K children.
	// The last key is a sentinel "maximum" key (all dimensions = max uint64).
	maxKey := make([]uint64, w.dimensionality)
	for i := range maxKey {
		maxKey[i] = ^uint64(0) // Max value
	}
	node.Keys = append(node.Keys, ChunkKey{
		Coords:     maxKey,
		FilterMask: 0,
	})

	// 5. Serialize
	buf := serializeChunkBTreeNode(node, w.dimensionality)

	// 6. Allocate space
	addr, err := allocator.Allocate(uint64(len(buf)))
	if err != nil {
		return 0, fmt.Errorf("failed to allocate space for B-tree: %w", err)
	}

	// 7. Write to file
	if err := writer.WriteAtAddress(buf, addr); err != nil {
		return 0, fmt.Errorf("failed to write B-tree at address %d: %w", addr, err)
	}

	return addr, nil
}

// serializeChunkBTreeNode serializes node to bytes.
//
// Format:
// - Header: 4 (sig) + 1 (type) + 1 (level) + 2 (entries) + 8 (left) + 8 (right) = 24 bytes
// - Keys: (entries+1) * (dim*8 + 4) bytes
//   - Each key: dim * uint64 (coordinates) + uint32 (filter mask)
// - Children: entries * 8 bytes
//   - Each child: uint64 (chunk address)
//
// Note: We use fixed 8-byte addresses (offsetSize=8) for simplicity in MVP.
// Future versions may support variable offsetSize from superblock.
func serializeChunkBTreeNode(node *ChunkBTreeNode, dimensionality int) []byte {
	// Size calculation
	keySize := dimensionality*8 + 4        // N coords (8 bytes each) + filter mask (4 bytes)
	keysSize := len(node.Keys) * keySize   // All keys
	childrenSize := len(node.ChildAddrs) * 8 // All children (8 bytes each)
	totalSize := 24 + keysSize + childrenSize

	buf := make([]byte, totalSize)
	pos := 0

	// Header
	copy(buf[pos:], node.Signature[:])
	pos += 4
	buf[pos] = node.NodeType
	pos++
	buf[pos] = node.NodeLevel
	pos++
	binary.LittleEndian.PutUint16(buf[pos:], node.EntriesUsed)
	pos += 2
	binary.LittleEndian.PutUint64(buf[pos:], node.LeftSibling)
	pos += 8
	binary.LittleEndian.PutUint64(buf[pos:], node.RightSibling)
	pos += 8

	// Keys (each key is N coords + filter mask)
	for _, key := range node.Keys {
		for _, coord := range key.Coords {
			binary.LittleEndian.PutUint64(buf[pos:], coord)
			pos += 8
		}
		binary.LittleEndian.PutUint32(buf[pos:], key.FilterMask)
		pos += 4
	}

	// Children (chunk addresses)
	for _, addr := range node.ChildAddrs {
		binary.LittleEndian.PutUint64(buf[pos:], addr)
		pos += 8
	}

	return buf
}

// compareChunkCoords compares coordinates in row-major order.
//
// Row-major order means:
// - Compare dimension 0 first (most significant)
// - If equal, compare dimension 1
// - And so on...
//
// Examples (2D):
//   - [0,0] < [0,1] < [1,0] < [1,1]
//   - [2,5] > [2,4]
//   - [1,10] < [2,0]
//
// Returns:
//   - -1: a < b
//   - 0: a == b
//   - 1: a > b
func compareChunkCoords(a, b []uint64) int {
	// Compare dimension by dimension (row-major)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		} else if a[i] > b[i] {
			return 1
		}
	}

	// All dimensions equal
	return 0
}

// Writer interface for WriteAtAddress method.
// Implemented by internal/writer.FileWriter.
type Writer interface {
	WriteAtAddress(data []byte, address uint64) error
}

// Allocator interface for space allocation.
// Implemented by internal/writer.Allocator.
type Allocator interface {
	Allocate(size uint64) (uint64, error)
}
