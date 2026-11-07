package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/writer"
)

// GroupWriter represents an HDF5 group opened for writing.
// Groups organize datasets and other groups in a hierarchical structure.
//
// This type enables writing attributes to groups, similar to datasets.
// It provides a clean, object-oriented API consistent with DatasetWriter.
//
// Example:
//
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create group
//	group, _ := fw.CreateGroup("/mygroup")
//
//	// Write attributes to group
//	group.WriteAttribute("description", "My data group")
//	group.WriteAttribute("version", int32(1))
//
// Note: This is a write-only handle. For reading group contents, use
// the file-level Walk() or Group() methods after reopening the file.
type GroupWriter struct {
	// path is the full path of this group (e.g., "/mygroup" or "/data/experiments")
	path string

	// headerAddr is the address of the group's object header in the HDF5 file.
	// This is used for writing attributes and linking to this group.
	headerAddr uint64

	// file is a reference to the parent FileWriter.
	// This is needed for attribute operations and accessing file-level structures.
	file *FileWriter
}

// WriteAttribute writes an attribute to this group.
//
// Storage strategy (automatic):
//   - 0-7 attributes: Compact storage (object header messages)
//   - 8+ attributes: Dense storage (Fractal Heap + B-tree v2)
//
// Supported value types:
//   - Scalars: int8, int16, int32, int64, uint8, uint16, uint32, uint64, float32, float64
//   - Arrays: []int32, []float64, etc. (1D arrays only)
//   - Strings: string (fixed-length, converted to byte array)
//   - String arrays: []string (variable-length strings via Global Heap)
//
// Parameters:
//   - name: Attribute name (ASCII, no null bytes)
//   - value: Attribute value (Go scalar, slice, or string)
//
// Returns:
//   - error: If attribute cannot be written
//
// Example:
//
//	group, _ := fw.CreateGroup("/mygroup")
//	group.WriteAttribute("MATLAB_class", "double")
//	group.WriteAttribute("MATLAB_complex", uint8(1))
//	group.WriteAttribute("description", "Temperature measurements")
//	group.WriteAttribute("topics", []string{"camera", "lidar", "imu"})
//
// Writing a name the group already has replaces that attribute's value;
// DeleteAttribute removes one.
//
// Limitations:
//   - No compound types
func (g *GroupWriter) WriteAttribute(name string, value interface{}) error {
	// Delegate to existing attribute writing infrastructure
	// This reuses the same code path as DatasetWriter.WriteAttribute
	return writeAttribute(g.file, g.headerAddr, name, value)
}

// DeleteAttribute removes an attribute by name from this group.
//
// This method supports both compact and dense attribute storage:
//   - Compact storage (0-7 attributes): Removes message from object header
//   - Dense storage (8+ attributes): Removes from B-tree and fractal heap
//
// Parameters:
//   - name: Attribute name to delete
//
// Returns:
//   - error: If attribute not found or deletion fails
//
// Example:
//
//	group, _ := fw.CreateGroup("/mygroup")
//	group.WriteAttribute("temp", int32(42))
//	group.DeleteAttribute("temp") // Remove attribute
//
// Reference: H5Adelete.c - H5A__delete().
func (g *GroupWriter) DeleteAttribute(name string) error {
	return deleteAttribute(g.file, g.headerAddr, name)
}

// Path returns the full path of this group.
//
// This can be used to display the group's location in the file hierarchy
// or for debugging purposes.
//
// Returns:
//   - string: The group's path (e.g., "/mygroup" or "/data/experiments")
//
// Example:
//
//	group, _ := fw.CreateGroup("/mygroup")
//	fmt.Println(group.Path()) // Output: /mygroup
func (g *GroupWriter) Path() string {
	return g.path
}

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
// Returns (heapAddr, stNodeAddr, btreeAddr, error).
func (fw *FileWriter) createGroupStructures() (uint64, uint64, uint64, error) {
	// Create local heap (4096 bytes supports ~300+ typical names).
	heap := structures.NewLocalHeap(4096)
	heapAddr, err := fw.writer.Allocate(heap.Size())
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to allocate heap: %w", err)
	}

	// An empty B-tree, as H5G__stab_create does through H5B_create
	// (H5Gstab.c:132): the first insert creates the first symbol table node
	// (H5B.c:853), so the group has none yet.
	stNodeAddr := undefinedAddress
	btreeAddr, err := fw.createEmptyGroupBTree()
	if err != nil {
		return 0, 0, 0, err
	}

	// Write heap
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to write local heap: %w", err)
	}

	return heapAddr, stNodeAddr, btreeAddr, nil
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
//   - *GroupWriter: Handle for writing attributes to the group
//   - error: If creation fails
//
// Example:
//
//	fw, _ := hdf5.CreateForWrite("data.h5", hdf5.CreateTruncate)
//	defer fw.Close()
//
//	// Create root-level group
//	group, _ := fw.CreateGroup("/data")
//	group.WriteAttribute("description", "My data group")
//
//	// Create nested group
//	nested, _ := fw.CreateGroup("/data/experiments")
//	nested.WriteAttribute("MATLAB_class", "double")
//
// Limitations for MVP (v0.11.0-beta):
//   - Only symbol table structure (no indexed groups)
//   - No link creation time tracking
//   - Parent group must exist (create parents first)
func (fw *FileWriter) CreateGroup(path string) (*GroupWriter, error) {
	// Validate path
	if err := validateGroupPath(path); err != nil {
		return nil, err
	}

	// Parse path into parent and name
	parent, name := parsePath(path)

	// The parent must exist and must not hold the name yet; both are
	// checked before anything is allocated (link_create.go).
	if err := fw.checkLinkNameFree(parent, name); err != nil {
		return nil, err
	}

	// Create group structures (heap, symbol table, B-tree)
	heapAddr, _, btreeAddr, err := fw.createGroupStructures()
	if err != nil {
		return nil, err
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

	// Pre-allocate OHDR with padding to accommodate future attributes.
	// This prevents corruption when attributes are added later.
	ohw.PadToSize(core.MinOHDRAllocSize)
	headerSize := ohw.Size()

	headerAddr, err := fw.writer.Allocate(headerSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate object header: %w", err)
	}

	// Write object header
	writtenSize, err := ohw.WriteTo(fw.writer, headerAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to write object header: %w", err)
	}

	if writtenSize != headerSize {
		return nil, fmt.Errorf("header size mismatch: expected %d, wrote %d", headerSize, writtenSize)
	}

	// Link to parent group; a group that could not be linked is deleted
	// again and never registered.
	if err := fw.linkToParent(parent, name, headerAddr); err != nil {
		return nil, fw.discardUnlinkedObject(headerAddr, fmt.Errorf("failed to link to parent: %w", err))
	}

	// Store group metadata for nested dataset linking
	fw.registerGroup(path, heapAddr, btreeAddr, headerAddr, headerSize)

	// Return GroupWriter handle
	return &GroupWriter{
		path:       path,
		headerAddr: headerAddr,
		file:       fw,
	}, nil
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
// When the SNOD is full (8 entries for K=4), it splits per the C library algorithm
// (H5Gnode.c:598-637). When the local heap is full, it reallocates a larger one.
//
// Parameters:
//   - parentPath: Path to parent group ("" or "/" for root)
//   - childName: Name of the child object
//   - childAddr: Address of the child's object header
//
// Returns:
//   - error: If linking fails
func (fw *FileWriter) linkToParent(parentPath, childName string, childAddr uint64) error {
	return fw.linkEntryToParent(parentPath, childName, childAddr, "")
}

// linkEntryToParent adds a symbol table entry for childName to the parent
// group. With a non-empty softTarget, the entry is a soft link: the target
// path is stored in the parent's local heap and the entry carries cache
// type 2 with an undefined object address (H5G_CACHED_SLINK) — soft links
// are not objects and have no object header.
func (fw *FileWriter) linkEntryToParent(parentPath, childName string, childAddr uint64, softTarget string) error {
	// A group of the 1.8 format keeps its links as link messages in its
	// header or in dense link storage (H5G_obj_insert, H5Gobj.c:441-560).
	if addr, ok := fw.linkMessageGroup(parentPath); ok {
		return fw.insertGroupLink(addr, childName, childAddr, softTarget)
	}

	// Get parent group metadata.
	var heapAddr, btreeAddr uint64
	if parentPath == "" || parentPath == "/" {
		heapAddr = fw.rootHeapAddr
		btreeAddr = fw.rootBTreeAddr
	} else {
		meta, exists := fw.groups[parentPath]
		if !exists {
			return fmt.Errorf("parent group %q not found (create it first)", parentPath)
		}
		heapAddr = meta.heapAddr
		btreeAddr = meta.btreeAddr
	}

	// Step 1: Refuse a name the group holds before the heap changes: adding
	// the name first may move a full heap (expandHeapAndAdd), and a refused
	// insert would then leave the group on the freed heap. C finds the
	// duplicate before H5G__link_to_ent touches the heap (H5Gnode.c:583-594).
	if err := fw.checkSymbolTableNameFree(heapAddr, btreeAddr, parentPath, childName); err != nil {
		return err
	}

	// Step 2: Read existing local heap.
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return fmt.Errorf("read local heap: %w", err)
	}

	// Step 3: Add child name to heap. If full, expand.
	nameOffset, err := heap.AddString(childName)
	if err != nil {
		heap, heapAddr, nameOffset, err = fw.expandHeapAndAdd(heap, heapAddr, parentPath, childName)
		if err != nil {
			return err
		}
	}

	// Step 3: The new entry.
	newEntry := structures.SymbolTableEntry{
		LinkNameOffset: nameOffset,
		ObjectAddress:  childAddr,
		CacheType:      0,
		Reserved:       0,
	}
	if softTarget != "" {
		targetOffset, addErr := heap.AddString(softTarget)
		if addErr != nil {
			heap, heapAddr, targetOffset, addErr = fw.expandHeapAndAdd(heap, heapAddr, parentPath, softTarget)
			if addErr != nil {
				return fmt.Errorf("add soft-link target to heap: %w", addErr)
			}
		}
		newEntry.ObjectAddress = 0xFFFFFFFFFFFFFFFF // HADDR_UNDEF: soft links are not objects
		newEntry.CacheType = structures.CacheTypeSoftLink
		newEntry.CachedSoftLinkOffset = uint32(targetOffset) //nolint:gosec // G115: local heap offsets fit in uint32
	}

	// Step 4: Insert it into the group's B-tree, splitting symbol table
	// nodes and B-tree nodes as the C library does (H5G__stab_insert_real,
	// H5Gstab.c:270-300, through H5B_insert).
	if err := fw.groupSymbolTable(heap).insert(btreeAddr, childName, newEntry); err != nil {
		return fmt.Errorf("insert %q into group B-tree: %w", childName, err)
	}

	// Step 6: Write updated heap.
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return fmt.Errorf("write heap: %w", err)
	}

	return nil
}

// readAddrFromBuf reads a file address of size bytes from a byte buffer; an
// all-ones address is HADDR_UNDEF at any width (core.DecodeAddress,
// H5Fint.c:3046-3050).
func readAddrFromBuf(data []byte, size int, endianness binary.ByteOrder) uint64 {
	return core.DecodeAddress(data, size, endianness)
}

// expandHeapAndAdd grows the data segment of the local heap whose prefix is
// at heapAddr and stores childName in it, as H5HL_insert does when no free
// block fits (H5HL.c:592-700). The segment is extended in place when the
// space after it is free (H5MF_try_extend, H5HL.c:617-640); otherwise the
// old segment is freed and a new one allocated (H5HL__dblk_realloc,
// H5HLdblk.c:163-257, free at :178-181, allocate at :184-187). Only the data
// segment moves: the prefix stays at heapAddr (H5HLdblk.c:190 changes only
// dblk_addr), so the group's symbol table message and a superblock-v0 root
// entry's cached heap address stay valid and are never rewritten.
//
// It returns the heap, the unchanged prefix address and the name's offset;
// the caller writes the heap (prefix and segment) with WriteTo.
func (fw *FileWriter) expandHeapAndAdd(heap *structures.LocalHeap, heapAddr uint64, _, childName string) (*structures.LocalHeap, uint64, uint64, error) {
	oldAddr := heap.DataSegmentAddress
	nameOffset, oldSize := heap.GrowAndAddString(childName)
	newSize := heap.DataSegmentSize

	if fw.writer.TryExtend(oldAddr, oldSize, newSize-oldSize) {
		return heap, heapAddr, nameOffset, nil
	}

	if err := fw.writer.Free(oldAddr, oldSize); err != nil {
		return nil, 0, 0, fmt.Errorf("free old local heap data segment: %w", err)
	}
	newAddr, err := fw.writer.Allocate(newSize)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("allocate local heap data segment: %w", err)
	}
	heap.SetDataSegmentAddress(newAddr)
	return heap, heapAddr, nameOffset, nil
}

// readLocalHeap reads the local heap whose prefix is at addr, with its data
// segment and free list, ready for AddString and WriteTo.
func (fw *FileWriter) readLocalHeap(addr uint64) (*structures.LocalHeap, error) {
	heap, err := structures.LoadLocalHeap(fw.writer, addr, fw.file.sb)
	if err != nil {
		return nil, fmt.Errorf("load local heap: %w", err)
	}
	return heap, nil
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

	// The parent must exist and must not hold the name yet, checked before
	// anything is allocated (link_create.go).
	parent, childName := parsePath(name)
	if err := fw.checkLinkNameFree(parent, childName); err != nil {
		return err
	}

	// Create DenseGroupWriter
	dgw := writer.NewDenseGroupWriter(name)

	// Add all links
	targets := make([]uint64, 0, len(links))
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
		targets = append(targets, targetAddr)
	}

	// Each hard link counts one reference of its target, as H5L__link_cb
	// does through H5O_link (H5L.c, H5Oint.c); otherwise deleting the
	// target's other link would free an object the new group still names.
	if err := fw.adjustRefCounts(targets, 1); err != nil {
		return err
	}
	undo := func(cause error) error {
		if rbErr := fw.adjustRefCounts(targets, -1); rbErr != nil {
			return errors.Join(cause, fmt.Errorf("restore reference counts: %w", rbErr))
		}
		return cause
	}

	// Write dense group
	ohAddr, err := dgw.WriteToFile(fw.writer, fw.writer.Allocator(), fw.file.sb)
	if err != nil {
		return undo(fmt.Errorf("failed to write dense group: %w", err))
	}

	if err := fw.linkToParent(parent, childName, ohAddr); err != nil {
		return undo(fw.discardUnlinkedObject(ohAddr, fmt.Errorf("failed to link to parent: %w", err)))
	}

	return nil
}

// adjustRefCounts adds delta (+1 or -1) to the reference count of the object
// at each address of addrs, once per occurrence, as CreateHardLink does for
// one link. On a failure the counts already changed are restored.
func (fw *FileWriter) adjustRefCounts(addrs []uint64, delta int) error {
	apply := func(addr uint64, d int) error {
		oh, err := core.ReadObjectHeader(fw.writer, addr, fw.file.sb)
		if err != nil {
			return fmt.Errorf("read object header at 0x%X: %w", addr, err)
		}
		if d > 0 {
			oh.IncrementReferenceCount()
		} else {
			oh.DecrementReferenceCount()
		}
		if err := writeObjectHeaderWithRefCount(fw, addr, oh); err != nil {
			return fmt.Errorf("update the reference count at 0x%X: %w", addr, err)
		}
		return nil
	}
	for i, addr := range addrs {
		if err := apply(addr, delta); err != nil {
			for _, done := range addrs[:i] {
				if rbErr := apply(done, -delta); rbErr != nil {
					return errors.Join(err, rbErr)
				}
			}
			return err
		}
	}
	return nil
}

// resolveObjectAddress resolves object path to file address.
// Searches all SNODs in the parent group's B-tree to find the named object.
//
// Parameters:
//   - path: Object path (e.g., "/data/dataset1" or "/dataset1")
//
// Returns:
//   - uint64: File address of object header
//   - error: Non-nil if object not found or parent doesn't exist
func (fw *FileWriter) resolveObjectAddress(path string) (uint64, error) {
	if path == "/" {
		return fw.rootGroupAddr, nil
	}
	if !strings.HasPrefix(path, "/") {
		return 0, fmt.Errorf("path must start with /: %s", path)
	}

	parent, name := parsePath(path)

	// A group of the 1.8 format (H5G__obj_lookup, H5Gobj.c:1056-1100).
	if addr, ok := fw.linkMessageGroup(parent); ok {
		target, err := fw.lookupGroupLink(addr, name)
		if errors.Is(err, errGroupEntryNotFound) {
			return 0, fmt.Errorf("object not found: %s: %w", path, err)
		}
		if err != nil {
			return 0, fmt.Errorf("look up %s: %w", path, err)
		}
		return target, nil
	}

	// Get parent B-tree and heap addresses.
	var btreeAddr, heapAddr uint64
	if parent == "" || parent == "/" {
		btreeAddr = fw.rootBTreeAddr
		heapAddr = fw.rootHeapAddr
	} else {
		meta, exists := fw.groups[parent]
		if !exists {
			return 0, fmt.Errorf("parent group %q not found", parent)
		}
		btreeAddr = meta.btreeAddr
		heapAddr = meta.heapAddr
	}

	// Read heap.
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return 0, fmt.Errorf("read local heap: %w", err)
	}

	// Look the name up as H5G__stab_lookup does through H5B_find
	// (H5Gstab.c:740-770).
	entry, err := fw.groupSymbolTable(heap).lookup(btreeAddr, name)
	if errors.Is(err, errGroupEntryNotFound) {
		return 0, fmt.Errorf("object not found: %s: %w", path, err)
	}
	if err != nil {
		return 0, fmt.Errorf("look up %s: %w", path, err)
	}
	return entry.ObjectAddress, nil
}

// unlinkFromParent removes a named child entry from its parent group's symbol table.
// This is the reverse of linkToParent. It reads the parent's B-tree + SNODs, finds
// the named entry, removes it, and rewrites the B-tree + SNODs.
//
// Parameters:
//   - parentPath: Path to parent group ("" or "/" for root)
//   - childName: Name of the child object to unlink
//
// Returns:
//   - uint64: The ObjectAddress of the removed entry (for cascade delete)
//   - error: If unlinking fails or child not found
//
// Reference: H5Gobj.c - H5G_obj_remove(), H5Gnode.c - H5G__node_remove().
func (fw *FileWriter) unlinkFromParent(parentPath, childName string) (uint64, error) {
	// A group of the 1.8 format (H5G_obj_remove, H5Gobj.c:925-975).
	if addr, ok := fw.linkMessageGroup(parentPath); ok {
		removed, err := fw.removeGroupLink(addr, childName)
		if errors.Is(err, errGroupEntryNotFound) {
			return 0, fmt.Errorf("child %q not found in parent group: %w", childName, err)
		}
		if err != nil {
			return 0, fmt.Errorf("remove %q from group %q: %w", childName, parentPath, err)
		}
		return removed, nil
	}

	// Get parent group metadata (symbol-table groups).
	var heapAddr, btreeAddr uint64
	if parentPath == "" || parentPath == "/" {
		heapAddr = fw.rootHeapAddr
		btreeAddr = fw.rootBTreeAddr
	} else {
		meta, exists := fw.groups[parentPath]
		if !exists {
			return 0, fmt.Errorf("parent group %q not found", parentPath)
		}
		heapAddr = meta.heapAddr
		btreeAddr = meta.btreeAddr
	}

	// Step 1: Read existing local heap.
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return 0, fmt.Errorf("read local heap: %w", err)
	}

	// Step 2: Remove the entry from the group's B-tree and its names from
	// the heap, as H5G__stab_remove does through H5B_remove (H5Gstab.c:540-580).
	removed, err := fw.groupSymbolTable(heap).remove(btreeAddr, childName)
	if errors.Is(err, errGroupEntryNotFound) {
		return 0, fmt.Errorf("child %q not found in parent group: %w", childName, err)
	}
	if err != nil {
		return 0, fmt.Errorf("remove %q from group B-tree: %w", childName, err)
	}
	removedAddr := removed.ObjectAddress

	// Write updated heap.
	if err := heap.WriteTo(fw.writer, heapAddr); err != nil {
		return 0, fmt.Errorf("write heap: %w", err)
	}

	return removedAddr, nil
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
	_, err := fw.CreateGroup(name)
	if err != nil {
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
