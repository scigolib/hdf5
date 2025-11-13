package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// BTreeV1Node represents a B-tree version 1 node.
// Used for indexing chunked dataset storage.
// Reference: H5Bpkg.h, H5Dbtree.c.
type BTreeV1Node struct {
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

	node := &BTreeV1Node{}
	offset := 0

	// Signature (4 bytes) - should be "TREE".
	copy(node.Signature[:], header[offset:offset+4])
	if string(node.Signature[:]) != "TREE" {
		return nil, fmt.Errorf("invalid B-tree signature: %s", string(node.Signature[:]))
	}
	offset += 4

	// Node type (1 byte) - for chunk B-tree, type = 1.
	node.NodeType = header[offset]
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
	dataSize := int(node.EntriesUsed)*entrySize + keySize // +keySize for final key.

	data := make([]byte, dataSize)
	//nolint:gosec // G115: HDF5 addresses fit in int64 for io.ReaderAt interface
	if _, err := r.ReadAt(data, int64(address)+int64(headerSize)); err != nil {
		return nil, fmt.Errorf("failed to read B-tree node data: %w", err)
	}

	// Parse keys and children.
	node.Keys = make([]ChunkKey, node.EntriesUsed+1) // +1 because there's always 1 more key than children.
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
		// Then convert to scaled indices by dividing by chunk dimension.
		for j := 0; j < ndims; j++ {
			byteOffset := binary.LittleEndian.Uint64(data[dataOffset : dataOffset+8])
			dataOffset += 8

			// Convert byte offset to scaled index.
			// From H5D__btree_decode_key: scaled[u] = tmp_offset / layout->dim[u].
			if chunkDims[j] == 0 {
				return nil, fmt.Errorf("chunk dimension %d is zero", j)
			}
			key.Scaled[j] = byteOffset / chunkDims[j]
		}

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

	if childIndex > int(node.EntriesUsed) {
		childIndex = int(node.EntriesUsed)
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

// CollectAllChunks recursively collects all chunks from B-tree.
// This handles both leaf and non-leaf nodes.
func (node *BTreeV1Node) CollectAllChunks(r io.ReaderAt, offsetSize uint8, chunkDims []uint64) ([]ChunkEntry, error) {
	ndims := len(chunkDims)
	var chunks []ChunkEntry

	// If this is a leaf node (level 0), children point to actual chunks.
	if node.NodeLevel == 0 {
		for i := 0; i < int(node.EntriesUsed); i++ {
			chunks = append(chunks, ChunkEntry{
				Key:     node.Keys[i],
				Address: node.Children[i],
			})
		}
		return chunks, nil
	}

	// Non-leaf node: children point to other B-tree nodes.
	// Recursively collect chunks from all child nodes.
	for i := 0; i < int(node.EntriesUsed); i++ {
		childAddr := node.Children[i]

		// Parse child node.
		childNode, err := ParseBTreeV1Node(r, childAddr, offsetSize, ndims, chunkDims)
		if err != nil {
			return nil, fmt.Errorf("failed to parse child node at 0x%x: %w", childAddr, err)
		}

		// Recursively collect chunks from child.
		childChunks, err := childNode.CollectAllChunks(r, offsetSize, chunkDims)
		if err != nil {
			return nil, fmt.Errorf("failed to collect chunks from child at 0x%x: %w", childAddr, err)
		}

		chunks = append(chunks, childChunks...)
	}

	return chunks, nil
}
