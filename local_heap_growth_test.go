package hdf5

// Local heap growth: a group's names live in its local heap, and the heap's
// data block grows when the names no longer fit. The C library keeps the
// heap prefix where it is and moves only the data block (H5HL_insert,
// H5HL.c:599-646, and H5HL__dblk_realloc, H5HLdblk.c:163-257, which sets
// heap->dblk_addr at :190), so the group's symbol table message never
// changes. These tests cover the growth path for the root and a nested
// group, for superblock v0 and the default v2, and through OpenForWrite.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/scigolib/hdf5/internal/structures"
)

// heapGrowthChildName is the name pattern of the review repro: about 48
// bytes per name, so a 4096-byte heap holds fewer than 90 of them.
func heapGrowthChildName(i int) string {
	return fmt.Sprintf("dataset_with_a_rather_long_name_number_%04d", i)
}

// heapGrowthCase is one file layout of the growth matrix.
type heapGrowthCase struct {
	name   string
	sbVer  uint8
	parent string // "/" for the root, otherwise a group created first
	n      int
}

func heapGrowthCases() []heapGrowthCase {
	cases := make([]heapGrowthCase, 0, 16)
	for _, sb := range []struct {
		name string
		ver  uint8
	}{{"sbv0", SuperblockV0}, {"sbv2", SuperblockV2}} {
		for _, parent := range []string{"/", "/g"} {
			for _, n := range []int{90, 91, 100, 1000} {
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

// heapGrowthParentName names a parent group in subtest names.
func heapGrowthParentName(parent string) string {
	if parent == "/" {
		return "root"
	}
	return "group"
}

// childPath joins a parent group path and a child name.
func childPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

// addHeapGrowthChildren creates datasets first..first+count-1 under parent.
func addHeapGrowthChildren(t *testing.T, fw *FileWriter, parent string, first, count int) {
	t.Helper()
	for i := first; i < first+count; i++ {
		ds, err := fw.CreateDataset(childPath(parent, heapGrowthChildName(i)), Int32, []uint64{1})
		require.NoError(t, err, "create child %d", i)
		require.NoError(t, ds.Write([]int32{int32(i)}), "write child %d", i)
	}
}

// writeHeapGrowthFile writes the file of one case and returns its path.
func writeHeapGrowthFile(t *testing.T, tc heapGrowthCase) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), tc.name+".h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(tc.sbVer))
	require.NoError(t, err)
	if tc.parent != "/" {
		_, err = fw.CreateGroup(tc.parent)
		require.NoError(t, err)
	}
	addHeapGrowthChildren(t, fw, tc.parent, 0, tc.n)
	require.NoError(t, fw.Close())
	return file
}

// requireHeapGrowthChildren opens file with this library and checks that
// parent holds exactly the datasets 0..n-1, by name.
func requireHeapGrowthChildren(t *testing.T, file, parent string, n int) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	prefix := childPath(parent, "")
	var got []string
	f.Walk(func(path string, obj Object) {
		if _, ok := obj.(*Dataset); !ok {
			return
		}
		if rest, found := strings.CutPrefix(path, prefix); found && !strings.Contains(rest, "/") {
			got = append(got, rest)
		}
	})
	want := make([]string, n)
	for i := range want {
		want[i] = heapGrowthChildName(i)
	}
	sort.Strings(got)
	require.Equal(t, want, got)
}

// TestLocalHeapGrowth_RoundTrip: a group whose names outgrow the initial
// 4096-byte local heap is read back whole by this library. Red before the
// fix for a nested group at N >= 100 ("link name read failed: offset beyond
// heap data": the symbol table message kept the old heap) and for the root
// of a superblock-v2 file (raw patch of a checksummed header).
func TestLocalHeapGrowth_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 32 files with up to 1000 datasets each")
	}
	for _, tc := range heapGrowthCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeHeapGrowthFile(t, tc)
			requireHeapGrowthChildren(t, file, tc.parent, tc.n)
		})
	}
}

// TestLocalHeapGrowth_OpenForWrite: children added through OpenForWrite to a
// group of an existing file past the heap's capacity are all read back.
func TestLocalHeapGrowth_OpenForWrite(t *testing.T) {
	for _, parent := range []string{"/", "/g"} {
		t.Run(heapGrowthParentName(parent), func(t *testing.T) {
			file := writeHeapGrowthFile(t, heapGrowthCase{name: "reopen", sbVer: SuperblockV2, parent: parent, n: 50})

			fw, err := OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			addHeapGrowthChildren(t, fw, parent, 50, 100)
			require.NoError(t, fw.Close())

			requireHeapGrowthChildren(t, file, parent, 150)
		})
	}
}

// TestCInterop_LocalHeapGrowth: the C library (h5dump 1.14.6) reads every
// file of the growth matrix. Red before the fix: H5HL_offset_into "unable to
// offset into local heap" for a nested group, "incorrect metadata checksum"
// for the root of a superblock-v2 file.
func TestCInterop_LocalHeapGrowth(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	for _, tc := range heapGrowthCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := writeHeapGrowthFile(t, tc)
			out := runH5(t, h5dump, "--enable-error-stack", "-H", file)
			last := heapGrowthChildName(tc.n - 1)
			require.Contains(t, out, `DATASET "`+last+`"`, "h5dump output misses the last child")
			require.Equal(t, tc.n, strings.Count(out, `DATASET "dataset_with_a_rather_long_name_number_`))
		})
	}
}

// freeRange is one Free call.
type freeRange struct{ addr, size uint64 }

// recordingFreer records the ranges a delete would free.
type recordingFreer struct{ calls []freeRange }

func (r *recordingFreer) Free(addr, size uint64) error {
	r.calls = append(r.calls, freeRange{addr, size})
	return nil
}

// TestLocalHeapGrowth_DeleteFreesMovedHeap: deleting a group whose heap data
// block moved frees the heap prefix (32 bytes for 8-byte offsets and
// lengths) and the moved data block as two ranges, as H5HL_delete does for a
// heap that is not a single cache object (H5HL.c:847-900), never one range
// of the heap's total size from the prefix address.
func TestLocalHeapGrowth_DeleteFreesMovedHeap(t *testing.T) {
	file := filepath.Join(t.TempDir(), "moved.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	defer func() { require.NoError(t, fw.Close()) }()

	_, err = fw.CreateGroup("/g")
	require.NoError(t, err)
	const n = 100 // past the 4096-byte initial heap
	addHeapGrowthChildren(t, fw, "/g", 0, n)
	for i := range n {
		require.NoError(t, fw.Delete(childPath("/g", heapGrowthChildName(i))))
	}

	meta := fw.groups["/g"]
	require.NotNil(t, meta)
	heap, err := structures.LoadLocalHeap(fw.writer, meta.heapAddr, fw.file.sb)
	require.NoError(t, err)
	require.False(t, heap.Contiguous(meta.heapAddr), "the data block must have moved")
	require.Equal(t, uint64(32), heap.PrefixSize())

	symTable := make([]byte, 16)
	fw.file.sb.Endianness.PutUint64(symTable[0:], meta.btreeAddr)
	fw.file.sb.Endianness.PutUint64(symTable[8:], meta.heapAddr)
	rec := &recordingFreer{}
	require.NoError(t, fw.groupStructureRanges(symTable, fw.file.sb, rec.Free))

	require.Contains(t, rec.calls, freeRange{meta.heapAddr, 32})
	require.Contains(t, rec.calls, freeRange{heap.DataSegmentAddress, heap.DataSegmentSize})
	for _, c := range rec.calls {
		require.NotEqual(t, freeRange{meta.heapAddr, heap.Size()}, c, "heap freed as one range from its prefix")
	}
}

// TestCInterop_LocalHeapGrowth_OpenForWrite grows, through OpenForWrite,
// the local heap of a file the C library wrote (h5repack), whose heap carries
// a C free list (H5HL__fl_serialize, H5HLcache.c:289-315), then checks the
// result with h5dump and with this library.
func TestCInterop_LocalHeapGrowth_OpenForWrite(t *testing.T) {
	h5dump := h5tool(t, "h5dump")
	h5repack := h5tool(t, "h5repack")
	for _, parent := range []string{"/", "/g"} {
		t.Run(heapGrowthParentName(parent), func(t *testing.T) {
			goFile := writeHeapGrowthFile(t, heapGrowthCase{name: "go", sbVer: SuperblockV0, parent: parent, n: 20})
			cFile := filepath.Join(t.TempDir(), "c.h5")
			runH5(t, h5repack, goFile, cFile)

			fw, err := OpenForWrite(cFile, OpenReadWrite)
			require.NoError(t, err)
			addHeapGrowthChildren(t, fw, parent, 20, 130)
			require.NoError(t, fw.Close())

			out := runH5(t, h5dump, "--enable-error-stack", "-H", cFile)
			require.Equal(t, 150, strings.Count(out, `DATASET "dataset_with_a_rather_long_name_number_`))
			requireHeapGrowthChildren(t, cFile, parent, 150)
		})
	}
}
