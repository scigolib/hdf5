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

// registerGroup records a group of the symbol table format by path, so
// children can be linked into it. CreateGroup and loadExistingGroups both
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

// loadExistingGroups registers the groups of the symbol table format that
// an opened file already holds, so that OpenForWrite can add objects to
// them as CreateForWrite can to groups it created. It walks the symbol
// tables from the root. An object reached by several hard links is
// registered under the first path found, and its children only once.
// Groups below a root of the link-message format are not walked: such a
// root has no symbol table to link into either.
func (fw *FileWriter) loadExistingGroups() error {
	if !fw.rootHasSymbolTable() {
		return nil
	}
	visited := map[uint64]struct{}{fw.rootGroupAddr: {}}
	return fw.loadGroupChildren("/", fw.rootHeapAddr, fw.rootBTreeAddr, visited, 0)
}

// loadGroupChildren registers the symbol-table groups linked from the group
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
			if err := fw.loadGroupEntry(path, g, ent, visited, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadGroupEntry registers the object of one symbol table entry of the
// group at parent when it is a group of the symbol table format, and walks
// into it.
func (fw *FileWriter) loadGroupEntry(parent string, g *groupSymbolTable, ent structures.SymbolTableEntry,
	visited map[uint64]struct{}, depth int,
) error {
	addr := ent.ObjectAddress
	if ent.CacheType == structures.CacheTypeSoftLink || addr == undefinedAddress {
		return nil
	}
	if _, seen := visited[addr]; seen {
		return nil
	}
	visited[addr] = struct{}{}

	name, err := g.keyName(ent.LinkNameOffset)
	if err != nil {
		return fmt.Errorf("group %q: %w", parent, err)
	}
	path := "/" + name
	if parent != "/" {
		path = parent + "/" + name
	}

	oh, err := core.ReadObjectHeader(fw.writer, addr, fw.file.sb)
	if err != nil {
		return fmt.Errorf("object %q: %w", path, err)
	}
	off := int(fw.file.sb.OffsetSize)
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgSymbolTable {
			continue
		}
		// The symbol table message: B-tree address, then heap address
		// (H5O__stab_decode, H5Ostab.c:90-120).
		if len(msg.Data) < 2*off {
			return fmt.Errorf("group %q: symbol table message of %d bytes: %w", path, len(msg.Data), utils.ErrCorrupt)
		}
		btreeAddr := readAddressLE(msg.Data[:off])
		heapAddr := readAddressLE(msg.Data[off : 2*off])
		allocSz, err := readChunk0Size(fw, addr)
		if err != nil {
			return fmt.Errorf("group %q: %w", path, err)
		}
		fw.registerGroup(path, heapAddr, btreeAddr, addr, allocSz)
		return fw.loadGroupChildren(path, heapAddr, btreeAddr, visited, depth+1)
	}
	return nil
}
