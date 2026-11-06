package hdf5

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scigolib/hdf5/internal/core"
	"github.com/stretchr/testify/require"
)

// Tests for rewriting object headers of existing objects (review of
// TASK-072, findings E1-E4): a rewritten header stays inside the space its
// chunk 0 has on disk, or moves messages to a continuation chunk; the
// checksum covers the header as it is after the rewrite; an empty chunked
// dataset has an undefined chunk index address; the attribute calls of an
// opened dataset see the attributes written in the same session.

// testChunk0Size returns the size of chunk 0 of the version 2 object header
// at addr in the file image b: prefix, messages and checksum (H5Ocache.c:
// 1012-1092 for the prefix, H5Opkg.h:85-107 for the sizes).
func testChunk0Size(t *testing.T, b []byte, addr uint64) uint64 {
	t.Helper()
	require.Equal(t, "OHDR", string(b[addr:addr+4]), "no OHDR at %d", addr)
	flags := b[addr+5]
	p := addr + 6
	if flags&0x20 != 0 { // times stored
		p += 16
	}
	if flags&0x10 != 0 { // attribute phase change values stored
		p += 4
	}
	var size uint64
	switch flags & 0x03 {
	case 0:
		size = uint64(b[p])
		p++
	case 1:
		size = uint64(binary.LittleEndian.Uint16(b[p:]))
		p += 2
	case 2:
		size = uint64(binary.LittleEndian.Uint32(b[p:]))
		p += 4
	default:
		size = binary.LittleEndian.Uint64(b[p:])
		p += 8
	}
	return p - addr + size + 4
}

// testHeaderChecksumOK reports whether the checksum of chunk 0 of the
// version 2 object header at addr in b matches its bytes.
func testHeaderChecksumOK(t *testing.T, b []byte, addr uint64) bool {
	t.Helper()
	n := testChunk0Size(t, b, addr)
	return core.JenkinsChecksum(b[addr:addr+n-4]) == binary.LittleEndian.Uint32(b[addr+n-4:])
}

// testSuperblockSize is the size of the superblock the writer writes: version
// 0 with the root symbol table entry, or version 2 (H5Fsuper_cache.c).
func testSuperblockSize(version uint8) uint64 {
	if version < core.Version2 {
		return 96
	}
	return 48
}

// testDatasetAddr returns the object header address of the dataset at path.
func testDatasetAddr(t *testing.T, file, path string) uint64 {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	ds := findDataset(f, path)
	require.NotNil(t, ds, "dataset %s not found", path)
	return ds.Address()
}

// requireOnlyHeaderChanged checks that between the file images before and
// after, only the superblock, the object header region [addr, addr+hdr) and
// the bytes appended past the old end of file differ.
func requireOnlyHeaderChanged(t *testing.T, before, after []byte, sbSize, addr, hdr uint64) {
	t.Helper()
	require.GreaterOrEqual(t, len(after), len(before), "file shrank")
	for i := sbSize; i < uint64(len(before)); i++ {
		if i >= addr && i < addr+hdr {
			continue
		}
		if before[i] != after[i] {
			require.Failf(t, "bytes outside the object header changed",
				"first difference at %d (header at %d, %d bytes)", i, addr, hdr)
		}
	}
}

// requireH5dumpClean runs h5dump on file when it is installed and requires
// exit status 0 and no error report.
func requireH5dumpClean(t *testing.T, file string) string {
	t.Helper()
	h5dump, err := exec.LookPath("h5dump")
	if err != nil {
		t.Log("h5dump not in PATH: C check skipped")
		return ""
	}
	out, runErr := runH5Err(h5dump, file)
	require.NoError(t, runErr, "h5dump %s:\n%s", file, out)
	require.NotContains(t, out, "error")
	return out
}

// createFilteredDataset writes the reviewer's E1 file: a chunked int32[40]
// dataset with shuffle, deflate and Fletcher32, whose first chunk follows
// its object header.
func createFilteredDataset(t *testing.T, sb uint8) (string, []int32) {
	t.Helper()
	data := make([]int32, 40)
	for i := range data {
		data[i] = int32(i*3 - 7)
	}
	file := writeOne(t, sb, func(fw *FileWriter) (*DatasetWriter, error) {
		return fw.CreateDataset("/filt", Int32, []uint64{40}, WithChunkDims([]uint64{10}),
			WithShuffle(), WithGZIPCompression(6), WithFletcher32())
	}, func(ds *DatasetWriter) error { return ds.Write(data) })
	return file, data
}

func requireFiltData(t *testing.T, file string, want []int32) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	ds := findDataset(f, "/filt")
	require.NotNil(t, ds)
	got, err := ds.Read()
	require.NoError(t, err)
	require.Len(t, got, len(want))
	for i := range want {
		require.InDelta(t, float64(want[i]), got[i], 0, "element %d", i)
	}
}

func requireAttrNames(t *testing.T, file, path string, want ...string) {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	ds := findDataset(f, path)
	require.NotNil(t, ds)
	names, err := ds.ListAttributes()
	require.NoError(t, err)
	require.ElementsMatch(t, want, names)
}

// TestRewriteHeader_OpenForWriteAttributesStayInHeader is finding E1: an
// attribute added to a dataset opened with OpenForWrite must not write past
// the space of the dataset's object header on disk. Before the fix the
// rewrite ran over the first chunk that follows the header.
func TestRewriteHeader_OpenForWriteAttributesStayInHeader(t *testing.T) {
	for _, sb := range []uint8{core.Version0, core.Version2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file, data := createFilteredDataset(t, sb)
			addr := testDatasetAddr(t, file, "/filt")
			before, err := os.ReadFile(file)
			require.NoError(t, err)
			hdr := testChunk0Size(t, before, addr)

			fw, err := OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			ds, err := fw.OpenDataset("/filt")
			require.NoError(t, err)
			for k := 0; k < 3; k++ {
				require.NoError(t, ds.WriteAttribute(fmt.Sprintf("added_%d", k), int32(k)))
			}
			require.NoError(t, fw.Close())

			after, err := os.ReadFile(file)
			require.NoError(t, err)
			requireOnlyHeaderChanged(t, before, after, testSuperblockSize(sb), addr, hdr)
			require.True(t, testHeaderChecksumOK(t, after, addr), "header checksum")
			requireFiltData(t, file, data)
			requireAttrNames(t, file, "/filt", "added_0", "added_1", "added_2")
			requireH5dumpClean(t, file)
		})
	}
}

// TestRewriteHeader_UpsertGrowthStaysInHeader: replacing an attribute with a
// larger value after OpenForWrite is bounded by the header space the same way
// (E1, the upsert path, which rewrote the header without any check).
func TestRewriteHeader_UpsertGrowthStaysInHeader(t *testing.T) {
	for _, sb := range []uint8{core.Version0, core.Version2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file, data := createFilteredDataset(t, sb)

			fw, err := OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			ds, err := fw.OpenDataset("/filt")
			require.NoError(t, err)
			require.NoError(t, ds.WriteAttribute("grow", int32(1)))
			require.NoError(t, fw.Close())

			addr := testDatasetAddr(t, file, "/filt")
			before, err := os.ReadFile(file)
			require.NoError(t, err)
			hdr := testChunk0Size(t, before, addr)

			big := make([]float64, 64)
			for i := range big {
				big[i] = float64(i) / 4
			}
			fw, err = OpenForWrite(file, OpenReadWrite)
			require.NoError(t, err)
			ds, err = fw.OpenDataset("/filt")
			require.NoError(t, err)
			require.NoError(t, ds.WriteAttribute("grow", big))
			require.NoError(t, fw.Close())

			after, err := os.ReadFile(file)
			require.NoError(t, err)
			requireOnlyHeaderChanged(t, before, after, testSuperblockSize(sb), addr, hdr)
			requireFiltData(t, file, data)

			f, err := Open(file)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			got, err := findDataset(f, "/filt").ReadAttribute("grow")
			require.NoError(t, err)
			require.Equal(t, big, got)
			requireH5dumpClean(t, file)
		})
	}
}

// TestRewriteHeader_AttributeBeforeChunkedWrite is finding E2: an attribute
// written before the first Write of a chunked dataset changes the header, and
// the chunk index address set by Write must leave a valid header checksum.
func TestRewriteHeader_AttributeBeforeChunkedWrite(t *testing.T) {
	for _, sb := range []uint8{core.Version0, core.Version2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file := writeOne(t, sb, func(fw *FileWriter) (*DatasetWriter, error) {
				return fw.CreateDataset("/c", Int32, []uint64{6}, WithChunkDims([]uint64{2}))
			}, func(ds *DatasetWriter) error {
				if err := ds.WriteAttribute("units", "m"); err != nil {
					return err
				}
				return ds.Write(sixInt32s())
			})
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			require.True(t, testHeaderChecksumOK(t, b, testDatasetAddr(t, file, "/c")), "header checksum")
			requireAttrNames(t, file, "/c", "units")
			out := requireH5dumpClean(t, file)
			if out != "" {
				require.Contains(t, out, "0, 7, 14, 21, 28, 35")
			}
		})
	}
}

// TestRewriteHeader_EmptyChunkedDatasetUndefinedIndex is finding E3: a
// chunked dataset that was never written has no chunk index, which the
// layout message records as the undefined address (H5Dchunk.c:1167,
// H5D__chunk_init leaves idx_info address HADDR_UNDEF until allocation), and
// readers return the fill value.
func TestRewriteHeader_EmptyChunkedDatasetUndefinedIndex(t *testing.T) {
	for _, sb := range []uint8{core.Version0, core.Version2} {
		t.Run(fmt.Sprintf("sb%d", sb), func(t *testing.T) {
			file := writeOne(t, sb, func(fw *FileWriter) (*DatasetWriter, error) {
				return fw.CreateDataset("/e", Int32, []uint64{6}, WithChunkDims([]uint64{2}))
			}, func(*DatasetWriter) error { return nil })

			layout := headerMessage(t, datasetHeaderMessages(t, file, "/e"), core.MsgDataLayout)
			require.GreaterOrEqual(t, len(layout.Data), 3+8)
			require.Equal(t, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, layout.Data[3:11],
				"chunk index address of an unwritten dataset")

			f, err := Open(file)
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			got, err := findDataset(f, "/e").Read()
			require.NoError(t, err)
			require.Equal(t, []float64{0, 0, 0, 0, 0, 0}, got)

			out := requireH5dumpClean(t, file)
			if out != "" {
				require.Contains(t, out, "0, 0, 0, 0, 0, 0")
			}
		})
	}
}

// TestRewriteHeader_DeleteAfterWriteSameSession is finding E4: an attribute
// written to an opened dataset can be deleted in the same session.
func TestRewriteHeader_DeleteAfterWriteSameSession(t *testing.T) {
	file, data := createFilteredDataset(t, core.Version2)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.OpenDataset("/filt")
	require.NoError(t, err)
	for k := 0; k < 3; k++ {
		require.NoError(t, ds.WriteAttribute(fmt.Sprintf("added_%d", k), int32(k)))
	}
	require.NoError(t, ds.DeleteAttribute("added_1"))
	require.NoError(t, fw.Close())

	requireAttrNames(t, file, "/filt", "added_0", "added_2")
	requireFiltData(t, file, data)
	requireH5dumpClean(t, file)
}

// TestRewriteHeader_ResizeKeepsAttributes: Resize rewrote the header from a
// copy cached before the attributes were written, dropping them.
func TestRewriteHeader_ResizeKeepsAttributes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "r.h5")
	fw, err := CreateForWrite(file, CreateTruncate, WithSuperblockVersion(core.Version2))
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/r", Int32, []uint64{4}, WithChunkDims([]uint64{2}),
		WithMaxDims([]uint64{Unlimited}))
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, 2, 3, 4}))
	require.NoError(t, ds.Resize([]uint64{6}))
	require.NoError(t, ds.WriteAttribute("kept", int32(5)))
	require.NoError(t, ds.Resize([]uint64{8}))
	require.NoError(t, fw.Close())

	requireAttrNames(t, file, "/r", "kept")
	shape := headerMessage(t, datasetHeaderMessages(t, file, "/r"), core.MsgDataspace)
	dsm, err := core.ParseDataspaceMessage(shape.Data)
	require.NoError(t, err)
	require.Equal(t, []uint64{8}, dsm.Dimensions)
	requireH5dumpClean(t, file)
}

// copyFixture copies the read-only fixture src into a temporary directory and
// returns the copy's path.
func copyFixture(t *testing.T, src string) string {
	t.Helper()
	b, err := os.ReadFile(src)
	require.NoError(t, err)
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	require.NoError(t, os.WriteFile(dst, b, 0o600))
	return dst
}

// rootLinkNames returns the names in the link messages of the root group's
// object header in file.
func rootLinkNames(t *testing.T, file string) []string {
	t.Helper()
	f, err := Open(file)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	oh, err := core.ReadObjectHeader(f.reader, f.sb.RootGroup, f.sb)
	require.NoError(t, err)
	var names []string
	for _, msg := range oh.Messages {
		if msg.Type != core.MsgLinkMessage {
			continue
		}
		lm, err := core.ParseLinkMessage(msg.Data, f.sb)
		require.NoError(t, err)
		names = append(names, lm.Name)
	}
	return names
}

// TestLinkMessageGroup_CreateInCompactRoot is finding E5: a C file of the
// 1.8 format keeps the root group's links as link messages in its object
// header (H5Gcompact.c), with no symbol table. Creating a dataset or a soft
// link there adds a link message as H5G_obj_insert does (H5Gobj.c:422-580)
// while the group holds fewer than max_compact links; the link that reaches
// max_compact moves every link to dense storage (H5Gobj.c:474-509).
func TestLinkMessageGroup_CreateInCompactRoot(t *testing.T) {
	file := copyFixture(t, filepath.Join("testdata", "reference", "h5fc_ext1_f.h5"))
	before := rootLinkNames(t, file)
	require.Len(t, before, 6)

	fw, err := OpenForWrite(file, OpenReadWrite)
	require.NoError(t, err)
	ds, err := fw.CreateDataset("/added", Int32, []uint64{4})
	require.NoError(t, err)
	require.NoError(t, ds.Write([]int32{1, -2, 3, -4}))
	require.NoError(t, fw.CreateSoftLink("/soft_added", "/added"))
	// The group now holds max_compact (8) links: the 9th turns it dense.
	third, err := fw.CreateDataset("/third", Int32, []uint64{1})
	require.NoError(t, err)
	require.NoError(t, third.Write([]int32{7}))
	_, dense := linkGroupState(t, fw, fw.rootGroupAddr)
	require.True(t, dense, "root group in dense link storage")
	require.NoError(t, fw.Close())

	// The conversion moves every link message out of the header
	// (H5Gobj.c:476-509).
	require.Empty(t, rootLinkNames(t, file))

	f, err := Open(file)
	require.NoError(t, err)
	added := findDataset(f, "/added")
	require.NotNil(t, added)
	got, err := added.Read()
	require.NoError(t, err)
	require.Equal(t, []float64{1, -2, 3, -4}, got)
	require.NotNil(t, findDataset(f, "/DSET_CONTIGUOUS"), "existing links kept")
	require.NotNil(t, findDataset(f, "/third"), "the 9th link read from dense storage")
	require.NoError(t, f.Close())
	requireLinkGroupFile(t, h5tool(t, "h5ls"), file, "", append(before, "added", "soft_added", "third"))

	out := requireH5dumpClean(t, file)
	if out != "" {
		require.Contains(t, out, `DATASET "added"`)
		require.Contains(t, out, `LINKTARGET "/added"`)
		require.Contains(t, out, `DATASET "third"`)
	}
}
