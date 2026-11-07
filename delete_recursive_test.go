package hdf5

// Deleting a group that still has links deletes its children as the C
// library does (TASK-085): H5Ldelete removes the link and H5O_link(-1)
// lowers the object's reference count; at zero H5O_delete frees the object
// and, for a group, lowers the count of every object its hard links name
// (H5G__node_remove for a symbol table, H5Gnode.c:774-784; the link
// messages' delete callback H5O_link_delete for compact links;
// H5G__dense_delete with adj_link for dense storage, H5Gdense.c:1581-1650).
// An object that keeps another link survives with its count lowered.

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// objectSpace is what h5stat counts for the groups and datasets of a file:
// the bytes a delete of a whole tree must free, measured before and after.
func objectSpace(s statSpace) uint64 {
	return s.groupSpace() + s.datasetSpace()
}

// TestCInterop_DeleteGroupTree: deleting a group with children frees exactly
// what h5stat counts for the group and everything below it, for each of the
// three forms a group keeps its links in (a symbol table, link messages in
// its header, dense link storage), and leaves a file h5dump reads with the
// rest of the objects in place. Red before the change: the delete was
// refused with "non-empty group".
func TestCInterop_DeleteGroupTree(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5stat := h5tool(t, "h5stat")
	h5dump := h5tool(t, "h5dump")

	linkFile := linkGroupsFile(t, h5copy, h5repack, t.TempDir())
	for _, tc := range []struct {
		name, file, path string
		gone             []string
		kept             []string
	}{
		{"symbol table", nestedGroupFile(t), "/parent",
			[]string{"/parent", "/parent/child", "/parent/sub", "/parent/sub/leaf"}, []string{"/other"}},
		{"link messages", linkFile, "/c", []string{"/c", "/c/x", "/c/y"}, []string{"/g", "/keep"}},
		{"dense links", linkFile, "/g", []string{"/g", "/g/d00", "/g/d16"}, []string{"/keep"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := objectSpace(h5statSpace(t, h5stat, tc.file))
			freed := deleteFreedBytes(t, tc.file, tc.path)
			after := objectSpace(h5statSpace(t, h5stat, tc.file))
			require.Equal(t, before-after, freed, "bytes freed vs h5stat before and after")

			paths := walkPaths(t, tc.file)
			for _, p := range tc.gone {
				require.NotContains(t, paths, p)
			}
			for _, p := range tc.kept {
				require.Contains(t, paths, p)
			}
			runH5(t, h5dump, "--enable-error-stack", tc.file)
		})
	}
}

// nestedGroupFile writes /parent/{child, sub/leaf} and /other with this
// library: symbol table groups (superblock 0).
func nestedGroupFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "nested.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/parent", "/parent/sub"} {
		_, err = fw.CreateGroup(g)
		require.NoError(t, err)
	}
	for _, p := range []string{"/parent/child", "/parent/sub/leaf", "/other"} {
		ds, err := fw.CreateDataset(p, Int32, []uint64{3})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{1, 2, 3}))
	}
	require.NoError(t, fw.Close())
	return file
}

// refCountAt reads the reference count of the object at path.
func refCountAt(t *testing.T, fw *FileWriter, path string) (uint64, uint32) {
	t.Helper()
	addr, err := fw.resolveObjectAddress(path)
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.Superblock())
	require.NoError(t, err)
	return addr, oh.ReferenceCount
}

// TestDelete_GroupTreeKeepsSharedChild: a dataset inside the deleted tree
// that another group also links to survives with its count lowered by one,
// and its data is intact after new objects reuse the freed space.
func TestDelete_GroupTreeKeepsSharedChild(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shared.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/a", "/a/sub", "/b"} {
		_, err = fw.CreateGroup(g)
		require.NoError(t, err)
	}
	want := []int32{7, 8, 9, 10}
	ds, err := fw.CreateDataset("/a/sub/x", Int32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds.Write(want))
	require.NoError(t, fw.CreateHardLink("/b/x", "/a/sub/x"))
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	xAddr, n := refCountAt(t, fw, "/b/x")
	require.Equal(t, uint32(2), n)
	require.NoError(t, fw.Delete("/a"))
	addr, n := refCountAt(t, fw, "/b/x")
	require.Equal(t, xAddr, addr)
	require.Equal(t, uint32(1), n, "the shared dataset keeps one link")

	// New objects take the freed space; none may land on the survivor.
	for i := range 4 {
		ds, err := fw.CreateDataset(fmt.Sprintf("/n%d", i), Int32, []uint64{4})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{int32(i), 0, 0, 0}))
	}
	require.NoError(t, fw.Close())

	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var got []int32
	f.Walk(func(path string, obj Object) {
		if d, ok := obj.(*Dataset); ok && path == "/b/x" {
			v, err := d.Read()
			require.NoError(t, err)
			for _, e := range v {
				got = append(got, int32(e))
			}
		}
	})
	require.Equal(t, want, got)
	paths := strings.Join(walkPaths(t, file), " ")
	require.NotContains(t, paths, "/a")
	requireH5dumpClean(t, file)
}

// TestDelete_GroupTreeWithCycle: a hard link from inside the tree back to the
// deleted group counts as one of its links, so the group survives the delete
// with its count lowered, unreachable, as in the C library; nothing is freed
// twice and the file stays readable. The file is built and the delete run in
// one session: opening a file with a reachable cycle is TASK-061.
func TestDelete_GroupTreeWithCycle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cycle.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/a", "/a/sub"} {
		_, err = fw.CreateGroup(g)
		require.NoError(t, err)
	}
	require.NoError(t, fw.CreateHardLink("/a/sub/up", "/a"))
	ds, err := fw.CreateDataset("/keep", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1}))

	// Deleting /a/sub frees it and, through its back link, lowers /a: the
	// cascade crosses the cycle once. Red before the change: /a/sub was a
	// non-empty group.
	aAddr, n := refCountAt(t, fw, "/a")
	require.Equal(t, uint32(2), n)
	require.NoError(t, fw.Delete("/a/sub"))
	addr, n := refCountAt(t, fw, "/a")
	require.Equal(t, aAddr, addr)
	require.Equal(t, uint32(1), n, "the freed back link lowered /a")

	// Now /a holds the only other link to itself no more: deleting it frees it.
	require.NoError(t, fw.Delete("/a"))
	require.NoError(t, fw.Close())

	require.Equal(t, []string{"/keep"}, walkPaths(t, file))
	requireH5dumpClean(t, file)
}

// TestDelete_LoweredParentKeepsUnlink: deleting a child group whose hard
// link points back at its parent lowers the parent's count after the unlink
// rewrote the parent's header; the count update must not bring the removed
// link back. /p keeps its links as link messages (an external link converts
// it), the case whose header the count update rewrites. Red before the fix
// (review F1): the count was written into the header read before the unlink,
// restoring link "a" to a freed object, and h5dump failed on the file.
//
// The dense case keeps /p's links in dense storage until the delete takes
// them below the minimum (6): the unlink moves them back into the header and
// frees the dense storage, and a stale header would name the freed storage
// again and lose every remaining link.
func TestDelete_LoweredParentKeepsUnlink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sbv   uint8
		dense bool
	}{
		{"compact superblock 2", SuperblockV2, false},
		{"compact superblock 0", SuperblockV0, false},
		{"dense to compact", SuperblockV2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "up.h5")
			fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(tc.sbv))
			require.NoError(t, err)
			for _, g := range []string{"/p", "/p/a"} {
				_, err = fw.CreateGroup(g)
				require.NoError(t, err)
			}
			ds, err := fw.CreateDataset("/p/a/x", Int32, []uint64{4})
			require.NoError(t, err)
			require.NoError(t, ds.Write([]int32{1, 2, 3, 4}))
			require.NoError(t, fw.CreateHardLink("/p/a/up", "/p"))
			require.NoError(t, fw.CreateExternalLink("/p/ext", "other.h5", "/x"))

			want := []string{"/n0", "/n1", "/n2", "/p"}
			if tc.dense {
				makeDenseAtMinimum(t, fw, "/p")
				want = []string{"/n0", "/n1", "/n2", "/p", "/p/d4", "/p/d5", "/p/d6", "/p/d7"}
			}

			require.NoError(t, fw.Delete("/p/a"))
			_, n := refCountAt(t, fw, "/p")
			require.Equal(t, uint32(1), n, "the freed back link lowered /p")
			if tc.dense {
				require.False(t, groupLinkInfo(t, fw, "/p").HasFractalHeap(), "/p is still dense")
			}
			for i := range 3 { // reuse the freed space
				ds, err := fw.CreateDataset(fmt.Sprintf("/n%d", i), Int32, []uint64{4})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{9, 9, 9, int32(i)}))
			}
			require.NoError(t, fw.Close())

			require.Equal(t, want, walkPaths(t, file))
			requireH5dumpClean(t, file)
		})
	}
}

// makeDenseAtMinimum adds d0..d7 to the group at path, which already holds
// two links (a and ext): ten links move it into dense storage; deleting
// d0..d3 leaves six, the minimum that stays dense, so the next delete moves
// the links back into the header.
func makeDenseAtMinimum(t *testing.T, fw *FileWriter, path string) {
	t.Helper()
	for i := range 8 {
		ds, err := fw.CreateDataset(fmt.Sprintf("%s/d%d", path, i), Int32, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{int32(i)}))
	}
	require.True(t, groupLinkInfo(t, fw, path).HasFractalHeap(), "%s is not dense", path)
	for i := range 4 {
		require.NoError(t, fw.Delete(fmt.Sprintf("%s/d%d", path, i)))
	}
	require.True(t, groupLinkInfo(t, fw, path).HasFractalHeap(), "%s left dense storage too early", path)
}

// TestDelete_GroupSurvivesItsBackLink: a group whose subtree links back to
// it keeps one link after its own link is removed, so it survives the delete
// unreachable, with its count lowered and its storage kept, as in the C
// library; nothing below it is freed.
func TestDelete_GroupSurvivesItsBackLink(t *testing.T) {
	file := filepath.Join(t.TempDir(), "survive.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/a", "/a/sub"} {
		_, err = fw.CreateGroup(g)
		require.NoError(t, err)
	}
	require.NoError(t, fw.CreateHardLink("/a/sub/up", "/a"))
	ds, err := fw.CreateDataset("/keep", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1}))

	aAddr, n := refCountAt(t, fw, "/a")
	require.Equal(t, uint32(2), n)
	free0, eof0 := allocatorFreeSpace(fw)
	require.NoError(t, fw.Delete("/a"))
	free1, eof1 := allocatorFreeSpace(fw)
	require.Equal(t, free0+eof0, free1+eof1, "nothing is freed: /a keeps its back link")
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), aAddr, fw.file.Superblock())
	require.NoError(t, err)
	require.Equal(t, uint32(1), oh.ReferenceCount)
	require.NoError(t, fw.Close())

	require.Equal(t, []string{"/keep"}, walkPaths(t, file))
	requireH5dumpClean(t, file)
}

// TestDelete_SharedGroupStaysWritable: a group inside the deleted tree that
// another path also links to stays registered under that path, so objects
// can still be created in it (forgetGroupsBelow reloads the registrations).
func TestDelete_SharedGroupStaysWritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sharedgrp.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/a", "/a/s", "/z"} {
		_, err = fw.CreateGroup(g)
		require.NoError(t, err)
	}
	require.NoError(t, fw.CreateHardLink("/z/s", "/a/s"))
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	require.NoError(t, fw.Delete("/a"))
	ds, err := fw.CreateDataset("/z/s/new", Int32, []uint64{2})
	require.NoError(t, err, "the shared group is still writable under /z/s")
	require.NoError(t, ds.Write([]int32{3, 4}))
	_, err = fw.CreateDataset("/a/s/bad", Int32, []uint64{2})
	require.Error(t, err, "the deleted path is gone")
	require.NoError(t, fw.Close())

	require.Equal(t, []float64{3, 4}, datasetValues(t, file)["/z/s/new"])
	requireH5dumpClean(t, file)
}

// TestDelete_GroupTreeCountBelowLinks: a crafted file whose dataset is named
// by two links in the tree but claims one reference is refused as corrupt
// before anything is written: the second decrement has no count left.
func TestDelete_GroupTreeCountBelowLinks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "lowcount.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/a")
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/a/x", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2}))
	require.NoError(t, fw.CreateHardLink("/a/y", "/a/x"))
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()
	addr, err := fw.resolveObjectAddress("/a/x")
	require.NoError(t, err)
	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	require.NoError(t, err)
	oh.ReferenceCount = 1
	require.NoError(t, fw.writeRefCount(addr, oh, sb))

	before := writerBytes(t, fw)
	free0 := fw.writer.Allocator().FreeBlocks()
	err = fw.Delete("/a")
	require.ErrorIs(t, err, ErrCorrupt)
	require.Equal(t, free0, fw.writer.Allocator().FreeBlocks(), "the allocator changed")
	require.True(t, bytes.Equal(before, writerBytes(t, fw)), "the refused delete changed the file")
}
