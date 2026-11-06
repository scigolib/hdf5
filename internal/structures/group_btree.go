package structures

// Version 1 B-tree nodes of type 0 (H5B_SNODE): the index of a group's symbol
// table. A node is "TREE", the node type, its level, the number of children,
// the left and right sibling addresses, then key 0, child 0, key 1, ...,
// child N-1, key N (H5B__cache_deserialize, H5Bcache.c:140-230). A group key
// is the offset of a name in the group's local heap, encoded as a length
// (sizeof_size bytes: H5G__node_sizeof_rkey, H5Gnode.c:120-130, and
// H5G__node_decode_key, H5Gnode.c:165); children are addresses. Nodes of
// level 0 point at symbol table nodes (SNOD), nodes above at nodes one level
// down (H5B__iterate_helper, H5B.c:1108-1160).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/utils"
)

// GroupBTreeSignature is the signature of a version 1 B-tree node (H5B_MAGIC).
const GroupBTreeSignature = "TREE"

// groupBTreeNodeType is H5B_SNODE_ID, the node type of a group B-tree.
const groupBTreeNodeType = 0

// GroupBTreeLevelUnknown asks ReadGroupBTreeNode not to check the node level
// (H5B_UNKNOWN_NODELEVEL, H5Bprivate.h).
const GroupBTreeLevelUnknown = -1

// GroupBTreeLayout holds the sizes of a file's group B-tree and symbol table
// nodes (H5B_shared_t for H5B_SNODE, H5B.c:1680-1730, and H5G_NODE_SIZE,
// H5Gpkg.h).
type GroupBTreeLayout struct {
	SizeofAddr int              // H5F_SIZEOF_ADDR
	SizeofSize int              // H5F_SIZEOF_SIZE: the width of a key
	InternalK  int              // btree_k[H5B_SNODE_ID]: a node holds up to 2K children
	LeafK      int              // sym_leaf_k: a symbol table node holds up to 2K entries
	Order      binary.ByteOrder // byte order of the fields
}

// NewGroupBTreeLayout returns the layout of sb's groups. A K of zero, which
// only a superblock built in memory can carry (the decoder rejects it), is
// the C default, as core.Superblock's encoder treats it.
func NewGroupBTreeLayout(sb *core.Superblock) GroupBTreeLayout {
	internalK, leafK := int(sb.SymInternalK), int(sb.SymLeafK)
	if internalK == 0 {
		internalK = core.DefaultSymInternalK
	}
	if leafK == 0 {
		leafK = core.DefaultSymLeafK
	}
	order := sb.Endianness
	if order == nil {
		order = binary.LittleEndian
	}
	return GroupBTreeLayout{
		SizeofAddr: int(sb.OffsetSize),
		SizeofSize: int(sb.LengthSize),
		InternalK:  internalK,
		LeafK:      leafK,
		Order:      order,
	}
}

// TwoK is the most children a group B-tree node holds (shared->two_k).
func (l GroupBTreeLayout) TwoK() int { return 2 * l.InternalK }

// headerSize is H5B_SIZEOF_HDR (H5Bpkg.h): signature, type, level, entries
// used and the two sibling addresses.
func (l GroupBTreeLayout) headerSize() int { return 8 + 2*l.SizeofAddr }

// NodeSize is the size of a group B-tree node in the file (shared->sizeof_rnode,
// H5B_shared_new, H5B.c:1700-1705): the header, 2K children and 2K+1 keys.
func (l GroupBTreeLayout) NodeSize() uint64 {
	return uint64(l.headerSize() + l.TwoK()*l.SizeofAddr + (l.TwoK()+1)*l.SizeofSize) //nolint:gosec // G115: small positive sizes
}

// SNODCapacity is the most entries a symbol table node holds (2*sym_leaf_k).
func (l GroupBTreeLayout) SNODCapacity() int { return 2 * l.LeafK }

// SNODEntrySize is H5G_SIZEOF_ENTRY (H5Gprivate.h:38-43).
func (l GroupBTreeLayout) SNODEntrySize() int { return l.SizeofSize + l.SizeofAddr + 4 + 4 + 16 }

// SNODSize is H5G_NODE_SIZE (H5Gpkg.h): the 8-byte header and 2*sym_leaf_k
// entries.
func (l GroupBTreeLayout) SNODSize() uint64 {
	return uint64(8 + l.SNODCapacity()*l.SNODEntrySize()) //nolint:gosec // G115: small positive sizes
}

// GroupBTreeNode is one node of a group B-tree, held as C holds it (H5B_t):
// key and child arrays of the full node capacity, of which the first N
// children and N+1 keys are in use.
type GroupBTreeNode struct {
	Level    uint8
	Left     uint64 // left sibling, UndefinedAddress when none
	Right    uint64 // right sibling, UndefinedAddress when none
	N        int    // children in use (nchildren)
	Keys     []uint64
	Children []uint64
}

// NewGroupBTreeNode returns an empty node of the given level with no
// siblings, as H5B_create makes one (H5B.c:180-240).
func NewGroupBTreeNode(l GroupBTreeLayout, level uint8) *GroupBTreeNode {
	return &GroupBTreeNode{
		Level:    level,
		Left:     undefinedAddress,
		Right:    undefinedAddress,
		Keys:     make([]uint64, l.TwoK()+1),
		Children: make([]uint64, l.TwoK()),
	}
}

// ReadGroupBTreeNode decodes the group B-tree node at addr as
// H5B__cache_deserialize does (H5Bcache.c:140-230): wrong signature, a node
// type other than a group's, a level other than expLevel (unless
// GroupBTreeLevelUnknown) and more than 2K children are corruption.
func ReadGroupBTreeNode(r io.ReaderAt, sb *core.Superblock, l GroupBTreeLayout, addr uint64, expLevel int) (*GroupBTreeNode, error) {
	hdrSize := uint64(l.headerSize()) //nolint:gosec // G115: small positive size
	hdr, err := core.ReadBlock(r, sb, addr, hdrSize, l.NodeSize(), "group B-tree node header")
	if err != nil {
		return nil, utils.WrapError("B-tree node header read failed", err)
	}
	if string(hdr[0:4]) != GroupBTreeSignature {
		return nil, fmt.Errorf("group B-tree node at 0x%x: invalid B-tree signature %q (expected TREE): %w",
			addr, hdr[0:4], utils.ErrCorrupt)
	}
	if hdr[4] != groupBTreeNodeType {
		return nil, fmt.Errorf("group B-tree node at 0x%x: expected group B-tree (type %d), got type %d: %w",
			addr, groupBTreeNodeType, hdr[4], utils.ErrCorrupt)
	}
	n := NewGroupBTreeNode(l, hdr[5])
	if expLevel != GroupBTreeLevelUnknown && int(n.Level) != expLevel {
		return nil, fmt.Errorf("group B-tree node at 0x%x: level %d, expected %d: %w",
			addr, n.Level, expLevel, utils.ErrCorrupt)
	}
	n.N = int(l.Order.Uint16(hdr[6:8]))
	if n.N > l.TwoK() {
		return nil, fmt.Errorf("group B-tree node at 0x%x: %d children, at most %d: %w",
			addr, n.N, l.TwoK(), utils.ErrCorrupt)
	}
	n.Left = readAddress(hdr[8:], l.SizeofAddr, l.Order)
	n.Right = readAddress(hdr[8+l.SizeofAddr:], l.SizeofAddr, l.Order)
	if n.N == 0 {
		return n, nil
	}

	bodySize := uint64(n.N*(l.SizeofSize+l.SizeofAddr) + l.SizeofSize) //nolint:gosec // G115: bounded by NodeSize
	body, err := core.ReadBlock(r, sb, addr+hdrSize, bodySize, l.NodeSize(), "group B-tree node keys")
	if err != nil {
		return nil, utils.WrapError("B-tree data read failed", err)
	}
	pos := 0
	for i := 0; i < n.N; i++ {
		n.Keys[i] = readAddressFromBytes(body[pos:], l.SizeofSize, l.Order)
		pos += l.SizeofSize
		n.Children[i] = readAddress(body[pos:], l.SizeofAddr, l.Order)
		pos += l.SizeofAddr
	}
	n.Keys[n.N] = readAddressFromBytes(body[pos:], l.SizeofSize, l.Order)
	return n, nil
}

// Encode returns the node image of NodeSize bytes as H5B__cache_serialize
// writes it (H5Bcache.c:265-320): keys at sizeof_size, children at
// sizeof_addr, the final key only when the node has children, and zeros in
// the unused rest.
func (n *GroupBTreeNode) Encode(l GroupBTreeLayout) []byte {
	buf := make([]byte, l.NodeSize())
	copy(buf, GroupBTreeSignature)
	buf[4] = groupBTreeNodeType
	buf[5] = n.Level
	l.Order.PutUint16(buf[6:8], uint16(n.N)) //nolint:gosec // G115: N <= 2K <= 2*MaxUint16 is checked by the callers
	writeAddr(buf[8:], n.Left, l.SizeofAddr, l.Order)
	writeAddr(buf[8+l.SizeofAddr:], n.Right, l.SizeofAddr, l.Order)
	pos := l.headerSize()
	for i := 0; i < n.N; i++ {
		writeAddr(buf[pos:], n.Keys[i], l.SizeofSize, l.Order)
		pos += l.SizeofSize
		writeAddr(buf[pos:], n.Children[i], l.SizeofAddr, l.Order)
		pos += l.SizeofAddr
	}
	if n.N > 0 {
		writeAddr(buf[pos:], n.Keys[n.N], l.SizeofSize, l.Order)
	}
	return buf
}

// WriteAt writes the node image at addr.
func (n *GroupBTreeNode) WriteAt(w io.WriterAt, l GroupBTreeLayout, addr uint64) error {
	if addr > uint64(1<<63-1) {
		return fmt.Errorf("group B-tree node address 0x%x exceeds the platform limit", addr)
	}
	if _, err := w.WriteAt(n.Encode(l), int64(addr)); err != nil {
		return fmt.Errorf("write group B-tree node at 0x%x: %w", addr, err)
	}
	return nil
}

// errGroupBTreeCycle reports a node reached twice in one walk.
var errGroupBTreeCycle = errors.New("group B-tree node reached twice")

// WalkGroupBTree calls fn with the address of every symbol table node of the
// group B-tree rooted at addr, left to right, as H5B__iterate_helper does
// (H5B.c:1108-1160): every child of a node of level L must be a node of level
// L-1, and a level-0 node's children are symbol table nodes. A node reached
// twice is corruption: in a tree each node has one parent, and a shared node
// would multiply the work of the walk.
func WalkGroupBTree(r io.ReaderAt, sb *core.Superblock, addr uint64, fn func(snodAddr uint64) error) error {
	l := NewGroupBTreeLayout(sb)
	visited := make(map[uint64]struct{})
	return walkGroupBTreeNode(r, sb, l, addr, GroupBTreeLevelUnknown, visited, fn)
}

func walkGroupBTreeNode(r io.ReaderAt, sb *core.Superblock, l GroupBTreeLayout, addr uint64, expLevel int,
	visited map[uint64]struct{}, fn func(uint64) error,
) error {
	if _, seen := visited[addr]; seen {
		return fmt.Errorf("group B-tree node at 0x%x: %w: %w", addr, errGroupBTreeCycle, utils.ErrCorrupt)
	}
	visited[addr] = struct{}{}

	node, err := ReadGroupBTreeNode(r, sb, l, addr, expLevel)
	if err != nil {
		return err
	}
	for i := 0; i < node.N; i++ {
		child := node.Children[i]
		if node.Level > 0 {
			if err := walkGroupBTreeNode(r, sb, l, child, int(node.Level)-1, visited, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(child); err != nil {
			return err
		}
	}
	return nil
}

// ReadGroupSNOD loads the symbol table node at addr for a group B-tree walk:
// a node that does not load, or holds more than 2*sym_leaf_k entries (more
// than the H5G_NODE_SIZE image C decodes it from, H5Gcache.c:155-187), is
// corruption of the group.
func ReadGroupSNOD(r io.ReaderAt, sb *core.Superblock, l GroupBTreeLayout, addr uint64) (*SymbolTableNode, error) {
	snod, err := ParseSymbolTableNode(r, addr, sb)
	if err != nil {
		if !errors.Is(err, utils.ErrCorrupt) {
			err = fmt.Errorf("%w: %w", utils.ErrCorrupt, err)
		}
		return nil, fmt.Errorf("symbol table node at 0x%x: %w", addr, err)
	}
	if int(snod.NumSymbols) > l.SNODCapacity() {
		return nil, fmt.Errorf("symbol table node at 0x%x: %d entries, at most %d: %w",
			addr, snod.NumSymbols, l.SNODCapacity(), utils.ErrCorrupt)
	}
	return snod, nil
}
