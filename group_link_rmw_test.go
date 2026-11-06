package hdf5

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Groups of the 1.8 format in a file written by the C library: /c keeps its
// links as link messages in its header, /g in dense link storage
// (linkGroupsFile: h5repack -L with the default thresholds max_compact 8 and
// min_dense 6, H5Gprivate.h:74-75). These tests add and remove links across
// both thresholds as H5G_obj_insert and H5G_obj_remove do (H5Gobj.c:422-580,
// 817-975) and check the result with h5ls and h5dump.

// linkGroupAddr returns the header address of the 1.8-format group at path as
// OpenForWrite registered it.
func linkGroupAddr(t *testing.T, fw *FileWriter, path string) uint64 {
	t.Helper()
	addr, ok := fw.linkMessageGroup(path)
	require.True(t, ok, "%s is not registered as a group of the 1.8 format", path)
	return addr
}

// linkGroupState returns the names of the links of the group at addr and
// whether they are in dense storage.
func linkGroupState(t *testing.T, fw *FileWriter, addr uint64) (names []string, dense bool) {
	t.Helper()
	g, err := fw.loadLinkGroup(addr)
	require.NoError(t, err)
	links, err := fw.groupLinks(g)
	require.NoError(t, err)
	for _, lm := range links {
		names = append(names, lm.Name)
	}
	slices.Sort(names)
	return names, g.dense()
}

// h5lsGroup returns the sorted names h5ls -r lists directly below the group
// path of file.
func h5lsGroup(t *testing.T, h5ls, file, path string) []string {
	t.Helper()
	out := runH5(t, h5ls, "-r", file)
	prefix := path + "/"
	var names []string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], prefix) {
			continue
		}
		rest := strings.TrimPrefix(fields[0], prefix)
		if rest != "" && !strings.Contains(rest, "/") { // "" is the root's own line
			names = append(names, rest)
		}
	}
	slices.Sort(names)
	return names
}

// softNames are n soft link names s00, s01, ...
func softNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("s%02d", i)
	}
	return names
}

// sorted returns a sorted copy of names.
func sorted(names ...[]string) []string {
	out := slices.Concat(names...)
	slices.Sort(out)
	return out
}

// requireLinkGroupFile checks the group at path of the closed file with the C
// tools: h5ls lists exactly want, h5dump reads the whole file without error.
func requireLinkGroupFile(t *testing.T, h5ls, file, path string, want []string) {
	t.Helper()
	require.Equal(t, sorted(want), h5lsGroup(t, h5ls, file, path))
	requireH5dumpClean(t, file)
}

// TestLinkGroups_OpenForWriteRegistersBothForms: OpenForWrite registers the
// groups of the 1.8 format below a symbol-table root, compact and dense, so
// that children can be linked into them. Red before TASK-086:
// loadExistingGroups walked symbol tables only and registered neither.
func TestLinkGroups_OpenForWriteRegistersBothForms(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	file := linkGroupsFile(t, h5copy, h5repack, t.TempDir())

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	for path, wantDense := range map[string]bool{"/c": false, "/g": true} {
		meta, ok := fw.groups[path]
		require.True(t, ok, "%s not registered", path)
		require.False(t, meta.hasSymbolTable(), path)
		addr, err := fw.resolveObjectAddress(path)
		require.NoError(t, err)
		require.Equal(t, addr, meta.headerAddr, path)
		_, dense := linkGroupState(t, fw, addr)
		require.Equal(t, wantDense, dense, path)
	}
}

// TestLinkGroups_CompactAcrossThresholds: /c holds x and y as link
// messages. Six more links keep it compact (8 = max_compact, H5Gobj.c:474);
// the ninth moves all links into dense storage (H5Gobj.c:476-509). Removing
// down to six keeps the dense storage; the removal that leaves five
// (< min_dense) moves the links back into the header and frees the dense
// storage (H5Gobj.c:851-895).
func TestLinkGroups_CompactAcrossThresholds(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5ls := h5tool(t, "h5ls")
	file := linkGroupsFile(t, h5copy, h5repack, t.TempDir())
	base := []string{"x", "y"}
	soft := softNames(7)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr := linkGroupAddr(t, fw, "/c")
	xAddr, err := fw.lookupGroupLink(addr, "x")
	require.NoError(t, err)
	for i, name := range soft[:6] {
		require.NoError(t, fw.insertGroupLink(addr, name, 0, "/c/x"))
		names, dense := linkGroupState(t, fw, addr)
		require.False(t, dense, "dense after %d links", 3+i)
		require.Equal(t, sorted(base, soft[:i+1]), names)
	}
	require.NoError(t, fw.insertGroupLink(addr, soft[6], 0, "/c/x"))
	names, dense := linkGroupState(t, fw, addr)
	require.True(t, dense, "still compact with 9 links")
	require.Equal(t, sorted(base, soft), names)

	got, err := fw.lookupGroupLink(addr, "x")
	require.NoError(t, err)
	require.Equal(t, xAddr, got, "hard link target after the move to dense storage")
	got, err = fw.lookupGroupLink(addr, "s03")
	require.NoError(t, err)
	require.Equal(t, undefinedAddress, got, "soft link")
	_, err = fw.lookupGroupLink(addr, "nope")
	require.ErrorIs(t, err, errGroupEntryNotFound)
	require.ErrorIs(t, fw.insertGroupLink(addr, "s00", 0, "/c/y"), errGroupEntryExists)
	require.NoError(t, fw.Close())
	requireLinkGroupFile(t, h5ls, file, "/c", sorted(base, soft))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr = linkGroupAddr(t, fw, "/c")
	g, err := fw.loadLinkGroup(addr)
	require.NoError(t, err)
	storage, err := fw.denseLinkStorageRanges(g.linfo)
	require.NoError(t, err)

	for i := 6; i >= 4; i-- {
		removed, err := fw.removeGroupLink(addr, soft[i])
		require.NoError(t, err)
		require.Equal(t, undefinedAddress, removed)
		_, dense := linkGroupState(t, fw, addr)
		require.True(t, dense, "compact again with %d links", 2+i)
	}
	_, err = fw.removeGroupLink(addr, "s03")
	require.NoError(t, err)
	names, dense = linkGroupState(t, fw, addr)
	require.False(t, dense, "still dense with 5 links (min_dense 6)")
	require.Equal(t, sorted(base, soft[:3]), names)
	g, err = fw.loadLinkGroup(addr)
	require.NoError(t, err)
	require.False(t, g.linfo.HasNameBTree(), "name index address kept")
	require.Error(t, fw.writer.Allocator().CheckFree(storage), "the dense storage was not freed")

	removed, err := fw.removeGroupLink(addr, "x")
	require.NoError(t, err)
	require.Equal(t, xAddr, removed)
	_, err = fw.removeGroupLink(addr, "x")
	require.ErrorIs(t, err, errGroupEntryNotFound)
	require.NoError(t, fw.Close())
	requireLinkGroupFile(t, h5ls, file, "/c", sorted([]string{"y"}, soft[:3]))
}

// TestLinkGroups_DenseAcrossThresholds: /g holds 17 links in dense storage.
// A link added there stays dense (H5G__dense_insert); removing links keeps
// the dense storage down to six, moves the remaining five into the header
// (H5Gobj.c:851-895), and a group emptied and filled again stays compact.
func TestLinkGroups_DenseAcrossThresholds(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5ls := h5tool(t, "h5ls")
	file := linkGroupsFile(t, h5copy, h5repack, t.TempDir())
	children := make([]string, 17)
	for i := range children {
		children[i] = fmt.Sprintf("d%02d", i)
	}

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr := linkGroupAddr(t, fw, "/g")
	require.NoError(t, fw.insertGroupLink(addr, "s00", 0, "/g/d00"))
	names, dense := linkGroupState(t, fw, addr)
	require.True(t, dense)
	require.Equal(t, sorted(children, []string{"s00"}), names)
	require.NoError(t, fw.Close())
	requireLinkGroupFile(t, h5ls, file, "/g", sorted(children, []string{"s00"}))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr = linkGroupAddr(t, fw, "/g")
	removedAddrs := map[uint64]bool{}
	for _, name := range children[:12] {
		removed, err := fw.removeGroupLink(addr, name)
		require.NoError(t, err)
		require.NotEqual(t, undefinedAddress, removed, name)
		removedAddrs[removed] = true
	}
	require.Len(t, removedAddrs, 12, "each hard link names its own dataset")
	_, dense = linkGroupState(t, fw, addr)
	require.True(t, dense, "compact with 6 links")
	_, err = fw.removeGroupLink(addr, children[12])
	require.NoError(t, err)
	names, dense = linkGroupState(t, fw, addr)
	require.False(t, dense, "dense with 5 links")
	require.Equal(t, sorted(children[13:], []string{"s00"}), names)
	require.NoError(t, fw.Close())
	requireLinkGroupFile(t, h5ls, file, "/g", sorted(children[13:], []string{"s00"}))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr = linkGroupAddr(t, fw, "/g")
	for _, name := range append(slices.Clone(children[13:]), "s00") {
		_, err := fw.removeGroupLink(addr, name)
		require.NoError(t, err)
	}
	names, dense = linkGroupState(t, fw, addr)
	require.Empty(t, names)
	require.False(t, dense)
	require.NoError(t, fw.insertGroupLink(addr, "again", 0, "/keep"))
	require.NoError(t, fw.Close())
	requireLinkGroupFile(t, h5ls, file, "/g", []string{"again"})
}

// TestLinkGroups_DropDenseLinks: moving the links of a dense group back into
// its header writes them as link messages, sets the dense storage addresses
// of the link info message to undefined (H5G__dense_delete,
// H5Gdense.c:1630-1646) and frees the heap and the name index
// (H5Gobj.c:878-891). Here all 17 links of /g move, more than min_dense, to
// check the move on its own.
func TestLinkGroups_DropDenseLinks(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5ls := h5tool(t, "h5ls")
	file := linkGroupsFile(t, h5copy, h5repack, t.TempDir())

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr := linkGroupAddr(t, fw, "/g")
	g, err := fw.loadLinkGroup(addr)
	require.NoError(t, err)
	storage, err := fw.denseLinkStorageRanges(g.linfo)
	require.NoError(t, err)
	st, err := fw.loadDenseLinkStorage(g.linfo)
	require.NoError(t, err)
	msgs, err := st.linkMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 17)
	// All 17 links back into the header: the move H5Gobj.c:878-891 makes
	// below min_dense, here with more links than that.
	require.NoError(t, fw.dropDenseLinks(g, msgs))
	require.Error(t, fw.writer.Allocator().CheckFree(storage), "the dense storage was not freed")
	names, dense := linkGroupState(t, fw, addr)
	require.False(t, dense)
	require.Len(t, names, 17)
	require.NoError(t, fw.Close())

	want := make([]string, 17)
	for i := range want {
		want[i] = fmt.Sprintf("d%02d", i)
	}
	requireLinkGroupFile(t, h5ls, file, "/g", want)
}
