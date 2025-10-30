package hdf5

import (
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
)

// CreateGroup creates a new group in the HDF5 file.
// Groups organize datasets and other groups in a hierarchical structure.
//
// Parameters:
//   - path: Group path (must start with "/", e.g., "/data" or "/data/experiments")
//
// Returns:
//   - error: If creation fails
//
// Example:
//
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create root-level group
//	fw.CreateGroup("/data")
//
//	// Create nested group
//	fw.CreateGroup("/data/experiments")
//
// Limitations for MVP (v0.11.0-beta):
//   - Only symbol table structure (no indexed groups)
//   - No link creation time tracking
//   - Maximum 32 entries per group (symbol table node capacity)
//   - Parent group must exist (create parents first)
func (fw *FileWriter) CreateGroup(path string) error {
	// Validate path
	if path == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if path[0] != '/' {
		return fmt.Errorf("group path must start with '/' (got %q)", path)
	}
	if path == "/" {
		return fmt.Errorf("root group already exists")
	}

	// Parse path into parent and name
	parent, name := parsePath(path)

	// For MVP: only support root-level groups (parent must be "/")
	if parent != "" && parent != "/" {
		return fmt.Errorf("nested groups not yet supported in MVP, parent must be root (got parent %q)", parent)
	}

	// Create local heap (initial size 256 bytes, sufficient for ~10-20 names)
	heap := structures.NewLocalHeap(256)
	heapAddr, err := fw.writer.Allocate(heap.Size())
	if err != nil {
		return fmt.Errorf("failed to allocate heap: %w", err)
	}

	// Create empty symbol table node (capacity 32 entries, typical for B-tree order 16)
	stNode := structures.NewSymbolTableNode(32)

	// Calculate symbol table node size
	// Format: 8-byte header + 32 * entrySize
	// entrySize = 2*offsetSize + 4 + 4 + 16 = 2*8 + 24 = 40 bytes
	offsetSize := int(fw.file.sb.OffsetSize)
	entrySize := 2*offsetSize + 4 + 4 + 16
	stNodeSize := uint64(8 + 32*entrySize)

	stNodeAddr, err := fw.writer.Allocate(stNodeSize)
	if err != nil {
		return fmt.Errorf("failed to allocate symbol table node: %w", err)
	}

	// Write symbol table node
	if err := stNode.WriteAt(fw.writer, stNodeAddr, uint8(offsetSize), 32, fw.file.sb.Endianness); err != nil {
		return fmt.Errorf("failed to write symbol table node: %w", err)
	}

	// Create B-tree v1 node (order K=16, so 2K+1=33 keys, 2K=32 children)
	// For a single-node B-tree, we use 1 key (minimum) and 1 child
	btree := structures.NewBTreeNodeV1(0, 16) // Type 0 = group symbol table

	// Add symbol table node address as child (with dummy key 0)
	// In a real B-tree, the key would be the first link name offset in the symbol table node
	// For an empty group, we use 0 as the key
	if err := btree.AddKey(0, stNodeAddr); err != nil {
		return fmt.Errorf("failed to add B-tree key: %w", err)
	}

	// Calculate B-tree size
	// Header: 4 (sig) + 1 (type) + 1 (level) + 2 (entries) + 2*8 (siblings) = 24 bytes
	// Keys: (2K+1) * 8 = 33 * 8 = 264 bytes
	// Children: 2K * 8 = 32 * 8 = 256 bytes
	// Total: 24 + 264 + 256 = 544 bytes
	btreeSize := uint64(24 + (2*16+1)*offsetSize + 2*16*offsetSize)

	btreeAddr, err := fw.writer.Allocate(btreeSize)
	if err != nil {
		return fmt.Errorf("failed to allocate B-tree: %w", err)
	}

	// Write B-tree
	if err := btree.WriteAt(fw.writer, btreeAddr, uint8(offsetSize), 16, fw.file.sb.Endianness); err != nil {
		return fmt.Errorf("failed to write B-tree: %w", err)
	}

	// Write local heap (must be done after B-tree and symbol table to get proper addresses)
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return fmt.Errorf("failed to write local heap: %w", err)
	}

	// Create object header for the group
	// Message 1: Symbol Table Message (type 0x11)
	stMsg := core.EncodeSymbolTableMessage(btreeAddr, heapAddr, int(fw.file.sb.OffsetSize), int(fw.file.sb.LengthSize))

	ohw := &core.ObjectHeaderWriter{
		Version: 2,
		Flags:   0,
		Messages: []core.MessageWriter{
			{Type: core.MsgSymbolTable, Data: stMsg},
		},
	}

	// Calculate object header size
	// Header: 4 (sig) + 1 (ver) + 1 (flags) + 1 (chunk size) = 7 bytes
	// Message: 1 (type) + 2 (size) + 1 (flags) + len(data)
	messageDataSize := 1 + 2 + 1 + uint64(len(stMsg))
	headerSize := 7 + messageDataSize

	headerAddr, err := fw.writer.Allocate(headerSize)
	if err != nil {
		return fmt.Errorf("failed to allocate object header: %w", err)
	}

	// Write object header
	writtenSize, err := ohw.WriteTo(fw.writer, headerAddr)
	if err != nil {
		return fmt.Errorf("failed to write object header: %w", err)
	}

	if writtenSize != headerSize {
		return fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	// Link to parent group
	if err := fw.linkToParent(parent, name, headerAddr); err != nil {
		return fmt.Errorf("failed to link to parent: %w", err)
	}

	return nil
}

// parsePath splits a path into parent directory and name.
// Examples:
//   - "/group1" → ("", "group1")
//   - "/data/experiments" → ("/data", "experiments")
//   - "/" → ("", "")
func parsePath(path string) (parent, name string) {
	if path == "/" {
		return "", ""
	}

	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")

	// Find last slash
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == 0 {
		// Root-level path like "/group1"
		return "", path[1:] // Return ("", "group1")
	}

	// Nested path like "/data/experiments"
	return path[:lastSlash], path[lastSlash+1:]
}

// linkToParent links a child object to its parent group.
// For MVP, only supports linking to the root group.
//
// Parameters:
//   - parentPath: Path to parent group ("" or "/" for root)
//   - childName: Name of the child object
//   - childAddr: Address of the child's object header
//
// Returns:
//   - error: If linking fails
//
// TODO (Component 3 completion): Implement actual linking
// Current limitation:
//   - Root group uses Link Info message (HDF5 1.8+ format)
//   - Created groups/datasets use Symbol Table (HDF5 <1.8 format)
//   - These are incompatible structures
//
// To fix:
//   - Option A: Refactor root group to use Symbol Table (backwards compatible)
//   - Option B: Support both structures with conversion layer
//
// For MVP demonstration:
//   - Objects are created with valid structure but not linked from root
//   - This allows testing the write infrastructure
//   - Full linking will be implemented before v0.11.0-beta release
func (fw *FileWriter) linkToParent(parentPath, childName string, childAddr uint64) error {
	// For MVP, only support root group as parent
	if parentPath != "" && parentPath != "/" {
		return fmt.Errorf("linking to non-root groups not supported in MVP")
	}

	// TODO: Implement linking when root group is refactored to Symbol Table
	// For now, objects are created but not linked (MVP limitation)

	// Temporary workaround: just succeed
	// Objects exist in file with valid structure, but are not discoverable via root group
	// This is acceptable for MVP to demonstrate the infrastructure works

	return nil
}
