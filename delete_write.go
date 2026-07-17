package hdf5

import (
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
)

// undefinedAddress is the HDF5 undefined address sentinel value.
// Objects with this address are not stored on disk and should be skipped.
const undefinedAddress = uint64(0xFFFFFFFFFFFFFFFF)

// Delete removes an object (dataset or empty group) from the HDF5 file.
//
// This performs a full deletion:
//  1. Unlinks the object from its parent group's symbol table
//  2. Decrements the object's reference count (hard link count)
//  3. If refcount reaches 0, performs cascade delete:
//     - Frees contiguous data blocks
//     - Frees chunked data blocks (walks chunk B-tree)
//     - Frees the object header itself
//
// Constraints:
//   - Cannot delete the root group "/"
//   - Cannot delete non-empty groups (delete children first)
//   - Path must start with "/"
//   - Object must exist
//
// Parameters:
//   - path: Absolute path to the object (e.g., "/dataset1", "/group1/data")
//
// Returns:
//   - error: If deletion fails
//
// Example:
//
//	fw, _ := hdf5.OpenForWrite("data.h5", hdf5.OpenReadWrite)
//	defer fw.Close()
//	fw.Delete("/old_dataset")       // Remove a dataset
//	fw.Delete("/empty_group")       // Remove an empty group
//
// Reference: H5Ldelete.c, H5G_obj_remove(), H5O_link(adjust=-1), H5O_delete().
func (fw *FileWriter) Delete(path string) error {
	// Validate path.
	if path == "" {
		return fmt.Errorf("delete: path cannot be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("delete: path must start with '/' (got %q)", path)
	}
	if path == "/" {
		return fmt.Errorf("delete: cannot delete root group")
	}

	// Parse path into parent + name.
	parent, name := parsePath(path)

	// Step 1: Unlink from parent group. This returns the object's address.
	objectAddr, err := fw.unlinkFromParent(parent, name)
	if err != nil {
		return fmt.Errorf("delete %q: unlink failed: %w", path, err)
	}

	// Step 2: Read the target object header to check reference count
	// and determine what needs to be freed.
	if objectAddr == undefinedAddress || objectAddr == 0 {
		// Object has no valid address (e.g., soft link target). Nothing to cascade-delete.
		return nil
	}

	sb := fw.file.Superblock()
	reader := fw.writer.Reader()
	oh, err := core.ReadObjectHeader(reader, objectAddr, sb)
	if err != nil {
		// Object header unreadable. Unlink succeeded, so parent is consistent.
		// Log the error context but don't fail the entire delete.
		return fmt.Errorf("delete %q: warning: unlinked but could not read object header at 0x%X: %w", path, objectAddr, err)
	}

	// Step 3: Handle reference counting.
	// V1 OHDR: ReferenceCount field in header (parsed during ReadObjectHeader).
	// V2 OHDR: RefCount message (type 22), default nlink=1 if absent.
	newRefCount := oh.DecrementReferenceCount()

	if newRefCount > 0 {
		// Object still has other hard links pointing to it.
		// Rewrite the object header with decremented refcount (no cascade delete).
		if err := fw.writeRefCount(objectAddr, oh, sb); err != nil {
			return fmt.Errorf("delete %q: failed to update reference count: %w", path, err)
		}
		return nil
	}

	// Step 4: Cascade delete — refcount is now 0, free all storage.
	if err := fw.cascadeDelete(objectAddr, oh, sb); err != nil {
		return fmt.Errorf("delete %q: cascade delete failed: %w", path, err)
	}

	// Step 5: Remove from groups map if tracked.
	delete(fw.groups, path)

	return nil
}

// writeRefCount rewrites the object header with an updated reference count.
// For V2 headers, this adds/updates a RefCount message.
// For V1 headers, the refcount is part of the header prefix and is not rewritten.
func (fw *FileWriter) writeRefCount(addr uint64, oh *core.ObjectHeader, sb *core.Superblock) error {
	if oh.Version != 2 {
		// V1: RefCount is in the header prefix. The ObjectHeaderWriter handles this.
		// We don't support rewriting V1 headers at this time (they are legacy format).
		// V1 headers default to refcount=1 and are only used for backward compatibility.
		return nil
	}

	// V2: Update or add RefCount message (type 0x0016).
	// Body: version byte (0) + uint32 count (H5Orefcount.c).
	refCountData := make([]byte, 5)
	refCountData[0] = 0 // message version
	sb.Endianness.PutUint32(refCountData[1:], oh.ReferenceCount)

	found := false
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgRefCount {
			if len(msg.Data) == 4 {
				// Pre-v0.15 layout without the version byte — replace it.
				msg.Data = make([]byte, 5)
			}
			copy(msg.Data, refCountData)
			found = true
			break
		}
	}
	if !found {
		// Add a RefCount message if one doesn't exist yet.
		if err := core.AddMessageToObjectHeader(oh, core.MsgRefCount, refCountData); err != nil {
			return fmt.Errorf("add refcount message: %w", err)
		}
	}

	// Bounds-checked write: the header may have grown past its allocation
	// at the end of the file (keeps the superblock EOA correct).
	return writeOHDRWithBoundsCheck(fw, addr, oh, sb)
}

// cascadeDelete frees all storage associated with an object whose refcount has
// reached zero. This walks the object header messages to find data blocks,
// chunk B-trees, and sub-group structures, freeing them all.
//
// Reference: H5O_delete(), H5O__layout_delete(), H5D_close().
func (fw *FileWriter) cascadeDelete(objectAddr uint64, oh *core.ObjectHeader, sb *core.Superblock) error {
	allocator := fw.writer.Allocator()

	// Walk object header messages to find freeable resources.
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgDataLayout:
			// Dataset data storage — free the data blocks.
			layout, err := core.ParseDataLayoutMessage(msg.Data, sb)
			if err != nil {
				// Skip unparseable layout messages rather than failing the whole delete.
				continue
			}
			fw.freeDataLayout(layout, allocator)

		case core.MsgSymbolTable:
			// Group — check if it has children (reject non-empty groups).
			// The symbol table message contains B-tree and heap addresses.
			if err := fw.freeGroupStructures(msg.Data, sb, allocator); err != nil {
				return err
			}
		}
	}

	// Free the object header itself.
	ohdrSize := core.ObjectHeaderSizeFromParsed(oh)
	if ohdrSize > 0 {
		_ = allocator.Free(objectAddr, ohdrSize)
	}

	return nil
}

// freeDataLayout frees data blocks described by a DataLayoutMessage.
//
// For contiguous layouts, this frees a single block.
// For chunked layouts, this walks the chunk B-tree and frees each chunk.
// For compact layouts, data is embedded in the message — nothing to free.
//
// Reference: H5O__layout_delete(), H5D__chunk_delete().
func (fw *FileWriter) freeDataLayout(layout *core.DataLayoutMessage, allocator interface{ Free(uint64, uint64) error }) {
	switch {
	case layout.IsContiguous():
		// Free contiguous data block.
		if layout.DataAddress != undefinedAddress && layout.DataAddress != 0 && layout.DataSize > 0 {
			_ = allocator.Free(layout.DataAddress, layout.DataSize)
		}

	case layout.IsChunked():
		// Walk chunk B-tree and free each chunk.
		// The DataAddress points to the chunk index B-tree root.
		if layout.DataAddress != undefinedAddress && layout.DataAddress != 0 {
			fw.freeChunkedData(layout.DataAddress, allocator)
		}

	case layout.IsCompact():
		// Compact layout: data is embedded in the object header message.
		// Nothing to free separately — it gets freed when the OHDR is freed.
	}
}

// freeChunkedData walks a chunk B-tree v1 and frees each chunk's data.
// This reads the B-tree at the given address and iterates all child pointers
// to find chunk data addresses and sizes.
//
// Reference: H5D__chunk_delete(), H5B_iterate().
//
//nolint:gocognit,gocyclo,cyclop // Inherently complex: parses variable-dimension chunk B-tree keys with fallback heuristics
func (fw *FileWriter) freeChunkedData(btreeAddr uint64, allocator interface{ Free(uint64, uint64) error }) {
	// Read the chunk B-tree. This is a B-tree v1 (type 1 = raw data chunks).
	offsetSize := fw.file.sb.OffsetSize
	endianness := fw.file.sb.Endianness

	// Read header: signature(4) + type(1) + level(1) + entries(2) + left(offsetSize) + right(offsetSize).
	headerSize := 8 + 2*int(offsetSize)
	header := make([]byte, headerSize)
	//nolint:gosec // G115: HDF5 addresses fit in int64.
	if _, err := fw.writer.ReadAt(header, int64(btreeAddr)); err != nil {
		return // Cannot read B-tree header; skip chunk freeing.
	}

	sig := string(header[0:4])
	if sig != "TREE" { //nolint:goconst // HDF5 signature used across multiple packages
		return // Not a valid B-tree.
	}

	nodeType := header[4]
	if nodeType != 1 {
		return // Not a chunk B-tree (type 1 = raw data chunks).
	}

	entriesUsed := endianness.Uint16(header[6:8])
	if entriesUsed == 0 {
		return
	}

	nodeLevel := header[5]

	if nodeLevel > 0 {
		// Only leaf nodes (level 0) are handled; internal nodes are not recursed.
		// Multi-level chunk B-trees are rare for small-medium datasets.
		return
	}

	// Leaf node layout after the header: Key[0], Child[0], Key[1], ..., Key[N].
	// Each key is chunkSize(4) + filterMask(4) + 4*ndims dimension offsets, and each
	// child pointer (offsetSize bytes) is the chunk data address. The dataset's
	// dimensionality is not stored in the B-tree, so the key size is unknown here.
	// Probe common key sizes (1-4 dimensions) and use the first that parses into
	// valid child pointers, then free each chunk using the size from its key.
	for _, ndims := range []int{1, 2, 3, 4} {
		keySize := 4 + 4 + 4*ndims // chunkSize + filterMask + ndims*4
		totalDataSize := (int(entriesUsed)+1)*keySize + int(entriesUsed)*int(offsetSize)
		data := make([]byte, totalDataSize)
		//nolint:gosec // G115: HDF5 addresses fit in int64.
		if _, err := fw.writer.ReadAt(data, int64(btreeAddr)+int64(headerSize)); err != nil {
			continue
		}

		// Try to parse entries and validate child pointers.
		valid := true
		type chunkInfo struct {
			addr uint64
			size uint32
		}
		chunks := make([]chunkInfo, 0, entriesUsed)

		pos := 0
		for i := uint16(0); i < entriesUsed; i++ {
			if pos+keySize > len(data) {
				valid = false
				break
			}
			chunkSize := endianness.Uint32(data[pos : pos+4])
			pos += keySize

			if pos+int(offsetSize) > len(data) {
				valid = false
				break
			}
			childAddr := readAddrFromBuf(data[pos:], int(offsetSize), endianness)
			pos += int(offsetSize)

			if childAddr == 0 || childAddr == undefinedAddress {
				valid = false
				break
			}
			chunks = append(chunks, chunkInfo{addr: childAddr, size: chunkSize})
		}

		if valid && len(chunks) == int(entriesUsed) {
			// Successfully parsed. Free each chunk.
			for _, c := range chunks {
				_ = allocator.Free(c.addr, uint64(c.size))
			}
			return
		}
	}
	// Could not determine key size. B-tree node will be freed with OHDR;
	// chunk data blocks are leaked.
}

// freeGroupStructures frees the B-tree, symbol table nodes, and local heap
// associated with a group. Returns an error if the group still has children
// (non-empty groups must have children deleted first).
//
// Reference: H5G_close(), H5G_obj_remove().
func (fw *FileWriter) freeGroupStructures(symTableData []byte, sb *core.Superblock, allocator interface{ Free(uint64, uint64) error }) error {
	// Parse symbol table message to get B-tree and heap addresses.
	// Format: B-tree address (offsetSize) + Heap address (offsetSize).
	osSize := int(sb.OffsetSize)
	if len(symTableData) < 2*osSize {
		return nil // Corrupted or empty message.
	}

	btreeAddr := readAddrFromBuf(symTableData[0:], osSize, sb.Endianness)
	heapAddr := readAddrFromBuf(symTableData[osSize:], osSize, sb.Endianness)

	// Read B-tree to check if group has children.
	_, snodAddrs, err := fw.readGroupBTree(btreeAddr)
	if err != nil {
		return nil //nolint:nilerr // Intentional: unreadable B-tree treated as empty group during cascade delete
	}

	// Check if any SNODs have entries (children).
	for _, addr := range snodAddrs {
		sn, readErr := fw.readSymbolTableNode(addr)
		if readErr != nil {
			continue
		}
		if len(sn.Entries) > 0 {
			return fmt.Errorf("cannot delete non-empty group (has %d children); delete children first", len(sn.Entries))
		}
	}

	// Group is empty. Free its structures.
	// Free each SNOD.
	for _, addr := range snodAddrs {
		_ = allocator.Free(addr, snodTotalSize)
	}

	// Free heap using its parsed size (fixed header + data segment).
	heap, heapErr := fw.readLocalHeap(heapAddr)
	if heapErr == nil {
		_ = allocator.Free(heapAddr, heap.Size())
	}

	// The B-tree node itself is part of the fixed layout; it will be freed
	// when the parent OHDR is freed or left as dead space.

	return nil
}
