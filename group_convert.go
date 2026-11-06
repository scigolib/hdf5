package hdf5

import (
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
	"github.com/scigolib/hdf5/internal/writer"
)

// convertToLinkMessageGroup converts the symbol-table group at path to the
// 1.8 format, as H5G_obj_insert does before it inserts a link a symbol table
// cannot hold, such as an external link (H5Gobj.c:512-552):
//
//  1. a link info message and a group info message are added to the group's
//     header (the defaults H5G_CRT_LINK_INFO_DEF and H5G_CRT_GROUP_INFO_DEF,
//     the group info message constant);
//  2. every entry of the symbol table is inserted as a link in name order
//     (H5G__stab_iterate with H5G__obj_stab_to_new_cb, H5Gobj.c:217-265),
//     without changing the reference count of its object (adj_link false);
//     past max_compact links the group moves to dense storage as any insert
//     does;
//  3. the symbol table message is removed.
//
// Deviation: C leaves the B-tree nodes, symbol table nodes and local heap of
// the old table allocated in the file; here they are freed, checked against
// the allocator before the first write.
//
// The group's registration then names no symbol table, so later operations
// take the 1.8 path (linkMessageGroup). For the root group the superblock's
// cached symbol table addresses are cleared too: the C library drops a
// cache that names a table the root no longer has (H5Groot.c:213-222).
func (fw *FileWriter) convertToLinkMessageGroup(path string) error {
	groupAddr, heapAddr, btreeAddr, err := fw.symbolTableGroup(path)
	if err != nil {
		return err
	}
	oh, err := fw.symbolTableHeader(path, groupAddr)
	if err != nil {
		return err
	}
	entries, ranges, err := fw.symbolTableContents(heapAddr, btreeAddr)
	if err != nil {
		return fmt.Errorf("group %q: %w", path, err)
	}

	// Step 1: the link info and group info messages.
	linfo, err := core.EncodeLinkInfoMessage(&core.LinkInfoMessage{
		FractalHeapAddress: undefinedAddress,
		NameBTreeAddress:   undefinedAddress,
	}, fw.file.sb)
	if err != nil {
		return fmt.Errorf("group %q: encode link info message: %w", path, err)
	}
	oh.Messages = append(oh.Messages,
		&core.HeaderMessage{Type: core.MsgLinkInfo, Data: linfo},
		&core.HeaderMessage{Type: msgGroupInfo, Data: []byte{groupInfoVersion, 0}, Flags: core.MsgFlagConstant},
	)
	if err := fw.rewriteObjectHeader(groupAddr, oh); err != nil {
		return fmt.Errorf("group %q: add link info: %w", path, err)
	}

	// Step 2: the links.
	for _, e := range entries {
		if err := fw.insertLink(groupAddr, e.name, e.target); err != nil {
			return fmt.Errorf("group %q: move %q to a link message: %w", path, e.name, err)
		}
	}

	// Step 3: the symbol table message, then the table's space.
	if err := fw.removeSymbolTableMessage(groupAddr); err != nil {
		return fmt.Errorf("group %q: remove symbol table message: %w", path, err)
	}
	fw.forgetSymbolTable(path)
	alloc := fw.writer.Allocator()
	for _, r := range ranges {
		if err := alloc.Free(r.Offset, r.Size); err != nil {
			return fmt.Errorf("group %q: free symbol table space at 0x%X: %w", path, r.Offset, err)
		}
	}
	return nil
}

// symbolTableGroup returns the header, local heap and B-tree addresses of
// the registered symbol-table group at path.
func (fw *FileWriter) symbolTableGroup(path string) (groupAddr, heapAddr, btreeAddr uint64, err error) {
	if path == "" || path == "/" {
		return fw.rootGroupAddr, fw.rootHeapAddr, fw.rootBTreeAddr, nil
	}
	meta, ok := fw.groups[path]
	if !ok {
		return 0, 0, 0, fmt.Errorf("parent group %q not found (create it first)", path)
	}
	return meta.headerAddr, meta.heapAddr, meta.btreeAddr, nil
}

// symbolTableHeader reads the header of the group at addr and requires a
// symbol table message and no link info message.
func (fw *FileWriter) symbolTableHeader(path string, addr uint64) (*core.ObjectHeader, error) {
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.sb)
	if err != nil {
		return nil, fmt.Errorf("group %q: read object header: %w", path, err)
	}
	hasStab := false
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgSymbolTable:
			hasStab = true
		case core.MsgLinkInfo:
			return nil, fmt.Errorf("%w: group %q has both a symbol table and a link info message", utils.ErrCorrupt, path)
		}
	}
	if !hasStab {
		return nil, fmt.Errorf("%w: group %q is registered with a symbol table its header does not have",
			utils.ErrCorrupt, path)
	}
	return oh, nil
}

// removeSymbolTableMessage rewrites the header at addr without its symbol
// table message (H5O_msg_remove).
func (fw *FileWriter) removeSymbolTableMessage(addr uint64) error {
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("re-read object header: %w", err)
	}
	kept := oh.Messages[:0:0]
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgSymbolTable {
			kept = append(kept, msg)
		}
	}
	oh.Messages = kept
	return fw.rewriteObjectHeader(addr, oh)
}

// forgetSymbolTable registers the group at path as one of the 1.8 format.
func (fw *FileWriter) forgetSymbolTable(path string) {
	if path == "" || path == "/" {
		fw.rootHeapAddr, fw.rootBTreeAddr, fw.rootStNodeAddr = 0, 0, 0
		fw.file.sb.RootBTreeAddr, fw.file.sb.RootHeapAddr = 0, 0
		return
	}
	meta := fw.groups[path]
	meta.heapAddr, meta.btreeAddr = 0, 0
}

// stabEntry is one entry of a symbol table as a link of the 1.8 format.
type stabEntry struct {
	name   string
	target newLinkTarget
}

// symbolTableContents returns the entries of the symbol table with the
// B-tree at btreeAddr and the local heap at heapAddr in name order (the
// order of the leaf symbol table nodes), and every block of file space the
// table owns, as H5G__stab_delete frees it (H5Gstab.c:200-245): its B-tree
// nodes, its symbol table nodes and its local heap, one block when the data
// segment follows the prefix and two otherwise (H5HL_delete, H5HL.c:847-900).
// The blocks are checked against the allocator without changing it.
func (fw *FileWriter) symbolTableContents(heapAddr, btreeAddr uint64) ([]stabEntry, []writer.FreeBlock, error) {
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("read local heap: %w", err)
	}
	g := fw.groupSymbolTable(heap)
	btreeNodes, snods, err := g.groupNodes(btreeAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("read group B-tree: %w", err)
	}

	var entries []stabEntry
	ranges := make([]writer.FreeBlock, 0, len(btreeNodes)+len(snods)+2)
	for _, addr := range btreeNodes {
		ranges = append(ranges, writer.FreeBlock{Offset: addr, Size: g.l.NodeSize()})
	}
	for _, addr := range snods {
		ranges = append(ranges, writer.FreeBlock{Offset: addr, Size: g.l.SNODSize()})
		if entries, err = g.appendSNODLinks(entries, addr); err != nil {
			return nil, nil, err
		}
	}

	if heap.Contiguous(heapAddr) {
		ranges = append(ranges, writer.FreeBlock{Offset: heapAddr, Size: heap.Size()})
	} else {
		ranges = append(ranges,
			writer.FreeBlock{Offset: heapAddr, Size: heap.PrefixSize()},
			writer.FreeBlock{Offset: heap.DataSegmentAddress, Size: heap.DataSegmentSize})
	}
	if err := fw.writer.Allocator().CheckFree(ranges); err != nil {
		return nil, nil, fmt.Errorf("%w: symbol table space: %w", utils.ErrCorrupt, err)
	}
	return entries, ranges, nil
}

// appendSNODLinks appends the entries of the symbol table node at addr to
// entries: a hard link to the entry's object, or a soft link to the target
// its scratch pad names in the local heap (H5G__ent_to_link, H5Gent.c).
func (g *groupSymbolTable) appendSNODLinks(entries []stabEntry, addr uint64) ([]stabEntry, error) {
	sn, err := g.readSNOD(addr)
	if err != nil {
		return nil, err
	}
	for _, ent := range sn.Entries {
		name, err := g.keyName(ent.LinkNameOffset)
		if err != nil {
			return nil, err
		}
		e := stabEntry{name: name, target: newLinkTarget{addr: ent.ObjectAddress}}
		if ent.CacheType == structures.CacheTypeSoftLink {
			if e.target.soft, err = g.keyName(uint64(ent.CachedSoftLinkOffset)); err != nil {
				return nil, err
			}
			if e.target.soft == "" {
				return nil, fmt.Errorf("%w: soft link %q has an empty target", utils.ErrCorrupt, name)
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}
