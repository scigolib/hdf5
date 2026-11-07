package hdf5

// Groups of the symbol table format past one B-tree node: a group B-tree
// node holds 2*sym_internal_k = 32 symbol table nodes of up to 2*sym_leaf_k
// = 8 entries, so a group of a few hundred names needs a root that split,
// and a few thousand a tree of three levels. The writer inserts, removes and
// looks up names as the C library does (H5B.c, H5Gnode.c). Every case is
// checked with this library and, in the TestCInterop_ tests, with h5ls -r
// listing exactly the expected names and h5dump reading the whole file.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/structures"
	"github.com/stretchr/testify/require"
)

// groupBTreeSizes are the group sizes of the matrix: around the root split
// (one node of 32 symbol table nodes, H5B.c:610) and a tree of three levels.
var groupBTreeSizes = []int{256, 257, 1000, 5000}

func groupBTreeCases() []heapGrowthCase {
	cases := make([]heapGrowthCase, 0, len(groupBTreeSizes)*2*2)
	for _, sb := range []struct {
		name string
		ver  uint8
	}{{"sbv0", SuperblockV0}, {"sbv2", SuperblockV2}} {
		for _, parent := range []string{"/", "/g"} {
			for _, n := range groupBTreeSizes {
				cases = append(cases, heapGrowthCase{
					name:   fmt.Sprintf("%s_%s_%d", sb.name, heapGrowthParentName(parent), n),
					sbVer:  sb.ver,
					parent: parent,
					n:      n,
				})
			}
		}
	}
	return cases
}

// h5lsPaths lists the object paths h5ls -r prints for file, without the root.
func h5lsPaths(t *testing.T, h5ls, file string) []string {
	t.Helper()
	out := runH5(t, h5ls, "-r", file)
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "/" {
			continue
		}
		paths = append(paths, fields[0])
	}
	sort.Strings(paths)
	return paths
}

// heapGrowthPaths is the sorted list of paths of datasets 0..n-1 under
// parent, with parent itself when it is not the root.
func heapGrowthPaths(parent string, n int) []string {
	paths := make([]string, 0, n+1)
	if parent != "/" {
		paths = append(paths, parent)
	}
	for i := range n {
		paths = append(paths, childPath(parent, heapGrowthChildName(i)))
	}
	sort.Strings(paths)
	return paths
}

// groupBTreeRootLevel returns the level of the root node of the B-tree of
// the group at parent, through OpenForWrite.
func groupBTreeRootLevel(t *testing.T, file, parent string) int {
	t.Helper()
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()
	btreeAddr := fw.rootBTreeAddr
	if parent != "/" {
		meta, ok := fw.groups[parent]
		require.True(t, ok, "group %q not loaded", parent)
		btreeAddr = meta.btreeAddr
	}
	node, err := fw.groupSymbolTable(nil).readNode(btreeAddr, structures.GroupBTreeLevelUnknown)
	require.NoError(t, err)
	return int(node.Level)
}

// TestGroupBTree_ManyChildren: a group of the symbol table format with up to
// 5000 names is read back whole, and its B-tree has the levels the C library
// would build. Red before the fix at 257 names: the single B-tree node held
// 33 children in the space of 32 and the file lost names.
func TestGroupBTree_ManyChildren(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 16 files with up to 5000 datasets each")
	}
	for _, tc := range groupBTreeCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeHeapGrowthFile(t, tc)
			requireHeapGrowthChildren(t, file, tc.parent, tc.n)
			level := groupBTreeRootLevel(t, file, tc.parent)
			switch {
			case tc.n >= 5000:
				require.Equal(t, 2, level, "B-tree of %d names", tc.n)
			default:
				require.Equal(t, 1, level, "B-tree of %d names", tc.n)
			}
		})
	}
}

// TestCInterop_GroupBTree_ManyChildren: h5ls -r lists exactly the names of
// every file of the matrix, and h5dump reads each whole.
func TestCInterop_GroupBTree_ManyChildren(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	h5dump := h5tool(t, "h5dump")
	for _, tc := range groupBTreeCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeHeapGrowthFile(t, tc)
			require.Equal(t, heapGrowthPaths(tc.parent, tc.n), h5lsPaths(t, h5ls, file))
			out := runH5(t, h5dump, "--enable-error-stack", "-H", file)
			require.Equal(t, tc.n, strings.Count(out, `DATASET "dataset_with_a_rather_long_name_number_`))
		})
	}
}

// writeNestedGroupsFile writes /g with 300 subgroups, each of the first
// three holding 50 datasets, and returns the file and the expected paths.
func writeNestedGroupsFile(t *testing.T, sbVer uint8) (string, []string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "nested.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sbVer))
	require.NoError(t, err)
	_, err = fw.CreateGroup("/g")
	require.NoError(t, err)
	want := []string{"/g"}
	for i := 0; i < 300; i++ {
		sub := fmt.Sprintf("/g/sub_%04d", i)
		_, err = fw.CreateGroup(sub)
		require.NoError(t, err, "create %s", sub)
		want = append(want, sub)
		if i < 3 {
			addHeapGrowthChildren(t, fw, sub, 0, 50)
			want = append(want, heapGrowthPaths(sub, 50)[1:]...)
		}
	}
	require.NoError(t, fw.Close())
	sort.Strings(want)
	return file, want
}

// walkPaths lists every object path of file below the root, read with this
// library.
func walkPaths(t *testing.T, file string) []string {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var got []string
	f.Walk(func(path string, _ Object) {
		if path != "/" {
			got = append(got, strings.TrimSuffix(path, "/"))
		}
	})
	sort.Strings(got)
	return got
}

// TestGroupBTree_NestedGroups: subgroups past a root split, with children of
// their own, are all read back.
func TestGroupBTree_NestedGroups(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sbv%d", sb), func(t *testing.T) {
			file, want := writeNestedGroupsFile(t, sb)
			require.Equal(t, want, walkPaths(t, file))
		})
	}
}

// TestCInterop_GroupBTree_NestedGroups: h5ls -r lists the same paths.
func TestCInterop_GroupBTree_NestedGroups(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sbv%d", sb), func(t *testing.T) {
			file, want := writeNestedGroupsFile(t, sb)
			require.Equal(t, want, h5lsPaths(t, h5ls, file))
		})
	}
}

// reopenPastThresholds writes 100 datasets into parent, then adds 900 more
// through OpenForWrite, past the root split, and a subgroup with 300 of its
// own (a group created before the reopen, loaded by OpenForWrite).
func reopenPastThresholds(t *testing.T, sbVer uint8, parent string) (string, []string) {
	t.Helper()
	file := writeHeapGrowthFile(t, heapGrowthCase{name: "reopen", sbVer: sbVer, parent: parent, n: 100})
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addHeapGrowthChildren(t, fw, parent, 100, 900)
	sub := childPath(parent, "sub")
	_, err = fw.CreateGroup(sub)
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addHeapGrowthChildren(t, fw, sub, 0, 300)
	require.NoError(t, fw.Close())

	want := append(heapGrowthPaths(parent, 1000), heapGrowthPaths(sub, 300)...)
	sort.Strings(want)
	return file, want
}

// TestGroupBTree_OpenForWrite: names added through OpenForWrite past the
// root split, and into a group of the file loaded by OpenForWrite, are all
// read back. Red before the fix: the B-tree overran at 257 names, and
// OpenForWrite knew no group but the root ("parent group not found").
func TestGroupBTree_OpenForWrite(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		for _, parent := range []string{"/", "/g"} {
			t.Run(fmt.Sprintf("sbv%d_%s", sb, heapGrowthParentName(parent)), func(t *testing.T) {
				file, want := reopenPastThresholds(t, sb, parent)
				require.Equal(t, want, walkPaths(t, file))
			})
		}
	}
}

// TestCInterop_GroupBTree_OpenForWrite: h5ls -r lists the same paths.
func TestCInterop_GroupBTree_OpenForWrite(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		for _, parent := range []string{"/", "/g"} {
			t.Run(fmt.Sprintf("sbv%d_%s", sb, heapGrowthParentName(parent)), func(t *testing.T) {
				file, want := reopenPastThresholds(t, sb, parent)
				require.Equal(t, want, h5lsPaths(t, h5ls, file))
			})
		}
	}
}

// deleteAfterSplit writes 1000 datasets into parent, then deletes the odd
// ones through OpenForWrite, then every other remaining one, and returns the
// file and the paths left.
func deleteAfterSplit(t *testing.T, sbVer uint8, parent string) (string, []string) {
	t.Helper()
	const n = 1000
	file := writeHeapGrowthFile(t, heapGrowthCase{name: "del", sbVer: sbVer, parent: parent, n: n})
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	kept := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		kept[i] = true
	}
	for i := 1; i < n; i += 2 {
		require.NoError(t, fw.Delete(childPath(parent, heapGrowthChildName(i))), "delete %d", i)
		kept[i] = false
	}
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	for i := 0; i < n; i += 4 {
		require.NoError(t, fw.Delete(childPath(parent, heapGrowthChildName(i))), "delete %d", i)
		kept[i] = false
	}
	require.NoError(t, fw.Close())

	var want []string
	if parent != "/" {
		want = append(want, parent)
	}
	for i := 0; i < n; i++ {
		if kept[i] {
			want = append(want, childPath(parent, heapGrowthChildName(i)))
		}
	}
	sort.Strings(want)
	return file, want
}

// TestGroupBTree_DeleteAfterSplit: names deleted from a group whose B-tree
// split leave exactly the others, in this library and after a reopen.
func TestGroupBTree_DeleteAfterSplit(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		for _, parent := range []string{"/", "/g"} {
			t.Run(fmt.Sprintf("sbv%d_%s", sb, heapGrowthParentName(parent)), func(t *testing.T) {
				file, want := deleteAfterSplit(t, sb, parent)
				require.Equal(t, want, walkPaths(t, file))
			})
		}
	}
}

// TestCInterop_GroupBTree_DeleteAfterSplit: h5ls -r lists exactly the names
// left, and h5dump reads the file.
func TestCInterop_GroupBTree_DeleteAfterSplit(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	h5dump := h5tool(t, "h5dump")
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		for _, parent := range []string{"/", "/g"} {
			t.Run(fmt.Sprintf("sbv%d_%s", sb, heapGrowthParentName(parent)), func(t *testing.T) {
				file, want := deleteAfterSplit(t, sb, parent)
				require.Equal(t, want, h5lsPaths(t, h5ls, file))
				runH5(t, h5dump, "--enable-error-stack", "-H", file)
			})
		}
	}
}

// TestGroupBTree_DeleteAll: deleting every name of a split group leaves an
// empty group (the root node back at level 0 with no children,
// H5B.c:1410-1416) that takes new names again.
func TestGroupBTree_DeleteAll(t *testing.T) {
	file := writeHeapGrowthFile(t, heapGrowthCase{name: "all", sbVer: SuperblockV0, parent: "/g", n: 300})
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	for i := 299; i >= 0; i-- {
		require.NoError(t, fw.Delete(childPath("/g", heapGrowthChildName(i))), "delete %d", i)
	}
	require.NoError(t, fw.Close())
	require.Equal(t, []string{"/g"}, walkPaths(t, file))
	require.Equal(t, 0, groupBTreeRootLevel(t, file, "/g"))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addHeapGrowthChildren(t, fw, "/g", 0, 20)
	require.NoError(t, fw.Close())
	requireHeapGrowthChildren(t, file, "/g", 20)
}

// emptyGroupRoundTrip creates the empty group /e, then (through
// OpenForWrite) its first child, then deletes the child again. It calls
// check after each step.
func emptyGroupRoundTrip(t *testing.T, sbVer uint8, check func(file string, want []string)) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "empty.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sbVer))
	require.NoError(t, err)
	_, err = fw.CreateGroup("/e")
	require.NoError(t, err)
	require.NoError(t, fw.Close())
	check(file, []string{"/e"})

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/e/x", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{7}))
	require.NoError(t, fw.Close())
	check(file, []string{"/e", "/e/x"})

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	require.NoError(t, fw.Delete("/e/x"))
	require.NoError(t, fw.Close())
	check(file, []string{"/e"})

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	require.NoError(t, fw.Delete("/e"))
	require.NoError(t, fw.Close())
	check(file, nil)
}

// TestGroupBTree_EmptyGroup: a new group has an empty B-tree and no symbol
// table node, as H5G__stab_create makes it (H5Gstab.c:132); its first insert
// creates the node (H5B.c:853), and the group can be emptied and deleted.
func TestGroupBTree_EmptyGroup(t *testing.T) {
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sbv%d", sb), func(t *testing.T) {
			emptyGroupRoundTrip(t, sb, func(file string, want []string) {
				require.Equal(t, want, walkPaths(t, file))
			})
		})
	}
}

// TestCInterop_GroupBTree_EmptyGroup: h5ls -r and h5dump read every step.
func TestCInterop_GroupBTree_EmptyGroup(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	h5dump := h5tool(t, "h5dump")
	for _, sb := range []uint8{SuperblockV0, SuperblockV2} {
		t.Run(fmt.Sprintf("sbv%d", sb), func(t *testing.T) {
			emptyGroupRoundTrip(t, sb, func(file string, want []string) {
				require.Equal(t, want, h5lsPaths(t, h5ls, file))
				runH5(t, h5dump, "--enable-error-stack", file)
			})
		})
	}
}

// TestGroupBTree_DuplicateName: a name already in the group is refused, as
// H5G__node_insert refuses it (H5Gnode.c:571-572), also past a split.
func TestGroupBTree_DuplicateName(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dup.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	addHeapGrowthChildren(t, fw, "/", 0, 300)
	_, err = fw.CreateDataset("/"+heapGrowthChildName(150), Int32, []uint64{1})
	require.ErrorIs(t, err, errGroupEntryExists)
	require.NoError(t, fw.Close())
	requireHeapGrowthChildren(t, file, "/", 300)
}

// Non-default K: sym_leaf_k and sym_internal_k of the superblock size every
// symbol table node and group B-tree node (H5Gnode.c:H5G_NODE_SIZE,
// H5B.c:H5B_SIZEOF_HDR and H5B_nodesize). No C tool on this machine writes a
// file with other K values (h5repack creates its output with a fresh file
// creation property list, h5repack_copy.c:88-140), so the test writes a
// version 0 file whose root holds one entry and lowers K in the superblock
// (no checksum in version 0, H5Fsuper_cache.c:476-487): every existing node
// fits the smaller sizes. Small K values build deep trees from few names.
const (
	smallSymLeafK     = 2 // symbol table nodes of 4 entries
	smallSymInternalK = 2 // group B-tree nodes of 4 children
)

// writeSmallKFile writes the version 0 file with /g and the lowered K, then
// adds 200 datasets to the root and to /g through OpenForWrite and deletes
// every third. It returns the file and the expected paths.
func writeSmallKFile(t *testing.T) (string, []string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "smallk.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	_, err = fw.CreateGroup("/g")
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	// Version 0 superblock: sym_leaf_k at byte 16, sym_internal_k at 18.
	osf, err := os.OpenFile(file, os.O_RDWR, 0)
	require.NoError(t, err)
	k := make([]byte, 4)
	binary.LittleEndian.PutUint16(k[0:], smallSymLeafK)
	binary.LittleEndian.PutUint16(k[2:], smallSymInternalK)
	_, err = osf.WriteAt(k, 16)
	require.NoError(t, err)
	require.NoError(t, osf.Close())

	const n = 200
	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addHeapGrowthChildren(t, fw, "/", 0, n)
	addHeapGrowthChildren(t, fw, "/g", 0, n)
	want := []string{"/g"}
	for i := 0; i < n; i++ {
		for _, parent := range []string{"/", "/g"} {
			p := childPath(parent, heapGrowthChildName(i))
			if i%3 == 0 {
				require.NoError(t, fw.Delete(p), "delete %s", p)
				continue
			}
			want = append(want, p)
		}
	}
	require.NoError(t, fw.Close())
	sort.Strings(want)
	return file, want
}

// TestGroupBTree_NonDefaultK: with sym_leaf_k = sym_internal_k = 2 the
// writer sizes and fills every node by the file's K, and builds the deep
// tree the C library would.
func TestGroupBTree_NonDefaultK(t *testing.T) {
	file, want := writeSmallKFile(t)
	require.Equal(t, want, walkPaths(t, file))
	require.GreaterOrEqual(t, groupBTreeRootLevel(t, file, "/g"), 3)
}

// TestCInterop_GroupBTree_NonDefaultK: the C library reads the K values
// back, lists exactly the names left and reads the whole file.
func TestCInterop_GroupBTree_NonDefaultK(t *testing.T) {
	h5ls := h5tool(t, "h5ls")
	h5dump := h5tool(t, "h5dump")
	file, want := writeSmallKFile(t)
	sb := runH5(t, h5dump, "-B", "-H", file)
	require.Contains(t, sb, fmt.Sprintf("BTREE_RANK %d", smallSymInternalK))
	require.Contains(t, sb, fmt.Sprintf("BTREE_LEAF %d", smallSymLeafK))
	require.Equal(t, want, h5lsPaths(t, h5ls, file))
	runH5(t, h5dump, "--enable-error-stack", file)
}

// TestGroupBTree_NodeCountLimits: K values whose full nodes overflow the
// 16-bit child count are refused before anything is read or written.
func TestGroupBTree_NodeCountLimits(t *testing.T) {
	base := structures.GroupBTreeLayout{SizeofAddr: 8, SizeofSize: 8, InternalK: 16, LeafK: 4, Order: binary.LittleEndian}

	ok := &groupSymbolTable{l: base}
	require.NoError(t, ok.checkNodeCounts())

	edge := base
	edge.InternalK, edge.LeafK = 32767, 32767 // the largest H5Pset_sym_k accepts for ik
	require.NoError(t, (&groupSymbolTable{l: edge}).checkNodeCounts())

	bigInternal := base
	bigInternal.InternalK = 32768
	g := &groupSymbolTable{l: bigInternal}
	require.ErrorIs(t, g.insert(0, "x", structures.SymbolTableEntry{}), ErrCorrupt)
	_, err := g.remove(0, "x")
	require.ErrorIs(t, err, ErrCorrupt)

	bigLeaf := base
	bigLeaf.LeafK = 32768
	require.ErrorIs(t, (&groupSymbolTable{l: bigLeaf}).insert(0, "x", structures.SymbolTableEntry{}), ErrNotSupported)
}
