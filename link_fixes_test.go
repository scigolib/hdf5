package hdf5

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/scigolib/hdf5/internal/writer"
	"github.com/stretchr/testify/require"
)

// walkObjects returns every path Walk visits in file.
func walkObjects(t *testing.T, file string) map[string]Object {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	// Datasets read their data through the open file: it closes with the test.
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	paths := map[string]Object{}
	f.Walk(func(p string, obj Object) { paths[p] = obj })
	return paths
}

// requireH5lsLists runs h5ls -r on file when it is installed and requires
// each of want in its output.
func requireH5lsLists(t *testing.T, file string, want ...string) {
	t.Helper()
	h5ls, err := exec.LookPath("h5ls")
	if err != nil {
		t.Log("h5ls not in PATH: C check skipped")
		return
	}
	out, runErr := runH5Err(h5ls, "-r", file)
	require.NoError(t, runErr, "h5ls -r %s:\n%s", file, out)
	for _, w := range want {
		require.Contains(t, out, w, "h5ls -r:\n%s", out)
	}
}

// TestCreateExternalLink_ConvertsSymbolTableGroup: an external link into a
// symbol-table group converts the group to the 1.8 format first
// (H5Gobj.c:512-552), so the C library reads the whole file, the link is a
// link message, and the links the group had stay readable.
func TestCreateExternalLink_ConvertsSymbolTableGroup(t *testing.T) {
	for _, sbv := range []uint8{SuperblockV0, SuperblockV2} {
		for _, parent := range []string{"", "/g"} {
			name := "v" + string(rune('0'+sbv)) + "_root"
			if parent != "" {
				name = "v" + string(rune('0'+sbv)) + "_subgroup"
			}
			t.Run(name, func(t *testing.T) {
				file := filepath.Join(t.TempDir(), "ext.h5")
				fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(sbv))
				require.NoError(t, err)
				ds, err := fw.CreateDataset("/d", Int32, []uint64{3})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{1, 2, 3}))
				_, err = fw.CreateGroup("/g")
				require.NoError(t, err)
				ds, err = fw.CreateDataset("/g/d2", Int32, []uint64{2})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{7, 8}))
				require.NoError(t, fw.CreateSoftLink(parent+"/s", "/d"))
				require.NoError(t, fw.CreateHardLink(parent+"/h", "/d"))

				require.NoError(t, fw.CreateExternalLink(parent+"/ext", "other.h5", "/x"))
				_, isLinkGroup := fw.linkMessageGroup(parent)
				require.True(t, isLinkGroup, "the group is in the 1.8 format after the external link")
				// A later link goes the 1.8 way too.
				ds, err = fw.CreateDataset(parent+"/after", Int32, []uint64{1})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{9}))
				require.NoError(t, fw.Close())

				requireH5dumpClean(t, file)
				requireH5lsLists(t, file, "ext", "External Link", "other.h5", "Soft Link", "after")

				paths := walkObjects(t, file)
				for _, p := range []string{"/d", "/g/", "/g/d2", parent + "/h", parent + "/after"} {
					require.Contains(t, paths, p)
				}
				h, ok := paths[parent+"/h"].(*Dataset)
				require.True(t, ok)
				vals, err := h.Read()
				require.NoError(t, err)
				require.Equal(t, []float64{1, 2, 3}, vals)

				// The converted group is writable again after a reopen.
				fw, err = OpenForWrite(file, OpenReadWrite)
				require.NoError(t, err)
				ds, err = fw.CreateDataset(parent+"/reopened", Int32, []uint64{1})
				require.NoError(t, err)
				require.NoError(t, ds.Write([]int32{4}))
				require.NoError(t, fw.Close())
				requireH5dumpClean(t, file)
				require.Contains(t, walkObjects(t, file), parent+"/reopened")
			})
		}
	}
}

// TestCreateExternalLink_ManyLinksGoDense: a symbol-table group holding more
// than max_compact links converts straight to dense storage.
func TestCreateExternalLink_ManyLinksGoDense(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dense.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	for i := 0; i < 12; i++ {
		ds, err := fw.CreateDataset("/d"+string(rune('a'+i)), Int32, []uint64{1})
		require.NoError(t, err)
		require.NoError(t, ds.Write([]int32{int32(i)}))
	}
	require.NoError(t, fw.CreateExternalLink("/ext", "other.h5", "/x"))
	require.NoError(t, fw.Close())

	requireH5dumpClean(t, file)
	requireH5lsLists(t, file, "External Link")
	paths := walkObjects(t, file)
	for i := 0; i < 12; i++ {
		require.Contains(t, paths, "/d"+string(rune('a'+i)))
	}
}

// writeLegacyExternalLink writes an external link the way hdf5-go v0.14.2
// and earlier did: an object header of its own holding one link message
// whose value is file length, file, path length, path, linked into the
// parent group as an object.
func writeLegacyExternalLink(t *testing.T, fw *FileWriter, name, file, path string) {
	t.Helper()
	v := binary.LittleEndian.AppendUint16(nil, uint16(len(file)))
	v = append(v, file...)
	v = binary.LittleEndian.AppendUint16(v, uint16(len(path)))
	v = append(v, path...)
	data, err := core.EncodeLinkMessage(&core.LinkMessage{
		Version: 1, Flags: core.LinkFlagLinkTypeFieldBit | core.LinkFlagCharSetBit,
		Type: core.LinkTypeExternal, Name: name, LinkValue: v,
	}, fw.file.sb)
	require.NoError(t, err)
	ohw := &core.ObjectHeaderWriter{Version: 2, Messages: []core.MessageWriter{{Type: core.MsgLinkMessage, Data: data}}}
	ohw.PadToSize(core.MinOHDRAllocSize)
	size, err := calculateObjectHeaderSize(ohw)
	require.NoError(t, err)
	addr, err := fw.writer.Allocate(size)
	require.NoError(t, err)
	_, err = ohw.WriteTo(fw.writer, addr)
	require.NoError(t, err)
	require.NoError(t, fw.linkToParent("/", name, addr))
}

// TestLegacyExternalLink_WarnsAndRefusesWrite: a file with an external link
// in the layout hdf5-go v0.14.2 and earlier wrote opens with
// WarnLegacyExternalLink, and OpenForWrite refuses it with ErrNeedsRepack.
func TestLegacyExternalLink_WarnsAndRefusesWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	writeLegacyExternalLink(t, fw, "ext", "other.h5", "/x")
	require.NoError(t, fw.Close())

	f, err := Open(file)
	require.NoError(t, err)
	f.Walk(func(string, Object) {})
	found := false
	for _, w := range f.Warnings() {
		if w.Code == WarnLegacyExternalLink {
			found = true
			require.Contains(t, w.Message, "v0.14.2")
		}
	}
	require.NoError(t, f.Close())
	require.True(t, found, "WarnLegacyExternalLink recorded")
	require.Equal(t, "legacy external link", WarnLegacyExternalLink.String())

	_, err = OpenForWrite(file, OpenReadWrite)
	require.ErrorIs(t, err, ErrNeedsRepack)
}

// TestOpenForWrite_StaleRootCacheIgnored: a version 0 superblock whose root
// entry still caches a symbol table the root header no longer has (the root
// holds an external link) is read as C reads it (H5Groot.c:213-222): the
// cache is dropped and the root's links are taken from its header.
func TestOpenForWrite_StaleRootCacheIgnored(t *testing.T) {
	file := filepath.Join(t.TempDir(), "stale.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(SuperblockV0))
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/d", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{5}))
	btree, heap := fw.rootBTreeAddr, fw.rootHeapAddr
	require.NoError(t, fw.CreateExternalLink("/ext", "other.h5", "/x"))
	require.NoError(t, fw.Close())

	// Put the old cache back into the root entry's scratch pad, as the C
	// library leaves it after the same conversion.
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	binary.LittleEndian.PutUint64(raw[80:88], btree)
	binary.LittleEndian.PutUint64(raw[88:96], heap)
	require.NoError(t, os.WriteFile(file, raw, 0o600))

	fw, err = OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	require.False(t, fw.rootHasSymbolTable(), "the stale cache names no table of the root")
	ds, err = fw.CreateDataset("/d2", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{6}))
	require.NoError(t, fw.Close())

	requireH5dumpClean(t, file)
	paths := walkObjects(t, file)
	require.Contains(t, paths, "/d")
	require.Contains(t, paths, "/d2")
}

// headerBlockFree reports whether the first byte of the header at addr is
// free space.
func headerBlockFree(fw *FileWriter, addr uint64) bool {
	return fw.writer.Allocator().CheckFree([]writer.FreeBlock{{Offset: addr, Size: 1}}) != nil
}

// TestDelete_V1RefCount: Delete decrements the reference count in the prefix
// of a version 1 header (H5O_link, H5Oint.c), so removing one of two hard
// links keeps the object with count 1 and removing the other frees it. This
// library writes version 2 headers, so the object is a dataset of a file
// the C library wrote with a version 0 superblock.
func TestDelete_V1RefCount(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "reference", "deflate.h5"))
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "v1rc.h5")
	require.NoError(t, os.WriteFile(file, src, 0o600))
	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	require.NoError(t, fw.CreateHardLink("/b", "/Dataset1"))
	addr, err := fw.resolveObjectAddress("/Dataset1")
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(fw.writer, addr, fw.file.sb)
	require.NoError(t, err)
	require.Equal(t, uint8(1), oh.Version)
	require.Equal(t, uint32(2), oh.ReferenceCount)

	require.NoError(t, fw.Delete("/Dataset1"))
	oh, err = core.ReadObjectHeader(fw.writer, addr, fw.file.sb)
	require.NoError(t, err)
	require.Equal(t, uint32(1), oh.ReferenceCount, "one link left")
	require.False(t, headerBlockFree(fw, addr))

	require.NoError(t, fw.Delete("/b"))
	require.True(t, headerBlockFree(fw, addr), "the object is freed with its last link")
	require.NoError(t, fw.Close())
	requireH5dumpClean(t, file)
}

// TestCreateDenseGroup_CountsLinks: each link of a dense group adds one to
// its target's reference count, as H5L__link_cb does through H5O_link, so
// deleting the target's other link keeps the object.
func TestCreateDenseGroup_CountsLinks(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dg.h5")
	fw, err := CreateForWrite(file, CreateTruncate)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/b", Int32, []uint64{2})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{3, 4}))
	require.NoError(t, fw.CreateDenseGroup("/dg", map[string]string{"t": "/b"}))
	addr, err := fw.resolveObjectAddress("/b")
	require.NoError(t, err)
	oh, err := core.ReadObjectHeader(fw.writer, addr, fw.file.sb)
	require.NoError(t, err)
	require.Equal(t, uint32(2), oh.ReferenceCount)

	require.NoError(t, fw.Delete("/b"))
	require.False(t, headerBlockFree(fw, addr), "/dg/t still holds the object")
	require.NoError(t, fw.Close())

	requireH5dumpClean(t, file)
	paths := walkObjects(t, file)
	require.NotContains(t, paths, "/b")
	tds, ok := paths["/dg/t"].(*Dataset)
	require.True(t, ok, "paths: %v", keysOf(paths))
	vals, err := tds.Read()
	require.NoError(t, err)
	require.Equal(t, []float64{3, 4}, vals)
}

func keysOf(m map[string]Object) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return strings.Join(ks, ",")
}
