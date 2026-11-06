package hdf5

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/writer"
	"github.com/stretchr/testify/require"
)

// writerState is what a refused create must leave as it was: the file's
// bytes up to its end, the allocator's free list, the registered groups and
// the header allocations.
type writerState struct {
	bytes        []byte
	free         []writer.FreeBlock
	groups       map[string]GroupMetadata
	headerAllocs map[uint64]uint64
}

func captureWriterState(t *testing.T, fw *FileWriter) writerState {
	t.Helper()
	st := writerState{
		bytes:        writerBytes(t, fw),
		free:         fw.writer.Allocator().FreeBlocks(),
		groups:       make(map[string]GroupMetadata, len(fw.groups)),
		headerAllocs: make(map[uint64]uint64, len(fw.headerAllocs)),
	}
	for p, m := range fw.groups {
		st.groups[p] = *m
	}
	for a, s := range fw.headerAllocs {
		st.headerAllocs[a] = s
	}
	return st
}

// stateDiff names what changed between two captured states, "" when nothing.
func stateDiff(before, after writerState) string {
	var diffs []string
	if len(before.bytes) != len(after.bytes) {
		diffs = append(diffs, fmt.Sprintf("end of file %d -> %d", len(before.bytes), len(after.bytes)))
	} else if !bytes.Equal(before.bytes, after.bytes) {
		diffs = append(diffs, "file bytes")
	}
	if fmt.Sprint(before.free) != fmt.Sprint(after.free) {
		diffs = append(diffs, fmt.Sprintf("free list %v -> %v", before.free, after.free))
	}
	if fmt.Sprint(before.groups) != fmt.Sprint(after.groups) {
		diffs = append(diffs, "registered groups")
	}
	if fmt.Sprint(before.headerAllocs) != fmt.Sprint(after.headerAllocs) {
		diffs = append(diffs, "header allocations")
	}
	return strings.Join(diffs, ", ")
}

// sortedWalkPaths returns every path Walk reports in file, sorted, without
// the trailing slash Walk gives groups ("/" stays "/").
func sortedWalkPaths(t *testing.T, file string) []string {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var paths []string
	f.Walk(func(p string, _ Object) {
		if len(p) > 1 {
			p = strings.TrimSuffix(p, "/")
		}
		paths = append(paths, p)
	})
	sort.Strings(paths)
	return paths
}

// longChildName is a 200-byte link name: about 19 of them fill the 4096-byte
// local heap of a new group, so the next one needs a larger heap.
func longChildName(i int) string {
	return fmt.Sprintf("n%03d_%s", i, strings.Repeat("x", 195))
}

// TestCreateGroup_DuplicateNameWithFullHeap is review finding R1: a name the
// group already holds is refused before the group's local heap is touched.
// The C library looks the name up first (H5L__link_cb, H5Lint.c:552-554;
// H5G__node_insert, H5Gnode.c:583-584, before H5HL_insert at :594). The old
// code added the name to the heap first; with a full heap that freed and
// moved the heap, and the refused insert never wrote the moved heap, so the
// group's symbol table pointed at freed space and every name in it broke.
func TestCreateGroup_DuplicateNameWithFullHeap(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dup_full_heap.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/g")
	require.NoError(t, err)

	const children = 40
	changed := 0
	for i := 0; i < children; i++ {
		_, err := fw.CreateGroup("/g/" + longChildName(i))
		require.NoError(t, err, "child %d", i)

		before := captureWriterState(t, fw)
		_, err = fw.CreateGroup("/g/" + longChildName(0))
		require.ErrorIs(t, err, errGroupEntryExists, "duplicate after child %d", i)
		if d := stateDiff(before, captureWriterState(t, fw)); d != "" {
			changed++
			if changed == 1 {
				t.Errorf("duplicate after child %d changed the file: %s", i, d)
			}
		}
	}
	require.Zero(t, changed, "refused duplicates that changed the file")
	require.NoError(t, fw.Close())

	want := []string{"/", "/g"}
	for i := 0; i < children; i++ {
		want = append(want, "/g/"+longChildName(i))
	}
	sort.Strings(want)
	require.Equal(t, want, sortedWalkPaths(t, file))
	requireH5dumpClean(t, file)
}

// TestCreateGroup_DuplicateKeepsRegisteredGroup is review finding R2: a
// refused CreateGroup registered its new, unlinked group under the existing
// path before linking it, so later objects created under that path went into
// the orphan group and vanished from the file.
func TestCreateGroup_DuplicateKeepsRegisteredGroup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dup_group.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/a")
	require.NoError(t, err)

	_, err = fw.CreateGroup("/a")
	require.ErrorIs(t, err, errGroupEntryExists)

	ds, err := fw.CreateDataset("/a/d", Float64, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]float64{1, 2}))
	require.NoError(t, fw.Close())

	require.Equal(t, []string{"/", "/a", "/a/d"}, sortedWalkPaths(t, file))
	requireH5dumpClean(t, file)
}

// duplicateCreator creates an object or a link at path through one of the
// public creators.
type duplicateCreator struct {
	name string
	// needsTarget marks creators that resolve a target path in the file
	// first, which needs a root group with a symbol table.
	needsTarget bool
	create      func(fw *FileWriter, path string) error
}

func duplicateCreators(t *testing.T) []duplicateCreator {
	t.Helper()
	ct, err := NewCompoundType(8, []CompoundMember{
		{Name: "a", Offset: 0, Type: Int32},
		{Name: "b", Offset: 4, Type: Float32},
	})
	require.NoError(t, err)
	return []duplicateCreator{
		{name: "CreateGroup", create: func(fw *FileWriter, p string) error {
			_, err := fw.CreateGroup(p)
			return err
		}},
		{name: "CreateGroupWithLinks", create: func(fw *FileWriter, p string) error {
			return fw.CreateGroupWithLinks(p, nil)
		}},
		{name: "CreateDenseGroup", create: func(fw *FileWriter, p string) error {
			return fw.CreateDenseGroup(p, nil)
		}},
		{name: "CreateDataset/contiguous", create: func(fw *FileWriter, p string) error {
			_, err := fw.CreateDataset(p, Int32, []uint64{4})
			return err
		}},
		{name: "CreateDataset/chunked", create: func(fw *FileWriter, p string) error {
			_, err := fw.CreateDataset(p, Int32, []uint64{4}, WithChunkDims([]uint64{2}))
			return err
		}},
		{name: "CreateCompound/contiguous", create: func(fw *FileWriter, p string) error {
			_, err := fw.CreateCompound(p, ct, []uint64{2})
			return err
		}},
		{name: "CreateCompound/chunked", create: func(fw *FileWriter, p string) error {
			_, err := fw.CreateCompound(p, ct, []uint64{2}, WithChunkDims([]uint64{1}))
			return err
		}},
		{name: "CreateHardLink", needsTarget: true, create: func(fw *FileWriter, p string) error {
			return fw.CreateHardLink(p, "/target")
		}},
		{name: "CreateSoftLink", create: func(fw *FileWriter, p string) error {
			return fw.CreateSoftLink(p, "/target")
		}},
		{name: "CreateExternalLink", create: func(fw *FileWriter, p string) error {
			return fw.CreateExternalLink(p, "other.h5", "/x")
		}},
	}
}

// requireDuplicatesRefused calls every creator for every path of existing
// and requires each call to be refused as a duplicate with the writer left
// as it was.
func requireDuplicatesRefused(t *testing.T, fw *FileWriter, existing []string, symbolTableRoot bool) {
	t.Helper()
	for _, c := range duplicateCreators(t) {
		if c.needsTarget && !symbolTableRoot {
			continue
		}
		for _, p := range existing {
			before := captureWriterState(t, fw)
			err := c.create(fw, p)
			require.ErrorIs(t, err, errGroupEntryExists, "%s(%q)", c.name, p)
			require.Empty(t, stateDiff(before, captureWriterState(t, fw)), "%s(%q) changed the writer", c.name, p)
		}
	}
}

// TestCreate_DuplicateNameRefusedByEveryCreator: every creator refuses a
// name its parent group already holds, whatever the existing object is (a
// group, a dataset, a soft link) and wherever it is (the root, a nested
// group), before it allocates or writes anything; then the file is as it
// was (R2's one order for every creator: validate, check the name,
// allocate, link, register).
func TestCreate_DuplicateNameRefusedByEveryCreator(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dup_every_creator.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	_, err = fw.CreateGroup("/a")
	require.NoError(t, err)
	_, err = fw.CreateGroup("/a/inner")
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/target", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{7, 8}))
	require.NoError(t, fw.CreateSoftLink("/soft", "/target"))
	require.NoError(t, fw.CreateSoftLink("/a/soft", "/target"))

	requireDuplicatesRefused(t, fw, []string{"/a", "/target", "/soft", "/a/inner", "/a/soft"}, true)
	require.NoError(t, fw.Close())

	require.Equal(t, []string{"/", "/a", "/a/inner", "/target"}, sortedWalkPaths(t, file))
	out := requireH5dumpClean(t, file)
	if out != "" {
		require.Equal(t, 2, strings.Count(out, `LINKTARGET "/target"`), "soft links listed by h5dump:\n%s", out)
	}
}

// TestCreate_DuplicateNameInCompactRoot: a root group of the 1.8 format
// keeps its links as link messages (H5Gcompact.c); a duplicate is refused
// there before anything is allocated as well. The old code allocated and
// wrote the new object first and only then found the name when inserting
// the link message.
func TestCreate_DuplicateNameInCompactRoot(t *testing.T) {
	file := copyFixture(t, filepath.Join("testdata", "reference", "h5fc_ext1_f.h5"))
	names := rootLinkNames(t, file)
	require.Contains(t, names, "DSET_CONTIGUOUS")

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	requireDuplicatesRefused(t, fw, []string{"/DSET_CONTIGUOUS"}, false)
	require.NoError(t, fw.Close())

	require.ElementsMatch(t, names, rootLinkNames(t, file))
	requireH5dumpClean(t, file)
}

// TestCreate_DuplicateNameInDenseRoot: the root of h5stat_newgrat.h5 keeps
// its 35001 links in dense storage (a fractal heap indexed by a v2 B-tree,
// H5Gdense.c). A duplicate is refused as one: the old code allocated the new
// object and then refused the insert into dense storage as unsupported.
func TestCreate_DuplicateNameInDenseRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("opens a 6 MB file with 35001 links")
	}
	file := copyFixture(t, newgratFile)
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	before := captureWriterState(t, fw)
	_, err = fw.CreateGroup("/GROUP1")
	require.ErrorIs(t, err, errGroupEntryExists)
	require.Empty(t, stateDiff(before, captureWriterState(t, fw)))

	_, err = fw.CreateDataset("/DATASET_NAME", Int32, []uint64{1})
	require.ErrorIs(t, err, errGroupEntryExists)
	require.Empty(t, stateDiff(before, captureWriterState(t, fw)))
}

// TestCreate_LinkFailureFreesAllocations: when linking fails after the new
// object was allocated, the creator frees what it allocated and registers
// nothing, as the C library deletes an object whose link could not be
// inserted (H5L__link_cb, H5Lint.c:660-669: H5O_dec_rc_by_loc drops the new
// object's only reference). The dense root of h5stat_newgrat.h5 (35001
// links, a fractal heap of many blocks) cannot take another link here: the
// insert is refused with ErrNotSupported after the new object was written.
func TestCreate_LinkFailureFreesAllocations(t *testing.T) {
	if testing.Short() {
		t.Skip("opens a 6 MB file with 35001 links")
	}
	file := copyFixture(t, newgratFile)
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	for _, c := range duplicateCreators(t) {
		// A soft link allocates nothing; CreateDenseGroup without links is
		// refused before it allocates.
		if c.needsTarget || c.name == "CreateSoftLink" || c.name == "CreateDenseGroup" {
			continue
		}
		before := captureWriterState(t, fw)
		err := c.create(fw, "/new_object")
		require.ErrorIs(t, err, ErrNotSupported, c.name)
		require.Empty(t, stateDiff(before, captureWriterState(t, fw)), "%s: %v", c.name, err)
	}
}
