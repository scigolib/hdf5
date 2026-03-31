package structures

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// chunkBTreeK is the B-tree "K" parameter for chunked datasets.
// Per C reference (H5Dbtree.c), HDF5_BTREE_CHUNK_IK_DEF = 32.
// Each node holds at most 2K = 64 children.
const chunkBTreeK = 32

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
// - Nbytes: uint32 (chunk size in bytes after filtering)
// - Filter mask: uint32 (0 for no filters)
// - Chunk scaled coordinates: uint64[dimensionality] (row-major, stored as byte offsets)
//
// For MVP (Phase 1):
// - No filters (FilterMask = 0)
// - Nbytes is set to chunk data size.
type ChunkKey struct {
	Coords     []uint64 // [dim0, dim1, ..., dimN] (scaled chunk indices)
	FilterMask uint32   // Always 0 for Phase 1 (no compression)
	Nbytes     uint32   // Chunk size in bytes (after filtering)
}

// ChunkBTreeWriter builds B-tree for chunk indexing.
//
// This writer constructs a B-tree v1 index for chunked datasets.
// It collects chunk coordinates and addresses, sorts them in row-major
// order, and writes a single leaf node to the file.
//
// Per C reference (H5Dbtree.c:687-690), B-tree keys store byte offsets
// (scaled coordinate * chunk dimension), not raw scaled indices.
// The key also includes an extra trailing dimension for the datatype size
// (always 0 in the coordinate, per H5Dchunk.c:913).
//
// Usage:
//
//	writer := NewChunkBTreeWriter(2, []uint64{10, 20}, 8) // 2D dataset, chunk 10x20, float64
//	writer.AddChunk([]uint64{0, 0}, chunkAddr1)
//	writer.AddChunk([]uint64{0, 1}, chunkAddr2)
//	writer.AddChunk([]uint64{1, 0}, chunkAddr3)
//	btreeAddr, err := writer.WriteToFile(fileWriter, allocator)
type ChunkBTreeWriter struct {
	dimensionality int
	chunkDims      []uint64 // Chunk dimensions for coordinate-to-byte-offset conversion.
	elementSize    uint32   // Datatype element size (stored as trailing dimension value 0 in keys).
	entries        []ChunkBTreeEntry
}

// ChunkBTreeEntry represents a single chunk in the index.
type ChunkBTreeEntry struct {
	Coordinate []uint64 // Scaled chunk coordinate
	Address    uint64   // File address of raw chunk data
	Nbytes     uint32   // Chunk size in bytes (after filtering)
}

// NewChunkBTreeWriter creates new chunk B-tree writer.
//
// Per C reference (H5Dbtree.c:687-690), B-tree keys encode coordinates as
// byte offsets (scaled * chunkDim), not raw scaled indices. The chunkDims
// and elementSize parameters are required for this conversion.
//
// Parameters:
//   - dimensionality: Number of dimensions in dataset (1, 2, 3, etc.)
//   - chunkDims: Chunk dimensions for each axis (used to convert scaled coords to byte offsets)
//   - elementSize: Datatype element size in bytes (stored as trailing 0-valued dimension)
//
// Returns:
//   - ChunkBTreeWriter ready to accept chunks.
func NewChunkBTreeWriter(dimensionality int, chunkDims []uint64, elementSize uint32) *ChunkBTreeWriter {
	dimsCopy := make([]uint64, len(chunkDims))
	copy(dimsCopy, chunkDims)
	return &ChunkBTreeWriter{
		dimensionality: dimensionality,
		chunkDims:      dimsCopy,
		elementSize:    elementSize,
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
	return w.AddChunkWithSize(coord, address, 0)
}

// AddChunkWithSize adds chunk to index with explicit size.
//
// Parameters:
//   - coord: Scaled chunk coordinate [dim0, dim1, ..., dimN]
//   - address: File address where chunk data is written
//   - nbytes: Size of chunk data in bytes (after filtering)
func (w *ChunkBTreeWriter) AddChunkWithSize(coord []uint64, address uint64, nbytes uint32) error {
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
		Nbytes:     nbytes,
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

	// 1. Sort entries by coordinate (row-major).
	sort.Slice(w.entries, func(i, j int) bool {
		return compareChunkCoords(w.entries[i].Coordinate, w.entries[j].Coordinate) < 0
	})

	maxPerNode := 2 * chunkBTreeK // 64

	// 2. If entries fit in a single leaf, write a single node (fast path).
	if len(w.entries) <= maxPerNode {
		return w.writeSingleLeaf(writer, allocator)
	}

	// 3. Multi-level: build bottom-up.
	return w.buildMultiLevelTree(writer, allocator)
}

// writeSingleLeaf writes all entries as a single leaf node (level 0).
// This is the original behavior for datasets with at most 64 chunks.
func (w *ChunkBTreeWriter) writeSingleLeaf(writer Writer, allocator Allocator) (uint64, error) {
	onDiskDims := w.dimensionality + 1

	node := w.buildLeafNode(w.entries, 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF)
	buf := serializeChunkBTreeNode(node, onDiskDims, w.chunkDims, w.elementSize)

	addr, err := allocator.Allocate(uint64(len(buf)))
	if err != nil {
		return 0, fmt.Errorf("failed to allocate space for B-tree: %w", err)
	}

	if err := writer.WriteAtAddress(buf, addr); err != nil {
		return 0, fmt.Errorf("failed to write B-tree at address %d: %w", addr, err)
	}

	return addr, nil
}

// buildLeafNode creates a ChunkBTreeNode (level 0) from the given entries
// with specified sibling addresses.
func (w *ChunkBTreeWriter) buildLeafNode(entries []ChunkBTreeEntry, leftSibling, rightSibling uint64) *ChunkBTreeNode {
	onDiskDims := w.dimensionality + 1

	node := &ChunkBTreeNode{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,
		NodeLevel:    0,
		EntriesUsed:  uint16(len(entries)), //nolint:gosec // G115: HDF5 limits B-tree entries to uint16
		LeftSibling:  leftSibling,
		RightSibling: rightSibling,
	}

	for _, entry := range entries {
		node.Keys = append(node.Keys, ChunkKey{
			Coords:     entry.Coordinate,
			FilterMask: 0,
			Nbytes:     entry.Nbytes,
		})
		node.ChildAddrs = append(node.ChildAddrs, entry.Address)
	}

	// Sentinel key: next chunk position after last entry.
	sentinelCoords := make([]uint64, onDiskDims)
	if len(entries) > 0 {
		lastEntry := entries[len(entries)-1]
		for i := 0; i < w.dimensionality && i < len(lastEntry.Coordinate); i++ {
			sentinelCoords[i] = lastEntry.Coordinate[i] + 1
		}
	}
	node.Keys = append(node.Keys, ChunkKey{
		Coords:     sentinelCoords,
		FilterMask: 0,
	})

	return node
}

// internalChild holds address and boundary keys for a child node in an internal B-tree node.
type internalChild struct {
	addr     uint64   // File address of child node.
	firstKey ChunkKey // First key of the child (left boundary).
	lastKey  ChunkKey // Sentinel key of the child (right boundary).
}

// buildMultiLevelTree constructs a multi-level B-tree v1 bottom-up.
//
// Algorithm:
//  1. Partition sorted entries into groups of at most 2K (64) entries.
//  2. For each group, build a leaf node (level 0) with sibling links.
//  3. Allocate addresses for all leaf nodes, then serialize and write them.
//  4. Build internal levels (level 1, 2, ...) from child node metadata until
//     only one root node remains.
//  5. Return root node address.
func (w *ChunkBTreeWriter) buildMultiLevelTree(writer Writer, allocator Allocator) (uint64, error) {
	maxPerNode := 2 * chunkBTreeK
	onDiskDims := w.dimensionality + 1

	// --- Level 0: leaf nodes ---

	// Partition entries into groups.
	var leafGroups [][]ChunkBTreeEntry
	for i := 0; i < len(w.entries); i += maxPerNode {
		end := i + maxPerNode
		if end > len(w.entries) {
			end = len(w.entries)
		}
		leafGroups = append(leafGroups, w.entries[i:end])
	}

	// Compute node size (same for all nodes regardless of level).
	// Per C reference (H5B.c:1670-1678):
	//   sizeof_rkey = 4 + 4 + onDiskDims*8
	//   sizeof_rnode = 24 + 2K*8 + (2K+1)*sizeof_rkey
	keySize := 4 + 4 + onDiskDims*8
	nodeSize := uint64(24 + 2*chunkBTreeK*8 + (2*chunkBTreeK+1)*keySize) //nolint:gosec // G115: constant expression, no overflow risk

	// Pass 1: allocate addresses for all leaf nodes.
	leafAddrs := make([]uint64, len(leafGroups))
	for i := range leafGroups {
		addr, err := allocator.Allocate(nodeSize)
		if err != nil {
			return 0, fmt.Errorf("failed to allocate leaf node %d: %w", i, err)
		}
		leafAddrs[i] = addr
	}

	// Pass 2: build, serialize and write each leaf node with correct sibling links.
	children := make([]internalChild, len(leafGroups))
	for i, group := range leafGroups {
		leftSib := uint64(0xFFFFFFFFFFFFFFFF)
		rightSib := uint64(0xFFFFFFFFFFFFFFFF)
		if i > 0 {
			leftSib = leafAddrs[i-1]
		}
		if i < len(leafGroups)-1 {
			rightSib = leafAddrs[i+1]
		}

		node := w.buildLeafNode(group, leftSib, rightSib)
		buf := serializeChunkBTreeNode(node, onDiskDims, w.chunkDims, w.elementSize)

		if err := writer.WriteAtAddress(buf, leafAddrs[i]); err != nil {
			return 0, fmt.Errorf("failed to write leaf node %d at address %d: %w", i, leafAddrs[i], err)
		}

		// Record child metadata for parent internal node.
		// firstKey = first key of this leaf, lastKey = sentinel (last key).
		children[i] = internalChild{
			addr:     leafAddrs[i],
			firstKey: node.Keys[0],
			lastKey:  node.Keys[len(node.Keys)-1],
		}
	}

	// --- Internal levels (level 1, 2, ...) ---
	level := uint8(1)
	for len(children) > 1 {
		children, _ = w.writeInternalLevel(writer, allocator, children, level, nodeSize)
		if len(children) == 0 {
			return 0, fmt.Errorf("failed to build internal level %d", level)
		}
		level++
	}

	// The single remaining child is the root.
	return children[0].addr, nil
}

// writeInternalLevel builds and writes one level of internal B-tree nodes from
// the given child list. Returns a new (smaller) child list for the next level.
func (w *ChunkBTreeWriter) writeInternalLevel(
	writer Writer,
	allocator Allocator,
	children []internalChild,
	level uint8,
	nodeSize uint64,
) ([]internalChild, error) {
	maxPerNode := 2 * chunkBTreeK
	onDiskDims := w.dimensionality + 1

	// Partition children into groups of at most 2K.
	var groups [][]internalChild
	for i := 0; i < len(children); i += maxPerNode {
		end := i + maxPerNode
		if end > len(children) {
			end = len(children)
		}
		groups = append(groups, children[i:end])
	}

	// Pass 1: allocate addresses for all internal nodes at this level.
	nodeAddrs := make([]uint64, len(groups))
	for i := range groups {
		addr, err := allocator.Allocate(nodeSize)
		if err != nil {
			return nil, fmt.Errorf("failed to allocate internal node level %d, node %d: %w", level, i, err)
		}
		nodeAddrs[i] = addr
	}

	// Pass 2: build, serialize, and write each internal node.
	result := make([]internalChild, len(groups))
	for i, group := range groups {
		leftSib := uint64(0xFFFFFFFFFFFFFFFF)
		rightSib := uint64(0xFFFFFFFFFFFFFFFF)
		if i > 0 {
			leftSib = nodeAddrs[i-1]
		}
		if i < len(groups)-1 {
			rightSib = nodeAddrs[i+1]
		}

		node := &ChunkBTreeNode{
			Signature:    [4]byte{'T', 'R', 'E', 'E'},
			NodeType:     1,
			NodeLevel:    level,
			EntriesUsed:  uint16(len(group)), //nolint:gosec // G115: HDF5 limits B-tree entries to uint16
			LeftSibling:  leftSib,
			RightSibling: rightSib,
		}

		// Internal node keys: key[i] = first key of child[i] (boundary key).
		// The sentinel (last key) = sentinel of the rightmost child.
		for _, child := range group {
			node.Keys = append(node.Keys, child.firstKey)
			node.ChildAddrs = append(node.ChildAddrs, child.addr)
		}
		// Sentinel: use the sentinel (lastKey) from the last child in this group.
		node.Keys = append(node.Keys, group[len(group)-1].lastKey)

		buf := serializeChunkBTreeNode(node, onDiskDims, w.chunkDims, w.elementSize)

		if err := writer.WriteAtAddress(buf, nodeAddrs[i]); err != nil {
			return nil, fmt.Errorf("failed to write internal node level %d, node %d at address %d: %w",
				level, i, nodeAddrs[i], err)
		}

		result[i] = internalChild{
			addr:     nodeAddrs[i],
			firstKey: node.Keys[0],
			lastKey:  node.Keys[len(node.Keys)-1],
		}
	}

	return result, nil
}

// serializeChunkBTreeNode serializes node to bytes.
//
// Per C reference (H5Dbtree.c:687-690), each key coordinate is stored as a
// byte offset (scaled_index * chunk_dimension), not as a raw scaled index.
// The key has onDiskDims coordinates, where the last coordinate corresponds
// to the datatype size dimension and is always 0 for data keys.
//
// Format:
// - Header: 4 (sig) + 1 (type) + 1 (level) + 2 (entries) + 8 (left) + 8 (right) = 24 bytes
// - For each entry (interleaved keys and children):
//   - Key: nbytes (4) + filter_mask (4) + coords (onDiskDims*8)
//   - Child: address (8 bytes)
//
// - Final key (sentinel): nbytes (4) + filter_mask (4) + coords (onDiskDims*8)
//
// Parameters:
//   - node: The B-tree node to serialize
//   - onDiskDims: Number of dimensions on disk (ndims+1, includes datatype size dimension)
//   - chunkDims: Chunk dimensions for converting scaled coords to byte offsets
//   - elementSize: Datatype element size (for the trailing dimension)
//
// Note: We use fixed 8-byte addresses (offsetSize=8) for simplicity in MVP.
// Future versions may support variable offsetSize from superblock.
func serializeChunkBTreeNode(node *ChunkBTreeNode, onDiskDims int, chunkDims []uint64, _ uint32) []byte {
	// Per C reference (H5B.c:1670-1678, H5Dbtree.c:773-776):
	// The C library always reads sizeof_rnode bytes from disk, computed as:
	//   sizeof_rkey = 4 (nbytes) + 4 (filter_mask) + onDiskDims*8 (coords)
	//   sizeof_rnode = H5B_SIZEOF_HDR(24) + 2K*sizeof_addr(8) + (2K+1)*sizeof_rkey
	// Default K for chunk B-trees = 32 (HDF5_BTREE_CHUNK_IK_DEF).
	// The buffer MUST be exactly sizeof_rnode bytes, zero-padded for unused slots.
	const offsetSize = 8 // sizeof_addr
	keySize := 4 + 4 + onDiskDims*8
	twoK := 2 * chunkBTreeK
	totalSize := 24 + twoK*offsetSize + (twoK+1)*keySize

	buf := make([]byte, totalSize) // zero-initialized — unused slots are zeros
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

	// Write keys and children interleaved: key0, child0, key1, child1, ..., keyN
	for i, key := range node.Keys {
		// Write key: nbytes + filter_mask + coords (as byte offsets)
		binary.LittleEndian.PutUint32(buf[pos:], key.Nbytes)
		pos += 4
		binary.LittleEndian.PutUint32(buf[pos:], key.FilterMask)
		pos += 4

		// Per C reference (H5Dbtree.c:687-690):
		//   tmp_offset = key->scaled[u] * layout->dim[u];
		//   UINT64ENCODE(raw, tmp_offset);
		// Convert each scaled coordinate to byte offset by multiplying by chunk dimension.
		// The key.Coords may already be in on-disk format (sentinel key with onDiskDims values),
		// or in scaled format (data key with dimensionality values).
		for j := 0; j < onDiskDims; j++ {
			if j < len(key.Coords) {
				coord := key.Coords[j]
				// For the sentinel key (max values), write as-is.
				// For data keys, convert scaled index to byte offset.
				if coord != ^uint64(0) && j < len(chunkDims) {
					coord *= chunkDims[j]
				}
				binary.LittleEndian.PutUint64(buf[pos:], coord)
			} else {
				// Trailing dimension (datatype size): always 0 for data keys.
				binary.LittleEndian.PutUint64(buf[pos:], 0)
			}
			pos += 8
		}

		// Write child address (except for the last sentinel key)
		if i < len(node.ChildAddrs) {
			binary.LittleEndian.PutUint64(buf[pos:], node.ChildAddrs[i])
			pos += 8
		}
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
