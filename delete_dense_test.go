package hdf5

// Delete frees dense attribute and dense link storage as the C library frees
// it once an object's last link goes (H5O_delete, H5Oint.c:1508-1600):
// H5O__ainfo_delete → H5A__dense_delete (H5Adense.c:1779-1830) and
// H5O__linfo_delete → H5G__dense_delete (H5Gdense.c:1581-1650), each freeing
// the name index and creation order index v2 B-trees and the fractal heap
// (H5HF__hdr_delete, H5HFhdr.c:1346-1429: free-space manager, direct and
// indirect blocks, huge objects, header). The oracle is h5stat 1.14.6: its
// attribute and group storage figures come from H5O__attr_bh_info and
// H5G__dense_bh_info, through H5B2_size and H5HF_size (H5HFstat.c:107-155),
// which count the same blocks.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/core"
)

// statSpace is the file space h5stat reports, by kind of storage.
type statSpace struct {
	datasetOH, raw, index          uint64
	groupOH, groupBTree, groupHeap uint64
	attrBTree, attrHeap            uint64
	metadata, unaccounted          uint64
}

// h5statSpace runs h5stat on file and returns its storage figures.
func h5statSpace(t *testing.T, h5stat, file string) statSpace {
	t.Helper()
	out := runH5(t, h5stat, file)
	field := func(re string) uint64 {
		m := regexp.MustCompile(re).FindStringSubmatch(out)
		require.Len(t, m, 2, "h5stat output misses %q:\n%s", re, out)
		v, err := strconv.ParseUint(m[1], 10, 64)
		require.NoError(t, err)
		return v
	}
	return statSpace{
		datasetOH:   field(`Datasets\(exclude compact data\): (\d+)/`),
		raw:         field(`Total raw data size: (\d+)`),
		index:       field(`Chunked datasets:\s+Index: (\d+)`),
		groupOH:     field(`\tGroups: (\d+)/\d+`),
		groupBTree:  field(`\tGroups:\s+B-tree/List: (\d+)`),
		groupHeap:   field(`\tGroups:\s+B-tree/List: \d+\s+Heap: (\d+)`),
		attrBTree:   field(`Attributes:\s+B-tree/List: (\d+)`),
		attrHeap:    field(`Attributes:\s+B-tree/List: \d+\s+Heap: (\d+)`),
		metadata:    field(`File metadata: (\d+) bytes`),
		unaccounted: field(`Unaccounted space: (\d+) bytes`),
	}
}

// datasetSpace is what a dataset owns: its object header, data, chunk index
// and attribute storage.
func (s statSpace) datasetSpace() uint64 {
	return s.datasetOH + s.raw + s.index + s.attrBTree + s.attrHeap
}

// groupSpace is what groups own: object headers, link storage (B-trees and
// local heaps, or dense link storage) and attribute storage.
func (s statSpace) groupSpace() uint64 {
	return s.groupOH + s.groupBTree + s.groupHeap + s.attrBTree + s.attrHeap
}

// keepFile copies the single-chunk dataset of v4_indexes.h5 into a new file
// as /keep: the root group keeps an entry after a delete, so its symbol table
// node is not freed with the deleted object (H5G__node_remove, H5Gnode.c:818).
func keepFile(t *testing.T, h5copy, file string) {
	t.Helper()
	runH5(t, h5copy, "-i", "testdata/chunk_index/v4_indexes.h5", "-o", file, "-s", "/single", "-d", "/keep")
}

// attrNames are the names of 12 attributes, more than the eight the writers
// keep compact.
func attrNames() []string {
	names := make([]string, 12)
	for i := range names {
		names[i] = fmt.Sprintf("a%02d", i)
	}
	return names
}

// writeDeleteDenseFile writes /keep and, unless keepOnly is set, /d: a
// dataset with dense attributes, or with group set a group with them.
func writeDeleteDenseFile(t *testing.T, file string, group, keepOnly bool) {
	t.Helper()
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV2))
	require.NoError(t, err)
	keep, err := fw.CreateDataset("/keep", Int32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, keep.Write([]int32{1, 2, 3, 4}))
	switch {
	case keepOnly && group:
		// A dataset of the same name: the root's local heap holds the same
		// names, so the group figures differ by /d's own storage only.
		ph, err := fw.CreateDataset("/d", Int32, []uint64{4})
		require.NoError(t, err)
		require.NoError(t, ph.Write([]int32{1, 2, 3, 4}))
	case keepOnly:
	case group:
		g, err := fw.CreateGroup("/d")
		require.NoError(t, err)
		for i, n := range attrNames() {
			require.NoError(t, g.WriteAttribute(n, int32(i)))
		}
	default:
		ds, err := fw.CreateDataset("/d", Float64, []uint64{4})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]float64{1, 2, 3, 4}))
		for i, n := range attrNames() {
			require.NoError(t, ds.WriteAttribute(n, int32(i)))
		}
	}
	require.NoError(t, fw.Close())
}

// TestCInterop_DeleteFreesDenseAttributes: deleting the last link of an
// object with dense attributes frees the attribute storage too, as many bytes
// as h5stat counts for the object and its attributes, and leaves a file
// h5dump reads. Red before the change: the attribute name index, the creation
// order index and the fractal heap stayed allocated (deleteRanges collected
// no dense storage).
func TestCInterop_DeleteFreesDenseAttributes(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5stat := h5tool(t, "h5stat")
	h5dump := h5tool(t, "h5dump")
	h5repack := h5tool(t, "h5repack")

	type files struct{ with, without string }
	goWritten := func(group, corder bool) func(t *testing.T, dir string) files {
		return func(t *testing.T, dir string) files {
			f := files{filepath.Join(dir, "with.h5"), filepath.Join(dir, "without.h5")}
			writeDeleteDenseFile(t, f.with, group, false)
			writeDeleteDenseFile(t, f.without, group, true)
			if corder {
				indexCreationOrder(t, f.with, "/d", attrNames())
			}
			return f
		}
	}
	// copied places an object of src in a file holding /keep (h5copy, which
	// rewrites its dense storage through the C library), next to that file
	// without it.
	copied := func(src func(t *testing.T, dir string) string, path string) func(t *testing.T, dir string) files {
		return func(t *testing.T, dir string) files {
			f := files{filepath.Join(dir, "with.h5"), filepath.Join(dir, "without.h5")}
			keepFile(t, h5copy, f.with)
			keepFile(t, h5copy, f.without)
			runH5(t, h5copy, "-i", src(t, dir), "-o", f.with, "-s", path, "-d", "/d")
			return f
		}
	}
	// repacked is a Go-written file rewritten by h5repack with the latest
	// format: dense attribute storage as the C library lays it out, with a
	// free-space manager when the heap has free space.
	repacked := func(group bool) func(t *testing.T, dir string) string {
		return func(t *testing.T, dir string) string {
			src := filepath.Join(dir, "go.h5")
			out := filepath.Join(dir, "repacked.h5")
			writeDeleteDenseFile(t, src, group, false)
			runH5(t, h5repack, "-L", src, out)
			return out
		}
	}

	for _, tc := range []struct {
		name  string
		build func(t *testing.T, dir string) files
		group bool
	}{
		{"go/dataset", goWritten(false, false), false},
		{"go/dataset_corder_indexed", goWritten(false, true), false},
		{"go/group", goWritten(true, false), true},
		{"c/dataset", copied(repacked(false), "/d"), false},
		{"c/dataset_corder_indexed", copied(func(t *testing.T, dir string) string {
			src := filepath.Join(dir, "corder.h5")
			writeDeleteDenseFile(t, src, false, false)
			indexCreationOrder(t, src, "/d", attrNames())
			return src
		}, "/d"), false},
		// 100 attributes: h5copy lays them out in a heap with a root
		// indirect block of two rows and a free-space manager, indexed by a
		// B-tree of depth 1 (h5debug).
		{"c/indirect_heap_free_space", copied(func(*testing.T, string) string {
			return "testdata/hdf5_official/h5stat_newgrat.h5"
		}, "/DATASET_NAME"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.build(t, t.TempDir())
			with, without := h5statSpace(t, h5stat, f.with), h5statSpace(t, h5stat, f.without)
			require.NotZero(t, with.attrBTree, "the object has no dense attributes")
			want := with.datasetSpace() - without.datasetSpace()
			if tc.group {
				want = with.groupSpace() - without.groupSpace()
			}

			require.Equal(t, want, deleteFreedBytes(t, f.with, "/d"))

			out := runH5(t, h5dump, "--enable-error-stack", f.with)
			require.Contains(t, out, `DATASET "keep"`)
			require.NotContains(t, out, `"d"`)
		})
	}
}

// h5debugValue runs h5debug on the structure at addr and returns the number
// printed for label.
func h5debugValue(t *testing.T, h5debug, file string, addr uint64, label string) uint64 {
	t.Helper()
	out := runH5(t, h5debug, file, strconv.FormatUint(addr, 10))
	m := regexp.MustCompile(regexp.QuoteMeta(label) + `:\s+(\d+)`).FindStringSubmatch(out)
	require.Len(t, m, 2, "h5debug output misses %q:\n%s", label, out)
	v, err := strconv.ParseUint(m[1], 10, 64)
	require.NoError(t, err)
	return v
}

// TestCInterop_DeleteFreesLegacyDenseAttributes: the dense attribute storage
// this library wrote up to v0.14.2 (attribute info message ID 0x000F, a name
// index of type 5, a heap with 0 for its huge-object and free-space
// addresses) is freed too. h5stat cannot count that storage (the C library
// does not know message 0x000F and reports it as unaccounted space, with
// other bytes), so the expected size comes from h5debug, which decodes the
// heap, the B-tree and the superblock, and from the C size formulas:
//   - the heap header H5HF_HEADER_SIZE = 26+12L+3O (H5HFpkg.h:67-100) and its
//     root direct block of "Starting block size" (no root indirect block);
//   - the B-tree header H5B2_HEADER_SIZE = 22+O+L (H5B2pkg.h:63-75) and its
//     one leaf of "Size of node" (depth 0);
//   - /d is the root's only member, so the root's symbol table node goes too
//     (H5G__node_remove, H5Gnode.c:817): H5G_NODE_SIZE = 8 + 2K entries of
//     H5G_SIZEOF_ENTRY = L+O+24 (H5Gpkg.h:43, H5Gprivate.h:38).
//
// The address of /d comes from h5ls, the dataset's own size from h5stat.
// Only the two storage addresses in the 0x000F message are this library's
// reading. Red before the change: the heap and the B-tree stayed allocated.
func TestCInterop_DeleteFreesLegacyDenseAttributes(t *testing.T) {
	h5stat := h5tool(t, "h5stat")
	h5dump := h5tool(t, "h5dump")
	h5debug := h5tool(t, "h5debug")
	h5ls := h5tool(t, "h5ls")
	for _, name := range []string{"dense_attr_v0142_sb0.h5", "dense_attr_v0142_sb2.h5", "dense_attr_type5.h5"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "reference", "legacy-hdf5go", name))
			require.NoError(t, err)
			file := filepath.Join(t.TempDir(), name)
			require.NoError(t, os.WriteFile(file, raw, 0o600))

			o := h5debugValue(t, h5debug, file, 0, "Size of file offsets (haddr_t type)")
			l := h5debugValue(t, h5debug, file, 0, "Size of file lengths (hsize_t type)")
			k := h5debugValue(t, h5debug, file, 0, "Symbol table leaf node 1/2 rank")
			loc := regexp.MustCompile(`Location:\s+\d+:(\d+)`).FindStringSubmatch(runH5(t, h5ls, "-v", file+"/d"))
			require.Len(t, loc, 2)
			addr, err := strconv.ParseUint(loc[1], 10, 64)
			require.NoError(t, err)

			f, err := Open(file)
			require.NoError(t, err)
			oh, err := core.ReadObjectHeader(f.reader, addr, f.Superblock())
			require.NoError(t, err)
			msg := attributeInfoMessage(oh)
			require.NotNil(t, msg)
			ainfo, err := core.ParseAttributeInfoMessage(msg.Data, f.Superblock())
			require.NoError(t, err)
			require.NoError(t, f.Close())

			require.Equal(t, "Direct", regexp.MustCompile(`Heap is:\s+(\w+)`).
				FindStringSubmatch(runH5(t, h5debug, file, strconv.FormatUint(ainfo.FractalHeapAddr, 10)))[1])
			require.Zero(t, h5debugValue(t, h5debug, file, ainfo.FractalHeapAddr, "Current # of rows in root indirect block"))
			require.Zero(t, h5debugValue(t, h5debug, file, ainfo.BTreeNameIndexAddr, "Depth"))
			heap := 26 + 12*l + 3*o + h5debugValue(t, h5debug, file, ainfo.FractalHeapAddr, "Starting block size")
			index := 22 + o + l + h5debugValue(t, h5debug, file, ainfo.BTreeNameIndexAddr, "Size of node")
			symbolNode := 8 + 2*k*(l+o+24)
			s := h5statSpace(t, h5stat, file)

			require.Equal(t, s.datasetOH+s.raw+heap+index+symbolNode, deleteFreedBytes(t, file, "/d"))

			out := runH5(t, h5dump, "--enable-error-stack", file)
			require.NotContains(t, out, `DATASET "d"`)
		})
	}
}

// linkGroupsFile writes /c with two datasets and /g with 17, repacks the file
// with the latest format, so that /c keeps its links as link messages in its
// header and /g in dense link storage (the C library's default thresholds, 8
// and 6, H5Gprivate.h), and copies both groups into a file holding /keep,
// whose root is a symbol table group.
func linkGroupsFile(t *testing.T, h5copy, h5repack, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "groups.h5")
	fw, err := CreateForWrite(src, CreateTruncate)
	require.NoError(t, err)
	for _, g := range []string{"/c", "/g"} {
		_, err := fw.CreateGroup(g)
		require.NoError(t, err)
	}
	for _, p := range []string{"/c/x", "/c/y"} {
		ds, err := fw.CreateDataset(p, Int32, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{1, 2}))
	}
	for i := range 17 {
		ds, err := fw.CreateDataset(fmt.Sprintf("/g/d%02d", i), Int32, []uint64{2})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{int32(i), 0}))
	}
	require.NoError(t, fw.Close())

	latest := filepath.Join(dir, "latest.h5")
	runH5(t, h5repack, "-L", src, latest)
	file := filepath.Join(dir, "file.h5")
	keepFile(t, h5copy, file)
	for _, g := range []string{"/c", "/g"} {
		runH5(t, h5copy, "-i", latest, "-o", file, "-s", g, "-d", g)
	}
	return file
}

// groupLinkInfo reads the link info message of the group at path.
func groupLinkInfo(t *testing.T, fw *FileWriter, path string) *core.LinkInfoMessage {
	t.Helper()
	addr, err := fw.resolveObjectAddress(path)
	require.NoError(t, err)
	sb := fw.file.Superblock()
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, sb)
	require.NoError(t, err)
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgLinkInfo {
			linfo, err := core.ParseLinkInfoMessage(msg.Data, sb)
			require.NoError(t, err)
			return linfo
		}
	}
	require.Fail(t, "no link info message", path)
	return nil
}

// TestDelete_NonEmptyLinkGroupsRefused: a group whose links are link
// messages in its header (/c) or dense link storage (/g) is refused while it
// has children, before anything is written, and every child stays readable
// by h5dump and by this library. Red before the change: checkGroupEmpty
// looked at symbol tables only, so both groups were deleted and their
// children orphaned.
func TestDelete_NonEmptyLinkGroupsRefused(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5dump := h5tool(t, "h5dump")
	file := linkGroupsFile(t, h5copy, h5repack, t.TempDir())

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	linfoC, linfoG := groupLinkInfo(t, fw, "/c"), groupLinkInfo(t, fw, "/g")
	require.False(t, linfoC.HasFractalHeap(), "/c is not compact")
	require.True(t, linfoG.HasFractalHeap(), "/g is not dense")

	for _, path := range []string{"/c", "/g"} {
		before := writerBytes(t, fw)
		free0 := fw.writer.Allocator().FreeBlocks()
		err := fw.Delete(path)
		require.Error(t, err, path)
		require.Contains(t, err.Error(), "non-empty group", path)
		require.Equal(t, free0, fw.writer.Allocator().FreeBlocks(), "%s: the allocator changed", path)
		require.True(t, bytes.Equal(before, writerBytes(t, fw)), "%s: the refused delete changed the file", path)
	}
	require.NoError(t, fw.Close())

	out := runH5(t, h5dump, "--enable-error-stack", "-H", file)
	children := make([]string, 0, 19)
	children = append(children, "x", "y")
	for i := range 17 {
		children = append(children, fmt.Sprintf("d%02d", i))
	}
	for _, c := range children {
		require.Contains(t, out, `DATASET "`+c+`"`)
	}

	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var seen []string
	f.Walk(func(path string, _ Object) { seen = append(seen, path) })
	joined := strings.Join(seen, " ")
	require.Contains(t, joined, "/c/x")
	require.Contains(t, joined, "/c/y")
	for i := range 17 {
		require.Contains(t, joined, fmt.Sprintf("/g/d%02d", i))
	}
}

// TestCInterop_DenseLinkStorageBlocks: the blocks collected for a group's
// dense link storage (H5G__dense_delete: the name index, the creation order
// index if any, and the fractal heap) are as many bytes as h5stat counts for
// the group's link storage. The file without the group holds a dataset of
// the same name, so the root's own link storage is the same in both.
func TestCInterop_DenseLinkStorageBlocks(t *testing.T) {
	h5copy := h5tool(t, "h5copy")
	h5repack := h5tool(t, "h5repack")
	h5stat := h5tool(t, "h5stat")
	dir := t.TempDir()
	file := linkGroupsFile(t, h5copy, h5repack, dir)
	with := filepath.Join(dir, "with.h5")
	without := filepath.Join(dir, "without.h5")
	keepFile(t, h5copy, with)
	keepFile(t, h5copy, without)
	runH5(t, h5copy, "-i", file, "-o", with, "-s", "/g", "-d", "/g")
	runH5(t, h5copy, "-i", "testdata/chunk_index/v4_indexes.h5", "-o", without, "-s", "/single", "-d", "/g")
	s1, s0 := h5statSpace(t, h5stat, with), h5statSpace(t, h5stat, without)
	want := s1.groupBTree + s1.groupHeap - s0.groupBTree - s0.groupHeap

	fw, err := OpenForWrite(with, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()
	var got uint64
	dense, err := core.DenseLinkStorageBlocks(fw.writer.Reader(), fw.file.Superblock(), groupLinkInfo(t, fw, "/g"),
		func(_, size uint64) error {
			got += size
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, uint64(17), dense.Records)
	require.Equal(t, want, got)
}

// TestDelete_SharedMessageHeapRefused: freeing an object whose messages live
// in the file's shared object header message heap would need that heap's
// reference counts (H5SM_delete, called from H5O__delete_mesg and
// H5A__dense_delete_bt2_cb), which this library does not maintain. The delete
// is refused with ErrNotSupported before anything is written.
//
// No file in testdata has a deletable object with such a reference under a
// symbol table root (h5stat_tsohm.h5 keeps its messages in the headers,
// flagged shareable only; the h5fc_ext files share none of their datasets'
// messages), and the C tools cannot create one, so the test marks the
// datatype message of a Go-written dataset as a heap reference and restores
// the header checksum.
// The check refuses before it resolves the reference. Red before the change:
// the dataset was deleted.
func TestDelete_SharedMessageHeapRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sohm.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for _, p := range []string{"/keep", "/d"} {
		ds, err := fw.CreateDataset(p, Int32, []uint64{4})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{1, 2, 3, 4}))
	}
	require.NoError(t, fw.Close())

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	addr, err := fw.resolveObjectAddress("/d")
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(fw.writer.Reader(), addr, fw.file.Superblock())
	require.NoError(t, err)
	require.Equal(t, uint8(2), oh.Version)
	var dt *core.HeaderMessage
	for _, msg := range oh.Messages {
		if msg.Type == core.MsgDatatype {
			dt = msg
		}
	}
	require.NotNil(t, dt)
	require.NoError(t, fw.Close())

	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	at := uint64(bytes.Index(raw[addr:], dt.Data)) + addr
	require.Greater(t, at, addr)
	// Version 2 header (H5Ocache.c:1150-1240): "OHDR", version, flags,
	// optional times and phase change values, the chunk size in 1 << (flags
	// & 3) bytes, the messages, the checksum. A message header is type(1)
	// size(2) flags(1), no creation order here.
	hdrFlags := raw[addr+5]
	require.Zero(t, hdrFlags&0x04, "creation order tracked")
	pos := addr + 6
	if hdrFlags&0x20 != 0 {
		pos += 16
	}
	if hdrFlags&0x10 != 0 {
		pos += 4
	}
	width := uint64(1) << (hdrFlags & 3)
	var chunk [8]byte
	copy(chunk[:], raw[pos:pos+width])
	end := pos + width + binary.LittleEndian.Uint64(chunk[:])
	require.Equal(t, byte(core.MsgDatatype), raw[at-4])
	raw[at-1] |= core.MsgFlagShared
	copy(raw[at:], []byte{3, 1, 0x77, 0, 0, 0, 0, 0, 0, 0}) // version 3, H5O_SHARE_TYPE_SOHM, heap ID
	binary.LittleEndian.PutUint32(raw[end:], core.JenkinsChecksum(raw[addr:end]))
	require.NoError(t, os.WriteFile(file, raw, 0o600))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()
	before := writerBytes(t, fw)
	free0 := fw.writer.Allocator().FreeBlocks()
	err = fw.Delete("/d")
	require.ErrorIs(t, err, ErrNotSupported)
	require.Equal(t, free0, fw.writer.Allocator().FreeBlocks())
	require.True(t, bytes.Equal(before, writerBytes(t, fw)), "the refused delete changed the file")
	_, err = fw.resolveObjectAddress("/d")
	require.NoError(t, err, "/d is gone")
}
