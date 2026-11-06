package hdf5

import (
	"fmt"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
)

// maxGroupLoadDepth bounds the nesting of groups loadExistingGroups follows.
// The visited set already ends every walk; the bound keeps a crafted chain
// of millions of nested groups from growing the goroutine stack without
// limit.
const maxGroupLoadDepth = 4096

// registerGroup records a group by path, so children can be linked into it.
// A group of the symbol table format has a local heap and a B-tree; a group
// of the 1.8 format is registered with both addresses 0
// (GroupMetadata.hasSymbolTable). CreateGroup and loadExistingGroups both
// register through it.
func (fw *FileWriter) registerGroup(path string, heapAddr, btreeAddr, headerAddr, headerAllocSz uint64) {
	fw.groups[path] = &GroupMetadata{
		heapAddr:      heapAddr,
		btreeAddr:     btreeAddr,
		headerAddr:    headerAddr,
		headerAllocSz: headerAllocSz,
	}
}

// symbolTableAddrs returns the B-tree and local heap addresses of a root
// group: btreeAddr and heapAddr when both are known (a version 0 superblock
// caches them), otherwise those of the symbol table message of oh, which
// holds two addresses of the file's offset size (H5O__stab_decode,
// H5Ostab.c). Without such a message they are returned unchanged.
func symbolTableAddrs(oh *core.ObjectHeader, sb *core.Superblock, btreeAddr, heapAddr uint64) (btree, heap uint64) {
	if btreeAddr != 0 && heapAddr != 0 {
		return btreeAddr, heapAddr
	}
	off := int(sb.OffsetSize)
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgSymbolTable && len(msg.Data) >= 2*off {
			return readAddressLE(msg.Data[:off]), readAddressLE(msg.Data[off : 2*off])
		}
	}
	return btreeAddr, heapAddr
}

// loadExistingGroups registers the groups an opened file already holds, so
// that OpenForWrite can add objects to them as CreateForWrite can to groups
// it created. It walks from the root through both group forms: symbol
// tables (H5Gstab.c) and, for groups of the 1.8 format, link messages in the
// header (H5Gcompact.c) or dense link storage (H5Gdense.c). An object reached
// by several hard links is registered under the first path found, and its
// children only once.
func (fw *FileWriter) loadExistingGroups() error {
	visited := map[uint64]struct{}{fw.rootGroupAddr: {}}
	if fw.rootHasSymbolTable() {
		return fw.loadGroupChildren("/", fw.rootHeapAddr, fw.rootBTreeAddr, visited, 0)
	}
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), fw.rootGroupAddr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("root group: %w", err)
	}
	return fw.loadLinkGroupChildren("/", fw.rootGroupAddr, oh, visited, 0)
}

// loadGroupChildren registers the groups linked from the symbol-table group
// at path and walks into each of them.
func (fw *FileWriter) loadGroupChildren(path string, heapAddr, btreeAddr uint64, visited map[uint64]struct{}, depth int) error {
	if depth >= maxGroupLoadDepth {
		return fmt.Errorf("group %q: groups nested deeper than %d: %w", path, maxGroupLoadDepth, utils.ErrCorrupt)
	}
	heap, err := fw.readLocalHeap(heapAddr)
	if err != nil {
		return fmt.Errorf("group %q: read local heap: %w", path, err)
	}
	g := fw.groupSymbolTable(heap)
	_, snods, err := g.groupNodes(btreeAddr)
	if err != nil {
		return fmt.Errorf("group %q: %w", path, err)
	}
	for _, snodAddr := range snods {
		sn, err := g.readSNOD(snodAddr)
		if err != nil {
			return fmt.Errorf("group %q: %w", path, err)
		}
		for _, ent := range sn.Entries {
			if ent.CacheType == structures.CacheTypeSoftLink {
				continue
			}
			name, err := g.keyName(ent.LinkNameOffset)
			if err != nil {
				return fmt.Errorf("group %q: %w", path, err)
			}
			if err := fw.loadGroupObject(path, name, ent.ObjectAddress, visited, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadLinkGroupChildren registers the groups that the hard links of the group
// of the 1.8 format at path (object header oh at addr) point at, and walks
// into each of them. Soft and external links name no object here.
func (fw *FileWriter) loadLinkGroupChildren(path string, addr uint64, oh *core.ObjectHeader,
	visited map[uint64]struct{}, depth int,
) error {
	if depth >= maxGroupLoadDepth {
		return fmt.Errorf("group %q: groups nested deeper than %d: %w", path, maxGroupLoadDepth, utils.ErrCorrupt)
	}
	g, err := fw.linkGroupOf(addr, oh)
	if err != nil {
		return fmt.Errorf("group %q: %w", path, err)
	}
	links, err := fw.groupLinks(g)
	if err != nil {
		return fmt.Errorf("group %q: %w", path, err)
	}
	for _, lm := range links {
		if lm.Type != core.LinkTypeHard {
			continue
		}
		target, err := linkTarget(lm, fw.file.sb)
		if err != nil {
			return fmt.Errorf("group %q: %w", path, err)
		}
		if err := fw.loadGroupObject(path, lm.Name, target, visited, depth); err != nil {
			return err
		}
	}
	return nil
}

// loadGroupObject registers the object a hard link named name of the group
// at parent points at when it is a group, of either form, and walks into it.
// A header with a link info message is a group of the 1.8 format even if it
// also has a symbol table message: the C library asks for the link info
// message first (H5G__obj_get_linfo, H5Gobj.c:297-343, before
// H5G__stab_* in H5G_obj_insert, H5Gobj.c:441-560).
func (fw *FileWriter) loadGroupObject(parent, name string, addr uint64, visited map[uint64]struct{}, depth int) error {
	sb := fw.file.sb
	if !addressDefined(addr, sb.OffsetSize) {
		return nil
	}
	if _, seen := visited[addr]; seen {
		return nil
	}
	visited[addr] = struct{}{}

	path := "/" + name
	if parent != "/" {
		path = parent + "/" + name
	}

	oh, err := core.ReadObjectHeader(fw.writer, addr, sb)
	if err != nil {
		return fmt.Errorf("object %q: %w", path, err)
	}
	var stab *core.HeaderMessage
	for _, msg := range oh.Messages {
		switch msg.Type {
		case core.MsgLinkInfo:
			allocSz, err := readChunk0Size(fw, addr)
			if err != nil {
				return fmt.Errorf("group %q: %w", path, err)
			}
			fw.registerGroup(path, 0, 0, addr, allocSz)
			return fw.loadLinkGroupChildren(path, addr, oh, visited, depth+1)
		case core.MsgSymbolTable:
			if stab == nil {
				stab = msg
			}
		}
	}
	if stab == nil {
		return nil
	}

	// The symbol table message: B-tree address, then heap address
	// (H5O__stab_decode, H5Ostab.c:90-120).
	off := int(sb.OffsetSize)
	if len(stab.Data) < 2*off {
		return fmt.Errorf("group %q: symbol table message of %d bytes: %w", path, len(stab.Data), utils.ErrCorrupt)
	}
	btreeAddr := readAddressLE(stab.Data[:off])
	heapAddr := readAddressLE(stab.Data[off : 2*off])
	allocSz, err := readChunk0Size(fw, addr)
	if err != nil {
		return fmt.Errorf("group %q: %w", path, err)
	}
	fw.registerGroup(path, heapAddr, btreeAddr, addr, allocSz)
	return fw.loadGroupChildren(path, heapAddr, btreeAddr, visited, depth+1)
}
