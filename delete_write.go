package hdf5

import (
	"fmt"
	"strings"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/writer"
)

// undefinedAddress is the HDF5 undefined address sentinel value.
// Objects with this address are not stored on disk and should be skipped.
const undefinedAddress = uint64(0xFFFFFFFFFFFFFFFF)

// Delete removes an object (dataset or empty group) from the HDF5 file.
//
// This performs a full deletion:
//  1. Unlinks the object from its parent group's symbol table
//  2. Decrements the object's reference count (hard link count)
//  3. If refcount reaches 0, frees the object's storage: the object header
//     and its continuation chunks, contiguous data, every chunk and every
//     block of the chunk index, dense attribute storage, and an empty
//     group's structures (a symbol table's B-tree and local heap, or the
//     fractal heap and indexes of dense link storage)
//
// Constraints:
//   - Cannot delete the root group "/"
//   - Cannot delete a group that still has links, in any of the three forms
//     a group stores them (a symbol table, link messages in its header, or
//     dense link storage): delete its children first
//   - Cannot delete the last link of an object that holds a reference into
//     the file's shared object header message heap (a message, an attribute
//     or an attribute's datatype or dataspace stored there): the error wraps
//     ErrNotSupported
//   - Path must start with "/"
//   - Object must exist
//
// Every refusal comes before anything is written, so the file is left
// unchanged.
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

	// Step 1: Everything that can refuse the delete runs before the first
	// write, so a refused delete leaves the file as it was: the lookup, the
	// object header and, for an object losing its last link, the check that
	// it is not a group with children and the collection and check of every
	// block its storage frees. (The C library decrements the link count before
	// it removes the entry, H5Gnode.c:782-828, and deletes a group's children
	// with it; this library refuses a non-empty group.)
	objectAddr, err := fw.resolveObjectAddress(path)
	if err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	sb := fw.file.Superblock()
	oh, ranges, err := fw.prepareDelete(objectAddr, sb)
	if err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}

	// Step 2: Unlink from parent group.
	if _, err := fw.unlinkFromParent(parent, name); err != nil {
		return fmt.Errorf("delete %q: unlink failed: %w", path, err)
	}
	if oh == nil {
		return nil
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

	// Step 4: The last link is gone: free every block of the object's
	// storage, checked in step 1 (H5O_delete, H5Oint.c:1508-1600). A Free
	// error here is a bug in that check, not a property of the file.
	allocator := fw.writer.Allocator()
	for _, r := range ranges {
		if err := allocator.Free(r.Offset, r.Size); err != nil {
			return fmt.Errorf("delete %q: free [0x%X, +%d) after the check passed: %w", path, r.Offset, r.Size, err)
		}
	}

	// Step 5: Remove from groups map if tracked.
	delete(fw.groups, path)

	return nil
}

// prepareDelete runs the checks of a delete that come before its first write:
// it returns the object header at objectAddr, nil for a link with no object
// (a soft link), and, when the object loses its last link, every block its
// storage frees, already checked against the allocator.
func (fw *FileWriter) prepareDelete(objectAddr uint64, sb *core.Superblock) (*core.ObjectHeader, []writer.FreeBlock, error) {
	if objectAddr == undefinedAddress || objectAddr == 0 {
		return nil, nil, nil
	}
	oh, err := fw.checkDeletable(objectAddr, sb)
	if err != nil {
		return nil, nil, err
	}
	if oh.ReferenceCount > 1 {
		return oh, nil, nil
	}
	ranges, err := fw.deleteRanges(objectAddr, oh, sb)
	if err != nil {
		return nil, nil, err
	}

	return oh, ranges, nil
}

// deleteRanges collects every block of file space the object at objectAddr
// holds, as H5O_delete frees it once the last link goes (H5Oint.c:1508-1600):
// each chunk of the object header, chunk 0 and the continuation chunks
// (H5O__cont_delete, H5Ocont.c:207-220); contiguous data (H5D__contig_delete,
// H5Dcontig.c:352); every chunk and every block of the chunk index
// (core.DatasetDescriptor.StorageBlocks); a group's B-tree nodes, symbol
// table nodes and local heap (H5G__stab_delete). Then it checks them against
// the allocator without changing it (writer.Allocator.CheckFree): every range
// lies in the file and overlaps neither free space nor another range.
//
// Dense attribute storage (H5O__ainfo_delete → H5A__dense_delete,
// H5Oainfo.c:327-342) and dense link storage (H5O__linfo_delete →
// H5G__dense_delete, H5Olinfo.c:331-344) are collected by
// core.DenseAttributeStorageBlocks and core.DenseLinkStorageBlocks: their v2
// B-trees, the fractal heap's blocks, free-space manager and huge objects.
// An object holding a reference into the shared object header message heap
// is refused (core.CheckNoSOHMReferences): C would decrement it there
// (H5SM_delete), which this library cannot do.
//
// It only reads, so Delete runs it before its first write: a structure that
// cannot be read or a range that fails the check refuses the delete with the
// file unchanged.
func (fw *FileWriter) deleteRanges(objectAddr uint64, oh *core.ObjectHeader, sb *core.Superblock) ([]writer.FreeBlock, error) {
	if err := core.CheckNoSOHMReferences(sb, oh.Messages); err != nil {
		return nil, err
	}

	limit := fw.file.ReadLimit()
	var ranges []writer.FreeBlock
	add := func(addr, size uint64) error {
		// Each kept range costs 16 bytes against the read limit, as
		// CollectChunks charges its entries.
		if uint64(len(ranges)) >= limit/16 {
			return fmt.Errorf("%w: the object's storage has more than %d blocks, more than the read limit of %d bytes holds",
				utils.ErrLimit, len(ranges), limit)
		}
		ranges = append(ranges, writer.FreeBlock{Offset: addr, Size: size})
		return nil
	}

	size0, err := readChunk0Size(fw, objectAddr)
	if err != nil {
		return nil, err
	}
	if err := add(objectAddr, size0); err != nil {
		return nil, err
	}

	ainfo := attributeInfoMessage(oh)
	for _, msg := range oh.Messages {
		if err := fw.messageRanges(oh, msg, msg == ainfo, sb, limit, add); err != nil {
			return nil, err
		}
	}

	if err := fw.writer.Allocator().CheckFree(ranges); err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
	}
	return ranges, nil
}

// messageRanges collects the blocks one message of oh refers to
// (deleteRanges); isAinfo marks the object's attribute info message.
func (fw *FileWriter) messageRanges(oh *core.ObjectHeader, msg *core.HeaderMessage, isAinfo bool, sb *core.Superblock,
	limit uint64, add func(addr, size uint64) error,
) error {
	switch msg.Type {
	case core.MsgContinuation:
		addr, length, err := continuationMessage(msg.Data, sb)
		if err != nil {
			return err
		}
		return add(addr, length)

	case core.MsgDataLayout:
		return fw.layoutRanges(oh, msg.Data, sb, limit, add)

	case core.MsgSymbolTable:
		return fw.groupStructureRanges(msg.Data, sb, add)

	case core.MsgLinkInfo:
		return fw.denseLinkRanges(msg.Data, sb, add)

	default:
		if isAinfo {
			return fw.denseAttributeRanges(msg.Data, sb, add)
		}
		return nil
	}
}

// denseAttributeRanges collects the dense attribute storage an attribute
// info message names (H5O__ainfo_delete, H5Oainfo.c:327-342), nothing while
// the attributes are compact (an undefined heap address).
func (fw *FileWriter) denseAttributeRanges(data []byte, sb *core.Superblock, add func(addr, size uint64) error) error {
	ainfo, err := core.ParseAttributeInfoMessage(data, sb)
	if err != nil {
		return fmt.Errorf("%w: attribute info message: %w", utils.ErrCorrupt, err)
	}
	if !ainfo.HasDenseStorage(sb.OffsetSize) {
		return nil
	}
	if _, err := core.DenseAttributeStorageBlocks(fw.writer.Reader(), sb, ainfo, add); err != nil {
		return fmt.Errorf("dense attribute storage: %w", err)
	}
	return nil
}

// denseLinkRanges collects the dense link storage a link info message names
// (H5O__linfo_delete, H5Olinfo.c:331-344), nothing for a group whose links
// are compact. Like groupStructureRanges it refuses a group that still has
// links; Delete checks that first (checkGroupEmpty).
func (fw *FileWriter) denseLinkRanges(data []byte, sb *core.Superblock, add func(addr, size uint64) error) error {
	linfo, err := core.ParseLinkInfoMessage(data, sb)
	if err != nil {
		return fmt.Errorf("%w: link info message: %w", utils.ErrCorrupt, err)
	}
	if !linfo.HasFractalHeap() {
		return nil
	}
	dense, err := core.DenseLinkStorageBlocks(fw.writer.Reader(), sb, linfo, add)
	if err != nil {
		return fmt.Errorf("dense link storage: %w", err)
	}
	if dense.Records > 0 {
		return nonEmptyGroupError(dense.Records)
	}
	return nil
}

// continuationMessage decodes a continuation message: the chunk's address
// and its length, of the file's offset and length sizes (H5O__cont_decode,
// H5Ocont.c).
func continuationMessage(data []byte, sb *core.Superblock) (addr, length uint64, err error) {
	os, ls := int(sb.OffsetSize), int(sb.LengthSize)
	if len(data) < os+ls {
		return 0, 0, fmt.Errorf("continuation message of %d bytes, need %d: %w", len(data), os+ls, utils.ErrCorrupt)
	}
	return readAddrFromBuf(data, os, sb.Endianness), readAddrFromBuf(data[os:], ls, sb.Endianness), nil
}

// layoutRanges collects the data blocks of a data layout message: the one
// block of contiguous data, every chunk and chunk index block of chunked
// data (as StorageBlocks reports them), nothing for compact data, which lives
// in the message. An undefined address is storage never allocated.
func (fw *FileWriter) layoutRanges(oh *core.ObjectHeader, data []byte, sb *core.Superblock, limit uint64,
	add func(addr, size uint64) error,
) error {
	layout, err := core.ParseDataLayoutMessage(data, sb)
	if err != nil {
		return fmt.Errorf("data layout message: %w", err)
	}

	switch {
	case layout.IsContiguous():
		if layout.DataAddress == undefinedAddress || layout.DataSize == 0 {
			return nil
		}
		return add(layout.DataAddress, layout.DataSize)

	case layout.IsChunked():
		if layout.DataAddress == undefinedAddress {
			return nil
		}
		desc, err := core.NewDatasetDescriptor(fw.writer.Reader(), oh, sb)
		if err != nil {
			return fmt.Errorf("dataset messages: %w", err)
		}
		if err := desc.StorageBlocks(fw.writer.Reader(), sb, limit, add); err != nil {
			return fmt.Errorf("chunk index: %w", err)
		}
	}

	return nil
}

// checkDeletable reads the object header at addr and, when the delete would
// remove its last link, refuses a group that still has children. It only
// reads, so Delete runs it before its first write.
func (fw *FileWriter) checkDeletable(addr uint64, sb *core.Superblock) (*core.ObjectHeader, error) {
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	if err != nil {
		return nil, fmt.Errorf("read object header at 0x%X: %w", addr, err)
	}
	if oh.ReferenceCount <= 1 {
		if err := fw.checkGroupEmpty(oh, sb); err != nil {
			return nil, err
		}
	}
	return oh, nil
}

// writeRefCount rewrites the object header with an updated reference count.
// For V2 headers, this adds/updates a RefCount message.
// For V1 headers, the refcount is part of the header prefix (not rewritten in MVP).
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

// groupStructureRanges collects the B-tree nodes, symbol table nodes and
// local heap of a group, as H5G__stab_delete frees them through H5B_delete
// and H5HL_delete (H5Gstab.c:200-245, H5B.c:1580-1638). The local heap is one
// block when its data segment follows the prefix (a single cache object),
// otherwise the prefix and the data segment (H5HL_delete, H5HL.c:847-900).
// Unlike the C library, which removes the entries of a non-empty group with
// it, it refuses a group that still has children (delete them first); Delete
// checks that before its first write (checkGroupEmpty), so the check here
// only guards other callers.
func (fw *FileWriter) groupStructureRanges(symTableData []byte, sb *core.Superblock, add func(addr, size uint64) error) error {
	btreeAddr, heapAddr, err := symbolTableMessageAddrs(symTableData, sb)
	if err != nil {
		return err
	}

	g := fw.groupSymbolTable(nil)
	btreeNodes, snods, err := g.groupNodes(btreeAddr)
	if err != nil {
		return fmt.Errorf("read group B-tree: %w", err)
	}
	if err := g.requireNoEntries(snods); err != nil {
		return err
	}

	for _, addr := range snods {
		if err := add(addr, g.l.SNODSize()); err != nil {
			return err
		}
	}
	for _, addr := range btreeNodes {
		if err := add(addr, g.l.NodeSize()); err != nil {
			return err
		}
	}

	heap, err := structures.LoadLocalHeap(fw.writer, heapAddr, sb)
	if err != nil {
		return fmt.Errorf("read local heap at 0x%X: %w", heapAddr, err)
	}
	if heap.Contiguous(heapAddr) {
		return add(heapAddr, heap.Size())
	}
	if err := add(heapAddr, heap.PrefixSize()); err != nil {
		return err
	}
	return add(heap.DataSegmentAddress, heap.DataSegmentSize)
}

// checkGroupEmpty refuses a group that still has links, in each form a group
// keeps them: symbol table entries (H5G__stab_delete), link messages in its
// header (H5G__compact_*) and dense link storage (H5G__dense_*), read through
// the readers the group listing uses. The C library deletes the children with
// the group (H5O__linfo_delete with adj_link, H5Gdense.c:1581; H5Gstab.c);
// this library refuses, so no child is orphaned. It only reads, so Delete
// runs it before it writes anything.
func (fw *FileWriter) checkGroupEmpty(oh *core.ObjectHeader, sb *core.Superblock) error {
	var links uint64
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgSymbolTable:
			if err := fw.requireEmptySymbolTable(msg.Data, sb); err != nil {
				return err
			}

		case core.MsgLinkMessage:
			links++

		case core.MsgLinkInfo:
			n, err := fw.denseLinkCount(msg.Data, sb)
			if err != nil {
				return err
			}
			links += n
		}
	}
	if links > 0 {
		return nonEmptyGroupError(links)
	}
	return nil
}

// requireEmptySymbolTable refuses a symbol table group with entries.
func (fw *FileWriter) requireEmptySymbolTable(data []byte, sb *core.Superblock) error {
	btreeAddr, _, err := symbolTableMessageAddrs(data, sb)
	if err != nil {
		return err
	}
	g := fw.groupSymbolTable(nil)
	_, snods, err := g.groupNodes(btreeAddr)
	if err != nil {
		return fmt.Errorf("read group B-tree: %w", err)
	}
	return g.requireNoEntries(snods)
}

// denseLinkCount returns the number of links in the dense link storage a
// link info message points at, 0 when it has none.
func (fw *FileWriter) denseLinkCount(data []byte, sb *core.Superblock) (uint64, error) {
	linfo, err := core.ParseLinkInfoMessage(data, sb)
	if err != nil {
		return 0, fmt.Errorf("%w: link info message: %w", utils.ErrCorrupt, err)
	}
	if !linfo.HasFractalHeap() || !linfo.HasNameBTree() {
		return 0, nil
	}
	objects, err := core.ReadDenseHeapObjects(fw.writer.Reader(), linfo.NameBTreeAddress, linfo.FractalHeapAddress, sb)
	if err != nil {
		return 0, fmt.Errorf("read dense links: %w", err)
	}
	return uint64(len(objects)), nil
}

// nonEmptyGroupError is the refusal of a group that still has children.
func nonEmptyGroupError[T int | uint64](children T) error {
	return fmt.Errorf("cannot delete non-empty group (has %d children); delete children first", children)
}

// requireNoEntries refuses a group whose symbol table nodes hold entries.
func (g *groupSymbolTable) requireNoEntries(snods []uint64) error {
	children := 0
	for _, addr := range snods {
		sn, err := g.readSNOD(addr)
		if err != nil {
			return err
		}
		children += len(sn.Entries)
	}
	if children > 0 {
		return nonEmptyGroupError(children)
	}
	return nil
}

// symbolTableMessageAddrs decodes a symbol table message: the B-tree
// address, then the local heap address, each of the file's offset size
// (H5O__stab_decode, H5Ostab.c).
func symbolTableMessageAddrs(data []byte, sb *core.Superblock) (btreeAddr, heapAddr uint64, err error) {
	osSize := int(sb.OffsetSize)
	if len(data) < 2*osSize {
		return 0, 0, fmt.Errorf("symbol table message of %d bytes, need %d: %w", len(data), 2*osSize, utils.ErrCorrupt)
	}
	return readAddrFromBuf(data[0:], osSize, sb.Endianness), readAddrFromBuf(data[osSize:], osSize, sb.Endianness), nil
}
