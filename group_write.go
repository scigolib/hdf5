package hdf5

import (
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/writer"
)

// validateGroupPath validates group path is not empty, starts with '/', and is not root.
func validateGroupPath(path string) error {
	if path == "" {
		return fmt.Errorf("group path cannot be empty")
	}
	if path[0] != '/' {
		return fmt.Errorf("group path must start with '/' (got %q)", path)
	}
	if path == "/" {
		return fmt.Errorf("root group already exists")
	}
	return nil
}

// createGroupStructures creates and writes the local heap, symbol table node, and B-tree for a group.
// Returns (heapAddr, btreeAddr, error).
func (fw *FileWriter) createGroupStructures() (uint64, uint64, error) {
	offsetSize := int(fw.file.sb.OffsetSize)

	// Create local heap
	heap := structures.NewLocalHeap(256)
	heapAddr, err := fw.writer.Allocate(heap.Size())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to allocate heap: %w", err)
	}

	// Create symbol table node
	stNode := structures.NewSymbolTableNode(32)
	entrySize := 2*offsetSize + 4 + 4 + 16
	stNodeSize := uint64(8 + 32*entrySize) //nolint:gosec // Safe: small constant calculation
	stNodeAddr, err := fw.writer.Allocate(stNodeSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to allocate symbol table node: %w", err)
	}

	if err := stNode.WriteAt(fw.writer, stNodeAddr, uint8(offsetSize), 32, fw.file.sb.Endianness); err != nil { //nolint:gosec // Safe: offsetSize is 8
		return 0, 0, fmt.Errorf("failed to write symbol table node: %w", err)
	}

	// Create B-tree
	btree := structures.NewBTreeNodeV1(0, 16)
	if err := btree.AddKey(0, stNodeAddr); err != nil {
		return 0, 0, fmt.Errorf("failed to add B-tree key: %w", err)
	}

	btreeSize := uint64(24 + (2*16+1)*offsetSize + 2*16*offsetSize) //nolint:gosec // Safe: small constant calculation
	btreeAddr, err := fw.writer.Allocate(btreeSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to allocate B-tree: %w", err)
	}

	if err := btree.WriteAt(fw.writer, btreeAddr, uint8(offsetSize), 16, fw.file.sb.Endianness); err != nil { //nolint:gosec // Safe: offsetSize is 8
		return 0, 0, fmt.Errorf("failed to write B-tree: %w", err)
	}

	// Write heap
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return 0, 0, fmt.Errorf("failed to write local heap: %w", err)
	}

	return heapAddr, btreeAddr, nil
}

// CreateGroup creates a new empty group in the HDF5 file.
// Groups organize datasets and other groups in a hierarchical structure.
//
// This method creates an empty group using symbol table format (old HDF5 format).
// For groups with many links, consider using CreateDenseGroup() or CreateGroupWithLinks().
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
	if err := validateGroupPath(path); err != nil {
		return err
	}

	// Parse path into parent and name
	parent, name := parsePath(path)

	// For MVP: only support root-level groups (parent must be "/")
	if parent != "" && parent != "/" {
		return fmt.Errorf("nested groups not yet supported in MVP, parent must be root (got parent %q)", parent)
	}

	// Create group structures (heap, symbol table, B-tree)
	heapAddr, btreeAddr, err := fw.createGroupStructures()
	if err != nil {
		return err
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
// Links the child by adding an entry to the parent's symbol table.
//
// Parameters:
//   - parentPath: Path to parent group ("" or "/" for root)
//   - childName: Name of the child object
//   - childAddr: Address of the child's object header
//
// Returns:
//   - error: If linking fails
func (fw *FileWriter) linkToParent(parentPath, childName string, childAddr uint64) error {
	// For MVP, only support root group as parent
	if parentPath != "" && parentPath != "/" {
		return fmt.Errorf("linking to non-root groups not supported in MVP")
	}

	// Use root group metadata
	heapAddr := fw.rootHeapAddr
	stNodeAddr := fw.rootStNodeAddr

	// Step 1: Read existing local heap
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return fmt.Errorf("read local heap: %w", err)
	}

	// Step 2: Add child name to heap
	nameOffset, err := heap.AddString(childName)
	if err != nil {
		return fmt.Errorf("add string to heap: %w", err)
	}

	// Step 3: Read existing symbol table node
	stNode, err := fw.readSymbolTableNode(stNodeAddr)
	if err != nil {
		return fmt.Errorf("read symbol table node: %w", err)
	}

	// Step 4: Add entry to symbol table
	entry := structures.SymbolTableEntry{
		LinkNameOffset: nameOffset,
		ObjectAddress:  childAddr,
		CacheType:      0, // No cache (MVP)
		Reserved:       0,
	}
	if err := stNode.AddEntry(entry); err != nil {
		return fmt.Errorf("add entry to symbol table: %w", err)
	}

	// Step 5: Write updated heap
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return fmt.Errorf("write heap: %w", err)
	}

	// Step 6: Write updated symbol table node
	offsetSize := fw.file.sb.OffsetSize
	if err := stNode.WriteAt(fw.writer, stNodeAddr, offsetSize, 32, fw.file.sb.Endianness); err != nil {
		return fmt.Errorf("write symbol table: %w", err)
	}

	return nil
}

// readLocalHeap reads a local heap from the file at the specified address.
// This is used to modify the heap by adding new strings for linking.
//
// Parameters:
//   - addr: Address of the local heap in the file
//
// Returns:
//   - *structures.LocalHeap: The heap structure (writable)
//   - error: If read fails
func (fw *FileWriter) readLocalHeap(addr uint64) (*structures.LocalHeap, error) {
	// Load existing heap from disk
	heap, err := structures.LoadLocalHeap(fw.writer, addr, fw.file.sb)
	if err != nil {
		return nil, fmt.Errorf("load local heap: %w", err)
	}

	// Convert to writable mode (copies Data to internal strings buffer)
	if err := heap.PrepareForModification(); err != nil {
		return nil, fmt.Errorf("prepare heap for modification: %w", err)
	}

	// Set write-mode fields
	// Note: DataSegmentAddress is set by WriteTo(), not here
	heap.OffsetToHeadFreeList = 1 // MVP: no free list (1 = H5HL_FREE_NULL)

	return heap, nil
}

// readSymbolTableNode reads a symbol table node from the file at the specified address.
// This is used to modify the node by adding new entries for linking.
//
// Parameters:
//   - addr: Address of the symbol table node in the file
//
// Returns:
//   - *structures.SymbolTableNode: The node structure (writable)
//   - error: If read fails
func (fw *FileWriter) readSymbolTableNode(addr uint64) (*structures.SymbolTableNode, error) {
	// Use the existing ParseSymbolTableNode function from structures package
	return structures.ParseSymbolTableNode(fw.writer, addr, fw.file.sb)
}

// CreateDenseGroup creates new dense group (HDF5 1.8+ format).
//
// Dense groups are more efficient for large numbers of links (>8).
// They use fractal heap + B-tree v2 instead of symbol table.
//
// Parameters:
//   - name: Group name (must start with "/")
//   - links: Map of link_name → target_path
//
// Returns:
//   - error: Non-nil if creation fails
//
// Example:
//
//	err := fw.CreateDenseGroup("/large_group", map[string]string{
//	    "dataset1": "/data/dataset1",
//	    "dataset2": "/data/dataset2",
//	    // ... many links
//	})
//
// Reference: H5Gcreate.c - H5Gcreate2().
func (fw *FileWriter) CreateDenseGroup(name string, links map[string]string) error {
	// Validate name
	if !strings.HasPrefix(name, "/") {
		return fmt.Errorf("group name must start with /: %s", name)
	}

	// Create DenseGroupWriter
	dgw := writer.NewDenseGroupWriter(name)

	// Add all links
	for linkName, targetPath := range links {
		// Resolve target path to object header address
		targetAddr, err := fw.resolveObjectAddress(targetPath)
		if err != nil {
			return fmt.Errorf("failed to resolve target %s: %w", targetPath, err)
		}

		err = dgw.AddLink(linkName, targetAddr)
		if err != nil {
			return fmt.Errorf("failed to add link %s: %w", linkName, err)
		}
	}

	// Write dense group
	ohAddr, err := dgw.WriteToFile(fw.writer, fw.writer.Allocator(), fw.file.sb)
	if err != nil {
		return fmt.Errorf("failed to write dense group: %w", err)
	}

	// Link to parent (root group for MVP)
	parent, childName := parsePath(name)
	if parent != "" && parent != "/" {
		return fmt.Errorf("nested groups not yet supported in MVP, parent must be root (got parent %q)", parent)
	}

	if err := fw.linkToParent(parent, childName, ohAddr); err != nil {
		return fmt.Errorf("failed to link to parent: %w", err)
	}

	return nil
}

// resolveObjectAddress resolves object path to file address.
//
// This is a helper for link creation - looks up the target object's
// address in the file by its path.
//
// For MVP: Only supports root-level objects (direct lookup in root group).
// Future: Full path resolution with nested groups.
//
// Parameters:
//   - path: Object path (e.g., "/data/dataset1" or "/dataset1")
//
// Returns:
//   - uint64: File address of object header
//   - error: Non-nil if object not found
func (fw *FileWriter) resolveObjectAddress(path string) (uint64, error) {
	// For MVP: only support root-level paths
	if path == "/" {
		return fw.rootGroupAddr, nil
	}

	if !strings.HasPrefix(path, "/") {
		return 0, fmt.Errorf("path must start with /: %s", path)
	}

	// Parse path
	parent, name := parsePath(path)

	// For MVP: only root-level objects supported
	if parent != "" && parent != "/" {
		return 0, fmt.Errorf("nested paths not yet supported in MVP (got %q)", path)
	}

	// Read root group's symbol table to find the object
	stNode, err := fw.readSymbolTableNode(fw.rootStNodeAddr)
	if err != nil {
		return 0, fmt.Errorf("failed to read symbol table: %w", err)
	}

	heap, err := fw.readLocalHeap(fw.rootHeapAddr)
	if err != nil {
		return 0, fmt.Errorf("failed to read local heap: %w", err)
	}

	// Search for object in symbol table
	for _, entry := range stNode.Entries {
		// Get link name from heap
		linkName, err := heap.GetString(entry.LinkNameOffset)
		if err != nil {
			continue
		}

		if linkName == name {
			return entry.ObjectAddress, nil
		}
	}

	return 0, fmt.Errorf("object not found: %s", path)
}

// Dense group threshold (HDF5 default: switch to dense when >8 links).
const denseGroupThreshold = 8

// CreateGroupWithLinks creates group with automatic format selection.
//
// This method automatically chooses the most efficient storage format:
//   - Symbol table (old format) for ≤8 links (compact)
//   - Dense format (new format) for >8 links (scalable)
//
// This matches HDF5 1.8+ behavior: start compact, use dense when needed.
//
// Parameters:
//   - name: Group name (must start with "/")
//   - links: Map of link_name → target_path (can be empty)
//
// Returns:
//   - error: Non-nil if creation fails
//
// Example:
//
//	// Small group (will use symbol table)
//	fw.CreateGroupWithLinks("/small", map[string]string{
//	    "data1": "/dataset1",
//	    "data2": "/dataset2",
//	})
//
//	// Large group (will use dense format)
//	largeLinks := make(map[string]string)
//	for i := 0; i < 100; i++ {
//	    largeLinks[fmt.Sprintf("link%d", i)] = fmt.Sprintf("/dataset%d", i)
//	}
//	fw.CreateGroupWithLinks("/large", largeLinks)
//
// Reference: H5Gint.c - H5G_convert_to_dense().
func (fw *FileWriter) CreateGroupWithLinks(name string, links map[string]string) error {
	if len(links) > denseGroupThreshold {
		// Use dense format for large groups
		return fw.CreateDenseGroup(name, links)
	}

	// Use symbol table format for small groups
	// Create empty group first
	if err := fw.CreateGroup(name); err != nil {
		return err
	}

	// For MVP: linking is handled by CreateDenseGroup for dense groups
	// For symbol table groups, links would need to be added via linkToParent
	// This is a limitation of the MVP - symbol table groups can be created empty,
	// but adding links after creation requires manual linkToParent calls

	// Future: implement addLinkToGroup() to add links to existing symbol table groups

	if len(links) > 0 {
		return fmt.Errorf("adding links to symbol table groups not yet supported in MVP (group %s has %d links)", name, len(links))
	}

	return nil
}
