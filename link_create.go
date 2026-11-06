package hdf5

import (
	"errors"
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
)

// Every creator (CreateGroup, CreateDenseGroup, the dataset creators and the
// link creators) follows the order of H5L__link_cb (H5Lint.c:552-669):
//
//  1. validate the path;
//  2. look the name up in the parent group and refuse one it already holds
//     (H5Lint.c:552-554), before anything is allocated or written;
//  3. allocate and write the new object (H5O_obj_create, :565);
//  4. link it into the parent (H5G_obj_insert, :605);
//  5. register it (a group, so children can be linked into it).
//
// When the link fails after the object was written, the object is deleted
// again, as C drops the new object's only reference (H5O_dec_rc_by_loc,
// :660-669, which deletes an object header whose link count is 0).

// checkLinkNameFree refuses name when the group at parentPath already holds
// a link of that name (errGroupEntryExists), and a parent that is not a
// group this writer knows. It only reads. It looks the name up as
// H5G__obj_lookup does (H5Gobj.c:1056-1090), in whichever form the group
// keeps its links: link messages or dense link storage (lookupGroupLink:
// H5G__compact_lookup, H5G__dense_lookup) or a symbol table
// (H5G__stab_lookup).
//
// Looking the name up first matters for a symbol table: the insert adds the
// name to the group's local heap before it can find the duplicate, and a
// full heap is moved for that (expandHeapAndAdd), so a refused insert would
// leave the group pointing at the freed heap. C finds the duplicate in
// H5G__node_insert before H5G__link_to_ent touches the heap
// (H5Gnode.c:583-594).
func (fw *FileWriter) checkLinkNameFree(parentPath, name string) error {
	parent := parentPath
	if parent == "" {
		parent = "/"
	}
	if addr, ok := fw.linkMessageGroup(parentPath); ok {
		_, err := fw.lookupGroupLink(addr, name)
		if errors.Is(err, errGroupEntryNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("look up %q in group %q: %w", name, parent, err)
		}
		return linkExistsError(parent, name)
	}
	if parent == "/" {
		return fw.checkSymbolTableNameFree(fw.rootHeapAddr, fw.rootBTreeAddr, parent, name)
	}
	meta, ok := fw.groups[parentPath]
	if !ok {
		return fmt.Errorf("parent group %q does not exist (create it first)", parentPath)
	}
	return fw.checkSymbolTableNameFree(meta.heapAddr, meta.btreeAddr, parentPath, name)
}

// linkExistsError is the error for a name the group parent already holds.
func linkExistsError(parent, name string) error {
	return fmt.Errorf("%w: %q in group %q", errGroupEntryExists, name, parent)
}

// checkSymbolTableNameFree looks name up in the symbol table of the group
// whose B-tree is at btreeAddr and local heap at heapAddr (H5G__stab_lookup,
// H5Gstab.c:740-770).
func (fw *FileWriter) checkSymbolTableNameFree(heapAddr, btreeAddr uint64, parent, name string) error {
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return fmt.Errorf("read local heap of group %q: %w", parent, err)
	}
	_, err = fw.groupSymbolTable(heap).lookup(btreeAddr, name)
	if errors.Is(err, errGroupEntryNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up %q in group %q: %w", name, parent, err)
	}
	return linkExistsError(parent, name)
}

// discardUnlinkedObject deletes the object at objectAddr that a creator
// wrote but could not link, and returns cause joined with any error of the
// delete. The object has no link, so it is deleted as Delete deletes an
// object whose last link goes: every block deleteRanges collects for it is
// freed (H5O_dec_rc_by_loc → H5O_delete, H5Oint.c). A block the delete
// cannot free stays allocated: space is lost, nothing is corrupted.
func (fw *FileWriter) discardUnlinkedObject(objectAddr uint64, cause error) error {
	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), objectAddr, sb)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("delete the unlinked object at 0x%X: %w", objectAddr, err))
	}
	ranges, err := fw.deleteRanges(objectAddr, oh, sb)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("delete the unlinked object at 0x%X: %w", objectAddr, err))
	}
	delete(fw.headerAllocs, objectAddr)
	allocator := fw.writer.Allocator()
	for _, r := range ranges {
		if err := allocator.Free(r.Offset, r.Size); err != nil {
			return errors.Join(cause, fmt.Errorf("delete the unlinked object at 0x%X: free [0x%X, +%d): %w",
				objectAddr, r.Offset, r.Size, err))
		}
	}
	return cause
}
