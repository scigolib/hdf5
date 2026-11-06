package hdf5

// The symbol table of a group in the original format: a version 1 B-tree of
// type 0 whose leaves point at symbol table nodes (SNOD), with names in the
// group's local heap. Insert, remove and lookup follow the C library
// operation by operation: H5B_insert, H5B__insert_helper, H5B__split and
// H5B__insert_child (H5B.c:420-1095), H5B__remove_helper (H5B.c:1200-1530)
// and H5B_find (H5B.c:300-400), with the group callbacks of H5Gnode.c
// (H5G__node_create, H5G__node_cmp3, H5G__node_found, H5G__node_insert,
// H5G__node_remove). Node sizes and K come from the file (the superblock), so
// a C-written file with other K values is changed in its own layout.

import (
	"errors"
	"fmt"
	"math"

	"github.com/scigolib/hdf5/internal/structures"
	"github.com/scigolib/hdf5/internal/utils"
)

// groupBTreeIns is H5B_ins_t: what a child operation asks its parent to do.
type groupBTreeIns int

const (
	groupInsNoop   groupBTreeIns = iota // H5B_INS_NOOP: nothing for the parent
	groupInsRight                       // H5B_INS_RIGHT: add a new child right of this one
	groupInsRemove                      // H5B_INS_REMOVE: drop this child
)

// btreeSplitRatios are the default split ratios of H5Pdxpl.c:65
// (H5D_XFER_BTREE_SPLIT_RATIO_DEF): left-most, middle and right-most node.
var btreeSplitRatios = [3]float64{0.1, 0.5, 0.9}

// errGroupEntryNotFound reports a name the group does not hold.
var errGroupEntryNotFound = errors.New("name not found in group")

// errGroupEntryExists reports a name the group already holds (H5Gnode.c:571-572).
var errGroupEntryExists = errors.New("symbol is already present in group")

// groupSymbolTable is one group's B-tree and local heap while an entry is
// inserted, removed or looked up.
type groupSymbolTable struct {
	fw   *FileWriter
	l    structures.GroupBTreeLayout
	heap *structures.LocalHeap
}

func (fw *FileWriter) groupSymbolTable(heap *structures.LocalHeap) *groupSymbolTable {
	return &groupSymbolTable{fw: fw, l: structures.NewGroupBTreeLayout(fw.file.sb), heap: heap}
}

// checkNodeCounts refuses K values whose full nodes could not record their
// child count in the 16-bit field of the node (H5Bcache.c:300,
// H5Gcache.c:265). The C library refuses such a sym_internal_k when the
// file is created (H5Pset_sym_k, H5Pfcpl.c:508-510), so a file that has
// one is corrupt; it accepts any sym_leaf_k, but a larger one than 32767
// cannot be written correctly by either library.
func (g *groupSymbolTable) checkNodeCounts() error {
	if g.l.TwoK() > math.MaxUint16 {
		return fmt.Errorf("group B-tree K %d: nodes of %d children: %w", g.l.InternalK, g.l.TwoK(), utils.ErrCorrupt)
	}
	if g.l.SNODCapacity() > math.MaxUint16 {
		return fmt.Errorf("symbol table leaf K %d: nodes of %d entries: %w", g.l.LeafK, g.l.SNODCapacity(), ErrNotSupported)
	}
	return nil
}

func (g *groupSymbolTable) readNode(addr uint64, expLevel int) (*structures.GroupBTreeNode, error) {
	node, err := structures.ReadGroupBTreeNode(g.fw.writer, g.fw.file.sb, g.l, addr, expLevel)
	if err != nil {
		return nil, fmt.Errorf("read group B-tree node: %w", err)
	}
	return node, nil
}

func (g *groupSymbolTable) writeNode(addr uint64, node *structures.GroupBTreeNode) error {
	if err := node.WriteAt(g.fw.writer, g.l, addr); err != nil {
		return fmt.Errorf("write group B-tree node: %w", err)
	}
	return nil
}

func (g *groupSymbolTable) readSNOD(addr uint64) (*structures.SymbolTableNode, error) {
	sn, err := structures.ReadGroupSNOD(g.fw.writer, g.fw.file.sb, g.l, addr)
	if err != nil {
		return nil, fmt.Errorf("read group symbol table node: %w", err)
	}
	return sn, nil
}

// writeSNOD writes entries as the symbol table node at addr, padded to
// H5G_NODE_SIZE (H5G__cache_node_serialize, H5Gcache.c:240-280).
func (g *groupSymbolTable) writeSNOD(addr uint64, entries []structures.SymbolTableEntry) error {
	sn := structures.NewSymbolTableNode(uint16(g.l.SNODCapacity())) //nolint:gosec // G115: checkNodeCounts bounds the capacity to 16 bits
	for _, e := range entries {
		if err := sn.AddEntry(e); err != nil {
			return fmt.Errorf("symbol table node at 0x%x: %w", addr, err)
		}
	}
	sb := g.fw.file.sb
	//nolint:gosec // G115: checkNodeCounts bounds the capacity to 16 bits
	if err := sn.WriteAt(g.fw.writer, addr, sb.OffsetSize, sb.LengthSize, uint16(g.l.SNODCapacity()), g.l.Order); err != nil {
		return fmt.Errorf("write symbol table node at 0x%x: %w", addr, err)
	}
	return nil
}

// newSNOD allocates and writes an empty symbol table node, as
// H5G__node_create does (H5Gnode.c:285-320).
func (g *groupSymbolTable) newSNOD() (uint64, error) {
	addr, err := g.fw.writer.Allocate(g.l.SNODSize())
	if err != nil {
		return 0, fmt.Errorf("allocate symbol table node: %w", err)
	}
	if err := g.writeSNOD(addr, nil); err != nil {
		return 0, err
	}
	return addr, nil
}

// keyName is the name a key or an entry points at in the local heap
// (H5HL_offset_into).
func (g *groupSymbolTable) keyName(offset uint64) (string, error) {
	s, err := g.heap.GetString(offset)
	if err != nil {
		return "", fmt.Errorf("group name at heap offset %d: %w: %w", offset, err, utils.ErrCorrupt)
	}
	return s, nil
}

// cmp3 is H5G__node_cmp3 (H5Gnode.c:397-430): -1 when name sorts at or
// before the left key, 1 when after the right key, 0 between them.
func (g *groupSymbolTable) cmp3(name string, ltKey, rtKey uint64) (int, error) {
	lt, err := g.keyName(ltKey)
	if err != nil {
		return 0, err
	}
	if name <= lt {
		return -1, nil
	}
	rt, err := g.keyName(rtKey)
	if err != nil {
		return 0, err
	}
	if name > rt {
		return 1, nil
	}
	return 0, nil
}

// searchNode is the binary search over a node's keys that H5B__insert_helper,
// H5B__remove_helper and H5B_find_helper share; cmp starts at -1 for insert
// and at 1 for the others (H5B.c:791, :1226, :330).
func (g *groupSymbolTable) searchNode(node *structures.GroupBTreeNode, name string, cmp int) (idx, outCmp int, err error) {
	lt, rt := 0, node.N
	for lt < rt && cmp != 0 {
		idx = (lt + rt) / 2
		if cmp, err = g.cmp3(name, node.Keys[idx], node.Keys[idx+1]); err != nil {
			return 0, 0, err
		}
		if cmp < 0 {
			rt = idx
		} else {
			lt = idx + 1
		}
	}
	return idx, cmp, nil
}

// searchSNOD finds name among a symbol table node's sorted entries. It
// returns the index of the match (cmp 0), or with the H5G__node_insert start
// values (idx -1, cmp 1) the last index probed and the last comparison.
func (g *groupSymbolTable) searchSNOD(entries []structures.SymbolTableEntry, name string) (idx, cmp int, err error) {
	idx, cmp = -1, 1
	lt, rt := 0, len(entries)
	for lt < rt && cmp != 0 {
		idx = (lt + rt) / 2
		s, err := g.keyName(entries[idx].LinkNameOffset)
		if err != nil {
			return 0, 0, err
		}
		switch {
		case name < s:
			cmp = -1
			rt = idx
		case name > s:
			cmp = 1
			lt = idx + 1
		default:
			cmp = 0
		}
	}
	return idx, cmp, nil
}

// lookup returns the entry named name, as H5B_find with H5G__node_found does
// (H5B.c:300-400, H5Gnode.c:451-500).
func (g *groupSymbolTable) lookup(rootAddr uint64, name string) (structures.SymbolTableEntry, error) {
	addr, expLevel := rootAddr, structures.GroupBTreeLevelUnknown
	// The level falls by one per step, so the walk ends within 256 nodes.
	for {
		node, err := g.readNode(addr, expLevel)
		if err != nil {
			return structures.SymbolTableEntry{}, err
		}
		idx, cmp, err := g.searchNode(node, name, 1)
		if err != nil {
			return structures.SymbolTableEntry{}, err
		}
		if cmp != 0 {
			return structures.SymbolTableEntry{}, errGroupEntryNotFound
		}
		if node.Level > 0 {
			addr, expLevel = node.Children[idx], int(node.Level)-1
			continue
		}
		sn, err := g.readSNOD(node.Children[idx])
		if err != nil {
			return structures.SymbolTableEntry{}, err
		}
		i, cmp, err := g.searchSNOD(sn.Entries, name)
		if err != nil {
			return structures.SymbolTableEntry{}, err
		}
		if cmp != 0 {
			return structures.SymbolTableEntry{}, errGroupEntryNotFound
		}
		return sn.Entries[i], nil
	}
}

// groupInsertState is the state H5B_insert threads through the recursion:
// the middle key and the left/right key change flags are shared by every
// level (H5B.c:553-680), while the left and right keys of a node are the
// keys of its parent around it.
type groupInsertState struct {
	name      string
	ent       structures.SymbolTableEntry
	mdKey     uint64
	ltChanged bool
	rtChanged bool
}

// insert adds ent (whose LinkNameOffset already holds name in the heap) to
// the B-tree at rootAddr, as H5B_insert does (H5B.c:553-680). When the root
// splits, the old root is copied to a new address and the new root is
// written at rootAddr, so the B-tree address in the group's symbol table
// message never changes.
func (g *groupSymbolTable) insert(rootAddr uint64, name string, ent structures.SymbolTableEntry) error {
	if err := g.checkNodeCounts(); err != nil {
		return err
	}
	root, err := g.readNode(rootAddr, structures.GroupBTreeLevelUnknown)
	if err != nil {
		return err
	}
	st := &groupInsertState{name: name, ent: ent}
	var ltKey, rtKey uint64
	ins, splitAddr, split, err := g.insertHelper(rootAddr, root, &ltKey, &rtKey, st)
	if err != nil {
		return err
	}
	if ins == groupInsNoop {
		return g.writeNode(rootAddr, root)
	}

	// The root split (H5B.c:610-675).
	if root.Level == ^uint8(0) {
		return fmt.Errorf("group B-tree at 0x%x: root level overflow: %w", rootAddr, utils.ErrCorrupt)
	}
	if !st.ltChanged {
		ltKey = root.Keys[0]
	}
	if !st.rtChanged {
		rtKey = split.Keys[split.N]
	}
	oldRootAddr, err := g.fw.writer.Allocate(g.l.NodeSize())
	if err != nil {
		return fmt.Errorf("allocate group B-tree node: %w", err)
	}
	split.Left = oldRootAddr
	if err := g.writeNode(oldRootAddr, root); err != nil {
		return err
	}
	if err := g.writeNode(splitAddr, split); err != nil {
		return err
	}
	newRoot := structures.NewGroupBTreeNode(g.l, root.Level+1)
	newRoot.N = 2
	newRoot.Children[0] = oldRootAddr
	newRoot.Keys[0] = ltKey
	newRoot.Children[1] = splitAddr
	newRoot.Keys[1] = st.mdKey
	newRoot.Keys[2] = rtKey
	return g.writeNode(rootAddr, newRoot)
}

// insertHelper is H5B__insert_helper (H5B.c:783-1095) for the node bt at
// addr. ltKey and rtKey are the keys around bt in its parent. It changes bt
// in memory; the caller writes bt, and the node bt split into when the
// result is groupInsRight (returned with its address).
//
//nolint:gocognit,gocyclo,cyclop,nestif // A line-by-line port of the C function; splitting it hides the correspondence.
func (g *groupSymbolTable) insertHelper(addr uint64, bt *structures.GroupBTreeNode, ltKey, rtKey *uint64,
	st *groupInsertState,
) (groupBTreeIns, uint64, *structures.GroupBTreeNode, error) {
	st.ltChanged, st.rtChanged = false, false

	idx, cmp, err := g.searchNode(bt, st.name, -1)
	if err != nil {
		return 0, 0, nil, err
	}

	var myIns groupBTreeIns
	var newChildAddr uint64
	switch {
	case bt.N == 0:
		// The only value in the tree (H5B.c:853-872): a first symbol table
		// node with both keys at the empty name (H5G__node_create,
		// H5Gnode.c:306-309), then the group's insert (follow_min).
		if bt.Level != 0 {
			return 0, 0, nil, fmt.Errorf("group B-tree node at 0x%x: level %d with no children: %w",
				addr, bt.Level, utils.ErrCorrupt)
		}
		snod, err := g.newSNOD()
		if err != nil {
			return 0, 0, nil, err
		}
		bt.Children[0], bt.Keys[0], bt.Keys[1] = snod, 0, 0
		bt.N = 1
		idx = 0
		myIns, newChildAddr, err = g.nodeInsert(bt.Children[0], &bt.Keys[1], st)
		if err != nil {
			return 0, 0, nil, err
		}
	case cmp < 0 && idx == 0, cmp > 0 && idx+1 >= bt.N, cmp == 0:
		// Below the left-most key: the minimum branch (H5B.c:873-910);
		// above the right-most key: the maximum branch (H5B.c:911-952); or
		// the branch whose keys hold the name (H5B.c:958-975). Groups
		// follow both the minimum and the maximum branch (follow_min,
		// follow_max: H5B_SNODE class, H5Gnode.c:75-90).
		if cmp > 0 {
			idx = bt.N - 1
		}
		if bt.Level > 0 {
			childAddr := bt.Children[idx]
			child, err := g.readNode(childAddr, int(bt.Level)-1)
			if err != nil {
				return 0, 0, nil, err
			}
			ins, splitAddr, split, err := g.insertHelper(childAddr, child, &bt.Keys[idx], &bt.Keys[idx+1], st)
			if err != nil {
				return 0, 0, nil, err
			}
			if err := g.writeNode(childAddr, child); err != nil {
				return 0, 0, nil, err
			}
			if split != nil {
				if err := g.writeNode(splitAddr, split); err != nil {
					return 0, 0, nil, err
				}
			}
			myIns, newChildAddr = ins, splitAddr
		} else {
			myIns, newChildAddr, err = g.nodeInsert(bt.Children[idx], &bt.Keys[idx+1], st)
			if err != nil {
				return 0, 0, nil, err
			}
		}
	default:
		return 0, 0, nil, fmt.Errorf("group B-tree node at 0x%x: could not determine which branch to follow: %w",
			addr, utils.ErrCorrupt)
	}

	// Update the left and right keys of the current node (H5B.c:1010-1027).
	if st.ltChanged {
		if idx > 0 {
			st.ltChanged = false
		} else {
			*ltKey = bt.Keys[idx]
		}
	}
	if st.rtChanged {
		if idx+1 < bt.N {
			st.rtChanged = false
		} else {
			*rtKey = bt.Keys[idx+1]
		}
	}

	// A new child right of child idx (H5B.c:1036-1068). Groups never add on
	// the left: H5G__node_insert returns only NOOP or RIGHT.
	var splitAddr uint64
	var split *structures.GroupBTreeNode
	if myIns == groupInsRight {
		target := bt
		if bt.N == g.l.TwoK() {
			if splitAddr, split, err = g.split(addr, bt, idx); err != nil {
				return 0, 0, nil, err
			}
			if idx >= bt.N {
				idx -= bt.N
				target = split
			}
		}
		insertGroupBTreeChild(target, idx, newChildAddr, st.mdKey)
	}

	// If this node split, hand the shared key up (H5B.c:1074-1081).
	if split != nil {
		st.mdKey = split.Keys[0]
		return groupInsRight, splitAddr, split, nil
	}
	return groupInsNoop, 0, nil, nil
}

// insertGroupBTreeChild is H5B__insert_child (H5B.c:698-750) for an anchor
// on the right: mdKey becomes key idx+1 and child becomes child idx+1.
func insertGroupBTreeChild(bt *structures.GroupBTreeNode, idx int, child, mdKey uint64) {
	copy(bt.Keys[idx+2:bt.N+2], bt.Keys[idx+1:bt.N+1])
	bt.Keys[idx+1] = mdKey
	idx++
	copy(bt.Children[idx+1:bt.N+1], bt.Children[idx:bt.N])
	bt.Children[idx] = child
	bt.N++
}

// split is H5B__split (H5B.c:424-550): it moves the right part of the full
// node bt to a new node of the same level and links the new node in as bt's
// right sibling. The new node is returned for the caller to write; a right
// sibling of bt is updated here.
func (g *groupSymbolTable) split(addr uint64, bt *structures.GroupBTreeNode, idx int) (uint64, *structures.GroupBTreeNode, error) {
	twoK := g.l.TwoK()
	var nleft int
	switch {
	case bt.Right == undefinedAddress:
		nleft = int(float64(twoK) * btreeSplitRatios[2])
	case bt.Left == undefinedAddress:
		nleft = int(float64(twoK) * btreeSplitRatios[0])
	default:
		nleft = int(float64(twoK) * btreeSplitRatios[1])
	}
	// Keep the new child in the same node as the child that split.
	if idx < nleft && nleft == twoK {
		nleft--
	} else if idx >= nleft && nleft == 0 {
		nleft++
	}
	nright := twoK - nleft

	newAddr, err := g.fw.writer.Allocate(g.l.NodeSize())
	if err != nil {
		return 0, nil, fmt.Errorf("allocate group B-tree node: %w", err)
	}
	nn := structures.NewGroupBTreeNode(g.l, bt.Level)
	copy(nn.Keys, bt.Keys[nleft:nleft+nright+1])
	copy(nn.Children, bt.Children[nleft:nleft+nright])
	nn.N = nright
	bt.N = nleft

	nn.Left = addr
	nn.Right = bt.Right
	if bt.Right != undefinedAddress {
		right, err := g.readNode(bt.Right, int(bt.Level))
		if err != nil {
			return 0, nil, err
		}
		right.Left = newAddr
		if err := g.writeNode(bt.Right, right); err != nil {
			return 0, nil, err
		}
	}
	bt.Right = newAddr
	return newAddr, nn, nil
}

// nodeInsert is H5G__node_insert (H5Gnode.c:540-690): it adds st.ent to the
// symbol table node at addr, splitting a full node into two of sym_leaf_k
// entries each. rtKey is the key right of the node in its B-tree parent.
func (g *groupSymbolTable) nodeInsert(addr uint64, rtKey *uint64, st *groupInsertState) (groupBTreeIns, uint64, error) {
	sn, err := g.readSNOD(addr)
	if err != nil {
		return 0, 0, err
	}
	entries := sn.Entries
	idx, cmp, err := g.searchSNOD(entries, st.name)
	if err != nil {
		return 0, 0, err
	}
	if cmp == 0 {
		return 0, 0, fmt.Errorf("%q: %w", st.name, errGroupEntryExists)
	}
	if cmp > 0 {
		idx++
	}

	k := g.l.LeafK
	if len(entries) < 2*k {
		if idx == len(entries) {
			*rtKey = st.ent.LinkNameOffset
			st.rtChanged = true
		}
		return groupInsNoop, 0, g.writeSNOD(addr, insertSymbolEntry(entries, idx, st.ent))
	}

	// The node is full: the right half moves to a new node at a new address
	// and the left half stays at addr.
	rightAddr, err := g.newSNOD()
	if err != nil {
		return 0, 0, err
	}
	left := append([]structures.SymbolTableEntry(nil), entries[:k]...)
	right := append([]structures.SymbolTableEntry(nil), entries[k:2*k]...)
	st.mdKey = left[k-1].LinkNameOffset
	if idx <= k {
		if idx == k {
			st.mdKey = st.ent.LinkNameOffset
		}
		left = insertSymbolEntry(left, idx, st.ent)
	} else {
		idx -= k
		if idx == k {
			*rtKey = st.ent.LinkNameOffset
			st.rtChanged = true
		}
		right = insertSymbolEntry(right, idx, st.ent)
	}
	if err := g.writeSNOD(addr, left); err != nil {
		return 0, 0, err
	}
	if err := g.writeSNOD(rightAddr, right); err != nil {
		return 0, 0, err
	}
	return groupInsRight, rightAddr, nil
}

// insertSymbolEntry returns entries with e inserted at idx.
func insertSymbolEntry(entries []structures.SymbolTableEntry, idx int, e structures.SymbolTableEntry) []structures.SymbolTableEntry {
	out := make([]structures.SymbolTableEntry, 0, len(entries)+1)
	out = append(out, entries[:idx]...)
	out = append(out, e)
	return append(out, entries[idx:]...)
}

// groupRemoveState is the state H5B_remove threads through the recursion.
type groupRemoveState struct {
	name      string
	removed   structures.SymbolTableEntry
	ltChanged bool
	rtChanged bool
}

// remove deletes the entry named name from the B-tree at rootAddr and frees
// its names in the heap, as H5B_remove with H5G__node_remove does
// (H5B.c:1542-1570, H5Gnode.c:697-890). A symbol table node that becomes
// empty is freed and dropped from its parent; a B-tree node left with no
// children is freed and unlinked from its siblings; a root left with no
// children becomes an empty leaf. It returns the removed entry.
func (g *groupSymbolTable) remove(rootAddr uint64, name string) (structures.SymbolTableEntry, error) {
	if err := g.checkNodeCounts(); err != nil {
		return structures.SymbolTableEntry{}, err
	}
	st := &groupRemoveState{name: name}
	var ltKey, rtKey uint64
	if _, err := g.removeHelper(rootAddr, structures.GroupBTreeLevelUnknown, 0, &ltKey, &rtKey, st); err != nil {
		return structures.SymbolTableEntry{}, err
	}
	return st.removed, nil
}

// removeHelper is H5B__remove_helper (H5B.c:1200-1530) for the group B-tree
// (critical key on the right: H5B_SNODE). depth is 0 at the root.
//
//nolint:gocognit,gocyclo,cyclop,funlen,nestif // A line-by-line port of the C function; splitting it hides the correspondence.
func (g *groupSymbolTable) removeHelper(addr uint64, expLevel, depth int, ltKey, rtKey *uint64,
	st *groupRemoveState,
) (groupBTreeIns, error) {
	bt, err := g.readNode(addr, expLevel)
	if err != nil {
		return 0, err
	}
	idx, cmp, err := g.searchNode(bt, st.name, 1)
	if err != nil {
		return 0, err
	}
	if cmp != 0 {
		return 0, fmt.Errorf("%q: %w", st.name, errGroupEntryNotFound)
	}

	var ret groupBTreeIns
	if bt.Level > 0 {
		ret, err = g.removeHelper(bt.Children[idx], int(bt.Level)-1, depth+1, &bt.Keys[idx], &bt.Keys[idx+1], st)
	} else {
		ret, err = g.nodeRemove(bt.Children[idx], &bt.Keys[idx+1], st)
	}
	if err != nil {
		return 0, err
	}

	if st.ltChanged {
		if idx > 0 {
			st.ltChanged = false
		} else {
			*ltKey = bt.Keys[idx]
		}
	}
	if st.rtChanged {
		if idx+1 < bt.N {
			st.rtChanged = false
		} else {
			*rtKey = bt.Keys[idx+1]
		}
	}

	if ret == groupInsRemove {
		switch {
		case bt.N == 1 && depth > 0:
			// The node's only child is gone: unlink the node from its
			// siblings, the right one taking its left key, and free it
			// (H5B.c:1352-1408).
			if bt.Left != undefinedAddress {
				left, err := g.readNode(bt.Left, int(bt.Level))
				if err != nil {
					return 0, err
				}
				left.Right = bt.Right
				if err := g.writeNode(bt.Left, left); err != nil {
					return 0, err
				}
			}
			if bt.Right != undefinedAddress {
				right, err := g.readNode(bt.Right, int(bt.Level))
				if err != nil {
					return 0, err
				}
				right.Keys[0] = bt.Keys[0]
				right.Left = bt.Left
				if err := g.writeNode(bt.Right, right); err != nil {
					return 0, err
				}
			}
			if err := g.fw.writer.Free(addr, g.l.NodeSize()); err != nil {
				return 0, fmt.Errorf("free group B-tree node at 0x%x: %w", addr, err)
			}
			return groupInsRemove, nil
		case bt.N == 1:
			// The root's last child is gone (H5B.c:1410-1416).
			bt.N = 0
			bt.Level = 0
		case idx == 0:
			// Keep the left-most key; the right key of the removed child
			// goes (H5B.c:1418-1441).
			copy(bt.Keys[1:bt.N], bt.Keys[2:bt.N+1])
			copy(bt.Children[0:bt.N-1], bt.Children[1:bt.N])
			bt.N--
			ret = groupInsNoop
		case idx+1 == bt.N:
			// The right-most child: the node's right key is now the left
			// key of that child (H5B.c:1442-1462).
			*rtKey = bt.Keys[bt.N-1]
			st.rtChanged = true
			bt.N--
			ret = groupInsNoop
		default:
			copy(bt.Keys[idx+1:bt.N], bt.Keys[idx+2:bt.N+1])
			copy(bt.Children[idx:bt.N-1], bt.Children[idx+1:bt.N])
			bt.N--
			ret = groupInsNoop
		}
	} else {
		ret = groupInsNoop
	}

	// A changed edge key is shared with the sibling on that side
	// (H5B.c:1478-1505).
	switch {
	case st.ltChanged && bt.Left != undefinedAddress:
		left, err := g.readNode(bt.Left, int(bt.Level))
		if err != nil {
			return 0, err
		}
		left.Keys[left.N] = bt.Keys[0]
		if err := g.writeNode(bt.Left, left); err != nil {
			return 0, err
		}
	case st.rtChanged && bt.Right != undefinedAddress:
		right, err := g.readNode(bt.Right, int(bt.Level))
		if err != nil {
			return 0, err
		}
		right.Keys[0] = bt.Keys[bt.N]
		if err := g.writeNode(bt.Right, right); err != nil {
			return 0, err
		}
	}

	if err := g.writeNode(addr, bt); err != nil {
		return 0, err
	}
	return ret, nil
}

// nodeRemove is H5G__node_remove for one name (H5Gnode.c:697-860): it frees
// the link name, and a soft link's value, in the local heap (H5HL_remove),
// drops the entry, and frees a node left empty. rtKey is the key right of
// the node in its B-tree parent.
func (g *groupSymbolTable) nodeRemove(addr uint64, rtKey *uint64, st *groupRemoveState) (groupBTreeIns, error) {
	sn, err := g.readSNOD(addr)
	if err != nil {
		return 0, err
	}
	entries := sn.Entries
	idx, cmp, err := g.searchSNOD(entries, st.name)
	if err != nil {
		return 0, err
	}
	if cmp != 0 || idx < 0 {
		return 0, fmt.Errorf("%q: %w", st.name, errGroupEntryNotFound)
	}
	ent := entries[idx]
	st.removed = ent

	if ent.CacheType == structures.CacheTypeSoftLink {
		if err := g.removeHeapString(uint64(ent.CachedSoftLinkOffset)); err != nil {
			return 0, fmt.Errorf("remove soft link value from local heap: %w", err)
		}
	}
	if err := g.removeHeapString(ent.LinkNameOffset); err != nil {
		return 0, fmt.Errorf("remove link name from local heap: %w", err)
	}

	switch {
	case len(entries) == 1:
		if err := g.fw.writer.Free(addr, g.l.SNODSize()); err != nil {
			return 0, fmt.Errorf("free symbol table node at 0x%x: %w", addr, err)
		}
		return groupInsRemove, nil
	case idx+1 == len(entries):
		*rtKey = entries[idx-1].LinkNameOffset
		st.rtChanged = true
	}
	rest := append(append([]structures.SymbolTableEntry(nil), entries[:idx]...), entries[idx+1:]...)
	return groupInsNoop, g.writeSNOD(addr, rest)
}

// removeHeapString frees the string at offset and its terminator
// (H5Gnode.c:760-764, :804-807).
func (g *groupSymbolTable) removeHeapString(offset uint64) error {
	s, err := g.keyName(offset)
	if err != nil {
		return err
	}
	if err := g.heap.Remove(offset, uint64(len(s))+1); err != nil {
		return fmt.Errorf("free heap string at %d: %w", offset, err)
	}
	return nil
}

// groupNodes returns the addresses of the B-tree nodes and the symbol
// table nodes of the group B-tree at rootAddr, for freeing a whole group
// (H5B_delete, H5B.c:1590-1650).
func (g *groupSymbolTable) groupNodes(rootAddr uint64) (btreeNodes, snods []uint64, err error) {
	visited := make(map[uint64]struct{})
	var walk func(addr uint64, expLevel int) error
	walk = func(addr uint64, expLevel int) error {
		if _, seen := visited[addr]; seen {
			return fmt.Errorf("group B-tree node at 0x%x reached twice: %w", addr, utils.ErrCorrupt)
		}
		visited[addr] = struct{}{}
		node, err := g.readNode(addr, expLevel)
		if err != nil {
			return err
		}
		btreeNodes = append(btreeNodes, addr)
		for i := 0; i < node.N; i++ {
			if node.Level == 0 {
				snods = append(snods, node.Children[i])
				continue
			}
			if err := walk(node.Children[i], int(node.Level)-1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootAddr, structures.GroupBTreeLevelUnknown); err != nil {
		return nil, nil, err
	}
	return btreeNodes, snods, nil
}

// createEmptyGroupBTree allocates and writes the B-tree of a new group as
// H5G__stab_create does through H5B_create (H5Gstab.c:120-135,
// H5B.c:180-240): an empty leaf root, sized from the file's K and field
// sizes. The first insert creates the first symbol table node.
func (fw *FileWriter) createEmptyGroupBTree() (uint64, error) {
	l := structures.NewGroupBTreeLayout(fw.file.sb)
	addr, err := fw.writer.Allocate(l.NodeSize())
	if err != nil {
		return 0, fmt.Errorf("allocate group B-tree: %w", err)
	}
	if err := structures.NewGroupBTreeNode(l, 0).WriteAt(fw.writer, l, addr); err != nil {
		return 0, fmt.Errorf("write group B-tree: %w", err)
	}
	return addr, nil
}
