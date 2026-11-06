package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/scigolib/hdf5/internal/utils"
)

// BTreeV1Node represents a B-tree version 1 node.
// Used for indexing chunked dataset storage.
// Reference: H5Bpkg.h, H5Dbtree.c.
type BTreeV1Node struct {
	Address      uint64  // File address of this node.
	Signature    [4]byte // Should be "TREE".
	NodeType     uint8   // Type of B-tree node.
	NodeLevel    uint8   // Level of node (0 = leaf).
	EntriesUsed  uint16  // Number of entries currently used.
	LeftSibling  uint64  // Address of left sibling (or UNDEFINED).
	RightSibling uint64  // Address of right sibling (or UNDEFINED).

	// For chunk B-tree (type 1), keys are chunk coordinates.
	// Each entry has: key + child pointer.
	Keys     []ChunkKey // Keys for this node.
	Children []uint64   // Child node/chunk addresses.
}

// ChunkKey represents coordinates for a chunk in N-dimensional space.
type ChunkKey struct {
	Scaled     []uint64 // Scaled chunk indices [dim0, dim1, ...].
	Nbytes     uint32   // Size of stored chunk data in bytes.
	FilterMask uint32   // Excluded filters mask.
}

// ParseBTreeV1Node parses a B-tree v1 node from file.
// chunkDims: chunk dimensions for converting byte offsets to scaled indices.
//
// Note: B-tree coordinates are ALWAYS stored as uint64 in the file format.
// The chunk dimensions in the layout message can be uint32 or uint64 depending on file version,
// but the B-tree keys always use uint64 for backward compatibility.
func ParseBTreeV1Node(r io.ReaderAt, address uint64, offsetSize uint8, ndims int, chunkDims []uint64) (*BTreeV1Node, error) {
	// Read node header (fixed size part).
	headerSize := 4 + 1 + 1 + 2 + int(offsetSize)*2 // signature + type + level + entries + 2 siblings.
	header := make([]byte, headerSize)

	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(header, int64(address)); err != nil {
		return nil, fmt.Errorf("failed to read B-tree node header: %w", err)
	}

	node := &BTreeV1Node{Address: address}
	offset := 0

	// Signature (4 bytes) - should be "TREE".
	copy(node.Signature[:], header[offset:offset+4])
	if string(node.Signature[:]) != "TREE" {
		return nil, fmt.Errorf("invalid B-tree signature: %s", string(node.Signature[:]))
	}
	offset += 4

	// Node type (1 byte) - for chunk B-tree, type = 1.
	node.NodeType = header[offset]
	// Only raw-data chunk B-trees (type 1) are parsed here; the cache rejects
	// any other type for this index (H5Bcache.c:169-170).
	if node.NodeType != btreeTypeChunk {
		return nil, fmt.Errorf("%w: B-tree node at 0x%X has type %d, want %d (chunk index)",
			utils.ErrCorrupt, address, node.NodeType, btreeTypeChunk)
	}
	offset++

	// Node level (1 byte) - 0 = leaf node.
	node.NodeLevel = header[offset]
	offset++

	// Entries used (2 bytes).
	node.EntriesUsed = binary.LittleEndian.Uint16(header[offset : offset+2])
	offset += 2

	// Left sibling address.
	node.LeftSibling = readAddress(header[offset:], int(offsetSize))
	offset += int(offsetSize)

	// Right sibling address.
	node.RightSibling = readAddress(header[offset:], int(offsetSize))
	// offset is not used after this point.

	// Now read keys and child pointers.
	// For chunk B-tree (type 1):
	// - Each key consists of: nbytes (4) + filter_mask (4) + ndims*8 (coordinates as byte offsets).
	// - Each child pointer is offsetSize bytes.
	// - Format: key0, child0, key1, child1, ..., keyN, childN.
	//
	// NOTE: Coordinates in B-tree keys are ALWAYS stored as uint64 (8 bytes each).
	// The chunkKeySize parameter only affects the layout message chunk dimensions,
	// not the B-tree key coordinates!

	if node.EntriesUsed == 0 {
		return node, nil
	}

	// Calculate size needed for keys and children.
	// Key format (from H5D__btree_decode_key):
	// - 4 bytes: nbytes (chunk size).
	// - 4 bytes: filter_mask.
	// - ndims * 8 bytes: coordinates (as byte offsets, ALWAYS uint64).
	keySize := 4 + 4 + ndims*8
	childSize := int(offsetSize)
	entrySize := keySize + childSize
	// dataSize is bounded by the fields read above: EntriesUsed is uint16
	// (<= 65535) and ndims is the dataspace rank, which comes from the message's
	// uint8 dimensionality field (<= 255), so the buffer is at most
	// 65535 * (8 + 255*8 + offsetSize) ≈ 134 MB. That is not an OOM vector, so
	// the direct read stays; ADR-003 records the bound.
	dataSize := int(node.EntriesUsed)*entrySize + keySize // +keySize for final key.

	data := make([]byte, dataSize)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(data, int64(address)+int64(headerSize)); err != nil {
		return nil, fmt.Errorf("failed to read B-tree node data: %w", err)
	}

	// Parse keys and children.
	node.Keys = make([]ChunkKey, int(node.EntriesUsed)+1) // +1 because there's always 1 more key than children.
	node.Children = make([]uint64, node.EntriesUsed)

	dataOffset := 0
	for i := 0; i <= int(node.EntriesUsed); i++ {
		// Read key.
		if dataOffset+keySize > len(data) {
			return nil, errors.New("b-tree data truncated reading key")
		}

		key := ChunkKey{Scaled: make([]uint64, ndims)}

		// Read nbytes (4 bytes).
		key.Nbytes = binary.LittleEndian.Uint32(data[dataOffset : dataOffset+4])
		dataOffset += 4

		// Read filter_mask (4 bytes).
		key.FilterMask = binary.LittleEndian.Uint32(data[dataOffset : dataOffset+4])
		dataOffset += 4

		// Read coordinates (ndims * 8 bytes each) as byte offsets.
		if err := decodeChunkKeyCoords(data[dataOffset:dataOffset+ndims*8], address, chunkDims, key.Scaled); err != nil {
			return nil, err
		}
		dataOffset += ndims * 8

		node.Keys[i] = key

		// Read child pointer (except after last key).
		if i < int(node.EntriesUsed) {
			if dataOffset+childSize > len(data) {
				return nil, errors.New("b-tree data truncated reading child")
			}
			node.Children[i] = readAddress(data[dataOffset:], childSize)
			dataOffset += childSize
		}
	}

	return node, nil
}

// decodeChunkKeyCoords converts the byte-offset coordinates of one chunk key
// (8 bytes each in raw) to scaled chunk indices, as H5D__btree_decode_key does
// (scaled[u] = offset / dim[u]), refusing an offset that is not on a chunk
// boundary (H5Dbtree.c:646).
func decodeChunkKeyCoords(raw []byte, address uint64, chunkDims, scaled []uint64) error {
	for j := range scaled {
		byteOffset := binary.LittleEndian.Uint64(raw[j*8:])

		if chunkDims[j] == 0 {
			return fmt.Errorf("chunk dimension %d is zero", j)
		}
		if byteOffset%chunkDims[j] != 0 {
			return fmt.Errorf("%w: B-tree node at 0x%X: bad coordinate offset %d for chunk dimension %d of %d",
				utils.ErrCorrupt, address, byteOffset, j, chunkDims[j])
		}
		scaled[j] = byteOffset / chunkDims[j]
	}

	return nil
}

// FindChunk searches B-tree for chunk at given scaled coordinates.
// coords: scaled chunk indices (not byte offsets).
func (node *BTreeV1Node) FindChunk(r io.ReaderAt, coords []uint64, offsetSize uint8, chunkDims []uint64) (uint64, error) {
	ndims := len(coords)

	// Find which child to follow.
	childIndex := 0
	for i := 0; i < int(node.EntriesUsed); i++ {
		// Compare coordinates with key.
		if compareCoords(coords, node.Keys[i].Scaled) < 0 {
			break
		}
		childIndex = i + 1
	}

	// childIndex reaches EntriesUsed for coordinates past the last key, one
	// beyond the last child: clamp it so such a coordinate cannot index past
	// Children (it used to panic).
	if node.EntriesUsed == 0 {
		return 0, fmt.Errorf("%w: chunk B-tree node at 0x%X has no entries", utils.ErrCorrupt, node.Address)
	}
	if childIndex >= int(node.EntriesUsed) {
		childIndex = int(node.EntriesUsed) - 1
	}

	childAddr := node.Children[childIndex]

	// If leaf node (level 0), child address points to actual chunk.
	if node.NodeLevel == 0 {
		return childAddr, nil
	}

	// Otherwise, recursively search child node.
	childNode, err := ParseBTreeV1Node(r, childAddr, offsetSize, ndims, chunkDims)
	if err != nil {
		return 0, err
	}

	// The child must be one level below (H5Bcache.c:173-176, exp_level from
	// H5B.c:1146): levels then strictly decrease and a node pointing back up
	// cannot recurse forever.
	if childNode.NodeLevel != node.NodeLevel-1 {
		return 0, fmt.Errorf("%w: chunk B-tree node at 0x%X has level %d, its parent 0x%X has level %d",
			utils.ErrCorrupt, childAddr, childNode.NodeLevel, node.Address, node.NodeLevel)
	}

	return childNode.FindChunk(r, coords, offsetSize, chunkDims)
}

// compareCoords compares two coordinate arrays.
// Returns: -1 if a < b, 0 if a == b, 1 if a > b.
func compareCoords(a, b []uint64) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// readAddress reads a little-endian file address of size bytes; an all-ones
// address is HADDR_UNDEF at any width (DecodeAddress).
func readAddress(data []byte, size int) uint64 {
	return DecodeAddress(data, size, binary.LittleEndian)
}

// String returns human-readable B-tree node description.
func (node *BTreeV1Node) String() string {
	return fmt.Sprintf("B-tree v1 node: type=%d level=%d entries=%d",
		node.NodeType, node.NodeLevel, node.EntriesUsed)
}

// ChunkEntry represents a chunk location in the B-tree.
type ChunkEntry struct {
	Key     ChunkKey // Chunk coordinates and metadata.
	Address uint64   // Address of chunk data.
}

// btreeTypeChunk is the node type of a raw-data chunk B-tree (H5B_CHUNK_ID).
const btreeTypeChunk = 1

// CollectAllChunks collects all chunk entries of the B-tree rooted at node,
// with the structural checks of walkChunks and no limit on the entry count
// beyond what the file holds. Dataset readers use
// DatasetDescriptor.VisitChunks, which also bounds the count by the chunk
// grid and checks every key against it.
func (node *BTreeV1Node) CollectAllChunks(r io.ReaderAt, offsetSize uint8, chunkDims []uint64) ([]ChunkEntry, error) {
	var chunks []ChunkEntry
	err := node.walkChunks(r, offsetSize, chunkDims, math.MaxUint64, func(e ChunkEntry) error {
		chunks = append(chunks, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return chunks, nil
}

// walkChunks walks the B-tree rooted at node and passes its leaf entries to
// emit in key order, stopping at the first error.
//
// The node graph comes from the file, so the walk mirrors the checks the C
// library makes while loading nodes (review finding H1):
//   - a child must be one level below its parent: H5B__iterate_helper passes
//     bt->level-1 as the expected level (H5B.c:1146) and the cache rejects a
//     node whose level differs (H5Bcache.c:173-176). Levels strictly decrease,
//     so the walk ends after at most 256 levels;
//   - a node may be visited once: a valid B-tree is a tree, while a child
//     pointing back up, or many entries pointing to one subtree, would loop or
//     multiply the work beyond the size of the file;
//   - the number of leaf entries may not exceed maxEntries, which the caller
//     derives from the chunk grid (each chunk has one entry).
//
// Every violation wraps utils.ErrCorrupt.
func (node *BTreeV1Node) walkChunks(r io.ReaderAt, offsetSize uint8, chunkDims []uint64, maxEntries uint64,
	emit func(ChunkEntry) error,
) error {
	w := &chunkWalk{
		r:          r,
		offsetSize: offsetSize,
		chunkDims:  chunkDims,
		maxEntries: maxEntries,
		visited:    map[uint64]bool{node.Address: true},
		emit:       emit,
	}

	return w.walk(node)
}

// chunkWalk is the state of one B-tree walk.
type chunkWalk struct {
	r          io.ReaderAt
	offsetSize uint8
	chunkDims  []uint64
	maxEntries uint64
	visited    map[uint64]bool
	entries    uint64
	emit       func(ChunkEntry) error
}

func (w *chunkWalk) walk(node *BTreeV1Node) error {
	if node.NodeLevel == 0 {
		if w.entries+uint64(node.EntriesUsed) > w.maxEntries {
			return fmt.Errorf("%w: chunk B-tree has more than %d entries, the most its chunk grid allows",
				utils.ErrCorrupt, w.maxEntries)
		}
		w.entries += uint64(node.EntriesUsed)
		for i := 0; i < int(node.EntriesUsed); i++ {
			if err := w.emit(ChunkEntry{Key: node.Keys[i], Address: node.Children[i]}); err != nil {
				return err
			}
		}
		return nil
	}

	for i := 0; i < int(node.EntriesUsed); i++ {
		childAddr := node.Children[i]
		if w.visited[childAddr] {
			return fmt.Errorf("%w: chunk B-tree node at 0x%X is reached twice (from node 0x%X)",
				utils.ErrCorrupt, childAddr, node.Address)
		}
		w.visited[childAddr] = true

		child, err := ParseBTreeV1Node(w.r, childAddr, w.offsetSize, len(w.chunkDims), w.chunkDims)
		if err != nil {
			return fmt.Errorf("failed to parse child node at 0x%x: %w", childAddr, err)
		}

		if child.NodeLevel != node.NodeLevel-1 {
			return fmt.Errorf("%w: chunk B-tree node at 0x%X has level %d, its parent 0x%X has level %d",
				utils.ErrCorrupt, childAddr, child.NodeLevel, node.Address, node.NodeLevel)
		}

		if err := w.walk(child); err != nil {
			return err
		}
	}

	return nil
}
