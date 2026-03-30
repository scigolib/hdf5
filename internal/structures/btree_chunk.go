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

	// Per C reference, a single B-tree leaf node holds at most 2K children (K=32 → 64).
	// Multi-level B-trees are not yet supported.
	const chunkBTreeK = 32
	if len(w.entries) > 2*chunkBTreeK {
		return 0, fmt.Errorf("too many chunks (%d) for single B-tree leaf node (max %d); multi-level chunk B-tree not yet supported",
			len(w.entries), 2*chunkBTreeK)
	}

	// 1. Sort entries by coordinate (row-major)
	sort.Slice(w.entries, func(i, j int) bool {
		return compareChunkCoords(w.entries[i].Coordinate, w.entries[j].Coordinate) < 0
	})

	// 2. Build node
	node := &ChunkBTreeNode{
		Signature:    [4]byte{'T', 'R', 'E', 'E'},
		NodeType:     1,                      // Raw Data Chunk (NOT 0 like groups!)
		NodeLevel:    0,                      // Leaf
		EntriesUsed:  uint16(len(w.entries)), //nolint:gosec // G115: HDF5 limits B-tree entries to uint16
		LeftSibling:  0xFFFFFFFFFFFFFFFF,     // Undefined (no siblings)
		RightSibling: 0xFFFFFFFFFFFFFFFF,     // Undefined (no siblings)
	}

	// 3. Add keys and addresses
	for _, entry := range w.entries {
		node.Keys = append(node.Keys, ChunkKey{
			Coords:     entry.Coordinate,
			FilterMask: 0, // No filters in Phase 1
			Nbytes:     entry.Nbytes,
		})
		node.ChildAddrs = append(node.ChildAddrs, entry.Address)
	}

	// 4. Add sentinel key (B-tree requirement: nchildren+1 keys).
	// Per C reference (H5Dbtree.c:646), decode_key validates:
	//   if (0 != (tmp_offset % layout->dim[u])) → error
	// So sentinel coords MUST be divisible by chunk dims.
	// Use the "next chunk position" after the last entry as sentinel.
	onDiskDims := w.dimensionality + 1
	sentinelCoords := make([]uint64, onDiskDims)
	if len(w.entries) > 0 {
		lastEntry := w.entries[len(w.entries)-1]
		for i := 0; i < w.dimensionality && i < len(lastEntry.Coordinate); i++ {
			// Next chunk position = last coord + 1 (in scaled units).
			// Will be multiplied by chunkDim during serialization.
			sentinelCoords[i] = lastEntry.Coordinate[i] + 1
		}
		// Last dimension (element size) stays 0.
	}
	node.Keys = append(node.Keys, ChunkKey{
		Coords:     sentinelCoords,
		FilterMask: 0,
	})

	// 5. Serialize
	// Per C reference (H5Dbtree.c:687-690), keys use ndims+1 dimensions
	// and store byte offsets (scaled * chunkDim), not raw scaled indices.
	buf := serializeChunkBTreeNode(node, onDiskDims, w.chunkDims, w.elementSize)

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
	const chunkBTreeK = 32
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
